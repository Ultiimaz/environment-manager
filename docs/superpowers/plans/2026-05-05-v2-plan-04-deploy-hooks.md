# env-manager v2, Plan 4 — Pre/post-deploy hooks

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Honour `iac.Config.Hooks.PreDeploy` and `Hooks.PostDeploy` from `.dev/config.yaml` during the build pipeline. Pre-deploy hooks run AFTER the new image is built but BEFORE traffic shifts; if any pre-deploy hook fails, abort the deploy and the previous container keeps serving. Post-deploy hooks run AFTER traffic shifts; failures are logged but never abort. Hooks execute inside a one-off container of the iac-declared `expose.service`, attached to the same networks (paas-net, traefik proxy net) as the deployed app — so `php artisan migrate --force` reaches the per-env Postgres database without manual setup.

**Architecture:** New `internal/hooks` package containing `Executor` with `RunPre(ctx, hooks []string) error` (abort on first failure) and `RunPost(ctx, hooks []string)` (log all failures, never return error). The Executor uses a small `ComposeRunner` interface so tests can substitute fakes; production callers wire in the existing `builder.ComposeExecutor`. Each hook is invoked as `docker compose -f <rendered> -p <env-id> --project-directory <repo> run --rm <service> sh -c "<hook>"` — `compose run` automatically attaches the same networks and env-file as `compose up` would, so hooks see the deployed app's world.

The runner's `Build` is restructured: the previous single `docker compose up -d --build` call splits into `compose build` → pre-deploy hooks → `compose up -d` (no rebuild) → post-deploy hooks. The iac-parse block introduced in Plan 3b is extended to capture the full `*iac.Config` pointer so the post-RenderCompose stages can read `cfg.Hooks` and `cfg.Expose.Service`.

**What works after this plan ships:**
- A project's `.dev/config.yaml` declaring `hooks.pre_deploy: [php artisan migrate --force]` runs the migrate inside a one-off container BEFORE the new container takes traffic; failed migrations abort the deploy
- `hooks.post_deploy: [php artisan queue:restart]` runs after traffic shift; failures are logged but the deploy is still marked success
- Projects with no hooks block (or with empty lists) behave exactly as before
- Hooks see all secrets + service URLs via `.env` and reach paas-postgres / paas-redis via Docker DNS

**Tech Stack:** Go 1.24, no new dependencies. Existing packages: `internal/iac` (Plan 2), `internal/services/{postgres,redis}` (Plan 3a/3b), `internal/builder`.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Three pillars in one paragraph each → CI/CD", "IaC schema → hooks", "Lifecycle flows → Flow C → steps 7-10", and "Implementation decomposition" row 4.

---

## File structure after this plan

**New files:**

```
backend/internal/hooks/
├── hooks.go        — Executor + ComposeRunner interface + RunPre / RunPost
└── hooks_test.go   — TDD tests with a fake ComposeRunner
```

**Modified files:**

```
backend/internal/builder/runner.go         — split build/up; insert pre/post hook calls
backend/internal/builder/runner_test.go    — assert hook ordering + abort-on-pre-failure + tolerate-on-post-failure
```

**Files unchanged:** the `iac` package (consumed only via `iac.Parse` already in runner), all services packages, all handlers.

---

## Locked details

