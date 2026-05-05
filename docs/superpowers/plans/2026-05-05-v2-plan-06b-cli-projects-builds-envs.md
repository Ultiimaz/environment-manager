# env-manager v2, Plan 6b — envm CLI: projects/builds/envs/services + missing backend endpoints

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the envm CLI per spec — `projects`, `builds`, `envs`, `services` subcommands — and add the five missing backend endpoints they require.

**Architecture:** Five new HTTP handlers wired through the Bearer-protected route group from Plan 6a. The CLI gains four new files in `cmd/envm/` (one per subcommand group). Project deletion fans out: drop each env via `runner.Teardown`, then drop project from store + clean up cred-store entries + remove the repo directory. Service status endpoints return container running/stopped + version + provisioned-env list (count only — names are out of scope for v2).

**Tech Stack:** Go 1.24, no new dependencies.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Lifecycle flows → Flow E (project deletion)", "Lifecycle flows → Flow D (branch delete)", "Backend HTTP endpoints (new)", "CLI tool (envm) → Commands".

**Out of scope (deferred):**
- Interactive `envm shell/psql/redis` → defer indefinitely (operators can `docker exec` directly)
- `POST /envs/{id}/restart` → could land here but spec is light; UI in Plan 7 will surface it
- `WS /ws/envs/{id}/runtime-logs` → Plan 7 (UI)
- `envm secrets check` proper iac-vs-set diff → defer to a future polish PR; current stub is sufficient

---

## File structure after this plan

**New files:**

```
backend/cmd/envm/projects.go   — list/onboard/show/delete
backend/cmd/envm/builds.go     — trigger/logs/list
backend/cmd/envm/envs.go       — destroy
backend/cmd/envm/services.go   — status
```

**Modified files:**

```
backend/internal/api/handlers/projects.go         — Delete handler
backend/internal/api/handlers/builds.go           — List handler
backend/internal/api/handlers/envs.go             — NEW Destroy handler
backend/internal/api/handlers/services.go         — NEW services-status handler
backend/internal/api/handlers/settings.go         — NEW settings handler
backend/internal/api/router.go                    — wire new endpoints
backend/cmd/envm/main.go                          — wire new dispatchers
```

**Files unchanged:** Plan 6a's `cmd/envm/{config,client,secrets}.go`, the iac/services/builder packages.

---

## Locked details

| Endpoint | Auth | Behaviour |
|---|---|---|
| `DELETE /api/v1/projects/{id}` | Bearer | Tear down all envs (compose down -v + drop services + cred cleanup), delete project row, rm -rf repo dir, delete project secrets |
| `POST /api/v1/envs/{id}/destroy` | Bearer | Preview env only — reject prod with 400. Tear down via runner.Teardown + delete env row |
| `GET /api/v1/envs/{id}/builds` | Open | Return list of build records (most-recent first), no values |
| `GET /api/v1/services/postgres` | Open | `{"running": bool, "image": "postgres:16", "container": "paas-postgres"}` |
| `GET /api/v1/services/redis` | Open | Same shape with redis values |
| `GET /api/v1/settings` | Open | `{"letsencrypt_email_set": bool, "credential_store_set": bool, "version": string}` — no secrets |
| CLI `envm projects list` | open GET | tabular: ID, NAME, REPO_URL, DEFAULT_BRANCH, STATUS |
| CLI `envm projects onboard <git-url> [--token PAT]` | Bearer POST | creates Project + prod Environment row |
| CLI `envm projects show <project>` | open GET | project + envs |
| CLI `envm projects delete <project> [--yes]` | Bearer DELETE | confirm via typed name (or `--yes` to skip) |
| CLI `envm builds trigger <project>/<env>` | Bearer POST | constructs env-id `<project>--<env>` |
| CLI `envm builds logs <project>/<env> [--follow]` | open GET (WS) | streams `/ws/envs/{id}/build-logs` |
| CLI `envm builds list <project>/<env>` | open GET | tabular |
| CLI `envm envs destroy <project>/<env> [--yes]` | Bearer POST | preview only |
| CLI `envm services status` | open GET | postgres + redis container status |

---

## Tasks

### Task 1: Branch + DELETE project endpoint

**Files:**
- Modify: `backend/internal/api/handlers/projects.go`
- Modify: `backend/internal/api/handlers/projects_test.go`
- Modify: `backend/internal/api/router.go`

