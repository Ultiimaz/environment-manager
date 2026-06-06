# Arr stack + Mullvad VPN + Plex — home-lab design

**Date:** 2026-06-06
**Host:** blocksweb home-lab, `192.168.1.116` (Ubuntu 24.04, Docker 29.1.3)
**Goal:** Automated movie/TV acquisition behind a VPN, streamed to a TV in real time
via Plex — no copying files to the TV first.

> **Responsible-use note:** The arr stack is general-purpose media-automation software.
> Use it only for content you have the legal right to download and store (media you own,
> public-domain, Creative-Commons, Linux ISOs, your own rips, etc.). This design takes no
> position on what is fetched; that is the operator's responsibility.

---

## 1. Decisions (locked)

| Decision | Choice | Notes |
|---|---|---|
| VPN provider | **Mullvad** (WireGuard) | Flat fee, no logs. **No port forwarding since 2023** — downloads fine, seeding less optimal. |
| Acquisition | **Torrents only** | qBittorrent behind the VPN with kill-switch. |
| Media server | **Plex** | LAN-only streaming. |
| TV client | **Cast-only Chromecast (1080p)** | Direct-plays 1080p H.264; would transcode HEVC/4K. |
| Deploy topology | **Hybrid** | VPN pod + Plex = direct compose; arr apps = env-manager. |
| Remote access | **Home LAN only** | No external exposure. |
| Transcode strategy | **Avoid via 1080p x264 profiles**; GTX 960 as optional safety net | Plex Pass not required day one. |

### Host facts that shaped this
- **Disk:** single 931 GB SSD, ~713 GB free, **no separate media drive**. Start here; add a
  dedicated disk as the library grows (4K films are 20–60 GB each).
- **GPU:** NVIDIA **GTX 960** (Maxwell) — NVENC/NVDEC for H.264 **and** HEVC. Usable for Plex
  hardware transcoding *if* Plex Pass + NVIDIA Container Toolkit are installed.
- **CPU:** Intel i5-6400 (4 cores, no HT) — ~1 simultaneous 1080p CPU transcode; 4K chokes.
- **`/data`:** 6.9 GB used — plenty of headroom on the existing tree.

---

## 2. Architecture

Six services in v1. (Overseerr request portal, Bazarr subtitles, Recyclarr, etc. are
deliberately deferred — YAGNI for the first working stack.)

| Service | Role | Runs in | Network / address |
|---|---|---|---|
| **Gluetun** | Mullvad WireGuard tunnel + kill-switch | Direct compose `/opt/media-stack/` | macvlan `192.168.1.15` |
| **qBittorrent** | Torrent client, **inside Gluetun netns** | Direct compose | `network_mode: service:gluetun` → `qbittorrent.home` |
| **Plex** | Media server, streams to TV | Direct compose | macvlan `192.168.1.16` → `plex.home` |
| **Prowlarr** | Indexer manager | env-manager | `prowlarr.home` |
| **Radarr** | Movies automation | env-manager | `radarr.home` |
| **Sonarr** | TV automation | env-manager | `sonarr.home` |

### Why this split
Only **qBittorrent** needs the VPN. The bulletproof pattern is Gluetun owning the network
namespace and qBittorrent riding inside it (`network_mode: service:gluetun`) with a
kill-switch: if the tunnel drops, qBittorrent loses *all* connectivity — no IP leak.

A container that shares Gluetun's namespace **cannot have its own Docker network or IP**, which
fights env-manager's per-service macvlan + Traefik-label injection. env-manager's bundled Docker
CLI is also too old and `compose/{project}/up` is known to fail (see `project_env_manager_quirks`).
So the VPN pod runs as a small **direct compose** stack. Plex joins that stack too because it
needs (a) a LAN IP for client discovery and (b) optional NVIDIA GPU passthrough, which
env-manager cannot express.

The three stateless arr apps stay **on-brand in env-manager**, getting automatic Traefik routing
and `*.home` names.

