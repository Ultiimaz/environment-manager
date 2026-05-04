package projects

import (
	"testing"
	"time"

	"github.com/environment-manager/backend/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestStore_ProjectRoundTrip(t *testing.T) {
	s := newTestStore(t)

	p := &models.Project{
		ID:            "abc12345",
		Name:          "myapp",
		RepoURL:       "https://github.com/u/myapp",
		LocalPath:     "/tmp/myapp",
		DefaultBranch: "main",
		Status:        models.ProjectStatusActive,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveProject(p); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	got, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != p.Name || got.RepoURL != p.RepoURL || got.DefaultBranch != p.DefaultBranch {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, p)
	}
}

func TestStore_ListProjects_Empty(t *testing.T) {
	s := newTestStore(t)
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(list))
	}
}

func TestStore_ListProjects_Multiple(t *testing.T) {
	s := newTestStore(t)
	for i, name := range []string{"a", "b", "c"} {
		p := &models.Project{
			ID:        "id" + name,
			Name:      name,
			Status:    models.ProjectStatusActive,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second).UTC(),
		}
		if err := s.SaveProject(p); err != nil {
			t.Fatalf("SaveProject %s: %v", name, err)
		}
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(list))
	}
}

func TestStore_GetProject_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetProject("does-not-exist")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestStore_DeleteProject(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "x", Name: "x", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	if err := s.DeleteProject("x"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	_, err := s.GetProject("x")
	if err == nil {
		t.Fatalf("expected GetProject after delete to fail")
	}
}

func TestStore_EnvironmentRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	if err := s.SaveProject(p); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	e := &models.Environment{
		ID:         "p1--main",
		ProjectID:  "p1",
		Branch:     "main",
		BranchSlug: "main",
		Kind:       models.EnvKindProd,
		URL:        "p.home",
		Status:     models.EnvStatusPending,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveEnvironment(e); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	got, err := s.GetEnvironment("p1", "main")
	if err != nil {
		t.Fatalf("GetEnvironment: %v", err)
	}
	if got.Branch != e.Branch || got.Kind != e.Kind {
		t.Fatalf("env mismatch: got %+v want %+v", got, e)
	}
}

func TestStore_ListEnvironments(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	for _, slug := range []string{"main", "develop", "feature-x"} {
		e := &models.Environment{
			ID:         "p1--" + slug,
			ProjectID:  "p1",
			Branch:     slug,
			BranchSlug: slug,
			Kind:       models.EnvKindPreview,
			Status:     models.EnvStatusPending,
		}
		if err := s.SaveEnvironment(e); err != nil {
			t.Fatalf("SaveEnvironment %s: %v", slug, err)
		}
	}
	list, err := s.ListEnvironments("p1")
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 envs, got %d", len(list))
	}
}

func TestStore_DeleteEnvironment(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	e := &models.Environment{
		ID: "p1--x", ProjectID: "p1", Branch: "x", BranchSlug: "x",
		Kind: models.EnvKindPreview, Status: models.EnvStatusPending,
	}
	_ = s.SaveEnvironment(e)
	if err := s.DeleteEnvironment("p1", "x"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := s.GetEnvironment("p1", "x"); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}
}

func TestStore_BuildRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	b := &models.Build{
		ID:          "build-1",
		EnvID:       "p1--main",
		TriggeredBy: models.BuildTriggerWebhook,
		SHA:         "abc123",
		StartedAt:   time.Now().UTC().Truncate(time.Second),
		Status:      models.BuildStatusRunning,
	}
	if err := s.SaveBuild(p.ID, b); err != nil {
		t.Fatalf("SaveBuild: %v", err)
	}
	got, err := s.GetBuild(p.ID, b.ID)
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if got.SHA != b.SHA || got.Status != b.Status {
		t.Fatalf("build round-trip mismatch")
	}
}

func TestStore_ListBuildsForEnv(t *testing.T) {
	s := newTestStore(t)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	for i := 0; i < 3; i++ {
		_ = s.SaveBuild(p.ID, &models.Build{
			ID: "b" + string(rune('0'+i)), EnvID: "p1--main",
			Status: models.BuildStatusSuccess,
		})
	}
	_ = s.SaveBuild(p.ID, &models.Build{
		ID: "other", EnvID: "p1--feature-x",
		Status: models.BuildStatusSuccess,
	})
	list, err := s.ListBuildsForEnv(p.ID, "p1--main")
	if err != nil {
		t.Fatalf("ListBuildsForEnv: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 builds for p1--main, got %d", len(list))
	}
}