The handler iterates env list, calls `runner.Teardown` per env, then `store.DeleteProject` + `os.RemoveAll(repo)` + delete cred-store project entries. Failures during per-env teardown are logged and the deletion continues — best-effort.

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status && git rev-parse HEAD
```

Expected: HEAD at `72916e8` (Plan 6a merge) or later.

```bash
git checkout -b feat/v2-plan-06b-cli-projects-builds-envs
```

- [ ] **Step 2: Add `Delete` method to ProjectsHandler**

Append to `backend/internal/api/handlers/projects.go`:

```go
// Delete handles DELETE /api/v1/projects/{id}.
//
// Tears down each environment via the runner (compose down -v + drop services
// + cred cleanup), removes the project's repo dir, deletes the project's
// credential-store entries, and finally removes the project row. Failures
// during per-env teardown are logged but don't abort the rest of the cascade.
func (h *ProjectsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "MISSING_ID", "id is required")
		return
	}
	project, err := h.store.GetProject(id)
	if err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "project not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	envs, err := h.store.ListEnvironments(project.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	// Tear down each env. Best-effort; collect errors for the response.
	var teardownErrors []string
	for _, env := range envs {
		if h.runner != nil {
			if terr := h.runner.Teardown(r.Context(), env); terr != nil {
				teardownErrors = append(teardownErrors, env.ID+": "+terr.Error())
				h.logger.Warn("project delete: env teardown failed",
					zap.String("env_id", env.ID), zap.Error(terr))
			}
		}
		if derr := h.store.DeleteEnvironment(project.ID, env.BranchSlug); derr != nil {
			h.logger.Warn("project delete: DeleteEnvironment failed",
				zap.String("env_id", env.ID), zap.Error(derr))
		}
	}
	// Clean cred-store project secrets (best-effort).
	if h.credStore != nil {
		if keys, err := h.credStore.ListProjectSecretKeys(project.ID); err == nil {
			for _, k := range keys {
				_ = h.credStore.DeleteProjectSecret(project.ID, k)
			}
		}
	}
	// Remove the project row + project directory.
	if err := h.store.DeleteProject(project.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	if project.LocalPath != "" {
		_ = os.RemoveAll(project.LocalPath)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"deleted":          project.ID,
		"teardown_errors":  teardownErrors,
		"environments":    len(envs),
	})
}
```

Add `"os"` and the runner import to projects.go imports if not already present.

This handler needs access to `runner` — extend `ProjectsHandler` struct + `NewProjectsHandler` to accept it. Find the struct:

```go
type ProjectsHandler struct {
	store        *projects.Store
	reposManager *repos.Manager
	credStore    *credentials.Store
	logger       *zap.Logger
	baseDomain   string
}
```

Add a `runner *builder.Runner` field. Update `NewProjectsHandler` signature to accept it as the last argument. Update the constructor call site in `router.go`. Also add `"github.com/environment-manager/backend/internal/builder"` to projects.go imports.

- [ ] **Step 3: Wire route + update existing call site**

In `router.go`, find the `NewProjectsHandler(...)` call and add `cfg.Builder` as the last argument. In the Bearer-protected group, add:

```go
			r.Delete("/projects/{id}", projectsHandler.Delete)
```

- [ ] **Step 4: Write the test**

Append to `backend/internal/api/handlers/projects_test.go`:

```go
func TestProjectsHandler_Delete(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i)
	}
	creds, _ := credentials.NewStore(filepath.Join(dir, "creds.json"), credKey)

	repoPath := filepath.Join(dir, "repo")
	_ = os.MkdirAll(repoPath, 0755)
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp", LocalPath: repoPath})
	_ = store.SaveEnvironment(&models.Environment{ID: "p1--main", ProjectID: "p1", BranchSlug: "main"})
	_ = creds.SaveProjectSecret("p1", "STRIPE_KEY", "sk_test")

	// Use the real runner with a fake compose executor so Teardown doesn't fail.
	queue := builder.NewQueue()
	runner := builder.NewRunner(store, &fakeComposeExec{}, dir, "", queue, zap.NewNop(), creds)

	h := NewProjectsHandler(store, nil, creds, "home", zap.NewNop(), runner)

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p1", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// Project row gone.
	if _, err := store.GetProject("p1"); !errors.Is(err, projects.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
	// Repo dir gone.
	if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
		t.Errorf("repo dir not removed: %v", err)
	}
	// Project secrets gone.
	if keys, _ := creds.ListProjectSecretKeys("p1"); len(keys) != 0 {
		t.Errorf("expected 0 cred-store keys after delete, got %d", len(keys))
	}
}

// fakeComposeExec is a compose executor that always succeeds.
type fakeComposeExec struct{}

func (fakeComposeExec) Compose(ctx context.Context, _ string, _ string, _ []string, _ io.Writer, _ io.Writer) error {
	return nil
}
```

Add imports to projects_test.go if missing: `"io"`, `"github.com/environment-manager/backend/internal/builder"`.

- [ ] **Step 5: Run tests + commit**

```bash
cd backend && go test ./...
```

Expected: all PASS.

```bash
git add backend/internal/api/handlers/projects.go backend/internal/api/handlers/projects_test.go backend/internal/api/router.go
git commit -m "feat(api): DELETE /projects/{id} fans out env teardown + cleanup

