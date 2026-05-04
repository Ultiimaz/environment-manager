# Step 3 — Builder + log streaming: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add the build runner that picks up a pending Environment, renders its compose file (Traefik labels + platform env vars injected), executes `docker compose up -d --build` against the host Docker daemon, captures stdout/stderr to a file + in-memory ring buffer, and exposes the captured output as a WebSocket stream. Trigger via `POST /api/v1/envs/{id}/build`. **API-triggerable only** — webhook auto-trigger is step 5; DB env-var injection is step 4.

**Architecture:** Two new packages — `internal/buildlog` for the file-tee + ring-buffer fan-out, and `internal/builder` for the per-env build orchestration. A new `BuildsHandler` exposes the trigger and WS endpoints. The build runner uses `exec.CommandContext` to invoke the host's `docker compose` binary. Per-env serialization uses a `map[envID]*sync.Mutex` so two builds for the same env queue, while different envs run in parallel.

**Tech Stack:** Go 1.24 + existing deps (`gorilla/websocket`, `go.uber.org/zap`). No new packages.

**Spec reference:** Spec sections "Lifecycle flows → Flow D" and "Build log streaming hook".

---

## File structure

**New files:**

| Path | Responsibility |
|---|---|
| `backend/internal/buildlog/log.go` | `Log` type — file writer + ring buffer + subscribers |
| `backend/internal/buildlog/log_test.go` | Unit tests for write/tail/subscribe |
| `backend/internal/builder/render.go` | Compose render: read source compose, inject Traefik labels + platform env, write to env's working dir |
| `backend/internal/builder/render_test.go` | Render tests with golden files |
| `backend/internal/builder/runner.go` | `Runner.Build(ctx, env)` — orchestrates: pull repo, render, exec compose, update Store |
| `backend/internal/builder/runner_test.go` | Runner tests with a fake compose executor |
| `backend/internal/builder/queue.go` | `Queue` — per-env mutex map for serialization |
| `backend/internal/builder/queue_test.go` | Concurrency test |
| `backend/internal/api/handlers/builds.go` | `BuildsHandler.Trigger` + `BuildsHandler.StreamLogs` |
| `backend/internal/api/handlers/builds_test.go` | Handler tests |

**Modified files:**

| Path | Change |
|---|---|
| `backend/internal/api/router.go` | Register `/api/v1/envs/{id}/build` (POST) and `/ws/envs/{id}/build-logs` (WS); add `Builder` to RouterConfig |
| `backend/cmd/server/main.go` | Instantiate `builder.Runner` and wire it through; reconcile stuck builds on startup |

---

## Disk layout (after step 3)

```
{dataDir}/envs/{env-id}/
├── repo/                    # per-env worktree (clone or shared bare ref later)
└── docker-compose.yaml      # rendered compose with injected labels + env vars

{dataDir}/builds/{env-id}/
└── latest.log               # most recent build's full stdout+stderr
```

Per-env worktree avoids the existing reposManager directory (which is shared across the legacy /repos API).

---

## Tasks

### Task 1: Log fan-out — TeeWriter + ring buffer

**Files:**
- Create: `backend/internal/buildlog/log.go`
- Create: `backend/internal/buildlog/log_test.go`

**Goal:** Provide a writer that tees output to a file *and* a fixed-size in-memory ring buffer. WS clients can attach: they get all bytes the ring still holds, then receive new bytes as they arrive. Supports multiple concurrent subscribers per Log.

- [ ] **Step 1: Write the failing tests**

```go
package buildlog

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLog_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	log, err := New(filepath.Join(dir, "build.log"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if _, err := log.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("world\n")); err != nil {
		t.Fatal(err)
	}

	got := log.Snapshot()
	if string(got) != "hello\nworld\n" {
		t.Errorf("snapshot = %q, want hello\\nworld\\n", string(got))
	}
}

func TestLog_SubscriberReceivesNewBytes(t *testing.T) {
	dir := t.TempDir()
	log, err := New(filepath.Join(dir, "build.log"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	_, _ = log.Write([]byte("before\n"))

	sub := log.Subscribe()
	defer log.Unsubscribe(sub)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = log.Write([]byte("after\n"))
	}()

	// Drain enough to assemble both writes.
	var buf strings.Builder
	deadline := time.After(time.Second)
	for buf.Len() < len("before\nafter\n") {
		select {
		case chunk, ok := <-sub:
			if !ok {
				t.Fatal("sub channel closed early")
			}
			buf.Write(chunk)
		case <-deadline:
			t.Fatalf("timeout: buf=%q", buf.String())
		}
	}
	if buf.String() != "before\nafter\n" {
		t.Errorf("got %q, want before\\nafter\\n", buf.String())
	}
}

func TestLog_MultipleSubscribers(t *testing.T) {
	dir := t.TempDir()
	log, _ := New(filepath.Join(dir, "build.log"), 4096)
	defer log.Close()

	sub1 := log.Subscribe()
	sub2 := log.Subscribe()
	defer log.Unsubscribe(sub1)
	defer log.Unsubscribe(sub2)

	go func() { _, _ = log.Write([]byte("data\n")) }()

	got1 := readWithDeadline(t, sub1, len("data\n"))
	got2 := readWithDeadline(t, sub2, len("data\n"))
	if got1 != "data\n" || got2 != "data\n" {
		t.Errorf("got1=%q got2=%q", got1, got2)
	}
}

func TestLog_RingDropsOldBytesUnderPressure(t *testing.T) {
	dir := t.TempDir()
	// Tiny ring so we can prove eviction.
	log, _ := New(filepath.Join(dir, "build.log"), 8)
	defer log.Close()

	_, _ = log.Write([]byte("AAAAAAAA"))   // fills ring
	_, _ = log.Write([]byte("BBBBBBBB"))   // pushes A's out

	got := log.Snapshot()
	if string(got) != "BBBBBBBB" {
		t.Errorf("snapshot = %q, want BBBBBBBB", string(got))
	}
}

// readWithDeadline reads from sub until n bytes accumulated or 1s elapses.
func readWithDeadline(t *testing.T, sub <-chan []byte, n int) string {
	t.Helper()
	var buf strings.Builder
	deadline := time.After(time.Second)
	for buf.Len() < n {
		select {
		case chunk, ok := <-sub:
			if !ok {
				return buf.String()
			}
			buf.Write(chunk)
		case <-deadline:
			t.Fatalf("timeout reading; got %q", buf.String())
		}
	}
	return buf.String()
}

func TestLog_CloseDrainsSubscribers(t *testing.T) {
	dir := t.TempDir()
	log, _ := New(filepath.Join(dir, "build.log"), 4096)
	sub := log.Subscribe()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range sub {
		}
	}()

	_, _ = log.Write([]byte("x\n"))
	log.Close()
	wg.Wait() // sub channel must be closed by Close()
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/buildlog/ -v` from `backend/`
Expected: `undefined: New`, `undefined: Log`.

