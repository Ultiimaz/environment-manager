# env-manager v2, Plan 5 — Multi-domain Traefik labels with Let's Encrypt

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate Traefik routers from `iac.Config.Domains` so a project can declare custom public domains (`blocksweb.nl`, `www.blocksweb.nl`) and an optional preview-domain pattern (`{branch}.stripe-payments.blocksweb.nl`). Public domains get HTTPS routers with Let's Encrypt resolvers and HTTP→HTTPS redirect routers. Internal `.home` domains keep their HTTP-only behaviour. Best-effort fallback preserved: when iac config is absent (or LE email unset), the runner emits the legacy single-router behaviour without crashing.

**Architecture:** The existing `InjectTraefikLabels` is extended to take a `TraefikOptions` struct (`ProxyNetwork`, `Domains *iac.Domains`, `LetsencryptEmail`). When `Domains == nil` (or empty), the function generates the legacy single HTTP router on `env.URL` — exact existing behaviour. When `Domains` carries entries, the function generates:

- One `<env_id>-home` HTTP router on `web` entrypoint for `env.URL` (the auto `.home` hostname).
- One `<env_id>-public` HTTPS router on `websecure` entrypoint for the union of `Domains.Prod` (prod envs) or the resolved `Domains.Preview.Pattern` (preview envs), with `tls.certresolver=letsencrypt`.
- One `<env_id>-public-http` HTTP router on `web` entrypoint for the same hosts, with a `redirectscheme` middleware redirecting to HTTPS.

If `LetsencryptEmail == ""`, public domains fall back to plain HTTP routers (no TLS, no redirect) and the runner logs a warning so the operator notices.

`cmd/server/main.go` reads `LETSENCRYPT_EMAIL` from the environment via `config.Config` and passes it to `Runner.SetLetsencryptEmail`. The runner builds the `TraefikOptions` from its captured `*iac.Config`.

**What works after this plan ships:**
- A project with `domains.prod: [blocksweb.nl, www.blocksweb.nl]` deploys with HTTPS routers on those hostnames and HTTP→HTTPS redirect, in addition to the auto `<project>.home` router
- Preview env with `domains.preview.pattern: "{branch}.stripe-payments.blocksweb.nl"` gets `<branch_slug>.stripe-payments.blocksweb.nl` resolved at deploy time
- Projects without iac config (or older v1 schemas) keep working — single HTTP router on the legacy `env.URL`
- Missing `LETSENCRYPT_EMAIL` → public domains still routable over HTTP; build log warns

**Tech Stack:** Go 1.24, no new dependencies. Existing packages: `internal/iac` (Plan 2), `internal/builder`, `internal/config`.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Domain management", "Domain + TLS management → Sources of domains / Per-router labels", and "Implementation decomposition" row 5.

**Out of scope (deferred):**
- Traefik command-flag bootstrap (`--certificatesresolvers.letsencrypt.*`) on `env-traefik` startup → Plan 8 migration runbook (one-time host op)
- Cross-project domain conflict check → can land as a Plan 5b polish or merge into Plan 7 UI
- env-manager's own public hostname for receiving GitHub webhooks → Plan 6 (envm CLI requires it anyway)

---

## File structure after this plan

**Modified files:**

```
backend/internal/builder/labels.go        — TraefikOptions struct + multi-domain router generation
backend/internal/builder/labels_test.go   — new test cases for multi-domain, LE, redirect, preview pattern
backend/internal/builder/runner.go        — pass iac.Domains + LE email to InjectTraefikLabels
backend/internal/builder/runner_test.go   — minimal update if signature change ripples through
backend/internal/config/config.go         — read LETSENCRYPT_EMAIL env var into Config
backend/cmd/server/main.go                — pass LetsencryptEmail to Runner via SetLetsencryptEmail
```

**New files:** none.

---

## Locked details

