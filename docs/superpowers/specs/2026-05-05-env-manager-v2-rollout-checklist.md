# env-manager v2 — rollout checklist

## Plan 1 — Legacy backend cleanup

After rollout:
- [ ] `data/projects/.migrated` still present (migration was run by v1 — fine to leave)
- [ ] `GET /api/v1/health` returns 200
- [ ] `GET /api/v1/projects` returns array
- [ ] `GET /api/v1/repos` returns 404 or SPA HTML (legacy endpoint gone)
- [ ] `POST /api/v1/webhook/github` still routes (push-to-deploy still works)
- [ ] env-manager container starts without errors in logs ("Starting server")
- [ ] `data/repos/blocksweb-dasboard-laravel` still present and usable
- [ ] stripe-payments builds still trigger correctly via `POST /api/v1/envs/{id}/build`
- [ ] No reference to deleted types in any kept Go file (`go vet ./...` clean)

## Plan 2 — IaC v2 parser

After merge:
- [ ] `cd backend && go test ./internal/iac/...` returns all PASS
- [ ] `cd backend && go test ./...` still all PASS (no regression in other packages)
- [ ] No new direct callers of `iac.Parse` outside the test file (this is a library plan; wiring is Plans 3-5)
- [ ] `internal/projects/devconfig.go` still exists and is still the active config parser (will be removed when its last caller migrates)

## Plan 3 — Service plane (Postgres + Redis)
*(populated when plan 3 is written)*

## Plan 4 — Pre/post-deploy hooks
*(populated when plan 4 is written)*

## Plan 5 — Custom-domain + Let's Encrypt
*(populated when plan 5 is written)*

## Plan 6 — envm CLI
*(populated when plan 6 is written)*

## Plan 7 — UI rebuild
*(populated when plan 7 is written)*

## Plan 8 — Migration runbook
*(populated when plan 8 is written)*
