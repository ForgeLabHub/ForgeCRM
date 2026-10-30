# Deploying ForgeCRM on blitz.cloud (Free Tier)

This guide documents deploying a production-ready ForgeCRM instance on blitz.cloud's free tier without requiring a credit card.

## Platform Architecture on blitz.cloud

ForgeCRM runs as two distinct application parts sharing the same container build:

1. **`api`**:
   - Compiles Go API server and embeds React web interface.
   - Binds to `0.0.0.0:${PORT}`.
   - Answers HTTP health check at `/health` and readiness probe at `/ready`.
   - Runs database migrations once on startup (`FORGECRM_MIGRATE_ON_START=true`).
   - Serves static assets and SPA routes on public HTTP endpoint.

2. **`worker`**:
   - Runs background job process (`forgecrm-worker`) without a public HTTP endpoint.
   - Waits for database readiness and schema existence before starting queue loops.
   - Executes background imports/exports, outbox event dispatches, webhook deliveries, workflow scheduler, and retention maintenance.
   - Handles graceful shutdown on `SIGTERM`.

3. **`PostgreSQL 17`**:
   - Provisioned via blitz.cloud Managed Database or an external free PostgreSQL 17 database.
   - Connected using `DATABASE_URL` or `FORGECRM_DATABASE_URL`.

## Deployment Instructions

### 1. Repository Setup
1. Fork or push your branch (`deploy/blitz-cloud`) to GitHub.
2. In blitz.cloud Dashboard, connect GitHub or paste your repository URL.

### 2. Managed Database Provisioning
1. In blitz.cloud Panel, go to **Databases** -> **Create Database**.
2. Select **PostgreSQL 17**.
3. Copy the database connection URL (`DATABASE_URL`).

### 3. Environment Variables

Configure the following environment variables in the blitz.cloud panel:

| Variable | Recommended Value / Description | Required |
| --- | --- | --- |
| `FORGECRM_ENV` | `production` | Yes |
| `DATABASE_URL` | `postgres://user:password@host:5432/dbname?sslmode=require` | Yes |
| `FORGECRM_SECRET_KEY` | Random 48+ char secret (e.g. `openssl rand -base64 48`) | Yes |
| `FORGECRM_BASE_URL` | `https://your-app.blitz.cloud` | Yes |
| `FORGECRM_MIGRATE_ON_START` | `true` (for `api` service) | Yes |
| `FORGECRM_DATABASE_MAX_CONNS` | `6` for `api`, `4` for `worker` | Yes |
| `FORGECRM_WORKER_CONCURRENCY` | `2` (for `worker` service) | Yes |
| `FORGECRM_RATE_LIMIT_STORE` | `memory` | Yes |

### 4. Creating the First Administrator

Run the one-off admin bootstrap command via terminal or CLI:

```bash
# Set FORGECRM_BOOTSTRAP_PASSWORD and run bootstrap command on the api container:
forgecrm-api -bootstrap-admin -bootstrap-email admin@example.com
```

### 5. Free Tier Considerations & Sleep/Wake Behavior

- **Sleep / Wake**:
  - The `api` part sleeps when inactive and wakes automatically on incoming HTTP requests.
  - The `worker` background process runs when active. On the free plan, background services pause after 2 days of account inactivity. Signing into the dashboard or pushing code resets the timer.
  - Outbox events, scheduled workflows, and background jobs are persisted safely in PostgreSQL 17 and resume seamlessly when the service wakes up.
