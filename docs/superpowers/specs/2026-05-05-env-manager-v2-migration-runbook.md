# env-manager v1 → v2 migration runbook

**For:** the home-lab operator running env-manager on `192.168.1.116`.
**Estimated downtime:** ~30s during Phase 3 (env-manager redeploy only; user containers stay up via Plan 4's split build/up).
**Prereqs:** all v2 PRs (Plans 1-8) merged + redeployed.

This runbook covers Phases 0-5, 7-8 of the v2 cutover. Phase 1 (the code refactor) is the eight merged PRs (Plans 1, 2, 3a, 3b, 4, 5, 6a, 6b, 7, 8 — yes, 6 was split into 6a/6b).

## Status note (2026-05-06)

Phases 1-5 were executed by the autonomous runbook session on 2026-05-06; Phase 0 was deliberately skipped (Docker stop = full home-lab outage; the per-phase rollback paths are sufficient). Phase 6 (env-manager's own public hostname `manager.blocksweb.nl`) was **dropped** by operator decision — env-manager stays LAN-only. Phase 7 (Traefik LE flags) was **deferred** because TLS for stripe-payments is currently terminated by Cloudflare orange-cloud (proxied), not at Traefik. If you switch to Cloudflare grey-cloud later you'll need Phase 7. Phase 4's stripe-payments migration was minimal (cosmetic schema cleanup; mariadb + redis sidecars retained); a future iteration can consolidate onto paas-postgres after a `mysqldump → psql` data migration.

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

**Two paths exist:**
- **Path A (minimal, executed on 2026-05-06):** drop the v1-only `public_branches` field from `.dev/config.yaml` so iac.Parse succeeds. Keep mariadb + redis sidecars. Keep Cloudflare TLS termination. Behaviour identical to v1; runner activates the v2 path with empty domains/services/hooks. **Risk: low.**
- **Path B (full):** consolidate onto paas-postgres + paas-redis, declare custom domains, add hooks. **Risk: high — requires `mysqldump → psql` data migration, Laravel `config/database.php` pgsql review, schema dialect testing.** Operator should execute on a dev environment first.

### Path A — minimal (DONE 2026-05-06)

The host clone at `/data/compose/16/data/repos/blocksweb-dasboard-laravel/` has been updated and committed locally:
```
fae7ff7c feat(.dev): migrate config.yaml to env-manager v2 schema (drop public_branches)
```
Both `main` and `develop` envs were rebuilt; Traefik routers now use the v2 `<env_id>-home` shape (`91497099a7a1c68c--main-home@docker`, `91497099a7a1c68c--develop-home@docker`). Verified: `stripe-payments.home` → 200, `develop.stripe-payments.home` → 200.

**Followup:** the host clone has 5 unpushed commits (4 pre-existing operator fixes + the v2 schema commit). When ready, push to GitHub origin/main from the host (using the PAT in cred-store) so source-of-truth aligns.

### Path B — full (DEFERRED, optional)

If you later want to migrate to paas-postgres / paas-redis:

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

## Phase 6 — DROPPED (env-manager stays LAN-only)

env-manager admin API is not exposed publicly. Operator decision (2026-05-06). Per-project GitHub webhooks for stripe-payments etc. are handled by manual build triggers via `envm builds trigger <project>/<env>` from the LAN, OR by configuring env-updater to forward `/api/v1/webhook/*` paths through `callback.blocksweb.nl/env-manager/...` (not yet done).

If you later decide to expose env-manager publicly, the original Phase 6 procedure (Cloudflare DNS A record + port 443 forward + Traefik labels with LE resolver) is preserved in git history of this file.

## Phase 7 — Traefik Let's Encrypt resolver flags (DEFERRED, optional)

Currently TLS for `blocksweb.nl` and `www.blocksweb.nl` is terminated by Cloudflare (orange-cloud / proxied), not at env-traefik. The origin serves plain HTTP. With this setup, no Let's Encrypt config on env-traefik is needed.

If you switch any public domain to Cloudflare DNS-only (grey cloud) — e.g., for a non-HTTP service or to bypass Cloudflare — you'll need to add LE flags to env-traefik so the origin can terminate TLS itself. Procedure:

1. Find how env-traefik was launched. As of 2026-05-06 it's NOT compose-managed (orphan stack — current invocation is the `docker run` printed by `docker inspect env-traefik`). Either:
   - Reproduce its current `docker run` invocation in a real `docker-compose.yaml` first
   - OR re-issue `docker run` with the extra args directly

2. Add command flags:
   ```
   --entrypoints.websecure.address=:443
   --certificatesresolvers.letsencrypt.acme.email=ops@blocksweb.nl
   --certificatesresolvers.letsencrypt.acme.storage=/data/acme.json
   --certificatesresolvers.letsencrypt.acme.tlschallenge=true
   ```

3. Add port mapping `-p 443:443` and a persistent volume `-v traefik_acme:/data`.

4. Open port 443 on the KPN modem + ISP router pointing at `192.168.1.6`.

5. Recreate the container. Watch `docker logs env-traefik` for "Server stopped"/"Server started". Cert acquisition for declared `domains.prod` takes ~30-60s per domain.

Until then, env-manager v2's Plan 5 label generator emits HTTPS routers referencing the `letsencrypt` resolver if a project declares `domains.prod` — Traefik will log warnings about the missing resolver but the `.home` HTTP routers (which is what every project actually uses today) work fine.

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
| 6 | n/a — phase dropped | n/a |
| 7 | env-traefik doesn't restart | `docker run` the original invocation (logged via `docker inspect env-traefik` before the change) |
