# env-manager v2, Plan 3a — Service-plane provisioners + Flow G bootstrap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap the singleton service plane (paas-net + paas-postgres + paas-redis) on env-manager startup, and ship ready-but-unwired provisioner libraries for per-environment database / ACL provisioning. Plan 3b will wire the per-env methods into the build flow.

**Architecture:** Two new packages under `backend/internal/services/`:
- `postgres/` — `Provisioner` with `EnsureService`, `EnsureEnvDatabase`, `DropEnvDatabase`
- `redis/` — `Provisioner` with `EnsureService`, `EnsureEnvACL`, `DropEnvACL`

Each package defines its own minimal `Docker` interface (~4 methods) for testability; the real implementation is `*docker.Client`, which gets four new methods (`ContainerStatus`, `RunContainer`, `ExecCommand`, `EnsureBridgeNetwork`). Per-env provisioning is testable end-to-end via a fake `Docker`. The credential store gains a `SystemSecrets` API for storing the singletons' superuser passwords.

`cmd/server/main.go` adds Flow G: after cred-store init, before reconcile, ensure paas-net + paas-postgres + paas-redis.

**What works after this plan ships:** env-manager boots, ensures `paas-net` exists, ensures `paas-postgres` (Postgres 16) is running on it, ensures `paas-redis` (Redis 7) is running on it. Per-env provisioner methods exist and are tested but **not yet called from the runner** — the runner change is Plan 3b.

**Tech Stack:** Go 1.24, `github.com/docker/docker` SDK (already in go.mod), `gopkg.in/yaml.v3` (unrelated to this plan but already present).

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Architecture → Service plane", "IaC schema → Per-env database semantics", "IaC schema → Per-env Redis semantics", and "Lifecycle flows → Flow G".

---

## File structure after this plan

**New files:**

```
backend/internal/services/postgres/
├── postgres.go        — Provisioner, Docker interface, types, password helper
└── postgres_test.go   — TDD tests using a fake Docker

backend/internal/services/redis/
├── redis.go
└── redis_test.go

backend/internal/services/realdocker/
└── realdocker.go      — adapter wrapping *docker.Client to satisfy both
                         postgres.Docker and redis.Docker interfaces
```

**Modified files:**

```
backend/internal/credentials/store.go        — adds SystemSecrets API
backend/internal/credentials/store_test.go   — adds SystemSecret tests
backend/internal/docker/client.go            — adds ContainerStatus, RunContainer,
                                                ExecCommand, EnsureBridgeNetwork +
                                                RunSpec type
backend/cmd/server/main.go                   — Flow G bootstrap call
```

**Files unchanged:** the `iac` package from Plan 2 (consumed only by Plan 3b onward), the builder/runner (also Plan 3b), all handlers.

---

## Naming & layout reference (locked)

Use these literal strings exactly across the implementation. Mismatches cost hours later.

| Thing | Value |
|---|---|
| Network | `paas-net` |
| Postgres container | `paas-postgres` |
| Postgres image | `postgres:16` |
| Postgres volume | `paas_postgres_data` |
| Postgres mount path | `/var/lib/postgresql/data` |
| Postgres superuser | `postgres` (Postgres default) |
| Redis container | `paas-redis` |
| Redis image | `redis:7` |
| Redis volume | `paas_redis_data` |
| Redis mount path | `/data` |
| Cred-store key (Postgres super) | `system:paas-postgres:superuser` |
| Cred-store key (Redis super) | `system:paas-redis:superuser` |
| Cred-store key (env DB pw) | `env:<env-id>:db_password` |
| Cred-store key (env Redis pw) | `env:<env-id>:redis_password` |
| Per-env DB name format | `<slug(project_name)>_<branch_slug>` (lowercased, `-` → `_`) |
| Per-env DB user | identical to DB name |
| Per-env Redis user | identical to per-env DB name |
| Per-env Redis prefix | `<slug(project_name)>:<branch_slug>` |
| Container labels | `env-manager.managed=true`, `env-manager.singleton=postgres\|redis` |

---

## Tasks

### Task 1: Branch + cred-store SystemSecrets API

**Files:**
- Modify: `backend/internal/credentials/store.go`
- Modify: `backend/internal/credentials/store_test.go`

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status
git rev-parse HEAD
```

Expected: clean working tree (untracked files OK); HEAD at `6486106` (Plan 2 merge) or later.

```bash
git checkout -b feat/v2-plan-03a-service-plane-bootstrap
```

- [ ] **Step 2: Write the failing test**

Append to `backend/internal/credentials/store_test.go`:

```go
func TestSystemSecret_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSystemSecret("system:paas-postgres:superuser", "hunter2"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSystemSecret("system:paas-postgres:superuser")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want hunter2", got)
	}
}

func TestSystemSecret_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSystemSecret("system:nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSystemSecret_OverwriteSameKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSystemSecret("system:paas-redis:superuser", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSystemSecret("system:paas-redis:superuser", "second"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSystemSecret("system:paas-redis:superuser")
	if got != "second" {
		t.Errorf("got %q, want second", got)
	}
}

