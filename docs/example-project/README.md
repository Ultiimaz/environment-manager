# Example project

A minimal `.dev/` directory you can drop into any repo to make it deployable
by env-manager. Three files:

```
.dev/
├── config.yaml                 # Project metadata, services, secrets, hooks
├── docker-compose.prod.yml     # Stack for the default branch
└── docker-compose.dev.yml      # Stack for every preview branch
```

## How env-manager picks them up

1. You add the repo via `envm projects onboard <git-url>` or the UI.
2. env-manager clones the repo and reads `.dev/config.yaml`.
3. The default branch (\`main\` / \`master\`) deploys with
   `docker-compose.prod.yml` to the project's prod URL.
4. Every other branch with a `.dev/` directory becomes a preview env at
   `<branch-slug>.<project>.<base-domain>`, deployed with
   `docker-compose.dev.yml`.
5. Push to the branch → rebuild. Delete the branch → teardown on the
   next webhook event.

## Secrets

`.dev/config.yaml#secrets` is a **list of names**, not values. Set the
real values in the credential store:

```bash
envm secrets set my-app SESSION_SIGNING_KEY=$(openssl rand -hex 32)
envm secrets set my-app STRIPE_API_KEY=sk_live_...
```

env-manager injects them as environment variables in the compose
runtime, so anywhere in the YAML you reference `${STRIPE_API_KEY}` it
gets the stored value.

## Per-env database

If `.dev/config.yaml#services.postgres` is `true`, env-manager
provisions a fresh Postgres database + dedicated role per environment
inside the singleton `paas-postgres` container. The compose stack
receives `DATABASE_URL` automatically — no extra config needed. Same
for `services.redis: true` and `REDIS_URL`.

When the environment is destroyed (branch deleted), the database and
role are dropped.

## Hooks

`pre_deploy` runs inside the freshly-built container before traffic
shifts. Non-zero exit aborts the deploy and the previous container
keeps serving — use this for migrations, schema checks, smoke tests
that absolutely have to pass before going live.

`post_deploy` runs after the traffic shift. Failures are logged, not
fatal — use this for cache warming, ping-the-monitoring-dashboard
side effects, anything you'd ideally do but won't roll back over.