- [ ] **Step 3: Implement**

```go
// Package buildlog provides a fan-out writer that tees build output to a
// log file on disk plus an in-memory ring buffer. WS subscribers attach
// after the fact, receive whatever bytes the ring still holds, then live
// stream new writes as they arrive.
package buildlog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Log is one build's output sink. Concurrent writes are serialized.
type Log struct {
	mu          sync.Mutex
	file        *os.File
	ring        []byte // fixed-size circular buffer (logical view)
	ringPos     int    // next-write index modulo cap(ring)
	ringFull    bool   // true once ring has wrapped at least once
	subscribers map[chan []byte]struct{}
	closed      bool
}

// New opens (truncates) the log file at path and returns a Log with the
// requested ring-buffer capacity. ringSize must be > 0.
func New(path string, ringSize int) (*Log, error) {
	if ringSize <= 0 {
		return nil, errors.New("ringSize must be > 0")
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	return &Log{
		file:        f,
		ring:        make([]byte, 0, ringSize),
		subscribers: make(map[chan []byte]struct{}),
	}, nil
}

// Write writes p to the file and ring, and broadcasts to subscribers.
// Implements io.Writer, so it can be passed as cmd.Stdout / cmd.Stderr.
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := l.file.Write(p)
	if err != nil {
		return n, err
	}
	l.appendToRing(p)
	for sub := range l.subscribers {
		// Each subscriber gets a copy so consumer mutation doesn't poison others.
		chunk := make([]byte, len(p))
		copy(chunk, p)
		// Non-blocking send: drop the chunk if subscriber isn't keeping up.
		// WS clients should drain promptly; if they fall behind, slowing the
		// build's output throughput is worse than dropping log bytes.
		select {
		case sub <- chunk:
		default:
		}
	}
	return n, nil
}

// appendToRing copies p into the ring, wrapping if it exceeds capacity.
// Caller must hold the mutex.
func (l *Log) appendToRing(p []byte) {
	cap := cap(l.ring)
	for len(p) > 0 {
		end := l.ringPos + len(p)
		if end > cap {
			end = cap
		}
		chunk := p[:end-l.ringPos]
		if l.ringPos == len(l.ring) {
			l.ring = append(l.ring, chunk...)
		} else {
			copy(l.ring[l.ringPos:end], chunk)
		}
		l.ringPos = end
		p = p[len(chunk):]
		if l.ringPos == cap {
			l.ringPos = 0
			l.ringFull = true
		}
	}
}

// Snapshot returns a copy of all bytes currently in the ring buffer
// (i.e. the most recent up-to-ringSize bytes of output).
func (l *Log) Snapshot() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ringFull {
		out := make([]byte, len(l.ring))
		copy(out, l.ring)
		return out
	}
	out := make([]byte, cap(l.ring))
	// Order: bytes from ringPos to end, then 0 to ringPos.
	n := copy(out, l.ring[l.ringPos:])
	copy(out[n:], l.ring[:l.ringPos])
	return out
}

// Subscribe returns a channel that receives every chunk written from this
// point forward. The channel is buffered to avoid head-of-line blocking
// the writer; if the subscriber falls behind, chunks are dropped.
// Caller MUST call Unsubscribe(ch) to release resources.
func (l *Log) Subscribe() <-chan []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan []byte, 64)
	l.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
// Safe to call multiple times.
func (l *Log) Unsubscribe(ch <-chan []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for sub := range l.subscribers {
		if (<-chan []byte)(sub) == ch {
			delete(l.subscribers, sub)
			close(sub)
			return
		}
	}
}

// Close flushes the file and closes all subscriber channels.
// After Close, Write returns io.ErrClosedPipe.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	for sub := range l.subscribers {
		close(sub)
		delete(l.subscribers, sub)
	}
	return l.file.Close()
}
```

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/buildlog/ -v` from `backend/`
Expected: 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/buildlog/
git commit -m "feat(buildlog): add file+ring fan-out for build output"
```

---

### Task 2: Compose renderer

**Files:**
- Create: `backend/internal/builder/render.go`
- Create: `backend/internal/builder/render_test.go`

**Goal:** Take a parsed Project + Environment, read the source compose YAML, inject Traefik labels for the env's URL and a `PROJECT_NAME`/`BRANCH`/`ENV_KIND`/`ENV_URL` env-var block, write the result to the env's working dir as `docker-compose.yaml`. Reuses `proxy.Manager.InjectTraefikLabels` from the existing codebase.

- [ ] **Step 1: Write the failing tests**

```go
package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func TestRenderCompose_InjectsPlatformEnvVars(t *testing.T) {
	repo := t.TempDir()
	composeSrc := `services:
  app:
    image: hello-world
