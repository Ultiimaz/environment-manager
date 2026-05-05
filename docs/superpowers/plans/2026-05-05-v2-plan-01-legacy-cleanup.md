# env-manager v2, Plan 1 — Legacy backend cleanup

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the legacy backend surface (containers / volumes / network / compose / git / repos / github / exec / stats / legacy webhook flow) and the internal Go packages that supported it, leaving a clean foundation that still builds, still passes all kept tests, and still serves the existing `.dev/`-based Project + Build + Webhook v2 flow on the home lab. **No new behaviour.** The frontend keeps its legacy pages for now (they break at the API layer but don't crash the SPA) — UI rebuild is Plan 7.

**Architecture:** Surgical deletion in dependency order. Routes go first (router stops referencing handlers), then handler files (nothing imports them), then `webhook.go` legacy paths (drop `git.Repository` + `state.Manager` fields), then `main.go` cleanup, then internal packages. Each task ends with `go build ./...` + `go test ./...` green.

**Tech Stack:** Go 1.24, no new dependencies. Existing packages: `chi/v5`, `gorilla/websocket`, `go-git/v5`, `zap`, `yaml.v3`.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Architecture → What gets deleted" and "Migration → Phase 1 items 1–4".

---

## File structure after this plan

**Backend packages remaining:**

```
backend/internal/
├── api/              (router.go + handlers/{health,projects,builds,webhook}.go)
├── builder/          (kept — Runner, Queue, Render, Labels, ComposeExecutor)
├── buildlog/         (kept — Log)
├── config/           (kept — config.go only; loader.go deleted)
├── credentials/      (kept — Store with project secrets)
├── docker/           (kept — Client wrapper used by buildlog/buildler? actually not — see Task 11)
├── models/           (slimmed: project.go kept; container/volume/network/compose models deleted)
├── projects/         (kept — entirety)
├── proxy/            (kept — used in v2 Plan 5 for TLS labels; trimmed to Manager + label generator)
```

**Backend packages deleted:**

```
backend/internal/backup/      — gone
backend/internal/state/       — gone
backend/internal/sync/        — gone
backend/internal/git/         — gone
backend/internal/repos/       — gone
backend/internal/stats/       — gone
backend/internal/config/loader.go  — gone (config.go kept)
```

**Handler files deleted:**

```
backend/internal/api/handlers/containers.go  — gone
backend/internal/api/handlers/volumes.go     — gone
backend/internal/api/handlers/network.go     — gone
backend/internal/api/handlers/compose.go     — gone
backend/internal/api/handlers/git.go         — gone
backend/internal/api/handlers/repos.go       — gone
backend/internal/api/handlers/github.go      — gone
backend/internal/api/handlers/exec.go        — gone
backend/internal/api/handlers/stats.go       — gone
backend/internal/api/handlers/logs.go        — gone (legacy /ws/containers/* — Plan 3 adds /ws/envs/{id}/runtime-logs)
```

**Files modified:**

```
backend/internal/api/router.go               — drop legacy routes + handler instantiations + RouterConfig fields
backend/internal/api/handlers/webhook.go     — drop git.Repository + state.Manager + legacy flow
backend/internal/api/handlers/health.go      — kept; respondError + respondSuccess helpers stay
backend/internal/api/handlers/builds.go      — kept; verify still compiles
backend/internal/api/handlers/projects.go    — kept; verify still compiles
backend/internal/models/                     — delete container.go, volume.go, network.go, compose.go, repository.go, stats.go, state.go (keep project.go)
backend/cmd/server/main.go                   — strip legacy init, simplify
backend/internal/proxy/manager.go            — verify still compiles after dependents are gone (it imports config.Loader for CoreDNS — needs surgery)
```

**Models to verify before deleting:** any of `models/{container,volume,network,compose,repository,stats,state}.go` may be referenced from the *kept* codebase. Task 9 has a grep step.

---

## Tasks

### Task 1: Verify clean starting state, create branch

- [ ] **Step 1: Verify on master + clean working tree**

```bash
cd G:/Workspaces/claude-code-tests/env-manager
git status
git log -1 --oneline
```

Expected: working tree clean (or only the .bridgemind_transcripts / .claude / hermes-deploy-state-2026-05-04.md untracked items from the session — those are unrelated). HEAD should be `7b6ae32` (the v2 design spec) or later.

- [ ] **Step 2: Create branch**

```bash
git checkout -b feat/v2-plan-01-legacy-cleanup
```

- [ ] **Step 3: Verify build + tests green before any changes**

```bash
cd backend
go build ./...
go test ./...
```

Expected: clean build, all packages report `ok` or `[no test files]`. **Do not proceed if anything fails** — diagnose first.

---

### Task 2: Strip legacy routes + handler instantiations from router.go

**Files:**
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Read current router.go to confirm shape**

Run: `cat backend/internal/api/router.go`

You should see imports for `backup`, `config`, `docker`, `git`, `proxy`, `repos`, `state`, `stats`, plus `handlers`, `builder`, `projects`, `credentials`. The `RouterConfig` struct has fields for all of these. The route block has `/containers`, `/volumes`, `/compose`, `/network`, `/git`, `/repos`, `/github`, plus `/projects`, `/envs`, `/webhook/*`. WS routes include `/ws/containers/*` and `/ws/envs/{id}/build-logs`.

- [ ] **Step 2: Replace router.go entirely with the cleaned version**

Write this exact content to `backend/internal/api/router.go`:

```go
package api

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/environment-manager/backend/internal/api/handlers"
	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
	"go.uber.org/zap"
)

// RouterConfig contains all dependencies for the router.
//
// Slimmed down for env-manager v2: legacy fields (DockerClient, GitRepo,
// ConfigLoader, StateManager, BackupScheduler, StatsStore, StatsCollector,
// ProxyManager) removed. Only the .dev/-based PaaS surface remains.
type RouterConfig struct {
	ReposManager    *repos.Manager // kept temporarily — still used by ProjectsHandler.Create for cloning
	ProjectsStore   *projects.Store
	Builder         *builder.Runner
	CredentialStore *credentials.Store
	StaticDir       string
	DataDir         string
	BaseDomain      string
	Logger          *zap.Logger
}

// NewRouter creates a new HTTP router.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Create handlers
	webhookHandler := handlers.NewWebhookHandler(cfg.Logger)
	webhookHandler.SetProjectsStore(cfg.ProjectsStore)
	webhookHandler.SetRunner(cfg.Builder)
	projectsHandler := handlers.NewProjectsHandler(cfg.ProjectsStore, cfg.ReposManager, cfg.CredentialStore, cfg.BaseDomain, cfg.Logger)
	buildsHandler := handlers.NewBuildsHandler(cfg.ProjectsStore, cfg.Builder, cfg.DataDir, cfg.Logger)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health
		r.Get("/health", handlers.HealthCheck)

		// Projects (.dev/-based deploys)
		r.Route("/projects", func(r chi.Router) {
			r.Get("/", projectsHandler.List)
			r.Post("/", projectsHandler.Create)
			r.Get("/{id}", projectsHandler.Get)
			r.Get("/{id}/secrets", projectsHandler.ListSecrets)
			r.Put("/{id}/secrets", projectsHandler.SetSecrets)
			r.Delete("/{id}/secrets/{key}", projectsHandler.DeleteSecret)
		})

		// Build trigger (WS log endpoint registered outside /api/v1)
		r.Route("/envs", func(r chi.Router) {
			r.Post("/{id}/build", buildsHandler.Trigger)
		})

		// Webhooks
		r.Post("/webhook/github", webhookHandler.GitHub)
	})

	// WebSocket routes
	r.Get("/ws/envs/{id}/build-logs", buildsHandler.StreamLogs)

	// Static files (frontend)
	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := http.Dir(cfg.StaticDir).Open(r.URL.Path); err != nil {
			http.ServeFile(w, r, filepath.Join(cfg.StaticDir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return r
}
```

- [ ] **Step 3: Verify it doesn't compile yet (handlers + main.go still reference old shape)**

```bash
cd backend
go build ./... 2>&1 | head -30
```

Expected: errors. The build fails because:
- `webhookHandler := handlers.NewWebhookHandler(cfg.Logger)` — current signature is `NewWebhookHandler(gitRepo, stateManager, logger)`. Fixed in Task 4.
- `cmd/server/main.go` references the deleted RouterConfig fields. Fixed in Task 7.

This step is a sanity check — the breakage list tells you what subsequent tasks are doing. **Don't commit yet.**

- [ ] **Step 4: Stage but don't commit**

```bash
git add backend/internal/api/router.go
git status --short
```

You should see `M backend/internal/api/router.go` only.

---

### Task 3: Delete legacy handler files

**Files:**
- Delete: 10 files in `backend/internal/api/handlers/`

- [ ] **Step 1: Delete the legacy handler files**

```bash
cd backend/internal/api/handlers
rm containers.go volumes.go network.go compose.go git.go repos.go github.go exec.go stats.go logs.go
ls
```

Expected remaining files:
```
builds.go
builds_test.go
health.go
projects.go
projects_test.go
webhook.go
webhook_v2_test.go
```

- [ ] **Step 2: Stage deletes**

```bash
cd ../../../..
git add backend/internal/api/handlers/
git status --short | head -15
```

You should see `D backend/internal/api/handlers/{containers,volumes,network,compose,git,repos,github,exec,stats,logs}.go`.

- [ ] **Step 3: Verify build still fails (expected — webhook.go + main.go not fixed yet)**

```bash
cd backend
go build ./... 2>&1 | head -10
```

Expected: errors mentioning `webhook.go` (uses removed types) or `main.go` (references deleted fields). Don't commit yet.

---

### Task 4: Strip legacy code paths from webhook.go

**Files:**
- Modify: `backend/internal/api/handlers/webhook.go`

The current webhook.go has:
- `gitRepo *git.Repository` field
- `stateManager *state.Manager` field
- `composeHandler *ComposeHandler` field (legacy compose rebuild trigger)
- `syncController *sync.Controller` field
- A long legacy decode + sync flow that runs after the new processProjectPush

We're keeping ONLY the new project-push flow + the branch-delete flow (Plan 1 of the original .dev/ rollout's step 6).

