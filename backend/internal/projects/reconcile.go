package projects

import (
	"time"

	"github.com/environment-manager/backend/internal/models"
)

// MarkStuckBuildsFailed scans every project's builds and rewrites any with
// Status=running to Status=failed with the current timestamp. Used at boot
// to clean up after a hard process exit. Returns the number of builds
// reconciled.
func MarkStuckBuildsFailed(s *Store) (int, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var count int
	for _, p := range projects {
		envs, _ := s.ListEnvironments(p.ID)
		for _, e := range envs {
			builds, _ := s.ListBuildsForEnv(p.ID, e.ID)
			for _, b := range builds {
				if b.Status != models.BuildStatusRunning {
					continue
				}
				b.Status = models.BuildStatusFailed
				b.FinishedAt = &now
				if err := s.SaveBuild(p.ID, b); err != nil {
					return count, err
				}
				count++
			}
			if e.Status == models.EnvStatusBuilding {
				e.Status = models.EnvStatusFailed
				_ = s.SaveEnvironment(e)
			}
		}
	}
	return count, nil
}
