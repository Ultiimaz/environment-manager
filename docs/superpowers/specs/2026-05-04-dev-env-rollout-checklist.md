# Rollout checklist — `.dev/` preview-deploy system

For each rollout step (per the spec's "Rollout sequence" section), record manual verifications below.

## Step 1 — schema + migration

After rollout:
- [ ] `data/projects/.migrated` exists and contains `v1`
- [ ] One project directory exists per pre-existing ComposeProject
- [ ] Each project directory has `project.yaml` + `environments/legacy.yaml`
- [ ] Server still serves all existing endpoints (no regression)
- [ ] Re-running the binary does not duplicate projects (idempotency check)

## Step 2 — `.dev/` parser + Project creation API

After rollout:
- [ ] `POST /api/v1/projects` with a valid `.dev/` repo returns 201 + project + env
- [ ] Project shows `default_branch` resolved from origin/HEAD
- [ ] Environment is created at `Status: pending` (no build yet — step 3)
- [ ] `GET /api/v1/projects` lists the new project
- [ ] `GET /api/v1/projects/{id}` returns project + environments
- [ ] Re-POSTing the same repo URL returns 409
- [ ] POSTing a repo without `.dev/` returns 400
- [ ] Legacy `/api/v1/repos` and `/api/v1/compose` still work unchanged

## Step 3 — Builder + log streaming

After rollout:
- [ ] `POST /api/v1/envs/{id}/build` returns 202 with `data.build_id`
- [ ] Build runs asynchronously: container appears under `docker ps` shortly after for a successful build
- [ ] WS `/ws/envs/{id}/build-logs` streams output during build, closes on completion
- [ ] Build success flips env to `Status: running`, sets `LastBuildID` and `LastDeployedSHA`
- [ ] Build failure flips env to `Status: failed` (prior containers, if any, untouched)
- [ ] Restart env-manager mid-build: stuck `running` builds reconciled to `failed` (check log for "Marked stuck builds as failed")
- [ ] No regression: legacy `/api/v1/repos` and `/api/v1/compose` still work
- [ ] Routable URL is **not** yet wired (Traefik labels deferred to step 4); container is running on its bridge but not externally accessible

## Step 4 — DB provisioning
*(populated when step 4 plan is written)*

## Step 5 — Webhook v2
*(populated when step 5 plan is written)*

## Step 6 — Branch-delete handler
*(populated when step 6 plan is written)*

## Step 7 — Reconcile-on-startup
*(populated when step 7 plan is written)*

## Step 8 — New Projects UI
*(populated when step 8 plan is written)*

## Step 9 — Flip default UI
*(populated when step 9 plan is written)*

## Step 10 — Remove old UI + handlers
*(populated when step 10 plan is written)*