- [ ] **Step 1: Read the current webhook.go to verify what's there**

```bash
cat backend/internal/api/handlers/webhook.go | head -50
```

Confirm fields and imports.

- [ ] **Step 2: Rewrite webhook.go**

Write this exact content to `backend/internal/api/handlers/webhook.go`:

```go
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
)

// WebhookHandler handles GitHub webhook events for Project repos.
// Legacy compose-project sync flow has been removed (env-manager v2).
type WebhookHandler struct {
	projectsStore *projects.Store
	runner        *builder.Runner
	logger        *zap.Logger
}

// NewWebhookHandler creates a new webhook handler.
// projectsStore + runner are wired via setters so the handler can be
// constructed before the runner exists (matches the legacy pattern).
func NewWebhookHandler(logger *zap.Logger) *WebhookHandler {
	return &WebhookHandler{logger: logger}
}

// SetProjectsStore wires the projects store.
func (h *WebhookHandler) SetProjectsStore(s *projects.Store) { h.projectsStore = s }

// SetRunner wires the build runner.
func (h *WebhookHandler) SetRunner(r *builder.Runner) { h.runner = r }

// GitHub handles POST /api/v1/webhook/github for both push and delete events.
// X-GitHub-Event header determines which payload shape to expect.
func (h *WebhookHandler) GitHub(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-GitHub-Event")

	switch event {
	case "delete":
		h.handleDelete(w, r)
		return
	case "push", "":
		// fall through; legacy clients sometimes omit the header
	default:
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "event " + event + " not handled"})
		return
	}

	var payload models.WebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	h.logger.Info("Received GitHub push webhook",
		zap.String("ref", payload.Ref),
		zap.String("repo", payload.Repository.FullName),
	)

	status := h.processProjectPush(payload.Repository.CloneURL, payload.Ref, headSHA(payload))
	respondSuccess(w, map[string]string{"status": "ok", "project_status": status})
}

// processProjectPush is called for every push to a known Project repo.
// Creates a preview env if the branch is new and has a .dev/ tree;
// rebuilds an existing env via the builder runner.
func (h *WebhookHandler) processProjectPush(repoURL, ref, headSHA string) string {
	if h.projectsStore == nil || h.runner == nil {
		return ""
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == ref {
		return "" // not a branch push (e.g. tag)
	}

	project, err := h.projectsStore.GetProjectByRepoURL(repoURL)
	if err != nil {
		return "" // unknown repo
	}

	if out, err := projects.FetchOrigin(project.LocalPath); err != nil {
		h.logger.Warn("git fetch failed",
			zap.String("repo", project.LocalPath),
			zap.Error(err),
			zap.String("out", string(out)))
	}

	slug, err := projects.BranchSlug(branch)
	if err != nil {
		h.logger.Warn("invalid branch slug, skipping",
			zap.String("branch", branch), zap.Error(err))
		return ""
	}

	env, err := h.projectsStore.GetEnvironment(project.ID, slug)
	if err != nil && !errors.Is(err, projects.ErrNotFound) {
		h.logger.Error("get env failed", zap.Error(err))
		return ""
	}

	if env == nil {
		if !projects.DevDirExistsForBranch(project.LocalPath, branch) {
			return "no_dev_dir"
		}
		env = &models.Environment{
			ID:          project.ID + "--" + slug,
			ProjectID:   project.ID,
			Branch:      branch,
			BranchSlug:  slug,
			Kind:        models.EnvKindPreview,
			ComposeFile: ".dev/docker-compose.dev.yml",
			Status:      models.EnvStatusPending,
			CreatedAt:   time.Now().UTC(),
		}
		if branch == project.DefaultBranch {
			env.Kind = models.EnvKindProd
			env.ComposeFile = ".dev/docker-compose.prod.yml"
		}
		env.URL = projects.ComposeURL(project, env, "home")
		if err := h.projectsStore.SaveEnvironment(env); err != nil {
			h.logger.Error("save new preview env", zap.Error(err))
			return ""
		}
	}

	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerWebhook,
		SHA:         headSHA,
		StartedAt:   time.Now().UTC(),
		Status:      models.BuildStatusRunning,
	}
	if err := h.projectsStore.SaveBuild(project.ID, build); err != nil {
		h.logger.Error("save build", zap.Error(err))
		return ""
	}
	go h.runner.Build(context.Background(), env, build)
	return "build_enqueued:" + build.ID
}

// handleDelete tears down preview envs when a branch is deleted on GitHub.
// Prod envs are exempt (project-deletion-only).
func (h *WebhookHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Ref        string `json:"ref"`
		RefType    string `json:"ref_type"`
		Repository struct {
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	if payload.RefType != "branch" {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "not branch"})
		return
	}

	if h.projectsStore == nil || h.runner == nil {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "projects not configured"})
		return
	}

	project, err := h.projectsStore.GetProjectByRepoURL(payload.Repository.CloneURL)
	if err != nil {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "unknown repo"})
		return
	}

	slug, err := projects.BranchSlug(payload.Ref)
	if err != nil {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "invalid slug"})
		return
	}

	env, err := h.projectsStore.GetEnvironment(project.ID, slug)
	if err != nil {
		if errors.Is(err, projects.ErrNotFound) {
			respondSuccess(w, map[string]string{"status": "ignored", "reason": "no matching env"})
			return
		}
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	if env.Kind == models.EnvKindProd {
		respondSuccess(w, map[string]string{"status": "ignored", "reason": "prod env exempt from auto-teardown"})
		return
	}

	go func() {
		if err := h.runner.Teardown(context.Background(), env); err != nil {
			h.logger.Error("teardown failed", zap.String("env_id", env.ID), zap.Error(err))
			return
		}
		if err := h.projectsStore.DeleteEnvironment(project.ID, slug); err != nil {
			h.logger.Error("delete env row failed", zap.String("env_id", env.ID), zap.Error(err))
		}
	}()

	respondSuccess(w, map[string]string{"status": "teardown_started", "env_id": env.ID})
}

func headSHA(payload models.WebhookPayload) string {
	if len(payload.Commits) > 0 {
		return payload.Commits[len(payload.Commits)-1].ID
	}
	return ""
}
```

