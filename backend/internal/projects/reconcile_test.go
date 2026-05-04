package projects

import (
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func TestMarkStuckBuildsFailed(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	e := &models.Environment{
		ID: "p1--main", ProjectID: "p1", Branch: "main", BranchSlug: "main",
		Kind: models.EnvKindProd, Status: models.EnvStatusBuilding,
	}
	_ = s.SaveEnvironment(e)

	stuck := &models.Build{ID: "b1", EnvID: e.ID, Status: models.BuildStatusRunning}
	done := &models.Build{ID: "b2", EnvID: e.ID, Status: models.BuildStatusSuccess}
	_ = s.SaveBuild(p.ID, stuck)
	_ = s.SaveBuild(p.ID, done)

	count, err := MarkStuckBuildsFailed(s)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	got, _ := s.GetBuild(p.ID, "b1")
	if got.Status != models.BuildStatusFailed {
		t.Errorf("stuck build status = %v, want failed", got.Status)
	}
	gotEnv, _ := s.GetEnvironment("p1", "main")
	if gotEnv.Status != models.EnvStatusFailed {
		t.Errorf("env status = %v, want failed", gotEnv.Status)
	}
}

func TestMarkStuckBuildsFailed_NoStuckBuilds(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	e := &models.Environment{
		ID: "p1--main", ProjectID: "p1", Branch: "main", BranchSlug: "main",
		Kind: models.EnvKindProd, Status: models.EnvStatusRunning,
	}
	_ = s.SaveEnvironment(e)

	count, err := MarkStuckBuildsFailed(s)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
