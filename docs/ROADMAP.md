# ForgeCRM — Roadmap & Status

Statuses: `NOT_STARTED` · `IN_PROGRESS` · `IMPLEMENTED` · `TESTED` · `VERIFIED` · `BLOCKED`

`VERIFIED` requires recorded evidence (command + result). `IMPLEMENTED` means code exists
and compiles but is not yet covered by passing tests.

## Cycle 1 — Foundation + Import vertical slice

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

## Cycle 2 — Integration platform

| # | Item | Status |
|---|---|---|
| 2.1 | Outbox drain → dispatcher | **VERIFIED** |
| 2.2 | Event gateway (inbound HTTP, HMAC, replay protection, idempotency) | **VERIFIED** |
| 2.3 | Webhook subscriptions, signed delivery, retry/backoff, delivery history, replay | **VERIFIED** — end-to-end via the worker; `webhooks.manage` seed gap found and fixed in migration 0006 |
| 2.4 | Integration hub CRUD + adapter lifecycle + runs history | **VERIFIED** |
| 2.5 | SSRF guard for outbound connections | **VERIFIED** |
| 2.6 | Postgres + SQLite + REST migration source adapters | **VERIFIED** |
| 2.7 | Migration reconciliation reports | **VERIFIED** |
| 2.8 | MySQL/MariaDB adapter | **DEFERRED** — the direction is assessed from the code in ADR 0008, which records the measured blockers (19 non-test files importing pgx, 64 `RETURNING`, 21 `ON CONFLICT`, 8 `SKIP LOCKED`, 26 `ILIKE`, 16 PostgreSQL migrations using `gen_random_uuid`, `plpgsql` triggers, partial indexes, `jsonb` and arrays). PostgreSQL remains the only supported datastore; no MySQL code is added, and compilation against a MySQL driver is explicitly not treated as support |
| 2.9 | MSSQL / Oracle adapters | **NOT_STARTED** |
| 2.10 | CDC adapter | **NOT_STARTED** |
| 2.11 | S3-compatible artefact storage | **VERIFIED** — `internal/storage`; a provider-agnostic `ObjectStore` with an S3 implementation (AWS, MinIO, Ceph, B2, Wasabi) and a memory store for tests. Metadata in the `documents` table (migration `0016`), server-built object keys, per-route permission checks, an SSRF guard on the endpoint, and an object/row reconciler. The `AttachmentPanel` in the CRM exposes upload, download and archive; `GET /api/v1/system/capabilities` reports `storage` and `max_upload_bytes`. Covered by `internal/testsupport/storage_test.go`, `storage_s3_test.go` and `internal/storage/storage_test.go` |

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
| 4.4 | Retention & rotation policy enforcement | **VERIFIED** — `internal/retention`; an opt-in registry of per-table windows, dry-run by default, batched deletes, append-only audit protection, per-policy counts and one audit entry per real run. Covers the run history that nothing pruned before: finished workflow runs, finished agent runs and processed inbound events, each swept by terminal status only so a paused run or an unhandled event is never removed. Covered by 13 integration tests in `internal/testsupport/retention_test.go` |

## Cycle 5 — AI & MCP

