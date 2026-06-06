# Builds hermes-voice:local — hermes:local with all voice deps + discord.py
# patches baked into the image. Replaces the runtime setup-voice-manager.sh
# dance so voice survives `docker rm hermes-manager && docker run ...`.
#
# Build:
#   sudo docker build -t hermes-voice:local /opt/src/hermes-voice
#
# Run (drop-in replacement for hermes:local on the manager bot):
#   sudo docker run -d --name hermes-manager --restart unless-stopped \
#     --network my-macvlan-net --ip 192.168.1.14 \
#     -v /data/hermes-manager:/opt/data \
#     -e DISCORD_BOT_TOKEN=... -e DISCORD_ALLOWED_USERS=... \
#     hermes-voice:local
#
# When hermes:local upgrades, this image must be rebuilt — the COPY below
# overlays an entire pre-patched discord.py extracted from the running
# manager bot. To refresh discord.patched.py from the current running bot:
#   docker exec hermes-manager cat /opt/hermes/gateway/platforms/discord.py \
#     > /opt/src/hermes-voice/discord.patched.py

FROM hermes:local

USER root

# System audio libraries the python deps link against at runtime.
# faster-whisper -> ctranslate2 uses libgomp; sounddevice -> portaudio;
# discord voice -> libopus + ffmpeg for PCM decoding.
RUN apt-get update -qq && apt-get install -y --no-install-recommends \
      portaudio19-dev \
      ffmpeg \
      libopus0 \
      espeak-ng \
    && rm -rf /var/lib/apt/lists/*

# Voice python deps installed into the same venv hermes runs from.
# `uv` is already in the base image (used by hermes itself).
RUN uv pip install --python /opt/hermes/.venv \
      "faster-whisper>=1.0,<2" \
      "sounddevice>=0.4.6,<1" \
      "numpy>=1.24,<3" \
      edge-tts \
      pynacl

# Bake in the patched discord.py (contains blocksweb-auto-join-patch +
# blocksweb-auto-join-textbind + VOICE_TIMEOUT=1800). Sourced from the
# running hermes-manager — see header comment for refresh procedure.
COPY discord.patched.py /opt/hermes/gateway/platforms/discord.py
RUN chown 10000:10000 /opt/hermes/gateway/platforms/discord.py

# Switch back to the runtime user that hermes:local uses.
# HERMES_UID is set in the base image env to 10000.
USER 10000