| Thing | Value |
|---|---|
| Auto `.home` router name | `<env_id>-home` (used when v2 path active) |
| Public HTTPS router name | `<env_id>-public` |
| Public HTTP→HTTPS redirect router name | `<env_id>-public-http` |
| Legacy single-router name (Domains == nil) | `<env_id>` (existing — unchanged) |
| Redirect middleware name | `https-redirect-<env_id>` (env-scoped to avoid conflicts across envs) |
| HTTP entrypoint | `web` |
| HTTPS entrypoint | `websecure` |
| LE resolver name | `letsencrypt` |
| Host rule format for multiple domains | `` Host(`d1`) || Host(`d2`) || ... `` |
| Preview pattern placeholder | `{branch}` → replaced with `env.BranchSlug` |
| Behaviour when `LetsencryptEmail == ""` and public domains exist | Generate HTTP-only routers on web entrypoint (no TLS, no redirect); log a warning |
| Behaviour when `Domains == nil` | Legacy single HTTP router on `env.URL` (existing behaviour preserved exactly) |
| Behaviour when env.Kind == preview AND `Domains.Preview.Pattern == ""` | Only auto `.home` router; no public domains |
| Behaviour when env.URL == "" | No `-home` router emitted (defensive — shouldn't happen for valid envs) |

---

## Tasks

### Task 1: Branch + introduce `TraefikOptions` (refactor, no behaviour change)

Change the `InjectTraefikLabels` signature to accept a `TraefikOptions` struct instead of a positional `proxyNetwork` argument. All existing tests + the runner caller pass `TraefikOptions{ProxyNetwork: ...}`. Behaviour unchanged.

**Files:**
- Modify: `backend/internal/builder/labels.go`
- Modify: `backend/internal/builder/labels_test.go`
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go` (if any test passes the proxyNetwork arg directly — most do via `newRunnerTest` which calls `NewRunner(..., "", ...)` → `InjectTraefikLabels` is called from inside Build with `r.proxyNetwork`, no test direct calls beyond `labels_test.go`)

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status
git rev-parse HEAD
```

Expected: clean working tree (untracked OK); HEAD at `1ad233b` (Plan 4 merge) or later.

```bash
git checkout -b feat/v2-plan-05-custom-domains
```

- [ ] **Step 2: Update `labels.go` signature**

In `backend/internal/builder/labels.go`, find:

```go
func InjectTraefikLabels(composePath string, env *models.Environment, expose *models.ExposeSpec, proxyNetwork string) error {
	if proxyNetwork == "" {
		return nil
	}
```

Replace with:

```go
// TraefikOptions configures InjectTraefikLabels behaviour.
//
//   - ProxyNetwork: name of the external network Traefik listens on (matches
//     the existing v1 contract). Empty → InjectTraefikLabels is a noop.
//   - Domains: per-env domain config from iac.Config.Domains. Nil → legacy
//     single-router behaviour (one HTTP router on env.URL).
//   - LetsencryptEmail: required for HTTPS+LE on non-.home domains. Empty →
//     public domains fall back to HTTP-only routers; caller is expected to
//     emit a warning. Plan 5 does NOT mutate Traefik command flags — that's
//     a manual one-time host op covered by Plan 8.
type TraefikOptions struct {
	ProxyNetwork     string
	Domains          *iac.Domains
	LetsencryptEmail string
}

func InjectTraefikLabels(composePath string, env *models.Environment, expose *models.ExposeSpec, opts TraefikOptions) error {
	if opts.ProxyNetwork == "" {
		return nil
	}
```

Find the place that uses `proxyNetwork` in the body and replace each occurrence with `opts.ProxyNetwork`. There should be 3-4 references in the existing body.

Add the iac import at the top of `labels.go`:

```go
import (
	// ... existing imports ...
	"github.com/environment-manager/backend/internal/iac"
)
```

- [ ] **Step 3: Update `labels_test.go` to pass `TraefikOptions`**

Search-and-replace in `labels_test.go`. Every existing call:

```go
err := InjectTraefikLabels(path, env, nil, "")
err := InjectTraefikLabels(path, env, nil, "my-net")
err := InjectTraefikLabels(path, env, &models.ExposeSpec{...}, "my-net")
```

becomes:

```go
err := InjectTraefikLabels(path, env, nil, TraefikOptions{})
err := InjectTraefikLabels(path, env, nil, TraefikOptions{ProxyNetwork: "my-net"})
err := InjectTraefikLabels(path, env, &models.ExposeSpec{...}, TraefikOptions{ProxyNetwork: "my-net"})
```

The test bodies stay the same. Use grep first to find every callsite:

```bash
cd backend && grep -n "InjectTraefikLabels(" internal/builder/labels_test.go
```

Update each one.

- [ ] **Step 4: Update `runner.go` callsite**

In `backend/internal/builder/runner.go` find:

```go
if err := InjectTraefikLabels(composePath, env, project.Expose, r.proxyNetwork); err != nil {
```

Replace with:

```go
if err := InjectTraefikLabels(composePath, env, project.Expose, TraefikOptions{ProxyNetwork: r.proxyNetwork}); err != nil {
```

- [ ] **Step 5: Run all builder tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS — no behaviour change, just argument shape.

- [ ] **Step 6: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/builder/labels.go backend/internal/builder/labels_test.go backend/internal/builder/runner.go
git commit -m "refactor(builder): TraefikOptions struct for InjectTraefikLabels

Replaces the positional proxyNetwork argument with a TraefikOptions
struct that will gain Domains + LetsencryptEmail fields in subsequent
commits. Pure refactor — Domains == nil and ProxyNetwork == r.proxyNetwork
preserves existing single-router behaviour. All tests pass without
modification beyond the call-shape change."
```

---

### Task 2: Multi-domain router generation (auto `.home` + custom prod with HTTPS+LE)

Implement the v2 path: when `opts.Domains != nil`, generate `<env_id>-home` (HTTP) + `<env_id>-public` (HTTPS+LE) routers. This task does NOT add the redirect router (Task 3) or preview pattern resolution (Task 4) — they slot into the same function in the next two tasks.

**Files:**
- Modify: `backend/internal/builder/labels.go`
- Modify: `backend/internal/builder/labels_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `labels_test.go`:

```go
func TestInjectTraefikLabels_V2_HomeAndPublicRouters(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:         "stripe--main",
		URL:        "stripe-payments.home",
		Kind:       models.EnvKindProd,
		BranchSlug: "main",
	}
	domains := &iac.Domains{
		Prod: []string{"blocksweb.nl", "www.blocksweb.nl"},
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}
	out := readCompose(t, path)

	// Home router: HTTP entrypoint, Host(`stripe-payments.home`).
	mustContain(t, out, "traefik.http.routers.stripe--main-home.rule=Host(`stripe-payments.home`)")
	mustContain(t, out, "traefik.http.routers.stripe--main-home.entrypoints=web")

	// Public router: HTTPS entrypoint, Host union, TLS+LE.
	mustContain(t, out, "traefik.http.routers.stripe--main-public.rule=Host(`blocksweb.nl`) || Host(`www.blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.stripe--main-public.entrypoints=websecure")
	mustContain(t, out, "traefik.http.routers.stripe--main-public.tls=true")
	mustContain(t, out, "traefik.http.routers.stripe--main-public.tls.certresolver=letsencrypt")

	// Backend service definition still points at the exposed port.
	mustContain(t, out, "traefik.http.services.stripe--main.loadbalancer.server.port=80")
}

