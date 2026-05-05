package builder

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// InjectPaasNet rewrites the compose file at composePath so every service
// has `network` listed in its `networks:` and the top-level `networks:`
// declares it as external.
//
// Idempotent: re-running on an already-injected file is a no-op. Empty
// network = noop (the function returns nil without touching the file),
// matching InjectTraefikLabels' bypass behaviour.
//
// Reuses the package-private yaml helpers from labels.go
// (labelsEnsureNetworkOnService, labelsEnsureExternalNetwork).
func InjectPaasNet(composePath string, network string) error {
	if network == "" {
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

	// Add paas-net to every service.
	for i := 0; i+1 < len(services.Content); i += 2 {
		svc := services.Content[i+1]
		if svc == nil || svc.Kind != yaml.MappingNode {
			continue
		}
		labelsEnsureNetworkOnService(svc, network)
	}

	// Top-level networks: <network>: { external: true }
	labelsEnsureExternalNetwork(root, network)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal compose YAML: %w", err)
	}
	return os.WriteFile(composePath, out, 0644)
}
