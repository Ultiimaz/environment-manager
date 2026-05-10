#!/usr/bin/env bash
# Graceful shutdown for vacation/long-off periods.
#
# IMPORTANT — what this does NOT do, and why:
#   It does NOT call `docker stop` on containers. `docker stop` from the CLI
#   marks a container as "manually stopped" and `unless-stopped` containers
#   that have that flag set do NOT auto-restart when the Docker daemon
#   starts again. After running `docker stop` + reboot, you'd have to
#   manually `docker start` everything.
#
# What it DOES do:
#   Triggers systemd shutdown. systemd sends SIGTERM to docker.service →
#   the Docker daemon's own shutdown handler signals each container
#   (default 15s grace, controlled by daemon.json shutdown-timeout) →
#   containers exit gracefully → daemon records they were running →
#   on next boot, daemon auto-restarts them per their restart policy.
#
# Postgres / MariaDB / Redis all handle SIGTERM as a clean shutdown well
# within the default 15s window.
#
# Usage:
#   sudo bash /opt/scripts/vacation-shutdown.sh         # show state, no action
#   sudo bash /opt/scripts/vacation-shutdown.sh --off   # power off the host

set -uo pipefail

POWEROFF=false
[[ "${1:-}" == "--off" ]] && POWEROFF=true

echo "==> Current state:"
echo "  $(docker ps -q | wc -l) running containers"
echo "  $(systemctl is-active docker) docker daemon"

if $POWEROFF; then
  echo
  echo "==> Powering off in 5s via systemd (Ctrl+C to abort)"
  echo "    systemd → docker.service stop → daemon SIGTERMs all containers"
  echo "    (15s grace each) → host poweroff. On next boot all containers"
  echo "    with unless-stopped/always policies auto-restart."
  sleep 5
  sync
  systemctl poweroff
else
  echo
  echo "  Dry run. To actually power off:"
  echo "    sudo bash /opt/scripts/vacation-shutdown.sh --off"
  echo "  Or directly:"
  echo "    sudo systemctl poweroff"
fi
