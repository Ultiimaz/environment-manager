#!/usr/bin/env python3
"""Idempotently patch hermes-manager's discord.py to bind a voice text channel
when the bot auto-joins a VC. Without this, transcripts get dropped silently
because _handle_voice_channel_input bails when _voice_text_channels[guild_id]
is unset (it's only set by the /voice join slash command, not auto-join).

Run inside the container:
  docker cp patch-voice-textbind.py hermes-manager:/tmp/
  docker exec hermes-manager python3 /tmp/patch-voice-textbind.py
"""
from __future__ import annotations
import sys
from pathlib import Path

TARGET = Path("/opt/hermes/gateway/platforms/discord.py")
MARKER = "blocksweb-auto-join-textbind"
ANCHOR = 'logger.info("[auto-join] wired voice callbacks via gateway back-ref")'

# Match indentation of the anchor line (40 spaces in our version of hermes).
INDENT = " " * 36

ADDITION = (
    "\n"
    + INDENT + f"# {MARKER} — bind VC as reply channel so transcripts dispatch\n"
    + INDENT + "try:\n"
    + INDENT + "    adapter_self._voice_text_channels[member.guild.id] = after.channel.id\n"
    + INDENT + "    if hasattr(adapter_self, \"_voice_sources\"):\n"
    + INDENT + "        adapter_self._voice_sources[member.guild.id] = {\n"
    + INDENT + "            \"platform\": \"discord\",\n"
    + INDENT + "            \"chat_id\": str(after.channel.id),\n"
    + INDENT + "            \"user_id\": _mid,\n"
    + INDENT + "            \"user_name\": member.display_name,\n"
    + INDENT + "            \"chat_type\": \"voice\",\n"
    + INDENT + "        }\n"
    + INDENT + "    logger.info(\"[auto-join] bound voice text channel %s for guild %s\", after.channel.id, member.guild.id)\n"
    + INDENT + "except Exception as _be:\n"
    + INDENT + "    logger.warning(\"[auto-join] text-channel bind failed: %s\", _be)"
)


def main() -> int:
    if not TARGET.exists():
        print(f"target not found: {TARGET}", file=sys.stderr)
        return 2
    src = TARGET.read_text()

    if MARKER in src:
        print(f"{TARGET} already patched (marker present)")
        return 0

    if ANCHOR not in src:
        print(f"anchor line not found in {TARGET} — auto-join patch missing or moved", file=sys.stderr)
        return 1

    # Insert the addition immediately after the anchor line. Single replace so
    # re-running on a partially-patched tree doesn't double-insert.
    new = src.replace(ANCHOR, ANCHOR + ADDITION, 1)
    TARGET.write_text(new)
    print(f"patched {TARGET}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
