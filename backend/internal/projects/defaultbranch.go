package projects

import (
	"fmt"

	"github.com/go-git/go-git/v5"
)

// ResolveDefaultBranch reads the current HEAD of the repo at repoPath and
// returns the branch's short name (e.g. "main"). Used at project-creation
// time to set Project.DefaultBranch from the freshly cloned repo.
//
// For freshly cloned repos this returns whatever branch origin/HEAD points
// at — i.e. GitHub's "default branch". After clone, go-git's HEAD already
// reflects the default branch.
func ResolveDefaultBranch(repoPath string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return "", fmt.Errorf("HEAD is not a branch: %s", head.Name())
	}
	return head.Name().Short(), nil
}
