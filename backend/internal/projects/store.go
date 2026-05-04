package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// ErrNotFound is returned when an entity does not exist on disk.
var ErrNotFound = errors.New("not found")

// Store persists Projects, Environments, and Builds under {root}/projects/.
// One directory per project; environments and builds nest underneath.
type Store struct {
	root string
	mu   sync.RWMutex
}

// NewStore creates the projects root if missing and returns a ready Store.
func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("mkdir projects root: %w", err)
	}
	return &Store{root: root}, nil
}

// Root returns the directory used by this store. For tests + diagnostics.
func (s *Store) Root() string { return s.root }

func (s *Store) projectPath(id string) string {
	return filepath.Join(s.root, id, "project.yaml")
}

// SaveProject writes the project metadata, creating the project dir.
func (s *Store) SaveProject(p *models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.ID == "" {
		return errors.New("project ID required")
	}
	dir := filepath.Join(s.root, p.ID)
	if err := os.MkdirAll(filepath.Join(dir, "environments"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "builds"), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(s.projectPath(p.ID), data, 0644)
}

// GetProject loads a project by ID. Returns ErrNotFound if absent.
func (s *Store) GetProject(id string) (*models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.projectPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var p models.Project
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProjects returns all projects on disk. Order is not guaranteed.
func (s *Store) ListProjects() ([]*models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	out := []*models.Project{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(s.root, e.Name(), "project.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip non-project dirs
		}
		var p models.Project
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue
		}
		out = append(out, &p)
	}
	return out, nil
}

// DeleteProject removes the project directory entirely.
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.root, id)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return ErrNotFound
	}
	return os.RemoveAll(dir)
}

func (s *Store) envPath(projectID, branchSlug string) string {
	return filepath.Join(s.root, projectID, "environments", branchSlug+".yaml")
}

// SaveEnvironment writes an environment to disk under its project.
func (s *Store) SaveEnvironment(e *models.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.ProjectID == "" || e.BranchSlug == "" {
		return errors.New("environment ProjectID and BranchSlug required")
	}
	dir := filepath.Join(s.root, e.ProjectID, "environments")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(s.envPath(e.ProjectID, e.BranchSlug), data, 0644)
}

// GetEnvironment loads an environment by project ID and branch slug.
func (s *Store) GetEnvironment(projectID, branchSlug string) (*models.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.envPath(projectID, branchSlug))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var e models.Environment
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ListEnvironments returns all environments belonging to a project.
func (s *Store) ListEnvironments(projectID string) ([]*models.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.root, projectID, "environments")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.Environment{}, nil
		}
		return nil, err
	}
	out := []*models.Environment{}
	for _, en := range entries {
		if en.IsDir() || filepath.Ext(en.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err != nil {
			continue
		}
		var e models.Environment
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		out = append(out, &e)
	}
	return out, nil
}

// DeleteEnvironment removes the env file. Build records under it are kept.
func (s *Store) DeleteEnvironment(projectID, branchSlug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.envPath(projectID, branchSlug)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ErrNotFound
	}
	return os.Remove(path)
}

func (s *Store) buildPath(projectID, buildID string) string {
	return filepath.Join(s.root, projectID, "builds", buildID+".yaml")
}

// SaveBuild writes a build record under its project.
func (s *Store) SaveBuild(projectID string, b *models.Build) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if projectID == "" || b.ID == "" || b.EnvID == "" {
		return errors.New("build requires projectID, ID, and EnvID")
	}
	dir := filepath.Join(s.root, projectID, "builds")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(s.buildPath(projectID, b.ID), data, 0644)
}

// GetBuild loads a build by project ID and build ID.
func (s *Store) GetBuild(projectID, buildID string) (*models.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.buildPath(projectID, buildID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var b models.Build
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBuildsForEnv returns all builds where Build.EnvID == envID.
func (s *Store) ListBuildsForEnv(projectID, envID string) ([]*models.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.root, projectID, "builds")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.Build{}, nil
		}
		return nil, err
	}
	out := []*models.Build{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var b models.Build
		if err := yaml.Unmarshal(data, &b); err != nil {
			continue
		}
		if b.EnvID == envID {
			out = append(out, &b)
		}
	}
	return out, nil
}

// normalizeRepoURL strips a trailing ".git" and trailing "/" so two
// equivalent forms of the same URL match. Lowercasing is intentionally NOT
// applied — repo paths are case-sensitive on most git hosts.
func normalizeRepoURL(u string) string {
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return u
}

// GetProjectByRepoURL finds a project by repo URL, tolerating ".git" and
// trailing-slash variations. Returns ErrNotFound when no match.
func (s *Store) GetProjectByRepoURL(url string) (*models.Project, error) {
	target := normalizeRepoURL(url)
	all, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if normalizeRepoURL(p.RepoURL) == target {
			return p, nil
		}
	}
	return nil, ErrNotFound
}
