# env-manager v2, Plan 3b — Runner-side service wiring + URL injection

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire Plan 3a's per-environment provisioner methods into the build runner so that an environment whose `.dev/config.yaml` declares `services.postgres: true` or `services.redis: true` gets a database / ACL user provisioned at build time, has its `DATABASE_URL` / `REDIS_URL` injected via `.env`, and joins `paas-net` so the app container can resolve `paas-postgres` / `paas-redis` over Docker DNS. Teardown drops the per-env DB / ACL and removes their cred-store password entries. **First consumer of `internal/iac` from Plan 2.**

**Architecture:** The runner parses `.dev/config.yaml` via `iac.Parse` at build and teardown time (best-effort: a missing or unparseable config logs a warning and skips service provisioning). When the parsed config declares services, the runner calls `postgres.Provisioner.EnsureEnvDatabase` / `redis.Provisioner.EnsureEnvACL` via newly-introduced `PostgresProvisioner` / `RedisProvisioner` interfaces. Those interfaces let tests substitute fakes; production injection wires the real `*postgres.Provisioner` / `*redis.Provisioner` from main. URL construction is centralised on the provisioner types — `EnsureEnvDatabase` / `EnsureEnvACL` are extended to also return the connection URL alongside the existing `*EnvDatabase` / `*EnvACL` value (URL field added to those structs). A new `InjectPaasNet` pass mirrors the existing `InjectTraefikLabels` shape: rewrite the compose YAML so each service has `paas-net` in its `networks:` and the top-level `networks:` declares `paas-net: { external: true }`.

**What works after this plan ships:**
- Project's `.dev/config.yaml` declaring `services.postgres: true` → fresh push provisions `<slug>_<branch>` DB + user inside `paas-postgres`, writes `DATABASE_URL=postgres://...` to `.env`, attaches `paas-net` to the app container
- Same for Redis with `services.redis: true`
- Branch deletion drops the DB / ACL + cred-store password entry
- Projects without `.dev/config.yaml` (or with a v1 schema config) keep working — provisioning is a no-op for them

**Tech Stack:** Go 1.24, no new dependencies. Existing packages: `internal/iac` (Plan 2), `internal/services/{postgres,redis,realdocker}` (Plan 3a), `internal/builder`, `internal/credentials`.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Lifecycle flows → Flow C", "Per-env database semantics", "Per-env Redis semantics", "Injected env vars", and "Implementation decomposition" row 3 (split into 3a + 3b).

---

## File structure after this plan

**Modified files:**

```
backend/internal/services/postgres/postgres.go        — EnvDatabase gains URL field; EnsureEnvDatabase populates it
backend/internal/services/postgres/postgres_test.go   — assert URL field
backend/internal/services/redis/redis.go              — EnvACL gains URL field; EnsureEnvACL populates it
backend/internal/services/redis/redis_test.go         — assert URL field
backend/internal/builder/runner.go                    — interfaces + SetServiceProvisioners + Build integration + Teardown integration
backend/internal/builder/runner_test.go               — fake provisioners + integration tests
backend/cmd/server/main.go                            — hold dockerCli; runner.SetServiceProvisioners
```

**New files:**

```
backend/internal/builder/networks.go                  — InjectPaasNet (yaml network injection)
backend/internal/builder/networks_test.go             — table-driven tests for InjectPaasNet
```

**Files unchanged:** `internal/iac/*` (consumed via `iac.Parse`), `internal/services/realdocker/*`, all handlers.

---

## Naming & locked details

| Thing | Value |
|---|---|
| Network attached to apps | `paas-net` |
| Postgres connection string | `postgres://<user>:<pw>@paas-postgres:5432/<db>?sslmode=disable` |
| Redis connection string | `redis://<user>:<pw>@paas-redis:6379/0` |
| Env var name for DB URL | `DATABASE_URL` |
| Env var name for Redis URL | `REDIS_URL` |
| Cred-store keys cleaned up on teardown | `env:<env-id>:db_password`, `env:<env-id>:redis_password` (project-secret slot keyed by env ID) |
| `.env` write target | `<project.LocalPath>/.env` (existing path; URL vars merged into the secrets map) |
| Behaviour when `.dev/config.yaml` absent / unparseable | log warning, skip provisioning, continue build |
| Behaviour when `services.postgres: false` / unset | no provisioner call, no URL written, no paas-net injection |

---

## Tasks

### Task 1: Branch + EnvDatabase/EnvACL gain URL field

