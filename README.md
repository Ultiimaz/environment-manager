# env-manager

A Git-driven PaaS for self-hosting on a single Linux box. Every Git
branch in a project becomes a live preview environment — push to deploy,
delete the branch to tear it down. Built for homelabs and small teams
that want Vercel-style ergonomics without paying Vercel.

```
        push to GitHub                .dev/ convention                Traefik
            │                                │                            │
            ▼                                ▼                            ▼
┌──────────────────────┐         ┌────────────────────────┐         ┌─────────────────┐
│  /webhook/github     │  ──▶   │  builder spawns env     │  ──▶   │  preview URL    │
│  (HMAC-signed)       │         │  per branch from .dev/  │         │  *.your-domain  │
└──────────────────────┘         └────────────────────────┘         └─────────────────┘
            │                                │
            │                                ▼
            │                    ┌────────────────────────┐
            │                    │  paas-postgres         │
            ▼                    │  paas-redis            │
   delete branch → teardown      │  (per-env DB / ACL)    │
                                 └────────────────────────┘
```

## What you get

- **Push-to-deploy**: every branch with a `.dev/` directory gets a Docker
  Compose stack, a branch URL, and live build logs.
- **Cheap previews**: production stacks run from `.dev/docker-compose.prod.yml`,
  every other branch uses `.dev/docker-compose.dev.yml`. One file per
  environment kind, no per-branch configuration drift.
- **Per-env Postgres / Redis**: opt into shared singletons by setting
  `services.postgres: true` / `services.redis: true` in `.dev/config.yaml`.
  Each environment gets its own database / ACL user, dropped when the
  environment is destroyed.
- **Dark, dense UI**: project list, environment grid, runtime + build log
  streaming, topology graph, search — all behind a single green-accent
  Linear-style theme.
- **CLI**: `envm` ships alongside the server for scripted operations:
  build, secrets, projects, backup, license issuance.

## Quick start (homelab)

Requires Linux + Docker. Runs in a single container behind Traefik.

```bash
git clone https://github.com/Ultiimaz/environment-manager
cd environment-manager
cp .env.example .env
# edit .env to set BASE_DOMAIN and CREDENTIAL_KEY
docker compose up -d
```

Visit `http://manager.<your-base-domain>`. The first-boot admin token is
printed once to the server log:

```
docker logs env-manager 2>&1 | grep "admin token"
```

Save it via the UI's Settings page — it goes to localStorage and is sent
on every mutating request.

## How a project works

Add a Git URL via the UI or `envm projects onboard <git-url>`. The
project must contain a `.dev/` directory at the repo root:

```
.dev/
├── config.yaml                   # project name, services, public branches
├── docker-compose.prod.yml       # main / default branch
└── docker-compose.dev.yml        # all other branches
```

`config.yaml` minimum:

```yaml
project_name: my-app
services:
  postgres: true       # optional — provisions a per-env DB + DATABASE_URL
  redis: false
public_branches: [main]      # which branches get a real domain (vs preview-only)
expose:
  service: web              # which compose service Traefik routes to
  port: 8080
```

A push to `main` redeploys the prod environment. A push to any other
branch with a `.dev/` directory creates a preview env at
`<branch-slug>.<project>.<base-domain>`. Deleting the branch tears the
env down on the next webhook event.

## Configuration

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `BASE_DOMAIN` | `localhost` | Base domain for project + manager URLs |
| `PORT` | `8080` | HTTP listen port |
| `DATA_DIR` | `./data` | Server state directory (must persist across restarts) |
| `STATIC_DIR` | `./static` | Frontend bundle (set by the Dockerfile) |
| `CREDENTIAL_KEY` | _required_ | 32-byte AES-GCM key for the credential store |
| `LETSENCRYPT_EMAIL` | _empty_ | If set, Traefik issues real certs for public branches |
| `GIT_REMOTE` | _empty_ | Optional remote for syncing project state |

### Security knobs (sold-product builds)

