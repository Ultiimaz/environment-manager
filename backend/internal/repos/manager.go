package repos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
)

// Manager handles Git repository operations
type Manager struct {
	basePath    string
	credentials *credentials.Store
	mu          sync.RWMutex
}

// NewManager creates a new repository manager
func NewManager(basePath string, creds *credentials.Store) (*Manager, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repos directory: %w", err)
	}

	return &Manager{
		basePath:    basePath,
		credentials: creds,
	}, nil
}

// Clone clones a repository
func (m *Manager) Clone(ctx context.Context, req models.CloneRequest) (*models.Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID from URL
	id := m.generateID(req.URL)
	name := m.extractRepoName(req.URL)
	localPath := filepath.Join(m.basePath, name)

	// Check if already exists
	if _, err := os.Stat(localPath); err == nil {
		return nil, fmt.Errorf("repository already exists at %s", localPath)
	}

	// Prepare clone options
	cloneOpts := &git.CloneOptions{
		URL:      req.URL,
		Progress: nil,
	}

	// Set branch if specified
	if req.Branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(req.Branch)
		cloneOpts.SingleBranch = true
	}

	// Set auth if token provided
	if req.Token != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: "git", // Can be anything for token auth
			Password: req.Token,
		}
	}

	// Clone the repository
	_, err := git.PlainCloneContext(ctx, localPath, false, cloneOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	// Save token if provided
	if req.Token != "" && m.credentials != nil {
		if err := m.credentials.SaveToken(req.URL, req.Token); err != nil {
			// Log but don't fail - repo is already cloned
			fmt.Printf("Warning: failed to save token: %v\n", err)
		}
	}

	// Determine actual branch
	branch := req.Branch
	if branch == "" {
		branch = m.getDefaultBranch(localPath)
	}

	// Detect compose files
	composeFiles := m.DetectComposeFiles(localPath)

	now := time.Now()
	repo := &models.Repository{
		ID:           id,
		Name:         name,
		URL:          req.URL,
		Branch:       branch,
		LocalPath:    localPath,
		HasToken:     req.Token != "",
		ClonedAt:     now,
		LastPulled:   now,
		ComposeFiles: composeFiles,
	}

	// Save metadata
	if err := m.saveMetadata(repo); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	return repo, nil
}

// List returns all cloned repositories
func (m *Manager) List() ([]*models.Repository, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.Repository{}, nil
		}
		return nil, err
	}

	var repos []*models.Repository
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		repoPath := filepath.Join(m.basePath, entry.Name())

		// Check if it's a git repo
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
			continue
		}

		repo, err := m.loadMetadata(repoPath)
		if err != nil {
			// Try to reconstruct from directory
			repo = m.reconstructMetadata(repoPath, entry.Name())
		}

		// Update HasToken from credential store
		if m.credentials != nil {
			repo.HasToken = m.credentials.HasToken(repo.URL)
		}

		repos = append(repos, repo)
	}

	return repos, nil
}

// Get returns a repository by ID
func (m *Manager) Get(id string) (*models.Repository, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	repos, err := m.List()
	if err != nil {
		return nil, err
	}

	for _, repo := range repos {
		if repo.ID == id {
			return repo, nil
		}
	}

	return nil, fmt.Errorf("repository not found: %s", id)
}

// Pull fetches and merges changes for a repository
func (m *Manager) Pull(id string) (*models.Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find repository
	repos, err := m.listInternal()
	if err != nil {
		return nil, err
	}

	var repo *models.Repository
	for _, r := range repos {
		if r.ID == id {
			repo = r
			break
		}
	}

	if repo == nil {
		return nil, fmt.Errorf("repository not found: %s", id)
	}

	// Open repository
	gitRepo, err := git.PlainOpen(repo.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := gitRepo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	// Prepare pull options
	pullOpts := &git.PullOptions{
		RemoteName: "origin",
	}

	// Get auth if available
	if m.credentials != nil {
		token, err := m.credentials.GetToken(repo.URL)
		if err == nil && token != "" {
			pullOpts.Auth = &http.BasicAuth{
				Username: "git",
				Password: token,
			}
		}
	}

	// Pull changes
	err = worktree.Pull(pullOpts)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, fmt.Errorf("failed to pull: %w", err)
	}

	// Update metadata
	repo.LastPulled = time.Now()
	repo.ComposeFiles = m.DetectComposeFiles(repo.LocalPath)

	if err := m.saveMetadata(repo); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	return repo, nil
}

