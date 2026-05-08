---
name: blocksweb-homelab-deploy
description: How to deploy a project to the Blocksweb home-lab so it reaches *.home — uses env-manager v2's REST API at http://manager.home/api/v1/. Requires the .dev/ convention in the repo and an ENVM_TOKEN in the environment. Engineer-only.
version: 2.0.0
metadata:
  hermes:
    tags: [deploy, home-lab, env-manager, blocksweb]
    category: devops
---

# Home-lab deploy cookbook (Engineer)

The home-lab at `192.168.1.116` runs **env-manager v2** — a `.dev/`-based PaaS.
You add a project (Git URL), and every branch with a `.dev/` directory becomes
a live environment: the default branch deploys to prod, every other branch is
a preview env at `<branch-slug>.<project>.home`. Push to deploy. Delete the
branch to tear it down.

You drive it via `curl http://manager.home/api/v1/...`. **Mutating endpoints
require the admin token** in the `Authorization: Bearer` header — read it
from the `ENVM_TOKEN` environment variable in your container.

## Hard rules

- **Never deploy `BTC-Direct/*` repos to the home-lab.** Same hard rule as everywhere else.
- **Never `DELETE /api/v1/projects/{id}` or `POST /api/v1/envs/{id}/destroy` without explicit human 👍 in `#engineering`.** Project deletion drops all environments and the per-env database/Redis ACLs. Irreversible.
- **Don't redeploy live blocksweb-* web properties to the home-lab.** Those go to Vercel / Laravel Cloud. The home-lab is for *internal tools and experiments only*.
- **Always check `ENVM_TOKEN` is set** before attempting any mutating call. If not set, surface a clear error to the user and stop — don't silently 401.

## The .dev/ convention

Every project you deploy MUST have a `.dev/` directory at the repo root:

```
.dev/
├── config.yaml                 # Project metadata
├── docker-compose.prod.yml     # Stack for the default branch
└── docker-compose.dev.yml      # Stack for every preview branch
```

Reference example with all fields documented:
**`https://github.com/Ultiimaz/environment-manager/tree/master/docs/example-project/.dev/`**

Minimum `.dev/config.yaml`:
```yaml
project_name: my-app
expose:
  service: web        # service in compose to route external traffic to
  port: 8080          # port that service listens on inside the container
services:
  postgres: false     # set true to get a per-env DATABASE_URL injected
  redis: false        # set true to get a per-env REDIS_URL injected
```

## End-to-end deploy flow

### 1. Prep the repo

If the repo doesn't already have `.dev/`, write the three files. Copy
`docs/example-project/.dev/` from the env-manager repo as a starting point.
Push to GitHub.

### 2. Onboard the project

```bash
curl -sX POST http://manager.home/api/v1/projects \
  -H "Authorization: Bearer $ENVM_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/Ultiimaz/<repo>",
    "token": "ghp_..."
  }'
```

Returns:
```json
{
  "project": { "id": "<project-id>", "name": "...", ... },
  "environment": { "id": "<env-id>", "branch": "main", ... },
  "required_secrets": ["NAMES", "FROM", ".dev/config.yaml#secrets"]
}
```

The `token` field is the GitHub PAT — only required for private repos. For
public Ultiimaz/* repos, omit it.

### 3. Set required secrets (if any)

If `required_secrets` is non-empty, the prod build will fail until you set
them. From `engineer.env` you have GITHUB_TOKEN; for app-specific secrets
ask the user via Discord, then:

```bash
curl -sX PUT "http://manager.home/api/v1/projects/<project-id>/secrets" \
  -H "Authorization: Bearer $ENVM_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"STRIPE_API_KEY": "sk_...", "SESSION_KEY": "..."}'
```

### 4. Trigger the first build

The webhook fires automatically on `git push`. If the project was just
onboarded from an existing repo, trigger explicitly:

```bash
curl -sX POST "http://manager.home/api/v1/envs/<env-id>/build" \
  -H "Authorization: Bearer $ENVM_TOKEN"
```

Returns 202 with `{"build_id": "...", "env_id": "..."}`.

### 5. Poll until terminal status

```bash
curl -s "http://manager.home/api/v1/envs/<env-id>/builds" | head -200
```

Each entry has a `status` field: `running`, `success`, `failed`, `cancelled`.
**Wait for the latest build's status to be terminal before reporting back.**
Reasonable poll: every 5 seconds, give up after 5 minutes.

### 6. On failure: read the log

```bash
curl -s "http://manager.home/api/v1/builds/<build_id>/log" | tail -100
```

Diagnose, push a fix to the same branch (this auto-triggers another build),
or surface the error to the user via Manager if you can't fix it.

### 7. Verify the deployed URL

When build status flips to `success`, fetch the env URL:

```bash
curl -s "http://manager.home/api/v1/projects/<project-id>" | head -50
```

The env's `url` field is the *.home address. Smoke-test it:

```bash
curl -sI "http://<env.url>/" | head -5
```

A 200 / 301 / 302 / 404 with a real Server header means the container is up
and Traefik is routing. A connection refused or empty response means the
container crashed or the port mismatch — re-read the build log + check
`expose.port` in `.dev/config.yaml`.

### 8. Report back

Tell the user (via Manager) the deployed URL, the build duration, and
anything notable (which secrets they still need to set, which env vars
were defaulted, etc.).

## Iteration on existing projects

User asks for a change to an already-deployed project. Don't onboard again —
the project still exists. Instead:

1. Locate the project: `GET /api/v1/projects` returns all projects;
   match on `name` or `repo_url`.
2. `git clone` the repo into your scratch dir.
3. Make the change, commit, push to the same branch.
4. The webhook auto-triggers a build. Poll status as in Step 5.
5. On success, smoke-test and report.

If the user asks for a "preview" or wants to try something risky, push to a
new branch (e.g. `feature/whatever`). env-manager creates a preview env
automatically at `<branch-slug>.<project>.home`.

## Common gotchas

- **`expose.port` mismatch**: most common build-passes-but-not-reachable
  cause. The compose service must actually listen on the port declared in
  `.dev/config.yaml#expose.port`. Verify with `docker inspect` if needed.
- **Healthcheck failing**: env-manager waits for a healthcheck in the prod
  compose. Without one it'll mark the env "running" but Traefik won't route
  until the service responds. Add a healthcheck to the compose service.
- **Public repo + `token`**: don't send a `token` field for public Ultiimaz/*
  repos — env-manager treats it as private and adds auth headers that fail
  on public clones. Omit `token` entirely for public repos.
- **Webhook not firing**: check the repo has a webhook to
  `https://callback.blocksweb.nl/env-manager/hooks/env-manager` with the
  HMAC secret. Webhook is auto-created on onboard; if you onboarded an old
  repo without re-creating it, do that manually via the GitHub UI.
- **LAB_MODE**: the home-lab runs with `LAB_MODE=true`, so read endpoints
  (`GET /projects`, `/builds/{id}/log`, etc.) work without auth. Mutating
  ones still need the Bearer header. If you ever run against a customer
  deployment with `LAB_MODE=false`, add the header to every call.

## Useful commands

```bash
# List all projects you have access to
curl -s http://manager.home/api/v1/projects | head -50

# Live runtime logs (WebSocket; use websocat or similar from CLI)
# /ws/envs/<env-id>/runtime-logs?token=$ENVM_TOKEN

# Latest build for an env
curl -s "http://manager.home/api/v1/envs/<env-id>/builds" \
  | head -1 | grep -oE '"id":"[^"]*"' | head -1

# Topology snapshot
curl -s http://manager.home/api/v1/topology
```
