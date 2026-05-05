package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

type fakeExec struct{}

func (fakeExec) Compose(ctx context.Context, _, _ string, _ []string, _, _ io.Writer) error {
	return nil
}

func newBuildsHandlerTest(t *testing.T) (*BuildsHandler, *projects.Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	store, _ := projects.NewStore(dataDir)
	r := builder.NewRunner(store, fakeExec{}, dataDir, "", builder.NewQueue(), zap.NewNop(), nil)
	h := NewBuildsHandler(store, r, dataDir, zap.NewNop())
	return h, store, dataDir
}

func TestBuildsHandler_Trigger_Success(t *testing.T) {
	h, store, dataDir := newBuildsHandlerTest(t)
	repoDir := filepath.Join(dataDir, "repo")
	project := &models.Project{
		ID:            "p1",
		Name:          "myapp",
		LocalPath:     repoDir,
		DefaultBranch: "main",
		Status:        models.ProjectStatusActive,
	}
	_ = store.SaveProject(project)

	// fakeExec doesn't actually need the compose file to exist (no real
	// docker run), but RenderCompose runs first and reads the source.
	devDir := filepath.Join(repoDir, ".dev")
	_ = writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
	})

	env := &models.Environment{
		ID:          "p1--main",
		ProjectID:   "p1",
		Branch:      "main",
		BranchSlug:  "main",
		Kind:        models.EnvKindProd,
		Status:      models.EnvStatusPending,
		ComposeFile: ".dev/docker-compose.prod.yml",
		URL:         "myapp.home",
	}
	_ = store.SaveEnvironment(env)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", env.ID)
	req := httptest.NewRequest("POST", "/api/v1/envs/"+env.ID+"/build", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Trigger(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Success bool                 `json:"success"`
		Data    TriggerBuildResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.BuildID == "" {
		t.Error("BuildID empty")
	}

	// Wait for the goroutine to complete (the build is fast with fakeExec).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for build to complete")
		default:
		}
		got, err := store.GetBuild("p1", body.Data.BuildID)
		if err == nil && got != nil && got.Status == models.BuildStatusSuccess {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBuildsHandler_Trigger_EnvNotFound(t *testing.T) {
	h, _, _ := newBuildsHandlerTest(t)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent--main")
	req := httptest.NewRequest("POST", "/api/v1/envs/nonexistent--main/build", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Trigger(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBuildsHandler_Trigger_InvalidEnvID(t *testing.T) {
	h, _, _ := newBuildsHandlerTest(t)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "no-double-dash")
	req := httptest.NewRequest("POST", "/api/v1/envs/no-double-dash/build", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Trigger(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBuildsHandler_List(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	now := time.Now().UTC()
	_ = store.SaveBuild("p1", &models.Build{ID: "b1", EnvID: "p1--main", Status: models.BuildStatusSuccess, StartedAt: now.Add(-1 * time.Minute)})
	_ = store.SaveBuild("p1", &models.Build{ID: "b2", EnvID: "p1--main", Status: models.BuildStatusRunning, StartedAt: now})

	h := NewBuildsHandler(store, nil, dir, zap.NewNop())
	req := httptest.NewRequest("GET", "/api/v1/envs/p1--main/builds", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1--main"})
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []*models.Build
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d builds, want 2", len(got))
	}
	// Most-recent first.
	if len(got) >= 2 && got[0].ID != "b2" {
		t.Errorf("expected b2 first (most recent), got %q", got[0].ID)
	}
}

func TestBuildsHandler_List_InvalidEnvID(t *testing.T) {
	h, _, _ := newBuildsHandlerTest(t)
	req := httptest.NewRequest("GET", "/api/v1/envs/no-double-dash/builds", nil)
	req = withChiURLParams(req, map[string]string{"id": "no-double-dash"})
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestBuildsHandler_List_EmptyReturnsArray(t *testing.T) {
	h, _, _ := newBuildsHandlerTest(t)
	req := httptest.NewRequest("GET", "/api/v1/envs/missing--main/builds", nil)
	req = withChiURLParams(req, map[string]string{"id": "missing--main"})
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if body != "[]\n" && body != "[]" {
		t.Errorf("body = %q, want [] for empty list", body)
	}
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