// Delete removes a repository
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	repos, err := m.listInternal()
	if err != nil {
		return err
	}

	var repo *models.Repository
	for _, r := range repos {
		if r.ID == id {
			repo = r
			break
		}
	}

	if repo == nil {
		return fmt.Errorf("repository not found: %s", id)
	}

	// Delete credential
	if m.credentials != nil {
		_ = m.credentials.DeleteToken(repo.URL)
	}

	// Delete directory
	return os.RemoveAll(repo.LocalPath)
}

// DetectComposeFiles finds docker-compose files in a repository
func (m *Manager) DetectComposeFiles(repoPath string) []string {
	var composeFiles []string

	patterns := []string{
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
	}

	// Walk directory to find compose files
	filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip .git directory
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}

		for _, pattern := range patterns {
			if strings.EqualFold(info.Name(), pattern) {
				relPath, _ := filepath.Rel(repoPath, path)
				composeFiles = append(composeFiles, relPath)
				break
			}
		}

		return nil
	})

	return composeFiles
}

// GetFiles lists files in a repository directory
func (m *Manager) GetFiles(id string, subPath string) ([]FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	repo, err := m.Get(id)
	if err != nil {
		return nil, err
	}

	targetPath := repo.LocalPath
	if subPath != "" {
		targetPath = filepath.Join(repo.LocalPath, subPath)
	}

	// Prevent path traversal
	if !strings.HasPrefix(targetPath, repo.LocalPath) {
		return nil, fmt.Errorf("invalid path")
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, FileInfo{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}

	return files, nil
}

// FileInfo represents a file in a repository
type FileInfo struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// GetFileContent returns the content of a file in a repository
func (m *Manager) GetFileContent(id string, filePath string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	repo, err := m.Get(id)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(repo.LocalPath, filePath)

	// Prevent path traversal
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}
	absRepo, err := filepath.Abs(repo.LocalPath)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absTarget, absRepo) {
		return "", fmt.Errorf("invalid path")
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// Internal helpers

func (m *Manager) generateID(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:8])
}

func (m *Manager) extractRepoName(url string) string {
	// Handle various URL formats
	url = strings.TrimSuffix(url, ".git")

	// Get last part of URL
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "repo"
}

func (m *Manager) getDefaultBranch(repoPath string) string {
	gitRepo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "main"
	}

	ref, err := gitRepo.Head()
	if err != nil {
		return "main"
	}

	return ref.Name().Short()
}

func (m *Manager) saveMetadata(repo *models.Repository) error {
	metaPath := filepath.Join(repo.LocalPath, ".repo-meta.yaml")
	data, err := yaml.Marshal(repo)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0644)
}

func (m *Manager) loadMetadata(repoPath string) (*models.Repository, error) {
	metaPath := filepath.Join(repoPath, ".repo-meta.yaml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	var repo models.Repository
	if err := yaml.Unmarshal(data, &repo); err != nil {
		return nil, err
	}

	return &repo, nil
}

func (m *Manager) reconstructMetadata(repoPath, name string) *models.Repository {
	gitRepo, err := git.PlainOpen(repoPath)

	var url, branch string
	if err == nil {
		if remote, err := gitRepo.Remote("origin"); err == nil {
			urls := remote.Config().URLs
			if len(urls) > 0 {
				url = urls[0]
			}
		}
		if ref, err := gitRepo.Head(); err == nil {
			branch = ref.Name().Short()
		}
	}

	if branch == "" {
		branch = "main"
	}

	info, _ := os.Stat(repoPath)
	clonedAt := time.Now()
	if info != nil {
		clonedAt = info.ModTime()
	}

	repo := &models.Repository{
		ID:           m.generateID(url),
		Name:         name,
		URL:          url,
		Branch:       branch,
		LocalPath:    repoPath,
		ClonedAt:     clonedAt,
		LastPulled:   clonedAt,
		ComposeFiles: m.DetectComposeFiles(repoPath),
	}

	// Save reconstructed metadata
	_ = m.saveMetadata(repo)

	return repo
}

func (m *Manager) listInternal() ([]*models.Repository, error) {
	entries, err := os.ReadDir(m.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*models.Repository{}, nil
		}
		return nil, err
	}

	var repos []*models.Repository
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		repoPath := filepath.Join(m.basePath, entry.Name())

		if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
			continue
		}

		repo, err := m.loadMetadata(repoPath)
		if err != nil {
			repo = m.reconstructMetadata(repoPath, entry.Name())
		}

		if m.credentials != nil {
			repo.HasToken = m.credentials.HasToken(repo.URL)
		}

		repos = append(repos, repo)
	}

	return repos, nil
}
