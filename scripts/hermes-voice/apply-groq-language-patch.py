#!/usr/bin/env python3
"""Patch /opt/hermes/tools/transcription_tools.py so the Groq Whisper call
forces language='en' (or whatever stt.groq.language is set to in
config.yaml), instead of letting Groq auto-detect.

Auto-detect picks the wrong language on short or noisy English speech
(observed: Romanian gibberish on a clean Discord voice clip), which is
worse than the local Whisper small.en model we were trying to replace.

Idempotent via the `blocksweb-groq-language` marker.
"""
from __future__ import annotations
import sys
from pathlib import Path

TARGET = Path("/opt/hermes/tools/transcription_tools.py")
MARKER = "blocksweb-groq-language"
ANCHOR = """                transcription = client.audio.transcriptions.create(
                    model=model_name,
                    file=audio_file,
                    response_format="text",
                )"""

REPLACEMENT = """                # blocksweb-groq-language — force language from config, defaulting
                # to 'en'. Without this, Groq auto-detects from short clips and
                # often guesses Romanian / Polish / random for normal English.
                _groq_lang = _load_stt_config().get("groq", {}).get("language") or "en"
                transcription = client.audio.transcriptions.create(
                    model=model_name,
                    file=audio_file,
                    response_format="text",
                    language=_groq_lang,
                )"""


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
