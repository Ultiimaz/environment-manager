---
name: ship-a-new-product
description: When the user asks to build a new piece of software (e.g. "build me a calendar app", "make a todo list", "I need a habit tracker") and have it deployed live to the home-lab, run this. Walks the full lifecycle: spec → scaffold → push → onboard → verify → report. Engineer-only.
version: 1.0.0
metadata:
  hermes:
    tags: [build, ship, end-to-end, home-lab, blocksweb]
    category: devops
---

# Ship a new product end-to-end

The user's vision: they describe an app in plain language ("calendar", "todo
list", "kanban for myself"), and you ship it live to a `*.home` URL they can
use immediately. They iterate by replying in Discord; each iteration goes
through this same skill.

## When to invoke

Trigger phrases include: "build me a", "make me a", "I want a", "ship a",
"deploy a new", "create an app for", "I need a tool that". When in doubt,
read the request and ask: *does the user expect software they can use after
my reply?* If yes, this skill applies.

If the request is about an **existing** project (e.g. "fix the login on the
calendar app"), skip the spec + scaffold steps and jump straight to
**Iteration**, below.

## Hard rules (inherited from blocksweb-homelab-deploy)

- Repos go under `Ultiimaz/`, never `BTC-Direct/`.
- Live blocksweb-* web properties stay on Vercel/Laravel Cloud — this skill
  is for *internal tools and experiments only*.
- Never destroy a project (`DELETE /api/v1/projects/{id}` or `POST /destroy`)
  without explicit human 👍 in `#engineering`.

## Flow

### Step 1 — Spec (always do this, even if briefly)

The user's ask is usually under-specified. Reply in the Discord channel with
a **terse spec** before scaffolding so they can correct you cheaply.

For most simple apps, a 4-line spec is enough:

```
Spec for `calendar-app`:
- Stack: Vite + React + TypeScript, FastAPI backend, Postgres
- Features: month view, add/edit events, no auth (single-user)
- Auth: none yet (LAN-only via *.home)
- Persistence: per-env Postgres via env-manager (DATABASE_URL injected)

Building unless you say otherwise. Reply 'go' to confirm or correct any line.
```

**If the user says "go" or stays silent for >2 minutes, proceed.** Don't
block forever waiting for an OK on tiny apps. For anything that touches
auth, money, or external integrations: wait for explicit confirmation.

### Step 2 — Scaffold via Claude Code CLI

Use `claude` (Claude Code CLI v2.1+, already installed at
`/opt/data/.npm-global/bin/claude`) to generate the code. **Run it in a
scratch dir under `/tmp/`** so failed attempts don't pollute persistent
volumes.

```bash
WORK=/tmp/build-$(date +%s)
mkdir -p "$WORK" && cd "$WORK"
git init -b main

claude -p "$(cat <<'PROMPT'
Build a minimal but working <app type> with these requirements:
<paste the spec>

Constraints:
- Single-container app (one Dockerfile, listens on port 8080).
- Use a frontend framework that builds to static files, served by the
  backend (no separate frontend container).
- Add a /health endpoint that returns 200.
- Persist data to Postgres if services.postgres is true; the
  DATABASE_URL env var will be injected at runtime by env-manager.
- Include Dockerfile and the .dev/ directory described below.

REQUIRED FILES:
1. .dev/config.yaml — fill in:
     project_name: <slug>
     expose: { service: web, port: 8080 }
     services: { postgres: <true/false>, redis: false }
2. .dev/docker-compose.prod.yml — single 'web' service, healthcheck
   on /health, restart unless-stopped.
3. .dev/docker-compose.dev.yml — same as prod but with deploy.resources
   memory limit 256M.
4. Dockerfile — multi-stage build, final image listens on :8080.
5. README.md — one paragraph: what it does, how to run locally.

Reference the .dev/ schema at:
https://github.com/Ultiimaz/environment-manager/tree/master/docs/example-project

Write all files. Make it actually run — verify with a local docker build at
the end. Print "DONE" when complete.
PROMPT
)" --allowedTools 'Read,Edit,Write,Bash' --max-turns 30
```

**If the agent prints "DONE" or runs `git status` showing files**, move on.
**If max-turns is hit**, read the last few turns — usually the last error
message tells you what's broken. Fix it manually if cheap, otherwise
re-prompt with a tighter scope.

### Step 3 — Smoke-test locally

Don't ship something that doesn't even build:

```bash
cd "$WORK"
docker build -t scratch-build:latest . 2>&1 | tail -20
```

If the build fails, surface the error (in `#engineering` or via Manager) and
stop. Don't push a known-broken state — the env-manager build will fail
the same way, but the round-trip is much slower.

### Step 4 — Create the GitHub repo

```bash
NAME=<derived-from-spec-slug>           # e.g. calendar-app, todo-list
gh repo create Ultiimaz/$NAME --private --source=. --push --description "Built by Hermes Engineer for Ultiimaz"
```

`--private` by default. The user can flip to public via the GitHub UI later.

### Step 5 — Onboard to env-manager

```bash
curl -sS -X POST http://manager.home/api/v1/projects \
  -H "Authorization: Bearer $ENVM_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"repo_url\": \"https://github.com/Ultiimaz/$NAME\"}"
```

Capture the response — extract `project.id` and `environment.id` (this is
the prod env, since it onboarded from the default branch).

If the response includes `required_secrets: ["X", "Y"]`, the user needs to
provide values. Ask via Manager and PUT them with the secrets endpoint
(see `blocksweb-homelab-deploy` skill).

### Step 6 — Wait for first build, verify

The webhook auto-triggers the first build on onboard. Poll:

```bash
for i in $(seq 1 60); do
  STATUS=$(curl -s -H "Authorization: Bearer $ENVM_TOKEN" \
    "http://manager.home/api/v1/envs/<env-id>/builds" \
    | head -1 | grep -oE '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
  echo "$i: $STATUS"
  case "$STATUS" in
    success|failed|cancelled) break ;;
  esac
  sleep 5
done
```

If `success`: smoke-test the URL.

```bash
URL=$(curl -s "http://manager.home/api/v1/projects/<project-id>" \
  | grep -oE '"url":"[^"]*"' | head -1 | cut -d'"' -f4)
curl -sI "http://$URL/health" | head -3
```

Expect `HTTP/1.1 200`. If you get a connection refused, the port mismatch
is the most likely cause — check `expose.port` matches the listener.

If `failed`: fetch the build log, diagnose, push a fix to the same branch
(auto-retriggers), poll again. Three attempts max — if it's still failing
after three rounds, escalate via Manager.

### Step 7 — Report back

Single message via Manager:

```
✅ Shipped `<name>` to http://<url>/
   Build: <duration>, status: success
   Repo: https://github.com/Ultiimaz/<name>
   Stack: <stack from spec>
   Notes: <anything the user should know — e.g. "Postgres data persists
          across deploys, login is not gated, etc.">
```

## Iteration on existing projects

User reply: "add login to the calendar app" or "the date picker is broken".

1. **Find the project.** Get all projects, match by name keyword:
   ```bash
   curl -s http://manager.home/api/v1/projects \
     | grep -oE '"id":"[^"]*"|"name":"[^"]*"|"repo_url":"[^"]*"'
   ```
   If multiple match, ask the user which one.
2. **Clone the repo** into a fresh `/tmp/iter-...` dir.
3. **Make the change** via Claude Code CLI with a scoped prompt:
   ```bash
   claude -p "User reported: \"<their message>\". The repo is at $(pwd).
              Make the change, run tests if any exist, build the Docker
              image to verify, then commit and push to main." \
     --allowedTools 'Read,Edit,Write,Bash' --max-turns 20
   ```
4. **Push** triggers a rebuild via webhook.
5. **Poll + smoke-test** as in Step 6 above.
6. **Report**. Mention what changed, link to the new commit.

If the user reports a bug that's reproducible by the smoke-test, fix it
locally first, smoke-test, *then* push. Don't push speculative fixes.

## Failure modes & escalation

- **`gh repo create` 401**: GITHUB_TOKEN lacks repo scope. Escalate to user.
- **env-manager onboard 401**: `ENVM_TOKEN` is empty or invalid.
  Escalate to user (need to refresh the token via
  `docker exec env-manager envm admin-token show`).
- **Build fails on a syntax error in Dockerfile/compose**: fixable via
  Claude Code re-prompt; usually one-shot.
- **Build fails on a real bug in generated code**: re-prompt Claude Code
  with the failing log lines; fixable.
- **Build succeeds but smoke-test 502s**: `expose.port` mismatch or app
  doesn't bind 0.0.0.0. Read the runtime log via the WebSocket endpoint.
- **Three consecutive build failures**: stop and tell the user. Don't burn
  the whole session on one bad scaffold.

## Cost note

Each invocation of Claude Code is non-trivial (multi-turn sub-agent with
tools). The Enterprise seat absorbs the per-token cost, but rate limits
are real. **One invocation per scaffold + one per iteration is fine; loops
are not.** If you find yourself re-prompting more than twice, surface the
error to the user instead of grinding.
