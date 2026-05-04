package projects

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit creates a minimal git repo for testing. Skips on systems without git.
func gitInit(t *testing.T, branchName string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", branchName)
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "initial")
	return dir
}

func TestResolveDefaultBranch(t *testing.T) {
	repo := gitInit(t, "main")
	got, err := ResolveDefaultBranch(repo)
	if err != nil {
		t.Fatalf("ResolveDefaultBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("got %q want main", got)
	}
}

func TestResolveDefaultBranch_Master(t *testing.T) {
	repo := gitInit(t, "master")
	got, err := ResolveDefaultBranch(repo)
	if err != nil {
		t.Fatalf("ResolveDefaultBranch: %v", err)
	}
	if got != "master" {
		t.Errorf("got %q want master", got)
	}
}

func TestResolveDefaultBranch_NotARepo(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveDefaultBranch(filepath.Join(dir, "nope"))
	if err == nil {
		t.Fatal("expected error for non-repo path")
	}
}
