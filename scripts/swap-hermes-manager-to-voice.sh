#!/usr/bin/env bash
# Swap hermes-manager from hermes:local to hermes-voice:local.
#
# Safe-swap: renames the existing container as hermes-manager-backup,
# creates a new one from hermes-voice:local with the same config, and
# only after verifying voice deps importable does it delete the backup.
#
# Rollback if anything goes wrong:
#   docker stop hermes-manager 2>/dev/null
#   docker rm hermes-manager 2>/dev/null
#   docker rename hermes-manager-backup hermes-manager
#   docker start hermes-manager
#
# Usage: sudo bash /opt/scripts/swap-hermes-manager-to-voice.sh

set -uo pipefail

OLD=hermes-manager
BACKUP=hermes-manager-backup
NEW_IMAGE=hermes-voice:local

if ! docker image inspect "$NEW_IMAGE" >/dev/null 2>&1; then
  echo "ERROR: $NEW_IMAGE not found. Build it first:" >&2
  echo "  docker build -t $NEW_IMAGE /opt/src/hermes-voice" >&2
  exit 1
fi

if docker ps -a --format '{{.Names}}' | grep -q "^${BACKUP}$"; then
  echo "ERROR: $BACKUP already exists — previous swap not cleaned up." >&2
  echo "Inspect/delete it manually before re-running:" >&2
  echo "  docker rm $BACKUP" >&2
  exit 1
fi

echo "==> Extracting current hermes-manager config"
# Save env, binds, network info, and the original run command.
ENV_FILE=/tmp/hermes-manager.env
docker inspect "$OLD" --format '{{range .Config.Env}}{{println .}}{{end}}' > "$ENV_FILE"

BINDS=$(docker inspect "$OLD" --format '{{range .HostConfig.Binds}}-v {{.}} {{end}}')
NETWORK=my-macvlan-net
IP=$(docker inspect "$OLD" -f '{{(index .NetworkSettings.Networks "my-macvlan-net").IPAMConfig.IPv4Address}}')
HOSTNAME=$(docker inspect "$OLD" --format '{{.Config.Hostname}}')
CMD=$(docker inspect "$OLD" --format '{{json .Config.Cmd}}' | python3 -c 'import sys,json; print(" ".join(json.load(sys.stdin)))')

echo "  network=$NETWORK ip=$IP hostname=$HOSTNAME cmd=$CMD"

echo "==> Stopping and renaming current container to $BACKUP"
docker stop -t 15 "$OLD"
docker rename "$OLD" "$BACKUP"

echo "==> Creating new container from $NEW_IMAGE"
# --add-host bypasses Docker's embedded DNS for *.home — see comment in
# recreate-hermes-manager-with-env.sh for the macvlan isolation context.
docker run -d \
  --name "$OLD" \
  --restart unless-stopped \
  --hostname "$HOSTNAME" \
  --network "$NETWORK" --ip "$IP" \
  $BINDS \
  --env-file "$ENV_FILE" \
  --add-host kanban.home:192.168.1.6 \
  --add-host manager.home:192.168.1.6 \
  --add-host traefik.home:192.168.1.6 \
  "$NEW_IMAGE" $CMD

echo "==> Waiting 10s for startup"
sleep 10

echo "==> Smoke test: voice deps importable"
if docker exec "$OLD" /opt/hermes/.venv/bin/python -c \
     "import faster_whisper, sounddevice, edge_tts, nacl; print('voice deps ok')" 2>&1; then
  echo "  voice deps OK"
else
  echo "  FAIL — rolling back"
  docker stop "$OLD" >/dev/null 2>&1
  docker rm "$OLD" >/dev/null 2>&1
  docker rename "$BACKUP" "$OLD"
  docker start "$OLD"
  echo "  rolled back to $BACKUP -> $OLD"
  exit 1
fi

echo "==> Smoke test: discord.py patches present"
markers=$(docker exec "$OLD" grep -c blocksweb /opt/hermes/gateway/platforms/discord.py)
timeout=$(docker exec "$OLD" grep -E '^\s*VOICE_TIMEOUT\s*=' /opt/hermes/gateway/platforms/discord.py | head -1)
echo "  blocksweb markers: $markers (expect 3)"
echo "  $timeout (expect 1800)"

if [[ "$markers" != "3" ]]; then
  echo "  FAIL — markers wrong, rolling back"
  docker stop "$OLD" >/dev/null 2>&1
  docker rm "$OLD" >/dev/null 2>&1
  docker rename "$BACKUP" "$OLD"
  docker start "$OLD"
  exit 1
fi

echo "==> All smoke tests passed. Backup remains at $BACKUP."
echo "    Verify Discord bot is online and voice works, then delete the"
echo "    backup with:  sudo docker rm $BACKUP"
