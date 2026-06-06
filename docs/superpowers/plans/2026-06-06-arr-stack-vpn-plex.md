# Arr stack + Mullvad VPN + Plex — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up an automated torrent → library → Plex media pipeline on the blocksweb home-lab (`192.168.1.116`), with qBittorrent locked behind a Mullvad WireGuard kill-switch, streaming to a cast-only Chromecast via direct-play.

**Architecture:** One host `docker compose` stack at `/opt/media-stack/`. Gluetun owns the VPN network namespace; qBittorrent rides inside it (`network_mode: service:gluetun`). Plex, Prowlarr, Radarr, Sonarr each get a static IP on the existing external `my-macvlan-net` and carry Traefik labels, so the `*.home` wildcard (→ Traefik `192.168.1.6`) routes each UI by Host header. All file-touching containers mount one media root at the same path (`/data`) so Radarr/Sonarr hardlink instead of copy.

**Tech Stack:** Docker Compose, Gluetun (`qmcgaw/gluetun`), LinuxServer.io images (qbittorrent, plex, prowlarr, radarr, sonarr), Mullvad WireGuard, Traefik v3 (already running), CoreDNS wildcard (already running).

---

## Deviation from the spec's "hybrid" wording

The spec said arr apps go "through env-manager." On inspection, env-manager **v2** is a Git/`.dev/`-driven PaaS that exposes **one service per project**, which doesn't fit three off-the-shelf multi-UI images. Direct compose uses the **same** Traefik + `*.home` routing mechanism, so the user-visible result is identical (`radarr.home`, `sonarr.home`, `prowlarr.home`). All six services therefore run in one host compose stack. The VPN-pod portion was always going to be direct compose anyway.

## Home-lab facts this plan relies on (verified)