func TestInjectTraefikLabels_V2_DomainsNilUsesLegacyPath(t *testing.T) {
	// Confirm the v1 path still emits exactly the legacy single router.
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 8080}, TraefikOptions{
		ProxyNetwork: "my-net",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Legacy single router uses unsuffixed env-id.
	mustContain(t, out, "traefik.http.routers.p--main.rule=Host(`myapp.home`)")
	mustContain(t, out, "traefik.http.routers.p--main.entrypoints=web")
	// No -home or -public suffixes when Domains is nil.
	if strings.Contains(out, "p--main-home") || strings.Contains(out, "p--main-public") {
		t.Errorf("legacy path should not emit -home or -public routers; got:\n%s", out)
	}
}

func TestInjectTraefikLabels_V2_LeEmailEmptyFallbackToHttp(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          &iac.Domains{Prod: []string{"blocksweb.nl"}},
		LetsencryptEmail: "", // unset
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Public router exists but on web entrypoint (no TLS, no LE).
	mustContain(t, out, "traefik.http.routers.p--main-public.rule=Host(`blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.p--main-public.entrypoints=web")
	// No tls=true label.
	if strings.Contains(out, "p--main-public.tls=true") {
		t.Errorf("expected no TLS label when LetsencryptEmail is empty; got:\n%s", out)
	}
}

// mustContain helper already exists in labels_test.go (used by other tests).
```

If `mustContain` is not yet defined in the test file, add a helper at the top:

```go
func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected to find %q in:\n%s", needle, haystack)
	}
}
```

(The grep for `mustContain` will tell you whether it already exists.)

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run TestInjectTraefikLabels_V2 -v
```

