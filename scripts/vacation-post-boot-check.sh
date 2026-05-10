#!/usr/bin/env bash
# Post-boot health check — run after powering the server back on.
#
# Verifies all expected containers are up, DNS resolves, and the major
# HTTP endpoints respond. Exits non-zero with a summary if anything is off.
#
# Usage:
#   bash /opt/scripts/vacation-post-boot-check.sh
#
# (No sudo needed for read-only docker queries if you're in the docker group;
# this host's user is not, so call with sudo:)
#   sudo bash /opt/scripts/vacation-post-boot-check.sh

set -uo pipefail

EXPECTED=(
  env-manager env-traefik env-coredns env-updater env-callback socket-proxy
  paas-postgres paas-redis
  hermes hermes-manager hermes-engineer hermes-scout hermes-dashboard
  portainer portainer_agent
  ollama open-webui searxng searxng-redis
  4b6dd11b4149317c--main-web-1
  91497099a7a1c68c--main-app-1 91497099a7a1c68c--main-worker-1
  91497099a7a1c68c--develop-app-1 91497099a7a1c68c--develop-worker-1
  91497099a7a1c68c--main-mysql-1 91497099a7a1c68c--develop-mysql-1
  91497099a7a1c68c--main-redis-1 91497099a7a1c68c--develop-redis-1
  minecraft2-mc-1
)

fail=0

echo "=== Container health ($(date)) ==="
for c in "${EXPECTED[@]}"; do
  status=$(docker inspect -f '{{.State.Status}}' "$c" 2>/dev/null || echo "MISSING")
  health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}-{{end}}' "$c" 2>/dev/null || echo "-")
  if [[ "$status" != "running" ]]; then
    printf "  FAIL  %-42s status=%s\n" "$c" "$status"
    fail=$((fail+1))
  elif [[ "$health" == "unhealthy" ]]; then
    printf "  WARN  %-42s status=%s health=%s\n" "$c" "$status" "$health"
    fail=$((fail+1))
  else
    printf "  OK    %-42s status=%s health=%s\n" "$c" "$status" "$health"
  fi
done

echo
echo "=== DNS resolution (via env-coredns at 192.168.1.4) ==="
for h in manager.home traefik.home kanban.home; do
  if dig +short +time=2 +tries=1 @192.168.1.4 "$h" 2>/dev/null | grep -qE '[0-9]'; then
    echo "  OK    $h"
  else
    echo "  FAIL  $h does not resolve via env-coredns"
    fail=$((fail+1))
  fi
done

echo
echo "=== HTTP probes ==="
probe() {
  local label="$1" url="$2"
  if curl -sSf -m 5 -o /dev/null "$url"; then
    echo "  OK    $label  ($url)"
  else
    echo "  FAIL  $label  ($url)"
    fail=$((fail+1))
  fi
}
probe "env-manager"        "http://192.168.1.7:8080/"
probe "traefik dashboard"  "http://192.168.1.6:8080/api/overview"
probe "portainer"          "http://192.168.1.116:9000/"

echo
echo "=== Recent docker daemon errors (last 50 lines, filtered) ==="
journalctl -u docker --since "10 min ago" --no-pager 2>/dev/null \
  | grep -iE 'error|warn|fail' | tail -10 \
  || echo "  (journalctl unavailable or no recent errors)"

echo
if [[ $fail -eq 0 ]]; then
  echo "==> ALL CHECKS PASSED"
  exit 0
else
  echo "==> $fail check(s) FAILED — investigate above"
  echo
  echo "Common fixes:"
  echo "  - container down:    sudo docker start <name>"
  echo "  - manager.home dns:  sudo docker logs env-coredns --tail 30"
  echo "  - http probe fail:   sudo docker logs <container> --tail 50"
  exit 1
fi