- `*.home` resolves via a **CoreDNS wildcard → `192.168.1.6`** (Traefik). No per-host DNS records needed. (`references/coredns.md`)
- Traefik filters on `--providers.docker.network=my-macvlan-net`, entrypoint `web` (:80). Labels must include `traefik.docker.network=my-macvlan-net`. (`references/traefik.md`)
- **Macvlan host-isolation:** the host (`192.168.1.116`) *cannot* reach `192.168.1.x` macvlan container IPs. Test routing from another LAN device (the Windows box `192.168.1.135`) OR from a throwaway bridge container. (`references/docker-networking.md` gotcha #2)
- User `ultiimaz` is **not** in the `docker` group → every docker command uses `sudo`. SSH via plink with the `echo y` + `echo $PW | sudo -S` pattern. (`references/ssh-pattern.md`)
- **NVIDIA Container Toolkit is already installed** (the `ollama` container uses the GTX 960 via CDI `nvidia.com/gpu=all`). The optional Plex GPU task reuses that. (`references/gotchas.md` #16)
- Free macvlan IPs: `.10–.14`, `.15–.19` are unused (topology uses `.1–.9`, `.135`). This plan claims `.15–.19`.

## Inputs the operator must supply

| Input | Where to get it | Used in |
|---|---|---|
| Mullvad **WireGuard private key** + **assigned address** | mullvad.net → account → WireGuard configuration → generate key → the `.conf` shows `PrivateKey=` and `Address=10.x.x.x/32` | Task 2 `.env` |
| Mullvad **server city** | any city Mullvad lists (e.g. `Amsterdam`) | Task 2 `.env` |
| Plex **claim token** | https://www.plex.tv/claim (valid ~4 min — grab it right before Task 5) | Task 5 `.env` |

## File / directory structure (created by this plan)

```
/opt/media-stack/
├── docker-compose.yml          # the whole stack (Task 2)
├── .env                        # secrets + PUID/PGID/TZ (Task 2, chmod 600)
└── config/                     # per-app config (bind mounts)
    ├── gluetun/  qbittorrent/  plex/  prowlarr/  radarr/  sonarr/

/data/media/                    # one filesystem → hardlinks work
├── torrents/{movies,tv}/       # qBittorrent download targets
├── movies/                     # Radarr library  → Plex "Movies"
└── tv/                         # Sonarr library  → Plex "TV Shows"
```

---

## Task 0: Confirm host readiness & PUID/PGID

**Files:** none (read-only checks)

- [ ] **Step 1: SSH in and capture the media owner's UID/GID + verify tun + compose**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S bash -c '\
id ultiimaz; \
echo ---TUN---; ls -l /dev/net/tun || echo NO-TUN; \
echo ---COMPOSE---; docker compose version; \
echo ---NET---; docker network ls | grep my-macvlan-net; \
echo ---IPFREE---; for ip in 15 16 17 18 19; do ping -c1 -W1 192.168.1.\$ip >/dev/null 2>&1 && echo \"192.168.1.\$ip IN-USE\" || echo \"192.168.1.\$ip free\"; done'"
```

Expected:
- `uid=1000(ultiimaz) gid=1000(ultiimaz)` → use `PUID=1000 PGID=1000` (adjust if different).
- `/dev/net/tun` exists (needed by Gluetun). If `NO-TUN`: `sudo modprobe tun`.
- `Docker Compose version v2.x`.
- `my-macvlan-net` present.
- `192.168.1.15..19 free`.

- [ ] **Step 2: Record the values** you'll reuse: `PUID`, `PGID`, and confirmed-free IPs `.15–.19`.

---

## Task 1: Create the media tree and config dirs

**Files:** host directories under `/data/media` and `/opt/media-stack`

- [ ] **Step 1: Create directories with correct ownership**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S bash -c '\
mkdir -p /data/media/torrents/movies /data/media/torrents/tv /data/media/movies /data/media/tv; \
mkdir -p /opt/media-stack/config/gluetun /opt/media-stack/config/qbittorrent /opt/media-stack/config/plex /opt/media-stack/config/prowlarr /opt/media-stack/config/radarr /opt/media-stack/config/sonarr; \
chown -R 1000:1000 /data/media /opt/media-stack; \
echo DONE'"
```

(Use the PUID:PGID from Task 0 if not `1000:1000`.)

- [ ] **Step 2: Verify the tree**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S find /data/media -maxdepth 2 -type d"
```

Expected: lists `torrents/movies`, `torrents/tv`, `movies`, `tv`.

---

## Task 2: Write `.env` and `docker-compose.yml`

**Files:**
- Create: `/opt/media-stack/.env`
- Create: `/opt/media-stack/docker-compose.yml`

Stage these on the host with `cat > ~/stage/... <<'EOF'` then `sudo mv` (avoids the sudo-eats-heredoc trap — see `references/ssh-pattern.md`). Do **one file per plink invocation** so stdin isn't consumed twice.

- [ ] **Step 1: Write `.env`** (fill in the three Mullvad values; Plex claim is added in Task 5)

```bash
# fill REAL values before running
WGKEY='PASTE_MULLVAD_PRIVATE_KEY'
WGADDR='PASTE_MULLVAD_ADDRESS'      # e.g. 10.66.123.45/32
WGCITY='Amsterdam'

echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
mkdir -p ~/stage && cat > ~/stage/media.env <<EOF
PUID=1000
PGID=1000
TZ=Europe/Amsterdam
WIREGUARD_PRIVATE_KEY=$WGKEY
WIREGUARD_ADDRESSES=$WGADDR
SERVER_CITIES=$WGCITY
PLEX_CLAIM=
EOF
echo svenleon | sudo -S bash -c 'mv ~/stage/media.env /opt/media-stack/.env; chown 1000:1000 /opt/media-stack/.env; chmod 600 /opt/media-stack/.env; echo WROTE-ENV'"
```

Expected: `WROTE-ENV`. (`.env` is mode 600, never committed anywhere.)

- [ ] **Step 2: Write `docker-compose.yml`** — full content below. The single-quoted `'COMPOSEEOF'` delimiter disables shell expansion so the Traefik backtick rules survive verbatim.

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "cat > ~/stage/docker-compose.yml <<'COMPOSEEOF'
networks:
  my-macvlan-net:
    external: true

services:
  gluetun:
    image: qmcgaw/gluetun:latest
    container_name: gluetun
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    environment:
      - VPN_SERVICE_PROVIDER=mullvad
      - VPN_TYPE=wireguard
      - WIREGUARD_PRIVATE_KEY=\${WIREGUARD_PRIVATE_KEY}
      - WIREGUARD_ADDRESSES=\${WIREGUARD_ADDRESSES}
      - SERVER_CITIES=\${SERVER_CITIES}
      - FIREWALL_OUTBOUND_SUBNETS=192.168.1.0/24,172.21.0.0/16
      - TZ=\${TZ}
    volumes:
      - /opt/media-stack/config/gluetun:/gluetun
    networks:
      my-macvlan-net:
        ipv4_address: 192.168.1.15
    labels:
      - traefik.enable=true
      - traefik.http.routers.qbittorrent.rule=Host(\`qbittorrent.home\`)
      - traefik.http.routers.qbittorrent.entrypoints=web
      - traefik.http.services.qbittorrent.loadbalancer.server.port=8080
      - traefik.docker.network=my-macvlan-net
    restart: unless-stopped

  qbittorrent:
    image: lscr.io/linuxserver/qbittorrent:latest
    container_name: qbittorrent
    network_mode: \"service:gluetun\"
    depends_on:
      gluetun:
        condition: service_healthy
    environment:
      - PUID=\${PUID}
      - PGID=\${PGID}
      - TZ=\${TZ}
      - WEBUI_PORT=8080
    volumes:
      - /opt/media-stack/config/qbittorrent:/config
      - /data/media:/data
    restart: unless-stopped

  plex:
    image: lscr.io/linuxserver/plex:latest
    container_name: plex
    environment:
      - PUID=\${PUID}
      - PGID=\${PGID}
      - TZ=\${TZ}
      - VERSION=docker
      - PLEX_CLAIM=\${PLEX_CLAIM}
      - ADVERTISE_IP=http://192.168.1.16:32400/
    volumes:
      - /opt/media-stack/config/plex:/config
      - /data/media:/data
    networks:
      my-macvlan-net:
        ipv4_address: 192.168.1.16
    labels:
      - traefik.enable=true
      - traefik.http.routers.plex.rule=Host(\`plex.home\`)
      - traefik.http.routers.plex.entrypoints=web
      - traefik.http.services.plex.loadbalancer.server.port=32400
      - traefik.docker.network=my-macvlan-net
    # --- OPTIONAL GPU transcode (Plex Pass required). Host already has nvidia toolkit (ollama). ---
    # devices:
    #   - nvidia.com/gpu=all
    restart: unless-stopped

  prowlarr:
    image: lscr.io/linuxserver/prowlarr:latest
    container_name: prowlarr
    environment:
      - PUID=\${PUID}
      - PGID=\${PGID}
      - TZ=\${TZ}
    volumes:
      - /opt/media-stack/config/prowlarr:/config
    networks:
      my-macvlan-net:
        ipv4_address: 192.168.1.17
    labels:
      - traefik.enable=true
      - traefik.http.routers.prowlarr.rule=Host(\`prowlarr.home\`)
      - traefik.http.routers.prowlarr.entrypoints=web
      - traefik.http.services.prowlarr.loadbalancer.server.port=9696
      - traefik.docker.network=my-macvlan-net
    restart: unless-stopped

  radarr:
    image: lscr.io/linuxserver/radarr:latest
    container_name: radarr
    environment:
      - PUID=\${PUID}
      - PGID=\${PGID}
      - TZ=\${TZ}
    volumes:
      - /opt/media-stack/config/radarr:/config
      - /data/media:/data
    networks:
      my-macvlan-net:
        ipv4_address: 192.168.1.18
    labels:
      - traefik.enable=true
      - traefik.http.routers.radarr.rule=Host(\`radarr.home\`)
      - traefik.http.routers.radarr.entrypoints=web
      - traefik.http.services.radarr.loadbalancer.server.port=7878
      - traefik.docker.network=my-macvlan-net
    restart: unless-stopped

  sonarr:
    image: lscr.io/linuxserver/sonarr:latest
    container_name: sonarr
    environment:
      - PUID=\${PUID}
      - PGID=\${PGID}
      - TZ=\${TZ}
    volumes:
      - /opt/media-stack/config/sonarr:/config
      - /data/media:/data
    networks:
      my-macvlan-net:
        ipv4_address: 192.168.1.19
    labels:
      - traefik.enable=true
      - traefik.http.routers.sonarr.rule=Host(\`sonarr.home\`)
      - traefik.http.routers.sonarr.entrypoints=web
      - traefik.http.services.sonarr.loadbalancer.server.port=8989
      - traefik.docker.network=my-macvlan-net
    restart: unless-stopped
COMPOSEEOF
echo svenleon | sudo -S bash -c 'mv ~/stage/docker-compose.yml /opt/media-stack/docker-compose.yml; chown 1000:1000 /opt/media-stack/docker-compose.yml; echo WROTE-COMPOSE'"
```

Expected: `WROTE-COMPOSE`.

- [ ] **Step 3: Validate the compose syntax** (no containers started yet)

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml config -q && echo COMPOSE-OK"
```

Expected: `COMPOSE-OK` (and no warnings about unset `${WIREGUARD_*}` — if there are, the `.env` didn't load; confirm it's in the same dir).

---

## Task 3: Bring up Gluetun and prove the tunnel

**Files:** none (runtime)

- [ ] **Step 1: Start only gluetun**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml up -d gluetun"
```

- [ ] **Step 2: Wait for healthy, then read its public IP via the control server**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S bash -c '\
sleep 20; \
docker inspect --format \"{{.State.Health.Status}}\" gluetun; \
docker exec gluetun wget -qO- http://127.0.0.1:8000/v1/publicip/ip; echo'"
```

Expected: `healthy`, and a public IP **that is not** `84.84.207.234`. If it errors, read logs:
```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker logs gluetun --tail 40"
```
Common fixes: wrong `WIREGUARD_PRIVATE_KEY`/`ADDRESSES`, or `SERVER_CITIES` not matching a Mullvad city.

---

## Task 4: Bring up qBittorrent — routing, kill-switch, password

**Files:** none (runtime + UI)

- [ ] **Step 1: Start qbittorrent**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml up -d qbittorrent"
```

- [ ] **Step 2: Confirm Traefik routes `qbittorrent.home`** (from the host, via a bridge container — host can't hit macvlan directly)

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker run --rm --network environment-manager_env-manager-net curlimages/curl:latest -sI -H 'Host: qbittorrent.home' http://172.21.0.3/ | head -5"
```

Expected: an HTTP status line (200/401/403) + a `Server` header → Traefik is routing to qBittorrent. (Connection refused/empty = label or network problem; confirm `traefik.docker.network=my-macvlan-net` and that gluetun is on macvlan.)

- [ ] **Step 3: Get the temporary WebUI password** (LinuxServer qBittorrent generates one on first boot)

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker logs qbittorrent 2>&1 | grep -i 'temporary password'"
```

Expected: a line with the generated password. Username is `admin`.

- [ ] **Step 4: From a LAN device** (the Windows box or phone — NOT the host) open `http://qbittorrent.home/`, log in with `admin` + the temp password. Then **Tools → Options → Web UI**: set a permanent username/password. **Options → Downloads**: set default save path to `/data/torrents`, enable "Keep incomplete torrents in" → `/data/torrents/incomplete` (optional). Save.

- [ ] **Step 5: Kill-switch proof** — stop the tunnel and confirm qBittorrent cannot reach the internet

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S bash -c '\
docker stop gluetun; sleep 3; \
docker exec qbittorrent sh -c \"timeout 5 wget -qO- https://ifconfig.me || echo BLOCKED-NO-LEAK\"; \
docker start gluetun; sleep 15; docker start qbittorrent 2>/dev/null; echo RESTARTED'"
```

Expected: `BLOCKED-NO-LEAK` (qBittorrent has no route when the VPN is down → zero leak), then `RESTARTED`. If it printed an IP instead of `BLOCKED`, **stop and fix** — the kill-switch isn't working; verify qBittorrent's `network_mode: service:gluetun`.

---

## Task 5: Bring up Plex, claim the server, create libraries

**Files:** `/opt/media-stack/.env` (PLEX_CLAIM line)

- [ ] **Step 1: Get a fresh claim token** from https://www.plex.tv/claim (copy the `claim-...` string; it expires in ~4 minutes — do the next step immediately).

- [ ] **Step 2: Write the claim token into `.env` and start Plex**

```bash
CLAIM='claim-PASTE_HERE'
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S sed -i 's|^PLEX_CLAIM=.*|PLEX_CLAIM=$CLAIM|' /opt/media-stack/.env; \
echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml up -d plex; \
sleep 15; echo svenleon | sudo -S docker logs plex --tail 15"
```

Expected: Plex starts; logs show it picked up the claim (no "claim token" errors).

- [ ] **Step 3: Confirm routing** (bridge-container trick again)

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker run --rm --network environment-manager_env-manager-net curlimages/curl:latest -sI -H 'Host: plex.home' http://172.21.0.3/web/index.html | head -5"
```

Expected: 200/301/302/401.

- [ ] **Step 4: From a LAN device** open `http://plex.home/web` (or `http://192.168.1.16:32400/web`). Sign in to the Plex account the claim linked. **Add Library → Movies** → folder `/data/movies`. **Add Library → TV Shows** → folder `/data/tv`. Finish setup. (Libraries are empty for now — that's expected.)

---

## Task 6: Bring up Prowlarr, Radarr, Sonarr

**Files:** none (runtime + UI)

- [ ] **Step 1: Start the three arr apps**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml up -d prowlarr radarr sonarr"
```

- [ ] **Step 2: Confirm all three route** (loop the bridge-container test)

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S bash -c 'for h in prowlarr radarr sonarr; do echo \"== \$h ==\"; docker run --rm --network environment-manager_env-manager-net curlimages/curl:latest -sI -H \"Host: \$h.home\" http://172.21.0.3/ | head -2; done'"
```

Expected: each prints an HTTP status line (200/401) → all three are routed.

- [ ] **Step 3: From a LAN device**, open `http://radarr.home/`, `http://sonarr.home/`, `http://prowlarr.home/` once each to confirm the UIs load. (Config happens in the next tasks.)

---

## Task 7: Configure download client + root folders + quality profiles in Radarr & Sonarr

All via the web UIs from a LAN device. qBittorrent is reachable at host `qbittorrent.home` port `80` (through Traefik) **or** directly at `192.168.1.15:8080`. Use `192.168.1.15` / port `8080` for the download-client config (arr→qbit is container-to-container on macvlan, which works).

- [ ] **Step 1: Radarr → download client.** `radarr.home` → Settings → Download Clients → `+` → qBittorrent. Host `192.168.1.15`, Port `8080`, username/password (the permanent ones from Task 4 Step 4), Category `radarr`. Test → Save.

- [ ] **Step 2: Radarr → root folder.** Settings → Media Management → Root Folders → Add → `/data/movies`. Save. (Confirm Media Management → "Use Hardlinks instead of Copy" is **enabled** — it is by default.)

- [ ] **Step 3: Radarr → quality profile for direct-play.** Settings → Profiles → edit/create a profile that allows up to **1080p** and, under Custom Formats / qualities, **rank `x264` (H.264) above `x265`/HEVC**. (Goal: grab files the cast-only Chromecast direct-plays so Plex doesn't transcode. See spec §5.) Set this as the default profile.

- [ ] **Step 4: Sonarr → repeat Steps 1–3** at `sonarr.home`: download client qBittorrent `192.168.1.15:8080` Category `sonarr`; root folder `/data/tv`; 1080p-x264-preferred profile.

- [ ] **Step 5: Verify the download-client tests passed** (both Radarr and Sonarr show a green "Test was successful" on the qBittorrent client). If it fails: confirm qBittorrent's WebUI is on 8080 and that you used `192.168.1.15` (gluetun's IP), not `127.0.0.1`.

---

## Task 8: Wire Prowlarr indexers into Radarr & Sonarr

- [ ] **Step 1: Get Radarr & Sonarr API keys.** In each: Settings → General → API Key (copy).

- [ ] **Step 2: Prowlarr → add apps.** `prowlarr.home` → Settings → Apps → `+` → Radarr. Prowlarr Server `http://192.168.1.17:9696`, Radarr Server `http://192.168.1.18:7878`, paste Radarr's API key. Test → Save. Repeat → Sonarr with `http://192.168.1.19:8989` + Sonarr's API key.

- [ ] **Step 3: Prowlarr → add indexers.** Indexers → Add Indexer → add the public/semi-private trackers you intend to use (only ones you're entitled to use). Prowlarr auto-syncs them to Radarr & Sonarr.

- [ ] **Step 4: Verify sync.** In Radarr → Settings → Indexers and Sonarr → Settings → Indexers: the indexers you added in Prowlarr appear automatically. Run Prowlarr → System → Tasks → "Sync App Indexers" if not immediate.

---

## Task 9: End-to-end test + hardlink + direct-play verification

- [ ] **Step 1: Add one title.** In `radarr.home` → Movies → Add, pick a title **you have the legal right to download** (e.g. a public-domain or Creative-Commons film, or content you own), choosing the 1080p-x264 profile. Trigger a search.

- [ ] **Step 2: Watch it flow.** Radarr → Activity → Queue shows qBittorrent downloading (into `/data/torrents/movies`). When complete, Radarr imports it into `/data/movies/...`.

- [ ] **Step 3: Confirm the import used a hardlink (not a copy).** Same inode = hardlink:

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "\
echo svenleon | sudo -S bash -c '\
echo ==TORRENT==; find /data/media/torrents/movies -type f -printf \"%i %p\n\" | head; \
echo ==LIBRARY==; find /data/media/movies -type f -printf \"%i %p\n\" | head'"
```

Expected: the media file appears in both trees with the **same inode number** (first column). Different inodes = it copied → re-check "Use Hardlinks" and that both paths are under the single `/data/media` mount.

- [ ] **Step 4: Plex sees it.** `plex.home/web` → Movies library → the title appears with artwork (run "Scan Library Files" if not auto-detected).

- [ ] **Step 5: Play on the TV via Chromecast.** Cast the movie. In Plex → Settings → (server) → **Dashboard/Activity**, confirm the stream shows **"Direct Play"** (not "Transcode") for the 1080p-x264 file. Direct Play = success: zero server transcode load, exactly as designed.

---

## Task 10 (OPTIONAL): Enable GTX 960 hardware transcoding

Only if you buy **Plex Pass** and want guaranteed smooth playback of HEVC/4K or non-direct-play files. The host's NVIDIA toolkit is already working (ollama). GTX 960 NVENC allows ~2 concurrent transcodes — fine for home.

- [ ] **Step 1: Uncomment the GPU device** in `/opt/media-stack/docker-compose.yml` under the `plex` service:

```yaml
    devices:
      - nvidia.com/gpu=all
```

- [ ] **Step 2: Recreate only Plex** (do **not** `--force-recreate` the whole stack — see gotcha #16 about dependency side-effects):

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml up -d plex"
```

- [ ] **Step 3: Verify the GPU is visible inside Plex**

```bash
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker exec plex nvidia-smi -L"
```

Expected: lists `GPU 0: NVIDIA GeForce GTX 960`. If it errors with `unresolvable CDI devices`, the host hit the NVIDIA driver/library mismatch — apply the module-reload recipe in `references/gotchas.md` #16.

- [ ] **Step 4: Enable HW transcode in Plex.** `plex.home` → Settings → Transcoder → check **"Use hardware acceleration when available"** and **"Use hardware-accelerated video encoding."** Force a transcode (cast and pick a lower quality) and confirm the Dashboard shows `(hw)` next to Transcode.

---

## Rollback / teardown

```bash
# Stop & remove the whole stack (keeps config + media on disk):
echo y | plink -ssh -pw svenleon -batch ultiimaz@192.168.1.116 "echo svenleon | sudo -S docker compose -f /opt/media-stack/docker-compose.yml down"

# Full wipe (DESTROYS config; media under /data/media is untouched unless you rm it):
# sudo rm -rf /opt/media-stack/config/*
```

## Spec coverage check

- VPN + kill-switch (spec §2, §8) → Tasks 2–4.
- Hardlink storage layout (spec §3) → Tasks 1, 7, 9.
- Data flow (spec §4) → Tasks 7–9.
- Transcode-avoidance 1080p-x264 (spec §5) → Task 7 Steps 3–4.
- Secrets (spec §6) → Tasks 2, 5.
- Verification plan (spec §8) → Tasks 3, 4, 6, 9.
- Optional GTX 960 transcode (spec §5) → Task 10.
- Risks (spec §9): macvlan host-isolation → bridge-container tests; Mullvad no-port-forward → accepted, public indexers only; CoreDNS wildcard → no DNS edits needed; GPU not in env-manager → all direct compose.

## Out of scope (per spec §10)

Overseerr/Bazarr/Recyclarr/Tautulli, remote/off-LAN streaming, Usenet, TLS for `*.home`.
