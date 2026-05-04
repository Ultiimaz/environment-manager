package builder

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// InjectTraefikLabels reads the compose file at composePath, injects Traefik
// routing labels and the proxy network onto the target service, and writes the
// file back.
//
// Target service selection:
//   - If expose != nil: use expose.Service + expose.Port.
//   - Otherwise: find the first service that has a ports: declaration and
//     extract the first port number from it.
//   - If no target can be found, return nil (no-op).
//
// When proxyNetwork is empty the function returns nil immediately so that
// tests that pass "" bypass label injection entirely.
func InjectTraefikLabels(composePath string, env *models.Environment, expose *models.ExposeSpec, proxyNetwork string) error {
	if proxyNetwork == "" {
		return nil
	}

	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("read compose: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse compose YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("compose YAML is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("compose YAML root is not a mapping")
	}

	services := labelsFindMapValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return fmt.Errorf("compose YAML has no services mapping")
	}

	// Determine target service + port.
	targetService, targetPort, ok := resolveTarget(services, expose)
	if !ok {
		// No routable service found — skip injection silently.
		return nil
	}

	svc := labelsFindMapValue(services, targetService)
	if svc == nil || svc.Kind != yaml.MappingNode {
		return fmt.Errorf("target service %q not found in compose", targetService)
	}

	// Router name is the environment ID (already slug-safe).
	routerName := env.ID
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", routerName):                      fmt.Sprintf("Host(`%s`)", env.URL),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", routerName):               "web",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName): strconv.Itoa(targetPort),
		"traefik.docker.network": proxyNetwork,
	}

	labelsEnsureLabels(svc, labels)
	labelsEnsureNetworkOnService(svc, proxyNetwork)
	labelsEnsureExternalNetwork(root, proxyNetwork)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal compose YAML: %w", err)
	}
	return os.WriteFile(composePath, out, 0644)
}

// resolveTarget picks the service name + port to route. Returns ok=false when
// no suitable target exists.
func resolveTarget(services *yaml.Node, expose *models.ExposeSpec) (name string, port int, ok bool) {
	if expose != nil {
		return expose.Service, expose.Port, true
	}
	// Convention: first service with a ports: declaration.
	for i := 0; i+1 < len(services.Content); i += 2 {
		svcName := services.Content[i].Value
		svc := services.Content[i+1]
		if svc == nil || svc.Kind != yaml.MappingNode {
			continue
		}
		portsNode := labelsFindMapValue(svc, "ports")
		if portsNode == nil {
			continue
		}
		p, found := extractFirstPort(portsNode)
		if found {
			return svcName, p, true
		}
	}
	return "", 0, false
}

// extractFirstPort attempts to read the first port number from a ports: node.
// Handles both sequence-of-scalars ("host:container" or bare "port") and
// sequence-of-mappings (long-form: {target: N, ...}).
func extractFirstPort(ports *yaml.Node) (int, bool) {
	if ports == nil {
		return 0, false
	}
	if ports.Kind == yaml.SequenceNode && len(ports.Content) > 0 {
		first := ports.Content[0]
		switch first.Kind {
		case yaml.ScalarNode:
			return parsePortScalar(first.Value)
		case yaml.MappingNode:
			// Long-form: target: <port>
			targetNode := labelsFindMapValue(first, "target")
			if targetNode != nil && targetNode.Kind == yaml.ScalarNode {
				p, err := strconv.Atoi(targetNode.Value)
				if err == nil && p > 0 {
					return p, true
				}
			}
			// Fall back to published:
			publishedNode := labelsFindMapValue(first, "published")
			if publishedNode != nil && publishedNode.Kind == yaml.ScalarNode {
				p, err := strconv.Atoi(publishedNode.Value)
				if err == nil && p > 0 {
					return p, true
				}
			}
		}
	}
	return 0, false
}

// parsePortScalar extracts the container port from a "hostPort:containerPort"
// or bare "port" string (e.g. "8080:3000" → 3000, "3000" → 3000).
func parsePortScalar(s string) (int, bool) {
	// Remove /tcp or /udp suffixes.
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	// "hostPort:containerPort" → container port is the last segment.
	if idx := strings.LastIndexByte(s, ':'); idx >= 0 {
		s = s[idx+1:]
	}
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p <= 0 {
		return 0, false
	}
	return p, true
}

// --- yaml.Node helpers (local copies so we don't import proxy internals) ---

func labelsFindMapValue(m *yaml.Node, key string) *yaml.Node {
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

func labelsSetMapValue(m *yaml.Node, key string, value *yaml.Node) {
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

func labelsSequenceContainsScalar(seq *yaml.Node, value string) bool {
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

func labelsEnsureLabels(svc *yaml.Node, labels map[string]string) {
	labelsNode := labelsFindMapValue(svc, "labels")
	if labelsNode == nil {
		labelsNode = &yaml.Node{Kind: yaml.SequenceNode}
		labelsSetMapValue(svc, "labels", labelsNode)
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
		if !labelsSequenceContainsScalar(labelsNode, entry) {
			labelsNode.Content = append(labelsNode.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: entry,
			})
		}
	}
}

func labelsEnsureNetworkOnService(svc *yaml.Node, network string) {
	networksNode := labelsFindMapValue(svc, "networks")
	if networksNode == nil {
		labelsSetMapValue(svc, "networks", &yaml.Node{
			Kind: yaml.SequenceNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: network},
			},
		})
		return
	}
	if networksNode.Kind == yaml.SequenceNode && !labelsSequenceContainsScalar(networksNode, network) {
		networksNode.Content = append(networksNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: network,
		})
	}
}

func labelsEnsureExternalNetwork(root *yaml.Node, network string) {
	topNetworks := labelsFindMapValue(root, "networks")
	if topNetworks == nil {
		topNetworks = &yaml.Node{Kind: yaml.MappingNode}
		labelsSetMapValue(root, "networks", topNetworks)
	}
	if topNetworks.Kind != yaml.MappingNode {
		return
	}
	if labelsFindMapValue(topNetworks, network) == nil {
		labelsSetMapValue(topNetworks, network, &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "external"},
				{Kind: yaml.ScalarNode, Value: "true"},
			},
		})
	}
}
