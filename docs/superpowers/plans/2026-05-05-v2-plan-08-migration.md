# env-manager v2, Plan 8 — Migration runbook + final polish

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Author the operator runbook for v1→v2 cutover (host operations: tear down legacy compose projects, add Traefik LE flags, migrate stripe-payments' `.dev/config.yaml` to v2 schema, verify) AND ship the deferred cross-project domain conflict check from Plan 5 review notes.

**Architecture:** This plan is mostly documentation — a step-by-step runbook the operator follows once. The only code change is a small `domains.Conflict` helper that walks all projects' iac configs and flags a duplicate-domain attempt at project create time.

**Spec reference:** `docs/superpowers/specs/2026-05-05-env-manager-v2-design.md` — sections "Migration + cutover plan" (Phases 2-5) and "Domain conflict handling".

**Out of scope:**
- Actual execution of the host operations (operator runs the runbook on the home lab)
- Updating stripe-payments' `.dev/config.yaml` (that's in a different repo)
- env-manager's own public hostname (operator step — covered in runbook)

---

## File structure after this plan

**New files:**

```
docs/superpowers/specs/2026-05-05-env-manager-v2-migration-runbook.md   — operator runbook
backend/internal/iac/conflict.go        — domain conflict check helper
backend/internal/iac/conflict_test.go   — table-driven tests
```

**Modified files:**

```
backend/internal/api/handlers/projects.go   — call iac.CheckDomainConflict in Create
```

---

## Tasks

### Task 1: Branch + author migration runbook

**Files:**
- Create: `docs/superpowers/specs/2026-05-05-env-manager-v2-migration-runbook.md`

- [ ] **Step 1: Verify clean master + create branch**

```bash
git status && git rev-parse HEAD
```

Expected: HEAD at `cff40c0` (Plan 7 merge) or later.

```bash
git checkout -b feat/v2-plan-08-migration
```

- [ ] **Step 2: Write the runbook**

Create `docs/superpowers/specs/2026-05-05-env-manager-v2-migration-runbook.md`:

```markdown
# env-manager v1 → v2 migration runbook

**For:** the home-lab operator running env-manager on `192.168.1.116`.
**Estimated downtime:** ~5 minutes during Phase 3.
**Prereqs:** all v2 PRs (Plans 1-7) merged + redeployed; redeploy script updated to set `LETSENCRYPT_EMAIL`.

This runbook covers Phases 2-5 of the v2 cutover. Phase 1 (the code refactor) is the eight merged PRs.

## Phase 0 — pre-cutover snapshot

```bash
ssh ultiimaz@192.168.1.116
sudo systemctl stop docker
sudo tar czf /tmp/env-mgr-pre-v2-snapshot.tar.gz \
    /data/compose/16/data \
    /opt/src/environment-manager
