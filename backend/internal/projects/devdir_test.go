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

const validV2Config = `project_name: myapp
expose:
  service: app
  port: 80
`

func TestDetectDevDir_Valid(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             validV2Config,
	})

	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if info.Config.ProjectName != "myapp" {
		t.Errorf("ProjectName = %q, want myapp", info.Config.ProjectName)
	}
	if info.Config.Expose.Service != "app" || info.Config.Expose.Port != 80 {
		t.Errorf("Expose = %+v, want {app 80}", info.Config.Expose)
	}
}

func TestDetectDevDir_SecretsFromConfig(t *testing.T) {
	cfg := `project_name: myapp
expose:
  service: app
  port: 80
secrets:
  - APP_KEY
  - DB_PASSWORD
`
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             cfg,
	})
	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if len(info.SecretKeys) != 2 || info.SecretKeys[0] != "APP_KEY" || info.SecretKeys[1] != "DB_PASSWORD" {
		t.Errorf("SecretKeys = %v, want [APP_KEY DB_PASSWORD]", info.SecretKeys)
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
		".dev/config.yaml": validV2Config,
	})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for missing docker-compose.dev.yml")
	}
}

func TestDetectDevDir_NoSecretsBlock(t *testing.T) {
	// secrets is optional in v2 schema — empty list yields nil SecretKeys.
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             validV2Config,
	})
	info, err := DetectDevDir(repo)
	if err != nil {
		t.Fatalf("DetectDevDir: %v", err)
	}
	if info.SecretKeys != nil {
		t.Errorf("SecretKeys = %v, want nil for omitted secrets block", info.SecretKeys)
	}
}

func TestDetectDevDir_InvalidConfig(t *testing.T) {
	repo := makeRepo(t, map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		// expose.port out of range — iac.Parse rejects.
		".dev/config.yaml": "project_name: myapp\nexpose:\n  service: app\n  port: 99999\n",
	})
	_, err := DetectDevDir(repo)
	if err == nil {
		t.Fatal("expected error for invalid config (port out of range)")
	}
}
