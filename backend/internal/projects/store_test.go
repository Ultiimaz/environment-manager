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