Expected: all 3 V2 tests fail (current legacy path doesn't emit `-home` or `-public` routers).

- [ ] **Step 3: Implement the v2 path in `labels.go`**

In `labels.go`, find the existing label-construction block (around the `routerName := env.ID` line):

```go
	// Router name is the environment ID (already slug-safe).
	routerName := env.ID
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", routerName):                      fmt.Sprintf("Host(`%s`)", env.URL),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", routerName):               "web",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName): strconv.Itoa(targetPort),
		"traefik.docker.network": opts.ProxyNetwork,
	}
```

Replace with:

```go
	// Compute label set: legacy single-router path when Domains is nil,
	// multi-domain v2 path otherwise.
	labels := buildTraefikLabels(env, targetPort, opts)
```

Then add a new helper function later in the file (just below `resolveTarget`):

```go
// buildTraefikLabels assembles the Traefik label map for a service.
//
// Two modes:
//
//   - Legacy: opts.Domains == nil. Emits one HTTP router named env.ID with
//     Host(env.URL). Existing behaviour, preserved exactly.
//
//   - v2: opts.Domains != nil. Emits a -home HTTP router for env.URL plus
//     a -public router for the iac-declared custom domains (HTTPS+LE when
//     opts.LetsencryptEmail is set, HTTP fallback otherwise). All routers
//     share the same backend service definition.
//
// Service backend (loadbalancer.server.port) is always emitted under the
// unsuffixed env.ID name so the routers can reference it via the implicit
// service-name = router-name resolution. Wait — Traefik resolves service
// references explicitly, so each router needs a `.service` label pointing
// at env.ID. We add that explicitly in the v2 path.
func buildTraefikLabels(env *models.Environment, targetPort int, opts TraefikOptions) map[string]string {
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": opts.ProxyNetwork,
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", env.ID): strconv.Itoa(targetPort),
	}

	if opts.Domains == nil {
		// Legacy single HTTP router on env.URL — preserve exact existing shape.
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", env.ID)] = fmt.Sprintf("Host(`%s`)", env.URL)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", env.ID)] = "web"
		return labels
	}

	// v2 path. Emit -home router for env.URL.
	if env.URL != "" {
		homeRouter := env.ID + "-home"
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", homeRouter)] = fmt.Sprintf("Host(`%s`)", env.URL)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", homeRouter)] = "web"
		labels[fmt.Sprintf("traefik.http.routers.%s.service", homeRouter)] = env.ID
	}

	// Public domains: prod uses Domains.Prod directly; preview is added in Task 4.
	publicHosts := opts.Domains.Prod

	if len(publicHosts) > 0 {
		publicRouter := env.ID + "-public"
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", publicRouter)] = formatHostRule(publicHosts)
		labels[fmt.Sprintf("traefik.http.routers.%s.service", publicRouter)] = env.ID
		if opts.LetsencryptEmail != "" {
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "websecure"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls", publicRouter)] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", publicRouter)] = "letsencrypt"
		} else {
			// LE not configured — emit HTTP-only public router so the domains
			// are at least reachable. Caller (runner) is expected to log a
			// warning. Task 3 adds the redirect-to-HTTPS router only when
			// LE is configured.
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "web"
		}
	}

	return labels
}

// formatHostRule joins multiple hostnames into a Traefik Host(...) rule
// using the || operator, e.g. Host(`a.com`) || Host(`b.com`).
func formatHostRule(hosts []string) string {
	parts := make([]string, len(hosts))
	for i, h := range hosts {
		parts[i] = fmt.Sprintf("Host(`%s`)", h)
	}
	return strings.Join(parts, " || ")
}
```

Make sure `"strings"` is in the imports at the top of `labels.go` if not already (the existing file already imports it).

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/builder/... -run TestInjectTraefikLabels -v
```

Expected: all PASS — both new V2 tests and existing legacy tests (the legacy path emits the same labels as before).

- [ ] **Step 5: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/builder/labels.go backend/internal/builder/labels_test.go
git commit -m "feat(builder): multi-domain Traefik labels with Let's Encrypt

Adds the v2 label-generation path: when opts.Domains != nil, emit
a <env_id>-home HTTP router for env.URL plus a <env_id>-public
router for iac-declared prod domains. Public router gets HTTPS +
letsencrypt cert resolver when opts.LetsencryptEmail is set;
otherwise falls back to HTTP-only so domains are still reachable
during operator setup. Legacy path (Domains == nil) is byte-for-
byte identical to v1 — existing tests pass unchanged."
```

---

### Task 3: HTTP→HTTPS redirect router for public domains

When LE is configured + public domains exist, add a `<env_id>-public-http` router on the `web` entrypoint that redirects to HTTPS via a `redirectscheme` middleware.

**Files:**
- Modify: `backend/internal/builder/labels.go`
- Modify: `backend/internal/builder/labels_test.go`

- [ ] **Step 1: Write the failing test**

Append to `labels_test.go`:

```go
func TestInjectTraefikLabels_V2_HttpsRedirect(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          &iac.Domains{Prod: []string{"blocksweb.nl"}},
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Redirect router on web entrypoint with same host rule.
	mustContain(t, out, "traefik.http.routers.p--main-public-http.rule=Host(`blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.p--main-public-http.entrypoints=web")
	mustContain(t, out, "traefik.http.routers.p--main-public-http.middlewares=https-redirect-p--main")
	// Middleware definition.
	mustContain(t, out, "traefik.http.middlewares.https-redirect-p--main.redirectscheme.scheme=https")
}

func TestInjectTraefikLabels_V2_NoRedirectWhenLeUnset(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          &iac.Domains{Prod: []string{"blocksweb.nl"}},
		LetsencryptEmail: "", // unset
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// No redirect router or middleware when LE is not configured.
	if strings.Contains(out, "p--main-public-http") {
		t.Errorf("expected no redirect router when LE unset; got:\n%s", out)
	}
	if strings.Contains(out, "https-redirect-p--main") {
		t.Errorf("expected no redirect middleware when LE unset; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run "TestInjectTraefikLabels_V2_(HttpsRedirect|NoRedirect)" -v
```

Expected: `_HttpsRedirect` fails; `_NoRedirectWhenLeUnset` passes (no redirect-router code emits anything yet, so the negative assertion holds vacuously).

- [ ] **Step 3: Implement the redirect emission in `buildTraefikLabels`**

In `labels.go`, find the v2 public-router block:

```go
		if opts.LetsencryptEmail != "" {
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "websecure"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls", publicRouter)] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", publicRouter)] = "letsencrypt"
		} else {
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "web"
		}
```

Replace with:

```go
		if opts.LetsencryptEmail != "" {
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "websecure"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls", publicRouter)] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", publicRouter)] = "letsencrypt"

			// HTTP→HTTPS redirect router + middleware.
			redirectRouter := env.ID + "-public-http"
			middlewareName := "https-redirect-" + env.ID
			labels[fmt.Sprintf("traefik.http.routers.%s.rule", redirectRouter)] = formatHostRule(publicHosts)
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", redirectRouter)] = "web"
			labels[fmt.Sprintf("traefik.http.routers.%s.middlewares", redirectRouter)] = middlewareName
			labels[fmt.Sprintf("traefik.http.routers.%s.service", redirectRouter)] = env.ID
			labels[fmt.Sprintf("traefik.http.middlewares.%s.redirectscheme.scheme", middlewareName)] = "https"
		} else {
			// HTTP-only fallback when LE unconfigured.
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", publicRouter)] = "web"
		}
