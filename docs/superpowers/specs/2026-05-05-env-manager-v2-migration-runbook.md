# env-manager v1 → v2 migration runbook

**For:** the home-lab operator running env-manager on `192.168.1.116`.
**Estimated downtime:** ~5 minutes during Phase 3.
**Prereqs:** all v2 PRs (Plans 1-7) merged + redeployed; redeploy script updated to set `LETSENCRYPT_EMAIL`.

This runbook covers Phases 2-5 of the v2 cutover. Phase 1 (the code refactor) is the eight merged PRs.

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

In the stripe-payments repo on your laptop:

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

## Phase 6 — env-manager's own public hostname (deferred from Plan 6)

To receive GitHub webhooks externally, env-manager needs its own public hostname. The current setup runs on LAN `192.168.1.6:8080`.

1. In Cloudflare DNS for `blocksweb.nl`: add A record `manager → 84.84.207.234` (your home-lab public IP), proxy off (grey cloud) so Let's Encrypt's TLS-ALPN works.

2. Open port 443 on the KPN modem + ISP router pointing at `192.168.1.6` (env-traefik).

3. Add Traefik labels to env-manager's container. Edit `/data/compose/16/docker-compose.yaml` (or wherever env-manager's compose lives):

```yaml
services:
  env-manager:
    # ... existing config ...
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.env-manager.rule=Host(\`manager.blocksweb.nl\`)"
      - "traefik.http.routers.env-manager.entrypoints=websecure"
      - "traefik.http.routers.env-manager.tls=true"
      - "traefik.http.routers.env-manager.tls.certresolver=letsencrypt"
      - "traefik.http.services.env-manager.loadbalancer.server.port=8080"
      - "traefik.docker.network=my-macvlan-net"
    networks:
      - paas-net
      - my-macvlan-net
```

4. `docker compose up -d` env-manager. Wait ~30s for cert issuance.

5. Update each GitHub repo's webhook URL to `https://manager.blocksweb.nl/api/v1/webhook/github` (was `http://192.168.1.6:8080/...` if exposed).

6. Update your local `~/.envm/config.yaml`:

```yaml
endpoint: https://manager.blocksweb.nl
token: envm_...
```

## Phase 7 — Traefik Let's Encrypt resolver flags (deferred from Plan 5)

Edit env-traefik's compose command flags:

```yaml
services:
  env-traefik:
    command:
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.network=my-macvlan-net"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.email=ops@blocksweb.nl"
      - "--certificatesresolvers.letsencrypt.acme.storage=/data/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.tlschallenge=true"
    volumes:
      - traefik_acme:/data
```

Then `docker compose up -d` env-traefik. Watch its logs for "Server stopped"/"Server started" cycle. Cert acquisition for already-pushed v2 stripe-payments takes ~30-60s per domain.

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
| 6 | Cert not issued for manager.blocksweb.nl | Verify port 443 forwarded, Cloudflare proxy is grey cloud, Phase 7 done |
| 7 | env-traefik doesn't restart | `docker logs env-traefik` for syntax errors in compose flags |
