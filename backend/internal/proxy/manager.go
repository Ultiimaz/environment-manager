package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	TraefikContainerName = "env-traefik"
	CoreDNSContainerName = "env-coredns"
	NetworkName          = "env-manager-net"
	DefaultTraefikIP     = "127.0.0.1"
	DefaultProxyNetwork  = "env-manager-net"
	CoreDNSIP            = "172.21.0.2"
)

// Manager handles Traefik and CoreDNS lifecycle
type Manager struct {
	dockerClient *client.Client
	registry     *Registry
	dataDir      string
	baseDomain   string
	traefikIP    string
	proxyNetwork string
	logger       *zap.Logger
}

// NewManager creates a new proxy manager
func NewManager(dataDir, baseDomain, traefikIP, proxyNetwork string, registry *Registry, logger *zap.Logger) (*Manager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	if traefikIP == "" {
		traefikIP = DefaultTraefikIP
	}
	if proxyNetwork == "" {
		proxyNetwork = DefaultProxyNetwork
	}

	return &Manager{
		dockerClient: cli,
		registry:     registry,
		dataDir:      dataDir,
		baseDomain:   baseDomain,
		traefikIP:    traefikIP,
		proxyNetwork: proxyNetwork,
		logger:       logger,
	}, nil
}

// Close closes the docker client
func (m *Manager) Close() error {
	return m.dockerClient.Close()
}

// IsTraefikRunning checks if Traefik container is running
func (m *Manager) IsTraefikRunning(ctx context.Context) (bool, error) {
	return m.isContainerRunning(ctx, TraefikContainerName)
}

// IsCoreDNSRunning checks if CoreDNS container is running
func (m *Manager) IsCoreDNSRunning(ctx context.Context) (bool, error) {
	return m.isContainerRunning(ctx, CoreDNSContainerName)
}

func (m *Manager) isContainerRunning(ctx context.Context, name string) (bool, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", name)

	containers, err := m.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return false, err
	}

	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name {
				return c.State == "running", nil
			}
		}
	}

	return false, nil
}

// GetBaseDomain returns the configured base domain
func (m *Manager) GetBaseDomain() string {
	return m.baseDomain
}

// SetBaseDomain updates the base domain
func (m *Manager) SetBaseDomain(domain string) {
	m.baseDomain = domain
}

// UpdateCoreDNS regenerates the Corefile with current subdomains
func (m *Manager) UpdateCoreDNS(ctx context.Context) error {
	entries := m.registry.List()

	corefile := m.generateCorefile(entries)

	corefilePath := filepath.Join(m.dataDir, "network", "Corefile")
	if err := os.MkdirAll(filepath.Dir(corefilePath), 0755); err != nil {
		return fmt.Errorf("failed to create network dir: %w", err)
	}

	if err := os.WriteFile(corefilePath, []byte(corefile), 0644); err != nil {
		return fmt.Errorf("failed to write Corefile: %w", err)
	}

	m.logger.Info("Updated CoreDNS Corefile", zap.Int("subdomains", len(entries)))

	// Reload CoreDNS if running (send SIGUSR1 or restart)
	if running, _ := m.IsCoreDNSRunning(ctx); running {
		if err := m.reloadCoreDNS(ctx); err != nil {
			m.logger.Warn("Failed to reload CoreDNS", zap.Error(err))
		}
	}

	return nil
}

func (m *Manager) generateCorefile(entries []SubdomainEntry) string {
	var sb strings.Builder

	// Main domain block with wildcard pointing to Traefik
	// Using template plugin for wildcard DNS resolution
	sb.WriteString(fmt.Sprintf("%s {\n", m.baseDomain))
	sb.WriteString("    hosts {\n")

	// Add explicit entries for each registered subdomain (for faster resolution)
	for _, entry := range entries {
		sb.WriteString(fmt.Sprintf("        %s %s.%s\n", m.traefikIP, entry.Subdomain, m.baseDomain))
	}

	// Add traefik.baseDomain for Traefik dashboard
	sb.WriteString(fmt.Sprintf("        %s traefik.%s\n", m.traefikIP, m.baseDomain))

	sb.WriteString("        fallthrough\n")
	sb.WriteString("    }\n")

	// Template plugin for wildcard - catches any subdomain not in hosts
	sb.WriteString(fmt.Sprintf("    template IN A %s {\n", m.baseDomain))
	sb.WriteString(fmt.Sprintf("        answer \"{{ .Name }} 60 IN A %s\"\n", m.traefikIP))
	sb.WriteString("    }\n")

	sb.WriteString("    log\n")
	sb.WriteString("}\n\n")

	// Fallback to upstream DNS
	sb.WriteString(". {\n")
	sb.WriteString("    forward . 8.8.8.8 8.8.4.4\n")
	sb.WriteString("    log\n")
	sb.WriteString("}\n")

	return sb.String()
}

func (m *Manager) reloadCoreDNS(ctx context.Context) error {
	// CoreDNS doesn't support reload via signal in all configurations
	// The safest way is to restart the container
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", CoreDNSContainerName)

	containers, err := m.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return err
	}

	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+CoreDNSContainerName {
				m.logger.Info("Restarting CoreDNS to apply config changes")
				return m.dockerClient.ContainerRestart(ctx, c.ID, container.StopOptions{})
			}
		}
	}

	return nil
}

// RegisterSubdomain registers a new subdomain and updates CoreDNS
func (m *Manager) RegisterSubdomain(ctx context.Context, entry SubdomainEntry) error {
	if err := m.registry.Register(entry); err != nil {
		return err
	}

	return m.UpdateCoreDNS(ctx)
}