| # | Item | Status |
|---|---|---|
| 5.1 | `ai.Provider` interface, Ollama + OpenAI-compatible providers | **VERIFIED** |
| 5.2 | AI task records, timeouts, request caps, cost metadata, audit | **VERIFIED** |
| 5.3 | Tool registry reusing domain authz (8 tools, permission-checked, audited) | **VERIFIED** |
| 5.3a | MCP **protocol** transport (JSON-RPC 2.0, `initialize`, `tools/list`, `tools/call`) | **VERIFIED** — `POST /api/v1/mcp` speaks JSON-RPC 2.0: `initialize` with protocol-version negotiation, `ping`, `tools/list` in the protocol shape (`inputSchema`), `tools/call`, notifications answered with an empty 202, and the JSON-RPC error codes for parse, invalid-request, method-not-found and invalid-params. `GET /api/v1/mcp/tools` and `POST /api/v1/mcp/call` remain as the admin UI's own shape. Verified by `internal/ai/mcp_protocol_test.go` (transport) and `internal/api/mcp_integration_test.go` (session, permission, confirmation and tenant scope over the real HTTP stack). See `docs/mcp/` |
| 5.4 | Prompt-injection hardening + unauthorized-access tests | **VERIFIED** |
| 5.4a | Human-in-the-loop agent execution: durable approval requests, risk tiers, payload-hash binding, second-factor step-up for `destructive` actions, approve/reject UI | **VERIFIED** — `internal/agent` (approval lifecycle), migrations `0009`–`0011`, `POST /api/v1/agent/actions/{id}/decide`, `POST /api/v1/auth/step-up`, and the `/approvals` screen. A lapsed step-up window is refused with `403`; a decision whose arguments hash does not match the stored one is refused. Covered by `internal/api/agent_http_test.go` (integration) and `internal/agent/agent_test.go` (unit). See `docs/adr/0005-hitl-agent-execution-model.md` |
| 5.5 | Contextual AI in entity cards, calibrated confidence, graceful failure, human handoff | **VERIFIED** — `internal/ai/assist.go` assembles a permission-checked context per entity (account, contact, deal, ticket, task), masks personal data, and returns CRM facts, model inference and recommendations in separate fields so a suggestion is never presented as a recorded fact. The `AssistPanel` is on the account, contact, deal, ticket and task screens. Integration tests in `internal/testsupport/ai_assist_test.go` cover cross-organization refusal and masking; `ai_assist_cache_test.go` covers the answer cache, which migration 0018 repaired (the table lacked the `recommendations` and `draft` columns the code writes, and the read ignored `context_hash`). Calibrated confidence is the server-computed grounding score in `internal/ai/confidence.go` — not the model's self-report, which is uncalibrated by nature, but a score from signals the server can check (structured reply, figures matched against the given facts, context completeness), returned with its basis. `confidence_test.go` covers the scoring and the cache test asserts it is identical on a fresh and a cached answer. `frontend/e2e/platform.spec.ts` drives the panel in real Chromium against a real provider and asserts the grounding note and its reason are shown; both contextual-AI E2E cases skip on provider availability so they are honest with and without one |

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
| 6.5 | OIDC / corporate SSO | **VERIFIED** — the Authorization Code flow with PKCE exists (`internal/oidc`) and a provider can be registered and managed from `/admin/sso`. See cycle 16. A local test IdP (6.2 / item 2.2) is still open |
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

## Cycle 8 — Webhook event vocabulary and HTTP-level lifecycle coverage

| # | Item | Status |
|---|---|---|
| 8.1 | Event vocabulary drift between the subscription form and the dispatcher | **VERIFIED FIXED** — `WebhooksPage` hard-coded a shortened list (`contact.created`) while the dispatcher matched the namespaced name (`crm.contact.created`). A subscription created from the form selected no events, so the customer's endpoint silently received nothing: the failure looks like a network fault and is invisible in the UI. The canonical list is now `outbox.EventTypes()`, exposed as `event_types` on `GET /api/v1/system/capabilities`, and the form builds its picker from it |
| 8.2 | A regression guard for the vocabulary | **VERIFIED** — `TestWebhookEventVocabularyMatchesTheDispatcher` (`internal/api`, integration tag) reads the advertised list, then emits and dispatches every advertised type and asserts each produces exactly one delivery. It was run against a deliberately shortened list and failed, naming each missing type |
| 8.3 | HTTP-level coverage of dead-letter and replay | **VERIFIED** — `TestWebhookLifecycleOverHTTP` drives create → dispatch → exhaust attempts → dead → replay entirely through the API, asserting the delivery state, `replay_count`, and the `webhooks.delivery_dead_lettered` / `webhooks.replayed` audit entries as an operator would see them. Replaying a still-pending delivery is asserted to be refused with 404 |
| 8.4 | `/webhooks` and `/events` in the E2E page sweep | **VERIFIED** — both pages added to the sweep in `frontend/e2e/platform.spec.ts`; the webhook form is asserted to offer `crm.contact.created` and not the bare `contact.created` |
| 8.5 | HTTP-level replay edge cases, and the two defects they exposed | **VERIFIED FIXED** — `internal/api/webhook_replay_http_test.go` adds ten scenarios through the real stack: the replay response shape, lifetime `replay_count` across two windows, the audit entry's context (actor, entity, `replay_count` metadata), the in-flight refusal, the wrong-subscription refusal, concurrent replays applied once, a retried replay after a lost response, cross-tenant refusal for both replay and delivery listing, a caller without `webhooks.manage`, and the shared invariant that a refused replay leaves no state or audit entry. Two defects were found and fixed: (1) the guard was `status <> 'pending'`, so a delivery a worker had claimed (`delivering`) could be replayed while it was being sent — contradicting ADR 0004's own "already in flight is refused"; the guard is now `status NOT IN ('pending','delivering')`. (2) `handleReplayDelivery` read only `deliveryId` and ignored the `{id}` subscription the route and OpenAPI both declare required, so a delivery could be replayed under a subscription it did not belong to; `Replay` now scopes the mutating statement by `subscription_id` as well as `organization_id`. Each fix was mutation-tested: reverting either one fails its test with the expected message |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./internal/ ./cmd/` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test ./internal/rbac/ ./internal/outbox/ ./internal/api/` | pass |
| Integration tests | `go test -p 1 -tags=integration ./...` | all packages pass, including `internal/testsupport` and the two new `internal/api` tests |
| Regression guard, negative | shorten the advertised list, re-run 8.2 | test fails, naming every omitted type |
| E2E | `npx playwright test` | 28 pass in real Chromium (was 26), including the two new cases |
| OpenAPI currency | `go run ./cmd/openapi-gen` | spec regenerated; adds `event_types` to `CapabilitiesResponse` |