`
	composePath := filepath.Join(repo, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeSrc), 0644); err != nil {
		t.Fatal(err)
	}

	envDir := t.TempDir()
	project := &models.Project{Name: "myapp", DefaultBranch: "main"}
	env := &models.Environment{
		Branch:     "feature/x",
		BranchSlug: "feature-x",
		Kind:       models.EnvKindPreview,
		URL:        "feature-x.myapp.home",
	}

	if err := RenderCompose(composePath, envDir, project, env); err != nil {
		t.Fatalf("RenderCompose: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(envDir, "docker-compose.yaml"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := string(out)
	mustContain := []string{
		"PROJECT_NAME: myapp",
		`BRANCH: "feature/x"`,
		"ENV_KIND: preview",
		"ENV_URL: feature-x.myapp.home",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output:\n%s", want, got)
		}
	}
}

func TestRenderCompose_FailsOnMissingSource(t *testing.T) {
	envDir := t.TempDir()
	err := RenderCompose(filepath.Join(envDir, "missing.yml"), envDir,
		&models.Project{Name: "p"},
		&models.Environment{Kind: models.EnvKindProd, BranchSlug: "main"})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestRenderCompose_PreservesExistingServices(t *testing.T) {
	repo := t.TempDir()
	composeSrc := `services:
  app:
    image: nginx:alpine
    ports:
      - "8080:80"
  worker:
    image: redis:7
`
	composePath := filepath.Join(repo, "docker-compose.yml")
	_ = os.WriteFile(composePath, []byte(composeSrc), 0644)

	envDir := t.TempDir()
	project := &models.Project{Name: "p", DefaultBranch: "main"}
	env := &models.Environment{Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd, URL: "p.home"}
	if err := RenderCompose(composePath, envDir, project, env); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(filepath.Join(envDir, "docker-compose.yaml"))
	got := string(out)
	for _, svc := range []string{"app", "worker", "nginx:alpine", "redis:7"} {
		if !strings.Contains(got, svc) {
			t.Errorf("output missing %q\n--- output:\n%s", svc, got)
		}
	}
}
```

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/builder/ -run TestRenderCompose -v` from `backend/`
Expected: undefined: RenderCompose.

- [ ] **Step 3: Implement**

```go
package builder

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/environment-manager/backend/internal/models"
)

// RenderCompose reads the source compose file, augments each service with
// platform environment variables (PROJECT_NAME, BRANCH, ENV_KIND, ENV_URL),
// and writes the result to envDir/docker-compose.yaml.
//
// Traefik label injection is intentionally NOT done here — it lives in the
// existing proxy.Manager.InjectTraefikLabels and runs as a separate pass
// from Runner.Build (so the Renderer stays test-friendly without pulling
// the proxy + subdomain registry into the unit-test surface).
func RenderCompose(srcPath, envDir string, project *models.Project, env *models.Environment) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source compose: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse source compose: %w", err)
	}

	platformEnv := map[string]string{
		"PROJECT_NAME": project.Name,
		"BRANCH":       env.Branch,
		"ENV_KIND":     string(env.Kind),
		"ENV_URL":      env.URL,
	}
	if err := injectServiceEnv(&doc, platformEnv); err != nil {
		return fmt.Errorf("inject platform env: %w", err)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(envDir, 0755); err != nil {
		return fmt.Errorf("mkdir env dir: %w", err)
	}
	dst := filepath.Join(envDir, "docker-compose.yaml")
	if err := os.WriteFile(dst, out, 0644); err != nil {
		return fmt.Errorf("write rendered compose: %w", err)
	}
	return nil
}

// injectServiceEnv walks the parsed compose doc and merges platform-level
// env vars into each service's `environment:` section. Existing values are
// not overwritten — user compose can shadow platform vars deliberately.
func injectServiceEnv(doc *yaml.Node, vars map[string]string) error {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected top-level kind: %v", root.Kind)
	}
	servicesNode := mapValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return nil // no services — leave unchanged
	}
	for i := 0; i < len(servicesNode.Content); i += 2 {
		svc := servicesNode.Content[i+1]
		if svc.Kind != yaml.MappingNode {
			continue
		}
		envNode := mapValue(svc, "environment")
		if envNode == nil {
			envNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			svc.Content = append(svc.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: "environment"},
				envNode,
			)
		}
		if envNode.Kind != yaml.MappingNode {
			// Convert sequence form to map form is not supported here; skip.
			continue
		}
		for k, v := range vars {
			if mapValue(envNode, k) != nil {
				continue // user value wins
			}
			envNode.Content = append(envNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: k},
				&yaml.Node{Kind: yaml.ScalarNode, Value: v, Style: yaml.DoubleQuotedStyle},
			)
		}
	}
	return nil
}

// mapValue returns the value node for key in a yaml mapping, or nil if absent.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
```

NOTE: The test asserts `PROJECT_NAME: myapp` (no quotes). The implementation uses `DoubleQuotedStyle` which produces `PROJECT_NAME: "myapp"`. Adjust the expected strings in the test:

```go
mustContain := []string{
    `PROJECT_NAME: "myapp"`,
    `BRANCH: "feature/x"`,
    `ENV_KIND: "preview"`,
    `ENV_URL: "feature-x.myapp.home"`,
}
```

If `BRANCH: "feature/x"` already includes quotes in the original test, leave it.

- [ ] **Step 4: Run tests, expect pass**

Run: `go test ./internal/builder/ -run TestRenderCompose -v` from `backend/`
Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/render.go backend/internal/builder/render_test.go
git commit -m "feat(builder): add compose renderer with platform env injection"
```

---

### Task 3: Per-env queue (mutex map)

**Files:**
- Create: `backend/internal/builder/queue.go`
- Create: `backend/internal/builder/queue_test.go`

**Goal:** A `Queue` provides one mutex per env ID. Same-env builds serialize; different-env builds parallelize. Used by Runner to enforce concurrency.

- [ ] **Step 1: Write the failing tests**

