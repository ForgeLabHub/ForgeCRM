# Deploying ForgeCRM on blitz.cloud

This guide deploys a working ForgeCRM (API + background worker + web interface)
on blitz.cloud's free tier, and on any host that builds a repository root.

## What runs where

ForgeCRM is a single container build that contains three things:

1. **API** (`forgecrm-api`) — the HTTP server, and the web interface it serves.
2. **Worker** (`forgecrm-worker`) — the background process: import/export jobs,
   outbox dispatch, webhook deliveries, the workflow scheduler and retention.
3. **Web interface** — the built React single-page app, served by the API from
   `/app/public`. One address answers both the API and the UI, same-origin, so
   the session cookie and the CSRF double-submit work without CORS.

The image is built by the **root `Dockerfile`**. The root `docker-compose.yml`
declares two parts, `api` and `worker`, which build that same image and run a
different entrypoint. blitz.cloud reads the compose file, proposes the two parts
and runs them as one app.

## Deploying from GitHub

1. Push this branch (or merge it) and connect the repository on
   `dashboard.blitz.cloud` under **Host something new → My own code**.
2. blitz.cloud finds the root `Dockerfile` and the compose file. Accept the two
   parts it proposes: `api` (public, at `/`) and `worker` (background).
3. Create a **PostgreSQL 17** database on the **Databases** page and connect it to
   the app. blitz.cloud then sets `DATABASE_URL` for you.

## Environment variables

Set these on the app's **Environment** tab. Values marked *injected* are provided
by blitz.cloud and must not be set by hand.

| Variable | Value | Notes |
| --- | --- | --- |
| `FORGECRM_ENV` | `production` | Required. |
| `DATABASE_URL` | *injected* | The managed PostgreSQL connection string. The app reads `FORGECRM_DATABASE_URL` first and falls back to `DATABASE_URL`. |
| `FORGECRM_SECRET_KEY` | `openssl rand -base64 48` | Required. Encrypts stored integration credentials; losing it makes them unrecoverable. |
| `FORGECRM_BASE_URL` | `https://your-app.<you>.blitz.cloud` | Required. Used for absolute links. |
| `FORGECRM_MIGRATE_ON_START` | `true` | Set on the **api** part. The API applies migrations before serving. |
| `PORT` | *injected* | blitz.cloud sets it; the API binds `0.0.0.0:$PORT`. |
| `FORGECRM_DATABASE_MAX_CONNS` | `6` (api), `4` (worker) | Free-tier memory is 512 MB; keep pools small. |
| `FORGECRM_RATE_LIMIT_STORE` | `memory` | Single replica, so the in-process limiter is correct. |

The API binds to `PORT` when blitz.cloud sets it, otherwise to
`FORGECRM_HTTP_ADDR` (default `:8080`). The worker waits for the schema the API
applies before it starts claiming work.

## Creating the first administrator

The API has a one-off bootstrap command. On blitz.cloud, open the **api** part's
terminal (or run a one-off command) and run:

```sh
FORGECRM_BOOTSTRAP_PASSWORD='choose-a-strong-password' \
  forgecrm-api -bootstrap-admin -bootstrap-email you@example.com
```

The password is passed through the environment so it never lands in the process
list. Sign in afterwards, then create the rest of the team from the admin pages.

## Free-tier behaviour

- The **api** part sleeps after 15 minutes without visitors and wakes on the next
  request. The first request after a sleep is slower.
- The **worker** part runs in the background and does not sleep while the account
  is active. On the free plan it stops when nothing happens in the account for two
  days (no sign-in, deploy or dashboard visit); press **Start** on the app's page
  to resume. Nothing is lost: jobs, outbox events and scheduled workflows live in
  PostgreSQL and resume where they left off.
- One managed database is included on the free plan, backed up nightly.

## Verifying the deployment

```sh
curl -fsS https://your-app.<you>.blitz.cloud/health   # -> {"status":"ok"}
curl -fsS https://your-app.<you>.blitz.cloud/ready    # -> {"status":"ready"}
curl -fsS -o /dev/null -w '%{http_code}\n' \
  https://your-app.<you>.blitz.cloud/                 # -> 200 (the web interface)
```

`/health` is liveness (the process is up), `/ready` is readiness (the database is
reachable). An unknown `/api/...` path stays a JSON 404; any other path returns the
interface, so client-side routes survive a refresh.
