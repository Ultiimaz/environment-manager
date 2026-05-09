#!/usr/bin/env bash
# Adds `_voice_text_channels[guild_id] = after.channel.id` to the auto-join
# patch in discord.py so transcripts produced via auto-join (not /voice join)
# actually reach the agent loop. Without this, _handle_voice_channel_input
# returns silently when text_ch_id is None.
#
# Idempotent — looks for the new marker before patching.
set -euo pipefail

CONTAINER=hermes-manager
TARGET=/opt/hermes/gateway/platforms/discord.py
MARKER='# blocksweb-auto-join-textbind'

if docker exec "$CONTAINER" grep -q "$MARKER" "$TARGET"; then
  echo "patch already applied (marker present) — no-op"
  exit 0
fi

# Patch in-place via Python so we don't fight whitespace.
docker exec -u 0 "$CONTAINER" python3 - <<PY
import pathlib, re, sys
p = pathlib.Path("$TARGET")
src = p.read_text()

# Insertion point: right after the "wired voice callbacks via gateway back-ref"
# log line within the existing auto-join patch.
needle = 'logger.info("[auto-join] wired voice callbacks via gateway back-ref")'
if needle not in src:
    print("ERROR: anchor line not found — auto-join patch missing or moved", file=sys.stderr)
    sys.exit(1)

addition = (
    '\n                                    # $MARKER — bind VC as reply channel so transcripts dispatch'
    '\n                                    try:'
    '\n                                        adapter_self._voice_text_channels[member.guild.id] = after.channel.id'
    '\n                                        if hasattr(adapter_self, "_voice_sources"):'
    '\n                                            adapter_self._voice_sources[member.guild.id] = {'
    '\n                                                "platform": "discord",'
    '\n                                                "chat_id": str(after.channel.id),'
    '\n                                                "user_id": _mid,'
    '\n                                                "user_name": member.display_name,'
    '\n                                                "chat_type": "voice",'
    '\n                                            }'
    '\n                                        logger.info("[auto-join] bound voice text channel %s for guild %s", after.channel.id, member.guild.id)'
    '\n                                    except Exception as _be:'
    '\n                                        logger.warning("[auto-join] text-channel bind failed: %s", _be)'
)

new = src.replace(needle, needle + addition, 1)
p.write_text(new)
print("patched", p)
PY

echo "==> reload manager"
docker restart "$CONTAINER" >/dev/null
sleep 5
docker exec "$CONTAINER" tail -10 /opt/data/logs/gateway.log
