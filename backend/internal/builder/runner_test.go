package builder

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// fakeExecutor records calls and emits canned output to the writer.
type fakeExecutor struct {
	calls   int
	output  string
	exitErr error
}

func (f *fakeExecutor) Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error {
	f.calls++
	if f.output != "" {
		_, _ = stdout.Write([]byte(f.output))
	}
	return f.exitErr
}

func writeFiles(root string, files map[string]string) error {
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func newRunnerTest(t *testing.T) (*Runner, *projects.Store, *models.Project, *models.Environment, string, *fakeExecutor) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	devDir := filepath.Join(repoDir, ".dev")
	if err := writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		"docker-compose.dev.yml":  "services:\n  app:\n    image: hello-world\n",
	}); err != nil {
		t.Fatal(err)
	}
	project := &models.Project{
		ID: "p1", Name: "myapp", LocalPath: repoDir, DefaultBranch: "main",
		Status: models.ProjectStatusActive,
	}
	if err := store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	env := &models.Environment{
		ID: "p1--main", ProjectID: "p1",
		Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		URL:         "myapp.home",
	}
	if err := store.SaveEnvironment(env); err != nil {
		t.Fatal(err)
	}

	exec := &fakeExecutor{output: "Step 1/3 : FROM alpine\n"}
	r := NewRunner(store, exec, dataDir, NewQueue(), zap.NewNop())
	return r, store, project, env, dataDir, exec
}

func TestRunner_BuildSuccess(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	if err := store.SaveBuild("p1", build); err != nil {
		t.Fatal(err)
	}

	if err := r.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if exec.calls != 1 {
		t.Errorf("exec calls = %d, want 1", exec.calls)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusRunning {
		t.Errorf("env status = %v, want running", gotEnv.Status)
	}
	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusSuccess {
		t.Errorf("build status = %v, want success", gotBuild.Status)
	}
	logPath := filepath.Join(dataDir, "builds", env.ID, "latest.log")
	if !fileExists(logPath) {
		t.Errorf("log file %s does not exist", logPath)
	}
}

func TestRunner_BuildFailure(t *testing.T) {
	r, store, _, env, _, exec := newRunnerTest(t)
	exec.exitErr = errors.New("docker exited with 1")
	exec.output = "Step 1/3 : FROM bogus\nERROR: pull access denied\n"

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	err := r.Build(context.Background(), env, build)
	if err == nil {
		t.Fatal("expected build error")
	}

	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusFailed {
		t.Errorf("build status = %v, want failed", gotBuild.Status)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusFailed {
		t.Errorf("env status = %v, want failed", gotEnv.Status)
	}
}
