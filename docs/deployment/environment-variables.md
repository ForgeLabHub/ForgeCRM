# Environment variables

Every setting ForgeCRM reads from the environment, where it is read, and what
happens when it is unset.

The values below were taken from the code, not from memory: each row was checked
against the call that reads it (`internal/config/config.go` for the API and
worker, `internal/api/server.go` for `FORGECRM_STATIC_DIR`, and the Docker and
Compose files for the build). A variable that no code reads is listed in
[Declared but unused](#declared-but-unused) rather than documented as if it did
something.

## Conventions

- **Component** is the process that reads the variable. `API` means
  `forgecrm-api`; `Worker` means `forgecrm-worker`; `Migration` means the
  `forgecrm-api -migrate` run or the migration step at startup; `Frontend` means
  the Vite build (a compile-time value baked into the bundle); `Compose` and
  `Docker` mean the build and orchestration files.
- **Default** is the value the code uses when the variable is unset. "none"
  means unset is meaningful (for example, an optional subsystem stays off).
- **Secret** marks a value that must never be logged, committed, or echoed.
- **Production** marks a variable that a production deployment must set, or whose
  default is unsafe in production.

The API logs which settings are present at startup through `cfg.Redacted()`.
Secrets are reported as `present` or `absent`, never printed.

## Core

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_ENV` | API, Worker | no | `development` | Selects the environment. `production` turns on the hardening checks below and disables dev conveniences such as returning password-reset tokens in API responses. | `production` | no | yes |
| `FORGECRM_HTTP_ADDR` | API | no | `:8080` | Address the API listens on. `PORT` overrides it when set. | `:8080` | no | no |
| `PORT` | API | no | none | Address the API listens on, supplied by some platforms (Render, Heroku). Wins over `FORGECRM_HTTP_ADDR`; the host is forced to `0.0.0.0`. | `10000` | no | no |
| `FORGECRM_BASE_URL` | API | yes in production | `http://localhost:8080` | Public origin, used to build links (invitations, password reset, OIDC redirect fallback). Must be `https://` in production. | `https://crm.example.com` | no | yes |
| `FORGECRM_LOG_LEVEL` | API, Worker | no | `info` | Minimum log level: `debug`, `info`, `warn`, `error`. | `info` | no | no |
| `FORGECRM_LOG_FORMAT` | API, Worker | no | `json` | Log encoding: `json` or `text`. Use `json` when a collector parses it. | `json` | no | no |
| `FORGECRM_MIGRATE_ON_START` | API | no | `false` | Apply pending migrations before serving. The API also verifies the schema before it listens, so a failure here stops the process rather than serving broken requests. | `true` | no | recommended |
| `FORGECRM_STATIC_DIR` | API | no | `public` | Directory holding the built web interface. The root Dockerfile copies the bundle to `/app/public` and sets this. When the directory is absent the API serves API routes only. | `/app/public` | no | no |
| `FORGECRM_VERSION` | API, Worker | no | `0.1.0-dev` | Fallback build version for hosts that do not pass linker flags. Used only when the value was not set at build time. | `1.4.2` | no | recommended |
| `FORGECRM_COMMIT` | API, Worker | no | `unknown` | Fallback commit SHA, same fallback rule as `FORGECRM_VERSION`. Reported by `/ready` and the startup log. | `a1b2c3d` | no | recommended |

## Database

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_DATABASE_URL` | API, Worker, Migration | yes | none | PostgreSQL connection string. `DATABASE_URL` is used when this is unset. | `postgres://user:pass@db:5432/forgecrm?sslmode=require` | yes | yes |
| `DATABASE_URL` | API, Worker, Migration | no | none | Fallback name for the connection string, for platforms that inject it under this name. | `postgres://...` | yes | no |
| `FORGECRM_DATABASE_MAX_CONNS` | API, Worker | no | `16` | Connections one process may hold. The number that matters is replicas x this value and must stay below the server's `max_connections`. Values above 25 are refused at startup. | `8` | no | no |
| `POSTGRES_USER` | Compose | no | `forgecrm` | Database user the Compose `postgres` service creates. Not read by the API. | `forgecrm` | no | no |
| `POSTGRES_PASSWORD` | Compose | no | `forgecrm` | Database password the Compose `postgres` service creates. | `change-me` | yes | yes |
| `POSTGRES_DB` | Compose | no | `forgecrm` | Database the Compose `postgres` service creates. | `forgecrm` | no | no |

## Bootstrap administrator

The first platform administrator is created by an explicit command, not on every
start. See `docs/deployment/README.md` for the operator procedure. The bootstrap
is idempotent and refuses to overwrite an existing owner.

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_BOOTSTRAP_ADMIN_EMAIL` | API | only for the bootstrap command | none | Address of the first owner. Must be paired with a password. | `owner@example.com` | no | yes |
| `FORGECRM_BOOTSTRAP_ADMIN_PASSWORD` | API | only for the bootstrap command | none | Password for the first owner, hashed with the project's password hasher. Never logged. | a long random string | yes | yes |
| `FORGECRM_BOOTSTRAP_PASSWORD` | API | no | none | Older name for the password, still accepted so an existing deploy script keeps working. The new name wins when both are set. | a long random string | yes | no |

## Secrets

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_SECRET_KEY` | API, Worker | yes in production | random in development | Key material that encrypts stored integration credentials and derives CSRF tokens. Losing it makes stored integration secrets unrecoverable. Must be at least 32 characters in production. | `openssl rand -base64 48` | yes | yes |
| `FORGECRM_METRICS_TOKEN` | API | no | none | Bearer token protecting `/metrics`. When empty, metrics are served only to authenticated platform admins. | a random string | yes | no |

## Sessions, cookies and rate limiting

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_SESSION_TTL` | API | no | `12h` | Session lifetime. | `12h` | no | no |
| `FORGECRM_SESSION_COOKIE` | API | no | `forgecrm_session` | Session cookie name. | `forgecrm_session` | no | no |
| `FORGECRM_COOKIE_DOMAIN` | API | no | none | Cookie domain, for sharing a session across subdomains. Empty means host-only. | `.example.com` | no | no |
| `FORGECRM_COOKIE_SECURE` | API | yes in production | `true` in production | Marks the session cookie `Secure`. Must be `true` in production. | `true` | no | yes |
| `FORGECRM_CSRF_HEADER` | API | no | `X-CSRF-Token` | Header carrying the CSRF double-submit token. | `X-CSRF-Token` | no | no |
| `FORGECRM_RATE_LIMIT_PER_MIN` | API | no | `300` | General requests allowed per client per minute. | `300` | no | no |
| `FORGECRM_LOGIN_RATE_LIMIT_PER_MIN` | API | no | `60` | Sign-in attempts allowed per client IP per minute. Raise it when many users share one address. | `60` | no | no |
| `FORGECRM_LOCKOUT_THRESHOLD` | API | no | `8` | Failed sign-ins before an account is locked. | `8` | no | no |
| `FORGECRM_LOCKOUT_WINDOW` | API | no | `15m` | Window over which failures are counted. | `15m` | no | no |
| `FORGECRM_RATE_LIMIT_STORE` | API | no | `memory` | `memory` keeps counters in the process (per replica); `redis` shares them across replicas. `redis` requires `FORGECRM_REDIS_URL` and the API refuses to start if the server is unreachable. | `memory` | no | no |
| `FORGECRM_REDIS_URL` | API | only with the redis store | none | Redis connection string for the shared limiter. | `redis://:pass@redis:6379/0` | yes | no |
| `FORGECRM_TRUSTED_PROXIES` | API | no | none | Comma-separated CIDRs of proxies whose forwarding headers may be believed. Empty means every request is attributed to its connection address. Do not use `*`. | `10.0.0.0/8,172.16.0.0/12` | no | recommended |

## Import Center

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_IMPORT_MAX_UPLOAD_BYTES` | API | no | `268435456` (256 MiB) | Upload ceiling per request. | `268435456` | no | no |
| `FORGECRM_IMPORT_MAX_UNPACKED_BYTES` | API | no | `2147483648` (2 GiB) | Total unpacked size of an archive. | `2147483648` | no | no |
| `FORGECRM_IMPORT_MAX_ARCHIVE_FILES` | API | no | `5000` | Maximum entries in an archive. | `5000` | no | no |
| `FORGECRM_IMPORT_MAX_ARCHIVE_DEPTH` | API | no | `8` | Maximum directory nesting in an archive. | `10` | no | no |
| `FORGECRM_IMPORT_MAX_RATIO` | API | no | `200` | Maximum tolerated compression ratio (zip-bomb guard). | `200` | no | no |
| `FORGECRM_IMPORT_BATCH_SIZE` | API, Worker | no | `500` | Rows inserted per batch during an import. | `500` | no | no |
| `FORGECRM_IMPORT_STORAGE_DIR` | API, Worker | no | none | Directory where uploaded files are stored. Must be on persistent storage. | `/var/lib/forgecrm/imports` | no | recommended |
| `FORGECRM_IMPORT_TEMP_DIR` | API, Worker | no | none | Scratch directory for extraction. Must be on persistent storage with room for the unpacked size. | `/var/lib/forgecrm/tmp` | no | recommended |

## Background jobs

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_WORKER_CONCURRENCY` | Worker | no | `4` | Jobs processed in parallel. | `4` | no | no |
| `FORGECRM_WORKER_POLL` | Worker | no | `2s` | How often the worker polls for new jobs. | `2s` | no | no |
| `FORGECRM_WORKER_SHUTDOWN_GRACE` | Worker | no | `30s` | Time the worker waits for an in-flight job after SIGTERM. Keep Compose's `stop_grace_period` above this. | `30s` | no | no |
| `FORGECRM_JOB_MAX_ATTEMPTS` | Worker | no | `5` | Default retry budget for background jobs. | `5` | no | no |

## Data retention (opt-in)

Retention is off unless enabled, and even when enabled it defaults to a dry run:
the sweep reports the rows a real run would remove and deletes nothing until
`FORGECRM_RETENTION_DRY_RUN` is `false`.

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_RETENTION_ENABLED` | Worker | no | `false` | Enables the retention sweep. | `false` | no | no |
| `FORGECRM_RETENTION_DRY_RUN` | Worker | no | `true` | When true, the sweep only reports. | `true` | no | no |
| `FORGECRM_RETENTION_ALLOW_AUDIT` | Worker | no | `false` | Allows sweeping audit and security evidence (`audit_log`, `security_events`, `login_attempts`). | `false` | no | no |
| `FORGECRM_RETENTION_WINDOWS` | Worker | no | none | Per-policy window overrides as `key=duration` pairs, comma-separated. An unknown key is refused at startup. | `password_reset_tokens_expired=24h` | no | no |

## Email (SMTP, optional)

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_SMTP_ENABLED` | API, Worker | no | `false` | Enables outbound email. | `true` | no | no |
| `FORGECRM_SMTP_HOST` | API, Worker | yes when enabled | none | SMTP server hostname. | `smtp.example.com` | no | no |
| `FORGECRM_SMTP_PORT` | API, Worker | no | `587` | SMTP port. | `587` | no | no |
| `FORGECRM_SMTP_USERNAME` | API, Worker | no | none | SMTP username. Must be paired with a password. | `apikey` | no | no |
| `FORGECRM_SMTP_PASSWORD` | API, Worker | no | none | SMTP password. | a secret string | yes | no |
| `FORGECRM_SMTP_FROM` | API, Worker | yes when enabled | none | From address. | `no-reply@example.com` | no | no |
| `FORGECRM_SMTP_FROM_NAME` | API, Worker | no | `ForgeCRM` | From display name. | `ForgeCRM` | no | no |
| `FORGECRM_SMTP_STARTTLS` | API, Worker | no | `true` | Use STARTTLS on the plain connection. | `true` | no | no |
| `FORGECRM_SMTP_TLS` | API, Worker | no | `true` | Use implicit TLS (SMTPS). | `false` | no | no |
| `FORGECRM_SMTP_TIMEOUT` | API, Worker | no | `15s` | Dial timeout for the SMTP connection, used by both the send path and the connection test. | `15s` | no | no |

## Telegram (optional)

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_TELEGRAM_ENABLED` | API, Worker | no | `false` | Enables the Telegram bot. With no token it disables itself rather than breaking startup. | `false` | no | no |
| `FORGECRM_TELEGRAM_BOT_TOKEN` | API, Worker | yes when enabled | none | Bot token from BotFather. | `123456:ABC-...` | yes | no |
| `FORGECRM_TELEGRAM_WEBHOOK_SECRET` | API | no | none | Secret Telegram sends in the webhook header, verified on each delivery. | a random string | yes | no |

## AI provider (optional)

The platform runs fully without a model provider. `ollama` keeps prompts local.

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_AI_ENABLED` | API, Worker | no | `false` | Enables AI features. Disabled when the provider is `none`. | `false` | no | no |
| `FORGECRM_AI_PROVIDER` | API, Worker | no | `none` | Provider kind: `none`, `ollama`, `openai`, `openai_compatible`. | `ollama` | no | no |
| `FORGECRM_AI_BASE_URL` | API, Worker | yes when enabled | none | Provider base URL. | `http://ollama:11434` | no | no |
| `FORGECRM_AI_API_KEY` | API, Worker | no | none | Provider API key. | a secret string | yes | no |
| `FORGECRM_AI_MODEL` | API, Worker | yes when enabled | none | Model name. | `llama3.1` | no | no |
| `FORGECRM_AI_TIMEOUT` | API, Worker | no | `60s` | Per-request timeout. | `60s` | no | no |
| `FORGECRM_AI_MAX_REQUESTS_PER_MIN` | API, Worker | no | `30` | AI requests allowed per organization per minute. Must not be negative. | `60` | no | no |

## Object storage (S3-compatible, optional)

Leave `FORGECRM_STORAGE_ENABLED=false` and the document routes answer 503; the
capabilities endpoint reports `storage: false` so the UI hides uploads.

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_STORAGE_ENABLED` | API, Worker | no | `false` | Enables attachment storage. | `false` | no | no |
| `FORGECRM_STORAGE_ENDPOINT` | API, Worker | yes when enabled | none | S3 endpoint. | `https://s3.amazonaws.com` | no | no |
| `FORGECRM_STORAGE_REGION` | API, Worker | no | `us-east-1` | Bucket region. | `us-east-1` | no | no |
| `FORGECRM_STORAGE_BUCKET` | API, Worker | yes when enabled | none | Bucket name. | `forgecrm-attachments` | no | no |
| `FORGECRM_STORAGE_ACCESS_KEY_ID` | API, Worker | yes when enabled | none | Access key id. | `AKIA...` | yes | no |
| `FORGECRM_STORAGE_SECRET_ACCESS_KEY` | API, Worker | yes when enabled | none | Secret access key. | a secret string | yes | no |
| `FORGECRM_STORAGE_USE_PATH_STYLE` | API, Worker | no | `true` | Path-style addressing. MinIO and most self-hosted gateways need it. | `true` | no | no |
| `FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT` | API, Worker | no | `false` | Allows an endpoint on a private network. The endpoint is an SSRF target otherwise, so keep it `false` for a public cloud endpoint. | `false` | no | no |
| `FORGECRM_STORAGE_MAX_OBJECT_BYTES` | API, Worker | no | `67108864` (64 MiB) | Maximum object size. | `67108864` | no | no |
| `FORGECRM_STORAGE_PRESIGN_TTL` | API, Worker | no | `15m` | Lifetime of a presigned URL. | `15m` | no | no |
| `FORGECRM_STORAGE_TIMEOUT` | API, Worker | no | `30s` | Per-operation timeout. | `30s` | no | no |

## Enterprise SSO (OIDC, optional)

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_OIDC_ENABLED` | API | no | `false` | Enables the OIDC sign-in surface. | `false` | no | no |
| `FORGECRM_OIDC_ISSUER` | API | yes when enabled | none | Issuer URL. Must be a valid URL. | `https://accounts.example.com` | no | no |
| `FORGECRM_OIDC_CLIENT_ID` | API | yes when enabled | none | Client id. | `forgecrm` | no | no |
| `FORGECRM_OIDC_CLIENT_SECRET` | API | no | none | Client secret, when the provider requires one. | a secret string | yes | no |
| `FORGECRM_OIDC_REDIRECT_URL` | API | no | derived from `FORGECRM_BASE_URL` | Redirect URL registered with the provider. | `https://crm.example.com/api/v1/auth/oidc/callback` | no | no |
| `FORGECRM_OIDC_SCOPES` | API | no | `openid email profile` | Space- or comma-separated scopes. | `openid,email,profile` | no | no |

## MCP client credentials

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_MCP_REQUESTS_PER_MIN` | API | no | `120` | Tool calls allowed per MCP credential per minute. `0` disables the limit. | `120` | no | no |

## Deployment and build

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_VERSION` | Docker, Compose | no | `dev` | Build argument passed to the linker for the image. Prefer setting it at build time; the runtime fallback above covers hosts that do not pass it. | `1.4.2` | no | recommended |
| `FORGECRM_COMMIT` | Docker, Compose | no | `unknown` | Build argument for the commit SHA. | `a1b2c3d` | no | recommended |
| `FORGECRM_HTTP_PORT` | Compose | no | `8080` | Host port the Compose `api` service publishes. | `8080` | no | no |
| `FORGECRM_PROXY_PORT` | Compose | no | `80` | Host port the optional `nginx` profile publishes. | `80` | no | no |
| `FORGECRM_API_URL` | Frontend (Vite dev, E2E) | no | `http://127.0.0.1:8080` | API origin the dev server proxies to, and the base URL the E2E suite targets. Baked into the dev build only. | `http://127.0.0.1:8080` | no | no |

## Test-only

Read only by the test suites. They must never be set in production.

| Name | Component | Required | Default | Description | Example | Secret | Production |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FORGECRM_TEST_DATABASE_URL` | Test | no | none | PostgreSQL used by integration tests. Tests skip when unset. | `postgres://...` | yes | never |
| `FORGECRM_TEST_REDIS_URL` | Test | no | none | Redis used by the distributed-limiter tests. Tests skip when unset. | `redis://127.0.0.1:6379/1` | no | never |
| `FORGECRM_TEST_MYSQL_URL` | Test | no | none | Reserved for a MySQL-backed test path; no current test reads it. | none | no | never |

## Declared but unused

These are read by the code but do not change behaviour today. They are listed so
an operator does not set them expecting an effect. Removing them is safe.

| Name | Where read | Why it has no effect |
| --- | --- | --- |
| `FORGECRM_SMTP_PUBLIC_URL` | `internal/config/config.go` | Stored on the email config; no code reads `Email.PublicURL`. |
| `FORGECRM_JOB_MAX_ATTEMPTS` | `internal/config/config.go` | Stored as `JobMaxAttempts`; the job queue uses a per-job attempt count instead. |
| `FORGECRM_TELEGRAM_WEBHOOK_SECRET` | `internal/notify/telegram.go` | Verified by `VerifyWebhookSecret`, which no route currently calls. |

## Production checklist

The API validates the following at startup and refuses to start when one is
wrong, naming the variable but never its value:

- `FORGECRM_ENV=production` requires `FORGECRM_SECRET_KEY` of at least 32
  characters.
- `FORGECRM_ENV=production` requires `FORGECRM_COOKIE_SECURE=true`.
- `FORGECRM_ENV=production` requires `FORGECRM_BASE_URL` to use `https://`.
- `FORGECRM_DATABASE_URL` must be a `postgres://` or `postgresql://` URL.
- `FORGECRM_BASE_URL` and other URLs must parse and have a host.
- `FORGECRM_BOOTSTRAP_ADMIN_EMAIL` and `FORGECRM_BOOTSTRAP_ADMIN_PASSWORD` must be
  set together or not at all.
- `FORGECRM_RATE_LIMIT_STORE=redis` requires `FORGECRM_REDIS_URL`, and a Redis
  URL alongside the `memory` store is refused as ambiguous.
- `FORGECRM_TRUSTED_PROXIES` must be a list of valid CIDRs; `*` is not accepted.
- An enabled subsystem (`STORAGE`, `SMTP`, `OIDC`, `AI`) must have its required
  settings.

## Divergences found and fixed

- `.env.example` did not mention `FORGECRM_BOOTSTRAP_ADMIN_EMAIL` or
  `FORGECRM_BOOTSTRAP_ADMIN_PASSWORD`. It does now, and the Docker Compose file
  documents the bootstrap command.
- `.env.example` listed `FORGECRM_IMPORT_MAX_ARCHIVE_DEPTH=10` while the code
  default is `8`. The example now matches the code.
- `FORGECRM_COOKIE_DOMAIN`, `FORGECRM_SMTP_PUBLIC_URL` and `FORGECRM_SMTP_TIMEOUT`
  were read by the code but absent from `.env.example`; they are documented here
  and the unused ones are flagged above.
- `FORGECRM_STATIC_DIR` is set by Compose and the root Dockerfile but was not
  documented; it is now.
