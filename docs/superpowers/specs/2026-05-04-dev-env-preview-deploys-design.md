# `.dev/` convention + per-branch preview deploys

**Status:** Design approved, awaiting implementation plan
**Date:** 2026-05-04
**Author:** Brainstorming session with Claude

## Goal

Turn env-manager into a self-hosted Heroku/Vercel-style PaaS for the home-lab Docker host. Any repo containing a `.dev/` directory becomes a deployable project. Every branch gets its own preview environment with a routable URL and live build logs. The default branch is the production environment.

## Non-goals

- Multi-host orchestration (single Docker host, as today).
- Kubernetes / Nomad / Swarm.
- Public-internet TLS provisioning (HTTP-only for now; LE/cert-manager is a follow-up).
- Cloudflare DNS API integration (manual external DNS for now; data model supports adding it later).
- Framework auto-detection (Laravel/Rails/Django migrations etc.).

## Convention

Every project repo has a `.dev/` directory at root containing:

```
.dev/
  Dockerfile.dev                  # base image for development environments
  docker-compose.prod.yml         # used when branch == default branch
  docker-compose.dev.yml          # used for every other branch (previews)
  config.yaml                     # project metadata (see schema below)
  secrets.example.env             # required user-supplied secrets (template, no values)
```

`config.yaml` schema:

```yaml
project_name: myapp                   # optional, defaults to repo name
external_domain: blocksweb.nl         # optional; without this prod stays internal
public_branches:                      # optional, branches besides default that get external_domain
  - develop
database:                             # optional, omit for no managed DB
  engine: postgres                    # postgres | mysql | mariadb
  version: "16"
```

Repos without `.dev/` cannot be onboarded through the new flow. Existing linked compose-projects continue to work via a `Kind=legacy` migration path (see "Migration").

## Data model

Three new entities replace the current `Repository` + `ComposeProject` pairing.

### `Project`

One row per onboarded repo.

| Field | Type | Notes |
|---|---|---|
| `ID` | string | `sha256(repo_url)[0:8]` |
| `Name` | string | from `config.yaml :: project_name`, defaults to repo name |
| `RepoURL` | string | unique (normalized) |
| `LocalPath` | string | clone directory |
| `DefaultBranch` | string | resolved from `origin/HEAD` at clone time, refreshable |
| `ExternalDomain` | string? | from `config.yaml :: external_domain` |
| `Database` | DBSpec? | engine + version; nil = no managed DB |
| `PublicBranches` | []string | branches besides default that get external-domain treatment |
| `Status` | enum | `active` \| `archived` \| `stale` |
| `CreatedAt` | time | |

### `Environment`

One row per active branch of a project.

| Field | Type | Notes |
|---|---|---|
| `ID` | string | `<project_id>:<branch_slug>` |
| `ProjectID` | string | FK to Project |
| `Branch` | string | raw branch name (e.g. `feature/user-auth`) |
| `BranchSlug` | string | slugified (`feature-user-auth`); collision-safe |
| `Kind` | enum | `prod` \| `preview` \| `legacy` |
| `URL` | string | computed (see "URL composition") |
| `ComposeFile` | string | `.dev/docker-compose.{prod,dev}.yml`, or legacy compose path |
| `Status` | enum | `pending` \| `building` \| `running` \| `failed` \| `destroying` |
| `LastBuildID` | string? | |
| `LastDeployedSHA` | string | |
| `CreatedAt` | time | |

`Kind = prod` iff `Environment.Branch == Project.DefaultBranch`. The kind transitions automatically if the default branch changes (e.g., `master` → `main`).

### `Build`

One row per deploy attempt. Log persisted to disk.

| Field | Type | Notes |
|---|---|---|
| `ID` | string | uuid |
| `EnvID` | string | FK |
| `TriggeredBy` | enum | `webhook` \| `manual` \| `branch-create` \| `clone` |
| `SHA` | string | commit being deployed |
| `StartedAt` / `FinishedAt` | time | |
| `Status` | enum | `running` \| `success` \| `failed` \| `cancelled` |
| `LogPath` | string | `/app/data/builds/<env-id>/latest.log` (single file per env, truncated on new build start) |

