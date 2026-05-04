// Package builder orchestrates compose rendering and execution for a
// project's environment. The render step rewrites the source compose YAML
// with platform environment variables; the runner exec's docker compose
// against the rendered file.
package builder

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// RenderCompose reads the source compose file, augments each service with
// platform environment variables (PROJECT_NAME, BRANCH, ENV_KIND, ENV_URL),
// and writes the result to envDir/docker-compose.yaml.
//
// Traefik label injection is intentionally NOT done here — it lives in
// proxy.Manager.InjectTraefikLabels and runs as a separate pass from
// Runner.Build. Keeping the renderer pure (no proxy/registry deps) makes
// it test-friendly.
func RenderCompose(srcPath, envDir string, project *models.Project, env *models.Environment) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source compose: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse source compose: %w", err)
	}

	platformEnv := map[string]string{
		"PROJECT_NAME": project.Name,
		"BRANCH":       env.Branch,
		"ENV_KIND":     string(env.Kind),
		"ENV_URL":      env.URL,
	}
	if err := injectServiceEnv(&doc, platformEnv); err != nil {
		return fmt.Errorf("inject platform env: %w", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(envDir, 0755); err != nil {
		return fmt.Errorf("mkdir env dir: %w", err)
	}
	dst := filepath.Join(envDir, "docker-compose.yaml")
	if err := os.WriteFile(dst, out, 0644); err != nil {
		return fmt.Errorf("write rendered compose: %w", err)
	}
	return nil
}

// injectServiceEnv walks the parsed compose doc and merges platform-level
// env vars into each service's `environment:` section. Existing values are
// not overwritten — user compose can shadow platform vars deliberately.
func injectServiceEnv(doc *yaml.Node, vars map[string]string) error {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected top-level kind: %v", root.Kind)
	}
	servicesNode := mapValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(servicesNode.Content); i += 2 {
		svc := servicesNode.Content[i+1]
		if svc.Kind != yaml.MappingNode {
			continue
		}
		envNode := mapValue(svc, "environment")
		if envNode == nil {
			envNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			svc.Content = append(svc.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "environment"},
				envNode,
			)
		}
		if envNode.Kind != yaml.MappingNode {
			// sequence-form environment lists not supported here; skip.
			continue
		}
		for k, v := range vars {
			if mapValue(envNode, k) != nil {
				continue
			}
			envNode.Content = append(envNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: k},
				&yaml.Node{Kind: yaml.ScalarNode, Value: v, Style: yaml.DoubleQuotedStyle},
			)
		}
	}
	return nil
}

// mapValue returns the value node for key in a yaml mapping, or nil if absent.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