---

## Cycle 9 — Workflow automation engine

| # | Item | Status |
|---|---|---|
| 9.1 | Definition document, validation, trigger types (event, schedule, manual) | **VERIFIED** — `internal/workflow`; the definition is JSON with a bounded step count, branch depth and delay; validation is at save time so a workflow that could not execute is rejected by the API |
| 9.2 | Step actions through `crm.Service` (create_task, update_deal, create_ticket, notify) | **VERIFIED** — every action calls the same service the HTTP API uses, as the workflow's author |
| 9.3 | Permission enforced at execution time, not authoring time | **VERIFIED** — the engine maps each step type to the permission it requires and resolves the author's permissions from the database on every run. This is required because the engine calls the service directly and bypasses the API middleware that normally checks the coarse permission; without it, `workflows.manage` would imply every action permission. `TestWorkflowPermissionIsRecheckedAtExecutionTime` revokes `tasks.create` and asserts the next run fails and creates nothing |
| 9.4 | At-least-once delivery, de-duplicated on `(workflow_id, event_id)` | **VERIFIED** — the claim and the run row are one transaction; a duplicate commits nothing. `TestWorkflowEventTriggerIsDeDuplicated` delivers one event three times and asserts one run and one task |
| 9.5 | Delay without holding a worker | **VERIFIED** — a delay writes a resume instant and the next step index; the worker scheduler resumes from there. `TestWorkflowDelayPausesAndResumes` |
| 9.6 | Chain depth bound | **VERIFIED** — an event a step causes inherits the run's correlation id and the chain depth is the run count for it; a run at `max_depth` is recorded as `skipped`. `TestWorkflowDepthLimitStopsLoops` |
| 9.7 | Cross-tenant notification refused | **VERIFIED** — the recipient's active membership is verified before the insert. `TestWorkflowCannotNotifyAcrossOrganizations` |
| 9.8 | Multi-replica scheduler | **VERIFIED** — the scheduler claims a due workflow by advancing `next_run_at` in the same transaction that selects it |
| 9.9 | REST API + admin page | **VERIFIED** — `/api/v1/workflows` and `/api/v1/workflow-runs`; the `/workflows` page lists, edits, enables, runs, archives and shows run history with the per-step log |
| 9.10 | Documentation | **VERIFIED** — `docs/adr/0007-workflow-automation-engine.md`, `docs/architecture/workflows.md` |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./internal/ ./cmd/` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test -count=1 ./...` | 15 packages pass |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -run TestWorkflow ./internal/testsupport/` | 6/6 pass against real PostgreSQL |
| Full integration suite | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 ./internal/testsupport/` | passes |
| Frontend | `npm run build`, `npm test` | build clean; 48 unit tests pass |
| API, live | create → list → run → run detail → steps against a running server | `201`, `1 item`, `200 succeeded`, run detail `200`, steps `200`; the created task is present in the database |
| API, refusals | mutation without CSRF, anonymous read, unknown step type | `403`, `401`, `422` |