### Slugification

`BranchSlug = lowercase(branch).replace(/[^a-z0-9]+/g, '-').replace(/-+/g, '-').trim('-').slice(0, 30)`.

The 30-character cap keeps a full URL like `<slug>.<project>.<base_domain>` comfortably under the 63-char DNS label limit even for long project names.

If two branches slug to the same value, the second one to spawn gets `-<hash6>` appended (`hash6 = sha256(branch)[0:6]`). If the branch slugs to empty (only special chars), the env is rejected with a build error.

### URL composition

Let `base = external_domain` if set and (branch == default or branch in `public_branches`), else `home`.

- Prod env (`Kind=prod`): `<project_name>.<base>` — e.g. `myapp.blocksweb.nl` or `myapp.home`
- Preview env: `<branch_slug>.<project_name>.<base>` — e.g. `feature-x.myapp.home`

## Lifecycle flows

### Flow A — Project onboarding (clone + initial spinup)

1. User submits `POST /api/v1/projects { repo_url, token? }` from the UI.
2. Backend clones the repo (using PAT or per-URL token from credential store).
3. If `.dev/` is missing, backend rejects with 400 and a message describing the required layout.
4. Backend parses `.dev/config.yaml`, resolves `origin/HEAD`, writes `Project` row.
5. Backend creates a prod `Environment` for the default branch.
6. If `Project.Database != nil`, backend ensures the project DB container is running (creates if missing).
7. Backend enqueues a `Build` with `TriggeredBy=clone`. UI redirects to the build log viewer.

### Flow B — Push to a known branch