sudo systemctl start docker
```

Keep `/tmp/env-mgr-pre-v2-snapshot.tar.gz` for at least a week.

## Phase 1 — set required env vars in the redeploy script

The v2 binary reads `LETSENCRYPT_EMAIL` and (already) `CREDENTIAL_KEY`. Before redeploying, edit env-updater's redeploy script:

```bash
sudo nano /opt/env-updater/scripts/redeploy-env-manager.sh
```

Add (alongside `CREDENTIAL_KEY=...`):

```bash
-e LETSENCRYPT_EMAIL="ops@blocksweb.nl"
```

Save. The next redeploy will pick it up.

## Phase 2 — tear down legacy compose projects (still v1 binary serving)

These three legacy projects haven't run since Plan 1's cleanup but their compose data is still on disk:

```bash
sudo docker stop kali win10 step3test-app 2>&1 | head
sudo docker rm kali win10 step3test-app 2>&1 | head
sudo rm -rf /data/compose/16/data/projects/720166ec7f156d89   # kali
sudo rm -rf /data/compose/16/data/projects/bbcac6d8e46e4324   # win10
sudo rm -rf /data/compose/16/data/projects/ccaf4392bfa1e987   # step3test
sudo rm -rf /data/compose/16/data/repos/step3-test
```

(Project IDs are SHA-prefixes of the repo URLs. Verify with `ls /data/compose/16/data/projects/` first.)

## Phase 3 — deploy the v2 binary

The redeploy fires when you push any change to env-manager's master OR you manually trigger:

```bash
sudo docker exec env-updater /scripts/redeploy-env-manager.sh
```

Watch the env-manager logs for:

- `==> env-manager admin token (save it now): envm_<64hex>` — **copy it now**, you'll need it in Phase 4
- `Service-plane: paas-postgres ready`
- `Service-plane: paas-redis ready`

If any of those don't appear, capture the logs and roll back via `git revert` of the master merge commit, then re-redeploy.

Save the admin token to your local machine:

```bash
mkdir -p ~/.envm
cat > ~/.envm/config.yaml <<EOF
endpoint: http://192.168.1.116:8080
token: envm_<paste-from-server-log>
EOF
chmod 600 ~/.envm/config.yaml
```

(For external access via `manager.blocksweb.nl`, see Phase 6.)

Verify CLI auth:

```bash
go install github.com/Ultiimaz/environment-manager/backend/cmd/envm@master
envm projects list
```

You should see your existing projects (stripe-payments).

## Phase 4 — migrate stripe-payments to v2 schema

In the stripe-payments repo on your laptop:

1. Edit `.dev/config.yaml` to the v2 schema. Replace its current contents with (adjust to taste):

```yaml
project_name: stripe-payments

expose:
  service: app
  port: 80

domains:
  prod:
    - blocksweb.nl
    - www.blocksweb.nl
  preview:
    pattern: "{branch}.stripe-payments.blocksweb.nl"

services:
  postgres: true
  redis: true

secrets:
  - STRIPE_SECRET_KEY
  - STRIPE_WEBHOOK_SECRET
  - ANTHROPIC_API_KEY
  - GOOGLE_CLIENT_ID
  - GOOGLE_CLIENT_SECRET

hooks:
  pre_deploy:
    - php artisan migrate --force
    - php artisan config:cache
  post_deploy:
    - php artisan queue:restart
```

2. Edit `.dev/docker-compose.prod.yml` and `.dev/docker-compose.dev.yml` — remove the `mysql` (or `mariadb`) and `redis` services; env-manager provisions them now via paas-postgres/paas-redis. Keep `app`, `worker`, etc.

3. Make sure your Laravel `config/database.php` reads `DATABASE_URL` (Laravel 12 supports it natively when present in `.env`). Same for `REDIS_URL` in `config/database.php`'s `redis` block.

4. Tear down the existing stripe-payments env containers (preserve env-manager-managed cred-store):

```bash
sudo docker compose -f /data/compose/16/data/envs/91497099a7a1c68c--main/docker-compose.yaml \
    -p 91497099a7a1c68c--main \
    --project-directory /data/compose/16/data/repos/blocksweb-dasboard-laravel \
    down -v
# repeat for --develop env if it exists
```

5. Push the .dev/config.yaml + compose changes to stripe-payments' `main`:

```bash
git push origin main
```

6. Watch env-manager redeploy stripe-payments. Look for:
   - `==> provisioning postgres database`
   - `==> provisioning redis ACL`
   - `==> docker compose build`
   - `==> pre_deploy[1/2]: php artisan migrate --force`
   - `==> docker compose up -d`
   - `==> post_deploy[1/1]: php artisan queue:restart`

7. Push to `develop` to trigger the same flow for the preview env.

## Phase 5 — verification

Once stripe-payments redeploys cleanly:

```bash
# Domains return 200
curl -k -I https://blocksweb.nl/login
curl -k -I https://www.blocksweb.nl/login
curl -k -I https://develop.stripe-payments.blocksweb.nl/login   # if preview pattern declared