// UnregisterSubdomain removes a subdomain and updates CoreDNS
func (m *Manager) UnregisterSubdomain(ctx context.Context, subdomain string) error {
	if err := m.registry.Unregister(subdomain); err != nil {
		return err
	}

	return m.UpdateCoreDNS(ctx)
}

// UnregisterProject removes all subdomains for a project
func (m *Manager) UnregisterProject(ctx context.Context, projectName string) error {
	if err := m.registry.UnregisterByProject(projectName); err != nil {
		return err
	}

	return m.UpdateCoreDNS(ctx)
}

// GetRoutes returns all registered routes
func (m *Manager) GetRoutes() []SubdomainEntry {
	return m.registry.List()
}

// IsSubdomainAvailable checks if a subdomain is available
func (m *Manager) IsSubdomainAvailable(subdomain string) bool {
	return m.registry.IsAvailable(subdomain)
}

// GenerateTraefikLabels generates Traefik labels for a service
func (m *Manager) GenerateTraefikLabels(subdomain, serviceName string, port int) map[string]string {
	routerName := fmt.Sprintf("%s-%s", subdomain, serviceName)

	return map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", routerName):                      fmt.Sprintf("Host(`%s.%s`)", subdomain, m.baseDomain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", routerName):               "web",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName): fmt.Sprintf("%d", port),
		"traefik.docker.network": m.proxyNetwork,
	}
}

// SubdomainInfo contains subdomain configuration for a service
type SubdomainInfo struct {
	Subdomain string
	Port      int
}

// InjectTraefikLabels injects Traefik labels and the proxy network into a
// docker-compose YAML using yaml.v3 node-level edits, so existing labels:
// and networks: keys are merged rather than duplicated.
func (m *Manager) InjectTraefikLabels(composeContent string, subdomains map[string]SubdomainInfo) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(composeContent), &doc); err != nil {
		return "", fmt.Errorf("parse compose YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return "", fmt.Errorf("compose YAML is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", fmt.Errorf("compose YAML root is not a mapping")
	}

	services := findMapValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return "", fmt.Errorf("compose YAML has no services mapping")
	}

	for serviceName, info := range subdomains {
		svc := findMapValue(services, serviceName)
		if svc == nil || svc.Kind != yaml.MappingNode {
			continue
		}

		labels := m.GenerateTraefikLabels(info.Subdomain, serviceName, info.Port)
		ensureLabels(svc, labels)
		ensureNetworkOnService(svc, m.proxyNetwork)
	}

	ensureExternalNetwork(root, m.proxyNetwork)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("marshal compose YAML: %w", err)
	}
	return string(out), nil
}

func findMapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMapValue(m *yaml.Node, key string, value *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		value,
	)
}

func sequenceContainsScalar(seq *yaml.Node, value string) bool {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return false
	}
	for _, n := range seq.Content {
		if n.Kind == yaml.ScalarNode && n.Value == value {
			return true
		}
	}
	return false
}

// ensureLabels merges labels into the service. If labels: is missing, it's
// created as a sequence. Existing map-form labels are converted to sequence
// form (key=value strings) so we don't have to support both shapes downstream.
func ensureLabels(svc *yaml.Node, labels map[string]string) {
	labelsNode := findMapValue(svc, "labels")
	if labelsNode == nil {
		labelsNode = &yaml.Node{Kind: yaml.SequenceNode}
		setMapValue(svc, "labels", labelsNode)
	} else if labelsNode.Kind == yaml.MappingNode {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for i := 0; i+1 < len(labelsNode.Content); i += 2 {
			seq.Content = append(seq.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: fmt.Sprintf("%s=%s", labelsNode.Content[i].Value, labelsNode.Content[i+1].Value),
			})
		}
		*labelsNode = *seq
	}
	if labelsNode.Kind != yaml.SequenceNode {
		return
	}
	for k, v := range labels {
		entry := fmt.Sprintf("%s=%s", k, v)
		if !sequenceContainsScalar(labelsNode, entry) {
			labelsNode.Content = append(labelsNode.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: entry,
			})
		}
	}
}

// ensureNetworkOnService makes sure the service's networks list contains the
// proxy network. Long-form (mapping) networks declarations are left alone.
func ensureNetworkOnService(svc *yaml.Node, network string) {
	networksNode := findMapValue(svc, "networks")
	if networksNode == nil {
		setMapValue(svc, "networks", &yaml.Node{
			Kind: yaml.SequenceNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: network},
			},
		})
		return
	}
	if networksNode.Kind == yaml.SequenceNode && !sequenceContainsScalar(networksNode, network) {
		networksNode.Content = append(networksNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: network,
		})
	}
}

// ensureExternalNetwork makes sure the top-level networks: block declares the
// proxy network as external so the compose project joins the existing macvlan.
func ensureExternalNetwork(root *yaml.Node, network string) {
	topNetworks := findMapValue(root, "networks")
	if topNetworks == nil {
		topNetworks = &yaml.Node{Kind: yaml.MappingNode}
		setMapValue(root, "networks", topNetworks)
	}
	if topNetworks.Kind != yaml.MappingNode {
		return
	}
	if findMapValue(topNetworks, network) == nil {
		setMapValue(topNetworks, network, &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "external"},
				{Kind: yaml.ScalarNode, Value: "true"},
			},
		})
	}
}
