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