func TestSystemSecret_RequiresKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSystemSecret("system:paas-postgres:superuser", "v"); err != nil {
		t.Fatal(err)
	}
	// Distinct keys are independent.
	if err := s.SaveSystemSecret("system:paas-redis:superuser", "v2"); err != nil {
		t.Fatal(err)
	}
	v1, _ := s.GetSystemSecret("system:paas-postgres:superuser")
	v2, _ := s.GetSystemSecret("system:paas-redis:superuser")
	if v1 != "v" || v2 != "v2" {
		t.Errorf("isolated keys broken: v1=%q v2=%q", v1, v2)
	}
}
```

- [ ] **Step 3: Run tests to verify failures**

```bash
cd backend && go test ./internal/credentials/... -run TestSystemSecret -v
```

Expected: compile errors — `Store has no method SaveSystemSecret/GetSystemSecret`.

- [ ] **Step 4: Add the API to `store.go`**

Modify the `credentials` struct on `backend/internal/credentials/store.go` (around line 31) to add a `SystemSecrets` field:

```go
// credentials is the internal structure stored on disk
type credentials struct {
	Tokens         map[string]string            `json:"tokens"`
	ProjectSecrets map[string]map[string]string `json:"project_secrets,omitempty"`
	SystemSecrets  map[string]string            `json:"system_secrets,omitempty"`
}
```

Then add these methods at the end of the file (after `DeleteProjectSecret`):

```go
// SaveSystemSecret encrypts and stores a process-level secret keyed by an
// arbitrary identifier (e.g. "system:paas-postgres:superuser"). Used for the
// service-plane's singleton container superuser passwords.
func (s *Store) SaveSystemSecret(key, value string) error {
	if s.key == nil {
		return ErrNoKey
	}
	if key == "" {
		return errors.New("key required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if creds == nil {
		creds = &credentials{}
	}
	if creds.Tokens == nil {
		creds.Tokens = make(map[string]string)
	}
	if creds.SystemSecrets == nil {
		creds.SystemSecrets = make(map[string]string)
	}
	encrypted, err := s.encrypt(value)
	if err != nil {
		return err
	}
	creds.SystemSecrets[key] = encrypted
	return s.save(creds)
}

// GetSystemSecret returns the decrypted value for a system secret.
// Returns ErrNotFound when the key is unset.
func (s *Store) GetSystemSecret(key string) (string, error) {
	if s.key == nil {
		return "", ErrNoKey
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	creds, err := s.load()
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if creds == nil || creds.SystemSecrets == nil {
		return "", ErrNotFound
	}
	enc, ok := creds.SystemSecrets[key]
	if !ok {
		return "", ErrNotFound
	}
	return s.decrypt(enc)
}
```

- [ ] **Step 5: Run tests to verify passes**

```bash
cd backend && go test ./internal/credentials/... -v
```

Expected: all PASS (existing tests + 4 new SystemSecret tests).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/credentials/store.go backend/internal/credentials/store_test.go
git commit -m "feat(credentials): add SystemSecrets API for service-plane singletons

SaveSystemSecret/GetSystemSecret persist arbitrary process-level
secrets in the same encrypted store as project secrets. Used by
the upcoming postgres/redis service-plane provisioners to
remember their singleton superuser passwords across reboots."
```

---

### Task 2: docker.Client — RunContainer, ContainerStatus, ExecCommand, EnsureBridgeNetwork

These are thin SDK wrappers. Unit-testing them would require a real Docker daemon, which isn't worth the harness effort. The provisioner tests in Tasks 4-10 exercise them indirectly via the `Docker` interface fake; the real-vs-fake adapter (Task 3) is verified manually on the home-lab smoke test.

**Files:**
- Modify: `backend/internal/docker/client.go`

- [ ] **Step 1: Add `RunSpec` type and the four new methods**

Append to `backend/internal/docker/client.go`:

```go
// RunSpec describes a service-plane container to launch. Used by RunContainer.
// Volumes maps a named volume (auto-created if missing) to a container path.
// Env, Cmd, and Labels are optional.
type RunSpec struct {
	Name    string
	Image   string
	Network string
	Volumes map[string]string
	Env     map[string]string
	Cmd     []string
	Labels  map[string]string
}

// ContainerStatus reports whether a container with the given name exists and
// whether it's running. Both false (with nil error) means the container is
// absent. Used by service-plane bootstrap for idempotency.
func (c *Client) ContainerStatus(ctx context.Context, name string) (exists, running bool, err error) {
	list, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return false, false, err
	}
	if len(list) == 0 {
		return false, false, nil
	}
	return true, list[0].State == "running", nil
}

// RunContainer pulls the image (idempotent), creates the container, attaches
// it to the named network, mounts the named volumes, and starts it. If a
// container with that name already exists the call returns nil — caller is
// expected to check ContainerStatus first if it cares.
func (c *Client) RunContainer(ctx context.Context, spec RunSpec) error {
	// Pull image — idempotent; ImagePull short-circuits when the image is local.
	pullReader, err := c.cli.ImagePull(ctx, spec.Image, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", spec.Image, err)
	}
	if _, err := io.Copy(io.Discard, pullReader); err != nil {
		_ = pullReader.Close()
		return fmt.Errorf("drain image pull: %w", err)
	}
	_ = pullReader.Close()

	// Build env slice
	envSlice := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		envSlice = append(envSlice, k+"="+v)
	}

	// Build mounts. Each volume entry => bind a named volume to a container path.
	mounts := make([]mount.Mount, 0, len(spec.Volumes))
	for vol, target := range spec.Volumes {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: vol,
			Target: target,
		})
	}

	cfg := &container.Config{
		Image:  spec.Image,
		Env:    envSlice,
		Cmd:    spec.Cmd,
		Labels: spec.Labels,
	}
	hostCfg := &container.HostConfig{
		Mounts:        mounts,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			spec.Network: {},
		},
	}

	resp, err := c.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name)
	if err != nil {
		// If the daemon already has a container with that name, treat as success.
		if errdefs.IsConflict(err) {
			return nil
		}
		return fmt.Errorf("create container %s: %w", spec.Name, err)
	}
	if err := c.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", spec.Name, err)
	}
	return nil
}

// ExecCommand runs cmd inside an existing container and returns the captured
// stdout, stderr, exit code, and any operational error from the docker daemon.
// A non-zero exit code is NOT returned as an error — callers inspect the int.
func (c *Client) ExecCommand(ctx context.Context, container string, cmd []string) (stdout string, stderr string, exitCode int, err error) {
	create, err := c.cli.ContainerExecCreate(ctx, container, types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", "", 0, fmt.Errorf("exec create: %w", err)
	}
	attach, err := c.cli.ContainerExecAttach(ctx, create.ID, types.ExecStartCheck{})
	if err != nil {
		return "", "", 0, fmt.Errorf("exec attach: %w", err)
	}
	defer attach.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attach.Reader); err != nil {
		return stdoutBuf.String(), stderrBuf.String(), 0, fmt.Errorf("exec read: %w", err)
	}
	inspect, err := c.cli.ContainerExecInspect(ctx, create.ID)
	if err != nil {
		return stdoutBuf.String(), stderrBuf.String(), 0, fmt.Errorf("exec inspect: %w", err)
	}
	return stdoutBuf.String(), stderrBuf.String(), inspect.ExitCode, nil
}

// EnsureBridgeNetwork creates a default-driver bridge network with no IPAM
// override if absent. Idempotent. Differs from EnsureNetwork (which requires
// a subnet) — used for paas-net where we just want Docker DNS between
// service-plane containers and their per-env consumers.
func (c *Client) EnsureBridgeNetwork(ctx context.Context, name string) error {
	networks, err := c.cli.NetworkList(ctx, types.NetworkListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return err
	}
	for _, n := range networks {
		if n.Name == name {
			return nil
		}
	}
	_, err = c.cli.NetworkCreate(ctx, name, types.NetworkCreate{
		Driver: "bridge",
		Labels: map[string]string{"env-manager.managed": "true"},
	})
	return err
}
```

Add the necessary imports at the top of `client.go`:

```go
import (
	"bytes"        // new — for ExecCommand buffers
	"context"
	"fmt"          // new — for error wrapping
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"   // new — for RunContainer mounts
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"            // new — for IsConflict
	"github.com/docker/docker/pkg/stdcopy"        // new — for ExecCommand demux
)
```

- [ ] **Step 2: Verify the package builds**

```bash
cd backend && go build ./internal/docker/...
```

Expected: clean build (no output). If `mount`, `errdefs`, or `stdcopy` packages aren't pulled by go modules already, run `go mod tidy` to fetch them — they're transitive deps of the existing `docker/docker` import so this should be a no-op.

- [ ] **Step 3: Run all backend tests to confirm no regression**

```bash
cd backend && go test ./...
```

Expected: all PASS (no behaviour change in existing code).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/docker/client.go backend/go.mod backend/go.sum
git commit -m "feat(docker): add high-level RunContainer/ExecCommand/ContainerStatus/EnsureBridgeNetwork

These thin SDK wrappers are consumed by the upcoming
internal/services/postgres and internal/services/redis
provisioners. RunContainer pulls + creates + starts; ExecCommand
demuxes stdout/stderr; EnsureBridgeNetwork is the no-IPAM
counterpart to EnsureNetwork for paas-net.

Direct unit tests deferred — the SDK calls are exercised by the
service-plane provisioner tests via interface fakes and
manually verified on the home lab during Plan 3a rollout."
```

If `go mod tidy` was a no-op (no `go.mod`/`go.sum` change), drop those from the staged paths.

---

### Task 3: Scaffold `internal/services/postgres`

**Files:**
- Create: `backend/internal/services/postgres/postgres.go`
- Create: `backend/internal/services/postgres/postgres_test.go`

- [ ] **Step 1: Write `postgres.go` with types, Docker interface, password helper, and Provisioner constructor**

```go
// Package postgres provisions the env-manager service-plane Postgres singleton
// and per-environment databases.
//
// EnsureService boots the singleton container "paas-postgres" if absent.
// EnsureEnvDatabase creates a per-env database + user inside that container,
// storing the generated password in the credential store.
// DropEnvDatabase tears them both down on environment teardown.
//
// All container interactions go through the Docker interface so tests can
// substitute a fake. Production callers wire in *docker.Client via the
// realdocker adapter.
package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Constants used across the service-plane.
const (
	ContainerName  = "paas-postgres"
	Image          = "postgres:16"
	VolumeName     = "paas_postgres_data"
	MountPath      = "/var/lib/postgresql/data"
	NetworkName    = "paas-net"
	SuperuserKey   = "system:paas-postgres:superuser"
	defaultPwBytes = 24
	readyTimeout   = 60 * time.Second
	readyInterval  = 1 * time.Second
)

// RunSpec mirrors docker.RunSpec but is locally redeclared so the postgres
// package doesn't import the docker package directly. The realdocker adapter
// translates between the two.
type RunSpec struct {
	Name    string
	Image   string
	Network string
	Volumes map[string]string
	Env     map[string]string
	Cmd     []string
	Labels  map[string]string
}

// Docker is the minimal subset of docker.Client behaviour the provisioner needs.
type Docker interface {
	ContainerStatus(ctx context.Context, name string) (exists, running bool, err error)
	RunContainer(ctx context.Context, spec RunSpec) error
	StartContainer(name string) error
	ExecCommand(ctx context.Context, container string, cmd []string) (stdout, stderr string, exitCode int, err error)
	EnsureBridgeNetwork(ctx context.Context, name string) error
}

// CredStore is the cred-store subset the provisioner needs.
type CredStore interface {
	GetSystemSecret(key string) (string, error)
	SaveSystemSecret(key, value string) error
	SaveProjectSecret(projectID, key, value string) error // used for per-env passwords keyed by env id
	GetProjectSecret(projectID, key string) (string, error)
}

// EnvDatabase describes a per-environment Postgres database after provisioning.
// The password is stored in the credential store under PasswordKey.
type EnvDatabase struct {
	DatabaseName string // e.g. stripepayments_main
	Username     string // identical to DatabaseName
	PasswordKey  string // "env:<env-id>:db_password"
}

// Provisioner manages the service-plane Postgres singleton.
type Provisioner struct {
	docker      Docker
	creds       CredStore
	logger      *zap.Logger
	passwordGen func() (string, error)
	now         func() time.Time
}

// New constructs a Provisioner with sensible defaults.
func New(d Docker, creds CredStore, logger *zap.Logger) *Provisioner {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Provisioner{
		docker:      d,
		creds:       creds,
		logger:      logger,
		passwordGen: defaultPasswordGen,
		now:         time.Now,
	}
}

// defaultPasswordGen returns 24 random bytes hex-encoded (48 chars).
func defaultPasswordGen() (string, error) {
	buf := make([]byte, defaultPwBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SlugDatabaseName produces a Postgres-safe database name from a project name
// and a branch slug: lowercase, hyphens to underscores, joined with underscore.
// Identical to Username.
func SlugDatabaseName(projectName, branchSlug string) string {
	clean := func(s string) string {
		s = strings.ToLower(s)
		s = strings.ReplaceAll(s, "-", "")
		return s
	}
	return clean(projectName) + "_" + strings.ReplaceAll(branchSlug, "-", "_")
}
```

- [ ] **Step 2: Write `postgres_test.go` with the fake Docker / fake CredStore + a SlugDatabaseName test**

```go
package postgres

import (
	"context"
	"errors"
	"testing"
)

// fakeDocker captures every call so tests can assert the exact sequence.
type fakeDocker struct {
	statuses     map[string]containerState // by name
	statusErr    error
	runErr       error
	runCalls     []RunSpec
	startCalls   []string
	startErr     error
	execResults  []execResult // returned in order; single-element slice repeats
	execCalls    []execCall
	netCalls     []string
	netErr       error
}

type containerState struct{ exists, running bool }

type execCall struct {
	container string
	cmd       []string
}
type execResult struct {
	stdout, stderr string
	exitCode       int
	err            error
}

func (f *fakeDocker) ContainerStatus(_ context.Context, name string) (bool, bool, error) {
	if f.statusErr != nil {
		return false, false, f.statusErr
	}
	st := f.statuses[name]
	return st.exists, st.running, nil
}
func (f *fakeDocker) RunContainer(_ context.Context, spec RunSpec) error {
	f.runCalls = append(f.runCalls, spec)
	if f.runErr != nil {
		return f.runErr
	}
	if f.statuses == nil {
		f.statuses = map[string]containerState{}
	}
	f.statuses[spec.Name] = containerState{exists: true, running: true}
	return nil
}
func (f *fakeDocker) StartContainer(name string) error {
	f.startCalls = append(f.startCalls, name)
	if f.startErr != nil {
		return f.startErr
	}
	if f.statuses != nil {
		st := f.statuses[name]
		st.running = true
		f.statuses[name] = st
	}
	return nil
}
func (f *fakeDocker) ExecCommand(_ context.Context, container string, cmd []string) (string, string, int, error) {
	f.execCalls = append(f.execCalls, execCall{container, cmd})
	if len(f.execResults) == 0 {
		return "", "", 0, nil
	}
	if len(f.execResults) == 1 {
		r := f.execResults[0]
		return r.stdout, r.stderr, r.exitCode, r.err
	}
	r := f.execResults[0]
	f.execResults = f.execResults[1:]
	return r.stdout, r.stderr, r.exitCode, r.err
}
func (f *fakeDocker) EnsureBridgeNetwork(_ context.Context, name string) error {
	f.netCalls = append(f.netCalls, name)
	return f.netErr
}

// fakeCreds implements CredStore in-memory.
type fakeCreds struct {
	system  map[string]string
	project map[string]map[string]string
}

func newFakeCreds() *fakeCreds {
	return &fakeCreds{
		system:  map[string]string{},
		project: map[string]map[string]string{},
	}
}
func (f *fakeCreds) GetSystemSecret(k string) (string, error) {
	v, ok := f.system[k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (f *fakeCreds) SaveSystemSecret(k, v string) error {
	f.system[k] = v
	return nil
}
func (f *fakeCreds) SaveProjectSecret(pid, k, v string) error {
	if f.project[pid] == nil {
		f.project[pid] = map[string]string{}
	}
	f.project[pid][k] = v
	return nil
}
func (f *fakeCreds) GetProjectSecret(pid, k string) (string, error) {
	v, ok := f.project[pid][k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

// newTestProvisioner builds a Provisioner with deterministic password gen.
func newTestProvisioner(t *testing.T, fd *fakeDocker, fc *fakeCreds) *Provisioner {
	t.Helper()
	p := New(fd, fc, nil)
	pwSeq := []string{
		"00000000000000000000000000000000000000000000aaaa",
		"00000000000000000000000000000000000000000000bbbb",
		"00000000000000000000000000000000000000000000cccc",
	}
	idx := 0
	p.passwordGen = func() (string, error) {
		if idx >= len(pwSeq) {
			return "deadbeef", nil
		}
		v := pwSeq[idx]
		idx++
		return v, nil
	}
	return p
}

func TestSlugDatabaseName(t *testing.T) {
	cases := []struct {
		name, project, branch, want string
	}{
		{"plain", "myapp", "main", "myapp_main"},
		{"hyphens stripped from project", "stripe-payments", "main", "stripepayments_main"},
		{"hyphens replaced in branch", "stripe-payments", "feature-x", "stripepayments_feature_x"},
		{"uppercase folded", "MyApp", "Main", "myapp_main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SlugDatabaseName(tc.project, tc.branch)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 3: Verify package builds and the slug test passes**

```bash
cd backend && go test ./internal/services/postgres/... -v
```

Expected: `TestSlugDatabaseName` PASS (with 4 subtests). Other test files don't yet exist.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/services/postgres/postgres.go backend/internal/services/postgres/postgres_test.go
git commit -m "feat(services/postgres): scaffold Provisioner + Docker interface

Types + interface + fake-docker test harness + slug helper, no
EnsureService/EnsureEnvDatabase/DropEnvDatabase implementations
yet (those land per-method TDD-style)."
```

---

### Task 4: `Provisioner.EnsureService` (singleton bootstrap)

**Files:**
- Modify: `backend/internal/services/postgres/postgres.go`
- Modify: `backend/internal/services/postgres/postgres_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `postgres_test.go`:

```go
func TestEnsureService_FreshBoot(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{},
		execResults: []execResult{
			// pg_isready succeeds first poll
			{exitCode: 0},
		},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatalf("EnsureService: %v", err)
	}

	// Network ensured
	if len(fd.netCalls) != 1 || fd.netCalls[0] != NetworkName {
		t.Fatalf("expected single EnsureBridgeNetwork(%q), got %v", NetworkName, fd.netCalls)
	}
	// Container created with the right spec
	if len(fd.runCalls) != 1 {
		t.Fatalf("expected 1 RunContainer call, got %d", len(fd.runCalls))
	}
	spec := fd.runCalls[0]
	if spec.Name != ContainerName || spec.Image != Image || spec.Network != NetworkName {
		t.Errorf("unexpected spec: %+v", spec)
	}
	if spec.Volumes[VolumeName] != MountPath {
		t.Errorf("volume mount wrong: %v", spec.Volumes)
	}
	if spec.Env["POSTGRES_PASSWORD"] == "" {
		t.Errorf("POSTGRES_PASSWORD not set")
	}
	if spec.Labels["env-manager.singleton"] != "postgres" {
		t.Errorf("singleton label missing")
	}
	// Superuser password persisted
	saved, err := fc.GetSystemSecret(SuperuserKey)
	if err != nil || saved != spec.Env["POSTGRES_PASSWORD"] {
		t.Errorf("password not persisted (saved=%q, env=%q, err=%v)", saved, spec.Env["POSTGRES_PASSWORD"], err)
	}
	// pg_isready was attempted at least once
	if len(fd.execCalls) == 0 {
		t.Error("expected pg_isready exec, got none")
	}
}

func TestEnsureService_ReusesStoredPassword(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "previously-saved-password")
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fd.runCalls[0].Env["POSTGRES_PASSWORD"] != "previously-saved-password" {
		t.Errorf("expected stored pw to be reused, got %q", fd.runCalls[0].Env["POSTGRES_PASSWORD"])
	}
	// passwordGen NOT consumed when stored pw exists.
}

func TestEnsureService_RunningIsNoop(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.runCalls) != 0 {
		t.Errorf("expected no RunContainer when already running, got %d calls", len(fd.runCalls))
	}
	if len(fd.startCalls) != 0 {
		t.Errorf("expected no StartContainer when already running, got %d calls", len(fd.startCalls))
	}
}

func TestEnsureService_StoppedIsStarted(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: false},
		},
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.startCalls) != 1 || fd.startCalls[0] != ContainerName {
		t.Errorf("expected single StartContainer(%q), got %v", ContainerName, fd.startCalls)
	}
}

func TestEnsureService_ReadyTimeout(t *testing.T) {
	// Force pg_isready to always return non-zero.
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{exitCode: 1, stderr: "not ready"}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)
	// Tighten timeout so the test runs fast.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := p.EnsureService(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
```

Add `"time"` to the imports of `postgres_test.go`.

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/services/postgres/... -run TestEnsureService -v
```

Expected: compile error — `*Provisioner has no method EnsureService`.

- [ ] **Step 3: Implement `EnsureService` in `postgres.go`**

Append to `postgres.go`:

```go
// EnsureService idempotently brings the singleton paas-postgres container into
// a running, ready-to-accept-connections state. Safe to call on every boot.
//
// Behaviour:
//   - paas-net is ensured to exist before the container is launched.
//   - If the container is running, returns nil after a sanity ready-check.
//   - If the container exists but is stopped, starts it and waits for ready.
//   - If absent, creates the volume-backed container with a generated
//     superuser password (or reuses one from the credential store).
func (p *Provisioner) EnsureService(ctx context.Context) error {
	if err := p.docker.EnsureBridgeNetwork(ctx, NetworkName); err != nil {
		return fmt.Errorf("ensure paas-net: %w", err)
	}

	exists, running, err := p.docker.ContainerStatus(ctx, ContainerName)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", ContainerName, err)
	}
	switch {
	case exists && running:
		return p.waitReady(ctx)
	case exists && !running:
		if err := p.docker.StartContainer(ContainerName); err != nil {
			return fmt.Errorf("start %s: %w", ContainerName, err)
		}
		return p.waitReady(ctx)
	}

	pw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		// First boot — generate and persist.
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return fmt.Errorf("generate superuser password: %w", gerr)
		}
		if serr := p.creds.SaveSystemSecret(SuperuserKey, generated); serr != nil {
			return fmt.Errorf("save superuser password: %w", serr)
		}
		pw = generated
	}

	spec := RunSpec{
		Name:    ContainerName,
		Image:   Image,
		Network: NetworkName,
		Volumes: map[string]string{VolumeName: MountPath},
		Env:     map[string]string{"POSTGRES_PASSWORD": pw},
		Labels: map[string]string{
			"env-manager.managed":   "true",
			"env-manager.singleton": "postgres",
		},
	}
	if err := p.docker.RunContainer(ctx, spec); err != nil {
		return fmt.Errorf("run %s: %w", ContainerName, err)
	}
	return p.waitReady(ctx)
}

// waitReady polls pg_isready inside the container until exit code 0 or the
// context deadline is hit. Internal helper.
func (p *Provisioner) waitReady(ctx context.Context) error {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readyTimeout)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	for {
		stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName, []string{"pg_isready", "-U", "postgres"})
		if err == nil && code == 0 {
			return nil
		}
		if p.now().After(deadline) {
			return fmt.Errorf("paas-postgres not ready before deadline: code=%d stdout=%q stderr=%q lastErr=%v", code, stdout, stderr, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("paas-postgres ready wait cancelled: %w", ctx.Err())
		case <-time.After(readyInterval):
		}
	}
}
```

- [ ] **Step 4: Run tests to verify passes**

```bash
cd backend && go test ./internal/services/postgres/... -v
```

Expected: all 5 `TestEnsureService_*` subtests + `TestSlugDatabaseName` PASS. The `ReadyTimeout` test should fail-fast within ~50-100ms.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/postgres/postgres.go backend/internal/services/postgres/postgres_test.go
git commit -m "feat(services/postgres): EnsureService boots singleton paas-postgres

Idempotent: noop when running, restarts if stopped, creates from
scratch otherwise. Generates and persists a 24-byte superuser
password on first boot; reuses it across restarts. Polls
pg_isready until ready or context deadline."
```

---

### Task 5: `Provisioner.EnsureEnvDatabase`

**Files:**
- Modify: `backend/internal/services/postgres/postgres.go`
- Modify: `backend/internal/services/postgres/postgres_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `postgres_test.go`:

```go
func TestEnsureEnvDatabase_FreshCreate(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		// pg_isready (during EnsureService warmup, if called) + 3 SQL execs all succeed
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvDatabase: %v", err)
	}

	want := &EnvDatabase{
		DatabaseName: "stripepayments_main",
		Username:     "stripepayments_main",
		PasswordKey:  "env:envid-abc:db_password",
	}
	if got.DatabaseName != want.DatabaseName || got.Username != want.Username || got.PasswordKey != want.PasswordKey {
		t.Errorf("got %+v want %+v", got, want)
	}

	// Password was generated and stored under the env-id key.
	stored, err := fc.GetProjectSecret("envid-abc", "db_password")
	if err != nil {
		t.Fatalf("password not stored: %v", err)
	}
	if stored == "" || len(stored) < 16 {
		t.Errorf("stored password seems wrong: %q", stored)
	}

	// Three psql commands ran in order: CREATE DATABASE, CREATE USER, GRANT.
	psqlCalls := filterPsqlCalls(fd.execCalls)
	if len(psqlCalls) != 3 {
		t.Fatalf("expected 3 psql calls, got %d: %+v", len(psqlCalls), psqlCalls)
	}
	if !contains(psqlCalls[0].cmd, "CREATE DATABASE \"stripepayments_main\"") {
		t.Errorf("first call should CREATE DATABASE, got %v", psqlCalls[0].cmd)
	}
	if !contains(psqlCalls[1].cmd, "CREATE USER \"stripepayments_main\"") {
		t.Errorf("second call should CREATE USER, got %v", psqlCalls[1].cmd)
	}
	if !contains(psqlCalls[2].cmd, "GRANT ALL ON DATABASE \"stripepayments_main\" TO \"stripepayments_main\"") {
		t.Errorf("third call should GRANT, got %v", psqlCalls[2].cmd)
	}
}

func TestEnsureEnvDatabase_IdempotentOnReRun(t *testing.T) {
	// Second invocation: psql returns "already exists" errors which the
	// provisioner must treat as success.
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{
			{exitCode: 1, stderr: "ERROR:  database \"stripepayments_main\" already exists"},
			{exitCode: 1, stderr: "ERROR:  role \"stripepayments_main\" already exists"},
			{exitCode: 0}, // GRANT is idempotent
		},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	_ = fc.SaveProjectSecret("envid-abc", "db_password", "existing-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvDatabase should be idempotent: %v", err)
	}
	if got.DatabaseName != "stripepayments_main" {
		t.Errorf("name wrong: %v", got)
	}
	// Existing password was NOT overwritten.
	stored, _ := fc.GetProjectSecret("envid-abc", "db_password")
	if stored != "existing-pw" {
		t.Errorf("expected existing-pw preserved, got %q", stored)
	}
}

func TestEnsureEnvDatabase_UnknownPsqlError(t *testing.T) {
	// A non-"already exists" stderr should propagate as a real error.
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{
			{exitCode: 1, stderr: "FATAL:  the database is broken"},
		},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	_, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "x", "main")
	if err == nil {
		t.Fatal("expected error for unknown psql failure")
	}
}

// helpers
func filterPsqlCalls(calls []execCall) []execCall {
	var out []execCall
	for _, c := range calls {
		if len(c.cmd) > 0 && c.cmd[0] == "psql" {
			out = append(out, c)
		}
	}
	return out
}
func contains(cmd []string, fragment string) bool {
	for _, s := range cmd {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}
```

Add `"strings"` to the imports of `postgres_test.go`.

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/services/postgres/... -run TestEnsureEnvDatabase -v
```

Expected: compile error — `*Provisioner has no method EnsureEnvDatabase`.

- [ ] **Step 3: Implement `EnsureEnvDatabase` in `postgres.go`**

Append to `postgres.go`:

```go
// EnsureEnvDatabase ensures a per-environment database + user + grant exist
// inside the singleton paas-postgres. Idempotent: pre-existing entities are
// detected via "already exists" stderr and treated as success.
//
// Generates and stores a 24-byte password under the credential store key
// "env:<envID>:db_password" on first creation. Pre-existing passwords are
// preserved across re-runs (we never rotate from inside this method).
func (p *Provisioner) EnsureEnvDatabase(ctx context.Context, envID, projectName, branchSlug string) (*EnvDatabase, error) {
	dbName := SlugDatabaseName(projectName, branchSlug)
	pwKey := "db_password" // stored under projectID = envID by SaveProjectSecret
	pwStoreKey := "env:" + envID + ":db_password"

	// Resolve user password — reuse existing or generate.
	password, err := p.creds.GetProjectSecret(envID, pwKey)
	if err != nil {
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return nil, fmt.Errorf("generate db password: %w", gerr)
		}
		if serr := p.creds.SaveProjectSecret(envID, pwKey, generated); serr != nil {
			return nil, fmt.Errorf("save db password: %w", serr)
		}
		password = generated
	}

	// CREATE DATABASE
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`CREATE DATABASE "%s";`, dbName),
		"already exists",
	); err != nil {
		return nil, fmt.Errorf("create database %s: %w", dbName, err)
	}

	// CREATE USER (= ROLE)
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`CREATE USER "%s" WITH ENCRYPTED PASSWORD '%s';`, dbName, password),
		"already exists",
	); err != nil {
		return nil, fmt.Errorf("create user %s: %w", dbName, err)
	}

	// GRANT ALL — idempotent natively
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`GRANT ALL ON DATABASE "%s" TO "%s";`, dbName, dbName),
		"",
	); err != nil {
		return nil, fmt.Errorf("grant on %s: %w", dbName, err)
	}

	return &EnvDatabase{
		DatabaseName: dbName,
		Username:     dbName,
		PasswordKey:  pwStoreKey,
	}, nil
}

// runPsqlIdempotent runs `psql -U postgres -c "<sql>"` inside paas-postgres.
// If the command fails with stderr containing benignFragment, the error is
// swallowed (idempotency). Empty benignFragment = always treat non-zero as
// real error. Returns nil on success or benign-failure.
func (p *Provisioner) runPsqlIdempotent(ctx context.Context, sql, benignFragment string) error {
	stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName,
		[]string{"psql", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-c", sql},
	)
	if err != nil {
		return fmt.Errorf("psql exec: %w (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if code == 0 {
		return nil
	}
	if benignFragment != "" && strings.Contains(stderr, benignFragment) {
		p.logger.Debug("psql idempotency hit", zap.String("sql", sql), zap.String("stderr", strings.TrimSpace(stderr)))
		return nil
	}
	return fmt.Errorf("psql exit %d: stderr=%s", code, strings.TrimSpace(stderr))
}
```

- [ ] **Step 4: Run tests to verify passes**

```bash
cd backend && go test ./internal/services/postgres/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/postgres/postgres.go backend/internal/services/postgres/postgres_test.go
git commit -m "feat(services/postgres): EnsureEnvDatabase provisions per-env DB + user

Creates database, role, and grant inside paas-postgres for a
given environment. Generates and stores a 24-byte password
under env:<id>:db_password. Idempotent: 'already exists' stderr
is recognised and swallowed."
```

---

### Task 6: `Provisioner.DropEnvDatabase`

**Files:**
- Modify: `backend/internal/services/postgres/postgres.go`
- Modify: `backend/internal/services/postgres/postgres_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `postgres_test.go`:

```go
func TestDropEnvDatabase_DropsBoth(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{{exitCode: 0}}, // both DROPs succeed
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	if err := p.DropEnvDatabase(context.Background(), "stripe-payments", "main"); err != nil {
		t.Fatalf("DropEnvDatabase: %v", err)
	}
	psqlCalls := filterPsqlCalls(fd.execCalls)
	if len(psqlCalls) != 2 {
		t.Fatalf("expected 2 psql calls, got %d: %+v", len(psqlCalls), psqlCalls)
	}
	if !contains(psqlCalls[0].cmd, "DROP DATABASE IF EXISTS \"stripepayments_main\"") {
		t.Errorf("first should DROP DATABASE, got %v", psqlCalls[0].cmd)
	}
	if !contains(psqlCalls[1].cmd, "DROP USER IF EXISTS \"stripepayments_main\"") {
		t.Errorf("second should DROP USER, got %v", psqlCalls[1].cmd)
	}
}

func TestDropEnvDatabase_AbsentIsNoop(t *testing.T) {
	// "does not exist" stderr from IF EXISTS should be impossible (psql
	// doesn't error on IF EXISTS), but defensive: even non-IF-EXISTS-friendly
	// stderr matching "does not exist" passes.
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{{exitCode: 0}}, // IF EXISTS suppresses
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	if err := p.DropEnvDatabase(context.Background(), "stripe-payments", "deleted-branch"); err != nil {
		t.Errorf("expected nil for absent DB, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/services/postgres/... -run TestDropEnvDatabase -v
```

Expected: compile error — `*Provisioner has no method DropEnvDatabase`.

- [ ] **Step 3: Implement `DropEnvDatabase` in `postgres.go`**

Append to `postgres.go`:

```go
// DropEnvDatabase removes the database and user for an environment. Both DROPs
// use IF EXISTS so the call is naturally idempotent. The credential-store
// password entry is NOT removed here — callers handle that as part of env
// teardown alongside their own cleanup.
func (p *Provisioner) DropEnvDatabase(ctx context.Context, projectName, branchSlug string) error {
	dbName := SlugDatabaseName(projectName, branchSlug)
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, dbName),
		"does not exist",
	); err != nil {
		return fmt.Errorf("drop database %s: %w", dbName, err)
	}
	if err := p.runPsqlIdempotent(ctx,
		fmt.Sprintf(`DROP USER IF EXISTS "%s";`, dbName),
		"does not exist",
	); err != nil {
		return fmt.Errorf("drop user %s: %w", dbName, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify passes**

```bash
cd backend && go test ./internal/services/postgres/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/postgres/postgres.go backend/internal/services/postgres/postgres_test.go
git commit -m "feat(services/postgres): DropEnvDatabase removes per-env DB + user

DROP DATABASE IF EXISTS + DROP USER IF EXISTS — idempotent.
Cred-store password entry is left untouched; caller's own
teardown logic owns that lifecycle."
```

---

### Task 7: Scaffold `internal/services/redis`

**Files:**
- Create: `backend/internal/services/redis/redis.go`
- Create: `backend/internal/services/redis/redis_test.go`

The Redis package mirrors postgres in shape: `Provisioner`, `Docker` interface, password helper, `EnsureService`, `EnsureEnvACL`, `DropEnvACL`. Test harness is structurally identical.

- [ ] **Step 1: Write `redis.go` skeleton**

```go
// Package redis provisions the env-manager service-plane Redis singleton and
// per-environment ACL users.
//
// EnsureService boots the singleton container "paas-redis" if absent.
// EnsureEnvACL creates a per-env ACL user with prefix-scoped permissions.
// DropEnvACL removes one on environment teardown.
package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	ContainerName  = "paas-redis"
	Image          = "redis:7"
	VolumeName     = "paas_redis_data"
	MountPath      = "/data"
	NetworkName    = "paas-net"
	SuperuserKey   = "system:paas-redis:superuser"
	defaultPwBytes = 24
	readyTimeout   = 60 * time.Second
	readyInterval  = 1 * time.Second
)

type RunSpec struct {
	Name    string
	Image   string
	Network string
	Volumes map[string]string
	Env     map[string]string
	Cmd     []string
	Labels  map[string]string
}

// Docker is the minimal docker.Client subset the provisioner needs.
type Docker interface {
	ContainerStatus(ctx context.Context, name string) (exists, running bool, err error)
	RunContainer(ctx context.Context, spec RunSpec) error
	StartContainer(name string) error
	ExecCommand(ctx context.Context, container string, cmd []string) (stdout, stderr string, exitCode int, err error)
	EnsureBridgeNetwork(ctx context.Context, name string) error
}

// CredStore is the cred-store subset the provisioner needs.
type CredStore interface {
	GetSystemSecret(key string) (string, error)
	SaveSystemSecret(key, value string) error
	SaveProjectSecret(projectID, key, value string) error
	GetProjectSecret(projectID, key string) (string, error)
}

// EnvACL describes a per-environment Redis ACL user after provisioning.
type EnvACL struct {
	Username    string // identical to per-env DB name (postgres convention)
	KeyPrefix   string // "<project_slug>:<branch_slug>"
	PasswordKey string // "env:<env-id>:redis_password"
}

// Provisioner manages the service-plane Redis singleton.
type Provisioner struct {
	docker      Docker
	creds       CredStore
	logger      *zap.Logger
	passwordGen func() (string, error)
	now         func() time.Time
}

func New(d Docker, creds CredStore, logger *zap.Logger) *Provisioner {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Provisioner{
		docker:      d,
		creds:       creds,
		logger:      logger,
		passwordGen: defaultPasswordGen,
		now:         time.Now,
	}
}

func defaultPasswordGen() (string, error) {
	buf := make([]byte, defaultPwBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SlugUserName mirrors postgres.SlugDatabaseName for ACL user naming.
func SlugUserName(projectName, branchSlug string) string {
	clean := strings.ToLower(strings.ReplaceAll(projectName, "-", ""))
	return clean + "_" + strings.ReplaceAll(branchSlug, "-", "_")
}

// SlugKeyPrefix produces the Redis key prefix scope: "<project>:<branch>".
// Hyphens in either are kept (they are valid in Redis key names).
func SlugKeyPrefix(projectName, branchSlug string) string {
	return strings.ToLower(strings.ReplaceAll(projectName, "_", "-")) + ":" + branchSlug
}
```

- [ ] **Step 2: Write `redis_test.go` with the fake Docker / fake CredStore**

```go
package redis

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDocker struct {
	statuses     map[string]containerState
	statusErr    error
	runErr       error
	runCalls     []RunSpec
	startCalls   []string
	startErr     error
	execResults  []execResult
	execCalls    []execCall
	netCalls     []string
	netErr       error
}

type containerState struct{ exists, running bool }
type execCall struct {
	container string
	cmd       []string
}
type execResult struct {
	stdout, stderr string
	exitCode       int
	err            error
}

func (f *fakeDocker) ContainerStatus(_ context.Context, name string) (bool, bool, error) {
	if f.statusErr != nil {
		return false, false, f.statusErr
	}
	st := f.statuses[name]
	return st.exists, st.running, nil
}
func (f *fakeDocker) RunContainer(_ context.Context, spec RunSpec) error {
	f.runCalls = append(f.runCalls, spec)
	if f.runErr != nil {
		return f.runErr
	}
	if f.statuses == nil {
		f.statuses = map[string]containerState{}
	}
	f.statuses[spec.Name] = containerState{exists: true, running: true}
	return nil
}
func (f *fakeDocker) StartContainer(name string) error {
	f.startCalls = append(f.startCalls, name)
	if f.startErr != nil {
		return f.startErr
	}
	if f.statuses != nil {
		st := f.statuses[name]
		st.running = true
		f.statuses[name] = st
	}
	return nil
}
func (f *fakeDocker) ExecCommand(_ context.Context, container string, cmd []string) (string, string, int, error) {
	f.execCalls = append(f.execCalls, execCall{container, cmd})
	if len(f.execResults) == 0 {
		return "", "", 0, nil
	}
	if len(f.execResults) == 1 {
		r := f.execResults[0]
		return r.stdout, r.stderr, r.exitCode, r.err
	}
	r := f.execResults[0]
	f.execResults = f.execResults[1:]
	return r.stdout, r.stderr, r.exitCode, r.err
}
func (f *fakeDocker) EnsureBridgeNetwork(_ context.Context, name string) error {
	f.netCalls = append(f.netCalls, name)
	return f.netErr
}

type fakeCreds struct {
	system  map[string]string
	project map[string]map[string]string
}

func newFakeCreds() *fakeCreds {
	return &fakeCreds{system: map[string]string{}, project: map[string]map[string]string{}}
}
func (f *fakeCreds) GetSystemSecret(k string) (string, error) {
	v, ok := f.system[k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (f *fakeCreds) SaveSystemSecret(k, v string) error { f.system[k] = v; return nil }
func (f *fakeCreds) SaveProjectSecret(pid, k, v string) error {
	if f.project[pid] == nil {
		f.project[pid] = map[string]string{}
	}
	f.project[pid][k] = v
	return nil
}
func (f *fakeCreds) GetProjectSecret(pid, k string) (string, error) {
	v, ok := f.project[pid][k]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func newTestProvisioner(t *testing.T, fd *fakeDocker, fc *fakeCreds) *Provisioner {
	t.Helper()
	p := New(fd, fc, nil)
	pwSeq := []string{
		"00000000000000000000000000000000000000000000aaaa",
		"00000000000000000000000000000000000000000000bbbb",
	}
	idx := 0
	p.passwordGen = func() (string, error) {
		if idx >= len(pwSeq) {
			return "deadbeef", nil
		}
		v := pwSeq[idx]
		idx++
		return v, nil
	}
	return p
}

func TestSlugUserName(t *testing.T) {
	if got := SlugUserName("stripe-payments", "feature-x"); got != "stripepayments_feature_x" {
		t.Errorf("got %q", got)
	}
}

func TestSlugKeyPrefix(t *testing.T) {
	if got := SlugKeyPrefix("stripe-payments", "main"); got != "stripe-payments:main" {
		t.Errorf("got %q", got)
	}
}

// helpers for command-content assertions
func filterRedisCliCalls(calls []execCall) []execCall {
	var out []execCall
	for _, c := range calls {
		if len(c.cmd) > 0 && c.cmd[0] == "redis-cli" {
			out = append(out, c)
		}
	}
	return out
}
func contains(cmd []string, fragment string) bool {
	for _, s := range cmd {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Verify package builds + slug tests pass**

```bash
cd backend && go test ./internal/services/redis/... -v
```

Expected: `TestSlugUserName` and `TestSlugKeyPrefix` PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/services/redis/redis.go backend/internal/services/redis/redis_test.go
git commit -m "feat(services/redis): scaffold Provisioner + Docker interface

Mirrors services/postgres shape: types, Docker/CredStore
interfaces, fake-docker test harness, password helper, slug
helpers. Methods land per-task."
```

---

### Task 8: Redis `Provisioner.EnsureService`

**Files:**
- Modify: `backend/internal/services/redis/redis.go`
- Modify: `backend/internal/services/redis/redis_test.go`

- [ ] **Step 1: Add the `time` import to `redis_test.go`**

Update the imports in `redis_test.go` to include `"time"`:

```go
import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Write the failing tests**

Append to `redis_test.go`:

```go
func TestRedisEnsureService_FreshBoot(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{stdout: "PONG", exitCode: 0}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)

	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatalf("EnsureService: %v", err)
	}
	if len(fd.netCalls) != 1 || fd.netCalls[0] != NetworkName {
		t.Fatalf("expected EnsureBridgeNetwork(%q)", NetworkName)
	}
	if len(fd.runCalls) != 1 {
		t.Fatalf("expected 1 RunContainer call, got %d", len(fd.runCalls))
	}
	spec := fd.runCalls[0]
	if spec.Name != ContainerName || spec.Image != Image {
		t.Errorf("wrong spec: %+v", spec)
	}
	if spec.Volumes[VolumeName] != MountPath {
		t.Errorf("volume mount wrong: %v", spec.Volumes)
	}
	// redis-server --requirepass <generated>
	if len(spec.Cmd) < 3 || spec.Cmd[0] != "redis-server" || spec.Cmd[1] != "--requirepass" {
		t.Errorf("expected redis-server --requirepass <pw>, got %v", spec.Cmd)
	}
	if spec.Cmd[2] == "" {
		t.Errorf("password arg empty: %v", spec.Cmd)
	}
	saved, err := fc.GetSystemSecret(SuperuserKey)
	if err != nil || saved != spec.Cmd[2] {
		t.Errorf("password not persisted (saved=%q, cmd=%q)", saved, spec.Cmd[2])
	}
}

func TestRedisEnsureService_RunningIsNoop(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "PONG", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)
	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.runCalls) != 0 || len(fd.startCalls) != 0 {
		t.Errorf("expected idempotent noop, got %d run / %d start", len(fd.runCalls), len(fd.startCalls))
	}
}

func TestRedisEnsureService_StoppedIsStarted(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: false}},
		execResults: []execResult{{stdout: "PONG", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)
	if err := p.EnsureService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fd.startCalls) != 1 || fd.startCalls[0] != ContainerName {
		t.Errorf("expected single StartContainer(%q), got %v", ContainerName, fd.startCalls)
	}
}

func TestRedisEnsureService_ReadyTimeout(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{},
		execResults: []execResult{{exitCode: 1, stderr: "Could not connect"}},
	}
	fc := newFakeCreds()
	p := newTestProvisioner(t, fd, fc)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.EnsureService(ctx); err == nil {
		t.Fatal("expected timeout error")
	}
}
```

- [ ] **Step 3: Run tests to verify failures**

```bash
cd backend && go test ./internal/services/redis/... -run TestRedisEnsureService -v
```

Expected: compile error.

- [ ] **Step 4: Implement `EnsureService` in `redis.go`**

Append to `redis.go`:

```go
// EnsureService idempotently brings paas-redis into a running state.
//
// On first boot, generates a 24-byte superuser password (stored under
// SuperuserKey), launches redis:7 with `redis-server --requirepass <pw>`,
// and waits for `redis-cli ping` → "PONG".
func (p *Provisioner) EnsureService(ctx context.Context) error {
	if err := p.docker.EnsureBridgeNetwork(ctx, NetworkName); err != nil {
		return fmt.Errorf("ensure paas-net: %w", err)
	}
	exists, running, err := p.docker.ContainerStatus(ctx, ContainerName)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", ContainerName, err)
	}
	switch {
	case exists && running:
		return p.waitReady(ctx)
	case exists && !running:
		if err := p.docker.StartContainer(ContainerName); err != nil {
			return fmt.Errorf("start %s: %w", ContainerName, err)
		}
		return p.waitReady(ctx)
	}

	pw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return fmt.Errorf("generate redis superuser password: %w", gerr)
		}
		if serr := p.creds.SaveSystemSecret(SuperuserKey, generated); serr != nil {
			return fmt.Errorf("save redis superuser password: %w", serr)
		}
		pw = generated
	}

	spec := RunSpec{
		Name:    ContainerName,
		Image:   Image,
		Network: NetworkName,
		Volumes: map[string]string{VolumeName: MountPath},
		Cmd:     []string{"redis-server", "--requirepass", pw},
		Labels: map[string]string{
			"env-manager.managed":   "true",
			"env-manager.singleton": "redis",
		},
	}
	if err := p.docker.RunContainer(ctx, spec); err != nil {
		return fmt.Errorf("run %s: %w", ContainerName, err)
	}
	return p.waitReady(ctx)
}

