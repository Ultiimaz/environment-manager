package projects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/models"
)

type fakeSpawner struct {
	spawned  []string // branch names
	tornDown []string // env IDs
}

func (f *fakeSpawner) SpawnPreview(ctx context.Context, project *models.Project, branch, slug string) error {
	f.spawned = append(f.spawned, branch)
	return nil
}

func (f *fakeSpawner) Teardown(ctx context.Context, env *models.Environment) error {
	f.tornDown = append(f.tornDown, env.ID)
	return nil
}

func setupReconcileFixture(t *testing.T) (*Store, *models.Project, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dataDir := t.TempDir()
	store, _ := NewStore(dataDir)

	// Create bare upstream + local clone with one branch
	upstream := filepath.Join(dataDir, "upstream.git")
	if err := os.MkdirAll(upstream, 0755); err != nil {
		t.Fatal(err)
	}
	runIn := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	runIn(upstream, "init", "--bare", "-b", "main")

	workdir := filepath.Join(dataDir, "work")
	if err := os.MkdirAll(filepath.Join(workdir, ".dev"), 0755); err != nil {
		t.Fatal(err)
	}
	for f, c := range map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services: {}\n",
		".dev/docker-compose.dev.yml":  "services: {}\n",
		".dev/config.yaml":             "project_name: r\n",
	} {
		if err := os.WriteFile(filepath.Join(workdir, f), []byte(c), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runIn(workdir, "init", "-b", "main")
	runIn(workdir, "config", "user.email", "t@t")
	runIn(workdir, "config", "user.name", "T")
	runIn(workdir, "remote", "add", "origin", upstream)
	runIn(workdir, "add", ".")
	runIn(workdir, "commit", "-m", "initial")
	runIn(workdir, "push", "origin", "main")

	project := &models.Project{
		ID: "p1", Name: "r",
		RepoURL:       upstream,
		LocalPath:     workdir,
		DefaultBranch: "main",
		Status:        models.ProjectStatusActive,
	}
	_ = store.SaveProject(project)
	return store, project, upstream
}

func TestReconcileBranches_SpawnsForNewRemoteBranch(t *testing.T) {
	store, project, upstream := setupReconcileFixture(t)

	// Create a new branch with .dev/ on the upstream side
	workdir2 := filepath.Join(filepath.Dir(project.LocalPath), "work2")
	runIn := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runIn(filepath.Dir(project.LocalPath), "clone", upstream, workdir2)
	runIn(workdir2, "config", "user.email", "t@t")
	runIn(workdir2, "config", "user.name", "T")
	runIn(workdir2, "checkout", "-b", "feature/x")
	runIn(workdir2, "commit", "--allow-empty", "-m", "feature commit")
	runIn(workdir2, "push", "origin", "feature/x")

	spawner := &fakeSpawner{}
	summaries, err := ReconcileBranches(context.Background(), store, spawner, "home", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	foundFeatureSpawn := false
	for _, b := range spawner.spawned {
		if b == "feature/x" {
			foundFeatureSpawn = true
		}
	}
	if !foundFeatureSpawn {
		t.Errorf("expected feature/x to be spawned; got spawned=%v summaries=%v", spawner.spawned, summaries)
	}
}

func TestReconcileBranches_TearsDownGoneBranch(t *testing.T) {
	store, project, _ := setupReconcileFixture(t)

	// Add a local preview env for a branch that doesn't exist remotely
	ghost := &models.Environment{
		ID: "p1--ghost", ProjectID: "p1",
		Branch: "ghost", BranchSlug: "ghost",
		Kind: models.EnvKindPreview, Status: models.EnvStatusRunning,
	}
	_ = store.SaveEnvironment(ghost)

	spawner := &fakeSpawner{}
	_, err := ReconcileBranches(context.Background(), store, spawner, "home", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	if len(spawner.tornDown) != 1 || spawner.tornDown[0] != "p1--ghost" {
		t.Errorf("expected ghost env tornDown; got %v", spawner.tornDown)
	}
	if _, err := store.GetEnvironment("p1", "ghost"); err == nil {
		t.Errorf("ghost env still in store after teardown")
	}
	_ = project
}

func TestReconcileBranches_ProdExempt(t *testing.T) {
	store, project, _ := setupReconcileFixture(t)

	// Add a prod env for a branch that doesn't exist remotely (synthesize drift)
	prod := &models.Environment{
		ID: "p1--orphaned", ProjectID: "p1",
		Branch: "orphaned", BranchSlug: "orphaned",
		Kind: models.EnvKindProd, Status: models.EnvStatusRunning,
	}
	_ = store.SaveEnvironment(prod)

	spawner := &fakeSpawner{}
	_, err := ReconcileBranches(context.Background(), store, spawner, "home", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range spawner.tornDown {
		if id == "p1--orphaned" {
			t.Errorf("prod env was torn down; expected exempt")
		}
	}
	if _, err := store.GetEnvironment("p1", "orphaned"); err != nil {
		t.Errorf("prod env unexpectedly removed: %v", err)
	}
	_ = project
}
