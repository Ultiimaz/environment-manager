package projects

import (
	"os/exec"
	"strings"
)

// DevDirExistsForBranch checks if the given branch's tree (in the local
// clone) contains a `.dev/` directory. Uses git ls-tree so we don't have
// to checkout the branch.
func DevDirExistsForBranch(repoPath, branch string) bool {
	cmd := exec.Command("git", "ls-tree", "--name-only", "origin/"+branch, ".dev")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// FetchOrigin runs `git fetch origin --prune` in repoPath. Returns the
// combined output and any error. Best-effort: callers typically log + continue.
func FetchOrigin(repoPath string) ([]byte, error) {
	cmd := exec.Command("git", "fetch", "origin", "--prune")
	cmd.Dir = repoPath
	return cmd.CombinedOutput()
}

// ListRemoteBranches returns the short names of remote branches under
// origin/* with the "origin/" prefix stripped, and "origin/HEAD" filtered
// out. Returns the empty slice on error.
func ListRemoteBranches(repoPath string) []string {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "origin/HEAD" || strings.HasPrefix(line, "origin/HEAD ->") {
			continue
		}
		// Strip "origin/" prefix
		branch := strings.TrimPrefix(line, "origin/")
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches
}
