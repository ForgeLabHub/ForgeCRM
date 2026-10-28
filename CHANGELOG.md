# Changelog

All notable changes to ForgeCRM are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once it reaches 1.0.

## [Unreleased]

### Security

- **Patched GO-2026-6809 (CVE-2026-111943) in the Go standard library.** The
  vulnerability affects `crypto/x509.Certificate.Verify` and is reachable from
  this codebase through the SMTP TLS handshake in `internal/notify/email.go`,
  so it is a real exposure rather than an unused transitive finding. The
  toolchain is now pinned to Go 1.26.10 (the first fixed 1.26 release) in
  `go.mod`, the build image and CI.

### Added

- **Web interface.** React 19 + TypeScript single-page application built with Vite,
  served by the same Nginx instance as the API so every request stays same-origin.
  Covers sign-in with second-factor verification, the CRM workspace (accounts,
  contacts, leads, deals, tasks, support tickets, reports), the Import Center with
  the field-mapping wizard, integrations, migrations, events and webhooks,
  background jobs, the AI and MCP status page, and the administration pages for
  users, roles, organizations, teams, email, Telegram, audit, security and
  diagnostics. Authorisation is enforced by the server on every request; the
  interface only decides what to show.
- **Frontend tests.** 31 Vitest tests covering the HTTP client (CSRF handling,
  error envelopes, session expiry), the paginated list component and the
  permission gate and sign-in flow. `fetch` is stubbed at the boundary so the
  client, auth context and components all run for real.
- **ESLint flat configuration** for the frontend, wired into the checks above.

- **Platform foundation.** Go API and worker with PostgreSQL, `chi` routing, `pgx`
  connection pooling, structured logging, request correlation and graceful shutdown.
- **Schema and migrations.** Five migrations covering platform, CRM, platform modules,
  RBAC seed data and migration secrets. Applied automatically on start when
  `FORGECRM_MIGRATE_ON_START` is set.
- **Authentication.** Argon2id password hashing, server-side sessions with opaque
  cookies, CSRF protection, TOTP enrolment and verification, login rate limiting,
  account lockout, session listing and revocation, password reset, and OIDC.
- **RBAC.** A permission catalog of 80 permissions, 14 default roles cloned per
  organization, server-side enforcement on every route, and audit of role changes.
- **CRM domain.** Accounts, contacts, leads, deals, pipelines and stages, tasks,
  activities, tickets, tags, comments, custom fields, documents, notifications,
  contracts, approvals, notes, search across permitted entities, and sales and support
  reports. List endpoints support pagination, sorting, filtering and search.
- **Import Center.** Staged wizard API with format detection by content and extension,
  type inference, preview, field mapping, deduplication, dry run, background execution,
  per-row error reporting, and idempotent re-runs. Formats: CSV, TSV, JSON, JSONL,
  XLSX, XML, and the archives ZIP, TAR, TAR.GZ, GZIP, BZIP2 and XZ.
- **Safe archive handling.** Entry count, total size, per-entry size, nesting depth and
  compression-ratio limits; rejection of absolute paths, `..` traversal, backslash
  traversal and symlinks; private extraction root; cleanup on every exit path.
- **Data migration.** Schema discovery, chunked reads, field mapping and
  reconciliation for PostgreSQL and SQLite sources, with read-only defaults.
- **Integration Hub.** A consistent adapter lifecycle (validate, test, discover, read,
  transform, write, status, close), SSRF-guarded outbound HTTP, retries, timeouts and
  run history.
- **Events and webhooks.** Inbound endpoints with signature verification and
  idempotency keys, outbound subscriptions with signed delivery, retries with backoff,
  delivery history and replay, and a transactional outbox.
- **Background jobs.** A database-backed queue with attempts, backoff, cancellation and
  retry, driven by a separate worker process.
- **Audit and observability.** Append-only audit log, security event log, application
  logs, and administrative diagnostics endpoints.
- **Notifications.** An email module with SMTP configuration validation, templates,
  queueing and a send log, plus a Telegram bot with webhook authentication and account
  linking. Both are disabled unless configured.
- **AI layer.** A provider-agnostic interface with cloud and local (Ollama) backends,
  timeouts, request limits and metadata logging. Disabled by default.
- **Deployment.** A multi-stage Dockerfile, Docker Compose stack with health checks and
  named volumes, an optional Nginx reverse proxy, and scripts for environment checking,
  setup, migrations, backup, restore and smoke testing.
- **CI.** Formatting, vet, unit and integration tests against a real PostgreSQL,
  builds, Docker build, secret scanning, dependency vulnerability scanning and a
  documentation presence check.

### Fixed

