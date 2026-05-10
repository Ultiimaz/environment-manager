#!/usr/bin/env bash
# Snapshot critical state before a long off-period.
#
# Bundles:
#   - Postgres dumpall (paas-postgres — kanban + env-manager + others)
#   - MariaDB dumps (PaaS app DBs)
#   - Hermes per-agent /data dirs (configs, skills, secrets, transcripts)
#   - env-updater webhook secrets and hooks.yaml
#   - /data/compose (Portainer-managed compose definitions)
#   - /opt/scripts (this set of vacation scripts)
#
# Output: /data/backup-vacation-YYYYMMDD-HHMM.tar.gz
#
# Sized at runtime — typical bundle is a few GB. Database dumps add ~5-10s,
# tar adds ~30-60s.
#
# Usage:
#   sudo bash /opt/scripts/vacation-backup.sh

set -euo pipefail

STAMP=$(date +%Y%m%d-%H%M)
WORK=/tmp/vac-backup-$STAMP
OUT=/data/backup-vacation-$STAMP.tar.gz

mkdir -p "$WORK"
trap "rm -rf $WORK" EXIT

echo "==> [1/4] dumping paas-postgres"
docker exec paas-postgres pg_dumpall -U postgres > "$WORK/paas-postgres.sql" \
  || echo "  (pg_dumpall failed — check user)"

echo "==> [2/4] dumping MariaDB instances"
for mc in 91497099a7a1c68c--main-mysql-1 91497099a7a1c68c--develop-mysql-1; do
  if docker ps -q -f name="$mc" | grep -q .; then
    # MariaDB images use MARIADB_ROOT_PASSWORD; older MySQL used MYSQL_ROOT_PASSWORD.
    # Pull whichever is set inside the container.
    if docker exec "$mc" sh -c 'mariadb-dump -u root -p"${MARIADB_ROOT_PASSWORD:-${MYSQL_ROOT_PASSWORD:-root}}" --all-databases 2>/dev/null' > "$WORK/$mc.sql" \
       && [[ -s "$WORK/$mc.sql" ]]; then
      echo "  dumped: $mc ($(wc -c < "$WORK/$mc.sql") bytes)"
    else
      rm -f "$WORK/$mc.sql"
      echo "  (dump skipped: $mc — root pw not accessible)"
    fi
  fi
done

echo "==> [3/4] tarring state directories"
tar czf "$OUT" \
  -C /tmp "vac-backup-$STAMP" \
  --transform "s|^vac-backup-$STAMP|db-dumps|" \
  /data/hermes-secrets \
  /data/hermes-manager \
  /data/hermes-engineer \
  /data/hermes-scout \
  /data/hermes \
  /data/compose \
  /opt/env-updater/secrets \
  /opt/env-updater/hooks.yaml \
  /opt/scripts \
  2>/dev/null || true

echo "==> [4/4] summary"
ls -lh "$OUT"
echo
echo "Backup at: $OUT"
echo "Restore note: untar to / then 'docker exec paas-postgres psql -U postgres -f /restore/paas-postgres.sql'"
