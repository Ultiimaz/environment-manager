#!/usr/bin/env python3
"""Patch /opt/hermes/tools/tts_tool.py so the Edge TTS function strips
emojis from text before generating audio.

When Manager runs tools in voice mode, Hermes injects progress bubbles
like '🔎 search_files: "trap"' and '📖 read_file: "/opt/data/vault/..."'
into the Discord text stream. With auto_tts: true, every text message
the bot sends also gets TTS'd — so Edge happily reads 'magnifying glass
search files quote trap quote', which is awful in a VC.

This patch strips Unicode category So / Sk / Sm (symbols, including all
emoji) from text right before passing to Edge's Communicate(). Plain
words and punctuation pass through unchanged.

Idempotent via the `blocksweb-tts-emoji-strip` marker.
"""
from __future__ import annotations
import sys
from pathlib import Path

TARGET = Path("/opt/hermes/tools/tts_tool.py")
MARKER = "blocksweb-tts-emoji-strip"
ANCHOR = "    communicate = _edge_tts.Communicate(text, **kwargs)"

REPLACEMENT = """    # blocksweb-tts-emoji-strip — drop emoji/symbols before TTS so Edge
    # doesn't speak '🔎' as 'magnifying glass'. Categories: So=symbol-other
    # (most emoji), Sk=symbol-modifier, Sm=symbol-math. Letters, digits,
    # punctuation pass through unchanged.
    import unicodedata as _ud
    _clean_text = "".join(
        ch for ch in text
        if _ud.category(ch) not in ("So", "Sk", "Sm")
    ).strip()
    if not _clean_text:
        _clean_text = "."  # empty TTS calls fail; one period is a quiet beat
    communicate = _edge_tts.Communicate(_clean_text, **kwargs)"""


def main() -> int:
    if not TARGET.exists():
        print(f"target not found: {TARGET}", file=sys.stderr)
        return 2

    src = TARGET.read_text()
    if MARKER in src:
        print(f"{TARGET} already patched (marker present)")
        return 0

    if ANCHOR not in src:
        print(f"anchor not found in {TARGET}", file=sys.stderr)
        return 1

    new = src.replace(ANCHOR, REPLACEMENT, 1)
    TARGET.write_text(new)
    print(f"patched {TARGET}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
