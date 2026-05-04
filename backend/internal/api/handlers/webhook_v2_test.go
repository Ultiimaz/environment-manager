package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// makeProjectFixture seeds a bare git repo + project record for webhook tests.
// The bare repo acts as "origin" so that git fetch and git ls-tree work
// without a network connection.
func makeProjectFixture(t *testing.T) (*projects.Store, *builder.Runner, *models.Project) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	queue := builder.NewQueue()
	runner := builder.NewRunner(store, fakeExec{}, dataDir, "", queue, zap.NewNop())

	// Create an "upstream" bare repo that the local clone will treat as origin.
	upstreamDir := filepath.Join(dataDir, "upstream.git")
	if err := os.MkdirAll(upstreamDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Seed the upstream with a .dev/ tree on main.
	seedDir := filepath.Join(dataDir, "seed")
	if err := os.MkdirAll(filepath.Join(seedDir, ".dev"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		".dev/docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		".dev/docker-compose.dev.yml":  "services:\n  app:\n    image: hello-world\n",
		".dev/config.yaml":             "project_name: myapp\n",
	} {
		if err := os.WriteFile(filepath.Join(seedDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
		}
	}

	runGit(seedDir, "init", "-b", "main")
	runGit(seedDir, "config", "user.email", "t@example.com")
	runGit(seedDir, "config", "user.name", "T")
	runGit(seedDir, "add", ".")
	runGit(seedDir, "commit", "-m", "initial")

	// Create a bare clone to act as origin.
	runGit(dataDir, "clone", "--bare", seedDir, upstreamDir)

	// Create the local working clone (what env-manager stores as LocalPath).
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	runGit(dataDir, "clone", upstreamDir, repoDir)
	runGit(repoDir, "config", "user.email", "t@example.com")
	runGit(repoDir, "config", "user.name", "T")

	project := &models.Project{
		ID:            "p1",
		Name:          "myapp",
		RepoURL:       "https://github.com/u/myapp",
		LocalPath:     repoDir,
		DefaultBranch: "main",
		Status:        models.ProjectStatusActive,
	}
	if err := store.SaveProject(project); err != nil {
		t.Fatal(err)
	}

	// Pre-create the prod env for main (simulates project onboarding).
	env := &models.Environment{
		ID:          "p1--main",
		ProjectID:   "p1",
		Branch:      "main",
		BranchSlug:  "main",
		Kind:        models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusRunning,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.SaveEnvironment(env); err != nil {
		t.Fatal(err)
	}

	return store, runner, project
}

// newWebhookV2Handler builds a WebhookHandler wired for project push-to-deploy.
func newWebhookV2Handler(store *projects.Store, runner *builder.Runner) *WebhookHandler {
	h := NewWebhookHandler(nil, nil, zap.NewNop())
	h.SetProjectsStore(store)
	h.SetRunner(runner)
	return h
}

// TestWebhook_ProjectPush_TriggersBuild verifies that a push to an existing
// env (main) enqueues a build and returns 200.
func TestWebhook_ProjectPush_TriggersBuild(t *testing.T) {
	store, runner, project := makeProjectFixture(t)
	h := newWebhookV2Handler(store, runner)

	payload := models.WebhookPayload{
		Ref: "refs/heads/main",
		Repository: struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
		}{
			FullName: "u/myapp",
			CloneURL: project.RepoURL,
		},
		Commits: []struct {
			ID       string   `json:"id"`
			Message  string   `json:"message"`
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
			Removed  []string `json:"removed"`
		}{
			{ID: "abc123", Message: "test commit"},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.GitHub(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Wait for the goroutine build to be persisted.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("build triggered by webhook did not appear in time")
		default:
		}
		builds, _ := store.ListBuildsForEnv("p1", "p1--main")
		for _, b := range builds {
			if b.TriggeredBy == models.BuildTriggerWebhook {
				return // success
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestWebhook_UnknownRepo_IsIgnored verifies that a push for a repo not in
// the projects store returns 200 with no project_status (empty string).
func TestWebhook_UnknownRepo_IsIgnored(t *testing.T) {
	store, runner, _ := makeProjectFixture(t)
	h := newWebhookV2Handler(store, runner)

	payload := models.WebhookPayload{
		Ref: "refs/heads/main",
		Repository: struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
		}{
			FullName: "u/otherrepo",
			CloneURL: "https://github.com/u/otherrepo",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.GitHub(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// project_status should be empty (unknown repo, ignored silently).
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if ps, ok := resp.Data["project_status"]; ok && ps != "" {
		t.Errorf("expected empty project_status for unknown repo, got %q", ps)
	}
}

// TestWebhook_NoPushForNilStoreOrRunner_IsNoop verifies backward compatibility:
// when neither projectsStore nor runner is wired (pre-step-5), the handler
// returns 200 and the legacy path runs unmodified.
func TestWebhook_NoPushForNilStoreOrRunner_IsNoop(t *testing.T) {
	// Handler with no projects wiring.
	h := NewWebhookHandler(nil, nil, zap.NewNop())
	// No SetProjectsStore / SetRunner calls.

	payload := models.WebhookPayload{
		Ref: "refs/heads/feature-xyz",
		Repository: struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
		}{
			FullName: "u/myapp",
			CloneURL: "https://github.com/u/myapp",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.GitHub(rec, req)

	// A non-main push with no store wired must still return 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