A defect was found by the integration test and fixed in this cycle: the event claim
ran after the run row was created, so every duplicate delivery of the same event
wrote a second run marked `skipped`, accumulating without bound under retry. See the
`Fixed` entry in `CHANGELOG.md`.

---

## Cycle 10 — Workflow templates and a structured definition editor

| # | Item | Status |
|---|---|---|
| 10.1 | Template catalog of ready CRM scenarios | **VERIFIED** — `internal/workflow/templates.go` registers six scenarios (overdue-deal owner notification, follow-up task on stage change, inactivity reminder, new-ticket acknowledgement, deal-won onboarding task, ticket priority triage). Each names a trigger and steps the engine already runs; construction fails if a template's event is not in `outbox.EventTypes()`, so a template cannot be created, enabled and then never fire |
| 10.2 | Create a workflow from a template | **VERIFIED** — `POST /api/v1/workflow-templates/{id}/create`; the definition is built and validated server-side, so the client never supplies it. Covered by `TestEveryCatalogTemplateCanBeCreatedAndRun` and `TestTemplateCreateFromAPI` |
| 10.3 | Import and export a workflow as a portable document | **VERIFIED** — `internal/workflow/export.go`; the export document carries a format name and version and import checks both first, so a document from another version is refused with a reason rather than partially applied. `TestExportImportRoundTripsThroughTheStore` |
| 10.4 | Definition validation, exposed to the editor | **VERIFIED** — the parser's structural rules (non-empty steps, unique ids, a condition with an operator, no delay inside a foreach) are checked before save and surfaced per error; the server remains the authority and its message is shown verbatim |
| 10.5 | New step actions the templates need | **VERIFIED** — `find_deals` and `foreach` steps added to `internal/workflow/engine.go`, with bound date filters (`expected_close_before`, `updated_before`, `no_activity_since`) on `ListDeals`. Every new step type has an entry in the permission map, so a workflow cannot use it to exceed the author's rights. `TestWorkflowFindDealsAndForeachCreatesOneTaskPerDeal` |
| 10.6 | Structured editor and template picker UI | **VERIFIED** — `frontend/src/components/WorkflowEditor.tsx` reads and writes the same JSON document the engine parses (the raw view is the same document serialised), so there is no second format. The `WorkflowsPage` gains the template picker, import/export dialogs and the runs history. `WorkflowEditor.test.tsx` (11 tests) |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./cmd ./internal` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test -count=1 ./...` | all packages pass |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 ./...` | passes against real PostgreSQL |
| OpenAPI currency | `go run ./cmd/openapi-gen -check` | spec current |
| Frontend | `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` | typecheck clean, lint 0 errors, 69 tests pass, build succeeds |

A latent defect was found and fixed in this cycle: `expected_close_date` is a
PostgreSQL `date` and was scanned into a `*string`, which pgx cannot encode in
binary format. Any deal that had a close date failed to load from `ListDeals`,
`GetDeal` and the workflow sweep. The column is now cast to text, which is the ISO
date the API already promised.

---

## Cycle 11 — Intelligence Platform: the Simulation Lab

| # | Item | Status |
|---|---|---|
| 11.1 | Pure analytics core (exact money, ratios, scenario maths) | **VERIFIED** — `internal/analytics`; imports only the standard library, never a float for money. Algorithm version `simulate-v1` is stored with every run so a result computed by an older version stays interpretable |
| 11.2 | CRM baseline reader | **VERIFIED** — `crm.Service.Baseline` reads the caller's own organization under `reports.read` and returns per-stage and period aggregates with explicit warnings for excluded currencies and a missing pipeline |
| 11.3 | `internal/simulate` service + persistence | **VERIFIED** — `CreateScenario`, `ListScenarios`, `GetScenario`, `ArchiveScenario`, `RunScenario`, `Preview`, `ListRuns`. Enforces `simulation.manage`/`simulation.run`/`simulation.read` in the service, not only at the route, and filters every query by `organization_id` |
| 11.4 | Migration `0019_simulation_lab` + permission seed | **VERIFIED** — `simulation_scenarios` and `simulation_runs`, both tenant-scoped, with the name-unique-per-org partial index and the run-history indexes; the three permissions are seeded to roles and `TestSeedDataMatchesThePermissionCatalog` keeps the catalog and the seed in step |
| 11.5 | API surface + OpenAPI | **VERIFIED** — scenario CRUD, run, preview and a session-only catalog; contract rows in `internal/api/contract.go`; `go run ./cmd/openapi-gen -check` reports the spec current |
| 11.6 | Simulation Lab frontend | **VERIFIED** — `frontend/src/pages/SimulationPage.tsx` and `frontend/src/api/simulation.ts`, a `Simulation Lab` entry in the Workspace navigation. Observed CRM figures and modelled figures are shown in separate columns with the model's assumptions, so a suggestion is never presented as a recorded fact |
| 11.7 | Tests: unit, PostgreSQL integration, frontend, E2E | **VERIFIED** — `internal/testsupport/simulation_test.go` (PostgreSQL), `frontend/src/pages/SimulationPage.test.tsx`, and the Chromium suite in `e2e/platform.spec.ts` |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./cmd ./internal` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test -count=1 ./...` | all packages pass |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 -p 1 ./...` | passes against real PostgreSQL |
| OpenAPI currency | `go run ./cmd/openapi-gen -check` | spec current |
| Frontend | `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` | typecheck clean, lint 0 errors, 79 tests pass, build succeeds |
| E2E (Chromium) | `npx playwright test --project=chromium` | 38 passed, including the Simulation Lab create → save → run → history path |

