#!/usr/bin/env bash
# Provision the Hermes Engineer agent for env-manager v2 ship flow.
#
#   1. Backup the existing v1 deploy skill.
#   2. Push the new v2 deploy skill + ship-a-new-product skill onto the host.
#   3. Read the admin token from env-manager and write it into engineer.env.
#   4. Restart hermes-engineer so the new env + skills take effect.
#
# Idempotent: re-running won't multiply ENVM_TOKEN entries or duplicate skill
# files. Reads skill content from the env-manager repo (working dir).
#
# Run from a host with sudo + docker access:
#   sudo bash provision-engineer-skills.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_SKILL_SRC="$REPO_ROOT/hermes-skills/blocksweb-homelab-deploy.md"
SHIP_SKILL_SRC="$REPO_ROOT/hermes-skills/ship-a-new-product.md"

ENG_SKILLS=/data/hermes-engineer/skills
DEPLOY_DIR="$ENG_SKILLS/blocksweb-homelab-deploy"
SHIP_DIR="$ENG_SKILLS/ship-a-new-product"
ENG_ENV=/data/hermes-secrets/engineer.env

if [[ ! -f "$DEPLOY_SKILL_SRC" || ! -f "$SHIP_SKILL_SRC" ]]; then
  echo "missing skill source files; run from a checked-out env-manager repo" >&2
  exit 1
fi

echo "==> backing up existing v1 deploy skill"
if [[ -f "$DEPLOY_DIR/SKILL.md" ]]; then
  cp -a "$DEPLOY_DIR/SKILL.md" "$DEPLOY_DIR/SKILL.md.v1.bak.$(date +%s)"
fi

echo "==> writing v2 deploy skill"
mkdir -p "$DEPLOY_DIR"
cp "$DEPLOY_SKILL_SRC" "$DEPLOY_DIR/SKILL.md"
chown -R 10000:10000 "$DEPLOY_DIR"

echo "==> writing ship-a-new-product skill"
mkdir -p "$SHIP_DIR"
cp "$SHIP_SKILL_SRC" "$SHIP_DIR/SKILL.md"
chown -R 10000:10000 "$SHIP_DIR"

echo "==> reading admin token from env-manager"
TOKEN="$(docker exec env-manager envm admin-token show 2>&1)"
if [[ ! "$TOKEN" =~ ^envm_ ]]; then
  echo "unexpected admin-token output: $TOKEN" >&2
  exit 1
fi

echo "==> writing ENVM_TOKEN into engineer.env"
# Strip any prior ENVM_TOKEN line so re-runs don't accumulate.
if [[ -f "$ENG_ENV" ]]; then
  grep -v '^ENVM_TOKEN=' "$ENG_ENV" > "$ENG_ENV.tmp" || true
  mv "$ENG_ENV.tmp" "$ENG_ENV"
fi
echo "ENVM_TOKEN=$TOKEN" >> "$ENG_ENV"
chmod 600 "$ENG_ENV"
chown root:root "$ENG_ENV"

echo "==> restarting hermes-engineer"
docker restart hermes-engineer

echo "==> verifying ENVM_TOKEN reached the container"
sleep 3
GOT=$(docker exec hermes-engineer printenv ENVM_TOKEN 2>/dev/null || true)
if [[ "$GOT" != "$TOKEN" ]]; then
  echo "engineer container ENVM_TOKEN mismatch — check the docker run / compose for env_file=$ENG_ENV" >&2
  exit 1
fi

echo "==> verifying skills present in container"
docker exec hermes-engineer ls /opt/data/skills/blocksweb-homelab-deploy/SKILL.md \
                           /opt/data/skills/ship-a-new-product/SKILL.md

echo
echo "DONE. Engineer can now use envm v2 + ship-a-new-product flow."
