package builder

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/iac"
	"github.com/environment-manager/backend/internal/models"
)

// TraefikOptions configures InjectTraefikLabels behaviour.
//
//   - ProxyNetwork: name of the external network Traefik listens on (matches
//     the existing v1 contract). Empty → InjectTraefikLabels is a noop.
//   - Domains: per-env domain config from iac.Config.Domains. Nil → legacy
//     single-router behaviour (one HTTP router on env.URL).
//   - LetsencryptEmail: required for HTTPS+LE on non-.home domains. Empty →
//     public domains fall back to HTTP-only routers; caller is expected to
//     emit a warning. Plan 5 does NOT mutate Traefik command flags — that's
//     a manual one-time host op covered by Plan 8.
type TraefikOptions struct {
	ProxyNetwork     string
	Domains          *iac.Domains
	LetsencryptEmail string
}

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
func InjectTraefikLabels(composePath string, env *models.Environment, expose *models.ExposeSpec, opts TraefikOptions) error {
	if opts.ProxyNetwork == "" {
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

	// Compute label set: legacy single-router path when Domains is nil,
	// multi-domain v2 path otherwise.
	labels := buildTraefikLabels(env, targetPort, opts)

	labelsEnsureLabels(svc, labels)
	labelsEnsureNetworkOnService(svc, opts.ProxyNetwork)
	labelsEnsureExternalNetwork(root, opts.ProxyNetwork)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal compose YAML: %w", err)
	}
	return os.WriteFile(composePath, out, 0644)
}

// buildTraefikLabels assembles the Traefik label map for a service.
//
// Two modes:
//
//   - Legacy: opts.Domains == nil. Emits one HTTP router named env.ID with
//     Host(env.URL). Existing behaviour, preserved exactly.
//
//   - v2: opts.Domains != nil. Emits a -home HTTP router for env.URL plus
//     a -public router for the iac-declared custom domains (HTTPS+LE when
//     opts.LetsencryptEmail is set, HTTP fallback otherwise). All routers
//     share the same backend service definition (env.ID) via an explicit
//     `.service` label on each suffixed router.
func buildTraefikLabels(env *models.Environment, targetPort int, opts TraefikOptions) map[string]string {
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": opts.ProxyNetwork,
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", env.ID): strconv.Itoa(targetPort),
	}

	if opts.Domains == nil {
		// Legacy single HTTP router on env.URL — preserve exact existing shape.
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", env.ID)] = fmt.Sprintf("Host(`%s`)", env.URL)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", env.ID)] = "web"
		return labels
	}

	// v2 path. Emit -home router for env.URL.
	if env.URL != "" {
		homeRouter := env.ID + "-home"
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", homeRouter)] = fmt.Sprintf("Host(`%s`)", env.URL)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", homeRouter)] = "web"
		labels[fmt.Sprintf("traefik.http.routers.%s.service", homeRouter)] = env.ID
	}

	// Public domains: prod uses Domains.Prod directly; preview is added in Task 4.
	publicHosts := opts.Domains.Prod

	if len(publicHosts) > 0 {
		publicRouter := env.ID + "-public"
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", publicRouter)] = formatHostRule(publicHosts)
		labels[fmt.Sprintf("traefik.http.routers.%s.service", publicRouter)] = env.ID
		if opts.LetsencryptEmail != "" {
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "websecure"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls", publicRouter)] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", publicRouter)] = "letsencrypt"
		} else {
			// LE not configured — emit HTTP-only public router so the domains
			// are at least reachable. Caller (runner) is expected to log a
			// warning. Task 3 adds the redirect-to-HTTPS router only when
			// LE is configured.
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "web"
		}
	}

	return labels
}

// formatHostRule joins multiple hostnames into a Traefik Host(...) rule
// using the || operator, e.g. Host(`a.com`) || Host(`b.com`).
func formatHostRule(hosts []string) string {
	parts := make([]string, len(hosts))
	for i, h := range hosts {
		parts[i] = fmt.Sprintf("Host(`%s`)", h)
	}
	return strings.Join(parts, " || ")
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
		// Service had no `networks:` clause — meaning it was implicitly on
		// the compose project's `default` network (where its peers live,
		// e.g. mysql, redis). Preserve that connectivity by listing both
		// `default` and the proxy network explicitly. Without `default`,
		// the service can't reach its compose-mate containers.
		labelsSetMapValue(svc, "networks", &yaml.Node{
			Kind: yaml.SequenceNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "default"},
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
