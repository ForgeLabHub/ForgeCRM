# ForgeCRM — Acceptance Criteria

Criteria are written so each one can be checked by a command or an observable result.
A module is `VERIFIED` only when every criterion below has recorded evidence.

## A. Platform foundation

- **A1** `docker compose up` (or `make dev`) brings up API, worker, frontend, PostgreSQL
  and nginx; `/health` returns `200`.
- **A2** `/ready` returns `200` only when the database is reachable; returns `503`
  otherwise.
- **A3** Migrations run automatically on API start and are idempotent (running twice is a
  no-op).
- **A4** `GET /api/v1/system/info` and `/capabilities` expose versions and enabled
  subsystems and contain **no** secret values.
- **A5** Graceful shutdown: `SIGTERM` finishes in-flight requests and stops the worker
  without dropping a claimed job.

## B. Authentication

- **B1** Passwords are stored as Argon2id hashes with a unique salt; the plaintext never
  appears in the database or logs.
- **B2** Login sets an opaque session cookie; only a hash of the token is stored
  server-side.
- **B3** A new session token is issued after successful login (session-fixation defence).
- **B4** Repeated failed logins lock the account for a configured window; the lockout is
  audited.
- **B5** TOTP MFA is enforced as a second factor when enabled; recovery codes work once
  each.
- **B6** The bootstrap command creates the first platform owner with an operator-supplied
  or randomly generated password; no default password exists in the codebase.
- **B7** `GET /api/v1/auth/sessions` lists active sessions; `DELETE .../sessions/{id}`
  revokes one and the revoked token immediately stops working.
- **B8** Password reset tokens are single-use, hashed at rest and expire.

## C. Authorization

- **C1** All 14 default roles exist as permission sets, not hard-coded branches.
- **C2** A request without the required permission returns `403` and is audited.
- **C3** A user cannot read or mutate another organization's records even with a valid,
  guessed UUID (`404`/`403`, never `200` with data).
- **C4** Role and permission changes are audited with actor, target and diff.
- **C5** Blocking a user immediately invalidates their sessions.
- **C6** Privilege-escalation tests pass: a `readonly_auditor` cannot perform writes, a
  `support_agent` cannot approve discounts, a user cannot grant themselves a role.
- **C7** Background jobs re-check permissions at execution time rather than trusting a
  stale token.

## D. CRM

- **D1** Full CRUD for accounts, contacts, leads, deals, pipelines/stages, tasks,
  activities, tickets, tags, comments and custom fields, all via `/api/v1`.
- **D2** Collections support pagination, sorting, filtering and text search with enforced
  maximum page size.
- **D3** Referential integrity is enforced by foreign keys; deleting a referenced record
  is rejected or cascades by explicit rule.
- **D4** Archiving preserves history and excludes the record from default listings.
- **D5** Deal stage changes append to `deal_stage_history`.
- **D6** Every mutation writes an audit entry with actor, entity, action and timestamp.
- **D7** Bulk operations verify permissions per record and report per-record outcomes.

## E. Import Center

- **E1** CSV, TSV, JSON array, JSONL/NDJSON, XLSX and XML files parse into a common row
  model.
- **E2** ZIP, TAR, TAR.GZ, TAR.BZ2, TAR.XZ, GZIP, BZIP2 and XZ archives are read without
  extracting to an attacker-controlled path.
- **E3** Path traversal (`../`, absolute paths), symlink escape, oversized entries, too
  many entries, excessive nesting and zip bombs are all rejected with a clear error.
- **E4** A corrupt/truncated archive fails gracefully; the failure is reported and the
  temp directory is removed.
- **E5** Preview returns inferred columns, sample rows and detected types.
- **E6** Mapping maps source columns to CRM fields with type conversion; invalid
  conversions are reported per row.
- **E7** Dry-run performs validation and reporting without writing business data.
- **E8** Execute runs as a background job, processes in batches, and is resumable from a
  checkpoint after interruption.
- **E9** Duplicate handling is configurable (skip / update / create) and keyed on a
  chosen natural key or `(external_source, external_id)`.
- **E10** The report contains total/imported/skipped/failed counts and a per-row error
  list; errors are retrievable via `/api/v1/imports/{id}/errors`.
- **E11** Re-running a completed import is safe and does not duplicate data.
- **E12** The original file's SHA-256, the processing config and the final report are
  persisted; the raw file is stored in a private directory, not a web-served one.
- **E13** Cancellation works before the commit point; behaviour after it is documented.

## F. Migration

- **F1** PostgreSQL, SQLite and REST sources can be connected and schema-discovered.
- **F2** Connection parameters are stored encrypted or via secret reference; the password
  never appears in the `integrations` table or in logs.
- **F3** Source access is read-only by default.
- **F4** Data is read in bounded batches; row-count reconciliation is reported.
- **F5** Incremental sync advances a watermark and does not re-import unchanged rows.
- **F6** Divergence between source and target is reported, not silently ignored.

## G. Events, webhooks, integrations

- **G1** Inbound webhooks verify an HMAC signature, a timestamp window and an idempotency
  key; replays are rejected.
- **G2** Outbound deliveries are HMAC-signed, retried with exponential backoff, and
  recorded in delivery history with response status.
- **G3** Failed deliveries appear in a dead-letter view and can be manually replayed.
- **G4** Domain writes that emit events do so through the outbox in one transaction.
- **G5** The SSRF guard rejects loopback, private, link-local and cloud-metadata
  destinations, including after redirects.
- **G6** Integration config changes are audited; secrets are masked in every response.
- **G7** An integration without valid credentials is `disabled` and the rest of the
  platform keeps working.

## H. Notifications

- **H1** SMTP configuration is validated; a connection test and a real send test are
  distinct operations.
- **H2** Sends are queued, retried with backoff, and logged with outcome.
- **H3** A successful send is only reported when the SMTP server accepted the message.
- **H4** Telegram operations require a linked, permissioned CRM user; unlinking revokes
  access. With no bot token the bot stays disabled and the platform is unaffected.

## I. AI & MCP

- **I1** The AI provider is selected by configuration; no provider key means `disabled`,
  not an error.
- **I2** AI requests have timeouts and request caps; operations are recorded with metadata
  and audit entries.
- **I3** MCP tools call the same domain services and enforce the same permissions as the
  REST API.
- **I4** No tool exposes raw SQL, shell, filesystem or unbounded export.
- **I5** Instructions embedded in untrusted documents or imported rows do not change tool
  authorization.
- **I6** Unauthorized MCP tool calls are denied and audited.

## J. Operations

- **J1** Logs are structured with levels and correlation IDs; passwords, tokens and keys
  are redacted.
- **J2** The audit log is append-only and filterable by time, actor, organization,
  component and correlation ID.
- **J3** `/metrics` is exposed only in a protected context.
- **J4** Backup and restore scripts work against a real database and the restored data is
  verified.
- **J5** Optional subsystems failing (SMTP, Telegram, AI, an integration) do not stop the
  API or worker.

## K. Quality gates

- **K1** `gofmt` clean, `go vet` clean, `go build ./...` succeeds.
- **K2** Unit + integration + API + security tests pass against a real PostgreSQL.
- **K3** Frontend: lint, typecheck, tests and production build succeed.
- **K4** Docker images build.
- **K5** CI does not disable or skip tests to appear green.
- **K6** Required documentation files exist.