```go
package builder

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueue_SerializesSameEnv(t *testing.T) {
	q := NewQueue()
	var concurrent int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := q.Acquire("env-a")
			defer release()
			c := atomic.AddInt32(&concurrent, 1)
			defer atomic.AddInt32(&concurrent, -1)
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}()
	}
	wg.Wait()

	if maxConcurrent != 1 {
		t.Errorf("maxConcurrent for same env = %d, want 1", maxConcurrent)
	}
}

func TestQueue_ParallelDifferentEnvs(t *testing.T) {
	q := NewQueue()
	var concurrent int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		envID := "env-" + string(rune('a'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := q.Acquire(envID)
			defer release()
			c := atomic.AddInt32(&concurrent, 1)
			defer atomic.AddInt32(&concurrent, -1)
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}()
	}
	wg.Wait()

	if maxConcurrent < 2 {
		t.Errorf("maxConcurrent across 4 envs = %d, expected >= 2", maxConcurrent)
	}
}
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/builder/ -run TestQueue -v -race` from `backend/`
Expected: undefined: NewQueue, undefined: Queue.

- [ ] **Step 3: Implement**

```go
package builder

import "sync"

// Queue serializes concurrent operations on the same key while allowing
// different keys to run in parallel. Used to ensure one build per env at
// a time.
type Queue struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewQueue returns an empty Queue.
func NewQueue() *Queue {
	return &Queue{locks: make(map[string]*sync.Mutex)}
}

// Acquire blocks until the lock for key is available, then returns a
// release function that the caller MUST call (typically via defer) to
// release the lock.
func (q *Queue) Acquire(key string) func() {
	q.mu.Lock()
	m, ok := q.locks[key]
	if !ok {
		m = &sync.Mutex{}
		q.locks[key] = m
	}
	q.mu.Unlock()
	m.Lock()
	return func() { m.Unlock() }
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/builder/ -run TestQueue -v -race` from `backend/`
Expected: 2 tests PASS, no race detected.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/queue.go backend/internal/builder/queue_test.go
git commit -m "feat(builder): add per-env Queue for build serialization"
```

---

### Task 4: Build runner

**Files:**
- Create: `backend/internal/builder/runner.go`
- Create: `backend/internal/builder/runner_test.go`

**Goal:** The `Runner` orchestrates one build: create env working dir, render compose, exec `docker compose -p <env-id> up -d --build` with stdout/stderr piped to a `buildlog.Log`, update Build + Environment status in the Store. The compose executor is an interface so tests can substitute a fake.

- [ ] **Step 1: Write the failing tests**

```go
package builder

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// fakeExecutor records calls and emits canned output to the writer.
type fakeExecutor struct {
	calls   int
	output  string
	exitErr error
}

func (f *fakeExecutor) Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr Writer) error {
	f.calls++
	if f.output != "" {
		_, _ = stdout.Write([]byte(f.output))
	}
	return f.exitErr
}

func newRunnerTest(t *testing.T) (*Runner, *projects.Store, *models.Project, *models.Environment, string, *fakeExecutor) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := projects.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(dataDir, "repos", "myapp")
	devDir := filepath.Join(repoDir, ".dev")
	if err := writeFiles(devDir, map[string]string{
		"docker-compose.prod.yml": "services:\n  app:\n    image: hello-world\n",
		"docker-compose.dev.yml":  "services:\n  app:\n    image: hello-world\n",
	}); err != nil {
		t.Fatal(err)
	}
	project := &models.Project{
		ID: "p1", Name: "myapp", LocalPath: repoDir, DefaultBranch: "main",
		Status: models.ProjectStatusActive,
	}
	if err := store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	env := &models.Environment{
		ID: "p1--main", ProjectID: "p1",
		Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd,
		ComposeFile: ".dev/docker-compose.prod.yml",
		Status:      models.EnvStatusPending,
		URL:         "myapp.home",
	}
	if err := store.SaveEnvironment(env); err != nil {
		t.Fatal(err)
	}

	exec := &fakeExecutor{output: "Step 1/3 : FROM alpine\n"}
	r := NewRunner(store, exec, dataDir, NewQueue(), zap.NewNop())
	return r, store, project, env, dataDir, exec
}

