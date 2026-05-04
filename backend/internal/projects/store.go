package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	var out []*models.Project
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
