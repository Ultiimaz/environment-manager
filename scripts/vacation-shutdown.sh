#!/usr/bin/env bash
# Graceful shutdown for vacation/long-off periods.
#
# Stops all running containers in two phases — apps first, then databases
# with a longer grace window so Postgres/MariaDB flush WAL cleanly. Without
# this, yanking power risks WAL-replay-on-boot which usually works but
# occasionally needs manual intervention you can't do from a beach.
#
# Usage:
#   sudo bash /opt/scripts/vacation-shutdown.sh         # stop containers, leave host running
#   sudo bash /opt/scripts/vacation-shutdown.sh --off   # stop containers, then poweroff
#
# Idempotent — re-runnable. Skips containers that are already stopped.

set -uo pipefail

POWEROFF=false
[[ "${1:-}" == "--off" ]] && POWEROFF=true

# Phase 1: app containers (parallel, 30s grace each)
APPS=(
  hermes-manager hermes-engineer hermes-scout hermes hermes-dashboard
  env-manager env-traefik env-coredns env-updater env-callback socket-proxy
  4b6dd11b4149317c--main-web-1
  91497099a7a1c68c--main-app-1 91497099a7a1c68c--main-worker-1
  91497099a7a1c68c--develop-app-1 91497099a7a1c68c--develop-worker-1
  ollama open-webui searxng searxng-redis
  minecraft2-mc-1
)

# Phase 2: stateful services (sequential, 60s grace — allow WAL flush)
DATABASES=(
  paas-postgres
  91497099a7a1c68c--main-mysql-1 91497099a7a1c68c--develop-mysql-1
  paas-redis
  91497099a7a1c68c--main-redis-1 91497099a7a1c68c--develop-redis-1
)

echo "==> Phase 1: stopping ${#APPS[@]} app containers (parallel)"
printf '%s\n' "${APPS[@]}" \
  | xargs -P 8 -I {} sh -c 'docker stop -t 30 "{}" >/dev/null 2>&1 && echo "  stopped: {}" || echo "  skipped: {} (already down or missing)"'

echo
echo "==> Phase 2: stopping ${#DATABASES[@]} stateful services (sequential, 60s grace)"
for db in "${DATABASES[@]}"; do
  if docker stop -t 60 "$db" >/dev/null 2>&1; then
    echo "  stopped: $db"
  else
    echo "  skipped: $db (already down or missing)"
  fi
done

echo
echo "==> Phase 3: any leftover running containers"
LEFT=$(docker ps -q)
if [[ -n "$LEFT" ]]; then
  echo "  found leftovers, stopping with 30s grace..."
  docker stop -t 30 $LEFT >/dev/null
fi

echo
echo "==> Phase 4: portainer (last so you keep UI access until end)"
docker stop -t 10 portainer portainer_agent >/dev/null 2>&1 || true

echo
echo "==> Final state:"
docker ps --format '  RUNNING: {{.Names}}' | head
echo "  total still running: $(docker ps -q | wc -l)"

echo
sync
echo "==> sync complete — disk caches flushed"

if $POWEROFF; then
  echo
  echo "==> --off flag set — powering off in 10 seconds (Ctrl+C to cancel)"
  sleep 10
  poweroff
else
  echo
  echo "Containers stopped. To power off the host now, run:  sudo poweroff"
fi
