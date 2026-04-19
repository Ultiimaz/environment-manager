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

// InjectTraefikLabels injects Traefik labels into a docker-compose YAML string
// This is a simple string-based injection - for production, use proper YAML parsing
func (m *Manager) InjectTraefikLabels(composeContent string, subdomains map[string]SubdomainInfo) (string, error) {
	// For each service with a subdomain, we need to add labels
	// This is a simplified version - proper implementation would parse YAML

	for serviceName, info := range subdomains {
		labels := m.GenerateTraefikLabels(info.Subdomain, serviceName, info.Port)

		// Find the service in the compose content and add labels
		// This is a basic implementation - could be improved with proper YAML parsing
		serviceMarker := fmt.Sprintf("  %s:", serviceName)
		if !strings.Contains(composeContent, serviceMarker) {
			continue
		}

		// Build labels string
		var labelLines strings.Builder
		labelLines.WriteString("    labels:\n")
		for k, v := range labels {
			labelLines.WriteString(fmt.Sprintf("      - \"%s=%s\"\n", k, v))
		}

		// Also add network
		networkLine := fmt.Sprintf("    networks:\n      - %s\n", m.proxyNetwork)

		// Find insertion point (after the service name line)
		// This is simplified - proper implementation would use YAML library
		composeContent = injectAfterService(composeContent, serviceName, labelLines.String()+networkLine)
	}

	// Ensure the proxy network is declared as external at the bottom.
	// strings.Contains on "networks:" is too broad (would match service-level
	// networks keys), so check for a top-level declaration of our network.
	proxyNetDecl := fmt.Sprintf("\n  %s:\n    external: true", m.proxyNetwork)
	if !strings.Contains(composeContent, proxyNetDecl) {
		if strings.Contains(composeContent, "\nnetworks:\n") {
			composeContent += fmt.Sprintf("%s\n", proxyNetDecl)
		} else {
			composeContent += fmt.Sprintf("\nnetworks:%s\n", proxyNetDecl)
		}
	}

	return composeContent, nil
}

// SubdomainInfo contains subdomain configuration for a service
type SubdomainInfo struct {
	Subdomain string
	Port      int
}

// injectAfterService is a helper to inject content after a service definition
func injectAfterService(content, serviceName, injection string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inService := false
	injected := false

	for i, line := range lines {
		result = append(result, line)

		// Check if we're entering the target service
		if strings.HasPrefix(line, "  "+serviceName+":") {
			inService = true
			continue
		}

		// If we're in the service and hit the next service or end of services block
		if inService && !injected {
			// Check if next line starts a new service or section
			if i+1 < len(lines) {
				nextLine := lines[i+1]
				// If next line is a new service (starts with 2 spaces + name + colon) or a new top-level key
				if (len(nextLine) > 2 && nextLine[0:2] == "  " && nextLine[2] != ' ' && strings.Contains(nextLine, ":")) ||
					(len(nextLine) > 0 && nextLine[0] != ' ' && strings.Contains(nextLine, ":")) {
					// Insert before next service
					injectionLines := strings.Split(strings.TrimRight(injection, "\n"), "\n")
					result = append(result[:len(result)-1], injectionLines...)
					result = append(result, line)
					injected = true
					inService = false
				}
			}
		}
	}

	// If we never injected (service was last), append at the end of the service
	if !injected && inService {
		injectionLines := strings.Split(strings.TrimRight(injection, "\n"), "\n")
		result = append(result, injectionLines...)
	}

	return strings.Join(result, "\n")
}