```

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS, including both new redirect tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/builder/labels.go backend/internal/builder/labels_test.go
git commit -m "feat(builder): emit HTTP→HTTPS redirect router for public domains

When LetsencryptEmail is set and public domains exist, add a
<env_id>-public-http router on web entrypoint with a redirectscheme
middleware that sends traffic to https. The middleware name is
env-scoped (https-redirect-<env_id>) so multiple envs don't clash.
When LE is unconfigured no redirect is emitted — the public router
serves plain HTTP, matching the operator-not-yet-set-up scenario."
```

---

### Task 4: Preview domain pattern resolution

Resolve `Domains.Preview.Pattern` for preview envs by substituting `{branch}` with `env.BranchSlug`. The resolved domain joins `publicHosts` and gets the same HTTPS+LE+redirect treatment as prod domains.

**Files:**
- Modify: `backend/internal/builder/labels.go`
- Modify: `backend/internal/builder/labels_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `labels_test.go`:

```go
func TestInjectTraefikLabels_V2_PreviewPattern(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:         "stripe--feature-x",
		URL:        "feature-x.stripe-payments.home",
		Kind:       models.EnvKindPreview,
		BranchSlug: "feature-x",
	}
	domains := &iac.Domains{
		Preview: iac.PreviewDomains{Pattern: "{branch}.stripe-payments.blocksweb.nl"},
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Resolved preview domain on public router.
	mustContain(t, out, "traefik.http.routers.stripe--feature-x-public.rule=Host(`feature-x.stripe-payments.blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.stripe--feature-x-public.entrypoints=websecure")
}

func TestInjectTraefikLabels_V2_PreviewPatternEmptyOnlyHome(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:         "p--feature-y",
		URL:        "feature-y.myapp.home",
		Kind:       models.EnvKindPreview,
		BranchSlug: "feature-y",
	}
	domains := &iac.Domains{
		Prod: []string{"shouldnt.be.used.com"}, // prod domains do NOT apply to preview envs
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Only home router; no public router because preview pattern is empty.
	mustContain(t, out, "p--feature-y-home")
	if strings.Contains(out, "p--feature-y-public") {
		t.Errorf("preview env without preview pattern should not emit -public router; got:\n%s", out)
	}
	if strings.Contains(out, "shouldnt.be.used.com") {
		t.Errorf("Domains.Prod should not apply to preview envs; got:\n%s", out)
	}
}