- **Outbox polling failed on a null correlation identifier.** `FetchPending` scanned a
  nullable column into a non-pointer `string`, which made every poll fail and stopped
  event dispatch.
- **Uploaded files lost their format.** The format was not recorded when the file was
  stored, so the worker rejected every import with `unsupported tabular format
  "unknown"`.
- **Background imports re-sniffed the storage path.** Stored uploads are named
  `<uuid>.upload`, so extension-based detection found nothing. The resolved source now
  carries the detected format.
- **Archive extraction failed with a missing temporary directory.** The extraction
  parent directory was assumed to exist; it is now created before use.
- **Archives were misdetected as delimited text.** A binary archive header can contain
  a delimiter byte, so content sniffing classified ZIP files as CSV and failed with a
  misleading encoding error. An archive container now always clears the tabular format.
- **Archive analysis only handled single-file containers.** Multi-file archives could
  be imported but not analysed, so the preview and mapping steps were unavailable.
- **Archive analysis leaked server filesystem paths.** The absolute extraction path was
  serialised to clients; it is now excluded from the response.
- **Search returned `null` instead of an empty array** when nothing matched, breaking
  clients that iterate the result.
- **`webhooks.manage` was never seeded.** The permission was added to the Go catalog
  and guards six webhook endpoints, but migration 0004 did not insert it. No role could
  hold it, so only a platform owner could create, list, test or replay webhooks, and
  `integration_admin` was refused despite the role existing to manage integrations.
  Migration 0006 seeds the permission and grants it to `platform_owner`,
  `platform_admin`, `integration_admin` and `tenant_admin`, for platform role
  templates and for every organization's existing clones.

### Security

- Uploaded archives are handled as hostile input, with dedicated tests for path
  traversal, absolute paths, symlinks, archive bombs, truncation and cleanup.
- SSRF protection blocks loopback, link-local, private and cloud metadata destinations.
- Cross-organization access returns 404 rather than 403.
- Blocking a user revokes their sessions immediately.

### Added

- **End-to-end test suite.** `frontend/e2e/platform.spec.ts` drives a real
  Chromium browser against the real Go API and a real PostgreSQL database; the
  network is never stubbed. It covers sign-in, sign-out, session revocation, the
  account create/edit/archive lifecycle, cross-checks each write through the API
  rather than the screen alone, and loads every workspace and administration
  route asserting on the page's own heading so a route that silently falls
  through to the overview is caught. `npm run e2e` runs it; the `e2e` job in CI
  starts PostgreSQL, applies migrations, provisions the owner and runs the suite.
- **`scripts/e2e-bootstrap.sh`** provisions the end-to-end platform owner through
  the same `--bootstrap-admin` path an operator uses, so the suite has no
  test-only back door. It is idempotent and safe to run before every suite.
- **Regression tests for request throttling** in
  `backend/internal/api/ratelimit_test.go`, covering the two defects below.

### Fixed

- **Health probes were rate limited, so a busy API reported the container as
  unhealthy.** `/health` and `/ready` shared the API's request budget. Once the
  budget was spent the probes answered 429, which an orchestrator reads as a
  failed container and acts on by restarting it — turning a traffic spike into an
  outage. The probes now sit outside the limiter while `/api/v1` stays inside it,
  which `TestHealthEndpointsAreNotRateLimited` and `TestAPISubtreeIsRateLimited`
  pin down.
- **The login throttle was a hardcoded 20 requests per minute per client IP.**
  Everyone behind one NAT gateway shares a single bucket, so an office of thirty
  people would lock itself out after twenty sign-ins between them. The budget is
  now the `FORGECRM_LOGIN_RATE_LIMIT_PER_MIN` setting (default 60) and is
  documented in `.env.example`, because the correct value depends on how many
  users share an address.
- **Six lint errors and two warnings in the frontend** that would have failed the
  CI lint step, including a Playwright fixture parameter named `use` that lint
  read as React's `use` hook.
- **The integration-test step ran the packages in parallel over one shared
  database.** Each integration package resets the schema in its setup, so one
  package could drop the tables another was still seeding, and the suite failed
  with `relation "users" does not exist`. It passed in earlier runs by luck of
  scheduling, which is the worst kind of flake: green until it is not. The step
  now runs with `-p 1`.
- **The end-to-end job could time out waiting for the Vite dev server.** Vite
  binds `localhost` by default, which resolves to `::1` on some runners, while
  Playwright polls `127.0.0.1`; the server started correctly and the wait still
  expired. The config now pins the dev server to `127.0.0.1`.

### Known gaps

- No license file.
- OIDC has not been exercised against a real identity provider.
- Rate limiting is per process, not global. Behind more than one API replica the
  effective budget is multiplied by the replica count, so a shared store
  (Redis) is required before running more than one replica in production.
