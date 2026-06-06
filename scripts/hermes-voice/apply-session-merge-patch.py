#!/usr/bin/env python3
"""Patch /opt/hermes/gateway/session.py so build_session_key() merges all
messages from the same Discord user into a single session when the marker
file /opt/data/.merge_sessions_per_user is present.

Solves: voice transcripts dispatch through the VC's text channel; typing in
DMs or #general lands on different chat_ids. The default key generator
includes chat_id, so a single user's voice + DM + group convos all get
separate session keys and don't share memory.

When the marker file exists, this patch short-circuits the function to
return `agent:main:discord:user:{user_id}` regardless of which Discord chat
the message came from.

Idempotent via the `blocksweb-merge-sessions-per-user` marker. Safe to run
on an already-patched file.
"""
from __future__ import annotations
import sys
from pathlib import Path

TARGET = Path("/opt/hermes/gateway/session.py")
MARKER = "blocksweb-merge-sessions-per-user"
ANCHOR = "    platform = source.platform.value"
INDENT = "    "

ADDITION = (
    "\n"
    + INDENT + f"# {MARKER} — if marker file is present, key per-user only so\n"
    + INDENT + "# voice (VC text channel), DM, and group messages from the same\n"
    + INDENT + "# Discord user merge into one session.\n"
    + INDENT + 'if (source.platform == Platform.DISCORD and source.user_id\n'
    + INDENT + '        and __import__("os").path.exists("/opt/data/.merge_sessions_per_user")):\n'
    + INDENT + '    return f"agent:main:discord:user:{source.user_id}"\n'
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
        print(f"anchor '{ANCHOR.strip()}' not found in {TARGET}", file=sys.stderr)
        return 1

    # Insert ADDITION before the anchor line so the early-return runs before
    # the original key-building logic kicks in.
    new = src.replace(ANCHOR, ADDITION + ANCHOR, 1)
    TARGET.write_text(new)
    print(f"patched {TARGET}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
