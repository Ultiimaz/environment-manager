# env-manager v2, Plan 6a — Admin auth + envm CLI (secrets commands)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap an admin token on first server boot, gate mutating endpoints behind a `Bearer` auth middleware, and ship a new `envm` CLI binary (built alongside `cmd/server`) with the secrets-management commands operators use daily. Plan 6b ships the rest of the CLI (projects/builds/envs/services).

**Architecture:** On first boot, the server checks the credential store for `system:admin_token`. If absent, it generates a 32-byte hex token, stores it encrypted, and logs it once. A new chi middleware reads `Authorization: Bearer <token>` and compares against the stored value — applied to all POST/PUT/DELETE endpoints under `/api/v1`. Read-only `GET` endpoints stay open on LAN (UI uses anonymous reads).

The CLI is a new Go binary at `cmd/envm`. It loads `~/.envm/config.yaml` (`endpoint` + `token`), constructs an `http.Client` that injects the Bearer header, and dispatches subcommands using stdlib `flag.FlagSet`. One backend endpoint is added: `GET /api/v1/projects/{id}/secrets/{key}?reveal=true` for `envm secrets get`.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3` (already in go.mod) for config-file parsing, no new dependencies. CLI uses stdlib `flag`/`os.Args` dispatch.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "CLI tool (envm) → Auth model" and "CLI tool (envm) → Commands" (secrets subset only).

**Out of scope:**
- `envm projects/builds/envs/services` commands → Plan 6b
- Interactive `envm shell/psql/redis` commands (require TTY allocation + WS upgrade) → deferred indefinitely; operators can `docker exec` directly until needed
- env-manager's own public hostname for receiving webhooks → Plan 8 host op
- GitHub release automation for distributing the binary → out of v2 scope

---

## File structure after this plan

**New files:**

```
backend/cmd/envm/
├── main.go              — subcommand dispatcher + version + config show
├── config.go            — load ~/.envm/config.yaml + Client construction
├── client.go            — HTTP client wrapper with Bearer header + error decoding
├── secrets.go           — list/get/set/delete/import/check
└── secrets_test.go      — unit tests for the secrets subcommands
```

**Modified files:**

```
backend/internal/api/router.go             — Bearer middleware on mutating endpoints
backend/internal/api/handlers/projects.go  — new GetSecret handler (+ test)
backend/internal/api/handlers/projects_test.go — test for GetSecret
backend/internal/api/handlers/auth.go      — NEW middleware file (small)
backend/internal/api/handlers/auth_test.go — NEW middleware test
backend/cmd/server/main.go                 — admin token bootstrap on first boot
Dockerfile                                 — also build + COPY cmd/envm binary
```

---

## Locked details

| Thing | Value |
|---|---|
| Cred-store key for admin token | `system:admin_token` |
| Token format | 32 random bytes hex-encoded → 64 chars, prefix `envm_` so total = `envm_<64hex>` (69 chars) |
| Auth header | `Authorization: Bearer <token>` |
| Middleware applies to | All `POST`/`PUT`/`DELETE` under `/api/v1` (not `/health`, not GET endpoints, not `/ws/*`) |
| Webhook bypass | `POST /api/v1/webhook/github` keeps HMAC auth — Bearer middleware is skipped for it |
| First-boot log line | `==> env-manager admin token: envm_<64hex>` printed at INFO level once |
| Subsequent boots | Existing token used, no log line |
| CLI config file | `~/.envm/config.yaml` (deviates from spec's `.toml` — yaml.v3 already in module deps) |
| CLI config schema | `endpoint: https://manager.local:8080` + `token: envm_...` |
| CLI version output | `envm <version>` where `<version>` is a build-time `-X` flag (default `dev`) |
| `envm secrets get KEY` requires `--reveal` | Yes; without `--reveal` returns the key list (mirrors API) |
| New endpoint: `GET /api/v1/projects/{id}/secrets/{key}` | Returns `{"key": "X", "value": "Y"}` only when `?reveal=true`; otherwise 400 with hint |

---

## Tasks

### Task 1: Branch + admin token bootstrap

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status
git rev-parse HEAD
```

Expected: HEAD at `09d29fc` (Plan 5 merge) or later.

```bash
git checkout -b feat/v2-plan-06a-cli-auth-secrets
```

- [ ] **Step 2: Add admin token bootstrap in `cmd/server/main.go`**

Insert AFTER `credStore` initialization (around line 45) and BEFORE the service-plane bootstrap block:

```go
	// Admin token bootstrap. Generate once on first boot, store encrypted in
	// cred-store under "system:admin_token", log once. Subsequent boots reuse.
	if credStore != nil {
		if _, err := credStore.GetSystemSecret("system:admin_token"); err != nil {
			rawBuf := make([]byte, 32)
			if _, rerr := rand.Read(rawBuf); rerr != nil {
				logger.Error("Failed to generate admin token", zap.Error(rerr))
			} else {
				token := "envm_" + hex.EncodeToString(rawBuf)
				if serr := credStore.SaveSystemSecret("system:admin_token", token); serr != nil {
					logger.Error("Failed to save admin token", zap.Error(serr))
				} else {
					logger.Info("==> env-manager admin token (save it now): " + token)
				}
			}
		}
	}
```

Add to the imports at the top:

```go
	"crypto/rand"
	"encoding/hex"
```

- [ ] **Step 3: Verify the package builds**

```bash
cd backend && go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): generate admin token on first boot

Generates a 32-byte hex admin token (with envm_ prefix) on first
boot when system:admin_token is absent from the credential store.
Persists encrypted; logs once at INFO level so the operator can
copy it to ~/.envm/config.yaml. Subsequent boots reuse silently."
```

---

### Task 2: Bearer auth middleware

**Files:**
- Create: `backend/internal/api/handlers/auth.go`
- Create: `backend/internal/api/handlers/auth_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/api/handlers/auth_test.go`:

```go
package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTokenStore implements the AdminTokenStore interface in-memory.
type fakeTokenStore struct {
	token string
	err   error
}

func (f *fakeTokenStore) GetSystemSecret(key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if key != "system:admin_token" {
		return "", errors.New("not found")
	}
	return f.token, nil
}

func TestBearerAuth_AllowsValidToken(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_abc"})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	req.Header.Set("Authorization", "Bearer envm_abc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected handler to be invoked")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestBearerAuth_RejectsMissingHeader(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_abc"})
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("expected handler not to be invoked")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_RejectsWrongToken(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_correct"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	req.Header.Set("Authorization", "Bearer envm_wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBearerAuth_RejectsMalformedHeader(t *testing.T) {
	mw := BearerAuth(&fakeTokenStore{token: "envm_abc"})
	cases := []string{
		"envm_abc",         // no Bearer prefix
		"Basic envm_abc",   // wrong scheme
		"Bearer",           // no token
		"Bearer  envm_abc", // double space — accepted by strings.TrimPrefix? verify
	}
	for _, h := range cases {
		t.Run(h, func(t *testing.T) {
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler invoked for malformed header")
			}))
			req := httptest.NewRequest("POST", "/api/v1/foo", nil)
			req.Header.Set("Authorization", h)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 for header %q", rec.Code, h)
			}
		})
	}
}

func TestBearerAuth_FailOpenWhenStoreUnavailable(t *testing.T) {
	// If cred-store fails (e.g. disk error), we should NOT serve the request —
	// fail closed. Returns 503 (service unavailable) rather than 401 because
	// the auth state is unknown, not denied.
	mw := BearerAuth(&fakeTokenStore{err: errors.New("disk error")})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler invoked despite cred-store failure")
	}))

	req := httptest.NewRequest("POST", "/api/v1/foo", nil)
	req.Header.Set("Authorization", "Bearer envm_anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when cred-store unavailable", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "credential store") && !strings.Contains(body, "unavailable") {
		t.Errorf("body should mention cred-store or unavailable, got %q", body)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/api/handlers/... -run TestBearerAuth -v
```

Expected: compile errors — `BearerAuth` not defined.

- [ ] **Step 3: Implement `auth.go`**

Create `backend/internal/api/handlers/auth.go`:

```go
package handlers

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// AdminTokenStore exposes only the admin-token read needed by the middleware.
// Implemented by *credentials.Store.
type AdminTokenStore interface {
	GetSystemSecret(key string) (string, error)
}

// BearerAuth returns a chi-compatible middleware that gates handlers behind
// the Authorization: Bearer <token> header. The expected token is read from
// the credential store on every request — cheap because Store.GetSystemSecret
// is a sync.RWMutex-protected map lookup with at most one disk read.
//
// Behaviour:
//   - Missing/malformed header → 401 Unauthorized
//   - Token mismatch → 401 Unauthorized
//   - Cred-store unavailable (read error) → 503 Service Unavailable
//   - Token match → handler invoked
//
// Apply to mutating routes only. Read-only GETs stay open on LAN per the v2
// design — the UI uses anonymous reads.
func BearerAuth(store AdminTokenStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expected, err := store.GetSystemSecret("system:admin_token")
			if err != nil {
				respondError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "credential store unavailable: "+err.Error())
				return
			}

			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or malformed Authorization header")
				return
			}
			given := strings.TrimPrefix(h, "Bearer ")
			given = strings.TrimSpace(given)
			if given == "" {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "empty bearer token")
				return
			}
			// Constant-time comparison to avoid timing attacks.
			if subtle.ConstantTimeCompare([]byte(given), []byte(expected)) != 1 {
				respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run all auth tests**

```bash
cd backend && go test ./internal/api/handlers/... -run TestBearerAuth -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/handlers/auth.go backend/internal/api/handlers/auth_test.go
git commit -m "feat(api): Bearer auth middleware for mutating endpoints

Reads system:admin_token from cred-store and compares (constant-
time) with the Authorization: Bearer header. 401 on mismatch,
503 when cred-store unavailable. Used by router.go to gate
POST/PUT/DELETE — read-only GETs stay open on LAN per v2 design."
```

---

### Task 3: Apply Bearer middleware in router

**Files:**
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Wire the middleware**

In `backend/internal/api/router.go`, modify the `r.Route("/api/v1", ...)` block. Replace:

```go
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
```

With:

```go
	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health (open)
		r.Get("/health", handlers.HealthCheck)

		// Webhooks (HMAC-secured separately — Bearer middleware does NOT apply)
		r.Post("/webhook/github", webhookHandler.GitHub)

		// Read-only project endpoints (open on LAN per v2 design)
		r.Get("/projects", projectsHandler.List)
		r.Get("/projects/{id}", projectsHandler.Get)
		r.Get("/projects/{id}/secrets", projectsHandler.ListSecrets)

		// Mutating endpoints — require admin token. Bearer middleware skipped
		// when credStore is nil (early-boot / no-key dev mode); in that mode
		// the token is unset and the previous behaviour (open) is preserved.
		if cfg.CredentialStore != nil {
			r.Group(func(r chi.Router) {
				r.Use(handlers.BearerAuth(cfg.CredentialStore))
				r.Post("/projects", projectsHandler.Create)
				r.Put("/projects/{id}/secrets", projectsHandler.SetSecrets)
				r.Delete("/projects/{id}/secrets/{key}", projectsHandler.DeleteSecret)
				r.Post("/envs/{id}/build", buildsHandler.Trigger)
			})
		} else {
			r.Post("/projects", projectsHandler.Create)
			r.Put("/projects/{id}/secrets", projectsHandler.SetSecrets)
			r.Delete("/projects/{id}/secrets/{key}", projectsHandler.DeleteSecret)
			r.Post("/envs/{id}/build", buildsHandler.Trigger)
		}
	})
```

- [ ] **Step 2: Run all tests (existing handler tests must still pass — they construct a router with no credStore so the open path runs)**

```bash
cd backend && go test ./...
```

Expected: all PASS.

If any handler test fails because it now expects a 401 from a previously-200 endpoint, the test was relying on the credStore's previous middleware-less wiring; check that the test setup leaves `cfg.CredentialStore == nil` (open path).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/router.go
git commit -m "feat(api): apply Bearer auth to mutating /api/v1 endpoints

POST/PUT/DELETE under /api/v1 now require Authorization: Bearer
<admin-token>. GETs stay open on LAN. The webhook keeps HMAC
auth (no Bearer required). When CredentialStore is nil (legacy/
dev mode), the middleware is skipped — preserves existing
behaviour for tests that don't set up the cred-store."
```

---

### Task 4: New backend endpoint — `GET /projects/{id}/secrets/{key}?reveal=true`

**Files:**
- Modify: `backend/internal/api/handlers/projects.go`
- Modify: `backend/internal/api/handlers/projects_test.go`
- Modify: `backend/internal/api/router.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/api/handlers/projects_test.go`:

```go
func TestProjectsHandler_GetSecret(t *testing.T) {
	dir := t.TempDir()
	store, _ := projects.NewStore(dir)
	credKey := make([]byte, 32)
	for i := range credKey {
		credKey[i] = byte(i)
	}
	creds, err := credentials.NewStore(filepath.Join(dir, "creds.json"), credKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SaveProject(&models.Project{ID: "p1", Name: "myapp"})
	_ = creds.SaveProjectSecret("p1", "STRIPE_KEY", "sk_test_xyz")

	h := NewProjectsHandler(store, nil, creds, "home", zap.NewNop())

	t.Run("without reveal param returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/projects/p1/secrets/STRIPE_KEY", nil)
		req = withChiURLParams(req, map[string]string{"id": "p1", "key": "STRIPE_KEY"})
		rec := httptest.NewRecorder()
		h.GetSecret(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("with reveal=true returns the value", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/projects/p1/secrets/STRIPE_KEY?reveal=true", nil)
		req = withChiURLParams(req, map[string]string{"id": "p1", "key": "STRIPE_KEY"})
		rec := httptest.NewRecorder()
		h.GetSecret(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		var resp map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp["value"] != "sk_test_xyz" {
			t.Errorf("value = %q, want sk_test_xyz", resp["value"])
		}
	})

	t.Run("unknown key returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/projects/p1/secrets/MISSING?reveal=true", nil)
		req = withChiURLParams(req, map[string]string{"id": "p1", "key": "MISSING"})
		rec := httptest.NewRecorder()
		h.GetSecret(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
}

// withChiURLParams installs URL params into a request's chi RouteContext so
// handlers calling chi.URLParam(r, "id") get the expected value during tests.
func withChiURLParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
```

Add the imports to `projects_test.go` if missing: `"context"`, `"github.com/go-chi/chi/v5"`. Most of the others (`encoding/json`, `httptest`, `path/filepath`, etc.) likely already present.

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/api/handlers/... -run TestProjectsHandler_GetSecret -v
```

Expected: compile error — `(*ProjectsHandler).GetSecret` not defined.

- [ ] **Step 3: Implement `GetSecret` handler in `projects.go`**

Append to `backend/internal/api/handlers/projects.go`:

```go
// GetSecret handles GET /api/v1/projects/{id}/secrets/{key}.
//
// Requires query parameter `reveal=true` to return the value — without it,
// returns 400 with a hint to use ListSecrets for the key list. This guards
// against accidental exposure (e.g. someone sharing a screenshot of an
// HTTP-debug tool).
func (h *ProjectsHandler) GetSecret(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	key := chi.URLParam(r, "key")
	if id == "" || key == "" {
		respondError(w, http.StatusBadRequest, "MISSING_PARAM", "id and key required")
		return
	}
	if r.URL.Query().Get("reveal") != "true" {
		respondError(w, http.StatusBadRequest, "REVEAL_REQUIRED", "add ?reveal=true to retrieve the secret value (use /secrets to list keys)")
		return
	}
	if h.credStore == nil {
		respondError(w, http.StatusInternalServerError, "NO_CREDENTIAL_STORE", "credential store not configured")
		return
	}
	value, err := h.credStore.GetProjectSecret(id, key)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "secret not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"key":   key,
		"value": value,
	})
}
```

- [ ] **Step 4: Wire the route in `router.go`**

Find the read-only project endpoints block (added in Task 3):

```go
		r.Get("/projects", projectsHandler.List)
		r.Get("/projects/{id}", projectsHandler.Get)
		r.Get("/projects/{id}/secrets", projectsHandler.ListSecrets)
```

Add a fourth GET, but inside the Bearer-protected group (revealing a secret value is mutating-equivalent — protect it):

In the Bearer-protected block, add:

```go
		r.Get("/projects/{id}/secrets/{key}", projectsHandler.GetSecret)
```

(Yes — chi allows `Get` inside a Group with auth middleware. The Bearer middleware applies to all routes in the group.)

So the protected group becomes:

```go
		if cfg.CredentialStore != nil {
			r.Group(func(r chi.Router) {
				r.Use(handlers.BearerAuth(cfg.CredentialStore))
				r.Post("/projects", projectsHandler.Create)
				r.Get("/projects/{id}/secrets/{key}", projectsHandler.GetSecret)
				r.Put("/projects/{id}/secrets", projectsHandler.SetSecrets)
				r.Delete("/projects/{id}/secrets/{key}", projectsHandler.DeleteSecret)
				r.Post("/envs/{id}/build", buildsHandler.Trigger)
			})
		} else {
			r.Post("/projects", projectsHandler.Create)
			r.Get("/projects/{id}/secrets/{key}", projectsHandler.GetSecret)
			r.Put("/projects/{id}/secrets", projectsHandler.SetSecrets)
			r.Delete("/projects/{id}/secrets/{key}", projectsHandler.DeleteSecret)
			r.Post("/envs/{id}/build", buildsHandler.Trigger)
		}
```

- [ ] **Step 5: Run all tests**

```bash
cd backend && go test ./...
```

Expected: all PASS, including 3 new GetSecret subtests.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/handlers/projects.go backend/internal/api/handlers/projects_test.go backend/internal/api/router.go
git commit -m "feat(api): GET /projects/{id}/secrets/{key} returns value when ?reveal=true

New endpoint for envm secrets get. Requires explicit ?reveal=true
query param to surface the decrypted value — guards against
accidental exposure in HTTP debug tools / screenshots. Sits
inside the Bearer-protected group; revealing a secret is auth-
gated even though it's a GET."
```

---

### Task 5: CLI scaffold — `cmd/envm` with `version` and `config show`

**Files:**
- Create: `backend/cmd/envm/main.go`
- Create: `backend/cmd/envm/config.go`
- Create: `backend/cmd/envm/client.go`

- [ ] **Step 1: Write `config.go`**

Create `backend/cmd/envm/config.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the user's ~/.envm/config.yaml. Both fields are required
// for any command that talks to the API.
type Config struct {
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`
}

// loadConfig reads ~/.envm/config.yaml. Returns a clear error message when
// the file is absent (with instructions for fixing it) so first-time users
// don't have to grep through code.
func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home dir: %w", err)
	}
	path := filepath.Join(home, ".envm", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s — create it with:\n\nmkdir -p ~/.envm && cat > ~/.envm/config.yaml <<EOF\nendpoint: https://manager.example.com\ntoken: envm_<paste-from-server-startup-log>\nEOF", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Endpoint == "" {
		return nil, fmt.Errorf("config %s: endpoint required", path)
	}
	if c.Token == "" {
		return nil, fmt.Errorf("config %s: token required", path)
	}
	return &c, nil
}
```

- [ ] **Step 2: Write `client.go`**

Create `backend/cmd/envm/client.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin wrapper around http.Client that injects the Bearer
// header and decodes JSON responses + error envelopes.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// NewClient constructs a Client. endpoint is the env-manager base URL
// (e.g. https://manager.blocksweb.nl); the API path is appended per call.
func NewClient(cfg *Config) *Client {
	return &Client{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		token:    cfg.Token,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Do issues a request to the env-manager API. body is JSON-encoded if non-nil.
// out is JSON-decoded if non-nil and the response is 2xx.
//
// Non-2xx responses are returned as a formatted error including status code
// and the server's error envelope (or response body when not JSON).
func (c *Client) Do(method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequest(method, c.endpoint+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
		}
	}
	return nil
}
```

- [ ] **Step 3: Write `main.go`**

Create `backend/cmd/envm/main.go`:

```go
// Command envm is the env-manager CLI. Subcommand-per-noun pattern:
//
//	envm secrets list <project>
//	envm secrets set <project> KEY=VALUE [...]
//	envm secrets get <project> KEY --reveal
//	envm secrets delete <project> KEY
//	envm secrets import <project> path/to/.env
//	envm secrets check <project>
//	envm config show
//	envm version
//
// Project/build/env management commands ship in Plan 6b.
//
// Configuration: ~/.envm/config.yaml with `endpoint:` and `token:` fields.
// The token is generated by env-manager on first boot and printed once to
// the server log.
package main

import (
	"fmt"
	"os"
)

// version is set at build time via `-ldflags "-X main.version=..."`.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Println("envm", version)
		return
	case "config":
		runConfig(os.Args[2:])
	case "secrets":
		runSecrets(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `envm — env-manager CLI

Usage:
  envm secrets list <project>
  envm secrets set <project> KEY=VALUE [...]
  envm secrets get <project> KEY --reveal
  envm secrets delete <project> KEY
  envm secrets import <project> path/to/.env
  envm secrets check <project>
  envm config show
  envm version

Configuration: ~/.envm/config.yaml
  endpoint: https://manager.example.com
  token: envm_<from-server-startup-log>`)
}

// runConfig dispatches `envm config <subcommand>`.
func runConfig(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm config show")
		os.Exit(2)
	}
	switch args[0] {
	case "show":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// Don't print the token — show only its presence + length.
		fmt.Printf("endpoint: %s\n", cfg.Endpoint)
		fmt.Printf("token:    %s (length=%d)\n", maskToken(cfg.Token), len(cfg.Token))
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand %q\n", args[0])
		os.Exit(2)
	}
}

// maskToken returns "envm_xxxx...yyyy" preserving the prefix and last 4 chars.
func maskToken(token string) string {
	if len(token) <= 12 {
		return "<short>"
	}
	return token[:5] + "xxxx..." + token[len(token)-4:]
}
```

- [ ] **Step 4: Verify the binary builds**

```bash
cd backend && go build -o /tmp/envm ./cmd/envm
```

Expected: clean build. Try it (test config-not-found path):

```bash
/tmp/envm version
# Expected: envm dev
```

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/envm/main.go backend/cmd/envm/config.go backend/cmd/envm/client.go
git commit -m "feat(cmd/envm): scaffold CLI binary with config + http client

cmd/envm is a new Go binary built from the existing backend
module. Loads ~/.envm/config.yaml (endpoint + token), constructs
an http.Client that injects the Bearer header, and dispatches
subcommands. Ships 'version' and 'config show' commands; secrets
subcommands land in the next commit."
```

---

### Task 6: `envm secrets` commands

**Files:**
- Create: `backend/cmd/envm/secrets.go`
- Create: `backend/cmd/envm/secrets_test.go`

- [ ] **Step 1: Write `secrets.go`**

Create `backend/cmd/envm/secrets.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// runSecrets dispatches `envm secrets <subcommand>`.
func runSecrets(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets <list|set|get|delete|import|check> <project> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		secretsList(args[1:])
	case "set":
		secretsSet(args[1:])
	case "get":
		secretsGet(args[1:])
	case "delete":
		secretsDelete(args[1:])
	case "import":
		secretsImport(args[1:])
	case "check":
		secretsCheck(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown secrets subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func mustClient() *Client {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return NewClient(cfg)
}

func mustProjectArg(args []string, usage string) string {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: "+usage)
		os.Exit(2)
	}
	return args[0]
}

// secretsList — GET /api/v1/projects/{id}/secrets.
func secretsList(args []string) {
	project := mustProjectArg(args, "envm secrets list <project>")
	c := mustClient()
	var resp struct {
		Keys []string `json:"keys"`
	}
	if err := c.Do("GET", "/api/v1/projects/"+url.PathEscape(project)+"/secrets", nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sort.Strings(resp.Keys)
	for _, k := range resp.Keys {
		fmt.Println(k)
	}
}

// secretsSet — PUT /api/v1/projects/{id}/secrets with a body of KEY=VALUE pairs.
func secretsSet(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets set <project> KEY=VALUE [KEY=VALUE...]")
		os.Exit(2)
	}
	project := args[0]
	pairs := args[1:]
	body := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "skipping malformed pair %q (expected KEY=VALUE)\n", p)
			continue
		}
		body[k] = v
	}
	if len(body) == 0 {
		fmt.Fprintln(os.Stderr, "no valid KEY=VALUE pairs provided")
		os.Exit(2)
	}
	c := mustClient()
	var resp struct {
		SavedKeys []string `json:"saved_keys"`
		Count     int      `json:"count"`
	}
	if err := c.Do("PUT", "/api/v1/projects/"+url.PathEscape(project)+"/secrets", body, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("saved %d secret(s)\n", resp.Count)
}

// secretsGet — GET /api/v1/projects/{id}/secrets/{key}?reveal=true.
// Requires explicit --reveal flag on the CLI side too (mirroring API contract).
func secretsGet(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets get <project> KEY --reveal")
		os.Exit(2)
	}
	project := args[0]
	key := args[1]
	hasReveal := false
	for _, a := range args[2:] {
		if a == "--reveal" {
			hasReveal = true
		}
	}
	if !hasReveal {
		fmt.Fprintln(os.Stderr, "envm secrets get requires --reveal to print the value")
		os.Exit(2)
	}
	c := mustClient()
	var resp struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	path := "/api/v1/projects/" + url.PathEscape(project) + "/secrets/" + url.PathEscape(key) + "?reveal=true"
	if err := c.Do("GET", path, nil, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(resp.Value)
}

// secretsDelete — DELETE /api/v1/projects/{id}/secrets/{key}.
func secretsDelete(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets delete <project> KEY")
		os.Exit(2)
	}
	project := args[0]
	key := args[1]
	c := mustClient()
	if err := c.Do("DELETE", "/api/v1/projects/"+url.PathEscape(project)+"/secrets/"+url.PathEscape(key), nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("deleted")
}

// secretsImport reads .env-style KEY=VALUE lines from a file (or stdin if "-")
// and bulk-sets them via PUT /secrets.
func secretsImport(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: envm secrets import <project> path/to/.env (use - for stdin)")
		os.Exit(2)
	}
	project := args[0]
	source := args[1]
	pairs, err := parseEnvFile(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "no KEY=VALUE lines found")
		os.Exit(1)
	}
	c := mustClient()
	var resp struct {
		Count int `json:"count"`
	}
	if err := c.Do("PUT", "/api/v1/projects/"+url.PathEscape(project)+"/secrets", pairs, &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("imported %d secret(s)\n", resp.Count)
}

// secretsCheck compares the project's iac-declared secrets list (TBD: read
// from server) against the keys currently set, surfacing both missing-but-
// required and set-but-not-declared. Plan 6a: simplest implementation —
// list set keys (relies on operator's eyeballs to compare against
// .dev/config.yaml). Plan 6b can add a server-side endpoint that returns
// the iac-declared list for proper diff. For now, "check" is an alias for
// "list" with a note.
func secretsCheck(args []string) {
	project := mustProjectArg(args, "envm secrets check <project>")
	fmt.Fprintln(os.Stderr, "(Plan 6a: 'check' currently lists set keys; full iac-vs-set diff lands in Plan 6b)")
	secretsList([]string{project})
}

// parseEnvFile reads a .env-style file (KEY=VALUE per line; # comments;
// optional `export ` prefix). Returns a map of all keys found.
func parseEnvFile(path string) (map[string]string, error) {
	var f *os.File
	if path == "-" {
		f = os.Stdin
	} else {
		var err error
		f, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
	}
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if k != "" {
			out[k] = v
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}
```

- [ ] **Step 2: Write `secrets_test.go`**

Create `backend/cmd/envm/secrets_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# Comment line
KEY1=value1
export KEY2=value2
KEY3="quoted value"

KEY4=
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
		"KEY3": "quoted value",
		"KEY4": "",
	}
	for k, v := range wants {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseEnvFile_FileNotFound(t *testing.T) {
	_, err := parseEnvFile("/nonexistent/path/.env")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("expected error to mention open; got %q", err.Error())
	}
}

func TestMaskToken(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"envm_abcd1234567890efgh", "envm_xxxx...efgh"},
		{"short", "<short>"},
		{"", "<short>"},
	}
	for _, tc := range cases {
		got := maskToken(tc.in)
		if got != tc.want {
			t.Errorf("maskToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd backend && go test ./cmd/envm/... -v
```

Expected: all PASS.

- [ ] **Step 4: Verify the binary builds with all subcommands wired**

```bash
cd backend && go build -o /tmp/envm ./cmd/envm
/tmp/envm
# Expected: usage with secrets list/set/get/delete/import/check + config show + version
```

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/envm/secrets.go backend/cmd/envm/secrets_test.go
git commit -m "feat(cmd/envm): secrets list/set/get/delete/import/check

Six secrets subcommands wrap the /api/v1/projects/{id}/secrets/*
endpoints. envm secrets get requires --reveal (mirrors the API's
?reveal=true guard). envm secrets import parses .env-style files
(handles # comments, export prefix, quoted values) and bulk-PUTs.
envm secrets check is a stub today — lists set keys with a note;
proper iac-vs-set diff lands in Plan 6b after the iac inspect
endpoint exists."
```

---

### Task 7: Dockerfile — also build cmd/envm

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Update the backend builder stage**

Find the line:

```
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server
```

Replace with:

```
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -o /envm ./cmd/envm
```

In the final image stage, after the existing `COPY --from=backend-builder /server /app/server` line, add:

```
COPY --from=backend-builder /envm /usr/local/bin/envm
```

This makes `envm` available inside the env-manager container so `docker exec env-manager envm secrets list <project>` works for emergency on-host operator use.

- [ ] **Step 2: Verify Dockerfile syntax**

```bash
docker build -f Dockerfile . --target backend-builder 2>&1 | tail -20
```

(Optional — only if Docker is available. The CI / live image rebuild on the home-lab will surface real syntax issues.)

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "build(docker): also compile cmd/envm and copy into image

The envm CLI lives at /usr/local/bin/envm in the env-manager
container so 'docker exec env-manager envm ...' works for
operator emergency use. Operators on their laptops should still
'go install ./cmd/envm' and put the result in their PATH for
day-to-day work."
```

---

### Task 8: Final sanity + plan/checklist commit

**Files:**
- Modify: `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`

- [ ] **Step 1: Run the full backend suite + vet + build**

```bash
cd backend && go test ./... -count=1 && go vet ./... && go build ./...
```

Expected: all green, all clean.

- [ ] **Step 2: Sanity-check the diff**

```bash
git diff --stat 09d29fc..HEAD
git log --oneline 09d29fc..HEAD
```

Expected: 7 commits (Tasks 1-7).

- [ ] **Step 3: Update rollout checklist**

Replace the Plan 6 placeholder in the rollout checklist with:

```markdown
## Plan 6a — Admin auth + envm CLI (secrets commands)

After merge + redeploy:
- [ ] `cd backend && go test ./...` — full suite green
- [ ] env-manager startup log shows `==> env-manager admin token (save it now): envm_<64hex>` once on first boot only
- [ ] Subsequent boots reuse the stored token without logging it
- [ ] `curl -X POST -H "Authorization: Bearer wrong" https://<env-manager>/api/v1/projects` → 401
- [ ] `curl -X POST -H "Authorization: Bearer <correct>" -H "Content-Type: application/json" --data '{"repo_url":"..."}' https://<env-manager>/api/v1/projects` → 201
- [ ] GET endpoints (e.g. `/projects`) still work without Authorization
- [ ] Webhook (`POST /webhook/github` with valid HMAC) still works without Bearer
- [ ] `go install github.com/environment-manager/backend/cmd/envm@<branch>` produces a binary named `envm`
- [ ] `envm version` prints `envm <version>`
- [ ] `~/.envm/config.yaml` populated with endpoint + token; `envm config show` displays endpoint + masked token
- [ ] `envm secrets list <project>` returns keys for a known project
- [ ] `envm secrets set <project> KEY=value` succeeds and `envm secrets list` shows the new key
- [ ] `envm secrets get <project> KEY --reveal` prints the value
- [ ] `envm secrets get <project> KEY` (no `--reveal`) errors with a clear message
- [ ] `envm secrets delete <project> KEY` removes the key
- [ ] `envm secrets import <project> .env-fixture` bulk-imports
- [ ] env-manager Docker image now ships `/usr/local/bin/envm` — `docker exec env-manager envm version` works

## Plan 6b — Project/build/env/services CLI commands

*(populated when Plan 6b is written)*
```

- [ ] **Step 4: Commit plan + checklist**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-06a-cli-auth-secrets.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 06a (cli auth + secrets)

Plan document + Plan 6a entry in the rollout checklist.
Implementation lands in the preceding 7 commits on this branch.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Push branch + open PR

- [ ] **Step 1: Push**

```bash
git push -u origin feat/v2-plan-06a-cli-auth-secrets
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "v2 plan 06a: admin auth + envm CLI (secrets)" --body "$(cat <<'EOF'
## Summary

- Admin token bootstrap: env-manager generates an \`envm_<64hex>\` token on first boot, stores it encrypted in the credential store under \`system:admin_token\`, and logs it once at INFO level
- Bearer auth middleware (\`handlers.BearerAuth\`) gates all POST/PUT/DELETE under \`/api/v1\`. GET endpoints stay open on LAN for the UI's anonymous reads. Webhook keeps HMAC auth (no Bearer required)
- New endpoint: \`GET /api/v1/projects/{id}/secrets/{key}?reveal=true\` returns the decrypted value (Bearer-protected; explicit \`reveal=true\` query param required)
- New \`cmd/envm\` Go binary built alongside cmd/server. Loads \`~/.envm/config.yaml\` (endpoint + token), constructs an HTTP client with the Bearer header
- Secrets subcommands: \`envm secrets list/set/get/delete/import/check\`. \`get\` requires \`--reveal\`. \`import\` parses .env-style files (comments, export prefix, quotes)
- \`envm version\`, \`envm config show\` (token masked)
- Dockerfile compiles cmd/envm and copies it to \`/usr/local/bin/envm\` inside the image so \`docker exec env-manager envm ...\` works

## Out of scope (deferred)

- \`envm projects/builds/envs/services\` commands → Plan 6b
- Interactive \`envm shell/psql/redis\` → deferred indefinitely (operators can \`docker exec\` directly)
- env-manager's own public hostname (\`manager.blocksweb.nl\`) → Plan 8 host op
- GitHub release automation → out of v2 scope

## Test plan

- [x] \`cd backend && go test ./...\` — full suite green
- [x] \`cd backend && go vet ./...\` — clean
- [x] \`cd backend && go build ./...\` — clean
- [x] \`cd backend && go build -o /tmp/envm ./cmd/envm\` — produces a working binary

After merge, manual home-lab verification per the rollout checklist Plan 6a section. **Operator must update the redeploy script's env vars to capture the printed token on first deploy.**

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report PR URL back to the user**

---

## Acceptance criteria

- [ ] Admin token generated on first boot, stored under `system:admin_token`, logged once
- [ ] `BearerAuth` middleware: 200 on valid token, 401 on missing/malformed/wrong, 503 on cred-store error
- [ ] Mutating endpoints under `/api/v1` (POST/PUT/DELETE + reveal-GET) are gated; GET-list endpoints stay open
- [ ] Webhook (`/webhook/github`) skips Bearer auth (HMAC-secured)
- [ ] `GET /projects/{id}/secrets/{key}?reveal=true` returns the value; without `?reveal=true` returns 400
- [ ] `cmd/envm` binary builds; `envm version` prints version; `envm config show` masks token; secrets subcommands work end-to-end
- [ ] `parseEnvFile` handles comments, blank lines, `export` prefix, quoted values
- [ ] Dockerfile compiles + copies `envm` to `/usr/local/bin/envm`
- [ ] `go test ./...` clean, `go vet ./...` clean, `go build ./...` clean
- [ ] Branch is 8 commits ahead of master (7 implementation + 1 docs)
- [ ] PR opened with the test-plan checklist
- [ ] Rollout checklist updated for Plan 6a

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/`.
- **Never use `> nul`, `> NUL`, or `> /dev/null`** — destructive on this Windows host.
- **TDD discipline:** every feat task → write failing test → run-fail → implement → run-pass → commit.
- **Commit cadence:** one commit per task (7 task commits + 1 docs commit). Don't squash. Don't amend.
- **`respondError` and friends** already exist in `internal/api/handlers/health.go` — reuse don't redefine.
- **chi route group with middleware:** the pattern is `r.Group(func(r chi.Router) { r.Use(mw); ...routes... })` — already shown in plan.
- **Don't try to be clever about the open-when-credStore-nil branch.** It's a dev-mode escape hatch matching the rest of the codebase's pattern (cred-store is optional). The test suite uses it; don't break it.
- **Token format `envm_<64hex>`** — the prefix is part of the token; constant-time compare is over the full string. Don't strip it on the server side.
- **CLI tests** are limited to `parseEnvFile` and `maskToken` — pure functions. Anything that talks to the API would need a mock server; defer to Plan 6b if needed.
