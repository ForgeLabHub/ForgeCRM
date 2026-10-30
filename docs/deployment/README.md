# Deployment

ForgeCRM deploys as a small set of containers. This directory documents a single-host
deployment with Docker Compose, which is the configuration that has been verified.

## Components

| Service | Purpose | Required |
| --- | --- | --- |
| `postgres` | PostgreSQL 17, all persistent state | yes |
| `api` | HTTP API, applies migrations on start | yes |
| `worker` | Background jobs and event dispatch | yes |
| `nginx` | Reverse proxy, TLS termination | optional, profile `proxy` |

The API and worker share one image; only the entrypoint differs. Redis, a message
broker and object storage are **not** used. The job queue, the outbox and event
delivery are all backed by PostgreSQL, which keeps the operational surface small. Add a
broker only when measured throughput requires it.

## First deployment

```bash
cp .env.example .env

# Required: the key that encrypts stored integration credentials.
printf 'FORGECRM_SECRET_KEY=%s\n' "$(openssl rand -base64 48)" >> .env

./scripts/check-env.sh          # verify the host before starting anything
docker compose up -d

FORGECRM_BOOTSTRAP_PASSWORD='<a strong password>' \
  docker compose run --rm --no-deps -e FORGECRM_BOOTSTRAP_PASSWORD \
  api -bootstrap-admin -bootstrap-email you@example.com

./scripts/smoke-test.sh http://localhost:8080
```

`scripts/setup.sh` performs these steps and is safe to re-run: it will not overwrite an
existing `.env` and will not create a second administrator.

## Configuration

All settings come from the environment. See [`.env.example`](../../.env.example) for the
full annotated list. The settings that matter most:

| Variable | Why it matters |
| --- | --- |
| `FORGECRM_ENV` | `development` returns password reset tokens in API responses. Use `production`. |
| `FORGECRM_SECRET_KEY` | Encrypts integration credentials. Losing it makes them unrecoverable. |
| `FORGECRM_BASE_URL` | Used in email links. Must be the public URL. |
| `FORGECRM_COOKIE_SECURE` | Must be `true` when serving over HTTPS. |
| `FORGECRM_METRICS_TOKEN` | Protects `/metrics`. Set it before exposing the port. |
| `FORGECRM_IMPORT_MAX_UPLOAD_BYTES` | Must match `client_max_body_size` in the proxy. |

## Migrations

The API applies pending migrations at start when `FORGECRM_MIGRATE_ON_START=true`
(the default). Migrations are idempotent: a second run reports no work.

```bash
./scripts/migrate.sh          # apply pending migrations
./scripts/migrate.sh status   # show the current version
```

Apply migrations before starting a new API version, and never run two versions with
different migration sets against the same database at once. The Compose file enforces
this by making the worker wait for the API to be healthy.

## Production checklist

- [ ] `FORGECRM_ENV=production`
- [ ] `FORGECRM_BASE_URL` is the public HTTPS URL
- [ ] `FORGECRM_COOKIE_SECURE=true`, TLS terminated at the proxy
- [ ] A unique `FORGECRM_SECRET_KEY`, stored in a secret manager, not in the image
- [ ] `FORGECRM_METRICS_TOKEN` set
- [ ] The database port is not published
- [ ] Backups configured and a restore tested — see [../operations/](../operations/)
- [ ] Resource limits set on the API and worker containers

## Scaling

- The **API** is stateless. Run several replicas behind the proxy.
- The **worker** is safe to run concurrently: jobs are claimed with `FOR UPDATE SKIP
  LOCKED`, so two workers never take the same job. Set `FORGECRM_WORKER_CONCURRENCY` per
  replica rather than scaling the replica count first.
- **Rate limiting is per process.** With multiple API replicas, a limit applies per
  replica, not globally. Move to a shared store before relying on it for protection.

## Other hosts

- **Platforms that build the repository root** (blitz.cloud and similar) use the
  root `Dockerfile`, which builds the API, the worker and the web interface into
  one image, and the root `docker-compose.yml`, whose two parts (`api`, `worker`)
  run that image with different entrypoints. See
  [blitz-cloud.md](blitz-cloud.md) for the free-tier walkthrough.

## Verified deployment

The stack in this repository was brought up and tested end to end: four containers
started, health checks passed, migrations reached version 5, an administrator was
bootstrapped, and the smoke test reported 9 of 9 checks passing, including a full
archive import executed by the worker.

The root build file was verified separately: the image built from the repository
root, migrations reached version 25, `/health` and `/ready` answered 200, the web
interface loaded at `/` and survived a refresh on a client-side route, and an
administrator signed in through the browser.
