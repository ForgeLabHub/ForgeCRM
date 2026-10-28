# Operations

## Health and readiness

| Endpoint | Meaning |
| --- | --- |
| `GET /health` | The process is alive. Does not touch the database. |
| `GET /ready` | The database is reachable. Returns 503 otherwise. |
| `GET /metrics` | Prometheus metrics. Protect with `FORGECRM_METRICS_TOKEN`. |

Use `/health` for a liveness probe and `/ready` for a readiness probe, so a database
outage removes an instance from rotation instead of restarting it.

## Backup

```bash
./scripts/backup.sh ./backups
```

This produces a consistent custom-format `pg_dump` (taken with
`--serializable-deferrable`, so it reflects one point in time while the API keeps
serving), a tarball of the import data volume, and a SHA-256 checksum file.

Back up before every upgrade and on a schedule that matches how much data you are
willing to lose. The database dump alone is not a complete backup: uploaded files live
in the `importdata` volume.

## Restore

```bash
./scripts/restore.sh ./backups/forgecrm-db-20260101T000000Z.dump --yes-really
```

The script verifies the checksum, stops the API and worker so nothing writes during the
restore, recreates the database, restores, and restarts the services. It refuses to run
without `--yes-really` because it destroys current data.

Test a restore on a throwaway instance before you need one. An untested backup is not a
backup.

## Troubleshooting

### The API starts but `/ready` returns 503

The database is unreachable. Check that the `postgres` service is healthy and that
`FORGECRM_DATABASE_URL` points at it. `docker compose logs postgres` shows why.

### Imports stay in `queued` and never run

No worker is consuming jobs. Check `docker compose logs worker`. If the worker is
running, look at `GET /api/v1/jobs` for a job stuck in `running`: a worker that died
mid-job leaves it claimed until the visibility timeout expires.

### An import fails with `unsupported tabular format`

The format could not be determined from either the file name or the content. Confirm
the file is one of the supported formats and that its name has a recognisable
extension. For archives, the error names the entry that could not be parsed.

### An archive is rejected

The message says why. `compression ratio exceeds the limit` means the file looks like an
archive bomb; raise `FORGECRM_IMPORT_MAX_RATIO` only if you trust the source.
`escapes the extraction root` or `is absolute` means the archive contains a path
traversal attempt and was correctly refused. `file is not valid UTF-8` means the data
file is not UTF-8; re-export it.

### Login fails with the correct password

After too many failed attempts the account is locked. The lock clears after
`FORGECRM_LOCKOUT_WINDOW`, or an administrator can unblock the user. A blocked user's
sessions are revoked immediately.

### Resetting a password when email is not configured

With `FORGECRM_SMTP_ENABLED=false` and `FORGECRM_ENV=development`, the invite response
includes a `reset_token`. With `FORGECRM_ENV=production` it does not, and an
administrator must configure SMTP or reset the password another way.

## Logs

Logs are structured JSON with `debug`, `info`, `warn` and `error` levels, and carry a
request correlation identifier. Set `FORGECRM_LOG_FORMAT=text` for local reading.

Credentials, tokens, keys and full personal data are never logged. Import logs record
the source file, its checksum and the processing configuration, not the row contents.

The administration API exposes filtered views of application logs, the audit log,
security events, job history, integration runs and webhook deliveries at
`/api/v1/audit`, `/api/v1/security-events` and `/api/v1/admin/logs`. These require the
corresponding permissions.

## Webhook delivery

A subscription receives every matching event. Delivery is **at-least-once**, not
exactly-once: a worker that dies after the endpoint accepted the POST but before the row
was marked delivered leaves the claim to expire and the delivery is sent again. Every
payload therefore carries a stable `event_id` for the receiver to de-duplicate on.

A delivery that keeps failing is retried with exponential backoff up to the
subscription's `max_attempts`, then moves to the terminal `dead` state. Dead is visible,
not silent:

- the transition writes one `webhooks.delivery_dead_lettered` audit entry, once per
  transition, so an alert built on it does not repeat for a single failure;
- `dead_lettered_at` records when it happened;
- `GET /api/v1/admin/metrics` reports three counters.

| Counter | Meaning | Alert on it? |
| --- | --- | --- |
| `webhook_deliveries_dead_total` | Deliveries in the terminal dead state right now. Falls when a delivery is replayed or swept. | No — it is a level, not an event. |
| `webhook_deliveries_dead_lettered_total` | Transitions into the dead state, ever. Read from the audit trail, so it only rises. | Yes — a rate over this is the signal. |
| `webhook_deliveries_pending_total` | Deliveries still eligible for an attempt. | If it grows without draining. |

All three are read from the database rather than a per-process counter, so they are
correct when several workers and API replicas run at once. None is labelled: a tenant id
or target URL in a metric label would multiply the series without bound and leak a
customer's endpoint into the monitoring system. The per-organization view is in the audit
page.

### Replaying a delivery

`POST /api/v1/webhooks/{id}/deliveries/{deliveryID}/replay` re-arms a delivery. It
resets the per-window attempt count, which grants a fresh retry budget, and increments
`replay_count` — never reset — so an operator can tell a delivery that has just died from
one that has already exhausted several budgets. The dead-letter marker is cleared and the
replay is audited. A delivery that is not this organization's, or that is already pending,
is reported as not found, so an id from another organization is not distinguishable from
one that does not exist.

## Rate limiting

Requests to `/api/v1` are limited per client address. Authentication endpoints
have their own, tighter budget so that password guessing is bounded separately
from ordinary API traffic. Both are configurable:

| Setting | Default | Meaning |
|---|---|---|
| `FORGECRM_RATE_LIMIT_PER_MIN` | 300 | API requests per client address per minute |
| `FORGECRM_LOGIN_RATE_LIMIT_PER_MIN` | 60 | Authentication attempts per client address per minute |