### Networking detail
- **Gluetun** is attached to `my-macvlan-net` with static IP `192.168.1.15`. It still creates its
  own `tun0` for the VPN and routes outbound traffic through it; the macvlan interface is only for
  LAN-side management/UI reachability. Gluetun's `FIREWALL_OUTBOUND_SUBNETS` is set to the LAN +
  Docker bridge subnets so the qBittorrent UI and the arr→qBit API calls are reachable while
  everything else is forced through Mullvad.
- **qBittorrent**'s web UI (`:8080`) and the torrent listen port are exposed at Gluetun's macvlan
  IP because they share the namespace.
- **Traefik** filters on `my-macvlan-net`. Gluetun carries **manual** Traefik labels (it's outside
  env-manager) to register `qbittorrent.home`. Plex carries manual labels for `plex.home`.
- **Container-to-container on the same macvlan works** (arr apps ↔ Gluetun ↔ Plex). Only host↔macvlan
  is isolated, which LAN-only streaming does not need.
- The three arr apps reach qBittorrent's API at `qbittorrent.home` (or `192.168.1.15:8080`) and each
  other / Prowlarr over `my-macvlan-net` exactly as env-manager already wires `*.home`.

---

## 3. Storage layout (hardlink-critical)

Single SSD, so one media root mounted at the **same in-container path** (`/data`) for every
service that touches files. Identical mount path + same filesystem is what lets Radarr/Sonarr
**hardlink** completed downloads into the library instead of doing a slow full copy.

```
/data/media/                 (host)  →  /data  (in qbittorrent, radarr, sonarr, plex)
├── torrents/
│   ├── movies/              ← qBittorrent download target (Radarr category)
│   └── tv/                  ← qBittorrent download target (Sonarr category)
├── movies/                  ← Radarr-managed library  → Plex "Movies"
└── tv/                      ← Sonarr-managed library   → Plex "TV Shows"
```

- Single shared **PUID/PGID** across all six containers so ownership is consistent.
- Per-app config volumes live under `/data/media/config/<app>/` (or env-manager's volume
  convention for the three it manages).
- **Scaling step (documented, not v1):** when the SSD fills, add a dedicated media disk, mount it,
  and move `/data/media` to it — keeping torrents + library on the *same* new filesystem to preserve
  hardlinks.

---

## 4. Data flow

1. Add a movie in **Radarr** → it queries **Prowlarr**'s indexers, selects a release per the
   quality profile.
2. Radarr sends the magnet/.torrent to **qBittorrent** (behind Mullvad, kill-switched), tagged with
   the `movies` category → downloads into `/data/torrents/movies/`.
3. On completion, Radarr **hardlinks + renames** the file into `/data/movies/...` (instant, same FS).
4. **Plex** detects the new file, fetches metadata + artwork.
5. Open Plex on the Chromecast → stream over the LAN. **Direct-play** when the codec matches
   (1080p H.264); transcode only as a fallback.
6. Sonarr follows the identical flow for TV into `/data/tv/`.

---

## 5. Transcode-avoidance strategy

A cast-only Chromecast direct-plays **1080p H.264 (x264)** natively. Therefore:

- **Radarr/Sonarr quality profiles prefer 1080p x264** and rank H.264 above HEVC/x265, so the
  files that land are ones the Chromecast plays without any server-side transcoding.
- Result: **near-zero day-to-day transcode load**, no Plex Pass required to get a working setup.
- **Optional safety net:** for occasional HEVC-only or higher-bitrate releases, enable **GTX 960
  hardware transcoding**. Prerequisites: Plex Pass + **NVIDIA Container Toolkit** on the host +
  GPU device reservation on the Plex container. Wired into the compose file but commented/optional.

---

## 6. Secrets / inputs required from operator