**Files:**
- Modify: `backend/internal/services/postgres/postgres.go`
- Modify: `backend/internal/services/postgres/postgres_test.go`
- Modify: `backend/internal/services/redis/redis.go`
- Modify: `backend/internal/services/redis/redis_test.go`

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status
git rev-parse HEAD
```

Expected: clean working tree (untracked OK); HEAD at `9eb070c` (Plan 3a merge) or later.

```bash
git checkout -b feat/v2-plan-03b-runner-services-wiring
```

- [ ] **Step 2: Write the failing tests for postgres URL**

Append to `backend/internal/services/postgres/postgres_test.go`:

```go
func TestEnsureEnvDatabase_PopulatesURL(t *testing.T) {
	fd := &fakeDocker{
		statuses: map[string]containerState{
			ContainerName: {exists: true, running: true},
		},
		execResults: []execResult{{exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvDatabase(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvDatabase: %v", err)
	}
	wantPrefix := "postgres://stripepayments_main:"
	wantSuffix := "@paas-postgres:5432/stripepayments_main?sslmode=disable"
	if got.URL == "" {
		t.Fatalf("URL empty")
	}
	if !strings.HasPrefix(got.URL, wantPrefix) {
		t.Errorf("URL prefix wrong: got %q want prefix %q", got.URL, wantPrefix)
	}
	if !strings.HasSuffix(got.URL, wantSuffix) {
		t.Errorf("URL suffix wrong: got %q want suffix %q", got.URL, wantSuffix)
	}
	// URL embeds the password verbatim — verify it matches the stored value.
	stored, _ := fc.GetProjectSecret("envid-abc", "db_password")
	if !strings.Contains(got.URL, stored) {
		t.Errorf("URL doesn't contain stored password")
	}
}
```

- [ ] **Step 3: Run test (expect failure: URL field doesn't exist)**

```bash
cd backend && go test ./internal/services/postgres/... -run TestEnsureEnvDatabase_PopulatesURL -v
```

Expected: compile error — `got.URL undefined`.

- [ ] **Step 4: Add URL field + populate in `postgres.go`**

Modify the `EnvDatabase` struct (around line 60) to add:

```go
type EnvDatabase struct {
	DatabaseName string
	Username     string
	PasswordKey  string
	URL          string // postgres://user:pw@paas-postgres:5432/db?sslmode=disable
}
```

In `EnsureEnvDatabase`, after the GRANT block and BEFORE the `return &EnvDatabase{...}` statement, build the URL. Replace the final return with:

```go
	return &EnvDatabase{
		DatabaseName: dbName,
		Username:     dbName,
		PasswordKey:  pwStoreKey,
		URL: fmt.Sprintf(
			"postgres://%s:%s@%s:5432/%s?sslmode=disable",
			dbName, password, ContainerName, dbName,
		),
	}, nil
```

(Reason for inlining `password` into the URL: `%s` formatting of a 48-char hex string is URL-safe — no special characters need encoding.)

- [ ] **Step 5: Run all postgres tests**

```bash
cd backend && go test ./internal/services/postgres/... -v
```

Expected: all PASS, including the new URL test.

- [ ] **Step 6: Repeat for redis — write the failing test**

Append to `backend/internal/services/redis/redis_test.go`:

```go
func TestEnsureEnvACL_PopulatesURL(t *testing.T) {
	fd := &fakeDocker{
		statuses:    map[string]containerState{ContainerName: {exists: true, running: true}},
		execResults: []execResult{{stdout: "OK", exitCode: 0}},
	}
	fc := newFakeCreds()
	_ = fc.SaveSystemSecret(SuperuserKey, "super-pw")
	p := newTestProvisioner(t, fd, fc)

	got, err := p.EnsureEnvACL(context.Background(), "envid-abc", "stripe-payments", "main")
	if err != nil {
		t.Fatalf("EnsureEnvACL: %v", err)
	}
	wantPrefix := "redis://stripepayments_main:"
	wantSuffix := "@paas-redis:6379/0"
	if got.URL == "" {
		t.Fatalf("URL empty")
	}
	if !strings.HasPrefix(got.URL, wantPrefix) {
		t.Errorf("URL prefix wrong: got %q want prefix %q", got.URL, wantPrefix)
	}
	if !strings.HasSuffix(got.URL, wantSuffix) {
		t.Errorf("URL suffix wrong: got %q want suffix %q", got.URL, wantSuffix)
	}
	stored, _ := fc.GetProjectSecret("envid-abc", "redis_password")
	if !strings.Contains(got.URL, stored) {
		t.Errorf("URL doesn't contain stored password")
	}
}
```

- [ ] **Step 7: Run test (expect failure)**

```bash
cd backend && go test ./internal/services/redis/... -run TestEnsureEnvACL_PopulatesURL -v
```

Expected: compile error — `got.URL undefined`.

- [ ] **Step 8: Add URL field + populate in `redis.go`**

Modify the `EnvACL` struct (around line 70):

```go
type EnvACL struct {
	Username    string
	KeyPrefix   string
	PasswordKey string
	URL         string // redis://user:pw@paas-redis:6379/0
}
```

In `EnsureEnvACL`, replace the final return with:

```go
	return &EnvACL{
		Username:    user,
		KeyPrefix:   prefix,
		PasswordKey: pwStoreKey,
		URL:         fmt.Sprintf("redis://%s:%s@%s:6379/0", user, password, ContainerName),
	}, nil
```

- [ ] **Step 9: Run all redis tests**

```bash
cd backend && go test ./internal/services/redis/... -v
```

Expected: all PASS.

- [ ] **Step 10: Run full backend suite (sanity)**

```bash
cd backend && go test ./...
```

Expected: all PASS — no other packages affected.

- [ ] **Step 11: Commit**

```bash
git add backend/internal/services/postgres/postgres.go backend/internal/services/postgres/postgres_test.go backend/internal/services/redis/redis.go backend/internal/services/redis/redis_test.go
git commit -m "feat(services): EnsureEnv* return connection URL alongside env metadata

Adds URL field to EnvDatabase and EnvACL — populated at the
end of EnsureEnvDatabase/EnsureEnvACL using the just-resolved
password and the well-known singleton container hostnames.

Plan 3b's runner integration consumes these URLs to write
DATABASE_URL/REDIS_URL into the per-env .env file."
```

---

### Task 2: Builder interfaces + Runner.SetServiceProvisioners

**Files:**
- Modify: `backend/internal/builder/runner.go`

This task only adds the provisioner-injection seam; it doesn't yet call the provisioners. That happens in Task 4.

- [ ] **Step 1: Add interfaces + SetServiceProvisioners in `runner.go`**

Insert after the `ComposeExecutor` interface declaration (around line 28) but before `DockerComposeExecutor`:

```go
// PostgresProvisioner is the runner-facing surface of services/postgres.
// Real implementation: *postgres.Provisioner. Tests: in-memory fake.
type PostgresProvisioner interface {
	EnsureEnvDatabase(ctx context.Context, envID, projectName, branchSlug string) (*PostgresEnvDatabase, error)
	DropEnvDatabase(ctx context.Context, projectName, branchSlug string) error
}

// RedisProvisioner is the runner-facing surface of services/redis.
// Real implementation: *redis.Provisioner. Tests: in-memory fake.
type RedisProvisioner interface {
	EnsureEnvACL(ctx context.Context, envID, projectName, branchSlug string) (*RedisEnvACL, error)
	DropEnvACL(ctx context.Context, projectName, branchSlug string) error
}

// PostgresEnvDatabase mirrors postgres.EnvDatabase. Defined locally so the
// builder package doesn't import services/postgres directly (the import
// would cause a cycle once the runner is consumed by services tests). Same
// shape, distinct nominal type — adapters bridge the two.
type PostgresEnvDatabase struct {
	DatabaseName string
	Username     string
	PasswordKey  string
	URL          string
}

// RedisEnvACL mirrors redis.EnvACL.
type RedisEnvACL struct {
	Username    string
	KeyPrefix   string
	PasswordKey string
	URL         string
}
```

(There is no current import cycle — `internal/services/{postgres,redis}` do not import `internal/builder`. The local redeclaration is defensive: it keeps the builder free of direct service-package imports so future service additions don't risk one. Two thin adapters in main.go translate between the two type families — written in Task 6.)

Add fields to the `Runner` struct (around line 44):

```go
type Runner struct {
	store        *projects.Store
	exec         ComposeExecutor
	dataDir      string
	proxyNetwork string
	queue        *Queue
	logger       *zap.Logger
	logRing      int
	credStore    *credentials.Store
	postgres     PostgresProvisioner // nil = postgres provisioning disabled
	redis        RedisProvisioner    // nil = redis provisioning disabled
}
```

Add the setter at the end of the file (after the `fail` helper):

```go
// SetServiceProvisioners wires the per-env service provisioners. Either or
// both may be nil; nil disables provisioning for that service. Safe to call
// before serving but not concurrently with Build/Teardown.
func (r *Runner) SetServiceProvisioners(pg PostgresProvisioner, rd RedisProvisioner) {
	r.postgres = pg
	r.redis = rd
}
```

- [ ] **Step 2: Verify the package builds**

```bash
cd backend && go build ./internal/builder/...
```

Expected: clean.

- [ ] **Step 3: Run existing builder tests**

```bash
cd backend && go test ./internal/builder/...
```

Expected: all PASS — no behaviour change yet, the new types are unreferenced.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/builder/runner.go
git commit -m "feat(builder): provisioner interfaces + SetServiceProvisioners seam

Defines PostgresProvisioner/RedisProvisioner interfaces and
locally-shaped PostgresEnvDatabase/RedisEnvACL types so the
builder package stays free of services/* imports. Adds nilable
provisioner fields on Runner with a setter; no callers yet —
Task 4 wires Build, Task 5 wires Teardown."
```

---

### Task 3: `InjectPaasNet` function

**Files:**
- Create: `backend/internal/builder/networks.go`
- Create: `backend/internal/builder/networks_test.go`

The yaml-mutation helpers `labelsEnsureNetworkOnService` and `labelsEnsureExternalNetwork` already exist in `labels.go` (lines 234-277). They're package-private but reusable. We'll call them from a new `InjectPaasNet` wrapper.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/builder/networks_test.go`:

```go
package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNetworksTestCompose(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "docker-compose.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func readNetworksTestCompose(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func TestInjectPaasNet_EmptyNetworkIsNoop(t *testing.T) {
	dir := t.TempDir()
	original := "services:\n  app:\n    image: alpine\n"
	path := writeNetworksTestCompose(t, dir, original)
	if err := InjectPaasNet(path, ""); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := readNetworksTestCompose(t, path); got != original {
		t.Errorf("file modified for empty-network input:\ngot: %s\nwant: %s", got, original)
	}
}

func TestInjectPaasNet_AddsNetworkToService(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatalf("InjectPaasNet: %v", err)
	}
	out := readNetworksTestCompose(t, path)
	if !strings.Contains(out, "paas-net") {
		t.Errorf("expected paas-net in compose, got:\n%s", out)
	}
	if !strings.Contains(out, "external: true") && !strings.Contains(out, "external: \"true\"") {
		t.Errorf("expected external network declaration, got:\n%s", out)
	}
}

func TestInjectPaasNet_PreservesExistingNetworks(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  app:
    image: alpine
    networks:
      - default
      - my-other-net
`
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	out := readNetworksTestCompose(t, path)
	for _, want := range []string{"default", "my-other-net", "paas-net"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q after injection:\n%s", want, out)
		}
	}
}

func TestInjectPaasNet_Idempotent(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	first := readNetworksTestCompose(t, path)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	second := readNetworksTestCompose(t, path)
	if first != second {
		t.Errorf("second injection changed file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInjectPaasNet_AddsToAllServices(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  app:
    image: alpine
  worker:
    image: busybox
`
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	out := readNetworksTestCompose(t, path)
	// Both services should reference paas-net. We check by counting occurrences:
	// the literal "paas-net" should appear at least twice for the services and
	// once more for the top-level networks: declaration.
	count := strings.Count(out, "paas-net")
	if count < 3 {
		t.Errorf("expected paas-net to appear in 2 services + 1 top-level (≥3 occurrences), got %d:\n%s", count, out)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run TestInjectPaasNet -v
```

Expected: compile error — `undefined: InjectPaasNet`.

- [ ] **Step 3: Implement `networks.go`**

Create `backend/internal/builder/networks.go`:

```go
package builder

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// InjectPaasNet rewrites the compose file at composePath so every service
// has `network` listed in its `networks:` and the top-level `networks:`
// declares it as external.
//
// Idempotent: re-running on an already-injected file is a no-op. Empty
// network = noop (the function returns nil without touching the file),
// matching InjectTraefikLabels' bypass behaviour.
//
// Reuses the package-private yaml helpers from labels.go
// (labelsEnsureNetworkOnService, labelsEnsureExternalNetwork).
func InjectPaasNet(composePath string, network string) error {
	if network == "" {
		return nil
	}
	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("read compose: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse compose YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("compose YAML is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("compose YAML root is not a mapping")
	}

	services := labelsFindMapValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return fmt.Errorf("compose YAML has no services mapping")
	}

	// Add paas-net to every service.
	for i := 0; i+1 < len(services.Content); i += 2 {
		svc := services.Content[i+1]
		if svc == nil || svc.Kind != yaml.MappingNode {
			continue
		}
		labelsEnsureNetworkOnService(svc, network)
	}

	// Top-level networks: <network>: { external: true }
	labelsEnsureExternalNetwork(root, network)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal compose YAML: %w", err)
	}
	return os.WriteFile(composePath, out, 0644)
}
```

- [ ] **Step 4: Run all builder tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS — both new and pre-existing builder tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/networks.go backend/internal/builder/networks_test.go
git commit -m "feat(builder): InjectPaasNet pass attaches paas-net to compose services

Mirrors the InjectTraefikLabels pattern — read compose, mutate
each service's networks: list to include paas-net, declare
paas-net: { external: true } at the top, write back. Reuses
the package-private labelsEnsure* yaml helpers from labels.go.
Idempotent and noops on empty network input."
```

---

### Task 4: Runner.Build wires service provisioning + URL injection

**Files:**
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go`

This is the central integration. The runner now:
1. Parses `.dev/config.yaml` via `iac.Parse`. Missing or unparseable → log warning, skip provisioning.
2. If `Config.Services.Postgres` AND `r.postgres != nil` → call `EnsureEnvDatabase`, capture URL.
3. If `Config.Services.Redis` AND `r.redis != nil` → call `EnsureEnvACL`, capture URL.
4. Merge URLs into the secrets map written to `.env`.
5. After `RenderCompose` + `InjectTraefikLabels`, call `InjectPaasNet` if EITHER service was provisioned.

- [ ] **Step 1: Write the failing test (fake provisioners + fixture iac config)**

Append to `backend/internal/builder/runner_test.go`:

```go
// fakePostgres / fakeRedis implement the runner's provisioner interfaces.
type fakePostgres struct {
	ensureCalls []string // env IDs ensured
	dropCalls   []string // "<project>/<branch>" entries
	url         string
	username    string
	dbName      string
	ensureErr   error
}

func (f *fakePostgres) EnsureEnvDatabase(_ context.Context, envID, projectName, branchSlug string) (*PostgresEnvDatabase, error) {
	f.ensureCalls = append(f.ensureCalls, envID)
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	url := f.url
	if url == "" {
		url = "postgres://stripepayments_main:fakepw@paas-postgres:5432/stripepayments_main?sslmode=disable"
	}
	user := f.username
	if user == "" {
		user = "stripepayments_main"
	}
	db := f.dbName
	if db == "" {
		db = "stripepayments_main"
	}
	return &PostgresEnvDatabase{
		DatabaseName: db,
		Username:     user,
		PasswordKey:  "env:" + envID + ":db_password",
		URL:          url,
	}, nil
}

func (f *fakePostgres) DropEnvDatabase(_ context.Context, projectName, branchSlug string) error {
	f.dropCalls = append(f.dropCalls, projectName+"/"+branchSlug)
	return nil
}

type fakeRedis struct {
	ensureCalls []string
	dropCalls   []string
	url         string
}

func (f *fakeRedis) EnsureEnvACL(_ context.Context, envID, projectName, branchSlug string) (*RedisEnvACL, error) {
	f.ensureCalls = append(f.ensureCalls, envID)
	url := f.url
	if url == "" {
		url = "redis://stripepayments_main:fakepw@paas-redis:6379/0"
	}
	return &RedisEnvACL{
		Username:    "stripepayments_main",
		KeyPrefix:   "stripe-payments:main",
		PasswordKey: "env:" + envID + ":redis_password",
		URL:         url,
	}, nil
}

func (f *fakeRedis) DropEnvACL(_ context.Context, projectName, branchSlug string) error {
	f.dropCalls = append(f.dropCalls, projectName+"/"+branchSlug)
	return nil
}

func TestRunner_Build_ServicesProvisioning(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)

	// Add a v2 .dev/config.yaml declaring services.
	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Set up cred-store + fake provisioners.
	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i + 7)
	}
	credStore, err := credentials.NewStore(filepath.Join(dataDir, "creds.json"), credKey)
	if err != nil {
		t.Fatal(err)
	}
	pg := &fakePostgres{}
	rd := &fakeRedis{}
	// Re-construct runner with credStore + provisioners.
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), credStore)
	r2.SetServiceProvisioners(pg, rd)
	_ = r // keep newRunnerTest's runner alive; we use r2

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pg.ensureCalls) != 1 || pg.ensureCalls[0] != env.ID {
		t.Errorf("postgres EnsureEnvDatabase calls: %v want [%s]", pg.ensureCalls, env.ID)
	}
	if len(rd.ensureCalls) != 1 || rd.ensureCalls[0] != env.ID {
		t.Errorf("redis EnsureEnvACL calls: %v want [%s]", rd.ensureCalls, env.ID)
	}

	// .env should contain DATABASE_URL + REDIS_URL.
	envPath := filepath.Join(project.LocalPath, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf(".env not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "DATABASE_URL=postgres://") {
		t.Errorf(".env missing DATABASE_URL; got:\n%s", content)
	}
	if !strings.Contains(content, "REDIS_URL=redis://") {
		t.Errorf(".env missing REDIS_URL; got:\n%s", content)
	}

	// The rendered compose should have paas-net attached to the service.
	composePath := filepath.Join(dataDir, "envs", env.ID, "docker-compose.yaml")
	composeData, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("compose not rendered: %v", err)
	}
	if !strings.Contains(string(composeData), "paas-net") {
		t.Errorf("rendered compose missing paas-net:\n%s", composeData)
	}
}

