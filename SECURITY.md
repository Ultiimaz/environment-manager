# Security policy

## Reporting a vulnerability

Please **do not** open public GitHub issues for security problems.

Email <s.tjeerdsma@btcdirect.eu> with:

- A description of the issue and the worst-case impact you can demonstrate.
- A minimal reproduction (steps, payload, or PoC) — locally is fine.
- The commit SHA or release tag the issue was found against.
- Whether you intend to publish a write-up, and if so on what timeline.

You should expect an acknowledgement within 72 hours and a remediation
plan within 14 days for issues that affect a current release.

Coordinated disclosure is appreciated. If you publish before a fix
ships, please redact details that make exploitation trivial against
existing deployments.

## Supported versions

Only the most recent `master` build receives security fixes. There is
no LTS branch. Customers running pinned releases should expect to
upgrade within 14 days of a security release.

## Hardening checklist (for operators)

When deploying env-manager outside a trusted homelab LAN:

- Set `LAB_MODE=false` so Bearer auth applies to read endpoints and
  WebSocket log streams as well as writes.
- Keep `BASE_DOMAIN` set to the real domain — it locks the CORS and
  WebSocket origin allow-list.
- Run behind TLS (Traefik handles this when `LETSENCRYPT_EMAIL` is set).
- Persist `CREDENTIAL_KEY` in a real secret manager — losing it makes
  every stored repository token unrecoverable.
- Schedule `envm backup` offsite. The archive contains the encrypted
  credential store, so possession alone doesn't expose secrets, but
  treat it as sensitive.
- Restrict the GitHub webhook secret to the env-manager instance's
  ingress only; the webhook handler verifies HMAC, but a leaked
  secret is still bad.

## Known scope

The following are **not** security boundaries today; rely on network
isolation if you need them to be:

- Project-level isolation: every project's container shares the host
  Docker daemon. A malicious `.dev/docker-compose.yml` can break out
  of its project. Only onboard repos you trust.
- Build-time isolation: the builder shells out to `docker build` on
  the host. A malicious Dockerfile has the same blast radius as the
  daemon.
- Runtime exec: `pre_deploy` / `post_deploy` hooks run inside the app
  container. They have no extra sandboxing.