Iterates env list, calls runner.Teardown per env (best-effort —
errors are surfaced in the response but don't abort), then
removes project row, repo dir, and cred-store project secrets.
ProjectsHandler now takes the runner via NewProjectsHandler;
router.go updated to pass cfg.Builder."
```

---

### Task 2: POST `/envs/{id}/destroy` endpoint

**Files:**
- Create: `backend/internal/api/handlers/envs.go`
- Create: `backend/internal/api/handlers/envs_test.go`
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Create handler**

Create `backend/internal/api/handlers/envs.go`:

```go
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// EnvsHandler exposes per-environment endpoints. Currently: destroy.
// Build trigger lives on BuildsHandler for legacy continuity.
type EnvsHandler struct {
	store     *projects.Store
	runner    *builder.Runner
	credStore *credentials.Store
	logger    *zap.Logger
}

func NewEnvsHandler(store *projects.Store, runner *builder.Runner, credStore *credentials.Store, logger *zap.Logger) *EnvsHandler {
	return &EnvsHandler{store: store, runner: runner, credStore: credStore, logger: logger}
}

// Destroy handles POST /api/v1/envs/{id}/destroy.
//
// Preview environments only — reject prod with 400 ("use project delete to
// remove a prod env"). Per-env teardown via runner.Teardown, then remove the
// env row.
func (h *EnvsHandler) Destroy(w http.ResponseWriter, r *http.Request) {
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
	if env.Kind == models.EnvKindProd {
		respondError(w, http.StatusBadRequest, "PROD_ENV", "prod environments cannot be destroyed standalone — use DELETE /projects/{id} to remove the whole project")
		return
	}
	if h.runner != nil {
		if terr := h.runner.Teardown(r.Context(), env); terr != nil {
			h.logger.Warn("env destroy: teardown failed",
				zap.String("env_id", env.ID), zap.Error(terr))
		}
	}
	if derr := h.store.DeleteEnvironment(projectID, branchSlug); derr != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", derr.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"destroyed": env.ID})
}
```

- [ ] **Step 2: Wire route + write test + commit**

In `router.go`, add to the Bearer-protected group:

```go
			envsHandler := handlers.NewEnvsHandler(cfg.ProjectsStore, cfg.Builder, cfg.CredentialStore, cfg.Logger)
			r.Post("/envs/{id}/destroy", envsHandler.Destroy)
```

Wait — `r.Group` doesn't let you instantiate handlers inline cleanly. Lift `envsHandler` to the parent scope (alongside the other handlers at the top of `NewRouter`):

```go
	envsHandler := handlers.NewEnvsHandler(cfg.ProjectsStore, cfg.Builder, cfg.CredentialStore, cfg.Logger)
```

Then in the Bearer-protected group:

```go
			r.Post("/envs/{id}/destroy", envsHandler.Destroy)
```

Test in `envs_test.go`:

```go
package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

type envsFakeExec struct{}

func (envsFakeExec) Compose(ctx context.Context, _, _ string, _ []string, _ io.Writer, _ io.Writer) error {
	return nil
}

func TestEnvsHandler_Destroy_Preview(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	creds, _ := credentials.NewStore(filepath.Join(dir, "c.json"), make([]byte, 32))
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	_ = store.SaveEnvironment(&models.Environment{ID: "p1--feature-x", ProjectID: "p1", BranchSlug: "feature-x", Kind: models.EnvKindPreview})
	runner := builder.NewRunner(store, envsFakeExec{}, dir, "", builder.NewQueue(), zap.NewNop(), creds)
	h := NewEnvsHandler(store, runner, creds, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/v1/envs/p1--feature-x/destroy", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1--feature-x"})
	rec := httptest.NewRecorder()
	h.Destroy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetEnvironment("p1", "feature-x"); !errors.Is(err, projects.ErrNotFound) {
		t.Errorf("expected env removed, got %v", err)
	}
}