1. GitHub `push` webhook lands at `/api/v1/webhook/github` with HMAC verified.
2. Backend matches `payload.repository.clone_url` to a `Project` (URL normalization tolerant of `.git` and trailing slashes).
3. Look up `Environment` for `payload.ref`'s branch.
4. **If exists:** enqueue `Build(env, trigger=webhook, sha=payload.head)`.
5. **If doesn't exist:** sparse-fetch the pushed branch, check `.dev/` is present in the tree.
   - If absent: ignore (branch hasn't opted into the deploy convention).
   - If present: create `Environment(branch, kind=preview)`, provision branch database (if managed DB), enqueue `Build`.

### Flow C — Branch deletion

1. GitHub `delete` webhook with `ref_type=branch` lands.
2. Backend finds the matching `Environment`. Prod envs are exempt — never auto-destroyed.
3. Status → `destroying`. Run `docker compose -p <env-id> down -v`.
4. Drop the env's database in the project DB container, drop the env's DB user.
5. Remove `/app/data/envs/<env-id>/` directory.
6. Delete `Environment` row. Traefik labels disappear when the container does.

### Flow D — Build execution + log streaming

A per-env build queue serializes builds for that environment (concurrency = 1 per env, parallelism across envs).

1. Pull repo to per-env worktree at `/app/data/envs/<env-id>/repo`. Checkout target branch and SHA.
2. Read `.dev/docker-compose.{prod,dev}.yml`.
3. Render compose:
   - Inject Traefik labels (extending existing `proxyManager.InjectTraefikLabels` to also append the macvlan network).
   - Inject DB connection vars (`DATABASE_URL`, `DB_HOST`, etc.) if managed DB.
   - Inject platform vars (`PROJECT_NAME`, `BRANCH`, `ENV_KIND`).
   - Merge user secrets from credential store namespace `env:<env-id>:user`.
4. Truncate `/app/data/builds/<env-id>/latest.log` and open for write; allocate an in-memory ring buffer for fan-out. The previous build's log content is replaced — only the most recent build's log is retained per env (per Q8 decision).
5. Run `docker compose -p <env-id> up -d --build`, tee output to file + ring.
6. On exit code 0: `Status=running`, `Build.Status=success`, update `LastDeployedSHA`.
7. On non-zero: `Status=failed`, `Build.Status=failed`. **Existing containers from the prior successful deploy are left running** — `docker compose up -d` is graceful about same-project redeploys.

WebSocket subscribers connect to `/api/v1/envs/<env-id>/build-logs`. Server tails the log file from disk; on EOF, switches to ring-buffer subscription if a build is in flight, so late joiners get historical bytes followed by live stream without missing any. If a new build starts while a client is connected, the connection emits a `build_superseded` event and reattaches to the new build's stream.

### Flow E — Manual trigger / rollback

`POST /api/v1/envs/<env-id>/build` (optional `?sha=<sha>`) enqueues a `Build` with `TriggeredBy=manual` at the specified SHA (or branch HEAD if no SHA).

### Flow F — Reconcile on startup

On env-manager boot:

1. For each `Project`, list remote branches via `git ls-remote`.
2. For each branch present remotely but no `Environment` exists locally → spawn one (Flow B's "doesn't exist" path).
3. For each `Environment` whose branch is no longer present remotely → destroy (Flow C).
4. Prod env is exempt from teardown even if default branch is missing.

## Resource provisioning

### Networks

- **Per-project bridge:** `<project_id>_net`, joined by every env in the project. Lets app containers reach the project DB container.
- **macvlan:** every env's user-facing service joins `my-macvlan-net` so Traefik can route to it.

### Volumes

Compose-declared named volumes are scoped per env via `docker compose -p <env-id>`. Cleanup on destroy: `down -v`.

### Database (per-branch separate database, shared instance)

If `Project.Database != nil`:

- One container per project named `<project_id>_db`, image `postgres:<version>` (or `mysql`/`mariadb`), joined to the project bridge. Lifecycle independent of any env — created on first project provisioning, destroyed only on full project deletion.
- For each new `Environment`, env-manager opens a superuser connection and runs:
  ```sql
  CREATE DATABASE "<project_id>_<branch_slug>";
  CREATE USER "<branch_slug>" WITH PASSWORD '<generated>';
  GRANT ALL PRIVILEGES ON DATABASE "<project_id>_<branch_slug>" TO "<branch_slug>";
  ```
  (MySQL/MariaDB equivalent.)
- Generated password stored in credential store at `env:<env-id>:db_password`.
- At build time, env-manager injects:
  ```
  DATABASE_URL=postgres://<user>:<pw>@<project_id>_db:5432/<project_id>_<branch_slug>
  DB_HOST=<project_id>_db
  DB_PORT=5432
  DB_DATABASE=<project_id>_<branch_slug>
  DB_USERNAME=<branch_slug>
  DB_PASSWORD=<from credential store>
  ```
- On env destroy: `DROP DATABASE` + `DROP USER`.

Tradeoff (accepted): a runaway migration on one preview branch can affect DB load for other previews. Acceptable for a homelab dev environment; documented for users.

### DNS

- **Internal:** env-manager appends `<env_url> → 192.168.1.6` records to its existing CoreDNS Corefile shim. Each env owns its row, removed on destroy.
- **External:** out of scope for auto-creation. When a project sets `external_domain`, env-manager prints the required Cloudflare DNS record on env creation:
  ```
  Set this DNS record at your provider:
    Type:  A
    Name:  myapp.blocksweb.nl
    Value: 84.84.207.234
    Proxy: on (Cloudflare orange cloud) for HTTP, off for non-HTTP entrypoints
  ```

### Credentials

Existing `credentials.Store` (AES-encrypted) gains two new namespaces per env:

- `env:<env-id>:db_password` — generated DB password
- `env:<env-id>:user` — user-supplied secrets parsed from `.dev/secrets.example.env`

UI shows a "Required secrets" form on first env creation, derived from the `secrets.example.env` template. Preview envs default to inheriting prod's user secrets — implemented as a **snapshot at env creation time**, not a live reference. Updating prod secrets does not propagate to existing previews; previews keep their snapshotted values until manually updated. This avoids unintended cross-env effects when rotating prod credentials.

### Platform-injected env vars

Always present in every container:

- `PROJECT_NAME`
- `BRANCH` (raw)
- `ENV_KIND` (`prod` \| `preview` \| `legacy`)
- `ENV_URL` (the assigned URL)
- DB vars listed above (if managed DB)
- All `env:<env-id>:user` entries

## UI

### Pages

1. **Projects list** (`/projects`) — replaces today's `/repos`. One row per project, badges for env counts, last-build timestamp.
2. **Project detail** (`/projects/<id>`) — env list with status pills, URLs, and per-env Logs / Redeploy actions.
3. **Build log viewer** (`/projects/<id>/envs/<env-id>/builds/<build-id>`) — terminal-style log pane, ANSI color preserved, autoscroll toggle. WS for live builds; tail-from-disk for historical.
4. **Add project modal** — single-step form (repo URL, optional PAT). On submit, transitions to live build log.
5. **Project settings drawer** — name, default branch, DB engine/version (read-only post-create), `public_branches`, danger-zone destroy.
6. **Per-env settings modal** — URL override (advanced), inherit-prod-secrets toggle, per-env secret overrides, manual destroy (preview only).

### Pages removed (folded into Projects)

- `/repos`
- `/compose-projects`
- The "link compose project to repo" modal

### Pages unchanged

- Containers / Networks / Volumes (Docker-level inspection)
- Dashboard, settings, credential store browser, GitHub PAT management
- Stats / system metrics

### Build log streaming hook

WebSocket connection to `/api/v1/envs/<env-id>/build-logs`. Server-side semantics:

1. Open the env's `latest.log` file on disk.
2. Stream existing bytes to the client.
3. On EOF: if a build is currently running for this env, register the client to the in-memory ring buffer; otherwise close (historical-only view).
4. When `Build.Status` becomes terminal and the ring is drained, close the connection.
5. If a new build starts while the client is attached, emit a `{type: "build_superseded", build_id: "<new-id>"}` JSON frame, truncate the client's view, and reattach to the new build's stream.

## Migration

Run-once migration on first boot of the new binary:

1. Read every existing `ComposeProject`.
2. **Linked to a repo:** create a `Project` row pointing at the repo, create one `Environment(Kind=legacy)` referencing the existing compose file path. No `.dev/` required for legacy envs.
3. **Unlinked compose project:** create a synthetic `Project(RepoURL="")` with one legacy env underneath.
4. Migration is metadata-only — no containers restart.

After migration, every previously-working deploy continues working under the new model. Legacy envs do not get preview environments; the user must re-onboard with `.dev/` to opt in.

**Legacy env runtime semantics:** legacy envs keep whatever Traefik labels and URL their existing compose file specifies — env-manager does not rewrite labels or compose existing URLs into the new `<project>.<base>` scheme. The legacy env's `URL` field in the data model is populated by parsing existing labels for display purposes only. New URL composition rules apply only to `Kind=prod` and `Kind=preview` envs.

## Rollout sequence

Each step is a deployable commit; system stays usable between steps.

1. Schema + migration (additive tables, run-once migrator). No new behavior.
2. `.dev/` parser + `POST /api/v1/projects`. UI hidden behind a feature flag.
3. Builder + log streaming, API-triggerable only.
4. DB provisioning (project DB container, branch-database creation, credential injection).
5. Webhook v2 (push triggers new builder for `Project` repos; legacy still uses `RebuildLinkedProjectsForRepo`).
6. Branch-delete handler (gate behind per-project toggle for the first week).
7. Reconcile-on-startup.
8. New Projects UI behind a feature flag, dogfooded on one repo.
9. Flip default UI to v2; old routes redirect.
10. Remove old Repos / ComposeProjects UI + handlers.

Steps 1–5 are additive and revert-safe. Step 6 is the first destructive new behavior. Step 9 breaks bookmarks. Step 10 deletes code paths — keep two release tags between 9 and 10.

## Error handling and edge cases

### Build failures

- Non-zero exit → log saved, `Build.Status=failed`, env keeps prior containers.
- Compose render fails → no Docker calls, error in log.
- Image build fails → partial layers stay in cache for retry.
- DB provisioning fails → build aborts before compose-up; env at prior state.

### Repo-side issues

- Repo deleted/renamed → `Project.Status=stale`, running envs untouched, surfaced in UI.
- `.dev/` removed from a non-default branch → preview stops getting builds; existing containers run until branch is deleted.
- `.dev/` removed from default branch → `Project.Status=archived`, prod env keeps running, no new builds. Manual unarchive after `.dev/` is restored.

### Slug edge cases

- Branch named with only special chars → slug empty → reject build.
- Slug exceeds 30 chars → truncate + append `-<hash6>`.
- Multiple branches slug to same value → second/third get `-<hash6>` for uniqueness.

### Webhook reliability

- Invalid signature → 401, no action.
- Unknown repo → 200 ignore (don't leak knowledge of which repos are tracked).
- Out-of-order delivery → tolerated; reconcile-on-startup makes eventual state correct.
- Long build (>10s) → GitHub times out, retries on next push, host-side build keeps running.

### DB failure modes

- Project DB OOM-killed → connection injection fails, build fails, retry on next push.
- Branch-database already exists (slug collision after incomplete destroy) → `IF NOT EXISTS`, log warning.
- DB engine version change in `config.yaml` → not auto-migrated. UI surfaces a warning.

### Race conditions

- Push during clone-time provisioning → queue serializes, second build runs after first.
- Branch-delete while env is `building` → defer destroy until build completes.
- Concurrent "Add project" for same repo → unique constraint on normalized `RepoURL` → second returns 409.

### Storage growth

- Build logs: 5MB × 100 envs ≈ 500MB worst case. Acceptable.
- Per-env worktrees: full clone per env. 100MB repo × 50 active envs = 5GB. Mitigation deferred until it surfaces: shared bare repo + per-env shallow worktrees.

## Testing

### Unit (Go, fast)

- Slugifier — table-driven.
- `.dev/config.yaml` parser — happy path, all-optional fields, invalid YAML, unknown fields.
- URL composer — `(Project, Environment, base) → URL`.
- Compose injector — golden-file based: parsed compose + injected vars + Traefik labels + network → expected output.
- Branch event handlers — webhook payload + state → next state, no Docker calls.

### Integration (Go + dockerized harness, ~5–10 min in CI)

- Project provisioning end-to-end (clone, env created, build runs, container up, URL responds).
- Push triggers build (mutate test repo, send webhook, assert new build).
- Branch delete tears down (env row gone, container gone, DB-database dropped).
- Build failure preserves prior state (failed build leaves prior containers running).
- Reconcile-on-startup (state drift in both directions, assert convergence).

### Manual rollout checklist

`docs/superpowers/specs/2026-05-04-dev-env-rollout-checklist.md` (created during implementation) lists per-step manual verifications. Examples:

- After step 4: spin up test repo with managed Postgres, confirm `DATABASE_URL` correct in container.
- After step 6: delete a test branch, confirm env is gone within 60s.
- After step 9: every legacy env still operates in the new UI.

### Frontend

- Vitest unit tests for slug + URL helpers (mirrors of backend logic for client-side rendering).
- Component tests for `EnvironmentRow`, `BuildLogViewer`, `AddProjectModal`.
- No Playwright until UI stabilizes.

### Out of scope for testing

- Cloudflare integration (out of scope feature-wise).
- TLS provisioning (out of scope feature-wise).
- Performance / load testing (single-user homelab).
- UI snapshot tests (too brittle for an evolving UI).

## Implementation note

This spec is large enough that a single implementation plan would not fit comfortably. The expected pattern is **one implementation plan per rollout step** (so ~10 plans total, written and executed sequentially, each one corresponding to a numbered step in "Rollout sequence"). The design decisions in this spec are shared context across all those plans; the plans themselves break down the per-step technical work.

## Open questions deferred to implementation

- Exact API path/verbs (`/api/v1/projects` vs `/api/v2/projects`) — decided during step 2.
- Build queue persistence on restart (in-memory vs disk-backed) — decided during step 3 based on observed restart frequency.
- Whether `Build` rows are pruned on a TTL or kept forever — defer until storage growth shows up.
- Whether to support multiple compose files per env (`docker-compose.override.yml`) — not in this spec; future addition if needed.