# Service plane
envm services status
docker exec paas-postgres psql -U postgres -c "\l" | grep stripepayments_main
docker exec paas-redis redis-cli -a "$(jq -r '.system_secrets["system:paas-redis:superuser"]' /data/compose/16/data/.credentials/store.json)" ACL LIST | grep stripepayments_main

# Secrets present
envm secrets list stripe-payments
# Should show 5 keys
```

## Phase 6 — env-manager's own public hostname (deferred from Plan 6)

To receive GitHub webhooks externally, env-manager needs its own public hostname. The current setup runs on LAN `192.168.1.6:8080`.

1. In Cloudflare DNS for `blocksweb.nl`: add A record `manager → 84.84.207.234` (your home-lab public IP), proxy off (grey cloud) so Let's Encrypt's TLS-ALPN works.

2. Open port 443 on the KPN modem + ISP router pointing at `192.168.1.6` (env-traefik).

3. Add Traefik labels to env-manager's container. Edit `/data/compose/16/docker-compose.yaml` (or wherever env-manager's compose lives):

```yaml
services:
  env-manager:
    # ... existing config ...
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.env-manager.rule=Host(\`manager.blocksweb.nl\`)"
      - "traefik.http.routers.env-manager.entrypoints=websecure"
      - "traefik.http.routers.env-manager.tls=true"
      - "traefik.http.routers.env-manager.tls.certresolver=letsencrypt"
      - "traefik.http.services.env-manager.loadbalancer.server.port=8080"
      - "traefik.docker.network=my-macvlan-net"
    networks:
      - paas-net
      - my-macvlan-net
```

4. `docker compose up -d` env-manager. Wait ~30s for cert issuance.

5. Update each GitHub repo's webhook URL to `https://manager.blocksweb.nl/api/v1/webhook/github` (was `http://192.168.1.6:8080/...` if exposed).

6. Update your local `~/.envm/config.yaml`:

```yaml
endpoint: https://manager.blocksweb.nl
token: envm_...
```

## Phase 7 — Traefik Let's Encrypt resolver flags (deferred from Plan 5)

Edit env-traefik's compose command flags:

```yaml
services:
  env-traefik:
    command:
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.network=my-macvlan-net"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.email=ops@blocksweb.nl"
      - "--certificatesresolvers.letsencrypt.acme.storage=/data/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.tlschallenge=true"
    volumes:
      - traefik_acme:/data
```

Then `docker compose up -d` env-traefik. Watch its logs for "Server stopped"/"Server started" cycle. Cert acquisition for already-pushed v2 stripe-payments takes ~30-60s per domain.

## Phase 8 — cleanup

After 1-2 weeks of stable v2 operation:

```bash
# Remove the snapshot
sudo rm /tmp/env-mgr-pre-v2-snapshot.tar.gz
```

Keep this runbook in `docs/superpowers/specs/` for reference.

## Risks + rollback per phase

| Phase | Risk | Rollback |
|---|---|---|
| 0 | Snapshot too big — fill /tmp | `sudo rm /tmp/env-mgr-pre-v2-snapshot.tar.gz` and pick a smaller scope |
| 1 | Wrong email | Re-edit + re-redeploy |
| 2 | Removed wrong project ID | `tar xzf /tmp/env-mgr-pre-v2-snapshot.tar.gz -C /` |
| 3 | v2 binary fails to boot | `git revert <merge-commit>` + redeploy |
| 4 | stripe-payments fails on v2 | Revert .dev/ changes, push; redeploy v1 binary if Phase 3 also reverted |
| 5 | Domain doesn't return 200 | Check Traefik logs (`docker logs env-traefik`), verify `LETSENCRYPT_EMAIL` is set, check Cloudflare DNS |
| 6 | Cert not issued for manager.blocksweb.nl | Verify port 443 forwarded, Cloudflare proxy is grey cloud, Phase 7 done |
| 7 | env-traefik doesn't restart | `docker logs env-traefik` for syntax errors in compose flags |
```

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-05-05-env-manager-v2-migration-runbook.md
git commit -m "docs: env-manager v1→v2 migration runbook for the home lab

Step-by-step runbook covering Phases 2-7 of the cutover: legacy
compose teardown, redeploy + admin-token capture, stripe-payments
.dev/config.yaml migration, verification, env-manager's own
public hostname setup, and Traefik LE resolver flag bootstrap
(deferred from Plans 5 + 6).

Operator-only — no code changes in this commit."
```

---

### Task 2: Cross-project domain conflict check

The Plan 5 code-quality review flagged this as deferred. Implementation: a helper that, given a project being created, walks all existing projects' `.dev/config.yaml` files and rejects a create that would claim a domain already owned by another project.

**Files:**
- Create: `backend/internal/iac/conflict.go`
- Create: `backend/internal/iac/conflict_test.go`
- Modify: `backend/internal/api/handlers/projects.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/iac/conflict_test.go`:

```go
package iac

import (
	"strings"
	"testing"
)

func TestCollectClaimedDomains_Empty(t *testing.T) {
	got := CollectClaimedDomains(nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestCollectClaimedDomains_FlattensProdAndPreview(t *testing.T) {
	configs := map[string]*Config{
		"projA": {
			ProjectName: "projA",
			Domains: Domains{
				Prod: []string{"a.com", "www.a.com"},
				Preview: PreviewDomains{Pattern: "{branch}.a.com"},
			},
		},
		"projB": {
			ProjectName: "projB",
			Domains:     Domains{Prod: []string{"b.com"}},
		},
	}
	got := CollectClaimedDomains(configs)
	expectations := map[string]string{
		"a.com":           "projA",
		"www.a.com":       "projA",
		"{branch}.a.com":  "projA",
		"b.com":           "projB",
	}
	if len(got) != len(expectations) {
		t.Errorf("got %d entries, want %d: %+v", len(got), len(expectations), got)
	}
	for d, want := range expectations {
		if got[d] != want {
			t.Errorf("domain %q: got owner %q, want %q", d, got[d], want)
		}
	}
}

func TestCheckDomainConflict_NoConflict(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Prod: []string{"a.com"}}},
	}
	candidate := &Config{Domains: Domains{Prod: []string{"b.com", "c.com"}}}
	err := CheckDomainConflict(candidate, "projB", existing)
	if err != nil {
		t.Errorf("expected no conflict, got %v", err)
	}
}

func TestCheckDomainConflict_ConflictWithProd(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Prod: []string{"a.com", "shared.com"}}},
	}
	candidate := &Config{Domains: Domains{Prod: []string{"shared.com"}}}
	err := CheckDomainConflict(candidate, "projB", existing)
	if err == nil {
		t.Fatal("expected conflict")
	}
	if !strings.Contains(err.Error(), "shared.com") || !strings.Contains(err.Error(), "projA") {
		t.Errorf("error should name the conflicting domain + owner, got %q", err.Error())
	}
}

func TestCheckDomainConflict_ConflictWithPreviewPattern(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Preview: PreviewDomains{Pattern: "{branch}.a.com"}}},
	}
	candidate := &Config{Domains: Domains{Preview: PreviewDomains{Pattern: "{branch}.a.com"}}}
	err := CheckDomainConflict(candidate, "projB", existing)
	if err == nil {
		t.Fatal("expected conflict on preview pattern")
	}
}

func TestCheckDomainConflict_SameProjectIsNotAConflict(t *testing.T) {
	existing := map[string]*Config{
		"projA": {Domains: Domains{Prod: []string{"a.com"}}},
	}
	candidate := &Config{Domains: Domains{Prod: []string{"a.com"}}}
	// projectID is projA — re-saving its own config shouldn't conflict with itself.
	err := CheckDomainConflict(candidate, "projA", existing)
	if err != nil {
		t.Errorf("self-reuse should not conflict, got %v", err)
	}
}
```

- [ ] **Step 2: Implement `conflict.go`**

Create `backend/internal/iac/conflict.go`:

```go
package iac

import (
	"fmt"
	"strings"
)

// CollectClaimedDomains walks the parsed configs of every onboarded project
// and returns a domain → projectID map flattening Domains.Prod + the literal
// Domains.Preview.Pattern (with the {branch} placeholder kept as-is, since
// the pattern itself is what's reserved across projects).
//
// Used by CheckDomainConflict and by the eventual UI to show a "domain
// registry" view.
func CollectClaimedDomains(configs map[string]*Config) map[string]string {
	out := make(map[string]string)
	for projectID, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, d := range cfg.Domains.Prod {
			d = strings.TrimSpace(strings.ToLower(d))
			if d != "" {
				out[d] = projectID
			}
		}
		if pat := strings.TrimSpace(cfg.Domains.Preview.Pattern); pat != "" {
			out[strings.ToLower(pat)] = projectID
		}
	}
	return out
}

// CheckDomainConflict reports whether candidate's domains collide with any
// already-claimed domain in existing, excluding the candidate's own
// projectID (so re-saving the same config doesn't trip the check).
//
// On conflict, returns an error naming the offending domain + the owning
// project. On success, returns nil.
func CheckDomainConflict(candidate *Config, projectID string, existing map[string]*Config) error {
	if candidate == nil {
		return nil
	}
	// Build the claim map filtered to OTHER projects.
	others := make(map[string]*Config, len(existing))
	for id, cfg := range existing {
		if id == projectID {
			continue
		}
		others[id] = cfg
	}
	claims := CollectClaimedDomains(others)
	candidateDomains := CollectClaimedDomains(map[string]*Config{projectID: candidate})
	for d := range candidateDomains {
		if owner, taken := claims[d]; taken {
			return fmt.Errorf("domain %q already claimed by project %q", d, owner)
		}
	}
	return nil
}
```

- [ ] **Step 3: Run tests**

```bash
cd backend && go test ./internal/iac/... -run "TestCollectClaimedDomains|TestCheckDomainConflict" -v
```

Expected: all PASS.

- [ ] **Step 4: Wire into `handlers/projects.go::Create`**

In the Create handler, after `iac.Parse(...)` (or after the legacy `DetectDevDir` parse currently used), add a domain conflict check. Currently `Create` uses the v1 `projects.DetectDevDir` parser. Plan 8 keeps that path and adds the check by parsing iac when the config is valid v2:

Find where `Create` parses the config (around line 100 of `projects.go`). After validation, add:

```go
	// Plan 8: cross-project domain conflict check.
	// Best-effort: try to parse the dev config as v2; if it parses, walk all
	// other projects' v2 configs and reject conflicting domains. v1-only
	// projects skip this check entirely (no Domains block to conflict on).
	iacBytes, err := os.ReadFile(filepath.Join(repo.LocalPath, ".dev", "config.yaml"))
	if err == nil {
		if iacCfg, perr := iac.Parse(iacBytes); perr == nil {
			others, oerr := h.collectIacConfigs(project.ID)
			if oerr == nil {
				if cerr := iac.CheckDomainConflict(iacCfg, project.ID, others); cerr != nil {
					_ = h.store.DeleteProject(project.ID)
					_ = h.reposManager.Delete(repo.ID)
					respondError(w, http.StatusConflict, "DOMAIN_CONFLICT", cerr.Error())
					return
				}
			}
		}
	}
```

(The exact insertion point depends on the existing flow — place it AFTER the project + env are saved but BEFORE the success response, so the check covers the just-onboarded project. If a conflict is detected, roll back via DeleteProject + reposManager.Delete and return 409 Conflict.)

Add a helper method on `ProjectsHandler`:

```go
// collectIacConfigs walks every onboarded project's .dev/config.yaml and
// returns a map of project ID → parsed iac.Config for projects that parse
// successfully under v2. Used by the domain conflict check.
func (h *ProjectsHandler) collectIacConfigs(excludeID string) (map[string]*iac.Config, error) {
	all, err := h.store.ListProjects()
	if err != nil {
		return nil, err
	}
	out := make(map[string]*iac.Config, len(all))
	for _, p := range all {
		if p.ID == excludeID {
			continue
		}
		bytes, rerr := os.ReadFile(filepath.Join(p.LocalPath, ".dev", "config.yaml"))
		if rerr != nil {
			continue
		}
		cfg, perr := iac.Parse(bytes)
		if perr != nil {
			continue
		}
		out[p.ID] = cfg
	}
	return out, nil
}
```

Add `"github.com/environment-manager/backend/internal/iac"` to imports.

- [ ] **Step 5: Run all tests**

```bash
cd backend && go test ./... -count=1 && go vet ./... && go build ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/iac/conflict.go backend/internal/iac/conflict_test.go backend/internal/api/handlers/projects.go
git commit -m "feat(iac): cross-project domain conflict check at project create

iac.CheckDomainConflict walks every other project's .dev/config.yaml,
flattens Domains.Prod + Preview.Pattern into a claim map, and returns
an error naming the offending domain + owner if the new project tries
to claim one. Wired into ProjectsHandler.Create as a 409 Conflict
response (with rollback). Self-reuse on re-save is allowed."
```

---

### Task 3: Final sanity + plan/checklist + push + PR + merge

- [ ] **Step 1: Sanity**

```bash
cd backend && go test ./... -count=1 && go vet ./... && go build ./...
cd frontend && pnpm build
```

Expected: clean.

- [ ] **Step 2: Update rollout checklist**

Replace the Plan 8 placeholder in `docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md`:

```markdown
## Plan 8 — Migration runbook + final polish

- [ ] Migration runbook at `docs/superpowers/specs/2026-05-05-env-manager-v2-migration-runbook.md` reviewed
- [ ] Operator works through Phases 0-8 of the runbook on the home lab
- [ ] `cd backend && go test ./internal/iac/... -v` passes new conflict tests
- [ ] Manual: try to onboard two projects with overlapping `domains.prod` — second create returns 409 with the offending domain + owner ID
```

- [ ] **Step 3: Commit + push + PR + merge**

```bash
git add docs/superpowers/plans/2026-05-05-v2-plan-08-migration.md docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md
git commit -m "docs: plan + rollout checklist for v2 plan 08 (migration)"
git push -u origin feat/v2-plan-08-migration
gh pr create --title "v2 plan 08: migration runbook + domain conflict check" --body "Operator runbook for v1→v2 cutover (Phases 0-8) plus the deferred cross-project domain conflict check from Plan 5 review.

🤖 Generated with [Claude Code](https://claude.com/claude-code)"
gh pr merge --merge --delete-branch
git checkout master && git pull
```

---

## Acceptance criteria

- [ ] Migration runbook authored
- [ ] `iac.CollectClaimedDomains` + `iac.CheckDomainConflict` implemented + tested
- [ ] Project create returns 409 on cross-project domain conflict, with rollback
- [ ] Self-reuse (re-saving same project's config) is not flagged
- [ ] All tests pass; both backend and frontend build clean
- [ ] PR merged

## Notes for the implementing engineer

- **The runbook is for the operator — not the implementer.** Don't try to execute the host commands during plan implementation.
- **Domain conflict integration in `projects.go::Create`** — the existing handler uses the v1 `DetectDevDir` parser. Don't break that. Add the v2 conflict check as best-effort: if iac parse fails (v1-only config), skip the check.
- **Rollback on conflict** — when 409, undo the project + repo creation that just happened. Mirrors the existing `DeleteProject + reposManager.Delete` pattern in the file.
