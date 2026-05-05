# env-manager v2: PaaS-focused redesign

**Status:** Design approved, awaiting implementation plan
**Date:** 2026-05-05
**Author:** Brainstorming session with Claude

## Goal

Refactor env-manager into a focused self-hosted PaaS for the home-lab. Three pillars: domain management through Traefik (with Let's Encrypt for public domains), CI/CD of git repos via push-to-deploy with pre/post-deploy hooks, and dependency management through IaC (one shared Postgres + one shared Redis with per-env scoped resources).

Scope: refactor in place — same `environment-manager` repo, same Docker image, same `env-updater` redeploy pipeline. Drop everything not directly serving the three pillars.

## Non-goals

- Multi-host orchestration (single Docker host, as today).
- Kubernetes / Nomad / Swarm.
- Multi-user / team / RBAC. Single operator.
- Cloudflare API integration (DNS records remain manual / grey-cloud).
- Wildcard certificates (per-domain certs only, ALPN-01 challenge).
- Per-env metrics dashboards / observability.
- Built-in MinIO / Reverb / object storage.
- Plugin / extension system.
- Self-hosted GitHub Actions runner. Builds happen on the env-manager host.

## Three pillars in one paragraph each

**Domain management.** A project's `.dev/config.yaml` declares custom public domains (`blocksweb.nl`, `www.blocksweb.nl`) and an optional preview-domain pattern (`{branch}.stripe-payments.blocksweb.nl`). env-manager generates Traefik routers, attaches Let's Encrypt resolvers for any non-`.home` domain, automatically appends the internal `<project>.home` and `<branch>.<project>.home` records (resolved by the existing CoreDNS wildcard). The user manages Cloudflare DNS A records manually.

**CI/CD.** Push to a branch with `.dev/` triggers a build via GitHub webhook. `pre_deploy` hooks (declared in IaC, e.g. `php artisan migrate --force`) run inside a fresh container *before* traffic is shifted; if any fails, the deploy aborts and the previous container keeps serving. `post_deploy` hooks run after the swap; failures are logged but don't abort. Branch deletion auto-tears-down preview envs (prod exempt).

**Dependency management via IaC.** `.dev/config.yaml` declares `services: { postgres: true, redis: true }`. env-manager runs one singleton Postgres 16 + one singleton Redis 7 on a shared `paas-net` bridge. For each Environment, it creates a per-env database (e.g. `stripepayments_main`) + scoped user, generates a password, stores it encrypted in the credential store, and injects `DATABASE_URL` / `REDIS_URL` into the app at deploy time. Apps reach the shared services via Docker DNS on `paas-net`. Per-env Redis uses ACL users with prefix-scoped keys (Redis 6+ `ACL SETUSER`).

## Architecture

Three planes:

**Control plane** (env-manager Go binary):
- Reads `.dev/config.yaml` from each repo as the source of truth
- Manages Project + Environment + Build lifecycle (existing types from prior spec)
- Speaks to host Docker daemon to run user containers
- Speaks to Postgres + Redis (via `docker exec`) to provision per-env resources
- Updates Traefik via Docker labels
- Serves the React UI + REST/WS API
- Receives GitHub webhooks at a public HTTPS endpoint

**Service plane** (shared, env-manager-managed):
- `paas-postgres` — Postgres 16, persistent volume `paas_postgres_data`, on `paas-net` only
- `paas-redis` — Redis 7, persistent volume `paas_redis_data`, on `paas-net` only
- `env-traefik` — existing container, gets a Let's Encrypt resolver added at v2 boot
- `env-coredns` — existing container, unchanged

**Workload plane** (user apps):
- Per-environment compose stack rendered from `.dev/docker-compose.{prod,dev}.yml`
- App containers join three networks: their own compose `default`, `paas-net` (to reach shared Postgres/Redis), `my-macvlan-net` (so Traefik can reach them)
- Containers come up + down per-env-id; nothing shared at this layer between environments

### What gets deleted

**Backend Go packages:**
- `internal/backup` — gone
- `internal/state` — gone
- `internal/sync` — gone
- `internal/git` — gone
- `internal/repos` — gone
- `internal/stats` — gone

**Backend HTTP handlers:**
- `internal/api/handlers/{containers,volumes,compose,network,git,repos,github,exec,stats,webhook_legacy}.go` — gone
- The `webhook.go` legacy code path (the main/master filter + `RebuildLinkedProjectsForRepo`) — gone

**Frontend pages + components:**
- `src/pages/{Repositories,Compose,Containers,ContainerDetail,Volumes,Network,Git,Dashboard}.tsx` — gone
- `src/components/{containers,volumes,network,git}/` — gone
- Half of `src/services/api.ts` — gone (only project + build + secret endpoints survive)
- All legacy types in `src/types/index.ts` — gone

**main.go wiring:** state restore, backup scheduler, stats collector, sync controller, repos manager, git repository, all gone.

**On-disk legacy data:**
- `desired-state.yaml` — gone
- `network/Corefile` shim (env-manager's stale copy) — gone (CoreDNS reads from named volume only)
- Existing legacy Project rows in `data/projects/` for `kali`, `win10`, `step3test` — gone (manually rm'd during migration)

About 60-70% of current code deletes.

### What stays + reshapes

Backend packages kept:
- `internal/projects` — Store, Slug, URL, DevConfig parser, DevDir detector, GetProjectByRepoURL, ResolveDefaultBranch, BranchSlug, ComposeURL, FetchOrigin, ListRemoteBranches, DevDirExistsForBranch, MarkStuckBuildsFailed, ReconcileBranches, EnvSpawner interface
- `internal/builder` — Runner, Queue, Render, Labels, ComposeExecutor, DockerComposeExecutor
- `internal/buildlog` — Log (file + ring fan-out)
- `internal/credentials` — Store with project secrets (already added)
- `internal/proxy` — Manager, label generator (extended for TLS)
- `internal/api/handlers/{projects,builds,webhook}.go` — kept, extended

Frontend kept:
- `src/components/ui/*` — shadcn primitives (only those actually used; prune)
- `src/components/layout/*` — sidebar, app-layout (rebuilt per Section 7 below)
- `src/components/projects/{add-project-modal,build-log-viewer}.tsx` — extended
- `src/pages/{Projects,ProjectDetail}.tsx` — extended

### What gets added

**Backend Go packages:**
- `internal/iac` — v2 `.dev/config.yaml` parser + validator (replaces existing `devconfig.go` with extensions)
- `internal/services/postgres` — provisioner (`EnsureService`, `EnsureEnvDatabase`, `DropEnvDatabase`)
- `internal/services/redis` — provisioner (`EnsureService`, `EnsureEnvACL`, `DropEnvACL`)
- `internal/hooks` — pre/post-deploy hook executor (runs commands in a temp container spawned from the new image)
- `cmd/envm` — CLI binary (separate `cmd` directory, same Go module)

**Frontend pages:**
- `src/pages/{Home,Builds,Services,Settings}.tsx` — new (5 sidebar destinations total)
- `src/pages/EnvironmentDetail.tsx` — promoted from inline component
- `src/components/projects/{runtime-log-viewer,domain-list,services-status}.tsx` — new

**Backend HTTP endpoints (new):**
- `POST /api/v1/projects/{id}/secrets` — bulk set (existing, kept)
- `DELETE /api/v1/projects/{id}` — full project teardown
- `POST /api/v1/envs/{id}/destroy` — preview env teardown (without deleting project)
- `POST /api/v1/envs/{id}/restart` — `docker compose restart` for the env
- `GET /api/v1/envs/{id}/builds` — recent build list
- `GET /api/v1/services/postgres` — health + provisioned-env list
- `GET /api/v1/services/redis` — health + provisioned-env list
- `GET /api/v1/settings` — config visibility (no secrets)
- `WS /ws/envs/{id}/runtime-logs?service=app` — `docker logs -f` for the env's named service

## IaC schema (`.dev/config.yaml` v2)

```yaml
project_name: stripe-payments

# What service:port Traefik routes to (user-facing)
expose:
  service: app
  port: 80

# Domain routing per environment kind.
# `.home` is always added automatically; you only list extras here.
domains:
  prod:
    - blocksweb.nl
    - www.blocksweb.nl
  preview:
    # Template: {branch} is the slugified branch name.
    # Optional — omit to keep preview envs internal-only (.home).
    pattern: "{branch}.stripe-payments.blocksweb.nl"

# Shared services — env-manager creates per-env database/ACL-user inside
# the singleton instance. Apps get DATABASE_URL / REDIS_URL injected.
services:
  postgres: true
  redis: true

# Required user-supplied secrets — names only, no values in the repo.
# Used by `envm secrets check <project>` to list what's missing.
secrets:
  - STRIPE_SECRET_KEY
  - STRIPE_WEBHOOK_SECRET
  - ANTHROPIC_API_KEY
  - GOOGLE_CLIENT_ID
  - GOOGLE_CLIENT_SECRET

# Hooks run inside a freshly built app container.
# pre_deploy: runs BEFORE the new container takes traffic
#   - if any fails, the deploy aborts; old container keeps serving
# post_deploy: runs AFTER traffic shift
#   - failures logged but don't abort
hooks:
  pre_deploy:
    - php artisan migrate --force
    - php artisan config:cache
  post_deploy:
    - php artisan queue:restart
```

### What's deliberately NOT in the schema

- **Build context / dockerfile path.** Defaults: `context = repo root`, `dockerfile = .dev/Dockerfile.dev`. If you need otherwise, change the compose file's `build:` section directly.
- **Compose file paths.** Hardcoded: `.dev/docker-compose.prod.yml` for prod, `.dev/docker-compose.dev.yml` for previews.
- **TLS toggles.** Auto-derived: any non-`.home` domain triggers Let's Encrypt. `.home` stays HTTP.
- **Resource limits / replicas.** Out of scope for v2 — set in compose file if needed.
- **Branch allowlist.** Any branch with `.dev/` in its tree gets a preview env. No allowlisting.

### Per-env database semantics

Each Environment gets its own database in the shared Postgres:
- Database name: `<slugify(project_name)>_<branch_slug>` (e.g. `stripepayments_main`)
- Username: same as database name
- Password: 24-byte random, stored encrypted in cred-store under `env:<env-id>:db_password`
- Connection string injected as `DATABASE_URL=postgres://<user>:<pw>@paas-postgres:5432/<db>?sslmode=disable`

On Environment teardown (branch deletion or project deletion): `DROP DATABASE` + `DROP USER`.

### Per-env Redis semantics

Each Environment gets a Redis ACL user (Redis 6+):
- Username: same naming as DB (e.g. `stripepayments_main`)
- Password: 24-byte random, stored encrypted in cred-store
- Key prefix scope: `<project_slug>:<branch_slug>:*`
- ACL command: `ACL SETUSER <user> on >password ~<prefix>:* +@all -@dangerous`
- Connection string injected as `REDIS_URL=redis://<user>:<pw>@paas-redis:6379/0`

On teardown: `ACL DELUSER <user>`. Keys with the user's prefix are NOT auto-deleted (they leak; not worth the cleanup risk for v2).

### Injected env vars

At build time, the runner writes a `.env` file at the project's repo root. Contents (in order):
1. Auto-generated platform vars: `PROJECT_NAME`, `BRANCH`, `ENV_KIND`, `ENV_URL`
2. Auto-generated service vars: `DATABASE_URL`, `REDIS_URL` (if services declared)
3. Auto-generated routing var: `APP_URL` — first prod domain, or computed `<project>.<branch>.home` for previews
4. All keys from `secrets:` — pulled from credential store. **Missing required secrets fail the build with a clear error.**

Compose's `env_file: .env` directive in the user's `docker-compose.{prod,dev}.yml` loads these into the container at runtime.

## Lifecycle flows

### Flow A — Project onboarding (UI or `envm projects onboard <repo-url>`)

1. User submits repo URL.
2. env-manager clones the repo to `/app/data/repos/<project-id>`.
3. Read + validate `.dev/config.yaml` against v2 schema.
4. Read + validate compose files exist and parse.
5. Validate `domains.prod` doesn't conflict with another project's prod domains.
6. Record Project row (no Environment yet — that comes on first push).
7. Return 201 with project ID + list of secrets the user still needs to set.
8. (Optional, if GitHub PAT configured) Register webhook on the GitHub repo.

No build runs yet. Branches with `.dev/` get envs the first time they're pushed.

### Flow B — Push to a branch (existing or new)

1. GitHub push webhook → public Cloudflare → Traefik → env-manager `/api/v1/webhook/github`.
2. Verify HMAC signature with the project's webhook secret.
3. Match `payload.repository.clone_url` to a Project (URL normalized).
4. Fetch origin in the project's local clone.
5. Look up Environment for branch slug:
   - **Exists**: enqueue Build (`TriggeredBy=webhook`, `sha=head`).
   - **Doesn't exist**: check `.dev/` is in the pushed branch's tree (`git ls-tree`).
     - If absent: ignore (branch hasn't opted in).
     - If present: create preview Environment row, enqueue Build.
6. `runner.Build(env, build)` runs in goroutine.

### Flow C — Build execution (with hooks)

1. Acquire per-env queue lock.
2. Truncate log file, reset ring buffer, attach WS subscribers.
3. Validate required secrets are all present in cred-store; abort early if not.
4. **Ensure shared services for this env**:
   - If `services.postgres` and DB doesn't exist: `CREATE DATABASE` + `CREATE USER` + grant; store creds.
   - If `services.redis` and ACL user doesn't exist: `ACL SETUSER`; store creds.
5. Write `.env` at `<repo>/.env` from credential store + injected platform/service vars.
6. Render compose: read source compose, inject Traefik labels (with Let's Encrypt resolver if any public domain), inject network attachment for `paas-net` and `my-macvlan-net`, write to `<env-dir>/docker-compose.yaml`.
7. Build image: `docker compose -p <env-id> --project-directory <repo> build` (image only, NOT `up` yet).
8. Run pre_deploy hooks (each as `docker run --rm <image> sh -c "<hook>"`):
   - If any hook exits non-zero: abort, mark Build failed, OLD containers keep running.
9. `docker compose up -d` — replaces containers; new image takes traffic.
10. Run post_deploy hooks (same `docker run` pattern):
    - Failures logged but don't abort.
11. Mark Build success, Environment status = running.

### Flow D — Branch deletion (GitHub `delete` event)

1. GitHub delete webhook (`ref_type=branch`).
2. Match repo to Project, find Environment by branch slug.
3. If Environment kind is `prod` → ignore (prod is project-deletion-only).
4. `docker compose -p <env-id> down -v` — containers + named volumes gone.
5. `DROP DATABASE <db>; DROP USER <user>;` on `paas-postgres`.
6. `ACL DELUSER <user>` on `paas-redis`.
7. `rm -rf /app/data/envs/<env-id>/`.
8. Delete Environment row.

### Flow E — Project deletion (UI button + typed confirmation)

1. User types project name to confirm deletion.
2. For each Environment under the project: Flow D's teardown (compose down -v + DB drop + ACL delete + dirs gone).
3. Drop prod env's DB + ACL (exempted from D).
4. Delete all Environment rows.
5. Delete Project row + project's entries from credential store.
6. `rm -rf /app/data/repos/<project-id>/`.
7. (Optional) DELETE GitHub webhook if env-manager registered it during onboarding.

### Flow F — Reconcile on startup

1. For each Project: `git fetch origin --prune` in its local clone.
2. List remote branches via `for-each-ref refs/remotes/origin/`.
3. For each branch present remotely with no local Environment AND `.dev/` in its tree → spawn preview (Flow B's "doesn't exist" path).
4. For each local Environment whose branch is no longer remote AND kind != prod → tear down (Flow D's body, minus the webhook-payload parsing).
5. Mark stuck builds as failed (existing `MarkStuckBuildsFailed` logic).

### Flow G — Service-plane bootstrap (boots before A-F)

On every env-manager startup, before any project work:
1. `docker network create paas-net` if missing.
2. If `paas-postgres` container is missing: `docker run -d --name paas-postgres --network paas-net -v paas_postgres_data:/var/lib/postgresql/data -e POSTGRES_PASSWORD=<from cred-store, generate if absent> postgres:16`.
3. If `paas-redis` container is missing: `docker run -d --name paas-redis --network paas-net -v paas_redis_data:/data redis:7 redis-server --requirepass <from cred-store>`.
4. Wait until both are healthy (`pg_isready`, `redis-cli ping`).
5. Continue with reconcile (Flow F).

If env-manager itself is recreated, this idempotently re-attaches to the existing volumes — no data loss.

## Domain + TLS management

### Sources of domains

Three sources, merged at build time:
1. **Auto-generated `.home` domains** (always present): `<project>.home` for prod, `<branch_slug>.<project>.home` for previews.
2. **Custom prod domains** from `domains.prod` in IaC.
3. **Custom preview domains** from `domains.preview.pattern` resolved per branch slug.

Each domain → one Traefik router. All routers point at the same backend service (the app's container on `paas-net`).

### Traefik configuration changes

Added at v2 first boot (env-manager edits the existing Traefik command flags):

```
--certificatesresolvers.letsencrypt.acme.email=${LETSENCRYPT_EMAIL}
--certificatesresolvers.letsencrypt.acme.storage=/data/acme.json
--certificatesresolvers.letsencrypt.acme.tlschallenge=true
--entrypoints.websecure.address=:443
```

`LETSENCRYPT_EMAIL` is configured via env-manager's container env var (set in the redeploy script alongside `CREDENTIAL_KEY`). Surfaced in the Settings UI as read-only. If unset, env-manager skips Let's Encrypt registration and any `domains.prod` entries with non-`.home` TLDs cause a clear startup error pointing at this missing config.

Plus the existing `--providers.docker.network=my-macvlan-net` — unchanged.

### Per-router labels (env-manager injects)

For `.home` domains:
```
traefik.http.routers.<env_id>-home.rule=Host(`<host>`)
traefik.http.routers.<env_id>-home.entrypoints=web
```

For public domains:
```
traefik.http.routers.<env_id>-public.rule=Host(`<d1>`) || Host(`<d2>`) || ...
traefik.http.routers.<env_id>-public.entrypoints=websecure
traefik.http.routers.<env_id>-public.tls=true
traefik.http.routers.<env_id>-public.tls.certresolver=letsencrypt
traefik.http.routers.<env_id>-public-http.rule=<same as above>
traefik.http.routers.<env_id>-public-http.entrypoints=web
traefik.http.routers.<env_id>-public-http.middlewares=https-redirect
traefik.http.middlewares.https-redirect.redirectscheme.scheme=https
```

### User responsibilities for new public domains

1. Add the domain to `.dev/config.yaml :: domains.prod` (or update `domains.preview.pattern`).
2. In Cloudflare DNS, create A record `<domain> → 84.84.207.234` with **proxy off (grey cloud)** so Let's Encrypt's TLS-ALPN works.
3. Open KPN modem + ISP-router port 443 to `192.168.1.6` (env-traefik). Port 80 already open.
4. Push the IaC change. env-manager rebuilds, Traefik discovers the router, requests a cert via ALPN-01, serves HTTPS.

### Domain conflict handling

At project create / on push: for each domain in `domains.prod` excluding `.home` wildcards:
- If already claimed by a *different* project's prod env → reject with a conflict error message.
- Same-project main + develop declaring the same domain: reject (it's a user mistake either way).

### Public webhook routing for env-manager itself

env-manager needs its own public hostname to receive GitHub webhooks externally. Special-cased at boot:
1. Pick a hostname (e.g. `manager.blocksweb.nl`).
2. Cloudflare DNS A record → `84.84.207.234`, grey cloud.
3. env-manager's container labels include both `web` (port 80, internal) and `websecure` (port 443, public, with Let's Encrypt) entrypoints, with the public hostname rule.
4. Open port 443 on KPN modem + ISP router.
5. In each project's GitHub repo settings, add webhook → `https://manager.blocksweb.nl/api/v1/webhook/github` with the per-project HMAC secret (env-manager generates and surfaces in the UI).

## CLI tool (`envm`)

A Go binary, distributed as a single static executable. Built alongside `cmd/server` from the same module. Connects to env-manager's API over HTTPS.

### Auth model

Single admin token, generated on env-manager's first boot and printed once to logs:
```
==> Initial admin token: envm_<random>
==> Save it to ~/.envm/config.toml or rotate via UI.
```

Stored persistently in cred-store under `system:admin_token`. Rotatable from the UI's Settings page.

User config at `~/.envm/config.toml`:
```toml
endpoint = "https://manager.blocksweb.nl"
token = "envm_..."
```

API: mutating endpoints require `Authorization: Bearer <token>`. Read-only project/env-status endpoints stay open on LAN for the UI's anonymous browsing convenience. (The UI on a public domain still uses anonymous reads; secrets ops stay locked behind the token.)

### Commands

```
envm secrets set <project> KEY=value [KEY=value...]
envm secrets list <project>                  # names only, no values
envm secrets get <project> KEY --reveal      # explicit flag to print value
envm secrets delete <project> KEY
envm secrets import <project> path/to/.env   # bulk import; - for stdin
envm secrets check <project>                 # required vs present, flags missing

envm projects list
envm projects onboard <git-url> [--token <PAT>]
envm projects show <project>
envm projects delete <project> [--yes]

envm builds trigger <project>/<env>
envm builds logs <project>/<env> [--follow]
envm builds list <project>/<env>

envm envs destroy <project>/<env> [--yes]    # preview env only

envm shell <project>/<env> [service]         # exec into container
envm psql <project>/<env>                    # psql to env's DB (uses scoped user)
envm redis <project>/<env>                   # redis-cli with env's ACL user

envm services status                         # paas-postgres + paas-redis health
envm services psql                           # superuser psql to paas-postgres

envm config show
envm version
```

### Design choices

- Subcommand-per-noun pattern (matches `gh`, `kubectl`, `flyctl`).
- `/` separator for project/env IDs in CLI args (`stripe-payments/main`), `--` separator on disk (`stripe-payments--main`).
- All destructive commands take `--yes` to skip confirmation; error otherwise on non-TTY.
- Single static binary released alongside the env-manager Docker image; download from GitHub release page.
- No JSON output flags in v2; add later if scripting need emerges.

## UI rebuild

### Tech stack

- React 19, Vite 6, TypeScript 5
- Tailwind v4 (current is v3 — bump for the new engine + design tokens)
- shadcn/ui — keep, prune unused components
- TanStack Query v5 for server state + polling
- React Router v7
- gorilla/websocket on backend ↔ native WebSocket on frontend
- Lucide icons (kept)
- Inter font (replaces default for "professional" feel)
- Dark mode default; light mode toggle via Tailwind v4

No backend changes for the rebuild — same Go HTTP API serves the new UI from `/static`.

### Information architecture

Top bar + collapsible sidebar. Five sidebar destinations: **Home**, **Projects**, **Builds**, **Services**, **Settings**.

### Pages

| Route | Purpose |
|---|---|
| `/` (Home) | Operator's morning dashboard. Live build queue card, service-plane health, recent failures, deploys-today count. |
| `/projects` | Project list (cards or table). Search box. "Add project" button → modal. |
| `/projects/:id` | Project detail. Tabs: Environments, Deploys (cross-env build history), Settings (read-only IaC view + danger-zone delete). |
| `/projects/:id/envs/:envId` | Environment detail. Trigger build / Restart / Destroy buttons (Destroy preview-only). Build history list. Two log viewers in tabs: build logs (file+ring) and runtime logs (live `docker logs -f`). |
| `/builds` | Cross-project build queue. Click a row → that build's log viewer. |
| `/services` | Postgres + Redis cards. Each shows: container status, version, persistent volume size, list of databases/ACL-users currently provisioned + which env they belong to. |
| `/settings` | env-manager config. Token info (presence not value), GitHub PAT status, Let's Encrypt email, public webhook URL (copyable for GitHub repo settings), version. |

### Component decisions

- **Build log viewer**: plain `<pre>` with `ansi-to-html`. xterm.js is overkill for build output and adds 200KB.
- **Runtime log viewer**: same component as build logs; the WS endpoint is different (`/ws/envs/{id}/runtime-logs?service=app`).
- **"Add project" modal**: repo URL field + optional GitHub PAT. Validation server-side; errors surface inline in the modal before navigation.
- **Empty states**: every list view has a clear "Nothing here yet, do X" call to action.

### What's deliberately not in the v2 UI

- Per-env metrics / observability charts (separate big project)
- Build artifact download / debug console (use CLI's `envm shell`)
- Multi-user / permissions / audit log
- Notification settings (handled via user's own integrations from hooks)

### Frontend deletes

```
src/pages/Repositories.tsx       — gone
src/pages/Compose.tsx            — gone
src/pages/Containers.tsx         — gone
src/pages/ContainerDetail.tsx    — gone
src/pages/Volumes.tsx            — gone
src/pages/Network.tsx            — gone
src/pages/Git.tsx                — gone
src/pages/Dashboard.tsx          — gone (replaced)
src/components/{containers,volumes,network,git,layout/legacy-*}/  — gone
half of src/services/api.ts      — gone (only project + build + secret endpoints survive)
all legacy types in src/types/index.ts  — gone
```

## Migration + cutover plan

### Pre-cutover snapshot

```
ssh ultiimaz@192.168.1.116
sudo systemctl stop docker
sudo tar czf /tmp/env-mgr-pre-v2-snapshot.tar.gz \
    /data/compose/16/data \
    /opt/src/environment-manager
sudo systemctl start docker
```

### Phase 1 — Code refactor (no host changes)

All on a feature branch.

1. Delete legacy backend packages (`backup`, `state`, `sync`, `git`, `repos`, `stats`).
2. Delete legacy handlers (containers, volumes, network, compose, git, repos, github, exec, stats).
3. Delete legacy frontend pages and components.
4. Reshape `main.go`: remove deleted-package wiring, add Flow G (service-plane bootstrap).
5. Add `internal/iac` (v2 config parser).
6. Add `internal/services/{postgres,redis}` (per-env provisioning).
7. Add `internal/hooks` (pre/post-deploy executor).
8. Extend `internal/proxy/labels.go` for custom-domain TLS.
9. Extend `internal/api/handlers/projects.go`: add domain conflict check, secret list endpoint matching IaC declaration, project deletion endpoint.
10. Add `cmd/envm` CLI binary alongside `cmd/server`.
11. Rebuild frontend per UI section.
12. Update `Dockerfile` to embed both `envm` and `server` binaries.
13. Branch builds + tests pass. Don't merge yet.

### Phase 2 — Host preparation (current env-manager still serving)

Done via SSH while old binary serves.

14. Stop + remove legacy compose projects:
    ```
    sudo docker stop kali win10 step3test-app
    sudo docker rm kali win10 step3test-app
    sudo rm -rf /data/compose/16/data/projects/{720166ec7f156d89,bbcac6d8e46e4324,ccaf4392bfa1e987}
    sudo rm -rf /data/compose/16/data/repos/step3-test
    ```
15. Tear down stripe-payments per-env containers (preserve data dir; v2 will rebuild without per-env DBs):
    ```
    sudo docker compose -f /data/compose/16/data/envs/91497099a7a1c68c--main/docker-compose.yaml \
        -p 91497099a7a1c68c--main \
        --project-directory /data/compose/16/data/repos/blocksweb-dasboard-laravel \
        down -v
    # repeat for --develop
    ```
    `down -v` purges the per-env mariadb + redis volumes. Their data is empty/seed-only (we just ran migrations).
16. Backup credential store: `sudo cp /data/compose/16/data/.credentials/store.json /tmp/creds-backup.json`. v2 keeps the same file format; this is a paranoia copy.

### Phase 3 — Deploy v2 binary

17. Merge feature branch to master.
18. Push triggers env-updater (existing pipeline, untouched).
19. v2 env-manager boots:
    - Flow G: creates `paas-net`, spawns `paas-postgres` + `paas-redis`, generates superuser passwords stored in cred-store.
    - Reconciler: stripe-payments Project row exists from v1 onboarding. v2 re-parses `.dev/config.yaml` on every deploy (no migration step needed for the Project row itself — only `expose`, `external_domain`, `public_branches` carry over from the v1 model and are still present in the v2 schema's evolved form). The Project row keeps its existing ID and metadata; only the *behaviour* changes when the user pushes a v2-compatible config.
    - Pre-cutover: until stripe-payments' `.dev/config.yaml` is updated, the project's existing Environments are flagged `Status=archived` (a new v2 status) — they don't auto-rebuild but they don't get torn down either.

### Phase 4 — Migrate stripe-payments

Done from local machine.

20. Update `.dev/config.yaml` to v2 schema (Section "IaC schema" format). Add `services: { postgres: true, redis: true }`, `hooks.pre_deploy: [php artisan migrate --force]`. `domains.prod` initially empty (or just adds the auto `.home`).
21. Update `.dev/docker-compose.{prod,dev}.yml`: remove `mysql` + `redis` services and their volumes (env-manager provisions them now). Keep `app` and `worker`. Keep `env_file: .env`.
22. Update Laravel `config/database.php` if hardcoded — likely already env-driven; ensure `DATABASE_URL` is parsed (Laravel 12 supports it natively in `database.php` config).
23. Commit + push to `main`. v2 builder picks up:
    - Provisions `stripepayments_main` in paas-postgres + ACL `stripepayments_main` in paas-redis.
    - Writes `.env` with `DATABASE_URL`, `REDIS_URL`, plus the 60 secrets.
    - Runs `pre_deploy: [php artisan migrate --force]` in temp container.
    - On success: `docker compose up -d` swaps the new container in.
24. Repeat 20–23 for `develop` branch.

### Phase 5 — Verification + cleanup

25. `curl http://stripe-payments.home/` → 200.
26. `envm services status` → both healthy.
27. `envm secrets list stripe-payments` → 60 keys present.
28. `envm projects show stripe-payments` → both envs running.
29. Remove the snapshot once confident.

### Risk + rollback

- **Phases 1 + 2**: reversible (branch not merged, legacy data wipe is disposable).
- **Phase 3**: risk = v2 binary fails to boot. Rollback = `git revert` merge commit, re-push, env-updater redeploys v1.
- **Phase 4**: risk = stripe-payments fails on v2. Rollback = revert v2 changes to `.dev/`, push. v1 env-manager would be back if Phase 3 also reverted.
- Total expected outage: ~5 minutes during Phase 3 image swap.

### What stays untouched

- Hermes (4-agent deploy), searxng, open-webui, minecraft — all independently managed compose stacks.
- env-coredns + env-traefik — kept; Traefik gets new flags during v2 first boot.
- Legacy `env-manager` Portainer stack 16 — kept for the data dir mount; the orphaned compose file there is irrelevant.

## Testing approach

### Unit tests (Go, fast)

- `internal/iac` parser — table-driven (happy path, all-optional fields, invalid YAML, unknown engine, missing required fields).
- Slug helpers, URL composer (existing tests, kept).
- Compose render injector — golden-file based.
- `internal/services/postgres` provisioner with a fake docker-exec interface.
- `internal/services/redis` provisioner with a fake docker-exec interface.
- `internal/hooks` executor with a fake docker-run interface.
- Domain conflict check.

### Integration tests (Go + dockerized harness, ~5–10 min CI)

- Full lifecycle on a real Postgres + Redis container started in the test:
  - Project onboard → environment create → DB + ACL provisioned.
  - Build + pre-deploy hook success.
  - Build + pre-deploy hook failure → previous container preserved.
  - Branch delete → DB + ACL dropped.
  - Project delete → all envs torn down.
  - Domain conflict rejected.
  - Reconcile-on-startup converges state correctly.

### Manual rollout checklist

`docs/superpowers/specs/2026-05-05-env-manager-v2-rollout-checklist.md` — created during implementation. Per-phase manual verifications (e.g. "after Phase 3: paas-postgres + paas-redis containers running, on paas-net, healthchecks green").

### What we explicitly don't test

- Cloudflare API integration (out of scope feature-wise).
- Wildcard cert / DNS-01 (out of scope).
- Per-env metrics (out of scope).
- UI snapshot tests (single-operator UI; not stable enough).
- Multi-tenant isolation hardening (ACL prefix scope is "good enough"; no fuzzing).

## Implementation decomposition

This spec is too large for one implementation plan. The natural split:

| Plan | Scope | Maps to migration phase |
|---|---|---|
| **1** | Backend refactor: delete legacy code, restructure `main.go`, all tests still pass, no behaviour change | Phase 1, items 1–4 |
| **2** | IaC v2 parser + schema | Phase 1, item 5 |
| **3** | Postgres + Redis service-plane provisioning + Flow G | Phase 1, item 6 |
| **4** | Pre/post-deploy hook executor | Phase 1, item 7 |
| **5** | Custom-domain + Let's Encrypt Traefik labels | Phase 1, items 8 + 9 (domain conflict check) |
| **6** | `envm` CLI binary | Phase 1, item 10 |
| **7** | UI rebuild | Phase 1, item 11 |
| **8** | Migration runbook execution | Phases 2–5 |

Plans 1–6 can land independently and be deployed to the home-lab as they're ready (each one passes through env-updater's existing pipeline; phase 3 of the migration is technically when plans 1–6 are all merged and v2 ships). Plans 7 + 8 land last.

## Deferred to v2.1+

- Cloudflare API integration (auto DNS records, wildcard certs via DNS-01)
- Per-env metrics + observability dashboards
- Multi-user / team / RBAC
- Volume backup automation
- Self-hosted GitHub Actions runner (offload builds from env-manager host)
- Built-in MinIO / object storage
- Built-in Reverb / WebSocket broker shared across projects
- Plugin system for custom service types
- Per-env Redis key cleanup on teardown (currently keys leak)
- Multi-DB-engine support (one Postgres + one MariaDB side by side, IaC picks)
