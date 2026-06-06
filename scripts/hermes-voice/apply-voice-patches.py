#!/usr/bin/env python3
"""Apply the two discord.py patches needed for hermes voice on auto-join.

Run at image-build time inside the container, against the baked discord.py.
Idempotent — patches that already applied are no-ops.

Patches:
  1. VOICE_TIMEOUT 300 -> 1800 (line is an indented class attribute).
  2. Auto-join text-channel binding so transcripts dispatch when bot joins VC
     via voice_state_update auto-join (not the /voice join slash command).
"""
from __future__ import annotations
import re
import sys
from pathlib import Path

TARGET = Path("/opt/hermes/gateway/platforms/discord.py")
TEXTBIND_MARKER = "blocksweb-auto-join-textbind"
TEXTBIND_ANCHOR = 'logger.info("[auto-join] wired voice callbacks via gateway back-ref")'
TEXTBIND_INDENT = " " * 36

TEXTBIND_ADDITION = (
    "\n"
    + TEXTBIND_INDENT + f"# {TEXTBIND_MARKER} — bind VC as reply channel so transcripts dispatch\n"
    + TEXTBIND_INDENT + "try:\n"
    + TEXTBIND_INDENT + "    adapter_self._voice_text_channels[member.guild.id] = after.channel.id\n"
    + TEXTBIND_INDENT + "    if hasattr(adapter_self, \"_voice_sources\"):\n"
    + TEXTBIND_INDENT + "        adapter_self._voice_sources[member.guild.id] = {\n"
    + TEXTBIND_INDENT + "            \"platform\": \"discord\",\n"
    + TEXTBIND_INDENT + "            \"chat_id\": str(after.channel.id),\n"
    + TEXTBIND_INDENT + "            \"user_id\": _mid,\n"
    + TEXTBIND_INDENT + "            \"user_name\": member.display_name,\n"
    + TEXTBIND_INDENT + "            \"chat_type\": \"voice\",\n"
    + TEXTBIND_INDENT + "        }\n"
    + TEXTBIND_INDENT + "    logger.info(\"[auto-join] bound voice text channel %s for guild %s\", after.channel.id, member.guild.id)\n"
    + TEXTBIND_INDENT + "except Exception as _be:\n"
    + TEXTBIND_INDENT + "    logger.warning(\"[auto-join] text-channel bind failed: %s\", _be)"
)


def patch_voice_timeout(src: str) -> tuple[str, str]:
    """Return (new_src, message). Idempotent: no-op if already 1800 or value moved."""
    new, n = re.subn(
        r'^(\s*)VOICE_TIMEOUT\s*=\s*300\b',
        r'\1VOICE_TIMEOUT = 1800',
        src, count=1, flags=re.MULTILINE,
    )
    if n:
        return new, "VOICE_TIMEOUT bumped 300 -> 1800"
    if re.search(r'^\s*VOICE_TIMEOUT\s*=\s*1800\b', src, flags=re.MULTILINE):
        return src, "VOICE_TIMEOUT already 1800 (skip)"
    return src, "VOICE_TIMEOUT not found at expected pattern (skip)"


def patch_textbind(src: str) -> tuple[str, str]:
    """Return (new_src, message). Idempotent via marker comment."""
    if TEXTBIND_MARKER in src:
        return src, "auto-join textbind already patched (marker present)"
    if TEXTBIND_ANCHOR not in src:
        return src, "auto-join anchor missing (upstream changed?) — SKIP"
    new = src.replace(TEXTBIND_ANCHOR, TEXTBIND_ANCHOR + TEXTBIND_ADDITION, 1)
    return new, "auto-join textbind patch applied"


def main() -> int:
    if not TARGET.exists():
        print(f"target not found: {TARGET}", file=sys.stderr)
        return 2

    src = TARGET.read_text()
    src, msg1 = patch_voice_timeout(src)
    print(msg1)
    src, msg2 = patch_textbind(src)
    print(msg2)
    TARGET.write_text(src)
    return 0


if __name__ == "__main__":
    sys.exit(main())