| Thing | Value |
|---|---|
| Pre-hook semantics | First failure aborts the deploy; subsequent hooks NOT run; running container kept |
| Post-hook semantics | All hooks run regardless of individual failure; failures logged only; build marked success |
| Hook command shape | `docker compose -f docker-compose.yaml -p <env-id> --project-directory <repo> run --rm <service> sh -c "<hook>"` |
| Service used for hooks | `iac.Config.Expose.Service` (required by iac.Parse — won't be empty when hooks run) |
| Build pipeline order after Plan 4 | `compose build` → `RunPre` → `compose up -d` → `RunPost` |
| Behaviour when iac config absent / parse-failed | Hooks lists empty → both loops are noops; build still proceeds (matches Plan 3b's best-effort fallback) |
| Behaviour when hooks declared but iac.Config.Expose.Service is empty | Cannot happen — iac.Parse rejects configs missing `expose.service`. Defensive guard: log warning and skip hook block |
| Hook output | Streamed to the build log (same writer as compose build/up output) |

---

## Tasks

### Task 1: Branch + new `internal/hooks` package (types, interface, RunPre, RunPost)

**Files:**
- Create: `backend/internal/hooks/hooks.go`
- Create: `backend/internal/hooks/hooks_test.go`

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status
git rev-parse HEAD
```

Expected: clean working tree (untracked OK); HEAD at `d88a4e8` (Plan 3b merge) or later.

```bash
git checkout -b feat/v2-plan-04-deploy-hooks
```

- [ ] **Step 2: Write the failing tests**

Create `backend/internal/hooks/hooks_test.go`:

```go
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
		Compose: fc,
		Log:     &log,
		EnvID:   "stripe--main",
		Workdir: "/data/repos/stripe",
		Service: "app",
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
```

- [ ] **Step 3: Run tests to verify failures**

```bash
cd backend && go test ./internal/hooks/... -v
```

Expected: compile errors — `Executor`, `RunPre`, `RunPost` not defined.

- [ ] **Step 4: Implement `internal/hooks/hooks.go`**

Create `backend/internal/hooks/hooks.go`:

```go
// Package hooks runs pre/post-deploy hook commands as defined by
// iac.Config.Hooks.PreDeploy and Hooks.PostDeploy.
//
// Pre-deploy hooks run AFTER the new image is built but BEFORE traffic
// shifts. The first failure aborts the deploy — subsequent hooks are
// skipped, and the caller is expected to keep the previous container
// serving. Pre-failures bubble up as a returned error.
//
// Post-deploy hooks run AFTER traffic shifts. Failures are logged but
// never propagate — the caller's build is still considered successful.
//
// Each hook is run inside a one-off container of the configured service
// via `docker compose run --rm <service> sh -c "<hook>"`. The compose
// run subcommand attaches the service's networks (including paas-net for
// service-plane access) and loads the .env file, so hooks see the same
// runtime as the deployed app.
package hooks

import (
	"context"
	"fmt"
	"io"
)

// ComposeRunner is the minimal docker-compose-invoking surface needed by
// the hook executor. The signature matches builder.ComposeExecutor so
// production callers can pass that directly.
type ComposeRunner interface {
	Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr io.Writer) error
}

// Executor runs a list of hook commands against a compose project.
//
// All fields are required:
//   - Compose: how to invoke `docker compose ...` (typically builder.DockerComposeExecutor)
//   - Log: where to stream hook stdout/stderr and post-deploy failure notes
//   - EnvID: the compose project name (-p value)
//   - Workdir: the env directory containing the rendered docker-compose.yaml
//   - ProjectDir: the user's repo root, passed as --project-directory so
//     compose resolves relative paths the same way the main `up` does
//   - Service: which compose service to run hooks against (typically the
//     iac-declared expose.service)
type Executor struct {
	Compose    ComposeRunner
	Log        io.Writer
	EnvID      string
	Workdir    string
	ProjectDir string
	Service    string
}

// RunPre executes each hook in order. Returns the first non-nil error;
// subsequent hooks are NOT run. A nil/empty hooks list returns nil.
//
// Caller treats the returned error as "abort deploy, keep previous container".
func (e *Executor) RunPre(ctx context.Context, hooks []string) error {
	for i, hook := range hooks {
		fmt.Fprintf(e.Log, "==> pre_deploy[%d/%d]: %s\n", i+1, len(hooks), hook)
		if err := e.runOne(ctx, hook); err != nil {
			return fmt.Errorf("pre_deploy hook %d (%q): %w", i+1, hook, err)
		}
	}
	return nil
}

// RunPost executes each hook in order. Failures are logged to e.Log but
// never propagate. A nil/empty hooks list is a noop.
func (e *Executor) RunPost(ctx context.Context, hooks []string) {
	for i, hook := range hooks {
		fmt.Fprintf(e.Log, "==> post_deploy[%d/%d]: %s\n", i+1, len(hooks), hook)
		if err := e.runOne(ctx, hook); err != nil {
			fmt.Fprintf(e.Log, "WARNING: post_deploy hook %d (%q) failed: %v\n", i+1, hook, err)
		}
	}
}

// runOne shells out to `docker compose ... run --rm <service> sh -c "<cmd>"`.
// Output is streamed to e.Log on both stdout and stderr so the build log
// captures the hook's full transcript.
func (e *Executor) runOne(ctx context.Context, command string) error {
	args := []string{
		"-f", "docker-compose.yaml",
		"-p", e.EnvID,
		"--project-directory", e.ProjectDir,
		"run", "--rm", e.Service,
		"sh", "-c", command,
	}
	return e.Compose.Compose(ctx, e.EnvID, e.Workdir, args, e.Log, e.Log)
}
```

- [ ] **Step 5: Run all hook tests**

```bash
cd backend && go test ./internal/hooks/... -v
```

Expected: all 6 tests PASS.

- [ ] **Step 6: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS — no other packages affected.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/hooks/hooks.go backend/internal/hooks/hooks_test.go
git commit -m "feat(hooks): pre/post-deploy hook executor

New internal/hooks package with Executor.RunPre (abort on first
failure) and Executor.RunPost (log all failures, never abort).
Each hook runs as 'docker compose run --rm <service> sh -c \"<cmd>\"'
so it sees the deployed app's networks and env vars. Production
caller wires the existing builder.ComposeExecutor — the hook
package's ComposeRunner interface matches its signature.

Plan 4 wires this into runner.Build in subsequent commits."
```

---

### Task 2: Refactor runner.Build to split `build` from `up`

This task is a pure refactor — the runner still ends in the same final state, but the previous single `compose up -d --build` is now `compose build` followed by `compose up -d` (no `--build`). No behaviour change. This sets up the next two tasks to insert hook calls between the steps.

**Files:**
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go`

- [ ] **Step 1: Update existing test expectations**

The existing `TestRunner_BuildSuccess` asserts `exec.calls != 1` (1 expected call). After splitting, there will be 2 calls (build + up). Update the assertion. Same for `TestRunner_SecretInjection` — it doesn't assert call count so it's fine. `TestRunner_Build_ServicesProvisioning` asserts URLs in `.env` and paas-net in compose; doesn't count calls. `TestRunner_BuildFailure` sets `exec.exitErr` once — after the split, the FIRST call (build) fails, the up call never happens, so the build still fails. Verify this matches expected behaviour.

In `runner_test.go`, change:

```go
	if exec.calls != 1 {
		t.Errorf("exec calls = %d, want 1", exec.calls)
	}
```

(in `TestRunner_BuildSuccess` around line 106-108) to:

```go
	if exec.calls != 2 {
		t.Errorf("exec calls = %d, want 2 (build + up)", exec.calls)
	}
```

- [ ] **Step 2: Run the test (expect failure)**

```bash
cd backend && go test ./internal/builder/... -run TestRunner_BuildSuccess -v
```

Expected: FAIL — `exec calls = 1, want 2`.

- [ ] **Step 3: Refactor runner.go**

In `runner.go`, find the existing block (around line 245-260):

```go
	_, _ = log.Write([]byte("==> docker compose up -d --build\n"))
	composeArgs := []string{
		"-f", "docker-compose.yaml",
		"-p", env.ID,
		"--project-directory", project.LocalPath,
		"up", "-d", "--build",
	}
	if err := r.exec.Compose(ctx, env.ID, envDir, composeArgs, log, log); err != nil {
		_, _ = log.Write([]byte("BUILD FAILED: " + err.Error() + "\n"))
		return r.fail(env, b, err.Error())
	}
```

Replace with:

```go
	composeBaseArgs := []string{
		"-f", "docker-compose.yaml",
		"-p", env.ID,
		"--project-directory", project.LocalPath,
	}

	_, _ = log.Write([]byte("==> docker compose build\n"))
	buildArgs := append(append([]string(nil), composeBaseArgs...), "build")
	if err := r.exec.Compose(ctx, env.ID, envDir, buildArgs, log, log); err != nil {
		_, _ = log.Write([]byte("BUILD FAILED: " + err.Error() + "\n"))
		return r.fail(env, b, err.Error())
	}

	// (Plan 4 inserts pre_deploy hooks here.)

	_, _ = log.Write([]byte("==> docker compose up -d\n"))
	upArgs := append(append([]string(nil), composeBaseArgs...), "up", "-d")
	if err := r.exec.Compose(ctx, env.ID, envDir, upArgs, log, log); err != nil {
		_, _ = log.Write([]byte("UP FAILED: " + err.Error() + "\n"))
		return r.fail(env, b, err.Error())
	}

	// (Plan 4 inserts post_deploy hooks here.)
```

(The two `append(append([]string(nil), composeBaseArgs...), ...)` calls are deliberate — they create independent slices, preventing accidental mutation if the runner is later parallelised. The cost is two ~10-element copies per build, negligible.)

- [ ] **Step 4: Run all builder tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS — `TestRunner_BuildSuccess` now sees 2 calls (build + up), `TestRunner_BuildFailure` still fails on the first call.

- [ ] **Step 5: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/builder/runner.go backend/internal/builder/runner_test.go
git commit -m "refactor(builder): split 'compose up --build' into separate build + up

Prep work for Plan 4 hook insertion: the previous single
'docker compose up -d --build' call is now 'docker compose build'
followed by 'docker compose up -d' (no rebuild). No behaviour
change — Plan 4's next commits insert pre_deploy hooks between
the two and post_deploy hooks after."
```

---

### Task 3: Wire pre_deploy hooks into runner.Build

Now we capture the parsed `*iac.Config` from the existing iac-parse block (from Plan 3b) so we can read `cfg.Hooks` later. Then we construct a `hooks.Executor` and call `RunPre` between the `compose build` and `compose up -d` calls.

**Files:**
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `runner_test.go`:

```go
func TestRunner_Build_PreDeployHooksRunBeforeUp(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	// Iac with pre_deploy hooks declared.
	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
hooks:
  pre_deploy:
    - "echo migrating"
    - "echo cache-clear"
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// 4 compose calls: build, hook 1, hook 2, up.
	if exec.calls != 4 {
		t.Errorf("exec.calls = %d, want 4 (build + 2 hooks + up)", exec.calls)
	}
}

// fakeOrderedExecutor records the args of every Compose call so we can
// assert the call sequence (not just the count).
type fakeOrderedExecutor struct {
	argsList [][]string
	exitErrs []error // returned in order; nil tail = always succeed afterwards
}

func (f *fakeOrderedExecutor) Compose(_ context.Context, _, _ string, args []string, _, _ io.Writer) error {
	f.argsList = append(f.argsList, append([]string(nil), args...))
	if len(f.exitErrs) == 0 {
		return nil
	}
	err := f.exitErrs[0]
	f.exitErrs = f.exitErrs[1:]
	return err
}

func TestRunner_Build_PreDeployFailureAbortsUp(t *testing.T) {
	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	devDir := filepath.Join(repoDir, ".dev")
	if err := writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		"config.yaml": `project_name: myapp
expose:
  service: app
  port: 80
hooks:
  pre_deploy:
    - "ok-1"
    - "broken-2"
    - "would-be-3"
`,
	}); err != nil {
		t.Fatal(err)
	}
	project := &models.Project{
		ID: "p1", Name: "myapp", LocalPath: repoDir, DefaultBranch: "main",
		Status: models.ProjectStatusActive,
	}
	_ = store.SaveProject(project)
	env := &models.Environment{
		ID: "p1--main", ProjectID: "p1",
		Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		URL:         "myapp.home",
	}
	_ = store.SaveEnvironment(env)

	// Compose calls return: build OK, hook1 OK, hook2 FAIL, then ... but we expect no further calls.
	exec := &fakeOrderedExecutor{
		exitErrs: []error{nil, nil, errors.New("migrate exited 1")},
	}
	r := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	err = r.Build(context.Background(), env, build)
	if err == nil {
		t.Fatal("expected build to fail when pre_deploy hook fails")
	}

	// Expect: build (1) + hook1 (1) + hook2 (1) = 3 calls. No third hook, no up, no post.
	if len(exec.argsList) != 3 {
		t.Fatalf("expected 3 compose calls (build + 2 hooks), got %d: %v", len(exec.argsList), exec.argsList)
	}
	// Last call should be the FAILED hook (containing "broken-2"), not 'up'.
	last := exec.argsList[2]
	if !strings.Contains(strings.Join(last, " "), "broken-2") {
		t.Errorf("expected last call to be the failed hook, got %v", last)
	}
	for _, args := range exec.argsList {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, " up ") || strings.HasSuffix(joined, " up") {
			t.Errorf("'up' should not have been called when pre-hook failed: %v", args)
		}
	}

	// Build status must be failed.
	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusFailed {
		t.Errorf("build status = %v, want failed", gotBuild.Status)
	}
}
```

Add `"io"` to the imports of `runner_test.go` if not already present. (`io` is needed for the fake's Compose signature; the existing `runner_test.go` already imports it.)

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run "TestRunner_Build_PreDeploy" -v
```

Expected: both fail — pre-deploy hooks aren't yet wired, so call counts are wrong.

- [ ] **Step 3: Capture `*iac.Config` and wire `RunPre` in `runner.go`**

In the iac-parse block (around line 145-180), restructure to capture the parsed config so it's accessible later. Replace:

```go
	// --- Plan 3b: parse iac config + provision services ---------------------
	servicesURLs := map[string]string{}
	attachPaasNet := false

	iacPath := filepath.Join(project.LocalPath, ".dev", "config.yaml")
	if iacBytes, err := os.ReadFile(iacPath); err != nil {
		_, _ = log.Write([]byte("==> no .dev/config.yaml — skipping service provisioning\n"))
	} else if cfg, perr := iac.Parse(iacBytes); perr != nil {
		_, _ = log.Write([]byte("WARNING: .dev/config.yaml parse failed; skipping service provisioning: " + perr.Error() + "\n"))
	} else {
		// ... existing services if-block
	}
```

with:

```go
	// --- Plan 3b/4: parse iac config; provision services + collect hooks ----
	servicesURLs := map[string]string{}
	attachPaasNet := false
	var iacCfg *iac.Config // captured for hook-block use below; nil if absent or parse-failed

	iacPath := filepath.Join(project.LocalPath, ".dev", "config.yaml")
	if iacBytes, ferr := os.ReadFile(iacPath); ferr != nil {
		_, _ = log.Write([]byte("==> no .dev/config.yaml — skipping service provisioning + hooks\n"))
	} else if cfg, perr := iac.Parse(iacBytes); perr != nil {
		_, _ = log.Write([]byte("WARNING: .dev/config.yaml parse failed; skipping service provisioning + hooks: " + perr.Error() + "\n"))
	} else {
		iacCfg = cfg
		if cfg.Services.Postgres {
			if r.postgres == nil {
				_, _ = log.Write([]byte("WARNING: services.postgres declared but provisioner not wired; skipping\n"))
			} else {
				_, _ = log.Write([]byte("==> provisioning postgres database\n"))
				db, perr := r.postgres.EnsureEnvDatabase(ctx, env.ID, project.Name, env.BranchSlug)
				if perr != nil {
					_, _ = log.Write([]byte("ERROR: postgres provisioning failed: " + perr.Error() + "\n"))
					return r.fail(env, b, "postgres provisioning: "+perr.Error())
				}
				servicesURLs["DATABASE_URL"] = db.URL
				attachPaasNet = true
			}
		}
		if cfg.Services.Redis {
			if r.redis == nil {
				_, _ = log.Write([]byte("WARNING: services.redis declared but provisioner not wired; skipping\n"))
			} else {
				_, _ = log.Write([]byte("==> provisioning redis ACL\n"))
				acl, perr := r.redis.EnsureEnvACL(ctx, env.ID, project.Name, env.BranchSlug)
				if perr != nil {
					_, _ = log.Write([]byte("ERROR: redis provisioning failed: " + perr.Error() + "\n"))
					return r.fail(env, b, "redis provisioning: "+perr.Error())
				}
				servicesURLs["REDIS_URL"] = acl.URL
				attachPaasNet = true
			}
		}
	}
```

(The diff from the existing block: rename inner `err` to `ferr` to avoid shadowing; declare `var iacCfg *iac.Config` at the outer scope; assign `iacCfg = cfg` in the else branch.)

Add the `hooks` import at the top of `runner.go`:

```go
import (
	// ... existing imports ...
	"github.com/environment-manager/backend/internal/hooks"
	"github.com/environment-manager/backend/internal/iac"
)
```

Between the `compose build` and `compose up -d` blocks (where Task 2 left a `// (Plan 4 inserts pre_deploy hooks here.)` comment), insert:

```go
	// --- Plan 4: pre_deploy hooks -------------------------------------------
	if iacCfg != nil && len(iacCfg.Hooks.PreDeploy) > 0 {
		if iacCfg.Expose.Service == "" {
			_, _ = log.Write([]byte("WARNING: hooks.pre_deploy declared but expose.service is empty; skipping\n"))
		} else {
			hookExec := &hooks.Executor{
				Compose:    r.exec,
				Log:        log,
				EnvID:      env.ID,
				Workdir:    envDir,
				ProjectDir: project.LocalPath,
				Service:    iacCfg.Expose.Service,
			}
			if err := hookExec.RunPre(ctx, iacCfg.Hooks.PreDeploy); err != nil {
				_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
				return r.fail(env, b, err.Error())
			}
		}
	}
	// ------------------------------------------------------------------------
```

- [ ] **Step 4: Run all builder + hook tests**

```bash
cd backend && go test ./internal/builder/... ./internal/hooks/... -v
```

Expected: all PASS, including the two new pre-deploy tests.

- [ ] **Step 5: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/builder/runner.go backend/internal/builder/runner_test.go
git commit -m "feat(builder): runner runs pre_deploy hooks before traffic shift

Captures the parsed *iac.Config from the existing service-provisioning
block and uses cfg.Hooks.PreDeploy / cfg.Expose.Service to wire a
hooks.Executor between the compose-build and compose-up calls. The
first hook failure aborts the deploy via r.fail, leaving the
previous container running."
```

---

### Task 4: Wire post_deploy hooks into runner.Build

Mirror of Task 3 but inserted AFTER `compose up -d` and using `RunPost` (failures logged, not aborting).

**Files:**
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `runner_test.go`:

```go
func TestRunner_Build_PostDeployHooksRunAfterUp(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
hooks:
  post_deploy:
    - "echo queue-restart"
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// 3 compose calls: build, up, post-hook.
	if exec.calls != 3 {
		t.Errorf("exec.calls = %d, want 3 (build + up + 1 post-hook)", exec.calls)
	}
}

func TestRunner_Build_PostDeployFailureDoesNotAbort(t *testing.T) {
	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	devDir := filepath.Join(repoDir, ".dev")
	if err := writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		"config.yaml": `project_name: myapp
expose:
  service: app
  port: 80
hooks:
  post_deploy:
    - "always-fails"
    - "second-also"
`,
	}); err != nil {
		t.Fatal(err)
	}
	project := &models.Project{
		ID: "p1", Name: "myapp", LocalPath: repoDir, DefaultBranch: "main",
		Status: models.ProjectStatusActive,
	}
	_ = store.SaveProject(project)
	env := &models.Environment{
		ID: "p1--main", ProjectID: "p1",
		Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		URL:         "myapp.home",
	}
	_ = store.SaveEnvironment(env)

	// build OK, up OK, hook 1 fails, hook 2 fails — but build should still succeed.
	exec := &fakeOrderedExecutor{
		exitErrs: []error{nil, nil, errors.New("queue restart failed"), errors.New("cache-clear failed")},
	}
	r := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build should succeed despite post-hook failures, got %v", err)
	}

	// Expect 4 calls: build + up + 2 hooks (both ran despite first's failure).
	if len(exec.argsList) != 4 {
		t.Fatalf("expected 4 calls, got %d: %v", len(exec.argsList), exec.argsList)
	}

	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusSuccess {
		t.Errorf("build status = %v, want success", gotBuild.Status)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusRunning {
		t.Errorf("env status = %v, want running", gotEnv.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run "TestRunner_Build_PostDeploy" -v
```

Expected: both fail — post-deploy hooks aren't yet wired.

- [ ] **Step 3: Insert `RunPost` in `runner.go`**

After the `compose up -d` call (where Task 2 left a `// (Plan 4 inserts post_deploy hooks here.)` comment), insert:

```go
	// --- Plan 4: post_deploy hooks ------------------------------------------
	if iacCfg != nil && len(iacCfg.Hooks.PostDeploy) > 0 {
		if iacCfg.Expose.Service == "" {
			_, _ = log.Write([]byte("WARNING: hooks.post_deploy declared but expose.service is empty; skipping\n"))
		} else {
			hookExec := &hooks.Executor{
				Compose:    r.exec,
				Log:        log,
				EnvID:      env.ID,
				Workdir:    envDir,
				ProjectDir: project.LocalPath,
				Service:    iacCfg.Expose.Service,
			}
			hookExec.RunPost(ctx, iacCfg.Hooks.PostDeploy)
		}
	}
	// ------------------------------------------------------------------------
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/builder/... ./internal/hooks/... -v
```

Expected: all PASS, including the two new post-deploy tests.

- [ ] **Step 5: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/builder/runner.go backend/internal/builder/runner_test.go
git commit -m "feat(builder): runner runs post_deploy hooks after traffic shift

Mirror of pre_deploy wiring, but inserted after compose up -d
and using hooks.RunPost which logs failures rather than aborting.
A failed post-hook does NOT mark the build as failed — the deploy
already succeeded by the time post-hooks run."
```

---

### Task 5: Final sanity + plan/checklist commit

**Files:**
- Modify: `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`

- [ ] **Step 1: Run the full backend suite + vet + build**

```bash
cd backend && go test ./... && go vet ./... && go build ./...
```

Expected: all green, all clean.

- [ ] **Step 2: Sanity-check the diff**

```bash
git diff --stat d88a4e8..HEAD
git log --oneline d88a4e8..HEAD
```

Expected: 4 commits (Tasks 1-4) on `feat/v2-plan-04-deploy-hooks`. Files: `backend/internal/hooks/{hooks,hooks_test}.go`, `backend/internal/builder/{runner,runner_test}.go`. No changes outside `backend/`.

- [ ] **Step 3: Update rollout checklist**

Replace the Plan 4 placeholder in `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` with:

```markdown
## Plan 4 — Pre/post-deploy hooks

After merge + redeploy:
- [ ] `cd backend && go test ./internal/hooks/... ./internal/builder/... -v` — all PASS, including 4 new TestRunner_Build_*Hook tests + 6 TestRunPre/TestRunPost tests
- [ ] Existing stripe-payments builds (still v1 schema) trigger normally — runner logs `==> no .dev/config.yaml — skipping service provisioning + hooks`, build still succeeds
- [ ] Manual: extend the test fixture project with `hooks.pre_deploy: ["echo from pre"]` in `.dev/config.yaml` — push, observe build log shows `==> pre_deploy[1/1]: echo from pre` BEFORE `==> docker compose up -d`
- [ ] Same project, change one pre-deploy hook to a deliberately-failing command (`exit 1`) — push, observe BUILD FAILED + the previous container still runs (`docker ps` confirms unchanged container ID)
- [ ] Add `hooks.post_deploy: ["echo from post"]`, push — build succeeds, log shows `==> post_deploy[1/1]: echo from post` AFTER `==> docker compose up -d`
- [ ] Make a post-deploy hook fail (`exit 1`) — build still marked success, log contains `WARNING: post_deploy hook 1 ("...") failed: ...`
```

- [ ] **Step 4: Commit plan + checklist**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-04-deploy-hooks.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 04 (pre/post-deploy hooks)

Plan document + Plan 4 entry in the rollout checklist.
Implementation lands in the preceding 4 commits on this branch.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Push branch + open PR

- [ ] **Step 1: Push**

```bash
git push -u origin feat/v2-plan-04-deploy-hooks
```

- [ ] **Step 2: Open PR via gh**

```bash
gh pr create --title "v2 plan 04: pre/post-deploy hooks" --body "$(cat <<'EOF'
## Summary

- New `internal/hooks` package: `Executor` with `RunPre` (abort on first failure) and `RunPost` (log all failures, never abort)
- Each hook runs as `docker compose run --rm <service> sh -c "<cmd>"` so it sees the deployed app's networks (paas-net included) and `.env` file
- Runner.Build splits the previous single `compose up -d --build` into `compose build` → pre-deploy hooks → `compose up -d` → post-deploy hooks
- Pre-deploy failure aborts the deploy: previous container keeps serving
- Post-deploy failure is logged but the build is marked successful
- Service for hooks is `iac.Config.Expose.Service` (required by iac.Parse)
- Best-effort iac fallback preserved: missing/malformed config → both hook lists are empty → noop, build proceeds

## Out of scope

- Migrating stripe-payments' `.dev/config.yaml` to declare hooks — Plan 8
- Custom domain + Let's Encrypt — Plan 5
- envm CLI — Plan 6
- UI rebuild — Plan 7

## Test plan

- [x] \`cd backend && go test ./internal/hooks/... -v\` — 6 new tests PASS
- [x] \`cd backend && go test ./internal/builder/... -v\` — 4 new TestRunner_Build_*Hook tests PASS, all pre-existing tests still PASS
- [x] \`cd backend && go test ./...\` — full suite green
- [x] \`cd backend && go vet ./...\` — clean
- [x] \`cd backend && go build ./...\` — clean

After merge, manual home-lab verification per the rollout checklist Plan 4 section.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report PR URL back to the user**

---

## Acceptance criteria

- [ ] `internal/hooks/{hooks,hooks_test}.go` exist with `Executor`, `ComposeRunner`, `RunPre`, `RunPost`, and 6 tests
- [ ] `RunPre` aborts on first failure with a wrapping error containing the hook command
- [ ] `RunPost` runs all hooks and never returns an error; failures are logged with the hook command + index
- [ ] Empty/nil hooks lists are handled by both methods without invoking compose
- [ ] Compose arg shape: `-f docker-compose.yaml -p <env-id> --project-directory <repo> run --rm <service> sh -c <command>` (verified by `TestRunPre_ComposeArgShape`)
- [ ] `runner.Build` makes `docker compose build` and `docker compose up -d` as separate calls (no `--build` on `up`)
- [ ] Pre-deploy hooks run BETWEEN build and up; failure prevents `up` from being called
- [ ] Post-deploy hooks run AFTER up; failure does not flip build status to failed
- [ ] `runner.Build` captures `*iac.Config` and reads `cfg.Hooks.PreDeploy/PostDeploy` and `cfg.Expose.Service`
- [ ] If iac config is absent/malformed: hook blocks are noop (lists are empty), build proceeds — matches Plan 3b's best-effort fallback
- [ ] Defensive guard: `iacCfg != nil && cfg.Expose.Service == ""` logs warning and skips the hook block (cannot happen via iac.Parse, but defensive)
- [ ] `go test ./...` clean, `go vet ./...` clean, `go build ./...` clean
- [ ] Branch is 5 commits ahead of master (4 implementation + 1 docs)
- [ ] PR opened with the test-plan checklist
- [ ] Rollout checklist updated for Plan 4

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/`.
- **Never use `> nul`, `> NUL`, or `> /dev/null`** — destructive on this Windows host.
- **Spec is canonical** — `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` overrides this plan if they conflict. Flag in your PR description.
- **TDD discipline:** every feat task → write failing test → run-fail → implement → run-pass → commit.
- **Commit cadence:** one commit per task (4 task commits + 1 docs commit). Don't squash. Don't amend.
- **`hookExec` is constructed twice** (once for pre, once for post) — that's intentional; the two hook calls don't share state and constructing two cheap structs is clearer than reusing one. If you find this duplicating, extract a helper, but only if naming the helper meaningfully — a `newHookExec` function with 6 lines of struct-literal call doesn't pay rent.
- **The reviewer for Plan 3b suggested extracting `r.provisionServices` and `r.teardownServices` from the runner.** This plan does NOT do those extracts — they should land in a focused refactor commit before or after this plan. Keeping Plan 4 small and behavior-focused makes review easier.
- **Compose run vs Docker run:** the spec section "Lifecycle flows → Flow C → step 8" says `docker run --rm <image> sh -c "<hook>"`. We use `docker compose run --rm <service> sh -c "..."` instead — equivalent semantics, with the bonus that compose handles network attachment (paas-net) and env-file resolution automatically. Image-name introspection via raw `docker run` is harder. Compose run is the simpler win.
- **Pre-deploy hook abort = build failed.** The previous container keeps serving because we never called `compose up`. No explicit rollback needed — just don't run `up`.
- **Post-deploy hook failure: WARNING but build success.** This is the spec's "failures logged but don't abort" behavior. A persistent post-hook failure (e.g. queue restart broken) means the deploy still succeeds with possibly-stale workers. That's a known trade-off; if the user needs strict post-deploy success, they can promote the command to pre-deploy.