// waitReady polls `redis-cli -a <pw> ping` until it returns PONG (exit 0
// with stdout containing "PONG") or the context deadline is hit.
func (p *Provisioner) waitReady(ctx context.Context) error {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readyTimeout)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	pw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		return fmt.Errorf("redis superuser password missing for ping: %w", err)
	}
	for {
		stdout, _, code, eErr := p.docker.ExecCommand(ctx, ContainerName,
			[]string{"redis-cli", "-a", pw, "ping"},
		)
		if eErr == nil && code == 0 && strings.Contains(stdout, "PONG") {
			return nil
		}
		if p.now().After(deadline) {
			return fmt.Errorf("paas-redis not ready before deadline: code=%d stdout=%q lastErr=%v", code, stdout, eErr)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("paas-redis ready wait cancelled: %w", ctx.Err())
		case <-time.After(readyInterval):
		}
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd backend && go test ./internal/services/redis/... -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/redis/redis.go backend/internal/services/redis/redis_test.go
git commit -m "feat(services/redis): EnsureService boots singleton paas-redis

Idempotent: noop when running, restarts if stopped, fresh boot
otherwise. Generates a 24-byte requirepass on first boot,
launches redis:7 with --requirepass, waits for redis-cli PONG."
```

---

### Task 9: `Provisioner.EnsureEnvACL`

**Files:**
- Modify: `backend/internal/services/redis/redis.go`
- Modify: `backend/internal/services/redis/redis_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `redis_test.go`:

```go
func TestEnsureEnvACL_FreshCreate(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "OK", exitCode: 0}}, // ACL SETUSER returns OK
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvACL(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvACL: %v", err)
	}
	want := &EnvACL{
		Username:    "stripepayments_main",
		KeyPrefix:   "stripe-payments:main",
		PasswordKey: "env:envid-abc:redis_password",
	}
	if got.Username != want.Username || got.KeyPrefix != want.KeyPrefix || got.PasswordKey != want.PasswordKey {
		t.Errorf("got %+v want %+v", got, want)
	}
	stored, err := fc.GetProjectSecret("envid-abc", "redis_password")
	if err != nil || stored == "" {
		t.Errorf("password not stored: %q (err=%v)", stored, err)
	}

	cliCalls := filterRedisCliCalls(fd.execCalls)
	if len(cliCalls) != 1 {
		t.Fatalf("expected 1 redis-cli call, got %d: %+v", len(cliCalls), cliCalls)
	}
	cmd := cliCalls[0].cmd
	// redis-cli -a <super> ACL SETUSER <user> on >password ~prefix:* +@all -@dangerous
	if !contains(cmd, "ACL") || !contains(cmd, "SETUSER") {
		t.Errorf("missing ACL SETUSER, got %v", cmd)
	}
	if !contains(cmd, "stripepayments_main") {
		t.Errorf("missing user, got %v", cmd)
	}
	if !contains(cmd, "~stripe-payments:main:*") {
		t.Errorf("missing prefix scope, got %v", cmd)
	}
	if !contains(cmd, "+@all") || !contains(cmd, "-@dangerous") {
		t.Errorf("missing capability flags, got %v", cmd)
	}
}

func TestEnsureEnvACL_IdempotentReUse(t *testing.T) {
	// Second call: stored password is reused (not regenerated).
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "OK", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	_ = fc.SaveProjectSecret("envid-abc", "redis_password", "stored-pw")
	p := newTestProvisioner(t, fd, fc)

	if _, err := p.EnsureEnvACL(context.Background(), "envid-abc", "stripe-payments", "main"); err != nil {
		t.Fatal(err)
	}
	stored, _ := fc.GetProjectSecret("envid-abc", "redis_password")
	if stored != "stored-pw" {
		t.Errorf("expected reused, got %q", stored)
	}
}

func TestEnsureEnvACL_RedisFailureBubbles(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{exitCode: 1, stderr: "(error) something broke"}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	_, err := p.EnsureEnvACL(context.Background(), "e", "p", "main")
	if err == nil {
		t.Fatal("expected redis-cli failure to surface")
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd backend && go test ./internal/services/redis/... -run TestEnsureEnvACL -v
```

Expected: compile error.

- [ ] **Step 3: Implement `EnsureEnvACL` in `redis.go`**

Append to `redis.go`:

```go
// EnsureEnvACL ensures a per-environment Redis ACL user exists with prefix
// scoping. Idempotent — `ACL SETUSER` replaces existing entries with the
// same name, so re-runs are safe. Stored passwords are reused across calls.
func (p *Provisioner) EnsureEnvACL(ctx context.Context, envID, projectName, branchSlug string) (*EnvACL, error) {
	user := SlugUserName(projectName, branchSlug)
	prefix := SlugKeyPrefix(projectName, branchSlug)
	pwKey := "redis_password"
	pwStoreKey := "env:" + envID + ":redis_password"

	password, err := p.creds.GetProjectSecret(envID, pwKey)
	if err != nil {
		generated, gerr := p.passwordGen()
		if gerr != nil {
			return nil, fmt.Errorf("generate redis password: %w", gerr)
		}
		if serr := p.creds.SaveProjectSecret(envID, pwKey, generated); serr != nil {
			return nil, fmt.Errorf("save redis password: %w", serr)
		}
		password = generated
	}

	superPw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		return nil, fmt.Errorf("redis superuser password missing: %w", err)
	}

	cmd := []string{
		"redis-cli", "-a", superPw,
		"ACL", "SETUSER", user,
		"on", ">" + password,
		"~" + prefix + ":*",
		"+@all", "-@dangerous",
	}
	stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName, cmd)
	if err != nil {
		return nil, fmt.Errorf("ACL SETUSER %s: %w (stdout=%q stderr=%q)", user, err, stdout, stderr)
	}
	if code != 0 {
		return nil, fmt.Errorf("ACL SETUSER %s exit %d: %s", user, code, strings.TrimSpace(stderr))
	}
	if !strings.Contains(stdout, "OK") {
		return nil, fmt.Errorf("ACL SETUSER %s: unexpected stdout %q", user, strings.TrimSpace(stdout))
	}

	return &EnvACL{
		Username:    user,
		KeyPrefix:   prefix,
		PasswordKey: pwStoreKey,
	}, nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/services/redis/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/redis/redis.go backend/internal/services/redis/redis_test.go
git commit -m "feat(services/redis): EnsureEnvACL provisions per-env scoped user

ACL SETUSER on the singleton paas-redis with key-prefix scoping
(~<project>:<branch>:*) and capability flags +@all -@dangerous.
Stores 24-byte password under env:<id>:redis_password. Idempotent
(ACL SETUSER replaces by name)."
```

---

### Task 10: `Provisioner.DropEnvACL`

**Files:**
- Modify: `backend/internal/services/redis/redis.go`
- Modify: `backend/internal/services/redis/redis_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `redis_test.go`:

```go
func TestDropEnvACL_RemovesUser(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "1", exitCode: 0}}, // DELUSER returns count
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	if err := p.DropEnvACL(context.Background(), "stripe-payments", "main"); err != nil {
		t.Fatalf("DropEnvACL: %v", err)
	}
	cliCalls := filterRedisCliCalls(fd.execCalls)
	if len(cliCalls) != 1 {
		t.Fatalf("expected 1 redis-cli call, got %d", len(cliCalls))
	}
	cmd := cliCalls[0].cmd
	if !contains(cmd, "DELUSER") || !contains(cmd, "stripepayments_main") {
		t.Errorf("expected ACL DELUSER stripepayments_main, got %v", cmd)
	}
}

