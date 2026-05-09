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
"  stt:\n"
"    provider: local\n"
"    # small.en transcribes ~4x more accurately than base.en. The model\n"
"    # downloads once on first use (~250 MB) into /opt/data/.cache.\n"
"    model: small.en\n"
"    # 0.02 was too low — whisper hallucinated 'I'm sorry' on mic noise.\n"
"    silence_threshold: 0.05\n"
"  tts:\n"
"    provider: edge\n"
"    voice: en-US-AvaNeural\n"
"  reply_mode: voice_for_voice\n"
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
