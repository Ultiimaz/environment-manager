#!/usr/bin/env bash
# Adds Google Stitch MCP to a Hermes agent's config.yaml so the agent can
# call Stitch tools natively (mcp_stitch_*) without going through a Claude
# Code subprocess.
#
# Idempotent: re-running after stitch is already wired is a no-op (well, a
# safe overwrite of the same block).
#
# Run on the host:
#   sudo bash add-stitch-mcp.sh hermes-engineer
#   sudo bash add-stitch-mcp.sh hermes          # marketing
#   sudo bash add-stitch-mcp.sh hermes-scout
#   sudo bash add-stitch-mcp.sh hermes-manager

set -euo pipefail

CONTAINER="${1:-hermes-engineer}"
case "$CONTAINER" in
  hermes-engineer) DATA_DIR=/data/hermes-engineer ;;
  hermes)          DATA_DIR=/data/hermes ;;
  hermes-scout)    DATA_DIR=/data/hermes-scout ;;
  hermes-manager)  DATA_DIR=/data/hermes-manager ;;
  *) echo "unknown container: $CONTAINER" >&2; exit 1 ;;
esac

CONFIG="$DATA_DIR/config.yaml"
STITCH_KEY="${STITCH_KEY:-AQ.Ab8RN6Lx7DJAyhoEivK_E8QqMisjyGr_mt6cNcUjwKJiZOynng}"

if [[ ! -f "$CONFIG" ]]; then
  echo "config not found: $CONFIG" >&2
  exit 1
fi

# Hermes' MCP support is gated on the `mcp` Python package being importable
# from /opt/hermes/.venv. The venv lives in the image, NOT in the persistent
# /opt/data mount, so this install is lost when the container is recreated.
# Bake into the Dockerfile if you do that often.
echo "==> ensuring mcp package is in $CONTAINER's venv"
docker exec -u 0 "$CONTAINER" uv pip install mcp 2>&1 | tail -3 || true

# Backup before edit.
cp -a "$CONFIG" "$CONFIG.before-stitch-mcp.$(date +%s)"

# Detect any active (uncommented) `mcp_servers:` block.
if grep -qE '^mcp_servers:' "$CONFIG"; then
  # Block exists — strip our existing stitch entry (if any) and re-add it
  # so re-runs always converge on the canonical form. We use a python pass
  # here because YAML round-tripping with awk on a 49 KB file is fragile.
  python3 <<PYEOF
import io, re, sys

path = "$CONFIG"
with open(path) as f:
    text = f.read()

# Find the active mcp_servers block (no leading #) and lift it out.
lines = text.split("\n")
out = []
in_block = False
block_indent = None
skip_existing_stitch = False

i = 0
while i < len(lines):
    line = lines[i]
    if not in_block and re.match(r'^mcp_servers:\s*$', line):
        in_block = True
        out.append(line)
        i += 1
        # Re-emit existing entries except 'stitch:' (which we'll re-add).
        while i < len(lines):
            sub = lines[i]
            if sub.startswith(' ') or sub.startswith('\t') or sub.strip() == '':
                # entry / blank inside the block
                # detect entry header (e.g. "  stitch:")
                m = re.match(r'^( +)([a-zA-Z0-9_-]+):\s*$', sub)
                if m and m.group(2) == 'stitch':
                    # skip the whole stitch sub-block
                    indent = m.group(1)
                    i += 1
                    while i < len(lines) and (lines[i].startswith(indent + ' ') or lines[i].startswith(indent + '\t') or lines[i].strip() == ''):
                        i += 1
                    continue
                out.append(sub)
                i += 1
                continue
            break
        # Append our stitch entry as the last item in the block.
        out.append("  stitch:")
        out.append("    url: https://stitch.googleapis.com/mcp")
        out.append("    headers:")
        out.append('      X-Goog-Api-Key: "$STITCH_KEY"')
        out.append("    timeout: 180")
        in_block = False
        continue
    out.append(line)
    i += 1

with open(path, 'w') as f:
    f.write("\n".join(out))
PYEOF
else
  # No active block — append a new one at the end.
  cat >> "$CONFIG" <<EOF

# Added by add-stitch-mcp.sh
mcp_servers:
  stitch:
    url: https://stitch.googleapis.com/mcp
    headers:
      X-Goog-Api-Key: "$STITCH_KEY"
    timeout: 180
EOF
fi

chown 10000:10000 "$CONFIG"
chmod 640 "$CONFIG"

echo "wrote stitch MCP config to $CONFIG"
echo "restart $CONTAINER to pick up the change:"
echo "  docker restart $CONTAINER"