func TestEnvsHandler_Destroy_RejectsProd(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	_ = store.SaveEnvironment(&models.Environment{ID: "p1--main", ProjectID: "p1", BranchSlug: "main", Kind: models.EnvKindProd})
	h := NewEnvsHandler(store, nil, nil, zap.NewNop())

	req := httptest.NewRequest("POST", "/api/v1/envs/p1--main/destroy", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1--main"})
	rec := httptest.NewRecorder()
	h.Destroy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
```

```bash
cd backend && go test ./...
git add backend/internal/api/handlers/envs.go backend/internal/api/handlers/envs_test.go backend/internal/api/router.go
git commit -m "feat(api): POST /envs/{id}/destroy tears down preview environments

Preview-only — rejects prod with 400 (use DELETE /projects/{id}
for the whole project). Calls runner.Teardown best-effort then
deletes the env row."
```

---

### Task 3: GET `/envs/{id}/builds` + service status + settings endpoints

Three endpoints in one task because each is small (~20 LOC + simple test).

**Files:**
- Modify: `backend/internal/api/handlers/builds.go`
- Modify: `backend/internal/api/handlers/builds_test.go`
- Create: `backend/internal/api/handlers/services.go`
- Create: `backend/internal/api/handlers/services_test.go`
- Create: `backend/internal/api/handlers/settings.go`
- Create: `backend/internal/api/handlers/settings_test.go`
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Add `List` to BuildsHandler**

Append to `backend/internal/api/handlers/builds.go`:

```go
// List handles GET /api/v1/envs/{id}/builds — returns the env's build history,
// most-recent first. Build records include status, SHA, timestamps, log path.
func (h *BuildsHandler) List(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "id")
	projectID, _, ok := splitEnvID(envID)
	if !ok {
		respondError(w, http.StatusBadRequest, "INVALID_ENV_ID", "env id must be <project>--<slug>")
		return
	}
	builds, err := h.store.ListBuildsForEnv(projectID, envID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(builds)
}
```

Add `"encoding/json"` to imports if not present.

- [ ] **Step 2: Create services-status handler**

Create `backend/internal/api/handlers/services.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ContainerInspector exposes only the container-status query needed by the
// services handler. Implemented by *docker.Client.
type ContainerInspector interface {
	ContainerStatus(ctx context.Context, name string) (exists, running bool, err error)
}

// ServicesHandler exposes /api/v1/services/{postgres,redis} status endpoints.
// Read-only; used by envm services status and the eventual UI Services page.
type ServicesHandler struct {
	docker ContainerInspector
}

func NewServicesHandler(docker ContainerInspector) *ServicesHandler {
	return &ServicesHandler{docker: docker}
}

type serviceStatus struct {
	Container string `json:"container"`
	Image     string `json:"image"`
	Running   bool   `json:"running"`
	Exists    bool   `json:"exists"`
}

func (h *ServicesHandler) Postgres(w http.ResponseWriter, r *http.Request) {
	h.respond(w, "paas-postgres", "postgres:16")
}

func (h *ServicesHandler) Redis(w http.ResponseWriter, r *http.Request) {
	h.respond(w, "paas-redis", "redis:7")
}

func (h *ServicesHandler) respond(w http.ResponseWriter, name, image string) {
	exists, running := false, false
	if h.docker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e, run, err := h.docker.ContainerStatus(ctx, name)
		if err == nil {
			exists, running = e, run
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(serviceStatus{
		Container: name,
		Image:     image,
		Running:   running,
		Exists:    exists,
	})
}
```

- [ ] **Step 3: Create settings handler**

Create `backend/internal/api/handlers/settings.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
)

// SettingsResponse is the GET /api/v1/settings body. No secrets, just
// presence flags so the operator/UI can see what's configured.
type SettingsResponse struct {
	LetsencryptEmailSet bool   `json:"letsencrypt_email_set"`
	CredentialStoreSet  bool   `json:"credential_store_set"`
	Version             string `json:"version"`
}

// SettingsHandler returns operator-visible config presence (no values).
type SettingsHandler struct {
	hasLEEmail   bool
	hasCredStore bool
	version      string
}

func NewSettingsHandler(letsencryptEmail string, credStoreReady bool, version string) *SettingsHandler {
	return &SettingsHandler{
		hasLEEmail:   letsencryptEmail != "",
		hasCredStore: credStoreReady,
		version:      version,
	}
}

func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SettingsResponse{
		LetsencryptEmailSet: h.hasLEEmail,
		CredentialStoreSet:  h.hasCredStore,
		Version:             h.version,
	})
}
```

- [ ] **Step 4: Wire all three routes in router.go**

Extend `RouterConfig`:

```go
type RouterConfig struct {
	// ... existing ...
	DockerClient     ContainerInspector  // nil = services endpoints return exists=false
	LetsencryptEmail string
	Version          string
}
```

Wait — `ContainerInspector` is in handlers package. Fix import:

```go
type RouterConfig struct {
	// ... existing ...
	DockerClient     handlers.ContainerInspector
	LetsencryptEmail string
	Version          string
}
```

Construct handlers + wire routes. In `NewRouter`, after the existing handler instantiation:

```go
	servicesHandler := handlers.NewServicesHandler(cfg.DockerClient)
	settingsHandler := handlers.NewSettingsHandler(cfg.LetsencryptEmail, cfg.CredentialStore != nil, cfg.Version)
