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
*(populated when step 3 plan is written)*

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