| Variable | Default | Purpose |
|---|---|---|
| `LAB_MODE` | `true` | Read endpoints + WS streams open on LAN. Set `false` to require Bearer auth on every endpoint |
| `LICENSE_ENFORCE` | `false` | Verify a signed `.lic` file at boot; mutating endpoints return 402 when invalid |
| `LICENSE_PUBLIC_KEY` | _empty_ | Base64 Ed25519 public key embedded by the publisher |
| `LICENSE_FILE` | `<DATA_DIR>/license.lic` | Path the watcher reads |

`LAB_MODE=true` (the homelab default) keeps the UI usable without
authentication on a trusted LAN. Flip it to `false` for any deployment
reachable from networks you don't fully trust — Bearer auth then applies
to read endpoints AND WebSocket log streams (clients pass the token via
`?token=` query param).

## Operations

### Backups

```bash
envm backup --out env-manager-backup.tar.gz
```

The endpoint streams a tar.gz of the entire data dir (project state +
encrypted credential store + build logs). Always admin-auth, regardless
of `LAB_MODE`. Schedule it on cron and rotate offsite. Restoring is
manual:

```bash
docker stop env-manager
tar xzf env-manager-backup.tar.gz -C /opt/env-manager/data
docker start env-manager
```

### License enforcement (sold-product builds only)

The default build runs unconstrained — fine for personal use and CI.
When you sell this product, generate a keypair once and embed the
public key in customer images:

```bash
envm license gen-keypair
# LICENSE_PUBLIC_KEY=...   ← embed in compose template
# LICENSE_PRIVATE_KEY=...  ← keep in your secret manager
```

Issue per-customer licenses on demand:

```bash
envm license issue \
  --to "Acme Corp" \
  --days 365 \
  --max-projects 10 \
  --private-key "$LICENSE_PRIVATE_KEY" \
  --out acme.lic
```

Ship `acme.lic` to the customer; they drop it at `LICENSE_FILE` (default
`<DATA_DIR>/license.lic`) and set `LICENSE_ENFORCE=true`. The watcher
re-verifies hourly so expiry is picked up live; mutating endpoints
return `402 Payment Required` when invalid, the UI shows a banner, and
read endpoints stay available so the customer can still extract their
data.

## API

`/api/v1/` base path. Bearer auth header on mutating routes (always)
and on read routes (when `LAB_MODE=false`).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Liveness |
| `GET` | `/projects` | List projects |
| `POST` | `/projects` | Onboard a Git URL |
| `GET` | `/projects/{id}` | Project + envs |
| `GET` | `/projects/{id}/secrets` | List secret keys (no values) |
| `PUT` | `/projects/{id}/secrets` | Set secrets |
| `POST` | `/envs/{id}/build` | Trigger build |
| `POST` | `/envs/{id}/destroy` | Tear down env |
| `GET` | `/envs/{id}/builds` | Build history |
| `GET` | `/builds/{id}/log` | Historical log |
| `WS` | `/ws/envs/{id}/build-logs` | Live build log |
| `WS` | `/ws/envs/{id}/runtime-logs` | Live container log |
| `GET` | `/services/postgres` \| `/services/redis` | Singleton status |
| `GET` | `/topology` | Graph: services ↔ envs ↔ projects |
| `GET` | `/settings` | Server config + license status |
| `GET` | `/admin/backup` | Stream tar.gz of data dir |
| `POST` | `/webhook/github` | HMAC-signed |

## Development

```bash
# Backend (Go 1.24+)
cd backend
go test ./...
go run ./cmd/server

# Frontend (Node 20, pnpm 10)
cd frontend
pnpm install --frozen-lockfile
pnpm dev   # proxies /api → :8080
```

CI runs `go vet`, `go test -race`, and `pnpm build` on every PR.

## License

This repository's source license is TBD (no `LICENSE` file checked in
yet — pick one before publishing the public sold-product build).

Independent of that, the runtime supports two enforcement modes:
unconstrained (default, no `.lic` needed) and license-enforced (for
sold-product distributions). They are the same code; `LICENSE_ENFORCE=true`
flips the gate.
