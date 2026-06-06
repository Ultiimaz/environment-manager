#!/usr/bin/env python3
"""Patch /opt/hermes/tools/transcription_tools.py so the Groq Whisper call
passes a `prompt` parameter biasing transcription toward Blocksweb-specific
proper nouns (project names, agent names, repo names, etc).

Without this, Groq routinely mishears: 'BlocksWeb' -> 'block trap',
'Hermes' -> 'her mess', 'env-manager' -> 'envy manager'. Whisper's prompt
is a 224-token hint that strongly biases towards the included vocabulary.

The prompt content is read from stt.groq.prompt in config.yaml — if not
set, a sane Blocksweb default is used.

Idempotent via the `blocksweb-groq-prompt` marker.
"""
from __future__ import annotations
import sys
from pathlib import Path

TARGET = Path("/opt/hermes/tools/transcription_tools.py")
MARKER = "blocksweb-groq-prompt"
ANCHOR = """                # blocksweb-groq-language — force language from config, defaulting
                # to 'en'. Without this, Groq auto-detects from short clips and
                # often guesses Romanian / Polish / random for normal English.
                _groq_lang = _load_stt_config().get("groq", {}).get("language") or "en"
                transcription = client.audio.transcriptions.create(
                    model=model_name,
                    file=audio_file,
                    response_format="text",
                    language=_groq_lang,
                )"""

REPLACEMENT = """                # blocksweb-groq-language — force language from config, defaulting
                # to 'en'. Without this, Groq auto-detects from short clips and
                # often guesses Romanian / Polish / random for normal English.
                _groq_lang = _load_stt_config().get("groq", {}).get("language") or "en"
                # blocksweb-groq-prompt — bias Whisper toward our proper nouns.
                _default_prompt = (
                    "Blocksweb dashboard. Hermes Manager Engineer Marketing Scout. "
                    "Ultimaz Sven. kanban env-manager stripe-payments blocksweb-dashboard. "
                    "Discord voice channel Lounge. Laravel Postgres Traefik."
                )
                _groq_prompt = _load_stt_config().get("groq", {}).get("prompt") or _default_prompt
                transcription = client.audio.transcriptions.create(
                    model=model_name,
                    file=audio_file,
                    response_format="text",
                    language=_groq_lang,
                    prompt=_groq_prompt,
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
        print(f"anchor not found in {TARGET} (groq-language patch may be missing)", file=sys.stderr)
        return 1

    new = src.replace(ANCHOR, REPLACEMENT, 1)
    TARGET.write_text(new)
    print(f"patched {TARGET}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