func TestRunner_BuildSuccess(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	if err := store.SaveBuild("p1", build); err != nil {
		t.Fatal(err)
	}

	if err := r.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if exec.calls != 1 {
		t.Errorf("exec calls = %d, want 1", exec.calls)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusRunning {
		t.Errorf("env status = %v, want running", gotEnv.Status)
	}
	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusSuccess {
		t.Errorf("build status = %v, want success", gotBuild.Status)
	}
	logPath := filepath.Join(dataDir, "builds", env.ID, "latest.log")
	if !fileExists(logPath) {
		t.Errorf("log file %s does not exist", logPath)
	}
}

func TestRunner_BuildFailure(t *testing.T) {
	r, store, _, env, _, exec := newRunnerTest(t)
	exec.exitErr = errors.New("docker exited with 1")
	exec.output = "Step 1/3 : FROM bogus\nERROR: pull access denied\n"

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	err := r.Build(context.Background(), env, build)
	if err == nil {
		t.Fatal("expected build error")
	}

	gotBuild, _ := store.GetBuild("p1", build.ID)
	if gotBuild.Status != models.BuildStatusFailed {
		t.Errorf("build status = %v, want failed", gotBuild.Status)
	}
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusFailed {
		t.Errorf("env status = %v, want failed", gotEnv.Status)
	}
}

// helpers

func writeFiles(root string, files map[string]string) error {
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := mkdirAll(filepath.Dir(path)); err != nil {
			return err
		}
		if err := writeFile(path, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func mkdirAll(dir string) error { return osMkdirAll(dir) }
func writeFile(path string, data []byte) error { return osWriteFile(path, data) }
func fileExists(path string) bool {
	_, err := osStat(path)
	return err == nil
}

// Indirected via package vars so the imports in runner_test.go don't grow.
var (
	osMkdirAll  = func(dir string) error { return mkdirAllReal(dir) }
	osWriteFile = func(p string, b []byte) error { return writeFileReal(p, b) }
	osStat      = func(p string) (any, error) { return statReal(p) }
)

func mkdirAllReal(dir string) error { return makeDir(dir) }
func writeFileReal(p string, b []byte) error { return writeBytes(p, b) }
func statReal(p string) (any, error) { return statPath(p) }

// Real OS calls — keep separate to avoid name collisions with stdlib refs.
func makeDir(dir string) error  { return errOSImport(dir, "mkdir") }
func writeBytes(p string, b []byte) error { return errOSImport(p, "write") }
func statPath(p string) (any, error) { _, e := errOSImport(p, "stat"), error(nil); return nil, e }
func errOSImport(p, op string) error  { return fmt.Errorf("os indirection unimplemented: %s %s", op, p) }
```

**STOP — that test scaffold is too convoluted. Throw it away and just use the stdlib directly.** The implementer can use this simpler version of the helpers section instead:

```go
// helpers
func writeFiles(root string, files map[string]string) error {
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
```

…and add `"os"` to the test imports. Discard everything from `func writeFiles` onwards in the snippet above and use this clean version.

- [ ] **Step 2: Run tests, expect compile failure**

Run: `go test ./internal/builder/ -run TestRunner -v` from `backend/`
Expected: undefined: Runner, Writer, NewRunner.

- [ ] **Step 3: Implement runner.go**

```go
package builder

import (
	"context"
	"io"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/buildlog"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// Writer is the subset of io.Writer the runner needs from a log sink.
type Writer = io.Writer

// ComposeExecutor abstracts the `docker compose` invocation so tests can
// substitute a fake. Real implementation lives in DockerComposeExecutor.
type ComposeExecutor interface {
	Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr Writer) error
}

// Runner builds an Environment: render compose, run `up -d --build`,
// update Store with results.
type Runner struct {
	store    *projects.Store
	exec     ComposeExecutor
	dataDir  string
	queue    *Queue
	logger   *zap.Logger
	logRing  int // ring buffer size for buildlog.Log
}

// NewRunner constructs a Runner.
func NewRunner(store *projects.Store, exec ComposeExecutor, dataDir string, queue *Queue, logger *zap.Logger) *Runner {
	return &Runner{
		store:   store,
		exec:    exec,
		dataDir: dataDir,
		queue:   queue,
		logger:  logger,
		logRing: 64 * 1024, // 64 KiB ring per build — enough for late-joiner replay
	}
}

// Build runs the full build pipeline for env. Returns the final error, if any.
// The Build record (b) is updated and persisted as the build progresses.
// The caller is expected to have already saved the initial Build record
// with Status=running and StartedAt set.
func (r *Runner) Build(ctx context.Context, env *models.Environment, b *models.Build) error {
	release := r.queue.Acquire(env.ID)
	defer release()

	project, err := r.store.GetProject(env.ProjectID)
	if err != nil {
		return r.fail(env, b, "load project: "+err.Error())
	}

	envDir := filepath.Join(r.dataDir, "envs", env.ID)
	logPath := filepath.Join(r.dataDir, "builds", env.ID, "latest.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return r.fail(env, b, "mkdir log dir: "+err.Error())
	}
	log, err := buildlog.New(logPath, r.logRing)
	if err != nil {
		return r.fail(env, b, "open log: "+err.Error())
	}
	defer log.Close()
	b.LogPath = logPath
	_ = r.store.SaveBuild(env.ProjectID, b)

	// Mark env as building.
	env.Status = models.EnvStatusBuilding
	_ = r.store.SaveEnvironment(env)

	// Render compose.
	srcPath := filepath.Join(project.LocalPath, env.ComposeFile)
	_, _ = log.Write([]byte("==> rendering compose: " + srcPath + "\n"))
	if err := RenderCompose(srcPath, envDir, project, env); err != nil {
		_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
		return r.fail(env, b, "render compose: "+err.Error())
	}

	// Exec docker compose up -d --build.
	_, _ = log.Write([]byte("==> docker compose up -d --build\n"))
	composeArgs := []string{"-f", "docker-compose.yaml", "-p", env.ID, "up", "-d", "--build"}
	if err := r.exec.Compose(ctx, env.ID, envDir, composeArgs, log, log); err != nil {
		_, _ = log.Write([]byte("BUILD FAILED: " + err.Error() + "\n"))
		return r.fail(env, b, err.Error())
	}

	// Success.
	now := time.Now().UTC()
	b.FinishedAt = &now
	b.Status = models.BuildStatusSuccess
	_ = r.store.SaveBuild(env.ProjectID, b)
	env.Status = models.EnvStatusRunning
	env.LastBuildID = b.ID
	env.LastDeployedSHA = b.SHA
	_ = r.store.SaveEnvironment(env)
	return nil
}

// fail marks the build + env as failed and returns the error wrapped.
func (r *Runner) fail(env *models.Environment, b *models.Build, msg string) error {
	now := time.Now().UTC()
	b.FinishedAt = &now
	b.Status = models.BuildStatusFailed
	_ = r.store.SaveBuild(env.ProjectID, b)
	env.Status = models.EnvStatusFailed
	_ = r.store.SaveEnvironment(env)
	r.logger.Warn("build failed", zap.String("env_id", env.ID), zap.String("build_id", b.ID), zap.String("reason", msg))
	return errors.New(msg)
}
```

Note: I forgot the `"os"` and `"errors"` imports — please add them. Final import block in runner.go should include: `context`, `errors`, `io`, `os`, `path/filepath`, `time`, `go.uber.org/zap`, plus the project's internal packages.

Also create a real `DockerComposeExecutor` in the same file (or a sibling file `executor.go`):

```go
// DockerComposeExecutor invokes the host's `docker compose` binary.
type DockerComposeExecutor struct{}

// Compose runs `docker compose <args>` in workdir with stdout/stderr piped
// to the supplied writers.
func (DockerComposeExecutor) Compose(ctx context.Context, projectName, workdir string, args []string, stdout, stderr Writer) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = workdir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
```

Add `"os/exec"` to the imports.

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/builder/ -run TestRunner -v` from `backend/`
Expected: 2 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/runner.go backend/internal/builder/runner_test.go
git commit -m "feat(builder): add Runner with ComposeExecutor abstraction"
```

---

### Task 5: BuildsHandler — Trigger endpoint

**Files:**
- Create: `backend/internal/api/handlers/builds.go`
- Create: `backend/internal/api/handlers/builds_test.go`

**Goal:** `POST /api/v1/envs/{id}/build` creates a `Build` row, spawns a goroutine to run `Runner.Build`, returns 202 with the build ID.

- [ ] **Step 1: Write the failing tests**

```go
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

type fakeExec struct{}

func (fakeExec) Compose(ctx context.Context, _, _ string, _ []string, _, _ builder.Writer) error {
	return nil
}

func newBuildsHandlerTest(t *testing.T) (*BuildsHandler, *projects.Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	store, _ := projects.NewStore(dataDir)
	r := builder.NewRunner(store, fakeExec{}, dataDir, builder.NewQueue(), zap.NewNop())
	h := NewBuildsHandler(store, r, zap.NewNop())
	return h, store, dataDir
}

func TestBuildsHandler_Trigger_Success(t *testing.T) {
	h, store, dataDir := newBuildsHandlerTest(t)
	project := &models.Project{ID: "p1", Name: "myapp", LocalPath: filepath.Join(dataDir, "repo"), DefaultBranch: "main", Status: models.ProjectStatusActive}
	_ = store.SaveProject(project)
	env := &models.Environment{ID: "p1--main", ProjectID: "p1", Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd, Status: models.EnvStatusPending, ComposeFile: ".dev/docker-compose.prod.yml"}
	_ = store.SaveEnvironment(env)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", env.ID)
	req := httptest.NewRequest("POST", "/api/v1/envs/"+env.ID+"/build", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Trigger(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var body TriggerBuildResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.BuildID == "" {
		t.Error("BuildID empty")
	}

	// Wait briefly for goroutine to write the build.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for build")
		default:
		}
		got, err := store.GetBuild("p1", body.BuildID)
		if err == nil && got != nil {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBuildsHandler_Trigger_EnvNotFound(t *testing.T) {
	h, _, _ := newBuildsHandlerTest(t)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent--main")
	req := httptest.NewRequest("POST", "/api/v1/envs/nonexistent--main/build", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Trigger(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// silence unused
var _ = bytes.Buffer{}
var _ = errors.New
```

- [ ] **Step 2: Run, expect compile failure**

Run: `go test ./internal/api/handlers/ -run TestBuildsHandler -v` from `backend/`
Expected: undefined: BuildsHandler.

- [ ] **Step 3: Implement**

Create `backend/internal/api/handlers/builds.go`:

```go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// BuildsHandler exposes /envs/{id}/build endpoints. EnvIDs use the
// "<project_id>--<branch_slug>" convention introduced in step 1.
type BuildsHandler struct {
	store  *projects.Store
	runner *builder.Runner
	logger *zap.Logger
}

// NewBuildsHandler wires the handler.
func NewBuildsHandler(store *projects.Store, runner *builder.Runner, logger *zap.Logger) *BuildsHandler {
	return &BuildsHandler{store: store, runner: runner, logger: logger}
}

// TriggerBuildResponse is returned from POST /api/v1/envs/{id}/build.
type TriggerBuildResponse struct {
	BuildID string `json:"build_id"`
	EnvID   string `json:"env_id"`
}

// Trigger handles POST /api/v1/envs/{id}/build. The build runs asynchronously;
// the response returns 202 Accepted with the build ID so callers can poll
// or open the WS log stream.
func (h *BuildsHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, branchSlug, ok := splitEnvID(envID)
	if !ok {
		respondError(w, http.StatusBadRequest, "INVALID_ENV_ID", "env id must be <project>--<slug>")
		return
	}
	env, err := h.store.GetEnvironment(projectID, branchSlug)
	if err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			respondError(w, http.StatusNotFound, "ENV_NOT_FOUND", "environment not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	now := time.Now().UTC()
	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerManual,
		StartedAt:   now,
		Status:      models.BuildStatusRunning,
	}
	if err := h.store.SaveBuild(env.ProjectID, build); err != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	go h.runBuild(env, build)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	respondSuccess(w, TriggerBuildResponse{BuildID: build.ID, EnvID: env.ID})
}

// runBuild is invoked in a goroutine. Uses a fresh background context so the
// HTTP request lifecycle doesn't cancel the build.
func (h *BuildsHandler) runBuild(env *models.Environment, b *models.Build) {
	if err := h.runner.Build(context.Background(), env, b); err != nil {
		h.logger.Warn("build returned error",
			zap.String("env_id", env.ID),
			zap.String("build_id", b.ID),
			zap.Error(err),
		)
	}
}

// splitEnvID parses "<projectID>--<slug>" into its parts.
func splitEnvID(envID string) (projectID, branchSlug string, ok bool) {
	idx := strings.Index(envID, "--")
	if idx < 0 || idx == 0 || idx == len(envID)-2 {
		return "", "", false
	}
	return envID[:idx], envID[idx+2:], true
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/api/handlers/ -run TestBuildsHandler -v` from `backend/`
Expected: 2 tests PASS. Note: the success test waits up to 2s for the goroutine to write the build — should resolve in ~50ms with the fake executor.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers/builds.go backend/internal/api/handlers/builds_test.go
git commit -m "feat(api): add POST /api/v1/envs/{id}/build trigger endpoint"
```

---

### Task 6: BuildsHandler — WS log streaming

**Files:**
- Modify: `backend/internal/api/handlers/builds.go` (append `StreamLogs` method)

**Goal:** GET `/ws/envs/{id}/build-logs` upgrades to WS, reads the env's `latest.log` from disk and streams it. (Live ring-buffer attachment for in-flight builds is deferred — test that the file-tail path works first; ring attachment can be added later if dogfooding shows late joiners need it.)

- [ ] **Step 1: Append the StreamLogs method**

Append to `backend/internal/api/handlers/builds.go`:

```go
// StreamLogs handles GET /ws/envs/{id}/build-logs. Streams the env's most
// recent build log file over WebSocket. Closes the connection when the
// reader hits EOF and the build is no longer running.
//
// MVP behavior: simple file-tail loop. Live ring-buffer attachment for
// in-flight builds with multi-subscriber fan-out is implemented at the
// buildlog package level but not yet wired here — that's a follow-up.
func (h *BuildsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, branchSlug, ok := splitEnvID(envID)
	if !ok {
		http.Error(w, "invalid env id", http.StatusBadRequest)
		return
	}
	env, err := h.store.GetEnvironment(projectID, branchSlug)
	if err != nil {
		http.Error(w, "env not found", http.StatusNotFound)
		return
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	logPath := filepath.Join(h.dataDir, "builds", env.ID, "latest.log")
	f, err := os.Open(logPath)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": "no log available"})
		return
	}
	defer f.Close()

	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if werr := conn.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
				return
			}
		}
		if err == io.EOF {
			// If the build is still running, wait briefly and retry.
			if cur, _ := h.store.GetEnvironment(env.ProjectID, env.BranchSlug); cur != nil && cur.Status == models.EnvStatusBuilding {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return
		}
		if err != nil {
			return
		}
	}
}
```

You'll need to update the BuildsHandler struct to carry `dataDir` and add it to `NewBuildsHandler`:

```go
type BuildsHandler struct {
	store   *projects.Store
	runner  *builder.Runner
	dataDir string
	logger  *zap.Logger
}

func NewBuildsHandler(store *projects.Store, runner *builder.Runner, dataDir string, logger *zap.Logger) *BuildsHandler {
	return &BuildsHandler{store: store, runner: runner, dataDir: dataDir, logger: logger}
}
```

Update the test in `builds_test.go` accordingly: `h := NewBuildsHandler(store, r, dataDir, zap.NewNop())`.

Add to imports: `io`, `os`, `path/filepath`, `time`, `github.com/gorilla/websocket`, `github.com/environment-manager/backend/internal/models`.

- [ ] **Step 2: Run all builds tests**

Run: `go test ./internal/api/handlers/ -run TestBuildsHandler -v` from `backend/`
Expected: 2 tests still PASS (no new test for StreamLogs — WS is hard to unit-test in-process; manual smoke test will exercise it in Task 8).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/handlers/builds.go backend/internal/api/handlers/builds_test.go
git commit -m "feat(api): add WS /ws/envs/{id}/build-logs streaming"
```

---

### Task 7: Wire builder + handlers into router

**Files:**
- Modify: `backend/internal/api/router.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add field to RouterConfig + builder import**

In `backend/internal/api/router.go`:

a) Add to imports (alphabetical):
```go
"github.com/environment-manager/backend/internal/builder"
```

b) Add field to `RouterConfig`:
```go
Builder *builder.Runner
```

c) Instantiate handler after `projectsHandler`:
```go
buildsHandler := handlers.NewBuildsHandler(cfg.ProjectsStore, cfg.Builder, cfg.DataDir, cfg.Logger)
```

d) Inside `r.Route("/api/v1", ...)`, after the `/projects` block, add:
```go
		// Builds (per-env build trigger; WS for live logs is registered below outside /api/v1)
		r.Route("/envs", func(r chi.Router) {
			r.Post("/{id}/build", buildsHandler.Trigger)
		})
```

e) After the existing WS block (`r.Get("/ws/containers/{id}/logs", ...)`), add:
```go
r.Get("/ws/envs/{id}/build-logs", buildsHandler.StreamLogs)
```

- [ ] **Step 2: Wire in main.go**

In `backend/cmd/server/main.go`:

a) Add import: `"github.com/environment-manager/backend/internal/builder"`

b) After the `projectsStore` block (and the legacy migration call), add:
```go
buildQueue := builder.NewQueue()
buildExec := builder.DockerComposeExecutor{}
buildRunner := builder.NewRunner(projectsStore, buildExec, cfg.DataDir, buildQueue, logger)
```

c) In the `api.NewRouter(api.RouterConfig{...})` call, add the field:
```go
Builder: buildRunner,
```

- [ ] **Step 3: Verify build + tests**

```bash
go build ./... && go test ./...
```

Expected: clean build, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/api/router.go backend/cmd/server/main.go
git commit -m "feat(server): wire builder runner + builds endpoints"
```

---

### Task 8: Stuck-build reconcile on startup

**Files:**
- Modify: `backend/cmd/server/main.go`

**Goal:** On boot, scan all builds with `Status=running` and mark them `failed`. Without this, a process restart mid-build leaves orphan `running` rows forever.

- [ ] **Step 1: Add helper invocation**

In `main.go`, after the `RunLegacyMigration` call and before `buildQueue` is created:

```go
// Reconcile builds that were running when the previous binary stopped.
if reconciled, err := projects.MarkStuckBuildsFailed(projectsStore); err != nil {
    logger.Error("Failed to reconcile stuck builds", zap.Error(err))
} else if reconciled > 0 {
    logger.Info("Marked stuck builds as failed", zap.Int("count", reconciled))
}
```

- [ ] **Step 2: Implement the helper**

Create `backend/internal/projects/reconcile.go`:

```go
package projects

import (
	"time"

	"github.com/environment-manager/backend/internal/models"
)

// MarkStuckBuildsFailed scans every project's builds and rewrites any with
// Status=running to Status=failed with the current timestamp. Used at boot
// to clean up after a hard process exit. Returns the number of builds
// reconciled.
func MarkStuckBuildsFailed(s *Store) (int, error) {
	projects, err := s.ListProjects()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var count int
	for _, p := range projects {
		envs, _ := s.ListEnvironments(p.ID)
		for _, e := range envs {
			builds, _ := s.ListBuildsForEnv(p.ID, e.ID)
			for _, b := range builds {
				if b.Status != models.BuildStatusRunning {
					continue
				}
				b.Status = models.BuildStatusFailed
				b.FinishedAt = &now
				if err := s.SaveBuild(p.ID, b); err != nil {
					return count, err
				}
				count++
			}
			// If env was building, mark it failed too.
			if e.Status == models.EnvStatusBuilding {
				e.Status = models.EnvStatusFailed
				_ = s.SaveEnvironment(e)
			}
		}
	}
	return count, nil
}
```

- [ ] **Step 3: Add tests**

Create `backend/internal/projects/reconcile_test.go`:

```go
package projects

import (
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func TestMarkStuckBuildsFailed(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	p := &models.Project{ID: "p1", Name: "p", Status: models.ProjectStatusActive}
	_ = s.SaveProject(p)
	e := &models.Environment{ID: "p1--main", ProjectID: "p1", Branch: "main", BranchSlug: "main", Kind: models.EnvKindProd, Status: models.EnvStatusBuilding}
	_ = s.SaveEnvironment(e)

	stuck := &models.Build{ID: "b1", EnvID: e.ID, Status: models.BuildStatusRunning}
	done := &models.Build{ID: "b2", EnvID: e.ID, Status: models.BuildStatusSuccess}
	_ = s.SaveBuild(p.ID, stuck)
	_ = s.SaveBuild(p.ID, done)

	count, err := MarkStuckBuildsFailed(s)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	got, _ := s.GetBuild(p.ID, "b1")
	if got.Status != models.BuildStatusFailed {
		t.Errorf("stuck build status = %v, want failed", got.Status)
	}
	gotEnv, _ := s.GetEnvironment("p1", "main")
	if gotEnv.Status != models.EnvStatusFailed {
		t.Errorf("env status = %v, want failed", gotEnv.Status)
	}
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/projects/ -run TestMarkStuckBuildsFailed -v
go build ./...
git add backend/internal/projects/reconcile.go backend/internal/projects/reconcile_test.go backend/cmd/server/main.go
git commit -m "feat(projects): reconcile stuck running builds on startup"
```

---

### Task 9: Manual smoke test

- [ ] **Step 1: Build binary, set up fixture, start server**

```powershell
cd G:/Workspaces/claude-code-tests/env-manager/backend
go build -o $env:TEMP/env-manager-step3.exe ./cmd/server

# Reuse the step-2 fixture or create a fresh one with a real Dockerfile.dev
# that exercises image build (don't use hello-world only)
$dataDir = "$env:TEMP/env-manager-step3-data"
Remove-Item $dataDir -Recurse -Force -ErrorAction SilentlyContinue

$env:DATA_DIR = $dataDir
$serverProc = Start-Process -FilePath "$env:TEMP/env-manager-step3.exe" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
```

- [ ] **Step 2: POST a project, then trigger a build**

```powershell
$repoURL = "..."  # use a real GitHub repo with .dev/ layout, or a local file:// fixture
$projBody = @{ repo_url = $repoURL } | ConvertTo-Json
$proj = Invoke-RestMethod -Uri http://localhost:8080/api/v1/projects -Method Post -Body $projBody -ContentType "application/json"
$envID = $proj.environment.id

$build = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/envs/$envID/build" -Method Post
Write-Host "Build started: $($build.data.build_id)"
```

- [ ] **Step 3: Watch the WS log stream**

Use `wscat` or PowerShell's `System.Net.WebSockets.ClientWebSocket`:

```powershell
$ws = New-Object System.Net.WebSockets.ClientWebSocket
$ct = [System.Threading.CancellationToken]::None
$ws.ConnectAsync([Uri]"ws://localhost:8080/ws/envs/$envID/build-logs", $ct).Wait()
$buf = New-Object byte[] 4096
$seg = [System.ArraySegment[byte]]::new($buf)
while ($ws.State -eq 'Open') {
    $r = $ws.ReceiveAsync($seg, $ct).Result
    if ($r.MessageType -eq 'Close') { break }
    [System.Text.Encoding]::UTF8.GetString($buf, 0, $r.Count) | Write-Host -NoNewline
}
```

Expected output: progress text from `docker compose up`, ending in success or a clear error.

- [ ] **Step 4: Verify env status flipped**

```powershell
$proj = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/projects/$($proj.project.id)"
$proj.environments
```

Expected: status `running` if the build succeeded, `failed` if the image couldn't build (and prior containers are preserved if any existed).

- [ ] **Step 5: Stop server, append rollout checklist**

Edit `docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md`. Replace the step 3 placeholder with:

```markdown
## Step 3 — Builder + log streaming

After rollout:
- [ ] `POST /api/v1/envs/{id}/build` returns 202 with a build_id
- [ ] Build runs asynchronously: container appears under `docker ps` shortly after
- [ ] WS `/ws/envs/{id}/build-logs` streams live output during build
- [ ] Build success flips env to `Status: running`
- [ ] Build failure flips env to `Status: failed`, prior containers (if any) untouched
- [ ] Restart env-manager mid-build: stuck `running` builds reconciled to `failed`
```

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md
git commit -m "docs: rollout checklist for step 3"
```

---

## Self-review

After all 9 tasks:

**Spec coverage:**
- TeeWriter + ring buffer for log fan-out — Task 1 (subscribe path tested but not yet wired into WS handler)
- Compose render with platform env vars — Task 2
- Per-env serialization — Task 3
- Runner orchestrating build pipeline — Task 4
- POST trigger endpoint — Task 5
- WS log endpoint (file-tail variant) — Task 6
- Wired into router + main — Task 7
- Stuck-build reconciliation — Task 8
- Manual end-to-end verification — Task 9

**Out of scope (future plans):**
- Traefik label injection in the rendered compose (defer to step 4 alongside DB injection — both touch the rendered compose)
- Auto-build on Project create (defer to step 5 — webhook v2 will trigger via the same builder)
- Build-superseded handling for fast successive triggers (defer until ring-buffer subscribers are wired)

**Known follow-ups in this plan:**
- WS handler (Task 6) uses simple file-tail rather than ring-buffer subscription. Wire `log.Subscribe()` into the StreamLogs handler in a future polish commit if late-joiner UX shows gaps.
- Traefik label injection skipped — services in the rendered compose will run on the project bridge but not be routable by the host Traefik until step 4.