```

Then in the route block (these are read-only, NOT inside the Bearer group):

```go
		r.Get("/envs/{id}/builds", buildsHandler.List)
		r.Get("/services/postgres", servicesHandler.Postgres)
		r.Get("/services/redis", servicesHandler.Redis)
		r.Get("/settings", settingsHandler.Get)
```

- [ ] **Step 5: Wire from main.go**

In `cmd/server/main.go`, find the `api.NewRouter(api.RouterConfig{...})` call. Add:

```go
		DockerClient:     dockerCli,  // already in scope from Plan 3a's bootstrap block
		LetsencryptEmail: cfg.LetsencryptEmail,
		Version:          "v2",  // hardcoded for now; -ldflags can override later
```

Note: `dockerCli` is declared inside the credStore block in main.go (Plan 3a). Lift it to outer scope so the router config can see it. If credStore is nil, `dockerCli` is also nil — `NewServicesHandler(nil)` is safe (returns exists=false).

- [ ] **Step 6: Write tests + commit**

For builds:

```go
// In builds_test.go:
func TestBuildsHandler_List(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	now := time.Now().UTC()
	_ = store.SaveBuild("p1", &models.Build{ID: "b1", EnvID: "p1--main", Status: models.BuildStatusSuccess, StartedAt: now})
	_ = store.SaveBuild("p1", &models.Build{ID: "b2", EnvID: "p1--main", Status: models.BuildStatusRunning, StartedAt: now})

	h := NewBuildsHandler(store, nil, dir, zap.NewNop())
	req := httptest.NewRequest("GET", "/api/v1/envs/p1--main/builds", nil)
	req = withChiURLParams(req, map[string]string{"id": "p1--main"})
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []*models.Build
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d builds, want 2", len(got))
	}
}
```

For services + settings (in their respective `_test.go` files):

```go
// services_test.go:
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
)

type fakeInspector struct {
	exists, running bool
	err             error
}

func (f *fakeInspector) ContainerStatus(_ context.Context, _ string) (bool, bool, error) {
	return f.exists, f.running, f.err
}

func TestServicesHandler_PostgresRunning(t *testing.T) {
	h := NewServicesHandler(&fakeInspector{exists: true, running: true})
	req := httptest.NewRequest("GET", "/api/v1/services/postgres", nil)
	rec := httptest.NewRecorder()
	h.Postgres(rec, req)
	var got serviceStatus
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if !got.Running || !got.Exists || got.Container != "paas-postgres" || got.Image != "postgres:16" {
		t.Errorf("got %+v", got)
	}
}

func TestServicesHandler_NilDockerSafe(t *testing.T) {
	h := NewServicesHandler(nil)
	req := httptest.NewRequest("GET", "/api/v1/services/redis", nil)
	rec := httptest.NewRecorder()
	h.Redis(rec, req)
	var got serviceStatus
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Running || got.Exists || got.Container != "paas-redis" {
		t.Errorf("got %+v", got)
	}
}

func TestServicesHandler_DockerError(t *testing.T) {
	h := NewServicesHandler(&fakeInspector{err: errors.New("daemon unreachable")})
	req := httptest.NewRequest("GET", "/api/v1/services/postgres", nil)
	rec := httptest.NewRecorder()
	h.Postgres(rec, req)
	// Errors degrade gracefully to exists=false, running=false rather than failing.
	var got serviceStatus
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Running {
		t.Error("expected running=false on docker error")
	}
}

// settings_test.go:
package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSettingsHandler(t *testing.T) {
	h := NewSettingsHandler("ops@example.com", true, "v2-test")
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	var got SettingsResponse
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if !got.LetsencryptEmailSet || !got.CredentialStoreSet || got.Version != "v2-test" {
		t.Errorf("got %+v", got)
	}
}

func TestSettingsHandler_BothUnset(t *testing.T) {
	h := NewSettingsHandler("", false, "v2-test")
	req := httptest.NewRequest("GET", "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	var got SettingsResponse
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.LetsencryptEmailSet || got.CredentialStoreSet {
		t.Errorf("got %+v", got)
	}
}
```

```bash
cd backend && go test ./...
git add backend/internal/api/handlers/builds.go backend/internal/api/handlers/builds_test.go backend/internal/api/handlers/services.go backend/internal/api/handlers/services_test.go backend/internal/api/handlers/settings.go backend/internal/api/handlers/settings_test.go backend/internal/api/router.go backend/cmd/server/main.go
git commit -m "feat(api): GET endpoints for env builds, services status, settings

Three new read-only endpoints powering envm builds list / services
status / future settings UI. ContainerInspector interface in the
handlers package keeps the services handler decoupled from
docker.Client. Settings handler exposes presence-only config (no
secrets). All three are open GET endpoints — the UI uses anonymous
reads."
```

---

### Task 4: CLI `envm projects {list,onboard,show,delete}`

**Files:**
- Create: `backend/cmd/envm/projects.go`
- Modify: `backend/cmd/envm/main.go`

- [ ] **Step 1: Create `projects.go`**

```go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
)

