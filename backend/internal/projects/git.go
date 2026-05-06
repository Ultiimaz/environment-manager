package projects

import (
	"os"
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

// FetchOrigin runs `git fetch origin --prune` in repoPath. When token is
// non-empty, it's injected via a one-off git credential helper so HTTPS
// remotes (typically GitHub PATs from the credential store) authenticate
// successfully. GIT_TERMINAL_PROMPT=0 is set unconditionally so a stale or
// missing token fails fast with a clear error rather than hanging on a TTY
// prompt.
//
// Returns the combined output and any error. Best-effort: callers typically
// log + continue.
func FetchOrigin(repoPath, token string) ([]byte, error) {
	var args []string
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if token != "" {
		// Inline shell credential helper. The helper script is one line of sh
		// that emits username/password to stdout for git to consume. The PAT
		// is passed via an env var (ENVM_GIT_TOKEN) so it doesn't appear in
		// the process arg list visible to `ps`.
		args = append(args,
			"-c", `credential.helper=!sh -c 'echo username=oauth; echo password="$ENVM_GIT_TOKEN"'`,
		)
		env = append(env, "ENVM_GIT_TOKEN="+token)
	}

	args = append(args, "fetch", "origin", "--prune")
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Env = env
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
