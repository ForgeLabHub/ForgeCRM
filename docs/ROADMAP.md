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
| 1.12 | Frontend admin panel (React + TS + Vite) wired to the real API | **VERIFIED** — 21 pages under `frontend/src/pages/` (CRM workspace, Import Center + wizard, Migrations, Integrations, Webhooks, Events, Background jobs, Agent approvals, and 9 admin pages) across 34 routes, all calling the real API; 6 unit-test files and 13 Playwright E2E cases |
| 1.13 | Tests: unit, integration (real PostgreSQL), API, security, import edge cases | **IN_PROGRESS** — backend unit + integration packages (`importx`, `importx/safe`, `internal/api` under the `integration` tag, `authn`, `ratelimit`, `openapi`, `archtest`), frontend unit tests and Playwright E2E; some areas are still verified manually rather than by automated tests |
| 1.14 | Docker Compose, scripts, CI, docs | **VERIFIED** — stack brought up end to end, smoke test 9/9; CI workflow runs on GitHub Actions (fmt, vet, unit, integration against a real PostgreSQL, frontend lint/type/test/build, Docker build, migrations, dependency + secret + security scan, docs presence) |

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
| 5.3a | MCP **protocol** transport (JSON-RPC 2.0, `initialize`, `tools/list`, `tools/call`) | **VERIFIED** — `POST /api/v1/mcp` speaks JSON-RPC 2.0: `initialize` with protocol-version negotiation, `ping`, `tools/list` in the protocol shape (`inputSchema`), `tools/call`, notifications answered with an empty 202, and the JSON-RPC error codes for parse, invalid-request, method-not-found and invalid-params. `GET /api/v1/mcp/tools` and `POST /api/v1/mcp/call` remain as the admin UI's own shape. Verified by `internal/ai/mcp_protocol_test.go` (transport) and `internal/api/mcp_integration_test.go` (session, permission, confirmation and tenant scope over the real HTTP stack). See `docs/mcp/` |
| 5.4 | Prompt-injection hardening + unauthorized-access tests | **VERIFIED** |
| 5.4a | Human-in-the-loop agent execution: durable approval requests, risk tiers, payload-hash binding, second-factor step-up for `destructive` actions, approve/reject UI | **VERIFIED** — `internal/agent` (approval lifecycle), migrations `0009`–`0011`, `POST /api/v1/agent/actions/{id}/decide`, `POST /api/v1/auth/step-up`, and the `/approvals` screen. A lapsed step-up window is refused with `403`; a decision whose arguments hash does not match the stored one is refused. Covered by `internal/api/agent_http_test.go` (integration) and `internal/agent/agent_test.go` (unit). See `docs/adr/0005-hitl-agent-execution-model.md` |
| 5.5 | Contextual AI in entity cards, calibrated confidence, graceful failure, human handoff | **NOT_STARTED** — Phase 3 |

> Correction: an earlier revision of this file marked "MCP tool server" as
> `VERIFIED`. The tool *logic* was verified — 8 tools that reuse `crm.Service` and
> enforce permissions — but the MCP *transport* did not exist, so the claim was
> narrowed rather than left to overstate the system. The transport now exists and
> is verified separately, which is why 5.3a is distinct from 5.3.


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

## Cycle 7 — Distributed rate limiting and client-address trust

| # | Item | Status |
|---|---|---|
| 7.1 | `Store` interface with `MemoryStore` and `RedisStore` | **VERIFIED** — `internal/ratelimit`; `MemoryStore` is mutex-guarded, `RedisStore` increments with a single Lua script so the read-modify-write is atomic on the server |
| 7.2 | Data race in the previous in-process limiter | **VERIFIED FIXED** — the old `httpx.RateLimiter` kept counters in a bare `map` written from every request goroutine. `net/http` serves each request in its own goroutine, so this was a live race and a possible unrecoverable `concurrent map write` panic. `TestMemoryStoreIsSafeForConcurrentUse` (64 goroutines × 250 increments) fails under `-race` against the old code and asserts no increment is lost against the new |
| 7.3 | Unbounded key growth | **VERIFIED FIXED** — the previous map was never swept because `Sweep` had no caller. `MemoryStore` has a background sweeper and a `maxKeys` bound, both covered by tests |
| 7.4 | Forwarding-header trust | **VERIFIED FIXED** — `chi`'s `middleware.RealIP` was rewriting `RemoteAddr` from `X-Forwarded-For` for every caller, so any client could choose the address it was rate-limited and logged under. It is removed. `ClientIPMiddleware` resolves the address once, from the peer unless the peer is in `FORGECRM_TRUSTED_PROXIES`, and the chain is read right-to-left skipping trusted hops |
| 7.5 | Per-policy fail mode | **VERIFIED** — `FailOpen` for general API traffic, `FailClosed` for authentication. Both decisions set `Degraded`, and a store error is never reported as a 429 |
| 7.6 | Redis opt-in deployment | **VERIFIED** — `redis` service behind a compose profile; `docker compose config` with and without the profile both validated. Default `up` starts no Redis |
| 7.7 | Multi-replica shared counter | **VERIFIED (unit level)** — `TestRedisStoreWindowIsSharedBetweenInstances` builds two limiters over separate connections and asserts a limit of 5 yields exactly 5 allows across both. Two API containers against a real database is item 1.6 |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./...` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests, race detector | `go test -race -count=1 ./...` | all packages pass, including the new 16 000-increment concurrency test |
| Redis integration | `go test -race -tags=integration -run TestRedisStore ./internal/ratelimit/` | 5 tests pass against `redis:7-alpine` |
| OpenAPI currency | `go run ./cmd/openapi-gen -check` | spec current; no route change |
| Compose | `docker compose config`, `docker compose --profile redis config` | both valid; Redis absent from the default profile |

The spoofing test was written before the fix and observed to fail against the
original left-to-right header parsing (`TestForwardedChainIsReadRightToLeft`),
which is what showed the obvious implementation returns the address the client
wrote rather than the one the proxy observed.

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
| 1.5 | Distributed rate limiting (Redis, tenant-aware, fail-open/closed per category) | **VERIFIED** — `internal/ratelimit` with a `Store` interface, `MemoryStore` and `RedisStore` (atomic Lua script); fail mode is per policy; see cycle 7 below |
| 1.6 | Multi-replica pool verification | **IN_PROGRESS** — the shared counter is verified across two independent connections (see cycle 7); running two API containers against one database and confirming the session and limiter behaviour together is still to do |
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

## Phase 3 — MCP and AI-native UX — **IN_PROGRESS**

| # | Item | Status |
|---|---|---|
| 3.1 | MCP protocol transport | **VERIFIED** — JSON-RPC 2.0 over `POST /api/v1/mcp`; see cycle 5.3a |
| 3.2 | Read and safe-write MCP tools | **PARTIAL** — 8 tools are reachable over the protocol; the read/write split is currently per-tool (the `dangerous` flag plus a required `confirm` acknowledgement), not a separate credential scope |
| 3.3 | MCP credential model, revocation, read/write split | **NOT_STARTED** — MCP currently authenticates with the caller's browser session; a dedicated credential with its own scope and revocation is not implemented |
| 3.4 | MCP UI and client config examples | **PARTIAL** — the AI & MCP page lists tools and can invoke one, with a confirmation checkbox for mutating tools. Client configuration examples live in `docs/mcp/`; a copy-to-clipboard generator is not built |
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

