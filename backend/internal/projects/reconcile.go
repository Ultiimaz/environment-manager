package projects

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/models"
)

// EnvSpawner abstracts what the reconciler does to spawn a new env. The
// real implementation creates an Environment row + saves a Build + fires
// runner.Build in a goroutine. Tests can substitute a fake.
type EnvSpawner interface {
	SpawnPreview(ctx context.Context, project *models.Project, branch, slug string) error
	Teardown(ctx context.Context, env *models.Environment) error
}

// ReconcileBranches walks every project, fetches origin, and converges
// local Environments to match remote branches:
//   - branches with `.dev/` but no local env → SpawnPreview
//   - local envs with no remote branch (and Kind != prod) → Teardown
//
// Returns a summary message per project for logging.
func ReconcileBranches(ctx context.Context, store *Store, spawner EnvSpawner, fallbackBaseDomain string, logger *zap.Logger) ([]string, error) {
	allProjects, err := store.ListProjects()
	if err != nil {
		return nil, err
	}
	var summaries []string
	for _, p := range allProjects {
		if p.LocalPath == "" || p.RepoURL == "" {
			// Legacy migrated projects (no repo): skip.
			continue
		}
		if out, err := FetchOrigin(p.LocalPath); err != nil {
			logger.Warn("git fetch failed during reconcile",
				zap.String("project", p.ID),
				zap.String("out", string(out)),
				zap.Error(err))
			// continue — we still attempt the local→remote diff with stale data
		}

		remoteBranches := ListRemoteBranches(p.LocalPath)
		remoteSet := make(map[string]bool, len(remoteBranches))
		for _, b := range remoteBranches {
			remoteSet[b] = true
		}

		// 1. Tear down envs whose branch is gone (skip prod and legacy).
		envs, _ := store.ListEnvironments(p.ID)
		for _, e := range envs {
			if e.Kind == models.EnvKindProd || e.Kind == models.EnvKindLegacy {
				continue
			}
			if !remoteSet[e.Branch] {
				logger.Info("reconcile: branch gone, tearing down",
					zap.String("project", p.ID),
					zap.String("branch", e.Branch))
				if err := spawner.Teardown(ctx, e); err != nil {
					logger.Warn("reconcile teardown failed", zap.Error(err))
					continue
				}
				_ = store.DeleteEnvironment(p.ID, e.BranchSlug)
				summaries = append(summaries, p.ID+": tore down "+e.BranchSlug)
			}
		}

		// 2. Spawn envs for new branches with .dev/.
		for _, branch := range remoteBranches {
			slug, err := BranchSlug(branch)
			if err != nil {
				continue
			}
			if _, err := store.GetEnvironment(p.ID, slug); err == nil {
				continue // env already exists
			}
			if !DevDirExistsForBranch(p.LocalPath, branch) {
				continue
			}
			logger.Info("reconcile: spawning missing env",
				zap.String("project", p.ID),
				zap.String("branch", branch))
			if err := spawner.SpawnPreview(ctx, p, branch, slug); err != nil {
				logger.Warn("reconcile spawn failed", zap.Error(err))
				continue
			}
			summaries = append(summaries, p.ID+": spawned "+slug)
		}
	}
	return summaries, nil
}

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
