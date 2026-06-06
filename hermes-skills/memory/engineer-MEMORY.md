env-manager v2 (`http://manager.home/api/v1/`) is the home-lab control plane.
Auth: `Bearer $ENVM_TOKEN`. Onboard: `POST /projects {repo_url}`. Build
polls at `GET /envs/{id}/builds`, log at `GET /builds/{id}/log`. Repo needs
a `.dev/` dir (config.yaml + Dockerfile.dev + docker-compose.{prod,dev}.yml).
Compose `context: .` (not `..`) — env-manager runs with --project-directory
at the repo root.
§
env-manager AUTO-INJECTS `DATABASE_URL` when `services.postgres: true` and
`REDIS_URL` when `services.redis: true` in `.dev/config.yaml`. NEVER ask
the user for these. Postgres 15+: per-env user can't CREATE in `public` —
apps must `CREATE SCHEMA IF NOT EXISTS <user>` and set search_path.
§
Tool paths: `gh` at `/opt/data/.local/bin/gh` (uses $GITHUB_TOKEN — full
scopes), `claude` v2.1+ at `/opt/data/.npm-global/bin/claude` for delegated
heavy work. Stitch design via native MCP: `mcp_stitch_create_project`,
`mcp_stitch_generate_screen_from_text` — generation can take 5-10 min, the
tool times out; poll `mcp_stitch_list_screens` afterwards.
§
Kanban (source of truth) at http://kanban.home — single board, lanes
backlog/todo/doing/done, free-form tags. For any "build me X": create
or pick up a ticket FIRST. Required for done: user_story,
acceptance_criteria, test_plan (server returns 422 otherwise). Link
related tickets via POST /api/v1/tickets/{id}/links {to_id,relation}
where relation ∈ blocks/relates_to/duplicates. The reverse view (e.g.
"blocked by") is auto-derived; only file the forward edge.
§
Default deploy target: live customer sites (blocksweb.nl, sellis2,
rocketgrowth, dashboard) → Vercel/Laravel. Everything else (tools,
experiments, "build me X") → env-manager (home-lab, *.home).
§
Hard rules: Ultiimaz/* only, NEVER BTC-Direct/*. Never DELETE projects
or destroy envs without explicit user approval. Never use emojis or
emoticons — TTS reads them as alt-text and it sounds infantile.
Hermes is turn-based — tool calls fire in the SAME response as
confirmation, never split. "Starting now" with no tool call = silent
failure. Skills: ship-a-new-product, blocksweb-homelab-deploy,
blocksweb-engineer-role.
