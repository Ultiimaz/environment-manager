package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func TestRenderCompose_InjectsPlatformEnvVars(t *testing.T) {
	repo := t.TempDir()
	composeSrc := `services:
  app:
    image: hello-world
`
	composePath := filepath.Join(repo, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeSrc), 0644); err != nil {
		t.Fatal(err)
	}

	envDir := t.TempDir()
	project := &models.Project{Name: "myapp", DefaultBranch: "main"}
	env := &models.Environment{
		Branch:     "feature/x",
		BranchSlug: "feature-x",
		Kind:       models.EnvKindPreview,
		URL:        "feature-x.myapp.home",
	}

	if err := RenderCompose(composePath, envDir, project, env); err != nil {
		t.Fatalf("RenderCompose: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(envDir, "docker-compose.yaml"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(out)
	mustContain := []string{
		`PROJECT_NAME: "myapp"`,
		`BRANCH: "feature/x"`,
		`ENV_KIND: "preview"`,
		`ENV_URL: "feature-x.myapp.home"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output:\n%s", want, got)
		}
	}
}

func TestRenderCompose_FailsOnMissingSource(t *testing.T) {
	envDir := t.TempDir()
	err := RenderCompose(filepath.Join(envDir, "missing.yml"), envDir,
		&models.Project{Name: "p"},
		&models.Environment{Kind: models.EnvKindProd, BranchSlug: "main"})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestRenderCompose_PreservesExistingServices(t *testing.T) {
	repo := t.TempDir()
	composeSrc := `services:
  app:
    image: nginx:alpine
    ports:
      - "8080:80"
  worker:
    image: redis:7
`
	composePath := filepath.Join(repo, "docker-compose.yml")
	_ = os.WriteFile(composePath, []byte(composeSrc), 0644)

	envDir := t.TempDir()
	project := &models.Project{Name: "p", DefaultBranch: "main"}
	env := &models.Environment{Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd, URL: "p.home"}
	if err := RenderCompose(composePath, envDir, project, env); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(filepath.Join(envDir, "docker-compose.yaml"))
	got := string(out)
	for _, svc := range []string{"app", "worker", "nginx:alpine", "redis:7"} {
		if !strings.Contains(got, svc) {
			t.Errorf("output missing %q\n--- output:\n%s", svc, got)
		}
	}
}
