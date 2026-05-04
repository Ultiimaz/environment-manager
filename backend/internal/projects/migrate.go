package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/models"
)

const migrationMarker = ".migrated"
const migrationVersion = "v1\n"

// RunLegacyMigration scans the existing dataDir for legacy ComposeProjects
// and Repositories, and writes equivalent Project + Environment(Kind=legacy)
// rows under the projects/ subtree. Idempotent: a marker file at
// {projects_root}/.migrated short-circuits subsequent runs.
//
// This is a metadata-only migration. No containers are touched.
func RunLegacyMigration(store *Store, loader *config.Loader, dataDir string) error {
	markerPath := filepath.Join(store.Root(), migrationMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return nil // already migrated
	}

	composeProjects, err := loader.ListComposeProjects()
	if err != nil {
		return fmt.Errorf("list compose projects: %w", err)
	}

	repoIndex, err := loadLegacyRepoIndex(dataDir)
	if err != nil {
		return fmt.Errorf("load repo index: %w", err)
	}

	now := time.Now().UTC()
	for _, cp := range composeProjects {
		project := buildLegacyProject(cp, repoIndex, now)
		if err := store.SaveProject(project); err != nil {
			return fmt.Errorf("save migrated project %q: %w", cp.ProjectName, err)
		}

		env := buildLegacyEnvironment(project, cp, now)
		if err := store.SaveEnvironment(env); err != nil {
			return fmt.Errorf("save legacy env for %q: %w", cp.ProjectName, err)
		}
	}

	return os.WriteFile(markerPath, []byte(migrationVersion), 0644)
}

// loadLegacyRepoIndex reads every .repo-meta.yaml under {dataDir}/repos/*
// and returns a map keyed by Repository.ID.
func loadLegacyRepoIndex(dataDir string) (map[string]*models.Repository, error) {
	index := make(map[string]*models.Repository)
	reposDir := filepath.Join(dataDir, "repos")
	entries, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(reposDir, e.Name(), ".repo-meta.yaml")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var r models.Repository
		if err := yaml.Unmarshal(data, &r); err != nil {
			continue
		}
		if r.ID != "" {
			index[r.ID] = &r
		}
	}
	return index, nil
}

func buildLegacyProject(cp *models.ComposeProject, repoIndex map[string]*models.Repository, now time.Time) *models.Project {
	p := &models.Project{
		Name:                cp.ProjectName,
		Status:              models.ProjectStatusActive,
		CreatedAt:           cp.Metadata.CreatedAt,
		MigratedFromCompose: cp.ProjectName,
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if cp.RepoID != "" {
		if r, ok := repoIndex[cp.RepoID]; ok {
			p.RepoURL = r.URL
			p.LocalPath = r.LocalPath
			p.DefaultBranch = r.Branch
			p.ID = legacyProjectID(r.URL, cp.ProjectName)
			return p
		}
	}
	// Unlinked: synthesize a stable ID from the project name only.
	p.ID = legacyProjectID("", cp.ProjectName)
	return p
}

func buildLegacyEnvironment(project *models.Project, cp *models.ComposeProject, now time.Time) *models.Environment {
	envID := project.ID + "--legacy"
	return &models.Environment{
		ID:          envID,
		ProjectID:   project.ID,
		Branch:      project.DefaultBranch,
		BranchSlug:  "legacy",
		Kind:        models.EnvKindLegacy,
		ComposeFile: cp.ComposeFile,
		Status:      mapDesiredStateToEnvStatus(cp.DesiredState),
		CreatedAt:   now,
	}
}

func mapDesiredStateToEnvStatus(desired string) models.EnvironmentStatus {
	if desired == "running" {
		return models.EnvStatusRunning
	}
	return models.EnvStatusPending
}

// legacyProjectID returns a stable 8-byte ID for a migrated project.
// Linked projects (with a repo URL) are keyed by repoURL alone, so the same
// repo migrating multiple times yields the same ID. Unlinked projects use
// "compose:<name>" as the key — the prefix prevents collisions with linked
// projects whose repo URL might happen to equal a compose project's name.
// composeName is unused for linked projects today; if a future change
// supports multiple compose projects per repo, callers should switch to
// hashing repoURL+":"+composeName to avoid silent ID collisions.
func legacyProjectID(repoURL, composeName string) string {
	key := repoURL
	if key == "" {
		key = "compose:" + composeName
	}
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:8])
}
