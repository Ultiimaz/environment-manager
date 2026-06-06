#!/usr/bin/env bash
# Installs voice-mode deps + config into hermes-manager and restarts.
# Run as root on 192.168.1.116:
#   sudo bash setup-voice-manager.sh
#
# Idempotent — re-running upgrades pip packages and overwrites the voice
# block in config.yaml only.
#
# NOT persistent across container recreation (apt + venv installs both live
# in image-managed dirs, not the /opt/data mount). If hermes-manager gets
# rebuilt from scratch, re-run this script.

set -euo pipefail

CONTAINER=hermes-manager
DATA=/data/hermes-manager
CONFIG=$DATA/config.yaml

echo "==> [1/4] apt packages (system audio libs)"
docker exec -u 0 "$CONTAINER" sh -c 'apt-get update -qq && apt-get install -y --no-install-recommends portaudio19-dev ffmpeg libopus0 espeak-ng' 2>&1 | tail -5

echo "==> [2/4] python voice deps in /opt/hermes/.venv"
docker exec -u 0 "$CONTAINER" sh -c 'uv pip install --python /opt/hermes/.venv "faster-whisper>=1.0,<2" "sounddevice>=0.4.6,<1" "numpy>=1.24,<3" edge-tts pynacl' 2>&1 | tail -8

echo "==> [3a/4] applying text-bind patch (transcripts get dropped silently otherwise)"
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
if [[ -f "$SCRIPT_DIR/patch-voice-textbind.py" ]]; then
  docker cp "$SCRIPT_DIR/patch-voice-textbind.py" "$CONTAINER:/tmp/patch-voice-textbind.py"
  docker exec "$CONTAINER" python3 /tmp/patch-voice-textbind.py
fi

echo "==> [3b/4] bumping VOICE_TIMEOUT 300s -> 1800s (agent work between voice events)"
# Idempotent — only rewrites if the constant still equals 300. The line is
# indented (class attribute), so match leading whitespace and preserve it.
docker exec -u 0 "$CONTAINER" python3 - <<'PY'
import pathlib, re
p = pathlib.Path("/opt/hermes/gateway/platforms/discord.py")
src = p.read_text()
new, n = re.subn(
    r'^(\s*)VOICE_TIMEOUT\s*=\s*300\b',
    r'\1VOICE_TIMEOUT = 1800',
    src, count=1, flags=re.MULTILINE,
)
if n:
    p.write_text(new)
    print("VOICE_TIMEOUT bumped to 1800")
else:
    print("VOICE_TIMEOUT already patched or constant moved — no-op")
PY

echo "==> [3/4] voice config in $CONFIG"
# Strip any existing top-level `voice:` block, then append the canonical one.
python3 - <<PY
import re, pathlib
p = pathlib.Path("$CONFIG")
text = p.read_text()
# Drop any existing top-level (zero-indent) "voice:" block.
text = re.sub(r'^voice:\s*\n(?:[ \t]+.*\n|\s*\n)*', '', text, flags=re.MULTILINE)
text = text.rstrip() + "\n\nvoice:\n" + (
"  enabled: true\n"
"  # Global TTS reply default — when true, every reply (voice OR text)\n"
"  # gets TTS'd into the user's connected VC. Per-chat opt-in/out via\n"
"  # the /voice tts and /voice off slash commands.\n"
"  auto_tts: true\n"
"  stt:\n"
"    provider: groq\n"
"    # whisper-large-v3 is most accurate. The -turbo variant is faster\n"
"    # but routinely mishears 'app' -> 'apple', 'Blocksweb' -> 'block trap'.\n"
"    model: whisper-large-v3\n"
"    groq:\n"
"      language: en\n"
"      # Whisper accepts a 224-token vocabulary hint that strongly biases\n"
"      # transcription. Pack with proper nouns + phrases this deployment\n"
"      # uses constantly so they stop mis-mapping to common words.\n"
"      prompt: \"Blocksweb dashboard companion app. Hermes Manager Engineer Marketing Scout Ultimaz Sven. Kanban env-manager stripe-payments blocksweb-dashboard. Discord voice channel Lounge. Laravel Postgres Traefik Vercel Stripe.\"\n"
"  tts:\n"
"    provider: edge\n"
"    voice: en-US-AvaNeural\n"
)
p.write_text(text)
print("voice block written, total bytes:", len(text))
PY
chown 10000:10000 "$CONFIG"

echo "==> [4/4] restart $CONTAINER"
docker restart "$CONTAINER" >/dev/null
sleep 6

echo
echo "Recent gateway log:"
docker exec "$CONTAINER" tail -25 /opt/data/logs/gateway.log
echo
echo "DONE. In Discord, run /voice join in your VC and check ${CONTAINER} logs."
