package projects

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/models"
)

func writeYAML(t *testing.T, path string, v interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRunLegacyMigration_LinkedComposeProject(t *testing.T) {
	dataDir := t.TempDir()

	// Seed: a Repository under data/repos/myapp
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	writeYAML(t, filepath.Join(repoDir, ".repo-meta.yaml"), &models.Repository{
		ID:        "repoid01",
		Name:      "myapp",
		URL:       "https://github.com/u/myapp",
		Branch:    "main",
		LocalPath: repoDir,
	})
	// Seed: a ComposeProject linked to that repo
	writeYAML(t, filepath.Join(dataDir, "compose", "myapp", "config.yaml"), &models.ComposeProject{
		ProjectName:     "myapp",
		ComposeFile:     "docker-compose.yaml",
		DesiredState:    "running",
		RepoID:          "repoid01",
		RepoComposePath: "docker-compose.yaml",
	})

	loader := config.NewLoader(dataDir)
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatalf("RunLegacyMigration: %v", err)
	}

	projects, err := store.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	p := projects[0]
	if p.RepoURL != "https://github.com/u/myapp" {
		t.Errorf("RepoURL = %q, want repo URL", p.RepoURL)
	}
	if p.MigratedFromCompose != "myapp" {
		t.Errorf("MigratedFromCompose = %q, want myapp", p.MigratedFromCompose)
	}

	envs, err := store.ListEnvironments(p.ID)
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("expected 1 env, got %d", len(envs))
	}
	if envs[0].Kind != models.EnvKindLegacy {
		t.Errorf("env Kind = %v, want legacy", envs[0].Kind)
	}
}

func TestRunLegacyMigration_UnlinkedComposeProject(t *testing.T) {
	dataDir := t.TempDir()
	writeYAML(t, filepath.Join(dataDir, "compose", "standalone", "config.yaml"), &models.ComposeProject{
		ProjectName:  "standalone",
		ComposeFile:  "docker-compose.yaml",
		DesiredState: "running",
	})

	loader := config.NewLoader(dataDir)
	store, _ := NewStore(dataDir)
	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}
	projects, _ := store.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].RepoURL != "" {
		t.Errorf("expected empty RepoURL for unlinked project")
	}
}

func TestRunLegacyMigration_Idempotent(t *testing.T) {
	dataDir := t.TempDir()
	writeYAML(t, filepath.Join(dataDir, "compose", "x", "config.yaml"), &models.ComposeProject{
		ProjectName: "x", ComposeFile: "docker-compose.yaml", DesiredState: "running",
	})
	loader := config.NewLoader(dataDir)
	store, _ := NewStore(dataDir)

	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}
	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}

	projects, _ := store.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project after re-run, got %d", len(projects))
	}
}

func TestRunLegacyMigration_NoComposeProjects(t *testing.T) {
	dataDir := t.TempDir()
	loader := config.NewLoader(dataDir)
	store, _ := NewStore(dataDir)
	if err := RunLegacyMigration(store, loader, dataDir); err != nil {
		t.Fatal(err)
	}
	// Marker should still be written so a future-empty state remains migrated.
	if _, err := os.Stat(filepath.Join(store.Root(), ".migrated")); err != nil {
		t.Errorf("expected .migrated marker even with no compose projects: %v", err)
	}
}