func TestRunner_Build_NoServicesDeclared_NoProvisioning(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r // unused

	// Write a v2 config with services explicitly false.
	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: false
  redis: false
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pg.ensureCalls) != 0 {
		t.Errorf("expected no postgres provisioning, got %d calls", len(pg.ensureCalls))
	}
	if len(rd.ensureCalls) != 0 {
		t.Errorf("expected no redis provisioning, got %d calls", len(rd.ensureCalls))
	}
}

func TestRunner_Build_NoIacConfig_NoProvisioning(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)
	_ = r
	// Don't write .dev/config.yaml — newRunnerTest skipped it. The runner
	// must treat the missing file as "no services declared" and continue.

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build should succeed without iac config, got %v", err)
	}
	if len(pg.ensureCalls) != 0 {
		t.Errorf("expected no postgres provisioning when iac absent, got %d calls", len(pg.ensureCalls))
	}
	if len(rd.ensureCalls) != 0 {
		t.Errorf("expected no redis provisioning when iac absent, got %d calls", len(rd.ensureCalls))
	}
}

func TestRunner_Build_NilProvisioner_NoProvisioning(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Don't call SetServiceProvisioners — both nil.
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
	// Build must not have crashed despite iac declaring services and provisioners being nil.
	gotEnv, _ := store.GetEnvironment(env.ProjectID, env.BranchSlug)
	if gotEnv.Status != models.EnvStatusRunning {
		t.Errorf("env status = %v, want running", gotEnv.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run "TestRunner_Build_Services|TestRunner_Build_No|TestRunner_Build_Nil" -v
```

Expected: at minimum the `_ServicesProvisioning` test fails (provisioners not yet called). Other tests may pass or fail depending on prior runner behaviour.

- [ ] **Step 3: Implement in `runner.go`**

Add the iac import at the top:

```go
import (
	// ... existing imports ...
	"github.com/environment-manager/backend/internal/iac"
)
```

Inside `Runner.Build`, BEFORE the existing secrets-writing block (around line 109), add provisioning + URL capture:

```go
	// --- Plan 3b: parse iac config + provision services ---------------------
	// Best-effort: missing/unparseable config → continue without service URLs.
	servicesURLs := map[string]string{} // DATABASE_URL / REDIS_URL — merged into .env below
	attachPaasNet := false              // toggled true if any service was provisioned

	iacPath := filepath.Join(project.LocalPath, ".dev", "config.yaml")
	if iacBytes, err := os.ReadFile(iacPath); err != nil {
		_, _ = log.Write([]byte("==> no .dev/config.yaml — skipping service provisioning\n"))
	} else if cfg, perr := iac.Parse(iacBytes); perr != nil {
		_, _ = log.Write([]byte("WARNING: .dev/config.yaml parse failed; skipping service provisioning: " + perr.Error() + "\n"))
	} else {
		if cfg.Services.Postgres && r.postgres != nil {
			_, _ = log.Write([]byte("==> provisioning postgres database\n"))
			db, perr := r.postgres.EnsureEnvDatabase(ctx, env.ID, project.Name, env.BranchSlug)
			if perr != nil {
				_, _ = log.Write([]byte("ERROR: postgres provisioning failed: " + perr.Error() + "\n"))
				return r.fail(env, b, "postgres provisioning: "+perr.Error())
			}
			servicesURLs["DATABASE_URL"] = db.URL
			attachPaasNet = true
		}
		if cfg.Services.Redis && r.redis != nil {
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
	// ------------------------------------------------------------------------
```

Now modify the existing secrets-writing block (around line 112) to merge `servicesURLs` into `secrets`:

```go
	if r.credStore != nil {
		secrets, err := r.credStore.GetProjectSecrets(project.ID)
		if err != nil {
			_, _ = log.Write([]byte("WARNING: failed to load project secrets: " + err.Error() + "\n"))
			secrets = map[string]string{}
		}
		// Merge auto-generated service URLs into the secrets map so the
		// downstream sort+write loop handles them uniformly.
		for k, v := range servicesURLs {
			secrets[k] = v
		}
		if len(secrets) > 0 {
			envPath := filepath.Join(project.LocalPath, ".env")
			var sb strings.Builder
			sb.WriteString("# Generated by env-manager from credential store. DO NOT EDIT.\n")
			keys := make([]string, 0, len(secrets))
			for k := range secrets {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString(k)
				sb.WriteString("=")
				sb.WriteString(secrets[k])
				sb.WriteString("\n")
			}
			if err := os.WriteFile(envPath, []byte(sb.String()), 0600); err != nil {
				_, _ = log.Write([]byte("WARNING: failed to write .env: " + err.Error() + "\n"))
			} else {
				_, _ = log.Write([]byte("==> wrote " + fmt.Sprintf("%d", len(secrets)) + " env entries to .env\n"))
			}
		}
	} else if len(servicesURLs) > 0 {
		// credStore is nil but service URLs were generated — still write them.
		envPath := filepath.Join(project.LocalPath, ".env")
		var sb strings.Builder
		sb.WriteString("# Generated by env-manager (service URLs only). DO NOT EDIT.\n")
		keys := make([]string, 0, len(servicesURLs))
		for k := range servicesURLs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(k + "=" + servicesURLs[k] + "\n")
		}
		_ = os.WriteFile(envPath, []byte(sb.String()), 0600)
	}
```

Finally, after the existing `InjectTraefikLabels` block, add the paas-net injection:

```go
	if attachPaasNet {
		_, _ = log.Write([]byte("==> attaching paas-net\n"))
		if err := InjectPaasNet(composePath, "paas-net"); err != nil {
			_, _ = log.Write([]byte("ERROR: " + err.Error() + "\n"))
			return r.fail(env, b, "inject paas-net: "+err.Error())
		}
	}
```

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/runner.go backend/internal/builder/runner_test.go
git commit -m "feat(builder): runner provisions services + writes URLs at build time

Build now parses .dev/config.yaml via iac.Parse (best-effort —
missing/unparseable falls back to v1 behaviour). When the
parsed config declares services.postgres or services.redis AND
the corresponding provisioner was wired via
SetServiceProvisioners, the runner calls EnsureEnvDatabase /
EnsureEnvACL, captures the connection URL, and merges it into
the secrets map written to .env. After RenderCompose +
InjectTraefikLabels, the runner calls InjectPaasNet so the app
container can resolve paas-postgres / paas-redis via Docker DNS."
```

---

### Task 5: Runner.Teardown drops services + cleans cred-store entries

**Files:**
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `runner_test.go`:

```go
func TestRunner_Teardown_DropsServices(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-create an envDir so the existing teardown logic finds something to remove.
	envDir := filepath.Join(dataDir, "envs", env.ID)
	if err := os.MkdirAll(envDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"), []byte("services: {}\n"), 0644)

	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i + 9)
	}
	credStore, err := credentials.NewStore(filepath.Join(dataDir, "creds.json"), credKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = credStore.SaveProjectSecret(env.ID, "db_password", "the-db-pw")
	_ = credStore.SaveProjectSecret(env.ID, "redis_password", "the-redis-pw")

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), credStore)
	r2.SetServiceProvisioners(pg, rd)

	if err := r2.Teardown(context.Background(), env); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	want := project.Name + "/" + env.BranchSlug
	if len(pg.dropCalls) != 1 || pg.dropCalls[0] != want {
		t.Errorf("postgres DropEnvDatabase calls = %v, want [%s]", pg.dropCalls, want)
	}
	if len(rd.dropCalls) != 1 || rd.dropCalls[0] != want {
		t.Errorf("redis DropEnvACL calls = %v, want [%s]", rd.dropCalls, want)
	}
	// Cred-store entries should be gone.
	if _, err := credStore.GetProjectSecret(env.ID, "db_password"); err == nil {
		t.Errorf("expected db_password removed from cred-store")
	}
	if _, err := credStore.GetProjectSecret(env.ID, "redis_password"); err == nil {
		t.Errorf("expected redis_password removed from cred-store")
	}
}

func TestRunner_Teardown_NoServicesDeclared_NoDrop(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	envDir := filepath.Join(dataDir, "envs", env.ID)
	_ = os.MkdirAll(envDir, 0755)
	_ = os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"), []byte("services: {}\n"), 0644)

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	if err := r2.Teardown(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(pg.dropCalls) != 0 || len(rd.dropCalls) != 0 {
		t.Errorf("expected zero drop calls, got pg=%v rd=%v", pg.dropCalls, rd.dropCalls)
	}
}

func TestRunner_Teardown_IacAbsent_NoDrop(t *testing.T) {
	r, store, _, env, dataDir, exec := newRunnerTest(t)
	_ = r
	// No .dev/config.yaml.

	envDir := filepath.Join(dataDir, "envs", env.ID)
	_ = os.MkdirAll(envDir, 0755)
	_ = os.WriteFile(filepath.Join(envDir, "docker-compose.yaml"), []byte("services: {}\n"), 0644)

	pg := &fakePostgres{}
	rd := &fakeRedis{}
	r2 := NewRunner(store, exec, dataDir, "", NewQueue(), zap.NewNop(), nil)
	r2.SetServiceProvisioners(pg, rd)

	if err := r2.Teardown(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(pg.dropCalls) != 0 || len(rd.dropCalls) != 0 {
		t.Errorf("expected zero drop calls when iac absent, got pg=%v rd=%v", pg.dropCalls, rd.dropCalls)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run TestRunner_Teardown -v
```

Expected: `_DropsServices` fails (no DropEnv* calls happen yet).

- [ ] **Step 3: Implement teardown integration in `runner.go`**

In `Runner.Teardown`, BEFORE the existing `os.RemoveAll(envDir)` block, add:

```go
	// --- Plan 3b: drop services + clean cred-store entries -----------------
	// Best-effort. Failures are logged but don't abort directory cleanup.
	project, err := r.store.GetProject(env.ProjectID)
	if err == nil {
		iacPath := filepath.Join(project.LocalPath, ".dev", "config.yaml")
		if iacBytes, ferr := os.ReadFile(iacPath); ferr == nil {
			if cfg, perr := iac.Parse(iacBytes); perr == nil {
				if cfg.Services.Postgres && r.postgres != nil {
					if derr := r.postgres.DropEnvDatabase(ctx, project.Name, env.BranchSlug); derr != nil {
						r.logger.Warn("DropEnvDatabase failed",
							zap.String("env_id", env.ID), zap.Error(derr))
					}
				}
				if cfg.Services.Redis && r.redis != nil {
					if derr := r.redis.DropEnvACL(ctx, project.Name, env.BranchSlug); derr != nil {
						r.logger.Warn("DropEnvACL failed",
							zap.String("env_id", env.ID), zap.Error(derr))
					}
				}
			}
		}
	}
	// Remove per-env cred-store password entries unconditionally — safe even
	// when the keys never existed (DeleteProjectSecret returns ErrNotFound,
	// which we ignore).
	if r.credStore != nil {
		_ = r.credStore.DeleteProjectSecret(env.ID, "db_password")
		_ = r.credStore.DeleteProjectSecret(env.ID, "redis_password")
	}
	// ----------------------------------------------------------------------
```

- [ ] **Step 4: Run all builder tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/runner.go backend/internal/builder/runner_test.go
git commit -m "feat(builder): runner drops services + cleans cred-store on teardown

Teardown now parses .dev/config.yaml (best-effort) and calls
DropEnvDatabase / DropEnvACL when the corresponding service was
declared. Per-env password entries (env:<id>:db_password and
env:<id>:redis_password) are unconditionally removed from the
credential store — DeleteProjectSecret on a missing key is a
benign ErrNotFound that we ignore. Failures are logged but
don't abort directory cleanup."
```

---

### Task 6: Wire provisioners into `cmd/server/main.go`

**Files:**
- Modify: `backend/cmd/server/main.go`

The Plan 3a Flow G block creates `dockerCli` and closes it after bootstrap. Plan 3b needs the same client alive for the lifetime of the process so the runner's provisioners can use it. We restructure so `dockerCli` is created once at startup, used for both bootstrap and runner, and closed at shutdown.

We also need provisioner adapters: `postgres.Provisioner` and `redis.Provisioner` return their own `*EnvDatabase` / `*EnvACL` types, but the runner's interfaces expect `*PostgresEnvDatabase` / `*RedisEnvACL` (locally redeclared in builder). Two thin adapter structs in main.go translate.

- [ ] **Step 1: Restructure the bootstrap block**

In `backend/cmd/server/main.go`, replace the existing service-plane bootstrap block with the following. The new structure: create `dockerCli` once, defer `Close()`, run bootstrap inline using direct provisioner refs, then later attach those same provisioners to the runner via adapters.

Find the existing block:

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

Replace with:

```go
	// Service-plane bootstrap + long-lived provisioners (Flow G + Plan 3b wiring).
	// dockerCli stays alive for the lifetime of the process so the runner's
	// provisioners can reuse it.
	var pgProvisioner *postgres.Provisioner
	var rdProvisioner *redis.Provisioner
	if credStore == nil {
		logger.Warn("Service-plane skipped: credential store unavailable")
	} else {
		dockerCli, err := docker.NewClient()
		if err != nil {
			logger.Error("Service-plane: docker client init failed", zap.Error(err))
		} else {
			defer func() { _ = dockerCli.Close() }()
			pgProvisioner = postgres.New(realdocker.NewPostgres(dockerCli), credStore, logger)
			rdProvisioner = redis.New(realdocker.NewRedis(dockerCli), credStore, logger)

			bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			if err := pgProvisioner.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: postgres failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-postgres ready")
			}
			if err := rdProvisioner.EnsureService(bootstrapCtx); err != nil {
				logger.Error("Service-plane bootstrap: redis failed", zap.Error(err))
			} else {
				logger.Info("Service-plane: paas-redis ready")
			}
			bootstrapCancel()
		}
	}
```

- [ ] **Step 2: Add provisioner adapters at the bottom of `main.go`**

Append to `main.go` after the existing `reconcileSpawner` definitions:

```go
// pgRunnerAdapter bridges *postgres.Provisioner to builder.PostgresProvisioner.
type pgRunnerAdapter struct{ p *postgres.Provisioner }

func (a *pgRunnerAdapter) EnsureEnvDatabase(ctx context.Context, envID, projectName, branchSlug string) (*builder.PostgresEnvDatabase, error) {
	db, err := a.p.EnsureEnvDatabase(ctx, envID, projectName, branchSlug)
	if err != nil {
		return nil, err
	}
	return &builder.PostgresEnvDatabase{
		DatabaseName: db.DatabaseName,
		Username:     db.Username,
		PasswordKey:  db.PasswordKey,
		URL:          db.URL,
	}, nil
}
func (a *pgRunnerAdapter) DropEnvDatabase(ctx context.Context, projectName, branchSlug string) error {
	return a.p.DropEnvDatabase(ctx, projectName, branchSlug)
}

// rdRunnerAdapter bridges *redis.Provisioner to builder.RedisProvisioner.
type rdRunnerAdapter struct{ p *redis.Provisioner }

func (a *rdRunnerAdapter) EnsureEnvACL(ctx context.Context, envID, projectName, branchSlug string) (*builder.RedisEnvACL, error) {
	acl, err := a.p.EnsureEnvACL(ctx, envID, projectName, branchSlug)
	if err != nil {
		return nil, err
	}
	return &builder.RedisEnvACL{
		Username:    acl.Username,
		KeyPrefix:   acl.KeyPrefix,
		PasswordKey: acl.PasswordKey,
		URL:         acl.URL,
	}, nil
}
func (a *rdRunnerAdapter) DropEnvACL(ctx context.Context, projectName, branchSlug string) error {
	return a.p.DropEnvACL(ctx, projectName, branchSlug)
}
```

- [ ] **Step 3: Wire the adapters into the runner**

After the line `buildRunner := builder.NewRunner(...)` (around the existing line 67), add:

```go
	if pgProvisioner != nil && rdProvisioner != nil {
		buildRunner.SetServiceProvisioners(
			&pgRunnerAdapter{p: pgProvisioner},
			&rdRunnerAdapter{p: rdProvisioner},
		)
	} else if pgProvisioner != nil {
		buildRunner.SetServiceProvisioners(&pgRunnerAdapter{p: pgProvisioner}, nil)
	} else if rdProvisioner != nil {
		buildRunner.SetServiceProvisioners(nil, &rdRunnerAdapter{p: rdProvisioner})
	}
```

- [ ] **Step 4: Verify everything builds + all backend tests pass**

```bash
cd backend && go build ./...
cd backend && go vet ./...
cd backend && go test ./...
```

Expected: clean build, clean vet, all tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): wire postgres/redis provisioners into the build runner

dockerCli now lives for the lifetime of the process and is shared
between Flow G bootstrap and the runner's per-env provisioning.
Two thin adapter structs (pgRunnerAdapter, rdRunnerAdapter)
translate between the services packages' EnvDatabase/EnvACL types
and builder's locally-redeclared shapes — keeping internal/builder
free of services/{postgres,redis} imports."
```

---

### Task 7: Final sanity + plan/checklist commit

**Files:**
- Modify: `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`

- [ ] **Step 1: Run the full backend suite + vet + build**

```bash
cd backend && go test ./... && go vet ./... && go build ./...
```

Expected: all green, all clean.

- [ ] **Step 2: Sanity-check the diff**

```bash
git diff --stat 9eb070c..HEAD
git log --oneline 9eb070c..HEAD
```

Expected: 6 commits (Tasks 1-6), files in `backend/` only.

- [ ] **Step 3: Update rollout checklist**

Replace the Plan 3b placeholder in `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` with:

```markdown
## Plan 3b — Per-env DB/ACL provisioning + URL injection

After merge + redeploy:
- [ ] Existing stripe-payments builds (still on v1 schema) trigger normally; runner logs `==> no .dev/config.yaml — skipping service provisioning` (or similar) — confirms graceful fallback
- [ ] `cd backend && go test ./internal/builder/... -v` passes locally with all four new TestRunner_Build_Services* and TestRunner_Teardown_DropsServices* tests
- [ ] Manual: create a test fixture project with `.dev/config.yaml` declaring `services.postgres: true`, push it, observe runner logs show `==> provisioning postgres database`
- [ ] After successful build: `docker exec paas-postgres psql -U postgres -c "\l"` shows the new database
- [ ] After successful build: `docker exec paas-postgres psql -U postgres -c "\du"` shows the new user
- [ ] After successful build: app's `.env` contains `DATABASE_URL=postgres://...`
- [ ] After successful build: `docker inspect <env-id>-app | jq '.[0].NetworkSettings.Networks | keys'` shows `paas-net` attached
- [ ] Branch delete: `docker exec paas-postgres psql -U postgres -c "\l"` no longer lists the test database
- [ ] Branch delete: `cat /data/compose/16/data/.credentials/store.json | jq '.project_secrets | keys'` no longer includes the env-id of the deleted env
- [ ] Same battery for redis with `services.redis: true` (`ACL LIST` instead of `\l`/`\du`)
```

- [ ] **Step 4: Commit plan + checklist**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-03b-runner-services-wiring.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 03b (runner services wiring)

Plan document + Plan 3b entry in the rollout checklist.
Implementation lands in the preceding 6 commits on this branch.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Push branch + open PR

- [ ] **Step 1: Push**

```bash
git push -u origin feat/v2-plan-03b-runner-services-wiring
```

- [ ] **Step 2: Open PR via gh**

```bash
gh pr create --title "v2 plan 03b: runner-side service wiring + URL injection" --body "$(cat <<'EOF'
## Summary

- Wires Plan 3a's per-env provisioners (`postgres.EnsureEnvDatabase`, `redis.EnsureEnvACL`) into the build runner — first consumer of `internal/iac` from Plan 2
- Build flow: parse `.dev/config.yaml` → if `services.postgres` / `services.redis` → provision per-env DB / ACL → write `DATABASE_URL` / `REDIS_URL` into the `.env` alongside secrets → attach `paas-net` to compose services so apps can reach the singletons via Docker DNS
- Teardown flow: parse iac → if services declared → drop per-env DB / ACL → remove `env:<id>:db_password` and `env:<id>:redis_password` from the credential store
- Best-effort iac parsing: missing or unparseable `.dev/config.yaml` falls back to v1 behaviour (no service provisioning)
- `EnsureEnvDatabase` / `EnsureEnvACL` extended to populate `URL` in their returned `EnvDatabase` / `EnvACL` — runner consumes URL directly without re-fetching the password
- `dockerCli` lifetime extended in `main.go` so the bootstrap (Flow G) and runner share a single SDK client

## Out of scope

- Migrating stripe-payments' `.dev/config.yaml` to v2 schema — Plan 8 (until then, runner falls back to v1 behaviour for stripe-payments)
- Pre/post-deploy hooks — Plan 4
- Custom domain + Let's Encrypt — Plan 5

## Test plan

- [x] `cd backend && go test ./...` — full suite green
- [x] `cd backend && go vet ./...` — clean
- [x] `cd backend && go build ./...` — clean
- [x] No file outside `backend/` modified except docs

After merge, manual home-lab verification per the rollout checklist Plan 3b section.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report PR URL back to the user**

---

## Acceptance criteria

- [ ] `EnvDatabase.URL` and `EnvACL.URL` populated after `EnsureEnvDatabase` / `EnsureEnvACL`
- [ ] `builder.PostgresProvisioner` / `builder.RedisProvisioner` interfaces exist; `Runner.SetServiceProvisioners(pg, rd)` works
- [ ] `builder.InjectPaasNet(composePath, network)` is idempotent and noops on empty network
- [ ] Runner.Build provisions services iff `iac.Config.Services.X` is true AND `r.X != nil`
- [ ] Runner.Build writes `DATABASE_URL` and/or `REDIS_URL` into `.env` (alongside secrets) when services were provisioned
- [ ] Runner.Build calls `InjectPaasNet` after `InjectTraefikLabels` when at least one service was provisioned
- [ ] Runner.Teardown calls `DropEnv*` for declared services and removes the per-env password cred-store entries
- [ ] Failures during provisioning abort the build with `r.fail` (don't silently continue)
- [ ] Failures during teardown drop are logged but don't abort directory cleanup
- [ ] Build with no `.dev/config.yaml` succeeds (logs warning, skips provisioning)
- [ ] Build with `.dev/config.yaml` declaring services but `r.X == nil` succeeds (logs warning, skips that service)
- [ ] `cmd/server/main.go` holds `dockerCli` for process lifetime; closes via `defer`
- [ ] `cmd/server/main.go` wires `pgRunnerAdapter` / `rdRunnerAdapter` into `buildRunner.SetServiceProvisioners`
- [ ] `go test ./...` clean, `go vet ./...` clean, `go build ./...` clean
- [ ] Branch is 7 commits ahead of master (6 implementation + 1 docs)
- [ ] PR opened with the test-plan checklist
- [ ] Rollout checklist updated for Plan 3b

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/`.
- **Never use `> nul`, `> NUL`, or `> /dev/null`** — destructive on this Windows host.
- **Spec is canonical** — `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` overrides this plan if they conflict. Flag in your PR description.
- **TDD discipline:** every feat task → write failing test → run-fail → implement → run-pass → commit.
- **Commit cadence:** one commit per task (7 task commits + 1 docs commit + 1 push). Don't squash. Don't amend.
- **`r.postgres` / `r.redis` may be nil** — every dereference must be guarded. Tests `TestRunner_Build_NilProvisioner_NoProvisioning` pin this.
- **Iac parse is best-effort** — never fail a build because `.dev/config.yaml` is malformed (until the migration in Plan 8 lands). Log a warning and continue. This is what makes Plan 3b ship-able alongside the still-v1 stripe-payments project.
- **Don't try to remove the legacy `internal/projects/devconfig.go`** — its callers (devdir.go, handlers/projects.go) still need it for project onboarding. Removal is part of Plan 8 (or a focused cleanup plan after).
- **The provisioner-interface-vs-concrete-type indirection in `builder/runner.go`** (locally-redeclared `PostgresEnvDatabase`/`RedisEnvACL`) is deliberate — it keeps the builder package free of `services/{postgres,redis}` imports. Adapters in main.go bridge the type wall.