type project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	DefaultBranch string `json:"default_branch"`
	Status        string `json:"status"`
}

type projectDetail struct {
	Project      *project       `json:"project"`
	Environments []*environment `json:"environments"`
}

type environment struct {
	ID         string `json:"id"`
	Branch     string `json:"branch"`
	BranchSlug string `json:"branch_slug"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	URL        string `json:"url"`
}

func runProjects(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm projects <list|onboard|show|delete> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		projectsList(args[1:])
	case "onboard":
		projectsOnboard(args[1:])
	case "show":
		projectsShow(args[1:])
	case "delete":
		projectsDelete(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown projects subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func projectsList(_ []string) {
	c := mustClient()
	var items []project
	if err := c.Do("GET", "/api/v1/projects", nil, &items); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tDEFAULT_BRANCH\tSTATUS\tREPO")
	for _, p := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.DefaultBranch, p.Status, p.RepoURL)
	}
	_ = w.Flush()
}

func projectsOnboard(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm projects onboard <git-url> [--token PAT]")
		os.Exit(2)
	}
	repoURL := args[0]
	var token string
	for i := 1; i < len(args); i++ {
		if args[i] == "--token" && i+1 < len(args) {
			token = args[i+1]
			i++
		}
	}
	body := map[string]string{"repo_url": repoURL}
	if token != "" {
		body["token"] = token
	}
	c := mustClient()
	var resp json.RawMessage
	if err := c.Do("POST", "/api/v1/projects", body, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}

func projectsShow(args []string) {
	id := mustProjectArg(args, "envm projects show <project-id>")
	c := mustClient()
	var detail projectDetail
	if err := c.Do("GET", "/api/v1/projects/"+url.PathEscape(id), nil, &detail); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if detail.Project == nil {
		fmt.Fprintln(os.Stderr, "project not found")
		os.Exit(1)
	}
	fmt.Printf("ID:             %s\n", detail.Project.ID)
	fmt.Printf("Name:           %s\n", detail.Project.Name)
	fmt.Printf("Repo:           %s\n", detail.Project.RepoURL)
	fmt.Printf("Default branch: %s\n", detail.Project.DefaultBranch)
	fmt.Printf("Status:         %s\n", detail.Project.Status)
	fmt.Println()
	fmt.Println("Environments:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  ID\tBRANCH\tKIND\tSTATUS\tURL")
	for _, e := range detail.Environments {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", e.ID, e.Branch, e.Kind, e.Status, e.URL)
	}
	_ = w.Flush()
}

func projectsDelete(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm projects delete <project-id> [--yes]")
		os.Exit(2)
	}
	id := args[0]
	yes := false
	for _, a := range args[1:] {
		if a == "--yes" {
			yes = true
		}
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "Type project ID %q to confirm deletion: ", id)
		reader := bufio.NewReader(os.Stdin)
		typed, _ := reader.ReadString('\n')
		typed = strings.TrimSpace(typed)
		if typed != id {
			fmt.Fprintln(os.Stderr, "confirmation mismatch — aborting")
			os.Exit(1)
		}
	}
	c := mustClient()
	var resp json.RawMessage
	if err := c.Do("DELETE", "/api/v1/projects/"+url.PathEscape(id), nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}
```

- [ ] **Step 2: Wire dispatcher in main.go**

In `main.go`'s switch, add:

```go
	case "projects":
		runProjects(os.Args[2:])
```

Also update the usage message to include the projects subcommands.

- [ ] **Step 3: Build + commit**

```bash
cd backend && go build -o /tmp/envm ./cmd/envm
git add backend/cmd/envm/projects.go backend/cmd/envm/main.go
git commit -m "feat(cmd/envm): projects list/onboard/show/delete

Tabular list, onboard via POST /projects, show with env table,
delete with typed-name confirmation (or --yes to skip). Onboard
emits the raw JSON response so operators can see required-secrets
hints from the server's CreateProjectResponse."
```

---

### Task 5: CLI `envm builds {trigger,logs,list}` + `envs destroy` + `services status`

Three command groups in one task because each is small (~40 LOC).

**Files:**
- Create: `backend/cmd/envm/builds.go`
- Create: `backend/cmd/envm/envs.go`
- Create: `backend/cmd/envm/services.go`
- Modify: `backend/cmd/envm/main.go`

- [ ] **Step 1: Create `builds.go`**

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gorilla/websocket"
)