- [ ] **Step 2 (continued): Stage**

```bash
cd ..
git add backend/internal/api/handlers/webhook.go
```

- [ ] **Step 3: Confirm webhook_v2_test.go still references the new shape**

```bash
grep -n "NewWebhookHandler\|SetProjectsStore\|SetRunner" backend/internal/api/handlers/webhook_v2_test.go | head -5
```

Expected: existing test calls match the new constructor (no `gitRepo` / `state.Manager` args). If the test file has `NewWebhookHandler(&git.Repository{}, &state.Manager{}, ...)`, you need to update those tests in Task 5.

---

### Task 5: Update webhook_v2_test.go to use new constructor

**Files:**
- Modify: `backend/internal/api/handlers/webhook_v2_test.go`

- [ ] **Step 1: Find offending lines**

```bash
grep -n "NewWebhookHandler\|git.Repository\|state.Manager" backend/internal/api/handlers/webhook_v2_test.go
```

- [ ] **Step 2: Replace `NewWebhookHandler(...)` calls**

For each occurrence of the old shape, e.g.:

```go
h := NewWebhookHandler(&git.Repository{}, &state.Manager{}, zap.NewNop())
```

Replace with:

```go
h := NewWebhookHandler(zap.NewNop())
```

Use the Edit tool with `replace_all: true` on the file:
- `old_string`: `h := NewWebhookHandler(&git.Repository{}, &state.Manager{}, zap.NewNop())`
- `new_string`: `h := NewWebhookHandler(zap.NewNop())`