A real defect was found by the integration tests and fixed: `avg(amount)` over
`numeric(18,2)` returns a much higher scale (`500.0000000000000000`), and the
exact money parser refuses a value with more than two decimals rather than
truncating it, so every revenue and retention scenario failed at run time. The
baseline reader now rounds the average to the money scale where it is produced;
the parser is left strict, because rounding there would hide the same class of
mistake elsewhere.

## Cycle 12 — Intelligence Platform: the Forecasting & Risk Lab

| # | Item | Status |
|---|---|---|
| 12.1 | Pure forecasting core | **VERIFIED** — `internal/analytics`, `forecast-v1`: mean, moving-average and trend methods over integer-minor-unit money; history window bounded by `MinHistoryPeriods=3` / `MaxHistoryPeriods=240`, horizon by `MaxForecastHorizon=36`. Reports measured out-of-sample error (MAE) beside the naive last-value baseline; insufficient history is a first-class error, never a fabricated number |
| 12.2 | `internal/forecast` service + persistence | **VERIFIED** — reads revenue history through `crm.Service` under the caller's principal, persists a run to `forecast_runs` freezing params, history and algorithm version. Enforces `forecast.run`/`forecast.read` in the service and filters by `organization_id` |
| 12.3 | `internal/risk` service + persistence | **VERIFIED** — scans the CRM for stalled deals, overdue tasks, accounts without contact, unusual deal amounts, conversion drops and stage concentration; keeps a live signal only while it holds (partial unique index on live signal per org+type); each signal cites its evidence rows. `Thresholds.validateBounds()` mirrors the column bounds so an out-of-range update returns 422 with a reason |
| 12.4 | Migration `0020_forecasting_risk` + permission seed | **VERIFIED** — `forecast_runs`, `risk_signals` and `risk_thresholds`, all tenant-scoped, with the live-signal partial unique index and history indexes; `forecast.read`, `forecast.run`, `risk.read`, `risk.manage` seeded to roles |
| 12.5 | API surface + OpenAPI | **VERIFIED** — forecast runs and a session-only catalog; risk signals, scan, status transitions and thresholds; contract rows in `internal/api/contract.go`; `go run ./cmd/openapi-gen -check` reports the spec current |
| 12.6 | Frontend pages | **VERIFIED** — `frontend/src/pages/ForecastPage.tsx`, `RiskPage.tsx` and `frontend/src/api/intelligence.ts`, with `Forecasting` and `Risk` entries in the Workspace navigation. A projection is never shown alone: observed series, measured error and the naive baseline sit beside it |
| 12.7 | Tests: unit, PostgreSQL integration, frontend, E2E | **VERIFIED** — `internal/testsupport/intelligence_test.go` (PostgreSQL), `frontend/src/pages/ForecastPage.test.tsx` / `RiskPage.test.tsx`, and the Chromium suite in `e2e/platform.spec.ts` |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./cmd ./internal` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test -count=1 ./...` | all packages pass |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 -p 1 ./...` | passes against real PostgreSQL |
| OpenAPI currency | `go run ./cmd/openapi-gen -check` | spec current |
| Frontend | `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` | typecheck clean, lint 0 errors, 92 tests pass, build succeeds |
| E2E (Chromium) | `npx playwright test --project=chromium` | 43 passed, including the Forecasting compute → accuracy → observed-series path and the Risk scan → thresholds path |

A real defect was found by the Chromium suite and fixed: the API binary assembled
`api.Deps` without copying the `Forecast` and `Risk` services that
`internal/wiring` builds, so every intelligence handler dereferenced a nil service
and the recovery middleware answered an opaque 500. The dependency is now copied,
and the routes are mounted behind a guard that answers 503 naming the subsystem
when a service is missing, so the same mistake is legible next time
(`TestIntelligenceRoutesRefuseWhenUnwired`).

---

## Cycle 13 — Graceful AI provider failure

| # | Item | Status |
|---|---|---|
| 13.1 | An unreachable or timed-out provider is a retryable outage | **VERIFIED** — `internal/ai/ai.go` `classifyProviderError` recognises a deadline or a transport-level failure and wraps it as `ErrProviderUnavailable`; `HTTPErrorFrom` maps it to a 503 with code `unavailable`. A provider that answered with an application error is passed through unchanged, so a 4xx is not disguised as "try again shortly" |
| 13.2 | The API reports the outage distinctly from "not configured" | **VERIFIED** — `respondDomainError` routes `ErrProviderUnavailable` alongside `ErrDisabled` (503/`disabled`) and `ErrRateLimited` (429); the three states stay distinct |
| 13.3 | The panel offers a retry only when retrying could work | **VERIFIED** — `AssistPanel` renders a "Try again" control only when the error envelope's `retryable` flag is set, so a permission refusal gets no button that cannot work |
| 13.4 | A configured-but-unreachable provider is visible before a request is spent | **VERIFIED** — the panel reads the reachability the status endpoint already reports (`healthy` plus the probe error) and warns when a provider is configured but the probe did not reach it; the controls stay enabled, because the probe can be stale, so the user is warned, not blocked |

### Evidence

| Check | Command | Result |
|---|---|---|
| Unit tests | `go test -count=1 ./internal/ai/... ./internal/httpx/...` | pass, including `provider_error_test.go` (classification, mapping, wrapping) |
| Integration test | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 -run TestAssistReportsAnUnreachableProvider ./internal/testsupport/` | passes against real PostgreSQL; the `ai_tasks` row is asserted `failed` with a recorded error |
| Frontend unit | `npx vitest run src/components/AssistPanel.test.tsx` | 11 pass, including retry-button presence and absence and the pre-request reachability warning |
| E2E (Chromium) | `npx playwright test --project=chromium` | 43 passed, 1 skipped (the no-provider case is skipped when a provider is configured, and vice versa) |

