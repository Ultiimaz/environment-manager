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

## Plan 3a — Service plane bootstrap (Postgres + Redis singletons)

After merge + redeploy:
- [ ] env-manager startup logs show `Service-plane: paas-postgres ready` AND `Service-plane: paas-redis ready`
- [ ] `docker network inspect paas-net` shows the bridge network exists
- [ ] `docker ps --filter name=paas-postgres` shows the container running on `paas-net`, image `postgres:16`, with volume `paas_postgres_data` mounted
- [ ] `docker ps --filter name=paas-redis` shows the container running on `paas-net`, image `redis:7`, with volume `paas_redis_data` mounted
- [ ] `docker exec paas-postgres pg_isready -U postgres` exits 0
- [ ] `docker exec paas-redis redis-cli -a $(superuser_pw_from_store) ping` returns `PONG`
- [ ] `cat /data/compose/16/data/.credentials/store.json | python3 -m json.tool` shows `system_secrets` with keys `system:paas-postgres:superuser` and `system:paas-redis:superuser` (encrypted blobs)
- [ ] Restarting env-manager (`docker restart env-manager`) is a no-op — logs show "ready" without recreating containers; superuser passwords reused from cred-store
- [ ] `cd backend && go test ./...` passes locally
- [ ] No regression: stripe-payments builds still trigger via webhook; existing envs still serve

## Plan 3b — Per-env DB/ACL provisioning + URL injection

After merge + redeploy:
- [ ] Existing stripe-payments builds (still on v1 schema) trigger normally; runner logs `==> no .dev/config.yaml — skipping service provisioning` (or similar) — confirms graceful fallback
- [ ] `cd backend && go test ./internal/builder/... -v` passes locally with the four new TestRunner_Build_Services* and three TestRunner_Teardown_*Services tests
- [ ] Manual: create a test fixture project with `.dev/config.yaml` declaring `services.postgres: true`, push it, observe runner logs show `==> provisioning postgres database`
- [ ] After successful build: `docker exec paas-postgres psql -U postgres -c "\l"` shows the new database
- [ ] After successful build: `docker exec paas-postgres psql -U postgres -c "\du"` shows the new user
- [ ] After successful build: app's `.env` contains `DATABASE_URL=postgres://...`
- [ ] After successful build: `docker inspect <env-id>-app | jq '.[0].NetworkSettings.Networks | keys'` shows `paas-net` attached
- [ ] Branch delete: `docker exec paas-postgres psql -U postgres -c "\l"` no longer lists the test database
- [ ] Branch delete: `cat /data/compose/16/data/.credentials/store.json | jq '.project_secrets | keys'` no longer includes the env-id of the deleted env
- [ ] Same battery for redis with `services.redis: true` (`ACL LIST` instead of `\l`/`\du`)

## Plan 4 — Pre/post-deploy hooks

After merge + redeploy:
- [ ] `cd backend && go test ./internal/hooks/... ./internal/builder/... -v` — all PASS, including 4 new TestRunner_Build_*Hook tests + 6 TestRunPre/TestRunPost tests
- [ ] Existing stripe-payments builds (still v1 schema) trigger normally — runner logs `==> no .dev/config.yaml — skipping service provisioning + hooks`, build still succeeds
- [ ] Manual: extend the test fixture project with `hooks.pre_deploy: ["echo from pre"]` in `.dev/config.yaml` — push, observe build log shows `==> pre_deploy[1/1]: echo from pre` BEFORE `==> docker compose up -d`
- [ ] Same project, change one pre-deploy hook to a deliberately-failing command (`exit 1`) — push, observe BUILD FAILED + the previous container still runs (`docker ps` confirms unchanged container ID)
- [ ] Add `hooks.post_deploy: ["echo from post"]`, push — build succeeds, log shows `==> post_deploy[1/1]: echo from post` AFTER `==> docker compose up -d`
- [ ] Make a post-deploy hook fail (`exit 1`) — build still marked success, log contains `WARNING: post_deploy hook 1 ("...") failed: ...`

## Plan 5 — Multi-domain Traefik labels with Let's Encrypt

After merge + redeploy:
- [ ] `cd backend && go test ./internal/builder/... -v` — all PASS, including new TestInjectTraefikLabels_V2_* and TestRunner_Build_V2Domains* tests
- [ ] env-manager redeploy script now sets `LETSENCRYPT_EMAIL=<your email>` alongside `CREDENTIAL_KEY`
- [ ] Existing stripe-payments builds (still v1 schema) trigger normally — runner emits the legacy single HTTP router on env.URL, no `-home`/`-public` routers
- [ ] Manual: extend the test fixture project's `.dev/config.yaml` with `domains.prod: ["mytestdomain.com"]`, push — observe rendered compose has `-home` + `-public` routers + redirect router + middleware
- [ ] If LETSENCRYPT_EMAIL is unset, build log shows `WARNING: domains declared but LETSENCRYPT_EMAIL is unset; public domains will serve HTTP only`
- [ ] Traefik command flags for `--certificatesresolvers.letsencrypt.*` are deferred to Plan 8 (manual host op); without them, certs won't actually issue — but the labels are emitted correctly so Plan 8's host-side flags will start issuing certs immediately
- [ ] Cross-project domain conflict detection deferred to a future plan
- [ ] env-manager's own public hostname (manager.blocksweb.nl) deferred to Plan 6

## Plan 6a — Admin auth + envm CLI (secrets commands)

After merge + redeploy:
- [ ] `cd backend && go test ./...` — full suite green
- [ ] env-manager startup log shows `==> env-manager admin token (save it now): envm_<64hex>` once on first boot only
- [ ] Subsequent boots reuse the stored token without logging it
- [ ] `curl -X POST -H "Authorization: Bearer wrong" https://<env-manager>/api/v1/projects` → 401
- [ ] `curl -X POST -H "Authorization: Bearer <correct>" -H "Content-Type: application/json" --data '{"repo_url":"..."}' https://<env-manager>/api/v1/projects` → 201
- [ ] GET endpoints (e.g. `/projects`) still work without Authorization
- [ ] Webhook (`POST /webhook/github` with valid HMAC) still works without Bearer
- [ ] `go install github.com/environment-manager/backend/cmd/envm@<branch>` produces a binary named `envm`
- [ ] `envm version` prints `envm <version>`
- [ ] `~/.envm/config.yaml` populated with endpoint + token; `envm config show` displays endpoint + masked token
- [ ] `envm secrets list <project>` returns keys for a known project
- [ ] `envm secrets set <project> KEY=value` succeeds and `envm secrets list` shows the new key
- [ ] `envm secrets get <project> KEY --reveal` prints the value
- [ ] `envm secrets get <project> KEY` (no `--reveal`) errors with a clear message
- [ ] `envm secrets delete <project> KEY` removes the key
- [ ] `envm secrets import <project> .env-fixture` bulk-imports
- [ ] env-manager Docker image now ships `/usr/local/bin/envm` — `docker exec env-manager envm version` works

## Plan 6b — Project/build/env/services CLI commands
*(populated when Plan 6b is written)*

## Plan 7 — UI rebuild
*(populated when plan 7 is written)*

## Plan 8 — Migration runbook
*(populated when plan 8 is written)*
