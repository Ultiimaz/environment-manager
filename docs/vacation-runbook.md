# Home-lab vacation runbook

Procedure for safely shutting down the Blocksweb home-lab host (192.168.1.116) for an extended off-period and bringing it back without manual intervention.

## Before leaving

```bash
# SSH in
ssh ultiimaz@192.168.1.116

# 1. Snapshot state to a single tarball (lives at /data/backup-vacation-YYYYMMDD-HHMM.tar.gz)
sudo bash /opt/scripts/vacation-backup.sh

# 2. Stop containers gracefully + power off in one shot
sudo bash /opt/scripts/vacation-shutdown.sh --off
```

Or split it: run shutdown without `--off`, verify `docker ps` shows nothing, then `sudo poweroff`.

## What survives the off-period

- **Restart policies** — every container has `unless-stopped` or `always`, so they all come back up automatically when Docker daemon starts on boot.
- **Image-managed patches** (Hermes voice patches, pip-installed deps in `/opt/hermes/.venv` inside the container) survive `stop` + `start`. They live in the container's writable layer, not the image, so as long as nothing runs `docker rm` on those containers they're fine.
- **Bind-mounts** (`/data/hermes-*`, `/data/compose`, `/opt/env-updater`) are on `/dev/sda2` and persist trivially.
- **Named volumes** (postgres data, ollama models, etc.) are at `/var/lib/docker/volumes/` — also on `/dev/sda2`.
- **Locally-built images** (`hermes:local`, `environment-manager:local`, `kanban-home:latest`, the PaaS app/worker images) live in `/var/lib/docker/overlay2/` — persist on disk. There is no registry to re-pull them from, so do not delete `/var/lib/docker/`.

## Coming back: cold-start procedure

```bash
# 1. Push power button. BIOS may need to be set to "after AC loss = power on"
#    if you want auto-recovery, otherwise manual press is fine.

# 2. SSH in (give it ~60s after boot for Docker daemon + container restart cascade)
ssh ultiimaz@192.168.1.116

# 3. Health check
sudo bash /opt/scripts/vacation-post-boot-check.sh

# 4. If anything fails, the script tells you what. Most common:
#    - container exited 1: sudo docker logs <name> --tail 50
#    - DNS missing:        sudo docker restart env-coredns
#    - http 502:           wait 30s for backend to finish starting, retry
```

If something is genuinely broken (a container's writable layer corrupted, image missing, etc.):

```bash
# Rehydrate Hermes voice patches into a fresh container
sudo bash /opt/scripts/setup-voice-manager.sh

# Or restore from the backup:
cd /
sudo tar xzf /data/backup-vacation-YYYYMMDD-HHMM.tar.gz
sudo docker exec -i paas-postgres psql -U postgres < /db-dumps/paas-postgres.sql
```

## Risk register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| WAL corruption from ungraceful shutdown | Low if `vacation-shutdown.sh` runs; medium if power yanked | Postgres won't start, kanban + env-manager DB-blocked | Use the shutdown script. WAL replay handles 99% of cases anyway. |
| Disk corruption from filesystem damage | Very low | Catastrophic | Backup tarball at `/data/backup-vacation-*.tar.gz`. Restore externally if needed. |
| Public IP rotation while away | Medium (KPN residential) | Cloudflare A record points wrong → `callback.blocksweb.nl` 502s, GitHub webhooks fail | Update Cloudflare DNS A record on return. Internal LAN unaffected. |
| Router/modem reboot causing static lease loss | Low | macvlan IPs may shift; fixed in compose so should re-take | All `*.home` traffic via 192.168.1.4 (CoreDNS) which has restart policy. |
| Local image deleted by accident | Low | Need rebuild — `hermes:local` (5GB) is the painful one | Tarball includes compose + scripts; can rebuild from `/opt/src/environment-manager` and Hermes repo. |
| Auto-update reboot during off | Zero (server is off) | n/a | n/a |

## What auto-recovers vs needs you

**Auto:** containers start, Discord bots reconnect, Traefik reloads routes, CoreDNS serves `*.home`, GitHub webhook resumes accepting pushes.

**Needs you:** verifying `manager.home` resolves and the env-manager UI loads. If Cloudflare A record is stale, updating it. Re-running `setup-voice-manager.sh` only if hermes-manager was recreated.

## Files

- `/opt/scripts/vacation-shutdown.sh` — graceful stop + optional poweroff
- `/opt/scripts/vacation-post-boot-check.sh` — health verification
- `/opt/scripts/vacation-backup.sh` — full state snapshot
- `/opt/scripts/setup-voice-manager.sh` — rehydrate hermes-manager voice patches if container is recreated
- `/data/backup-vacation-*.tar.gz` — most recent snapshot

All scripts are idempotent.