func TestInjectTraefikLabels_V2_ProdEnvIgnoresPreviewPattern(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:   "p--main",
		URL:  "myapp.home",
		Kind: models.EnvKindProd,
	}
	domains := &iac.Domains{
		Prod:    []string{"myapp.com"},
		Preview: iac.PreviewDomains{Pattern: "{branch}.myapp.com"}, // ignored for prod
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	mustContain(t, out, "Host(`myapp.com`)")
	if strings.Contains(out, "{branch}") {
		t.Errorf("literal {branch} placeholder leaked into output:\n%s", out)
	}
	// Pattern resolution on prod with empty BranchSlug would produce ".myapp.com" —
	// verify the preview pattern was NOT applied for prod env.
	if strings.Contains(out, "Host(`.myapp.com`)") {
		t.Errorf("preview pattern resolved for prod env; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify failures**

```bash
cd backend && go test ./internal/builder/... -run "TestInjectTraefikLabels_V2_Preview" -v
```

Expected: `_PreviewPattern` fails (preview pattern not yet handled). `_PreviewPatternEmptyOnlyHome` and `_ProdEnvIgnoresPreviewPattern` may pass already since the current implementation doesn't read Preview.Pattern at all.

- [ ] **Step 3: Update `buildTraefikLabels` to compute preview hosts**

In `labels.go`, find the line:

```go
	// Public domains: prod uses Domains.Prod directly; preview is added in Task 4.
	publicHosts := opts.Domains.Prod
```

Replace with:

```go
	// Public domains: prod uses Domains.Prod directly; preview resolves
	// Preview.Pattern with {branch} → env.BranchSlug substitution.
	var publicHosts []string
	switch env.Kind {
	case models.EnvKindProd:
		publicHosts = opts.Domains.Prod
	case models.EnvKindPreview:
		if opts.Domains.Preview.Pattern != "" && env.BranchSlug != "" {
			resolved := strings.ReplaceAll(opts.Domains.Preview.Pattern, "{branch}", env.BranchSlug)
			publicHosts = []string{resolved}
		}
	}
```

- [ ] **Step 4: Run all builder tests**

```bash
cd backend && go test ./internal/builder/... -v
```

Expected: all PASS, including all 3 preview tests.

- [ ] **Step 5: Run the full backend suite**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/builder/labels.go backend/internal/builder/labels_test.go
git commit -m "feat(builder): resolve preview domain pattern with {branch} substitution

Preview envs now derive their public domain from Domains.Preview.Pattern
by replacing the literal '{branch}' with env.BranchSlug. Prod envs
continue to use Domains.Prod directly. Empty pattern → no public
router for preview envs (only the auto .home stays). Domains.Prod
is ignored for preview envs to avoid duplicate routing across envs."
```

---

### Task 5: Wire `LETSENCRYPT_EMAIL` config + iac.Domains into runner.Build

The runner now passes the iac-derived `*iac.Domains` and the configured LE email to `InjectTraefikLabels`. Configuration is read by `cmd/server/main.go` from the `LETSENCRYPT_EMAIL` env var via `config.Config`. The runner accepts the email via a `SetLetsencryptEmail` setter (mirroring `SetServiceProvisioners`'s pattern).

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/builder/runner.go`
- Modify: `backend/internal/builder/runner_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add `LetsencryptEmail` to `Config`**

In `backend/internal/config/config.go`:

Add to the `Config` struct:

```go
type Config struct {
	Port             int
	DataDir          string
	StaticDir        string
	GitRemote        string
	BaseDomain       string
	TraefikIP        string
	ProxyNetwork     string
	LetsencryptEmail string // empty = LE disabled, public domains fall back to HTTP
}
```

In `Load()`, after the `proxyNetwork` block:

```go
	letsencryptEmail := os.Getenv("LETSENCRYPT_EMAIL")
```

And in the return:

```go
	return &Config{
		Port:             port,
		DataDir:          dataDir,
		StaticDir:        staticDir,
		GitRemote:        gitRemote,
		BaseDomain:       baseDomain,
		TraefikIP:        traefikIP,
		ProxyNetwork:     proxyNetwork,
		LetsencryptEmail: letsencryptEmail,
	}, nil
```

- [ ] **Step 2: Add `letsencryptEmail` field + setter to `Runner`**

In `backend/internal/builder/runner.go`, add to the `Runner` struct:

```go
type Runner struct {
	// ... existing fields ...
	letsencryptEmail string
}
```

Add the setter alongside `SetServiceProvisioners`:

```go
// SetLetsencryptEmail wires the Let's Encrypt email used by the v2 Traefik
// label generator. Empty string means LE is disabled — public domains will
// fall back to plain HTTP routers and the build log will warn.
func (r *Runner) SetLetsencryptEmail(email string) {
	r.letsencryptEmail = email
}
```

- [ ] **Step 3: Build `TraefikOptions` from iac.Config in `Build`**

In `runner.go`, find the `InjectTraefikLabels` call (around line 232):

```go
	if err := InjectTraefikLabels(composePath, env, project.Expose, TraefikOptions{ProxyNetwork: r.proxyNetwork}); err != nil {
```

Replace with:

```go
	traefikOpts := TraefikOptions{
		ProxyNetwork:     r.proxyNetwork,
		LetsencryptEmail: r.letsencryptEmail,
	}
	if iacCfg != nil {
		traefikOpts.Domains = &iacCfg.Domains
		// Surface a one-time warning if the operator declared public domains
		// but didn't set LETSENCRYPT_EMAIL — the labels still emit HTTP-only
		// routers, but TLS/redirect/LE won't apply.
		if r.letsencryptEmail == "" && hasPublicDomains(env, &iacCfg.Domains) {
			_, _ = log.Write([]byte("WARNING: domains declared but LETSENCRYPT_EMAIL is unset; public domains will serve HTTP only\n"))
		}
	}
	if err := InjectTraefikLabels(composePath, env, project.Expose, traefikOpts); err != nil {
```

Add the helper at the bottom of `runner.go`:

```go
// hasPublicDomains returns true when the env will receive any non-.home
// router. Used by the runner to surface a warning when LE is unset.
func hasPublicDomains(env *models.Environment, d *iac.Domains) bool {
	if d == nil {
		return false
	}
	switch env.Kind {
	case models.EnvKindProd:
		return len(d.Prod) > 0
	case models.EnvKindPreview:
		return d.Preview.Pattern != ""
	}
	return false
}
```

- [ ] **Step 4: Wire in `cmd/server/main.go`**

In `backend/cmd/server/main.go`, after the existing `buildRunner.SetServiceProvisioners(...)` block (or where the runner is constructed), add:

```go
	buildRunner.SetLetsencryptEmail(cfg.LetsencryptEmail)
```

- [ ] **Step 5: Write a runner-level integration test**

Append to `runner_test.go`:

```go
func TestRunner_Build_V2DomainsLabelsAppliedFromIac(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
domains:
  prod:
    - blocksweb.nl
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// proxyNetwork must be non-empty for InjectTraefikLabels to run.
	r2 := NewRunner(store, exec, dataDir, "my-net", NewQueue(), zap.NewNop(), nil)
	r2.SetLetsencryptEmail("ops@example.com")

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}

	composePath := filepath.Join(dataDir, "envs", env.ID, "docker-compose.yaml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "p1--main-home") {
		t.Errorf("expected -home router; got:\n%s", out)
	}
	if !strings.Contains(out, "p1--main-public") {
		t.Errorf("expected -public router; got:\n%s", out)
	}
	if !strings.Contains(out, "letsencrypt") {
		t.Errorf("expected letsencrypt resolver; got:\n%s", out)
	}
}

func TestRunner_Build_V2DomainsWarnsWhenLeUnset(t *testing.T) {
	r, store, project, env, dataDir, exec := newRunnerTest(t)
	_ = r

	devCfg := `project_name: myapp
expose:
  service: app
  port: 80
domains:
  prod:
    - blocksweb.nl
`
	if err := os.WriteFile(filepath.Join(project.LocalPath, ".dev", "config.yaml"), []byte(devCfg), 0644); err != nil {
		t.Fatal(err)
	}

	r2 := NewRunner(store, exec, dataDir, "my-net", NewQueue(), zap.NewNop(), nil)
	// Don't call SetLetsencryptEmail — leaves it empty.

	build := &models.Build{
		ID: "b1", EnvID: env.ID, SHA: "abc",
		TriggeredBy: models.BuildTriggerManual,
		Status:      models.BuildStatusRunning,
	}
	_ = store.SaveBuild("p1", build)

	if err := r2.Build(context.Background(), env, build); err != nil {
		t.Fatalf("Build: %v", err)
	}

	logBytes, err := os.ReadFile(filepath.Join(dataDir, "builds", env.ID, "latest.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBytes), "LETSENCRYPT_EMAIL is unset") {
		t.Errorf("expected LE-unset warning in build log; got:\n%s", logBytes)
	}
}
```

- [ ] **Step 6: Run all tests**

```bash
cd backend && go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Run vet + build**

```bash
cd backend && go vet ./... && go build ./...
```

Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/config/config.go backend/internal/builder/runner.go backend/internal/builder/runner_test.go backend/cmd/server/main.go
git commit -m "feat(builder): runner passes iac.Domains + LE email to label injector

config.Config gains LetsencryptEmail (read from LETSENCRYPT_EMAIL).
main.go calls buildRunner.SetLetsencryptEmail(cfg.LetsencryptEmail).
Runner.Build builds TraefikOptions from the captured iacCfg and
warns in the build log when public domains are declared but LE is
unconfigured. Two new runner tests pin the integration:
v2 domains apply when LE is set, and the warning fires when it
isn't."
```

---

### Task 6: Final sanity + plan/checklist commit

**Files:**
- Modify: `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`

- [ ] **Step 1: Run the full backend suite + vet + build**

```bash
cd backend && go test ./... -count=1 && go vet ./... && go build ./...
```

Expected: all green, all clean.

- [ ] **Step 2: Sanity-check the diff**

```bash
git diff --stat 1ad233b..HEAD
git log --oneline 1ad233b..HEAD
```

Expected: 5 commits (Tasks 1-5) on `feat/v2-plan-05-custom-domains`. Files: `backend/internal/builder/{labels,labels_test,runner,runner_test}.go`, `backend/internal/config/config.go`, `backend/cmd/server/main.go`. No changes outside `backend/`.

- [ ] **Step 3: Update rollout checklist**

Replace the Plan 5 placeholder in `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` with:

```markdown
## Plan 5 — Multi-domain Traefik labels with Let's Encrypt

After merge + redeploy:
- [ ] `cd backend && go test ./internal/builder/... -v` — all PASS, including new TestInjectTraefikLabels_V2_* and TestRunner_Build_V2Domains* tests
- [ ] env-manager redeploy script now sets `LETSENCRYPT_EMAIL=<your email>` alongside `CREDENTIAL_KEY`
- [ ] Existing stripe-payments builds (still v1 schema) trigger normally — runner emits the legacy single HTTP router on env.URL, no `-home`/`-public` routers
- [ ] Manual: extend the test fixture project's `.dev/config.yaml` with `domains.prod: ["mytestdomain.com"]`, push — observe rendered compose has `-home` + `-public` routers + redirect router + middleware
- [ ] If LETSENCRYPT_EMAIL is unset, build log shows `WARNING: domains declared but LETSENCRYPT_EMAIL is unset; public domains will serve HTTP only`
- [ ] Traefik command flags for `--certificatesresolvers.letsencrypt.*` are deferred to Plan 8 (manual host op); without them, certs won't actually issue — but the labels are emitted correctly so Plan 8's host-side flags will start issuing certs immediately
- [ ] Cross-project domain conflict detection deferred to a future plan (Plan 5b or merged into Plan 7 UI)
- [ ] env-manager's own public hostname (manager.blocksweb.nl) deferred to Plan 6
```

- [ ] **Step 4: Commit plan + checklist**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-05-custom-domains.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 05 (custom domains)

Plan document + Plan 5 entry in the rollout checklist.
Implementation lands in the preceding 5 commits on this branch.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Push branch + open PR

- [ ] **Step 1: Push**

```bash
git push -u origin feat/v2-plan-05-custom-domains
```

- [ ] **Step 2: Open PR via gh**

```bash
gh pr create --title "v2 plan 05: multi-domain Traefik labels with Let's Encrypt" --body "$(cat <<'EOF'
## Summary

- Generates Traefik routers from \`iac.Config.Domains\`. Custom prod domains get HTTPS routers with Let's Encrypt + HTTP→HTTPS redirect; \`.home\` domains stay HTTP-only
- New \`TraefikOptions\` struct on \`InjectTraefikLabels\` with \`ProxyNetwork\`, \`Domains\`, \`LetsencryptEmail\` fields. Legacy callers pass \`Domains: nil\` to keep the existing single-router behaviour
- Preview envs derive their public domain from \`Domains.Preview.Pattern\` by substituting \`{branch}\` with \`env.BranchSlug\`
- Best-effort fallback preserved: when \`.dev/config.yaml\` is absent or LE email is unset, the runner falls back gracefully (legacy single router, or HTTP-only public routers respectively)
- \`config.Config.LetsencryptEmail\` reads \`LETSENCRYPT_EMAIL\` env var; \`Runner.SetLetsencryptEmail\` mirrors the \`SetServiceProvisioners\` injection seam

## Out of scope (deferred)

- Traefik command-flag bootstrap on \`env-traefik\` — Plan 8 migration runbook (one-time host op)
- Cross-project domain conflict check — future Plan 5b or Plan 7
- env-manager's own public hostname — Plan 6 (envm CLI)

## Test plan

- [x] \`cd backend && go test ./internal/builder/... -v\` — all PASS (8 new TestInjectTraefikLabels_V2_* tests + 2 new TestRunner_Build_V2Domains* tests)
- [x] \`cd backend && go test ./...\` — full suite green
- [x] \`cd backend && go vet ./...\` — clean
- [x] \`cd backend && go build ./...\` — clean

After merge, manual home-lab verification per the rollout checklist Plan 5 section.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report PR URL back to the user**

---

## Acceptance criteria

- [ ] `TraefikOptions` struct exists with `ProxyNetwork`, `Domains`, `LetsencryptEmail` fields
- [ ] `InjectTraefikLabels` signature: `(composePath, env, expose, opts TraefikOptions) error`
- [ ] When `opts.Domains == nil`: legacy single HTTP router on `env.URL` (byte-identical to v1)
- [ ] When `opts.Domains != nil`: emits `<env_id>-home` HTTP router for `env.URL`
- [ ] When `opts.Domains.Prod` non-empty AND `env.Kind == prod`: emits `<env_id>-public` router with HTTPS+LE labels (when LE email set) or HTTP fallback (when unset)
- [ ] When LE email set: emits `<env_id>-public-http` redirect router + `https-redirect-<env_id>` middleware
- [ ] When env is preview AND `Domains.Preview.Pattern` non-empty: substitutes `{branch}` with `env.BranchSlug` and uses the result as the single public host
- [ ] Prod env with `Preview.Pattern` set: pattern is ignored
- [ ] Preview env with `Domains.Prod` set: prod entries are ignored
- [ ] All `traefik.http.routers.*.service` labels point to `env.ID` (the unsuffixed name) so they share the single backend
- [ ] `config.Config.LetsencryptEmail` reads `LETSENCRYPT_EMAIL` env var
- [ ] `Runner.SetLetsencryptEmail(email string)` setter exists; nil-safe
- [ ] `cmd/server/main.go` calls `buildRunner.SetLetsencryptEmail(cfg.LetsencryptEmail)`
- [ ] Runner.Build emits a WARNING in the build log when iac declares public domains but LE email is unset
- [ ] All existing tests pass without modification beyond the call-shape change
- [ ] `go test ./...` clean, `go vet ./...` clean, `go build ./...` clean
- [ ] Branch is 6 commits ahead of master (5 implementation + 1 docs)
- [ ] PR opened with the test-plan checklist
- [ ] Rollout checklist updated for Plan 5

## Notes for the implementing engineer

- **Working directory:** `G:\Workspaces\claude-code-tests\env-manager` (Windows). Run `go` commands from `backend/`.
- **Never use `> nul`, `> NUL`, or `> /dev/null`** — destructive on this Windows host.
- **TDD discipline:** every feat task → write failing test → run-fail → implement → run-pass → commit.
- **Commit cadence:** one commit per task (5 task commits + 1 docs commit). Don't squash. Don't amend.
- **Spec is canonical** — `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` overrides this plan if they conflict. Flag in PR.
- **Backwards compat is non-negotiable.** Existing tests must pass without behaviour changes — only call-shape updates from the signature change. If a legacy test needs a behaviour change, you've broken something.
- **Don't add Traefik command-flag mutations.** That's a one-time host op deferred to Plan 8. Plan 5 only emits *labels* — Traefik picks them up via its docker provider, no daemon flag required at this layer.
- **Don't add cross-project domain conflict detection.** Out of scope; would require iterating all projects' `.dev/config.yaml` files at build time. Deferred.
- **The `mustContain` test helper** may already exist in `labels_test.go`. Grep first; if missing, add it (the plan provides the body).
- **`models.EnvKindProd` / `models.EnvKindPreview`** — these constants already exist in `internal/models/project.go`.
- **Router service references.** Each router needs `routers.<name>.service=<env_id>` so Traefik knows which backend to route to. The legacy code skipped this because the router and service shared a name (`<env_id>`); the v2 code uses suffixed router names (`-home`, `-public`, `-public-http`) so the explicit `.service` reference is required. Don't omit it.
