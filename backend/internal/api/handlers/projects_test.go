package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// fileURL converts a local directory path to a file:// URL that go-git
// can clone from, handling Windows drive-letter paths correctly.
func fileURL(path string) string {
	if runtime.GOOS == "windows" {
		// Convert C:\foo\bar → file:///C:/foo/bar
		return "file:///" + strings.ReplaceAll(path, `\`, `/`)
	}
	return "file://" + path
}

func makeFixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	files := map[string]string{
		".dev/Dockerfile.dev":          "FROM alpine\n",
		".dev/docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		".dev/docker-compose.dev.yml":  "services:\n  app:\n    image: hello-world\n",
		".dev/config.yaml":             "project_name: fixture\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "T")
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func newTestProjectsHandler(t *testing.T) (*ProjectsHandler, string) {
	t.Helper()
	dataDir := t.TempDir()
	credStore, err := credentials.NewStore(filepath.Join(dataDir, ".credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	reposManager, err := repos.NewManager(filepath.Join(dataDir, "repos"), credStore)
	if err != nil {
		t.Fatal(err)
	}
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	logger := zap.NewNop()
	h := NewProjectsHandler(store, reposManager, logger)
	return h, dataDir
}

func TestProjectsHandler_Create_Success(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	repoPath := makeFixtureRepo(t)

	body := map[string]string{"repo_url": fileURL(repoPath)}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got CreateProjectResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Project.Name != "fixture" {
		t.Errorf("project.Name = %q, want fixture", got.Project.Name)
	}
	if got.Environment.Branch != "main" {
		t.Errorf("env.Branch = %q, want main", got.Environment.Branch)
	}
	if len(got.RequiredSecrets) != 0 {
		t.Errorf("required_secrets = %v, want empty (fixture has no secrets file)", got.RequiredSecrets)
	}
}

func TestProjectsHandler_Create_RepoMissingDevDir(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "T")
	run("commit", "--allow-empty", "-m", "initial")

	body := map[string]string{"repo_url": fileURL(dir)}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestProjectsHandler_Create_DuplicateRepoURL(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	repoPath := makeFixtureRepo(t)
	body := map[string]string{"repo_url": fileURL(repoPath)}
	bodyBytes, _ := json.Marshal(body)

	req1 := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first call status = %d", rec1.Code)
	}

	req2 := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second call status = %d, want 409; body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestProjectsHandler_Create_MissingRepoURL(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	body := map[string]string{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestProjectsHandler_List_Empty(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := bytes.TrimSpace(rec.Body.Bytes())
	if string(body) != "[]" {
		t.Errorf("body = %s, want []", body)
	}
}

func TestProjectsHandler_List_AfterCreate(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	repoPath := makeFixtureRepo(t)
	bodyBytes, _ := json.Marshal(map[string]string{"repo_url": fileURL(repoPath)})

	req := httptest.NewRequest("POST", "/api/v1/projects", bytes.NewReader(bodyBytes))
	h.Create(httptest.NewRecorder(), req)

	listReq := httptest.NewRequest("GET", "/api/v1/projects", nil)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	var list []*models.Project
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}
}

func TestProjectsHandler_Get_NotFound(t *testing.T) {
	h, _ := newTestProjectsHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/projects/nope", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nope")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
