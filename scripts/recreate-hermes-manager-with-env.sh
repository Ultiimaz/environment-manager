#!/usr/bin/env bash
# Recreate hermes-manager from hermes-voice:local using /data/hermes-secrets/
# manager.env as the canonical env source. Used when adding new env vars
# (e.g. GROQ_API_KEY) that the running container can't pick up at runtime.
#
# Safe-recreate: renames the old container as -backup-recreate before
# removing, smoke-tests the new one, auto-rolls-back on failure.
#
# Usage: sudo bash /opt/scripts/recreate-hermes-manager-with-env.sh

set -uo pipefail

OLD=hermes-manager
BACKUP=hermes-manager-backup-recreate
IMAGE=hermes-voice:local
ENV_FILE=/data/hermes-secrets/manager.env
NETWORK=my-macvlan-net
IP=192.168.1.14
BIND=/data/hermes-manager:/opt/data
HOSTNAME=hermes-manager

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: $ENV_FILE not found" >&2
  exit 1
fi

if docker ps -a --format '{{.Names}}' | grep -q "^${BACKUP}$"; then
  echo "ERROR: $BACKUP already exists — clean it up first:" >&2
  echo "  docker rm $BACKUP" >&2
  exit 1
fi

echo "==> Stopping current container, renaming to $BACKUP"
docker stop -t 15 "$OLD"
docker rename "$OLD" "$BACKUP"

echo "==> Creating new container with $ENV_FILE as env source"
docker run -d \
  --name "$OLD" \
  --restart unless-stopped \
  --hostname "$HOSTNAME" \
  --network "$NETWORK" --ip "$IP" \
  -v "$BIND" \
  --env-file "$ENV_FILE" \
  "$IMAGE" gateway run

echo "==> Waiting 12s for startup"
sleep 12

echo "==> Smoke test: discord bot connected"
if docker exec "$OLD" tail -30 /opt/data/logs/gateway.log 2>&1 | grep -q "✓ discord connected"; then
  echo "  Discord connected OK"
else
  echo "  FAIL — rolling back"
  docker stop "$OLD" >/dev/null 2>&1
  docker rm "$OLD" >/dev/null 2>&1
  docker rename "$BACKUP" "$OLD"
  docker start "$OLD"
  echo "  Rolled back. Logs from failed attempt:"
  docker logs "$OLD" --tail 20
  exit 1
fi

echo "==> Smoke test: GROQ_API_KEY present in container env"
if docker exec "$OLD" sh -c 'test -n "$GROQ_API_KEY" && echo "set"' 2>/dev/null | grep -q set; then
  echo "  GROQ_API_KEY set"
else
  echo "  WARNING — GROQ_API_KEY not in container env. STT will fall back."
fi

echo
echo "==> Done. Verify voice + STT works, then clean up backup:"
echo "  sudo docker rm $BACKUP"
