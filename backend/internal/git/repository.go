package git

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Repository wraps git operations
type Repository struct {
	repo     *git.Repository
	worktree *git.Worktree
	dataDir  string
	remote   string
	auth     *ssh.PublicKeys
}

// NewRepository creates or opens a git repository
func NewRepository(dataDir, remote string) (*Repository, error) {
	// Try to load SSH keys for authentication
	var auth *ssh.PublicKeys
	homeDir, _ := os.UserHomeDir()
	keyPath := filepath.Join(homeDir, ".ssh", "id_rsa")
	if _, err := os.Stat(keyPath); err == nil {
		auth, _ = ssh.NewPublicKeysFromFile("git", keyPath, "")
	}

	// Try to open existing repo
	repo, err := git.PlainOpen(dataDir)
	if err == git.ErrRepositoryNotExists {
		// Initialize new repository
		repo, err = git.PlainInit(dataDir, false)
		if err != nil {
			return nil, fmt.Errorf("failed to init git repo: %w", err)
		}

		// Add remote if provided
		if remote != "" {
			_, err = repo.CreateRemote(&config.RemoteConfig{
				Name: "origin",
				URLs: []string{remote},
			})
			if err != nil && err != git.ErrRemoteExists {
				return nil, fmt.Errorf("failed to add remote: %w", err)
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to open git repo: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	return &Repository{
		repo:     repo,
		worktree: worktree,
		dataDir:  dataDir,
		remote:   remote,
		auth:     auth,
	}, nil
}

// CommitChanges stages all changes and creates a commit
func (r *Repository) CommitChanges(message string) error {
	// Stage all changes
	_, err := r.worktree.Add(".")
	if err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Check if there are changes to commit
	status, err := r.worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if status.IsClean() {
		return nil // Nothing to commit
	}

	// Create commit
	_, err = r.worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Environment Manager",
			Email: "env-manager@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// Push pushes commits to the remote
func (r *Repository) Push() error {
	if r.remote == "" {
		return nil // No remote configured
	}

	opts := &git.PushOptions{
		RemoteName: "origin",
	}
	if r.auth != nil {
		opts.Auth = r.auth
	}

	err := r.repo.Push(opts)
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// Pull pulls changes from the remote
func (r *Repository) Pull() error {
	if r.remote == "" {
		return nil // No remote configured
	}

	opts := &git.PullOptions{
		RemoteName: "origin",
	}
	if r.auth != nil {
		opts.Auth = r.auth
	}

	err := r.worktree.Pull(opts)
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// Status returns the current git status
func (r *Repository) Status() (git.Status, error) {
	return r.worktree.Status()
}

// GetRecentCommits returns the most recent commits
func (r *Repository) GetRecentCommits(limit int) ([]CommitInfo, error) {
	iter, err := r.repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var commits []CommitInfo
	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if count >= limit {
			return fmt.Errorf("limit reached")
		}
		commits = append(commits, CommitInfo{
			Hash:    c.Hash.String()[:7],
			Message: c.Message,
			Author:  c.Author.Name,
			Date:    c.Author.When,
		})
		count++
		return nil
	})

	// Ignore the "limit reached" error
	if err != nil && err.Error() != "limit reached" {
		return nil, err
	}

	return commits, nil
}

// CommitAndPush commits changes and pushes to remote
func (r *Repository) CommitAndPush(message string) error {
	if err := r.CommitChanges(message); err != nil {
		return err
	}
	return r.Push()
}

// CommitInfo represents information about a commit
type CommitInfo struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}