## Cycle 14 — The constructor: recipes into standalone projects

| # | Item | Status |
|---|---|---|
| 14.1 | A recipe manifest names the modules, adapters and demo for a project | **VERIFIED** — `internal/constructor/recipes/analytics-demo/recipe.yaml`; the loader validates it and `List`/`Load` are exercised by `internal/constructor/constructor_test.go` |
| 14.2 | Generation copies only the named modules and rewrites their imports | **VERIFIED** — `internal/constructor/generate.go` rewrites `github.com/geff1778/forgecrm/internal/` to the recipe's module path; a unit test proves a cross-package import is re-pointed and no file still names the monolith |
| 14.3 | Generation refuses to overwrite and writes nothing on failure | **VERIFIED** — a non-empty output directory is an error, and a missing module aborts before the first file is written; both are unit-tested |
| 14.4 | A generated project builds, tests and runs on its own | **VERIFIED** — `scripts/constructor-test/run.sh` scaffolds `analytics-demo`, builds, tests and runs the demo twice and checks the deterministic output; CI job `constructor`, `make test-constructor` |
| 14.5 | The demo asserts real numbers, not a printed "success" | **VERIFIED** — the generated `cmd/demo` checks the funnel, revenue, retention, forecast and risk outputs against the values `internal/analytics` produces inside ForgeCRM |
| 14.6 | The constructor is itself extraction-proven | **VERIFIED** — `scripts/extract-test/constructor/setup.sh` (also copies the embedded `recipes/`), an archtest pin, and tests that use a fixture tree rather than the monolith |

