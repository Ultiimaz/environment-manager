package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectDevDir_Valid(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "project_name: myapp\n",
		".dev/secrets.example.env":     "APP_KEY=\n",
	})

	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if info.Config.ProjectName != "myapp" {
		t.Errorf("ProjectName = %q, want myapp", info.Config.ProjectName)
	}
	if len(info.SecretKeys) != 1 || info.SecretKeys[0] != "APP_KEY" {
		t.Errorf("SecretKeys = %v, want [APP_KEY]", info.SecretKeys)
	}
}

func TestDetectDevDir_MissingDevDir(t *testing.T) {
	repo := makeRepo(t, map[string]string{"README.md": "no .dev here\n"})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for missing .dev directory")
	}
}

func TestDetectDevDir_MissingRequiredFile(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		// missing docker-compose.dev.yml
		".dev/config.yaml": "project_name: myapp\n",
	})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for missing docker-compose.dev.yml")
	}
}

func TestDetectDevDir_NoSecretsFile(t *testing.T) {
	// secrets.example.env is OPTIONAL — its absence yields nil keys, no error.
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "",
	})
	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if info.SecretKeys != nil {
		t.Errorf("SecretKeys = %v, want nil for missing secrets file", info.SecretKeys)
	}
}

func TestDetectDevDir_InvalidConfig(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "database:\n  engine: cockroach\n  version: \"23\"\n",
	})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for invalid config (unknown DB engine)")
	}
}