A refusal is `429` with `Retry-After`, `RateLimit-Limit`, `RateLimit-Remaining`
and `RateLimit-Reset`. The older `X-RateLimit-*` names are sent as well.

### Choosing where the counters live

`FORGECRM_RATE_LIMIT_STORE` decides:

- `memory` (default) keeps counters in the API process. Each replica then
  enforces its own budget, so **N replicas grant N times the configured limit**.
  This is correct for the default single-container deployment.
- `redis` shares counters across replicas, so the configured value is the actual
  budget. It requires `FORGECRM_REDIS_URL`, for example
  `redis://:password@redis:6379/0`.

```bash
# Start the optional Redis service and point the API at it.
docker compose --profile redis up -d
# then in .env:
#   FORGECRM_RATE_LIMIT_STORE=redis
#   FORGECRM_REDIS_URL=redis://redis:6379/0
```

The API refuses to start when `redis` is selected but the server cannot be
reached. That is deliberate: starting anyway would leave the limit unenforced and
report it only as a per-request warning.

### Behaviour when the counter store is unavailable

The two policies fail in opposite directions, because they protect different
things:

- **General API traffic fails open.** A request is allowed and the decision is
  marked degraded. A broken counter must not take the whole API down.
- **Authentication fails closed.** A request is refused with `503` and code
  `unavailable`. If the counter cannot be read, there is no barrier to password
  guessing.

Both cases log a warning and are visible in the metrics; neither is reported as a
`429`, because a `429` tells the client to slow down when the truth is that the
server cannot count.

### Client address behind a proxy

By default the client address is the connection address, and forwarding headers
are ignored. Set `FORGECRM_TRUSTED_PROXIES` to a comma-separated list of the
proxies in front of the API:

```
FORGECRM_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16
```

This is a security setting, not a convenience. `X-Forwarded-For` is a request
header that any client can set. When the connection does not come from a listed
proxy, the header is ignored, which is what stops a caller from moving itself
into a fresh rate-limit bucket on every request or forging its entry in the
authentication log.

If the API is reachable only through the proxy, `*` trusts every peer. That
accepts the header from anyone and lets a caller choose its own address, so the
API logs a warning at startup when it is set. The proxy chain is read
right-to-left, skipping addresses in the trusted list; the first untrusted entry
is the client, because that is the address the last trusted hop observed and the
client cannot have written it.

### Troubleshooting

**A limit is configured but a client is not being refused.** Check which store is
in use in the startup log (`rate limiter configured`). With `memory` and several
replicas, the effective limit is the configured value times the replica count.

**Every client shares one bucket.** The proxy is not in
`FORGECRM_TRUSTED_PROXIES`, so all requests are attributed to the proxy's
address. Add the proxy's network to the list.

**A `503` with code `unavailable` appears on a sign-in.** The counter store is
unreachable and authentication is failing closed. This is not a rate limit: no
budget was consumed and `Retry-After` is absent. Check that Redis is up and that
`FORGECRM_REDIS_URL` points at it. The warning `rate limit store unavailable;
policy fail mode applied` names the policy.

**General traffic is served but the limit is not enforced.** The counter store is
unreachable and general traffic is failing open. The same warning is logged. This
is the intended degradation, but it is worth an alert: while it lasts, the limit
exists in configuration only.

## Verifying multi-replica operation

A single-replica deployment cannot tell a shared counter from a per-process one,
because there is only one process. The rehearsal below builds the topology that
can: two API containers behind a real nginx, sharing one PostgreSQL and one
Redis.

```bash
./scripts/multi-replica-verify.sh          # tear the stack down afterwards
./scripts/multi-replica-verify.sh --keep   # leave it up to inspect
```

It uses a separate compose project, separate ports (18080-18082) and a separate
volume, so it cannot touch the default stack. It checks, and fails loudly on:

1. every container reaches `healthy`, and both replicas answer `/ready`;
2. a session created on one replica is accepted by the other, and logout and
   revocation on one take effect on the other;
3. the login budget is a property of the deployment, not of a replica — a budget
   of 2 is 2 attempts across the pair, not 2 per replica;
4. a direct client cannot open its own bucket by sending `X-Forwarded-For`, and
   rotating the header through the proxy does not help either;
5. with Redis down, sign-in returns `503 unavailable` (fail-closed) while
   ordinary API traffic keeps being served (fail-open), and both recover;
6. stopping one replica leaves the other serving, and the counters written before
   a restart are still in force afterwards.

The same properties are asserted at the code level, without containers, by the
integration tests in `internal/api/cross_replica_test.go` and
`internal/api/session_sharing_test.go`, and the lockout counter by
`internal/authn/lockout_integration_test.go`. Those run in CI against a real
PostgreSQL and Redis and are the faster signal; the script is what proves the
deployment wiring.

### Running a production replica set

The rehearsal's topology is the shape to copy, with three settings that must
match across replicas:

- `FORGECRM_SECRET_KEY` must be identical, or a session or encrypted value
  written by one replica cannot be read by another.
- `FORGECRM_RATE_LIMIT_STORE=redis` with a shared `FORGECRM_REDIS_URL`, or each
  replica enforces its own budget.
- `FORGECRM_TRUSTED_PROXIES` must name the load balancer's addresses, or every
  request is attributed to the balancer and one client's traffic counts against
  another's budget.

`FORGECRM_MIGRATE_ON_START` may be left on for every replica: migrations are
advisory-locked, so concurrent starts serialise rather than race. Leaving it on
for one replica and off for the rest, as the rehearsal does, is only to keep the
startup log unambiguous.