If there are multiple variants, repeat per variant.

- [ ] **Step 3: Remove now-unused imports**

The test file may have:

```go
import (
    ...
    "github.com/environment-manager/backend/internal/git"
    "github.com/environment-manager/backend/internal/state"
    ...
)
```

Remove those two lines from the import block.

- [ ] **Step 4: Run the test to verify it compiles**

```bash
cd backend
go build ./internal/api/handlers/ 2>&1 | head -20
```

Expected: `internal/api/handlers/` builds clean. (`main` and `models` may still fail — that's OK for now.)

- [ ] **Step 5: Stage**

```bash
cd ..
git add backend/internal/api/handlers/webhook_v2_test.go
```

---

### Task 6: Slim models package — delete legacy types

**Files:**
- Inspect + selectively delete files in `backend/internal/models/`

- [ ] **Step 1: List models files + check what's still imported**

```bash
ls backend/internal/models/
grep -rn "models\." backend/internal/api backend/internal/builder backend/internal/buildlog backend/internal/projects backend/internal/credentials backend/cmd 2>/dev/null | grep -oE "models\.[A-Z][A-Za-z]*" | sort -u
```

The grep output shows every `models.X` reference in the kept code. Likely to see: `models.Project`, `models.Environment`, `models.Build`, `models.DBSpec`, `models.ExposeSpec`, `models.EnvKind*`, `models.EnvStatus*`, `models.ProjectStatus*`, `models.BuildStatus*`, `models.BuildTrigger*`, `models.WebhookPayload`, `models.WebhookCommit`, `models.WebhookRepository` (if used), `models.CloneRequest` (used by ProjectsHandler.Create via reposManager), `models.Repository` (used by reposManager).

- [ ] **Step 2: Delete files for unused models**

Files in `models/` to assess:

- `project.go` — KEEP (Project, Environment, Build, DBSpec, ExposeSpec, enums)
- `repository.go` — KEEP (Repository + CloneRequest still used by reposManager which is still imported)
- `state.go` — likely contains `WebhookPayload`, `WebhookCommit`, `WebhookRepository`, `DesiredState`, `ContainerState`, `ComposeState`, `SyncResult`. KEEP only the webhook types; if `state.go` mixes them with deleted types, split.
- `compose.go` — DELETE (ComposeProject, ComposeContainer, etc. — gone)
- `container.go` — DELETE
- `volume.go` — DELETE
- `network.go` — DELETE
- `stats.go` — DELETE

For `state.go`: read the file:

```bash
cat backend/internal/models/state.go
```

If it contains `WebhookPayload` definition, edit it to keep ONLY:
- `WebhookPayload` struct
- `WebhookCommit` struct
- `WebhookRepository` struct (or whatever it's called)
- Drop `DesiredState`, `ContainerState`, `ComposeState`, `SyncResult`.

Move the kept webhook types to a new file `backend/internal/models/webhook.go` (clearer name) and delete `state.go`.

- [ ] **Step 3: Execute deletes + writes**

```bash
cd backend/internal/models
rm compose.go container.go volume.go network.go stats.go
```

For `state.go` → `webhook.go` rename: read `state.go`, create `webhook.go` containing only the webhook-related types, then `rm state.go`.

If `state.go` ONLY had webhook types to begin with, just rename:

```bash
git mv state.go webhook.go
```

- [ ] **Step 4: Verify**

```bash
cd ../../..
go build ./internal/models/
go build ./internal/api/...  # should compile, may still fail on missing main.go references
```

`internal/models` should build clean. `internal/api` may still error if any handler references a deleted model — fix those references back in the handler code if you find them (most likely candidate is `repos.go`'s deletion exposing a model still referenced from `repos` package itself, but `repos` is deleted in Task 8 so this is OK).

- [ ] **Step 5: Stage**

```bash
git add backend/internal/models/
git status --short backend/internal/models/
```

---

### Task 7: Cleanup main.go — remove legacy package init

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Replace main.go entirely**

Write this exact content to `backend/cmd/server/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/environment-manager/backend/internal/api"
	"github.com/environment-manager/backend/internal/builder"
	"github.com/environment-manager/backend/internal/config"
	"github.com/environment-manager/backend/internal/credentials"
	"github.com/environment-manager/backend/internal/models"
	"github.com/environment-manager/backend/internal/projects"
	"github.com/environment-manager/backend/internal/repos"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Credential store (encrypted with CREDENTIAL_KEY env var; nil = read-only fallback)
	var credKey []byte
	if key := os.Getenv("CREDENTIAL_KEY"); key != "" {
		credKey = []byte(key)
		if len(credKey) != 32 {
			logger.Warn("CREDENTIAL_KEY should be 32 bytes, token storage disabled")
			credKey = nil
		}
	}
	credStore, err := credentials.NewStore(cfg.DataDir+"/.credentials", credKey)
	if err != nil {
		logger.Warn("Failed to initialize credential store", zap.Error(err))
	}

	// Repository manager (kept for ProjectsHandler.Create which still uses go-git
	// for the initial clone. Plan 5 may inline this and let us delete the package.)
	reposManager, err := repos.NewManager(cfg.DataDir+"/repos", credStore)
	if err != nil {
		logger.Fatal("Failed to initialize repos manager", zap.Error(err))
	}

	// Projects store + reconcile state from previous boot
	projectsStore, err := projects.NewStore(cfg.DataDir)
	if err != nil {
		logger.Fatal("Failed to initialize projects store", zap.Error(err))
	}

	if reconciled, err := projects.MarkStuckBuildsFailed(projectsStore); err != nil {
		logger.Error("Failed to reconcile stuck builds", zap.Error(err))
	} else if reconciled > 0 {
		logger.Info("Marked stuck builds as failed", zap.Int("count", reconciled))
	}

	// Build runner
	buildQueue := builder.NewQueue()
	buildExec := builder.DockerComposeExecutor{}
	buildRunner := builder.NewRunner(projectsStore, buildExec, cfg.DataDir, cfg.ProxyNetwork, buildQueue, logger, credStore)

	// Branch reconcile (fetch origin per project, spawn missing previews, tear down gone branches)
	spawner := &reconcileSpawner{
		store:              projectsStore,
		runner:             buildRunner,
		fallbackBaseDomain: cfg.BaseDomain,
	}
	if summaries, err := projects.ReconcileBranches(context.Background(), projectsStore, spawner, cfg.BaseDomain, logger); err != nil {
		logger.Error("reconcile branches failed", zap.Error(err))
	} else if len(summaries) > 0 {
		logger.Info("Reconcile complete", zap.Strings("changes", summaries))
	}

	// Router
	router := api.NewRouter(api.RouterConfig{
		ReposManager:    reposManager,
		ProjectsStore:   projectsStore,
		Builder:         buildRunner,
		CredentialStore: credStore,
		StaticDir:       cfg.StaticDir,
		DataDir:         cfg.DataDir,
		BaseDomain:      cfg.BaseDomain,
		Logger:          logger,
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("Starting server", zap.Int("port", cfg.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
}

// reconcileSpawner wires projects.ReconcileBranches to the actual Store +
// Runner. Lives in main rather than projects to keep the projects package
// import-free of builder.
type reconcileSpawner struct {
	store              *projects.Store
	runner             *builder.Runner
	fallbackBaseDomain string
}

func (s *reconcileSpawner) SpawnPreview(ctx context.Context, project *models.Project, branch, slug string) error {
	env := &models.Environment{
		ID:          project.ID + "--" + slug,
		ProjectID:   project.ID,
		Branch:      branch,
		BranchSlug:  slug,
		Kind:        models.EnvKindPreview,
		ComposeFile: ".dev/docker-compose.dev.yml",
		Status:      models.EnvStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if branch == project.DefaultBranch {
		env.Kind = models.EnvKindProd
		env.ComposeFile = ".dev/docker-compose.prod.yml"
	}
	env.URL = projects.ComposeURL(project, env, s.fallbackBaseDomain)
	if err := s.store.SaveEnvironment(env); err != nil {
		return err
	}

	build := &models.Build{
		ID:          uuid.NewString(),
		EnvID:       env.ID,
		TriggeredBy: models.BuildTriggerBranchCreate,
		StartedAt:   time.Now().UTC(),
		Status:      models.BuildStatusRunning,
	}
	if err := s.store.SaveBuild(project.ID, build); err != nil {
		return err
	}
	go s.runner.Build(context.Background(), env, build)
	return nil
}

func (s *reconcileSpawner) Teardown(ctx context.Context, env *models.Environment) error {
	return s.runner.Teardown(ctx, env)
}
```

- [ ] **Step 2: Verify build now goes through main**

```bash
cd backend
go build ./...
```

Expected: build may still fail on the `internal/projects` package's reference to `RunLegacyMigration` (which we removed from main but the function still exists). Or the `Loader` reference. Read errors carefully.

If `RunLegacyMigration` errors: that function is still in `internal/projects/migrate.go`. Leaving it unused for now is fine — Go's `unused` warnings are at vet level, not compile errors. But its tests in `migrate_test.go` may import `config.Loader` which we'll be deleting in Task 8. Plan: delete `migrate.go` + `migrate_test.go` in Task 8.

Other likely error: `internal/proxy` may still import `internal/config` (for the Loader). Plan: trim proxy in Task 9.

- [ ] **Step 3: Stage**

```bash
git add backend/cmd/server/main.go
```

---

### Task 8: Delete legacy internal packages

**Files:**
- Delete: `backend/internal/{backup,state,sync,git,stats}/`
- Delete: `backend/internal/projects/migrate.go` + `migrate_test.go` (legacy ComposeProject migration; v2 doesn't migrate)
- Delete: `backend/internal/config/loader.go`

- [ ] **Step 1: Delete the packages**

```bash
cd backend/internal
rm -rf backup state sync git stats
ls
```

Expected remaining: `api builder buildlog config credentials docker models projects proxy repos`.

- [ ] **Step 2: Delete projects/migrate.go + tests**

```bash
rm projects/migrate.go projects/migrate_test.go
```

- [ ] **Step 3: Delete config/loader.go (keep config.go)**

```bash
rm config/loader.go
ls config/
```

Expected: `config.go` only.

- [ ] **Step 4: Verify build**

```bash
cd ../..
go build ./...
```

Likely remaining issues:
- `internal/proxy/manager.go` imports `config.Loader` — fix in Task 9.
- `internal/repos/manager.go` may reference `config.Loader` — read + fix if so.
- `internal/projects/migrate_test.go` is gone; its imports of `config` no longer matter.

If `repos/manager.go` is clean (it should be — it uses `credentials.Store` only), the only blocker is `proxy/manager.go`. Move to Task 9.

- [ ] **Step 5: Stage**

```bash
git add -A
git status --short | head -30
```

You should see `D` lines for everything deleted plus your earlier modifications.

---

### Task 9: Trim proxy package + verify build

**Files:**
- Modify: `backend/internal/proxy/manager.go` (remove config.Loader dependency, remove CoreDNS update flow)
- Possibly: `backend/internal/proxy/registry.go` (subdomain registry — used by legacy network handler; check if still needed)

- [ ] **Step 1: Inspect proxy package**

```bash
ls backend/internal/proxy/
grep -n "config\." backend/internal/proxy/manager.go | head -10
```

The proxy.Manager probably has a constructor like `NewManager(dataDir, baseDomain, traefikIP, proxyNetwork string, registry *Registry, logger *zap.Logger)` with no config.Loader dependency. If so, no changes needed there — the issue is that nothing CALLS `NewManager` anymore (we removed it from main.go), so the compiler is fine with unused exports.

**However**, `manager.go` may have a method `UpdateCoreDNS(ctx)` that imports `config.GenerateCorefile` from the deleted loader.go. If so:

- [ ] **Step 2: Delete the UpdateCoreDNS method**

Read `manager.go`, find `UpdateCoreDNS(ctx context.Context) error`, delete the method body. If it's the only consumer of `config` package import, remove that import too.

If you're unsure, run:

```bash
grep -n "import\|package\|GenerateCorefile\|UpdateCoreDNS" backend/internal/proxy/manager.go | head -20
```

The structure is:
- `package proxy`
- imports including `config`
- `Manager` struct
- `NewManager` constructor
- `UpdateCoreDNS` method (delete this)
- Other methods (`InjectTraefikLabels`, `GenerateTraefikLabels`, etc. — keep)

Use the Edit tool to remove the `UpdateCoreDNS` method and the now-unused `config` import.

- [ ] **Step 3: Verify proxy builds**

```bash
go build ./internal/proxy/
```

Expected: clean.

- [ ] **Step 4: Verify whole module builds**

```bash
go build ./...
```

Expected: clean. If errors remain, read them carefully and fix the specific reference. Common ones:
- `internal/repos/manager.go` may have unused imports (clean those up).
- Any handler test that references deleted types should have been caught in Task 5; if not, fix now.

- [ ] **Step 5: Run all tests**

```bash
go test ./...
```

Expected: every kept package returns `ok` or `[no test files]`. **Do NOT proceed if any test fails.** Diagnose:
- A test in `webhook_v2_test.go` may still reference deleted types.
- A test in `projects/migrate_test.go` was deleted — if `projects/migrate_branches_test.go` (the reconcile tests) somehow imported migrate types, it'll fail. Read errors and fix.

- [ ] **Step 6: Stage + initial commit**

```bash
git add -A
git status --short | head -50
```

You should see a substantial deletion + a few modifications. Commit:

```bash
git commit -m "refactor(v2): delete legacy backend surface

Drops handlers (containers, volumes, network, compose, git, repos,
github, exec, stats, logs), internal packages (backup, state, sync,
git, stats, projects/migrate, config/loader), legacy webhook code
path, and legacy model types. Strips main.go to only Project +
Build + Webhook v2 init. Trims proxy.Manager to drop the deleted
config.Loader-dependent CoreDNS update flow.

Per env-manager v2 design spec (Plan 1 of 8). No new behaviour;
.dev/-based deploys still work, .legacy /api/v1/{repos,compose,...}
endpoints disappear.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Manual verification + rollout-checklist update

- [ ] **Step 1: Build the binary**

```bash
cd backend
go build -o $env:TEMP/env-manager-v2-plan1.exe ./cmd/server
```

Expected: clean build.

- [ ] **Step 2: Quick smoke test against the existing data dir**

The existing host data dir has the migrated projects in it. Run a TEMP DataDir to avoid touching prod state:

```powershell
$tmp = "$env:TEMP/env-manager-v2-plan1-data"
Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $tmp/data -Force | Out-Null
$env:DATA_DIR = "$tmp/data"
$proc = Start-Process -FilePath "$env:TEMP/env-manager-v2-plan1.exe" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
```

- [ ] **Step 3: Verify endpoints**

```powershell
Invoke-RestMethod http://localhost:8080/api/v1/health
Invoke-RestMethod http://localhost:8080/api/v1/projects
# Legacy endpoints should 404
try { Invoke-RestMethod http://localhost:8080/api/v1/repos } catch { Write-Host "Expected: $($_.Exception.Response.StatusCode.value__) for /repos" }
try { Invoke-RestMethod http://localhost:8080/api/v1/compose } catch { Write-Host "Expected: $($_.Exception.Response.StatusCode.value__) for /compose" }
try { Invoke-RestMethod http://localhost:8080/api/v1/containers } catch { Write-Host "Expected: $($_.Exception.Response.StatusCode.value__) for /containers" }
```

Expected:
- `/health` returns ok
- `/projects` returns `[]`
- `/repos`, `/compose`, `/containers` all return 404 (not 200 with HTML — that would mean SPA fallback, which is wrong since the API path should hit the route block first; but if the chi route block doesn't have a `/repos` registered, the catch-all `/*` SPA handler picks it up. That's still a "broken endpoint" outcome and acceptable for transition.)

Actually — chi's route precedence: registered routes win, but UNregistered paths under `/api/v1/*` fall through to the catch-all `/*` static handler at the end. To keep the legacy API endpoints returning 404 (instead of HTML), add this AFTER the `/api/v1` route block:

This is actually a clarification point — leave the SPA fallback as-is for now. Legacy frontend pages just won't work. Plan 7's UI rebuild deletes those pages anyway.

- [ ] **Step 4: Stop the server**

```powershell
Stop-Process -Id $proc.Id -Force
```

- [ ] **Step 5: Append to rollout checklist**

Edit `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` (create if absent):

```markdown
# env-manager v2 — rollout checklist

## Plan 1 — Legacy backend cleanup

After rollout:
- [ ] `data/projects/.migrated` still present (migration was run by v1 — fine to leave)
- [ ] `GET /api/v1/health` returns 200
- [ ] `GET /api/v1/projects` returns array
- [ ] `GET /api/v1/repos` returns 404 or SPA HTML (legacy endpoint gone)
- [ ] `POST /api/v1/webhook/github` still routes (push-to-deploy still works)
- [ ] env-manager container starts without errors in logs ("Starting server")
- [ ] `data/repos/blocksweb-dasboard-laravel` still present and usable
- [ ] stripe-payments builds still trigger correctly via `POST /api/v1/envs/{id}/build`
- [ ] No reference to deleted types in any kept Go file (`go vet ./...` clean)

## Plan 2 — IaC v2 parser
*(populated when plan 2 is written)*

## Plan 3 — Service plane (Postgres + Redis)
*(populated when plan 3 is written)*

## Plan 4 — Pre/post-deploy hooks
*(populated when plan 4 is written)*

## Plan 5 — Custom-domain + Let's Encrypt
*(populated when plan 5 is written)*

## Plan 6 — envm CLI
*(populated when plan 6 is written)*

## Plan 7 — UI rebuild
*(populated when plan 7 is written)*

## Plan 8 — Migration runbook
*(populated when plan 8 is written)*
```

- [ ] **Step 6: Commit checklist**

```bash
git add docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: rollout checklist for env-manager v2"
```

---

## Self-review

After implementing all tasks:

**Spec coverage:**
- Backend packages deleted (backup, state, sync, git, repos[partial], stats, config/loader) — Tasks 8 + 9.
- Handlers deleted (containers, volumes, network, compose, git, repos, github, exec, stats, logs) — Task 3.
- Webhook legacy flow stripped — Task 4.
- main.go restructured — Task 7.
- Models slimmed — Task 6.
- Proxy package trimmed — Task 9.
- Tests still pass — verified end of Task 9.
- Manual verification runbook — Task 10.

**Out-of-scope reminders:**
- Frontend pages still exist; deleted in Plan 7's UI rebuild.
- `repos.Manager` still exists and is used by ProjectsHandler.Create — collapsing it into project-onboarding code happens in Plan 5 (custom-domain plan touches the handler significantly).
- IaC v2 parser, hooks, service plane, CLI — separate plans.

**Known after-effects accepted by this plan:**
- Frontend's legacy pages (`/repos`, `/compose`, etc.) will hit the SPA fallback and not crash the React app, but their data fetches will fail. Operator should avoid those pages until Plan 7.
- `internal/proxy.Manager.UpdateCoreDNS` is gone; CoreDNS Corefile is now edited by env-manager only when needed by the v2 domain plan (Plan 5).
- Legacy migration logic (v1's `RunLegacyMigration` for ComposeProject → Project) is deleted. The migration ran successfully on the host already; the function isn't needed going forward. If you redeploy v2 to a fresh host with old data, the migration won't run automatically — but that's fine because there IS no fresh host with old data; the home lab's data has already been migrated.
