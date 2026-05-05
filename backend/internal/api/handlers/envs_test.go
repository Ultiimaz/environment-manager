package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

type envsFakeExec struct{}

func (envsFakeExec) Compose(ctx context.Context, _, _ string, _ []string, _ io.Writer, _ io.Writer) error {
	return nil
}

func TestEnvsHandler_Destroy_Preview(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	creds, _ := credentials.NewStore(filepath.Join(dir, "c.json"), make([]byte, 32))
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	_ = store.SaveEnvironment(&models.Environment{ID: "p1--feature-x", ProjectID: "p1", BranchSlug: "feature-x", Kind: models.EnvKindPreview})
	runner := builder.NewRunner(store, envsFakeExec{}, dir, "", builder.NewQueue(), zap.NewNop(), creds)
	h := NewEnvsHandler(store, runner, creds, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/v1/envs/p1--feature-x/destroy", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1--feature-x"})
	rec := httptest.NewRecorder()
	h.Destroy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetEnvironment("p1", "feature-x"); !errors.Is(err, projects.ErrNotFound) {
		t.Errorf("expected env removed, got %v", err)
	}
}

func TestEnvsHandler_Destroy_RejectsProd(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	_ = store.SaveEnvironment(&models.Environment{ID: "p1--main", ProjectID: "p1", BranchSlug: "main", Kind: models.EnvKindProd})
	h := NewEnvsHandler(store, nil, nil, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/v1/envs/p1--main/destroy", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1--main"})
	rec := httptest.NewRecorder()
	h.Destroy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