1. **Mullvad WireGuard private key + assigned address** — from the Mullvad account WireGuard config
   generator. Supplied to Gluetun as `WIREGUARD_PRIVATE_KEY` + `WIREGUARD_ADDRESSES`
   (+ `SERVER_CITIES`/`SERVER_COUNTRIES` for endpoint selection). Stored in `/opt/media-stack/.env`
   (mode 600), never committed.
2. **Plex claim token** — from `https://claim.plex.tv` (valid ~4 min) to link the server to the
   Plex account on first boot (`PLEX_CLAIM` env). LAN-only after that.

---

## 7. Prerequisites & setup order

1. **Host prep:** create `/data/media/{torrents/{movies,tv},movies,tv,config}`; choose PUID/PGID.
2. *(Optional, only for GPU transcode)* install NVIDIA driver + **NVIDIA Container Toolkit**, verify
   `docker run --rm --gpus all nvidia/cuda nvidia-smi`.
3. **Direct compose stack** `/opt/media-stack/`: Gluetun → qBittorrent → Plex. Bring up Gluetun
   first, verify the tunnel, then the rest.
4. **env-manager apps:** deploy Prowlarr, Radarr, Sonarr (with fallback to host `docker compose` +
   manual macvlan/Traefik labels if env-manager's `up` fails per the known quirk).
5. **Wire it together:** Prowlarr → add indexers, push to Radarr/Sonarr; Radarr/Sonarr → add
   qBittorrent download client (`qbittorrent.home`) + categories + root folders; Radarr/Sonarr →
   quality profiles (1080p x264). Plex → add Movies (`/data/movies`) + TV (`/data/tv`) libraries.

---

## 8. Verification plan

- **VPN active & correct:** `docker exec qbittorrent wget -qO- ifconfig.me` returns a **Mullvad** IP,
  **not** the home public IP `84.84.207.234`.
- **Kill-switch:** stop Gluetun → `docker exec qbittorrent wget -T5 -qO- ifconfig.me` fails (no
  network). Restart Gluetun → connectivity returns.
- **Hardlink works:** after one import, `ls -i /data/media/torrents/movies/<f>` and
  `/data/media/movies/.../<f>` show the **same inode**.
- **Traefik routing:** `qbittorrent.home`, `radarr.home`, `sonarr.home`, `prowlarr.home`,
  `plex.home` all resolve and load.
- **End-to-end:** one real (legally-obtained) title added in Radarr → downloads → imports → appears
  in Plex → **plays on the TV via the Chromecast** (confirm Plex dashboard shows *Direct Play*, not
  *Transcode*, for a 1080p x264 file).

---

## 9. Risks & known gotchas

| Risk | Mitigation |
|---|---|
| Mullvad has **no port forwarding** | Accept lower seed connectivity; not a download blocker. Avoid ratio-enforced private trackers, or use a different provider if those are needed later. |
| env-manager `compose up` fails / old Docker CLI | Fallback: deploy the three arr apps via host `docker compose` with manual `my-macvlan-net` + Traefik labels (documented in `project_env_manager_quirks`). |
| macvlan host-isolation | Not needed for LAN-only streaming; all required paths are container↔container on the same macvlan. |
| Gluetun on macvlan + kill-switch blocking the UI | Set `FIREWALL_OUTBOUND_SUBNETS` to LAN + Docker subnets so UI/API stay reachable. |
| GPU passthrough not expressible in env-manager | Plex deliberately placed in the direct compose stack. |
| Single SSD fills up | Documented scaling step: add dedicated disk, keep torrents+library on one FS for hardlinks. |
| Traefik registers zero routers | Ensure Gluetun/Plex labels use the `my-macvlan-net` provider network (the recurring home-lab footgun). |

---

## 10. Out of scope (v1)

- Overseerr/request portal, Bazarr subtitles, Recyclarr profile sync, Tautulli stats.
- Remote / off-LAN streaming.
- Usenet.
- TLS/HTTPS for the internal `*.home` UIs (stack runs HTTP-only like the rest of the host).
