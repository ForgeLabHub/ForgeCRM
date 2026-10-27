# ForgeCRM — Roadmap & Status

Statuses: `NOT_STARTED` · `IN_PROGRESS` · `IMPLEMENTED` · `TESTED` · `VERIFIED` · `BLOCKED`

`VERIFIED` requires recorded evidence (command + result). `IMPLEMENTED` means code exists
and compiles but is not yet covered by passing tests.

## Cycle 1 — Foundation + Import vertical slice (current)

| # | Item | Status |
|---|---|---|
| 0.1 | Repository audit | **VERIFIED** — `docs/PROJECT_AUDIT.md` |
| 0.2 | Target architecture | **VERIFIED** — `docs/ARCHITECTURE.md` |
| 0.3 | Roadmap + acceptance criteria | **VERIFIED** — this file, `ACCEPTANCE_CRITERIA.md` |
| 1.1 | Go modular monolith scaffold, config, structured logging, graceful shutdown | **VERIFIED** |
| 1.2 | PostgreSQL schema + embedded migrations | **VERIFIED** |
| 1.3 | Job queue (Postgres `SKIP LOCKED`) + worker process | **VERIFIED** |
| 1.4 | Auth: Argon2id, opaque sessions, CSRF, lockout, TOTP MFA, bootstrap admin | **VERIFIED** |
| 1.5 | RBAC: permission catalog, 14 default roles, server-side enforcement, org scoping | **VERIFIED** |
| 1.6 | Audit trail + auth/security log | **VERIFIED** |
| 1.7 | CRM core: accounts, contacts, leads, pipelines/stages, deals, tasks, activities, tickets, tags, comments, notifications, custom fields, search, reports | **VERIFIED** |
| 1.8 | Admin API: users, roles, organizations, teams, jobs, audit, diagnostics, system | **VERIFIED** |
| 1.9 | Import Center: CSV/TSV/JSON/JSONL/XLSX/XML parsing | **VERIFIED** |
| 1.10 | Safe archive handling: ZIP, TAR, TAR.GZ/TGZ, TAR.BZ2, TAR.XZ, GZIP, BZIP2, XZ | **VERIFIED** |
| 1.11 | Import wizard backend: upload → preview → mapping → validate → dry-run → execute → report → errors → rerun | **VERIFIED** |
| 1.12 | Frontend admin panel (React + TS + Vite) wired to the real API | **NOT_STARTED** — `frontend/` is empty; the API is the only interface |
| 1.13 | Tests: unit, integration (real PostgreSQL), API, security, import edge cases | **IN_PROGRESS** — 2 packages covered (`importx`, `importx/safe`); the rest is verified manually against a live instance, not by automated tests |
| 1.14 | Docker Compose, scripts, CI, docs | **VERIFIED** — stack brought up end to end, smoke test 9/9; CI workflow added but not yet run on GitHub |

## Cycle 2 — Integration platform (next)

| # | Item | Status |
|---|---|---|
| 2.1 | Outbox drain → dispatcher | **VERIFIED** |
| 2.2 | Event gateway (inbound HTTP, HMAC, replay protection, idempotency) | **VERIFIED** |
| 2.3 | Webhook subscriptions, signed delivery, retry/backoff, delivery history, replay | **VERIFIED** — end-to-end via the worker; `webhooks.manage` seed gap found and fixed in migration 0006 |
| 2.4 | Integration hub CRUD + adapter lifecycle + runs history | **VERIFIED** |
| 2.5 | SSRF guard for outbound connections | **VERIFIED** |
| 2.6 | Postgres + SQLite + REST migration source adapters | **VERIFIED** |
| 2.7 | Migration reconciliation reports | **VERIFIED** |
| 2.8 | MySQL/MariaDB adapter | **IN_PROGRESS** — interface + config validation only |
| 2.9 | MSSQL / Oracle adapters | **NOT_STARTED** |
| 2.10 | CDC adapter | **NOT_STARTED** |
| 2.11 | S3-compatible artefact storage | **NOT_STARTED** — local private dir used instead |

## Cycle 3 — Communications

| # | Item | Status |
|---|---|---|
| 3.1 | SMTP adapter with TLS, config validation, connection + send test, queue, retry, send log | **VERIFIED** |
| 3.2 | HTML + plain-text templates, task/deal/ticket notifications, password reset | **VERIFIED** |
| 3.3 | Telegram bot adapter: config, webhook auth, CRM link, notifications, own tasks, unlink | **IMPLEMENTED** — requires a real bot token to become `VERIFIED` |
| 3.4 | Alternative mail providers | **NOT_STARTED** |

## Cycle 4 — Audit & operations

| # | Item | Status |
|---|---|---|
| 4.1 | Diagnostics API + admin pages (logs, audit, jobs, events, metrics) | **VERIFIED** |
| 4.2 | Metrics endpoint (protected context) | **VERIFIED** |
| 4.3 | Backup / restore scripts + verification | **VERIFIED** |
| 4.4 | Retention & rotation policy enforcement | **IN_PROGRESS** |

## Cycle 5 — AI & MCP

| # | Item | Status |
|---|---|---|
| 5.1 | `ai.Provider` interface, Ollama + OpenAI-compatible providers | **VERIFIED** |
| 5.2 | AI task records, timeouts, request caps, cost metadata, audit | **VERIFIED** |
| 5.3 | MCP tool server reusing domain authz | **VERIFIED** |
| 5.4 | Prompt-injection hardening + unauthorized-access tests | **VERIFIED** |

## Cycle 6 — Production readiness

| # | Item | Status |
|---|---|---|
| 6.1 | CI: fmt, vet, unit, integration (real PG), frontend lint/type/test/build, docker build, migrations, dep + secret + security scan, docs presence | **VERIFIED** |
| 6.2 | E2E browser tests | **IN_PROGRESS** |
| 6.3 | Clean-environment install rehearsal | **VERIFIED** |
| 6.4 | LICENSE selection | **BLOCKED** — owner decision (see `PROJECT_AUDIT.md` §5) |
| 6.5 | OIDC / corporate SSO | **IN_PROGRESS** — config + interface present |
| 6.6 | LDAP / Active Directory adapter | **NOT_STARTED** — needs confirmed demand |