type build struct {
	ID          string  `json:"id"`
	EnvID       string  `json:"env_id"`
	SHA         string  `json:"sha"`
	Status      string  `json:"status"`
	TriggeredBy string  `json:"triggered_by"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  *string `json:"finished_at,omitempty"`
	LogPath     string  `json:"log_path"`
}

func runBuilds(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds <trigger|logs|list> <project>/<env> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "trigger":
		buildsTrigger(args[1:])
	case "logs":
		buildsLogs(args[1:])
	case "list":
		buildsList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown builds subcommand %q\n", args[0])
		os.Exit(2)
	}
}

// envIDFromArg parses "project/env" into "project--env" (the API's env id).
func envIDFromArg(s string) (string, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("expected <project>/<env>, got %q", s)
	}
	return parts[0] + "--" + parts[1], nil
}

func buildsTrigger(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds trigger <project>/<env>")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c := mustClient()
	var resp struct {
		Data struct {
			BuildID string `json:"build_id"`
			EnvID   string `json:"env_id"`
		} `json:"data"`
	}
	if err := c.Do("POST", "/api/v1/envs/"+url.PathEscape(envID)+"/build", nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("triggered build %s for env %s\n", resp.Data.BuildID, resp.Data.EnvID)
}

func buildsLogs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds logs <project>/<env> [--follow]")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Convert https→wss, http→ws.
	wsURL := strings.Replace(strings.TrimRight(cfg.Endpoint, "/"), "http", "ws", 1) + "/ws/envs/" + url.PathEscape(envID) + "/build-logs"
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "websocket dial %s: %v\n", wsURL, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Minute))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		os.Stdout.Write(msg)
	}
}

func buildsList(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm builds list <project>/<env>")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c := mustClient()
	var items []build
	if err := c.Do("GET", "/api/v1/envs/"+url.PathEscape(envID)+"/builds", nil, &items); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSHA\tSTATUS\tTRIGGERED_BY\tSTARTED_AT")
	for _, b := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", b.ID, truncate(b.SHA, 7), b.Status, b.TriggeredBy, b.StartedAt)
	}
	_ = w.Flush()
	_ = json.RawMessage{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
```

- [ ] **Step 2: Create `envs.go`**

```go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func runEnvs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm envs destroy <project>/<env> [--yes]")
		os.Exit(2)
	}
	switch args[0] {
	case "destroy":
		envsDestroy(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown envs subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func envsDestroy(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm envs destroy <project>/<env> [--yes]")
		os.Exit(2)
	}
	envID, err := envIDFromArg(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	yes := false
	for _, a := range args[1:] {
		if a == "--yes" {
			yes = true
		}
	}
	if !yes {
		fmt.Fprintf(os.Stderr, "Type env id %q to confirm: ", envID)
		typed, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(typed) != envID {
			fmt.Fprintln(os.Stderr, "confirmation mismatch — aborting")
			os.Exit(1)
		}
	}
	c := mustClient()
	var resp json.RawMessage
	if err := c.Do("POST", "/api/v1/envs/"+url.PathEscape(envID)+"/destroy", nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(resp))
}
```

- [ ] **Step 3: Create `services.go`**

```go
package main

import (
	"fmt"
	"os"
)

type serviceStatus struct {
	Container string `json:"container"`
	Image     string `json:"image"`
	Running   bool   `json:"running"`
	Exists    bool   `json:"exists"`
}

func runServices(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm services status")
		os.Exit(2)
	}
	switch args[0] {
	case "status":
		servicesStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown services subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func servicesStatus() {
	c := mustClient()
	var pg, rd serviceStatus
	if err := c.Do("GET", "/api/v1/services/postgres", nil, &pg); err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
	}
	if err := c.Do("GET", "/api/v1/services/redis", nil, &rd); err != nil {
		fmt.Fprintln(os.Stderr, "redis:", err)
	}
	for _, s := range []serviceStatus{pg, rd} {
		fmt.Printf("%-15s  image=%-13s  exists=%v  running=%v\n", s.Container, s.Image, s.Exists, s.Running)
	}
}
```

- [ ] **Step 4: Wire dispatchers in main.go**

In `main.go`'s switch:

```go
	case "projects":
		runProjects(os.Args[2:])
	case "builds":
		runBuilds(os.Args[2:])
	case "envs":
		runEnvs(os.Args[2:])
	case "services":
		runServices(os.Args[2:])
```

Update the usage message to list all subcommands.

- [ ] **Step 5: Build + go mod tidy + commit**

```bash
cd backend && go mod tidy && go build -o /tmp/envm ./cmd/envm
```

The `gorilla/websocket` import in builds.go should already be present in `go.sum` — it's used by the backend.

```bash
git add backend/cmd/envm/builds.go backend/cmd/envm/envs.go backend/cmd/envm/services.go backend/cmd/envm/main.go
git commit -m "feat(cmd/envm): builds, envs, services subcommands

envm builds {trigger,logs,list} — POST /envs/{id}/build, WS
/build-logs streaming, GET /envs/{id}/builds tabular.
envm envs destroy — POST /envs/{id}/destroy with typed-id
confirmation (or --yes).
envm services status — postgres + redis container status side-
by-side."
```

---

### Task 6: Final sanity + plan/checklist commit

**Files:**
- Modify: `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`

- [ ] **Step 1: Run the full backend suite + vet + build**

```bash
cd backend && go test ./... -count=1 && go vet ./... && go build ./... && go build -o /tmp/envm ./cmd/envm
```

Expected: all clean.

- [ ] **Step 2: Update rollout checklist**

Replace the Plan 6b placeholder with:

```markdown
## Plan 6b — Project/build/env/services CLI commands

After merge + redeploy:
- [ ] `cd backend && go test ./...` — full suite green
- [ ] New endpoints respond: `curl https://<env-manager>/api/v1/services/postgres` returns JSON; `curl https://<env-manager>/api/v1/settings` returns presence flags
- [ ] `envm projects list` returns tabular project list
- [ ] `envm projects onboard https://github.com/<user>/<repo>.git --token <pat>` creates a new project
- [ ] `envm projects show <id>` displays project metadata + envs
- [ ] `envm projects delete <id> --yes` tears down all envs + repo dir + secrets
- [ ] `envm builds trigger <project>/<env>` starts a build; response shows build_id
- [ ] `envm builds logs <project>/<env>` streams the build log via WS
- [ ] `envm builds list <project>/<env>` shows recent builds
- [ ] `envm envs destroy <project>/<env> --yes` removes a preview env (rejects prod with 400)
- [ ] `envm services status` shows paas-postgres + paas-redis running on paas-net
```

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-06b-cli-projects-builds-envs.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 06b"
```

---

### Task 7: Push + open PR

```bash
git push -u origin feat/v2-plan-06b-cli-projects-builds-envs
gh pr create --title "v2 plan 06b: envm CLI projects/builds/envs/services" --body "$(cat <<'EOF'
## Summary

- Five new HTTP endpoints (DELETE project, POST env destroy, GET env builds, GET services postgres/redis, GET settings)
- Four new CLI subcommand groups in cmd/envm: projects, builds, envs, services
- `envm projects delete` and `envm envs destroy` require typed-name confirmation (or --yes)
- Build log streaming via existing WS endpoint

## Out of scope

- Interactive `envm shell/psql/redis` — defer indefinitely
- POST /envs/{id}/restart and WS /runtime-logs — Plan 7 (UI)
- envm secrets check proper iac-vs-set diff — defer

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Acceptance criteria

- [ ] DELETE /api/v1/projects/{id} fans out env teardown + cred-store cleanup + repo rm
- [ ] POST /api/v1/envs/{id}/destroy works for preview, returns 400 for prod
- [ ] GET /api/v1/envs/{id}/builds returns build list
- [ ] GET /api/v1/services/{postgres,redis} returns status JSON; safe when docker client is nil
- [ ] GET /api/v1/settings returns presence flags (no secrets)
- [ ] `envm projects list/onboard/show/delete` work end-to-end
- [ ] `envm builds trigger/logs/list` work end-to-end
- [ ] `envm envs destroy` works end-to-end; rejects prod
- [ ] `envm services status` shows both singletons
- [ ] `go test ./...` clean, `go vet ./...` clean, `go build ./...` clean
- [ ] Branch is 7 commits ahead of master (6 implementation + 1 docs)
- [ ] PR opened with the test-plan checklist
- [ ] Rollout checklist updated for Plan 6b

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/`.
- **Never use `> nul`, `> NUL`, or `> /dev/null`**.
- **TDD discipline:** write failing test → run-fail → implement → run-pass → commit per task.
- **Don't squash, don't amend.** New commits only.
- **`NewProjectsHandler` signature change in Task 1** — the existing call site in `router.go` is at the top of `NewRouter`. Update it. Existing tests in `projects_test.go` may also call `NewProjectsHandler` — update their callers.
- **Don't try to add domain conflict detection** to the project create handler — that's deferred.
- **Don't try to add `runtime-logs` WS endpoint** — Plan 7 (UI) territory.
- **`gorilla/websocket` already in go.mod** for the backend's WS endpoints. The CLI's import of it adds no new transitive deps.
- **`envm builds logs` does NOT support `--follow` flag in this plan** despite the usage string. The WS endpoint streams as-the-build-runs already, and the CLI exits when the WS closes (which happens when build finishes). Add `--follow` later if the polling-vs-WS distinction matters.