func TestDropEnvACL_AbsentUserIsNoop(t *testing.T) {
	// DELUSER returns "0" in stdout when the user didn't exist; treat as success.
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "0", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)
	if err := p.DropEnvACL(context.Background(), "stripe-payments", "no-such-branch"); err != nil {
		t.Errorf("expected nil for absent ACL, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd backend && go test ./internal/services/redis/... -run TestDropEnvACL -v
```

Expected: compile error.

- [ ] **Step 3: Implement `DropEnvACL` in `redis.go`**

Append to `redis.go`:

```go
// DropEnvACL removes a per-environment ACL user. Idempotent — ACL DELUSER
// returns 0 (rather than erroring) when the user is absent. Cred-store
// password entry is left untouched; caller's own teardown handles that.
//
// Note: keys with the user's prefix are NOT auto-deleted. Per the design
// spec, this is acceptable for v2 (ACL gone = no access; orphan keys leak
// but cause no harm).
func (p *Provisioner) DropEnvACL(ctx context.Context, projectName, branchSlug string) error {
	user := SlugUserName(projectName, branchSlug)
	superPw, err := p.creds.GetSystemSecret(SuperuserKey)
	if err != nil {
		return fmt.Errorf("redis superuser password missing: %w", err)
	}
	cmd := []string{"redis-cli", "-a", superPw, "ACL", "DELUSER", user}
	stdout, stderr, code, err := p.docker.ExecCommand(ctx, ContainerName, cmd)
	if err != nil {
		return fmt.Errorf("ACL DELUSER %s: %w (stdout=%q stderr=%q)", user, err, stdout, stderr)
	}
	if code != 0 {
		return fmt.Errorf("ACL DELUSER %s exit %d: %s", user, code, strings.TrimSpace(stderr))
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/services/redis/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/redis/redis.go backend/internal/services/redis/redis_test.go
git commit -m "feat(services/redis): DropEnvACL removes per-env scoped user

ACL DELUSER on paas-redis. Idempotent — DELUSER returns 0 when
the user is absent rather than failing. Orphan keys with the
user's prefix are left in place per design spec."
```

---

### Task 11: Wire Flow G into `cmd/server/main.go`

**Files:**
- Create: `backend/internal/services/realdocker/realdocker.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write the realdocker adapter**

Create `backend/internal/services/realdocker/realdocker.go`:

```go
// Package realdocker adapts *docker.Client to satisfy the Docker interfaces
// declared by services/postgres and services/redis. The two interfaces are
// structurally identical except for the RunContainer parameter type — Go
// method sets can't have two RunContainer methods with different parameter
// types on the same struct, so this package exposes two separate adapter
// types: PostgresAdapter and RedisAdapter. They share an underlying
// *docker.Client.
package realdocker

import (
	"context"

	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/services/postgres"
	"github.com/environment-manager/backend/internal/services/redis"
)

// PostgresAdapter satisfies postgres.Docker.
type PostgresAdapter struct {
	c *docker.Client
}

// NewPostgres returns a PostgresAdapter wrapping the given client.
// Panics if c is nil.
func NewPostgres(c *docker.Client) *PostgresAdapter {
	if c == nil {
		panic("realdocker.NewPostgres: nil docker client")
	}
	return &PostgresAdapter{c: c}
}

func (a *PostgresAdapter) ContainerStatus(ctx context.Context, name string) (bool, bool, error) {
	return a.c.ContainerStatus(ctx, name)
}
func (a *PostgresAdapter) StartContainer(name string) error {
	return a.c.StartContainer(name)
}
func (a *PostgresAdapter) ExecCommand(ctx context.Context, container string, cmd []string) (string, string, int, error) {
	return a.c.ExecCommand(ctx, container, cmd)
}
func (a *PostgresAdapter) EnsureBridgeNetwork(ctx context.Context, name string) error {
	return a.c.EnsureBridgeNetwork(ctx, name)
}
func (a *PostgresAdapter) RunContainer(ctx context.Context, spec postgres.RunSpec) error {
	return a.c.RunContainer(ctx, docker.RunSpec{
		Name:    spec.Name,
		Image:   spec.Image,
		Network: spec.Network,
		Volumes: spec.Volumes,
		Env:     spec.Env,
		Cmd:     spec.Cmd,
		Labels:  spec.Labels,
	})
}

// RedisAdapter satisfies redis.Docker.
type RedisAdapter struct {
	c *docker.Client
}

// NewRedis returns a RedisAdapter wrapping the given client.
// Panics if c is nil.
func NewRedis(c *docker.Client) *RedisAdapter {
	if c == nil {
		panic("realdocker.NewRedis: nil docker client")
	}
	return &RedisAdapter{c: c}
}

func (a *RedisAdapter) ContainerStatus(ctx context.Context, name string) (bool, bool, error) {
	return a.c.ContainerStatus(ctx, name)
}
func (a *RedisAdapter) StartContainer(name string) error {
	return a.c.StartContainer(name)
}
func (a *RedisAdapter) ExecCommand(ctx context.Context, container string, cmd []string) (string, string, int, error) {
	return a.c.ExecCommand(ctx, container, cmd)
}
func (a *RedisAdapter) EnsureBridgeNetwork(ctx context.Context, name string) error {
	return a.c.EnsureBridgeNetwork(ctx, name)
}
func (a *RedisAdapter) RunContainer(ctx context.Context, spec redis.RunSpec) error {
	return a.c.RunContainer(ctx, docker.RunSpec{
		Name:    spec.Name,
		Image:   spec.Image,
		Network: spec.Network,
		Volumes: spec.Volumes,
		Env:     spec.Env,
		Cmd:     spec.Cmd,
		Labels:  spec.Labels,
	})
}
```

- [ ] **Step 2: Verify the adapter compiles**

```bash
cd backend && go build ./internal/services/realdocker/...
```

Expected: clean build.

- [ ] **Step 3: Wire Flow G into `main.go`**

Modify `backend/cmd/server/main.go`. Add imports:

```go
import (
	// ... existing imports ...
	"github.com/environment-manager/backend/internal/docker"
	"github.com/environment-manager/backend/internal/services/postgres"
	"github.com/environment-manager/backend/internal/services/redis"
	"github.com/environment-manager/backend/internal/services/realdocker"
)
```

Insert this block AFTER the credential store init (line ~45) and BEFORE the reposManager init:

```go
	// Service-plane bootstrap (Flow G): ensure paas-net + paas-postgres + paas-redis.
	// Failures are logged but don't abort startup — Plan 3a ships the bootstrap
	// without consumers; the runner doesn't yet require these to be running.
	if credStore == nil {
		logger.Warn("Service-plane bootstrap skipped: credential store unavailable")
	} else {
		dockerCli, err := docker.NewClient()
		if err != nil {
			logger.Error("Service-plane bootstrap: docker client init failed", zap.Error(err))
		} else {
			pg := postgres.New(realdocker.NewPostgres(dockerCli), credStore, logger)
			rd := redis.New(realdocker.NewRedis(dockerCli), credStore, logger)

			bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			if err := pg.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: postgres failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-postgres ready")
			}
			if err := rd.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: redis failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-redis ready")
			}
			bootstrapCancel()
			_ = dockerCli.Close()
		}
	}
```

- [ ] **Step 4: Verify everything builds**

```bash
cd backend && go build ./...
cd backend && go vet ./...
cd backend && go test ./...
```

Expected: clean build, clean vet, all tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/realdocker/realdocker.go backend/cmd/server/main.go
git commit -m "feat(server): wire service-plane bootstrap (Flow G) on startup

After cred-store init and before reconcile, ensure paas-net,
paas-postgres, and paas-redis are running. Failures are logged
but don't abort: Plan 3a ships bootstrap without consumers;
the runner doesn't depend on these yet (Plan 3b wires it in).

The realdocker package wraps *docker.Client and exposes two
adapter types — PostgresAdapter and RedisAdapter — to satisfy
postgres.Docker and redis.Docker respectively. They have to
be separate types because Go method sets can't carry two
RunContainer methods with different parameter types on the
same struct."
```

---

### Task 12: Final sanity + plan/checklist commit

**Files:**
- Modify: `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`

- [ ] **Step 1: Run the full backend test suite**

```bash
cd backend && go test ./...
```

Expected: all PASS — every package, including the existing iac/builder/credentials tests still passing alongside the new services/postgres + services/redis suites.

- [ ] **Step 2: `go vet` + `go build`**

```bash
cd backend && go vet ./...
cd backend && go build ./...
```

Expected: no output for either.

- [ ] **Step 3: Sanity-check the diff**

```bash
git diff --stat 6486106..HEAD
git log --oneline 6486106..HEAD
```

Expected:
- 11 commits (Tasks 1 through 11) on `feat/v2-plan-03a-service-plane-bootstrap`
- Files changed: backend/internal/credentials/store{,_test}.go, backend/internal/docker/client.go, backend/internal/services/postgres/{postgres,postgres_test}.go, backend/internal/services/redis/{redis,redis_test}.go, backend/internal/services/realdocker/realdocker.go, backend/cmd/server/main.go
- No changes outside `backend/`

- [ ] **Step 4: Update rollout checklist**

Replace the Plan 3 placeholder in `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` with:

```markdown
## Plan 3a — Service plane bootstrap (Postgres + Redis singletons)

After merge + redeploy:
- [ ] env-manager startup logs show `Service-plane: paas-postgres ready` AND `Service-plane: paas-redis ready`
- [ ] `docker network inspect paas-net` shows the bridge network exists
- [ ] `docker ps --filter name=paas-postgres` shows the container is running on `paas-net`, image `postgres:16`, with volume `paas_postgres_data` mounted
- [ ] `docker ps --filter name=paas-redis` shows the container is running on `paas-net`, image `redis:7`, with volume `paas_redis_data` mounted
- [ ] `docker exec paas-postgres pg_isready -U postgres` exits 0
- [ ] `docker exec paas-redis redis-cli -a $(superuser_pw_from_store) ping` returns `PONG`
- [ ] `cat /data/compose/16/data/.credentials/store.json | python3 -m json.tool` shows `system_secrets` keys: `system:paas-postgres:superuser`, `system:paas-redis:superuser` (both base64-encoded encrypted blobs)
- [ ] Restarting env-manager (`docker restart env-manager`) is a no-op — logs show "ready" without recreating containers; superuser passwords reused from cred-store
- [ ] `cd backend && go test ./...` passes locally
- [ ] No regression in other plans: stripe-payments builds still trigger via webhook; existing envs still serve

## Plan 3b — Per-env DB/ACL provisioning + URL injection
*(populated when plan 3b is written)*
```

(The previous "Plan 3 — Pre/post-deploy hooks" line moves to Plan 4. The placeholders for Plans 4-8 stay where they are, just shifted in numbering since 3 → 3a + 3b. Verify the existing file before editing.)

- [ ] **Step 5: Commit the plan + checklist**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-03a-service-plane-bootstrap.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 03a (service-plane bootstrap)

Plan document + Plan 3a entry in the rollout checklist.
Implementation lands in the preceding 11 commits on this branch.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: Push branch + open PR

- [ ] **Step 1: Push**

```bash
git push -u origin feat/v2-plan-03a-service-plane-bootstrap
```

- [ ] **Step 2: Open PR via gh**

```bash
gh pr create --title "v2 plan 03a: service-plane bootstrap (paas-postgres + paas-redis)" --body "$(cat <<'EOF'
## Summary

- Boots the singleton service plane on env-manager startup: `paas-net` bridge + `paas-postgres` (Postgres 16) + `paas-redis` (Redis 7)
- Adds two provisioner libraries (`internal/services/postgres`, `internal/services/redis`) with `Ensure*` and `Drop*` methods for per-environment DB / ACL lifecycle — **not yet wired into the runner** (that's Plan 3b)
- Cred-store gains a `SystemSecrets` API for storing the singletons' superuser passwords across reboots
- `docker.Client` gains `RunContainer`, `ContainerStatus`, `ExecCommand`, `EnsureBridgeNetwork` thin SDK wrappers
- All provisioner methods unit-tested via fake `Docker` interface (≈25 subtests across both packages)

## What ships in 3a

- env-manager boots → ensures paas-net → ensures paas-postgres + paas-redis are running on it (idempotent)
- Per-env DB + ACL provisioning code is **library-only** — no caller exists yet
- Superuser passwords generated on first boot, stored encrypted, reused on subsequent boots

## What's deferred to 3b

- Wiring `EnsureEnvDatabase` / `EnsureEnvACL` into the build runner (consumed when an env has `services.postgres: true` or `services.redis: true` in `.dev/config.yaml`)
- Injecting `DATABASE_URL` / `REDIS_URL` into the runner's `.env` generation
- Calling `DropEnvDatabase` / `DropEnvACL` on environment teardown

## Test plan

- [x] `cd backend && go test ./...` — full suite green
- [x] `cd backend && go vet ./...` — clean
- [x] `cd backend && go build ./...` — clean
- [x] No file outside `backend/` modified except the docs

After merge, manual home-lab verification per the rollout checklist.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report PR URL back to the user**

---

## Acceptance criteria

- [ ] `backend/internal/services/postgres/` and `backend/internal/services/redis/` packages exist and compile
- [ ] Each package exposes the four named methods (`EnsureService`, `EnsureEnvDatabase|ACL`, `DropEnvDatabase|ACL`)
- [ ] Cred-store `SaveSystemSecret`/`GetSystemSecret` round-trip works
- [ ] `docker.Client.RunContainer/ContainerStatus/ExecCommand/EnsureBridgeNetwork` defined
- [ ] `realdocker.PostgresAdapter` and `realdocker.RedisAdapter` satisfy `postgres.Docker` and `redis.Docker` respectively
- [ ] `cmd/server/main.go` calls both `EnsureService`s after cred-store init, before reconcile
- [ ] `go test ./...` clean, `go vet ./...` clean, `go build ./...` clean
- [ ] Branch `feat/v2-plan-03a-service-plane-bootstrap` is 12 commits ahead of master (11 implementation + 1 docs)
- [ ] PR opened with the test-plan checklist
- [ ] Rollout checklist updated for Plan 3a (and Plan 3b placeholder added below it)

## Out of scope (explicit)

- Per-env DB or ACL is provisioned at build time — Plan 3b
- `DATABASE_URL` / `REDIS_URL` injection into the runner's `.env` — Plan 3b
- Provisioner methods called from environment teardown — Plan 3b
- App containers join `paas-net` automatically — Plan 3b (via builder render injection)
- Domain conflict checking — Plan 5
- Hooks executor — Plan 4
- Migrating stripe-payments' `.dev/config.yaml` to declare `services` — Plan 8

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/` subdirectory.
- **Never use `> nul`, `> NUL`, or `> /dev/null`** — destructive on this Windows host.
- **Spec is canonical** — `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` overrides this plan if they conflict. Flag the discrepancy in your PR description.
- **TDD discipline:** every feat task → write failing test → run-fail → implement → run-pass → commit. Don't skip the run-fail step.
- **Commit cadence:** one commit per task (12 tasks → 12 commits including the docs commit). Don't squash. Don't amend. Create new commits when fixing review feedback.
- **Mock vs real Docker:** Tasks 4-10 deliberately use a fake `Docker` interface — the fakes are in each `*_test.go` file (`fakeDocker`, `fakeCreds`). The real-vs-fake parity is verified manually on the home-lab smoke test in the rollout checklist; don't try to add real-Docker integration tests in this plan (they belong to a future testing plan if at all).
- **Idempotency tests are the main value-add** — `EnsureService` re-run as noop, `EnsureEnvDatabase` swallowing "already exists", etc. These are the hard-to-debug-in-prod behaviours.
- **The `realdocker` package exposes two adapter types** (`PostgresAdapter`, `RedisAdapter`) instead of one because Go method sets can't carry two `RunContainer` methods with different parameter types on the same struct. Use `realdocker.NewPostgres(client)` and `realdocker.NewRedis(client)` constructors — both wrap the same underlying `*docker.Client`.
