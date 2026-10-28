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
| 5.3 | Tool registry reusing domain authz (8 tools, permission-checked, audited) | **VERIFIED** |
| 5.3a | MCP **protocol** transport (JSON-RPC 2.0, `initialize`, `tools/list`, `tools/call`) | **NOT_STARTED** — `POST /api/v1/mcp` speaks a custom JSON shape, not the protocol, so no standard MCP client can connect. See `PROJECT_AUDIT.md` finding B |
| 5.4 | Prompt-injection hardening + unauthorized-access tests | **VERIFIED** |
| 5.5 | Contextual AI in entity cards, calibrated confidence, graceful failure, human handoff | **NOT_STARTED** — Phase 3 |

> Correction: an earlier revision of this file marked "MCP tool server" as
> `VERIFIED`. The tool *logic* is verified — 8 tools that reuse `crm.Service` and
> enforce permissions. The MCP *transport* does not exist, so the claim was
> narrowed rather than left to overstate the system.


## Cycle 6 — Production readiness

| # | Item | Status |
|---|---|---|
| 6.1 | CI: fmt, vet, unit, integration (real PG), frontend lint/type/test/build, docker build, migrations, dep + secret + security scan, docs presence | **VERIFIED** |
| 6.2 | E2E browser tests | **IN_PROGRESS** |
| 6.3 | Clean-environment install rehearsal | **VERIFIED** |
| 6.4 | LICENSE selection | **BLOCKED** — owner decision (see `PROJECT_AUDIT.md` §3 finding F) |
| 6.5 | OIDC / corporate SSO | **PARTIAL** — config and `capabilities` flag only; no Authorization Code flow exists. See `PROJECT_AUDIT.md` finding A |
| 6.6 | LDAP / Active Directory adapter | **NOT_STARTED** — needs confirmed demand |

---

# Master plan phases

The cycles above describe how the platform was built. The phases below describe
where it is going. Strategy and Definition of Done: `docs/MASTER_PLAN.md`.

## Phase 0 — Safe start — **COMPLETE**

| # | Item | Status |
|---|---|---|
| 0.1 | Audit the repository against the code, not the prior report | **VERIFIED** — `docs/PROJECT_AUDIT.md`, 8 findings |
| 0.2 | Integrate PRs #1–#3 into `main` without losing history | **VERIFIED** — fast-forward `7cf51de` → `ff83452`, no force push |
| 0.3 | ADR 0001 — hybrid architecture | **VERIFIED** — `docs/adr/0001-hybrid-architecture.md` |
| 0.4 | ADR 0002 — measurable extraction triggers | **VERIFIED** — `docs/adr/0002-service-extraction-triggers.md` |
| 0.5 | Enforce module boundaries with tests | **VERIFIED** — `internal/archtest`, 5 tests, `3b1ce12` |
| 0.6 | Master plan, audit, roadmap, statuses | **VERIFIED** |

## Phase 1 — Contracts and platform base — **IN PROGRESS**

| # | Item | Status |
|---|---|---|
| 1.1 | OpenAPI 3.1 generated from real routes | **VERIFIED** — `cmd/openapi-gen` builds the live router and walks it; 144 operations, 111 paths |
| 1.2 | Contract test: spec matches the router | **VERIFIED** — `internal/openapi/openapi_test.go`; drift guard proven negatively (added and modified routes both fail) |
| 1.3 | OpenAPI validation in CI | **IMPLEMENTED** — `go run ./cmd/openapi-gen -check` runs in the backend job |
| 1.4 | API reference site | **NOT_STARTED** |
| 1.5 | Distributed rate limiting (Redis, tenant-aware, fail-open/closed per category) | **NOT_STARTED** |
| 1.6 | Multi-replica pool verification | **NOT_STARTED** |
| 1.7 | Request correlation and diagnostics | **PARTIAL** — `request_id`/`correlation_id` emitted; no trace propagation |

## Phase 2 — Identity and security — **NOT_STARTED**

| # | Item | Status |
|---|---|---|
| 2.1 | OIDC Authorization Code Flow + PKCE | **NOT_STARTED** |
| 2.2 | Local test IdP (Keycloak) | **NOT_STARTED** |
| 2.3 | `capabilities.oidc` truthful | **VERIFIED** — field renamed `oidc_configured`, and `mcp` renamed `tool_api`; `capabilities_test.go` fails if either misleading name returns. The setting is still only a setting: the flow itself is item 2.1 |
| 2.4 | Consent model + policy service | **NOT_STARTED** |
| 2.5 | Consent enforcement in every channel | **NOT_STARTED** |
| 2.6 | Consent UI and "why was this allowed/blocked" | **NOT_STARTED** |

## Phase 3 — MCP and AI-native UX — **NOT_STARTED**

| # | Item | Status |
|---|---|---|
| 3.1 | MCP protocol transport | **NOT_STARTED** — the capability flag no longer claims otherwise (`tool_api`) |
| 3.2 | Read and safe-write MCP tools | **PARTIAL** — 8 tools exist behind a non-protocol endpoint |
| 3.3 | MCP credential model, revocation, read/write split | **NOT_STARTED** |
| 3.4 | MCP UI and client config examples | **NOT_STARTED** |
| 3.5 | Contextual AI, calibrated confidence, graceful failure, human handoff | **NOT_STARTED** |

## Phases 4–9 — Planned

Agentic system, tenant adaptation and SDK, identity resolution and collaboration,
mobile and analytics, automation and infrastructure, polish and documentation.
Each is scoped in `docs/MASTER_PLAN.md` §4. None is started; none is claimed as
done.

### Ordering rationale

Phase 1 precedes Phase 2 because the OIDC flow needs a contract to be testable
against, and because the rate limiter is the last obstacle to running more than
one replica — which every later phase depends on. Phase 3 precedes Phase 4 because
agents call tools, so the tool transport and credential model must be sound before
anything runs autonomously.

