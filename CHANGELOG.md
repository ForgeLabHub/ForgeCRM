# Changelog

All notable changes to ForgeCRM are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once it reaches 1.0.

## [Unreleased]

### Added

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

### Known gaps

- No web administration panel.
- No license file.
- OIDC has not been exercised against a real identity provider.
- Rate limiting is per process, not global.
