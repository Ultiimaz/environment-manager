#!/usr/bin/env bash
# Adds /etc/hosts entries for *.home to every hermes agent container so
# they can reach kanban.home / manager.home / traefik.home without going
# through Docker's embedded DNS (which forwards through the host and
# fails because the host can't reach 192.168.1.4 due to macvlan
# isolation).
#
# Idempotent — skips containers that already have the entry.

set -uo pipefail

ENTRY='192.168.1.6 kanban.home manager.home traefik.home'
AGENTS=(hermes hermes-manager hermes-engineer hermes-scout)

for c in "${AGENTS[@]}"; do
  echo "=== $c ==="
  if ! docker ps -q -f name="^${c}$" | grep -q .; then
    echo "  not running, skip"
    continue
  fi
  if docker exec "$c" grep -q kanban.home /etc/hosts 2>/dev/null; then
    echo "  hosts already patched, skip"
  else
    # -u 0 forces root for the write — hermes-voice:local runs as uid 10000
    # and can't write /etc/hosts otherwise.
    docker exec -u 0 "$c" sh -c "echo '$ENTRY' >> /etc/hosts"
    echo "  hosts entry added"
  fi
  result=$(docker exec "$c" python3 -c "
import urllib.request, json
try:
    r = urllib.request.urlopen('http://kanban.home/api/v1/tickets', timeout=5)
    data = json.loads(r.read())
    print(f'OK {len(data)} tickets visible')
except Exception as e:
    print(f'FAIL {e}')
" 2>&1 | tail -1)
  echo "  $result"
done
