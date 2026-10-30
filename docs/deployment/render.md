# Deploying ForgeCRM on Render

This guide deploys a working ForgeCRM (API + background worker + web interface) on
Render using the Blueprint in `render.yaml` at the repository root.

## What runs where

The root `Dockerfile` produces one image with three things:

1. **API** (`forgecrm-api`) — the HTTP server, and the web interface it serves.
2. **Worker** (`forgecrm-worker`) — the background process: import/export jobs,
   outbox dispatch, webhook deliveries, the workflow scheduler and retention.
3. **Web interface** — the built React single-page app, served by the API from
   `/app/public`. One address answers both the API and the UI, same-origin, so the
   session cookie and the CSRF double-submit work without CORS.

`render.yaml` declares two services that build that same image and run a different
entrypoint, plus one managed PostgreSQL database. There is no separate static site:
the API serves the interface, which keeps the two on one origin.

## Deploying

1. Push this branch (or merge it) and open the Render Dashboard.
2. **New → Blueprint**, then pick the repository. Render reads `render.yaml`.
3. Accept the plan it proposes: `forgecrm-api` (web), `forgecrm-worker` (worker)
   and the `forgecrm-db` PostgreSQL database.
4. Render injects `PORT` and `FORGECRM_DATABASE_URL` for the database. The
   `FORGECRM_SECRET_KEY` is generated once on the web service and shared with the
   worker.

## Environment variables

Set these on the **forgecrm-api** service. Values marked *injected* are provided by
Render and must not be set by hand.

| Variable | Value | Notes |
| --- | --- | --- |
| `FORGECRM_ENV` | `production` | Required. |
| `FORGECRM_DATABASE_URL` | *injected* | The managed PostgreSQL connection string. The app reads `FORGECRM_DATABASE_URL` first and falls back to `DATABASE_URL`. |
| `FORGECRM_SECRET_KEY` | `generateValue: true` | Required. Encrypts stored integration credentials; losing it makes them unrecoverable. |
| `FORGECRM_BASE_URL` | `https://your-app.onrender.com` | Required. Used for absolute links. |
| `FORGECRM_MIGRATE_ON_START` | `true` | Set on the **forgecrm-api** service. The API applies migrations before serving. |
| `PORT` | *injected* | Render sets it; the API binds `0.0.0.0:$PORT`. |
| `FORGECRM_DATABASE_MAX_CONNS` | `6` (api), `4` (worker) | Keep pools small on the starter plan. |
| `FORGECRM_RATE_LIMIT_STORE` | `memory` | Single replica, so the in-process limiter is correct. |

The worker waits for the schema the API applies before it starts claiming work.

## Creating the first administrator

The API has a one-off bootstrap command. In the Render Dashboard, open the
**forgecrm-api** service's **Shell** and run:

```sh
FORGECRM_BOOTSTRAP_PASSWORD='choose-a-strong-password' \
  forgecrm-api -bootstrap-admin -bootstrap-email you@example.com
```

The password is passed through the environment so it never lands in the process
list. Sign in afterwards, then create the rest of the team from the admin pages.

## Verifying the deployment

```sh
curl -fsS https://your-app.onrender.com/health   # -> {"status":"ok"}
curl -fsS https://your-app.onrender.com/ready    # -> {"status":"ready"}
curl -fsS -o /dev/null -w '%{http_code}\n' \
  https://your-app.onrender.com/contacts         # -> 200 (SPA deep route)
```

## Notes

- The Blueprint sets `FORGECRM_LOG_FORMAT=json`, which suits Render's log viewer.
- If you deploy more than one API instance, set `FORGECRM_RATE_LIMIT_STORE=redis`
  and `FORGECRM_REDIS_URL`, or the limit applies per instance.
