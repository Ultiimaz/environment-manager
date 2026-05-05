package hooks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeCompose records the args passed to each Compose call and can return
// canned errors per call (in order; trailing nil = always succeed afterwards).
type fakeCompose struct {
	calls       [][]string
	errs        []error // returned in order; if exhausted, returns nil
	stdoutWrite string  // optional output written to the stdout writer per call
}

func (f *fakeCompose) Compose(_ context.Context, _, _ string, args []string, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.stdoutWrite != "" {
		_, _ = stdout.Write([]byte(f.stdoutWrite))
	}
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

func newExecutor(t *testing.T, fc *fakeCompose, log *bytes.Buffer) *Executor {
	t.Helper()
	return &Executor{
		Compose:    fc,
		Log:        log,
		EnvID:      "p1--main",
		Workdir:    "/tmp/envdir",
		ProjectDir: "/tmp/repo",
		Service:    "app",
	}
}

func TestRunPre_HappyPathRunsAllHooks(t *testing.T) {
	fc := &fakeCompose{}
	var log bytes.Buffer
	e := newExecutor(t, fc, &log)

	hooks := []string{"echo a", "echo b", "echo c"}
	if err := e.RunPre(context.Background(), hooks); err != nil {
		t.Fatalf("RunPre: %v", err)
	}
	if len(fc.calls) != 3 {
		t.Fatalf("expected 3 compose calls, got %d", len(fc.calls))
	}
	// Each call should be `... run --rm app sh -c <cmd>`.
	for i, call := range fc.calls {
		if !sliceContains(call, "run") || !sliceContains(call, "--rm") {
			t.Errorf("call %d missing 'run --rm': %v", i, call)
		}
		if !sliceContains(call, "app") {
			t.Errorf("call %d missing service 'app': %v", i, call)
		}
		// Command should be the last arg, after "sh -c".
		if call[len(call)-1] != hooks[i] {
			t.Errorf("call %d cmd: got %q want %q", i, call[len(call)-1], hooks[i])
		}
	}
}

func TestRunPre_FirstFailureAbortsRest(t *testing.T) {
	fc := &fakeCompose{
		errs: []error{nil, errors.New("hook 2 failed"), nil},
	}
	var log bytes.Buffer
	e := newExecutor(t, fc, &log)

	hooks := []string{"good", "bad", "would-also-be-good"}
	err := e.RunPre(context.Background(), hooks)
	if err == nil {
		t.Fatal("expected RunPre to return error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error should mention failing hook: %q", err.Error())
	}
	// Only 2 compose calls — the third hook never ran.
	if len(fc.calls) != 2 {
		t.Errorf("expected 2 calls (third aborted), got %d", len(fc.calls))
	}
}

func TestRunPre_EmptyHooksNoop(t *testing.T) {
	fc := &fakeCompose{}
	var log bytes.Buffer
	e := newExecutor(t, fc, &log)
	if err := e.RunPre(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := e.RunPre(context.Background(), []string{}); err != nil {
		t.Fatal(err)
	}
	if len(fc.calls) != 0 {
		t.Errorf("expected 0 compose calls for empty hooks, got %d", len(fc.calls))
	}
}

func TestRunPost_RunsAllEvenOnFailure(t *testing.T) {
	fc := &fakeCompose{
		errs: []error{errors.New("first failed"), nil, errors.New("third failed")},
	}
	var log bytes.Buffer
	e := newExecutor(t, fc, &log)

	hooks := []string{"a", "b", "c"}
	e.RunPost(context.Background(), hooks)
	if len(fc.calls) != 3 {
		t.Fatalf("expected 3 calls regardless of failures, got %d", len(fc.calls))
	}
	logStr := log.String()
	if !strings.Contains(logStr, "first failed") {
		t.Errorf("expected log to mention first failure: %q", logStr)
	}
	if !strings.Contains(logStr, "third failed") {
		t.Errorf("expected log to mention third failure: %q", logStr)
	}
}

func TestRunPost_EmptyHooksNoop(t *testing.T) {
	fc := &fakeCompose{}
	var log bytes.Buffer
	e := newExecutor(t, fc, &log)
	e.RunPost(context.Background(), nil)
	e.RunPost(context.Background(), []string{})
	if len(fc.calls) != 0 {
		t.Errorf("expected 0 compose calls, got %d", len(fc.calls))
	}
}

func TestRunPre_ComposeArgShape(t *testing.T) {
	// Pin the exact arg shape so a future refactor doesn't accidentally
	// break the docker compose invocation.
	fc := &fakeCompose{}
	var log bytes.Buffer
	e := &Executor{
		Compose:    fc,
		Log:        &log,
		EnvID:      "stripe--main",
		Workdir:    "/data/envs/stripe--main",
		ProjectDir: "/data/repos/stripe",
		Service:    "app",
	}
	if err := e.RunPre(context.Background(), []string{"php artisan migrate --force"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-f", "docker-compose.yaml",
		"-p", "stripe--main",
		"--project-directory", "/data/repos/stripe",
		"run", "--rm", "app",
		"sh", "-c", "php artisan migrate --force",
	}
	if len(fc.calls[0]) != len(want) {
		t.Fatalf("arg count mismatch: got %v want %v", fc.calls[0], want)
	}
	for i := range want {
		if fc.calls[0][i] != want[i] {
			t.Errorf("arg[%d]: got %q want %q (full: %v)", i, fc.calls[0][i], want[i], fc.calls[0])
		}
	}
}

// sliceContains reports whether any element in slice equals target.
func sliceContains(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
