package proxy

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// SubdomainEntry represents a registered subdomain route
type SubdomainEntry struct {
	Subdomain   string    `yaml:"subdomain" json:"subdomain"`
	ProjectName string    `yaml:"project_name" json:"project_name"`
	ServiceName string    `yaml:"service_name" json:"service_name"`
	Port        int       `yaml:"port" json:"port"`
	CreatedAt   time.Time `yaml:"created_at" json:"created_at"`
}

// Registry manages subdomain assignments
type Registry struct {
	path    string
	entries map[string]SubdomainEntry
	mu      sync.RWMutex
}

// NewRegistry creates a new subdomain registry
func NewRegistry(path string) (*Registry, error) {
	r := &Registry{
		path:    path,
		entries: make(map[string]SubdomainEntry),
	}

	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return r, nil
}

// load reads entries from disk
func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}

	var entries []SubdomainEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = make(map[string]SubdomainEntry)
	for _, e := range entries {
		r.entries[e.Subdomain] = e
	}

	return nil
}

// saveLocked writes entries to disk. Caller must already hold r.mu (write lock).
// sync.RWMutex is non-recursive, so save() must not acquire any lock itself.
func (r *Registry) saveLocked() error {
	entries := make([]SubdomainEntry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}

	data, err := yaml.Marshal(entries)
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, data, 0644)
}

// Register adds a new subdomain entry
func (r *Registry) Register(entry SubdomainEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[entry.Subdomain]; exists {
		return fmt.Errorf("subdomain %q already registered", entry.Subdomain)
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	r.entries[entry.Subdomain] = entry

	return r.saveLocked()
}

// Unregister removes a subdomain entry
func (r *Registry) Unregister(subdomain string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[subdomain]; !exists {
		return fmt.Errorf("subdomain %q not found", subdomain)
	}

	delete(r.entries, subdomain)

	return r.saveLocked()
}

// UnregisterByProject removes all subdomains for a project
func (r *Registry) UnregisterByProject(projectName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for subdomain, entry := range r.entries {
		if entry.ProjectName == projectName {
			delete(r.entries, subdomain)
		}
	}

	return r.saveLocked()
}

// List returns all registered subdomains
func (r *Registry) List() []SubdomainEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]SubdomainEntry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}

	return entries
}

// Get returns a specific subdomain entry
func (r *Registry) Get(subdomain string) (SubdomainEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[subdomain]
	return entry, exists
}

// IsAvailable checks if a subdomain is available
func (r *Registry) IsAvailable(subdomain string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.entries[subdomain]
	return !exists
}

// GetByProject returns all subdomains for a project
func (r *Registry) GetByProject(projectName string) []SubdomainEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var entries []SubdomainEntry
	for _, e := range r.entries {
		if e.ProjectName == projectName {
			entries = append(entries, e)
		}
	}

	return entries
}