### Evidence

| Check | Command | Result |
|---|---|---|
| Unit tests | `go test -count=1 ./internal/constructor/ ./internal/archtest/` | pass |
| Constructor harness | `./scripts/constructor-test/run.sh` | `analytics-demo` generated, built, tested and run; deterministic output |
| Extraction harness | `./scripts/extract-test/run.sh constructor` | build, tests and smoke pass in a clean module |
| Format and vet | `gofmt -l ./cmd ./internal`, `go vet ./...` | clean |

---

## Cycle 15 — Saved list views

| # | Item | Status |
|---|---|---|
| 15.1 | A view is a stored list filter/sort/column selection, not a new query language | **VERIFIED** — `internal/views`; the stored `filters` document is exactly the query-parameter set the CRM list endpoints accept, so applying a view replays those parameters to the same endpoint. A view cannot name a filter no list endpoint honours |
| 15.2 | Owner-scoped visibility, one predicate in the query | **VERIFIED** — `List`/`Get` filter `owner_user_id = $3 OR shared = true` in SQL, so another user's private view is never loaded. `Update`/`Delete` are narrower: only the owner may change a view, and a shared view is readable by the organization but not editable by it. `missReason` answers 404 for "not there" and 403 for "not yours" |
| 15.3 | Migration `0023` + permission seed | **VERIFIED** — `saved_views` with the entity-type CHECK, the partial unique name index per owner and screen, and the scope/owner indexes; `views.read`/`views.manage` seeded to the roles that work a CRM list. `TestSeedDataMatchesThePermissionCatalog` keeps the catalog and the seed in step |
| 15.4 | API surface + OpenAPI | **VERIFIED** — `/api/v1/views` CRUD behind `views.read`/`views.manage` and a nil-service guard; contract rows in `internal/api/contract.go`; `go run ./cmd/openapi-gen -check` reports the spec current |
| 15.5 | Frontend switcher | **VERIFIED** — `frontend/src/components/SavedViewsBar.tsx` and `frontend/src/api/views.ts`; the switcher is controlled by the screen, so applying a view is the same code path a hand-entered filter takes. It sits on the deals, leads, tasks and tickets list screens. A read-only user is offered no save or delete control |
| 15.6 | Tests: unit, PostgreSQL integration, frontend, E2E | **VERIFIED** — `internal/views/views_test.go` (7 unit tests), `internal/testsupport/views_test.go` (7 PostgreSQL tests: owner scoping, shared read-only, cross-org isolation, name uniqueness, validation, archive-and-free-the-name, filter round-trip), `frontend/src/components/SavedViewsBar.test.tsx` (7 tests), and two Chromium cases in `e2e/platform.spec.ts` |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./cmd ./internal` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test -count=1 ./internal/views/... ./internal/api/... ./internal/openapi/... ./internal/archtest/...` | pass |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 -p 1 ./internal/testsupport/... ./internal/api/...` | pass against real PostgreSQL (`testsupport` 168s, `api` 128s) |
| OpenAPI currency | `go run ./cmd/openapi-gen -check` | spec current |
| Frontend | `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` | typecheck clean, lint 0 errors, 141 tests pass, build succeeds |

---

## Cycle 16 — Single sign-on: the flow and its administration

| # | Item | Status |
|---|---|---|
| 16.1 | Authorization-code flow with PKCE and a nonce | **VERIFIED** — `internal/oidc`. `Begin` stores the state, nonce and PKCE verifier server-side and returns the provider's authorization URL; `Complete` claims the state row once, exchanges the code, and verifies the ID token's signature, issuer, audience, expiry and nonce before reading a claim. Nothing is tied to one vendor: the provider's discovery document is fetched |
| 16.2 | Identity resolution without an account-takeover path | **VERIFIED** — a local account is identified by `(provider, subject)`, never by email. An email claim is used to find an existing account only when the provider marks it verified and it falls inside the configured domains; otherwise a provider that let a user set an arbitrary address would be a takeover path. Just-in-time provisioning is opt-in per provider |
| 16.3 | Secret handling and open-redirect defence | **VERIFIED** — the client secret is sealed with the platform keyring and never returned by an API; `has_secret` reports only its presence. `return_to` is validated as a relative path, and the provider list the sign-in page reads is anonymous and carries no secret |
| 16.4 | Migration `0022` + permission seed | **VERIFIED** — `oidc_providers`, `user_identities` and `oidc_login_states`; `sso.read`/`sso.manage` seeded and added to the Go catalog in the same change, so `TestSeedDataMatchesThePermissionCatalog` holds |
| 16.5 | Administration page | **VERIFIED** — `frontend/src/pages/admin/SSOPage.tsx` and the `ssoAdminApi` client. Lists providers, shows the exact redirect URL to register with each, and creates, edits, enables and deletes them. The secret is write-only: an edit that leaves the field blank sends no secret, so it cannot clear the stored one. A read-only caller is offered no management control |
| 16.6 | Tests: unit, PostgreSQL integration, frontend, E2E | **VERIFIED** — `internal/oidc/oidc_test.go` (input validation), `internal/api/sso_integration_test.go` (the whole flow against PostgreSQL through an in-process provider that publishes real discovery and JWKS documents and signs real ID tokens, including wrong issuer, audience, nonce and an unverified email), `frontend/src/pages/admin/SSOPage.test.tsx` (3 tests), and a Chromium case in `e2e/platform.spec.ts` |

### Evidence

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l ./cmd ./internal` | clean |
| Vet | `go vet ./...` | clean |
| Unit tests | `go test -count=1 ./internal/oidc/... ./internal/archtest/...` | pass |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 -p 1 ./internal/api/...` | pass against real PostgreSQL |
| Frontend | `npm run typecheck`, `npm run lint`, `npm test`, `npm run build` | typecheck clean, lint 0 errors, 144 tests pass, build succeeds |
| E2E | `npx playwright test -g "single sign-on"` | pass in Chromium against the real API and database |

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

## Phase 2 — Identity and security — **IN PROGRESS**

| # | Item | Status |
|---|---|---|
| 2.1 | OIDC Authorization Code Flow + PKCE | **VERIFIED** — `internal/oidc`; the flow, its administration page and the PostgreSQL integration tests. See cycle 16 |
| 2.2 | Local test IdP (Keycloak) | **PARTIAL** — no Keycloak container is provisioned, but the flow is driven end to end against an in-process provider that publishes real discovery and JWKS documents and signs real ID tokens (`internal/api/sso_integration_test.go`). A shared Keycloak for the E2E stack remains open |
| 2.3 | `capabilities.oidc` truthful | **VERIFIED** — field renamed `oidc_configured`, and `mcp` renamed `tool_api`; `capabilities_test.go` fails if either misleading name returns. The field reports whether the deployment-level OIDC setting is present; the flow itself is item 2.1 |
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
| 3.5 | Contextual AI, calibrated confidence, graceful failure, human handoff | **PARTIAL** — `internal/ai` assembles a permission-checked, PII-masked context for an account, contact, deal, ticket or task and runs summarise / risks / next-steps / draft / recap / ask through the provider abstraction, surfaced in the CRM record screens. Human handoff is the existing approval path: the model drafts, the user saves. Calibrated confidence is implemented as a server-computed grounding score (`internal/ai/confidence.go`): it is not the model's self-reported number, which is uncalibrated by nature, but a score the server derives from signals it can verify — whether the reply was structured, whether the figures in it appear in the CRM facts the model was given, and whether the context was complete. The basis is returned alongside the level so the panel shows the reason, not a bare number. Graceful failure is implemented (cycle 13): no provider → 503 `disabled`, rate cap → 429 `rate_limited`, unreachable or timed-out provider → retryable 503 `unavailable`, each distinct; the panel offers a retry only for the retryable case. Streaming is implemented (cycle 14): `POST /api/v1/ai/stream/chat` and `POST /api/v1/ai/stream/assist/{id}` answer with server-sent events (`delta` events carry the text as it arrives, a final `done` event carries the same result the buffered route returns, and an `error` event carries the same code the buffered route would have used). The buffered routes are unchanged, so a proxy or client that cannot read an event stream keeps working; the panel falls back to the buffered request when a stream closes with no result. A provider that implements only the base interface is buffered and delivered as a single delta |

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

