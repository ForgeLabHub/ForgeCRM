# Module inventory

This is the code-verified catalogue of every logical module in ForgeCRM, backend
and frontend. It was produced at commit `7a6d67f` by reading the sources, the
import graph (`go list`), the migrations and the tests — not by trusting the
existing documents. Where a document and the code disagreed, the code won and the
disagreement is recorded in the relevant row.

The unit of inventory is the **Go package** (and, for the frontend, the layer).
A directory with a `go.mod`-level boundary is not automatically a module: a
package is listed here because it owns behaviour, not because it has a folder.

Two things this document deliberately does not do:

- It does not invent interfaces. Every type and function named below exists in
  the package named beside it.
- It does not claim `READY` extraction. See `EXTRACT_READY.md`; nothing is
  extract-ready until it has been pulled into a clean project and built there.

## How to read a row

| Field | Meaning |
| --- | --- |
| **Location** | Real paths. Everything under `backend/internal/<pkg>` unless stated. |
| **Public API** | Exported types and functions actually present in the package. |
| **Internal deps** | Other `internal/…` packages it imports (non-test). |
| **External deps** | Third-party and stdlib-relevant dependencies. |
| **Fake / test double** | An in-process substitute that ships in the non-test code, or a documented injection point. A `_test.go` helper is listed as "test-only". |
| **Tests** | Real test files. Unit = no database; integration = `//go:build integration`. |
| **Extract** | `no` / `almost` / `yes`, consistent with `EXTRACT_READY.md`. |
| **Cut lines** | What moves with the module and what stays behind. |

---

## Backend — entrypoints (`backend/cmd`)

### cmd/forgecrm-api
- **Responsibility:** HTTP server process. Builds config, connects the pool, wires
  services through `internal/wiring`, mounts the router, runs migrations when
  `-migrate`/`FORGECRM_MIGRATE_ON_START`, bootstraps the platform owner.
- **Public API:** `main` only.
- **Internal deps:** `api, audit, authn, config, database, httpx, logging, migrate,
  rbac, secrets, wiring`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** none.
- **Tests:** none (covered indirectly by `testsupport` integration tests, which
  build the same `api.New`).
- **Extract:** `no` — a composition root, not a component.
- **Cut lines:** stays with the monolith; every extraction produces a *new*
  entrypoint rather than moving this one.

### cmd/forgecrm-worker
- **Responsibility:** background process. Drives webhook delivery, workflow runs,
  retention, the job queue, imports, outbox dispatch and notifications.
- **Public API:** `main` only.
- **Internal deps:** `agent, ai, audit, config, crm, database, events, importx,
  integrations, jobs, logging, notify, outbox, rbac, retention, secrets, workflow`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** none.
- **Tests:** none directly; the components it drives are tested in `testsupport`.
- **Extract:** `no` — composition root.
- **Cut lines:** stays; a builder recipe that needs background work generates its
  own worker entrypoint.

### cmd/openapi-gen
- **Responsibility:** regenerate `docs/api/openapi.json` from the live router;
  `-check` fails when the committed spec disagrees with the code.
- **Public API:** `main` only.
- **Internal deps:** `api, config, httpx, openapi, ratelimit`.
- **External deps:** none beyond stdlib.
- **Fake:** none.
- **Tests:** covered by `internal/openapi` and `internal/api` contract tests.
- **Extract:** `no`.
- **Cut lines:** stays; extraction does not regenerate a spec it does not own.

---

## Backend — domain core

### internal/crm  — the core domain
- **Responsibility:** entities and rules for contacts, accounts, leads, deals,
  tasks, tickets, activities, comments, tags, custom fields, pipelines, search,
  notifications and the read-only reporting/intelligence queries. Tenant-scoped;
  checks record ownership; writes audit.
- **Public API (types):** `Service`, `Account`, `Contact`, `Lead`, `Deal`, `Task`,
  `Ticket`, `Activity`, `Comment`, `Tag`, `CustomFieldDefinition`, `Notification`,
  `Pipeline`, `PipelineStage`, `SearchResult`, and the `*Input` DTOs
  (`AccountInput`, `ContactInput`, `DealInput`, `TaskInput`, `TicketInput`,
  `LeadInput`), plus observation/report types (`SalesSummaryReport`,
  `PipelineReport`, `SupportSummaryReport`, `RevenueHistory`, `RiskObservationSet`,
  `BaselineData`, `StageBaseline`, `PeriodConversion`).
- **Public API (funcs):** `NewService`, `MaxBulkArchiveIDs`; `ErrNotFound`,
  `ErrConflict`.
- **Internal deps:** `audit, database, httpx, outbox, rbac`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** none in-process. Tests drive a real PostgreSQL.
- **Tests:** `crm_test.go` (the validators and the `JSONMap` column codec; the
  first in-package tests) plus `internal/testsupport` integration tests and
  `internal/api` handler tests, which drive a real PostgreSQL.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/crm`,
  with `audit`, `database`, `httpx`, `outbox` and `rbac` as its dependencies, all
  of them proven. The "`httpx` and `outbox` are the real friction" claim was
  wrong: both are proven leaves/scoped modules, so they travel with it.
- **Cut lines:** the package moves whole. `database` (pool + `WithTx`), `rbac`
  (Principal + permission catalog), `httpx` and `outbox` travel as declared
  dependencies.

### internal/analytics  — pure computation
- **Responsibility:** pure, deterministic arithmetic over money, ratios,
  percentages, funnel/revenue/retention modelling, forecasting methods and risk
  detection. **No database, no clock, no I/O.**
- **Public API:** `Money`, `Ratio`, `Unit`, `Comparison`, `Evidence`,
  `RiskSignal`, `RiskSeverity`, `RiskSignalType`, `RiskThresholds`,
  `Forecast`, `ForecastMethod`, `ForecastPoint`, `ForecastParams`,
  `ForecastQuality`, `FunnelParams`, `RetentionParams`, `RevenueParams`,
  `StageSnapshot`, `Baseline`, `PeriodPoint`, `DealObservation`,
  `TaskObservation`, `AccountObservation`, `ConversionPeriodObservation`;
  funcs `NewMoney`, `ParseMoney`, `NewRatioFrom*`, `FloatPercent`,
  `PercentString`, `RunForecast`, `RunFunnel`, `RunRetention`, `RunRevenue`,
  `DetectStalledDeals`, `DetectOverdueTasks`, `DetectAccountsWithoutContact`,
  `DetectAmountSpikes`, `DetectConversionDrops`, `DetectStageConcentration`,
  `SortSignals`, `SortPeriods`, `DefaultRiskThresholds`, and version consts.
- **Internal deps:** none.
- **External deps:** stdlib only (`math/big`, `math`, `sort`, …).
- **Fake:** not applicable — it is already pure and deterministic.
- **Tests:** `analytics_test.go` etc. (4 files), no database.
- **Extract:** `yes` — **zero internal dependencies**, and it was actually
  extracted, built, tested and run in a clean module. See `EXTRACT_READY.md`
  (Candidate 1) and `EXTRACTION_TESTING.md`.
- **Cut lines:** the whole package moves with nothing else. A consumer must
  supply observations and read back results.

---

## Backend — application services

### internal/importx
- **Responsibility:** Import Center: format detection, parsing (CSV/XLSX/JSON/
  NDJSON/archives), mapping, validation, dry-run and execution against the CRM.
- **Public API:** `Service`, `Job`, `JobPayload`, `CreateRequest`, `Options`,
  `MappingEntry`, `AnalysisResult`, `ValidationReport`, `ValidationIssue`,
  `Report`, `RowError`, `FieldList`, `NewService`.
- **Internal deps:** `audit, config, crm, database, httpx, importx/parsers,
  importx/safe, jobs, outbox, rbac`. **10 internal deps** — see the 8+ note in
  `DEPENDENCY_MAP.md`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`;
  sub-package `parsers` adds `github.com/xuri/excelize/v2`, `safe` adds
  `github.com/ulikunitz/xz`.
- **Fake:** `safe` operates on local temp dirs (no external service). No in-process
  fake of the CRM write path.
- **Tests:** `importx` unit tests (2 files, incl. detection and temp cleanup);
  integration via `testsupport`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/importx`,
  with `audit`, `config`, `crm`, `database`, `httpx`, `importx/parsers`,
  `importx/safe`, `jobs`, `outbox` and `rbac` as its dependencies, all proven. The
  earlier `no` understated it: the write path into `crm` is a dependency, not a
  blocker, and every dependency is proven.
- **Cut lines:** `parsers/` and `safe/` are separable sub-packages; the service
  itself is not.

### internal/importx/safe  — untrusted-archive extraction
- **Responsibility:** the one place that writes bytes chosen by an uploader to
  disk. It detects a container from its magic bytes and refuses a path traversal,
  a symlink, an oversized entry, a zip bomb and excessive nesting, writing into a
  private `0700` root it owns.
- **Public API:** `Limits`, `DefaultLimits`, `Extractor`, `NewExtractor`,
  `Format`, `DetectFormat`, `ExtractedFile`, `ExtractResult`, `IsArchive`,
  `IsSupported`, and the `Err*` rejections (`ErrTooManyEntries`, `ErrTooLarge`,
  `ErrEntryTooLarge`, `ErrTooDeep`, `ErrUnsafePath`, `ErrUnsupportedLink`,
  `ErrRatioExceeded`, `ErrUnsupportedType`).
- **Internal deps:** none — it is a leaf.
- **External deps:** `github.com/ulikunitz/xz` (standard library for zip/tar/gzip/bzip2).
- **Fake:** operates on local temp directories; no external service.
- **Tests:** 2 unit files (~24 tests) covering traversal, symlinks, zip bombs,
  deep nesting, corrupt data and independent fixture decoding.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/safe`.
  Because it is the code that handles attacker-controlled input, "it can be lifted
  out and its security tests run in isolation" is worth more here than anywhere
  else.
- **Cut lines:** moves on its own; nothing inside `importx` needs to move with it.

### internal/importx/parsers  — tabular source parsing
- **Responsibility:** convert CSV/TSV/JSON/JSONL/XLSX/XML into a common row model,
  reporting the inferred column set, the detected types and any row-level problems
  instead of guessing silently.
- **Public API:** `Parse`, `Stream`, `Detect`, `ListSheets`, `Format`, `Row`,
  `Result`, `RowError`, `Options`, `StreamOptions`, `Batch`, `ParseDate`,
  `ParseBool`, `ParseNumber`.
- **Internal deps:** none — it is a leaf.
- **External deps:** `github.com/xuri/excelize/v2`.
- **Tests:** added in this slice; pins header normalisation, row recovery, type
  inference, XLSX sheet handling, XML records and encoding rejection. See the
  bug note below.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/parsers`.
  Writing its tests found two real bugs, both fixed: XLSX was rejected by the
  UTF-8 check applied to every format, and the XML parser split each record into
  an attribute row and a field row.
- **Cut lines:** moves on its own; nothing inside `importx` needs to move with it.

### internal/migratex  — external data migration
- **Responsibility:** move data from an external source (PostgreSQL, SQLite,
  REST) into ForgeCRM, with reconciliation records.
- **Public API:** `Service`, `Source`, `SourceKind`, `TableSchema`, `ColumnInfo`,
  `ConnectionConfig`, `CreateRequest`, `MigrationJob`, `Reconciliation`,
  `JSONMap`, `NewService`, `NewPostgresSource`, `NewSQLiteSource`,
  `NewRESTSource`, `Implemented`.
- **Internal deps:** `audit, database, httpx, integrations, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`,
  `modernc.org/sqlite`.
- **Fake:** `Source` is an interface (a documented seam); a REST source adapter is
  included.
- **Tests:** integration via `testsupport`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/migratex`,
  with `audit`, `database`, `httpx`, `integrations`, `rbac` and `secrets` as its
  dependencies, all proven. The `Source` interface is the boundary; sources stay
  pluggable, and `Implemented` names the three that have a real adapter.
- **Cut lines:** the `Source` interface is the boundary; sources are pluggable.

### internal/ai  — provider abstraction, contextual assist, MCP, agent tools
- **Responsibility:** model-provider abstraction (Ollama, OpenAI-compatible),
  permission-checked + PII-masked context assembly, insight generation with a
  server-computed grounding score, the MCP JSON-RPC transport, the tool registry
  and the AI-backed agent executor.
- **Public API:** `Provider` (interface), `Service`, `OllamaProvider`,
  `OpenAICompatibleProvider`, `CompletionRequest`, `CompletionResponse`,
  `AssistRequest`, `Insight`, `Fact`, `Disclosure`, `Draft`, `Confidence`,
  `ContextBlock`, `EntityType`, `Kind`, `Trust`, `MCPServer`, `ToolRegistry`,
  `Tool`, `ToolDescriptor`, `AgentExecutor`, `PromptInput`, `Message`,
  `RPCError`, `Request`, `Response`; funcs `NewService`, `NewMCPServer`,
  `NewToolRegistry`, `NewAgentExecutor`, `AssembleSystemPrompt`,
  `DeniedToolNames`, `HTTPErrorFrom`, `RiskTier`; errors `ErrDisabled`,
  `ErrRateLimited`, `ErrProviderUnavailable`, `ErrUnknownEntity`,
  `ErrInsightNotCached`.
- **Internal deps:** `agent, audit, config, crm, database, httpx, logging,
  ratelimit, rbac`. **9 internal deps.**
- **External deps:** `github.com/google/uuid`, stdlib `net/http`.
- **Fake:** `Provider` is an interface with **two real implementations** but **no
  in-process fake**; tests use `httptest` servers or an unreachable address.
- **Tests:** 7 unit files (prompt assembly, confidence/grounding, masking, MCP
  protocol, provider error classification) + integration (`ai_assist_test.go`,
  `ai_assist_cache_test.go`, `ai_provider_outage_test.go`).
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/ai`, with
  `agent`, `audit`, `config`, `crm`, `database`, `httpx`, `logging`, `ratelimit`,
  `rbac` and `retention` as its dependencies, all proven. The smoke proves the
  context fence neutralises a forged record and the risk tiers are the explicit
  mapping. The earlier `no` understated it: binding to `crm` is not a blocker when
  `crm` is proven.
- **Cut lines:** the `Provider` interface and prompt/confidence logic are the
  portable part; the service binds to `crm`.

### internal/agent  — human-in-the-loop execution
- **Responsibility:** the approval state machine (ADR 0005). A proposed
  destructive action is recorded, approved/denied, then executed through the same
  permission-checked tool path.
- **Public API:** `Service`, `Executor` (interface), `ExecutionFunc`, `Config`,
  `Action`, `Run`, `Proposal`, `Approval`, `Decision`, `Decider`, `RiskTier`;
  funcs `NewService`, `ArgumentsHash`, `CanonicalArguments`.
- **Internal deps:** `audit, database, httpx`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `Executor` is the injection seam.
- **Tests:** `agent_test.go` (unit) + `testsupport/agent_approvals_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/agent`,
  with `audit`, `database` and `httpx` (and `httpx`'s own leaves) as its
  dependencies. Its argument-hash test runs in the extracted module. The
  `Executor` is supplied by the host application, so the seam travels with it.
- **Cut lines:** moves with `audit`/`database`; the `Executor` is supplied by the
  host application.

### internal/workflow  — automation engine
- **Responsibility:** declarative workflows: triggers (event/schedule/manual),
  steps (condition, branch, foreach, delay, actions), expression evaluation,
  run/step history, template catalog, JSON import/export.
- **Public API:** `Engine`, `Store`, `Config`, `Workflow`, `Definition`,
  `Step`, `Condition`, `Template`, `TemplateRef`, `CreateInput`, `Run`,
  `RunResult`, `StepRun`, `EvalContext`, `ExportDocument`; funcs `NewEngine`,
  `NewStore`, `NewEvalContext`, `ParseDefinition`, `ExportWorkflow`,
  `ImportWorkflow`, `ValidateCatalog`, `ParseCron`, `RegisterKnownEvents`,
  `Templates`, `TemplateByID`, `InputFromTemplate`; const
  `DefinitionSchemaVersion`; trigger kinds `TriggerEvent`, `TriggerEventID`,
  `TriggerManual`, `TriggerSchedule`.
- **Internal deps:** `audit, crm, database, httpx, logging, outbox, rbac`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** none in-process.
- **Tests:** `workflow` unit tests (2 files: cron, definition) +
  `testsupport/workflow_engine_test.go`, `workflow_templates_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/workflow`,
  with `audit`, `crm`, `database`, `httpx`, `logging`, `outbox` and `rbac` as its
  dependencies, all proven. The smoke registers the real event vocabulary and
  validates the catalog. Calling `crm.Service` directly is not a blocker when `crm`
  is proven.
- **Cut lines:** the definition/parse/template layer is separable from the
  execution engine.

### internal/simulate  — Simulation Lab
- **Responsibility:** "what if" scenarios against a CRM baseline. **Read-only:**
  preview and run never write CRM data, never enqueue jobs, never send webhooks.
- **Public API:** `Service`, `Scenario`, `Run`, `CatalogEntry`, `ParamSpec`,
  `CreateScenarioInput`, `RunInput`, `PreviewInput`, `PreviewResult`, `Catalog`,
  `NewService`.
- **Internal deps:** `analytics, audit, crm, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** none; reads through `crm.Service`.
- **Tests:** `testsupport/simulation_test.go` (integration) + frontend page tests.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/simulate`,
  with `analytics`, `audit`, `crm`, `database` and `rbac` as its dependencies, all
  proven. Reading `crm` is not a blocker when `crm` is proven.
- **Cut lines:** `analytics` is the portable core; `simulate` is the persistence +
  service wrapper.

### internal/forecast
- **Responsibility:** revenue forecasting from org history; freezes params,
  history and algorithm version into a run row.
- **Public API:** `Service`, `Run`, `RunInput`, `CatalogEntry`, `Catalog`,
  `NewService`.
- **Internal deps:** `analytics, audit, crm, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Tests:** `testsupport/intelligence_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/forecast`,
  with `analytics`, `audit`, `crm`, `database` and `rbac` as its dependencies.
- **Cut lines:** as `simulate`.

### internal/risk
- **Responsibility:** scan the CRM for explainable risk conditions; keep a live
  signal per (org, type) with cited evidence; manage thresholds.
- **Public API:** `Service`, `Signal`, `ListFilter`, `ScanResult`, `Thresholds`,
  `NewService`.
- **Internal deps:** `analytics, audit, crm, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Tests:** `testsupport/intelligence_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/risk`,
  with `analytics`, `audit`, `crm`, `database` and `rbac` as its dependencies.
- **Cut lines:** as `simulate`.

### internal/integrations  — adapter lifecycle
- **Responsibility:** external integration adapters (REST, webhook-out, SMTP
  probe, database descriptor). The outbound SSRF guard it used to own now lives in
  `internal/netguard`; the names below are re-exported for existing callers.
- **Public API:** `Hub`, `Adapter` (interface), `GuardedClient`, `Integration`,
  `Run`, `Schema`, `Table`, `Column`, `Record`, `ReadRequest`, `WriteResult`,
  `Status`, `CreateRequest`; funcs `NewHub`, `NewGuardedClient`,
  `ValidateOutboundURL`, `IsBlockedIP` (the last four re-export `netguard`);
  `ErrWriteNotSupported`.
- **Internal deps:** `audit, database, httpx, netguard, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `Adapter` is the seam; `webhookOutAdapter`/`restAdapter` are real
  implementations.
- **Tests:** `integrations` unit tests (URL validation) + integration. The SSRF
  tests moved to `netguard`.
- **Extract:** **`yes`** — extracted and proven by
  `scripts/extract-test/integrations`, with `audit`, `database`, `httpx`,
  `netguard`, `rbac` and `secrets` as its dependencies, all of them proven. The
  SSRF guard is already out to `netguard`; the rest lifts out with the keyring.
  **was:** `almost` — the SSRF guard is already out; the rest needs `secrets`
  and `audit`.
- **Cut lines:** the guard moved to `netguard`; the persistence does not.

### internal/events  — inbound events + webhook delivery
- **Responsibility:** accept inbound events, verify signatures/timestamps, and
  deliver outbound webhooks with retry budget, dead-letter and replay.
- **Public API:** `Gateway`, `Dispatcher`, `DeliverySender`, `Poster` (interface),
  `Subscription`, `Delivery`, `InboundEvent`, `CreateSubscriptionRequest`;
  funcs `NewGateway`, `NewDispatcher`, `NewDeliverySender`,
  `NewDeliverySenderWithPoster`, `NewPlainPoster`, `SignInboundForTest`,
  `CorrelationIDFrom`; consts `MaxInboundPayloadBytes`, `TimestampTolerance`.
- **Internal deps:** `audit, database, httpx, integrations, logging, outbox,
  rbac, secrets`. **8 internal deps.**
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `Poster` is the documented transport seam; `NewPlainPoster` is a
  test-only unguarded poster.
- **Tests:** `testsupport/webhook_sink_test.go`, `webhook_deadletter_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/events`,
  with `audit`, `database`, `httpx`, `integrations`, `logging`, `outbox`, `rbac`
  and `secrets` as its dependencies, all proven. The smoke pins the signature,
  tolerance and payload-cap contract.
- **Cut lines:** the `Poster` interface is the boundary.

### internal/notify  — email + Telegram
- **Responsibility:** SMTP email and Telegram delivery; link codes; command
  handling.
- **Public API:** `EmailService`, `TelegramService`, `Message`, `LinkRequest`,
  `Update`; funcs `NewEmailService`, `NewTelegramService`; vars `ErrDisabled`,
  `ErrTelegramDisabled`, `AllowedCommands`; const `LinkCodeTTL`.
- **Internal deps:** `audit, config, database, httpx, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, stdlib `net/smtp`.
- **Fake:** `Enabled()` tolerates nil; when disabled it is a no-op. **No realistic
  fake transport.**
- **Tests:** none in-package (no `_test.go`). This is a gap.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/notify`,
  with `audit`, `config`, `database`, `httpx`, `rbac` and `secrets` as its
  dependencies, all proven. The smoke proves a disabled transport reports off and
  refuses to send. The earlier "would need a `Mailer` interface" was wrong: the
  transports already tolerate a nil receiver and return `ErrDisabled`.
- **Cut lines:** would need a `Mailer` interface to be extractable.

### internal/authn  — authentication
- **Responsibility:** passwords (Argon2), sessions, TOTP MFA, recovery codes,
  login attempts and lockout, OIDC config surface.
- **Public API:** `Service`, `LoginRequest`, `LoginResult`, `Session`, `User`;
  funcs `NewService`, `ValidatePassword`, `CorrelationContext`; consts
  `MFATicketTTL`, `MinPasswordLength`, `ResetTokenTTL`, `StepUpWindow`.
- **Internal deps:** `audit, config, database, logging, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`,
  `github.com/pquerna/otp`, `.../totp`.
- **Fake:** none.
- **Tests:** `authn_test.go` (password policy) + `testsupport/concurrency_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/authn`,
  with `audit`, `config`, `database`, `httpx`, `logging`, `migrate`, `migrations`,
  `ratelimit`, `rbac`, `retention` and `secrets` as its dependencies, all proven.
  The earlier `no` said extraction was not sensible "until those move first"; they
  have all moved, so it now moves too. The smoke pins the password policy and the
  step-up window.
- **Cut lines:** moves with `rbac`, `secrets`, `database`, `config`, `migrate`.

---

## Backend — platform / infrastructure

### internal/rbac  — permission catalog
- **Responsibility:** the permission catalog and `Principal` with `Has`/`HasAll`/
  `HasAny`/`Require`. Must stay in sync with migration 0004.
- **Public API:** `Permission`, `Principal`, `Role`, `Service`; funcs
  `NewService`, `NewPrincipal`, `All`, `Known`; `ErrForbidden`.
- **Internal deps:** `database`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** `NewPrincipal` builds a principal from an explicit permission set —
  usable as a fake principal in tests.
- **Tests:** `principal_test.go` + `testsupport` (seed/catalog match).
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/rbac`,
  with `database` as its one dependency. The package is extracted whole rather
  than split: the docs once proposed splitting a pure catalog and `Principal` from
  the DB-backed service, but the catalog, the principal and the service share the
  permission type and already lift out together, so a split would be a change to
  the code and not to the boundary. The boundary is pinned in `archtest`: `rbac`
  may import `database` and nothing else in the module.
- **Cut lines:** moves with `database`.

### internal/audit
- **Responsibility:** append-only action trail (`audit_log`, DB-trigger enforced)
  and the security-event stream (`security_events`).
- **Public API:** `Logger`, `Entry`, `Action`, `SecurityEvent`; funcs `New`,
  `StrPtr`, `UUIDPtr`.
- **Internal deps:** `database, logging`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** none (writes to the DB). Uses the ambient transaction.
- **Tests:** `audit_test.go` (the action vocabulary, the validation ordering that
  keeps an unattributable row out of the trail, and the marshalling helpers) plus
  integration via `testsupport`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/audit`,
  with `database` and `logging` as its two dependencies. Its schema lives in
  migration 0001, so a real extraction would carry a migration with it; that is
  noted rather than hidden. What the harness proves is that the Go package has no
  coupling to the application.
- **Cut lines:** moves with `database` and `logging`.

### internal/database
- **Responsibility:** connection pool, `WithTx`, `TxFromContext`, error
  classification (`IsNoRows`, `IsUniqueViolation`, …), `Querier`.
- **Public API:** `DB`, `Tx`, `Querier` (interface), `Connect`, `ConnectWithPool`,
  `WithTx`, `TxFromContext`, `Is*` helpers, `DefaultMaxConns`, `MaxAllowedConns`.
- **Internal deps:** none.
- **External deps:** `github.com/jackc/pgx/v5`, `.../pgconn`, `.../pgxpool`.
- **Fake:** `Querier` is an interface (seam). No in-memory DB.
- **Tests:** `pool_test.go` (no server needed: the pool bound is checked before a
  connection is attempted).
- **Extract:** **`yes`** — a leaf with no in-module dependency; extracted and
  proven by `scripts/extract-test/database`. Its pool guard and transaction
  contract are testable with no server, which is what makes the extraction cheap.
  That it is the shared substrate is a fact about desirability, not feasibility,
  and the guarantee worth having is that it *can* lift out.
- **Cut lines:** it is the common infrastructure component.

### internal/httpx  — HTTP primitives
- **Responsibility:** RFC 9457 problem responses, list/pagination/sort, request
  ID + access-log + recovery + security-header middleware, trusted-proxy client
  IP resolution, the rate-limit middleware.
- **Public API:** `APIError`, `ErrorCode`, `ListResponse`, `Pagination`,
  `SortSpec`, `RateLimitOptions`, `SubjectFunc`, `TrustedProxies`; funcs
  `WriteError`, `WriteJSON`, `WriteJSONList`, `WriteJSONStatus`, `DecodeJSON`,
  `ParsePagination`, `ParseSort`, `NewError`, `NewListResponse`, `PathParam`,
  `RequestID`, `ClientIP`, `ClientIPFrom`, `TrustedProxiesFrom`, and the
  middleware constructors.
- **Internal deps:** `logging, ratelimit`.
- **External deps:** `github.com/go-chi/chi/v5`, `github.com/google/uuid`.
- **Fake:** `SubjectFunc` injects the rate-limit subject.
- **Tests:** 4 unit files, including the client-address trust tests.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/httpx`
  together with `logging` and `ratelimit`. It is the shared HTTP substrate, so it
  is not usually *desirable* to extract on its own, but it is now *possible*, and
  that is what keeps a handler from reaching into it and dragging the application
  layer back in.
- **Cut lines:** moves with `logging` and `ratelimit`.

### internal/ratelimit  — pluggable limiter
- **Responsibility:** fixed-window limiting with a `Store` abstraction.
  `MemoryStore` (single process, swept, bounded) and `RedisStore` (atomic Lua,
  multi-replica), plus fail-open/fail-closed policy.
- **Public API:** `Store` (interface), `Limiter`, `Policy`, `Decision`, `FailMode`,
  `MemoryStore`, `RedisStore`, `RedisOptions`; funcs `New`, `NewMemoryStore`,
  `NewRedisStore`; `ErrUnavailable`.
- **Internal deps:** none.
- **External deps:** `github.com/redis/go-redis/v9`.
- **Fake:** `MemoryStore` is a real, usable in-process implementation — the
  strongest existing "fake" in the codebase. No explicit `NoopStore`.
- **Tests:** `ratelimit_test.go` (memory, policies) + `redis_integration_test.go`.
- **Extract:** `yes` — **zero internal deps**; Redis is optional. The memory path
  was extracted, built and unit-tested in a clean module (the Redis path compiled
  but was not exercised there). See `EXTRACT_READY.md` (Candidate 2).
- **Cut lines:** the whole package moves; Redis is a build-time dependency of the
  file that constructs `RedisStore` only.

### internal/retention  — data lifecycle
- **Responsibility:** per-table retention windows, dry-run, batched delete,
  protected tables, status predicates, expiry-driven policies.
- **Public API:** `Runner`, `Policy`, `Options`, `Result`; funcs `New`,
  `Policies`, `ValidateWindows`; `ErrDisabled`.
- **Internal deps:** `audit, database`.
- **External deps:** none beyond stdlib.
- **Fake:** `DryRun` is the default posture; `Runner` is nil-safe.
- **Tests:** `retention_test.go` + `testsupport/retention_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/retention`,
  with `audit` and `database` as its dependencies. Its safety tests run in the
  extracted module.
- **Cut lines:** moves with `audit`/`database`.

### internal/netguard  — outbound SSRF guard
- **Responsibility:** the single place that decides whether an outbound request
  destination is safe: rejects loopback, private, link-local, multicast and
  unspecified addresses, and non-`http(s)` schemes, for the URL itself and for
  every resolved IP. Used by the Integration Hub and by the S3 object store.
- **Public API:** `GuardedClient`, `NewGuardedClient`, `ValidateOutboundURL`,
  `IsBlockedIP`; `ErrBlocked`.
- **Internal deps:** none — **standard library only.**
- **External deps:** none beyond stdlib.
- **Fake:** none needed; the decision is pure.
- **Tests:** `netguard_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/netguard`;
  the setup also asserts the `go.mod` stays free of any `require`.
- **Cut lines:** already cut. It is a leaf, so anything may depend on it and it
  depends on nothing.

### internal/objectstore  — object-storage provider layer
- **Responsibility:** the storage *mechanism*: the `ObjectStore` contract, the
  in-process `Memory` provider, the S3-compatible `S3` provider, and the pure
  helpers for object keys and content types. It does **not** decide who may read or
  write an object, and it does not touch the database — that is `storage.Service`.
- **Public API:** `ObjectStore` (interface), `Memory`, `S3`, `ObjectInfo`,
  `ObjectMeta`, `NormaliseContentType`, `IsAllowedContentType`; funcs `NewMemory`,
  `NewS3`; `ErrObjectMissing`.
- **Internal deps:** `netguard`.
- **External deps:** `github.com/google/uuid`, `github.com/minio/minio-go/v7`,
  `.../pkg/credentials`.
- **Fake:** `Memory` (`NewMemory`) is a **real in-process `ObjectStore`**, used by
  both tests and the local development profile.
- **Tests:** `contract_test.go` (the shared behaviour contract, run against
  `Memory`), `s3_contract_test.go` (the same contract against a real S3 wire
  protocol via `gofakes3`), `helpers_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/objectstore`.
- **Cut lines:** already cut, out of `internal/storage`. `storage.Service` depends
  on it; the dependency does not point back.

### internal/storage  — attachment service (authorization + metadata)
- **Responsibility:** attachments: upload/download orchestration, metadata rows,
  entity binding, tenant/rights checks, size/type limits, orphan reconciliation.
  Delegates the mechanism to `internal/objectstore`.
- **Public API:** `Service`, `Config`, `Document`, `UploadInput`,
  `ReconcileReport`; funcs `New`; `ErrNotConfigured`. The `ObjectStore`, `Memory`,
  `S3`, `ObjectInfo` and `ObjectMeta` names are re-exported as aliases so existing
  callers are unchanged.
- **Internal deps:** `audit, database, httpx, objectstore, rbac`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `objectstore.Memory` through the re-exported `NewMemory`.
- **Tests:** `storage_test.go` (key/extension/entity rules) +
  `testsupport/storage_s3_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/storage`,
  with `audit`, `database`, `httpx`, `objectstore` (and its `netguard`), `ratelimit`
  and `rbac` as its dependencies, all of them proven. Binding to
  `rbac`/`audit`/`database` by design is not a blocker when all three are proven.
  **was:** `no` — `Service` binds to `rbac`/`audit`/`database` by design; the
  portable half (`objectstore`) has already been lifted out.
- **Cut lines:** the provider layer moved to `internal/objectstore`; `Service` stays.

### internal/jobs
- **Responsibility:** the background job queue: enqueue, claim (`SKIP LOCKED`),
  retry, dead-letter.
- **Public API:** `Queue`, `Job`, `EnqueueRequest`, `NewQueue`, `ErrNotRetryable`.
- **Internal deps:** `database`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** none.
- **Tests:** `jobs_test.go` (status/kind vocabularies, the pre-write validation,
  and the retry backoff; the first in-package tests).
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/jobs`,
  with `database` as its dependency. Writing the backoff test found a real latent
  bug: the shift was clamped at 10, so the delay topped out at 1024s and the
  documented 30-minute ceiling was unreachable dead code; the clamp is now 11.
- **Cut lines:** moves with `database`.

### internal/outbox  — transactional outbox
- **Responsibility:** write events in the same transaction as the change; the
  worker drains them.
- **Public API:** `Store`, `Envelope`, `Event`, `NewStore`, `EventTypes`.
- **Internal deps:** `database, logging`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** none.
- **Tests:** `outbox_test.go` (event vocabulary).
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/outbox`,
  with `database` and `logging` as its two dependencies. Its own test runs in the
  extracted module.
- **Cut lines:** moves with `database`/`logging`.

### internal/secrets
- **Responsibility:** Argon2 password hashing, token hashing, HMAC signing,
  integration-secret encryption (keyring).
- **Public API:** `Keyring`, `Argon2Params`; funcs `HashPassword`,
  `HashPasswordWithParams`, `VerifyPassword`, `HashToken`, `SignPayload`,
  `VerifySignature`, `RandomToken`, `RandomNumericCode`, `ConstantTimeEqual`,
  `DefaultArgon2Params`, `NewKeyring`, `IsSensitiveKey`; `ErrInvalidHash`,
  `ErrPasswordMismatch`.
- **Internal deps:** none.
- **External deps:** `golang.org/x/crypto/argon2`.
- **Fake:** not applicable (pure crypto).
- **Tests:** `secrets_test.go` (20 tests: hashing, the malformed-hash path, the
  keyring, CSRF and webhook signatures).
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/secrets`.
- **Cut lines:** the whole package moves.

### internal/config
- **Responsibility:** environment parsing and validation for every subsystem.
- **Public API:** `Config`, `AIConfig`, `EmailConfig`, `OIDCConfig`,
  `RetentionConfig`, `StorageConfig`, `TelegramConfig`, `Load`.
- **Internal deps:** `database, httpx, retention`.
- **External deps:** none beyond stdlib.
- **Fake:** not applicable.
- **Tests:** `config_test.go`.
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/config`,
  with `database`, `httpx` and `retention` as its dependencies, all proven. Naming
  every subsystem's config is not a blocker when the packages it names are proven.
- **Cut lines:** a recipe generates its own trimmed config.

### internal/logging
- **Responsibility:** structured `slog` setup and redaction of sensitive keys;
  request-scoped actor/org/correlation.
- **Public API:** `New`, `Redact`, `IsSensitiveKey`, `WithActor`,
  `WithCorrelationID`, `WithOrganization`, `Actor`, `CorrelationID`,
  `Organization`.
- **Internal deps:** none.
- **External deps:** stdlib `log/slog`.
- **Fake:** not applicable.
- **Tests:** `logging_test.go` (redaction through `New`, `With` and nested
  groups; context helpers; level/format wiring).
- **Extract:** **`yes`** — extracted and proven by `scripts/extract-test/logging`.
- **Cut lines:** moves alone.

### internal/migrate / internal/migrations
- **Responsibility:** embedded SQL migrations and the migrate runner (`Up`/`Down`)
  over `golang-migrate`; `migrations.FS` is the embedded FS.
- **Public API:** `migrate.Up`, `migrate.Down`, `migrations.FS`.
- **Internal deps:** `migrate -> migrations`.
- **External deps:** `github.com/golang-migrate/migrate/v4` (+ postgres, iofs).
- **Fake:** not applicable.
- **Tests:** none in-package; applied in CI and integration.
- **Extract:** **`yes`** — `migrations` and `migrate` are each extracted and proven
  by `scripts/extract-test/migrations` and `scripts/extract-test/migrate`.
  `migrations` is a leaf in code but carries data as well: the `//go:embed`
  directive names `*.sql`, so the extraction must copy the SQL files — a Go-only
  copy fails to build, and the harness does copy them. The earlier `no` ("schema is
  application-wide") was wrong: the schema is data the binary embeds, not a
  dependency on the application.
- **Cut lines:** moves with the SQL it embeds.

### internal/openapi  — spec generation
- **Responsibility:** build `docs/api/openapi.json` from the live router and the
  `api.APIContract` table.
- **Public API:** `Build`, `Report`, `Routes`, `Route`, `Contract`, `Info`,
  `Server`, `Group`, `QueryParam`, `PathParamSchema`, `Permissions`, `PrefixAPI`,
  `TagFor`, `DefaultDescription`, `Anonymous`, `SessionOnly`.
- **Internal deps:** none (it is imported *by* `api` and `openapi-gen`).
- **External deps:** `github.com/go-chi/chi/v5`, `github.com/google/uuid`.
- **Fake:** not applicable.
- **Tests:** `openapi_test.go` (drift, access declarations).
- **Extract:** `no` — it describes this application's routes.
- **Cut lines:** a recipe gets its own generator or none.

### internal/wiring  — composition root
- **Responsibility:** the single place the dependency graph is assembled; returns
  a `Services` struct.
- **Public API:** `Build`, `Services`.
- **Internal deps:** 22 packages.
- **Tests:** none; exercised by every integration test.
- **Extract:** `no` — it is the opposite of a module.
- **Cut lines:** a recipe replaces it.

### internal/archtest  — dependency rules
- **Responsibility:** test-only enforcement of the layering rules (no package
  imports `api`; production does not import `testsupport`; core does not import
  composition/HTTP; every internal package is reachable from a command).
- **Public API:** test functions only.
- **Internal deps:** none.
- **Tests:** 5 tests, all reading the real graph via `go list`.
- **Extract:** `almost` (as a reusable governance tool).
- **Cut lines:** portable as a template; the module path is hard-coded.

### internal/testsupport  — test harness
- **Responsibility:** integration-test scaffolding: opens a scratch database,
  drops/rebuilds `public`, applies migrations.
- **Public API:** test helpers only.
- **Internal deps:** many (it composes the app).
- **Tests:** 15 integration files (`//go:build integration`).
- **Extract:** `no` — never ships in a binary.

---

## Frontend (`frontend/src`)

| Module | Responsibility | Key files | Depends on | Tests | Extract |
| --- | --- | --- | --- | --- | --- |
| **API clients** | one module per backend area over a shared `client.ts` | `api/client.ts`, `api/{auth,crm,imports,integrations,workflows,simulation,intelligence,ai,assist,documents,notifications,agent,admin}.ts` | backend OpenAPI shapes; `types/api.ts` | `api/client.test.ts` | `almost` (the client core is generic) |
| **Types** | hand-written response/request types | `types/api.ts` | — | typechecked | `almost` |
| **Auth hook** | session, permission gate | `hooks/useAuth.tsx`, `components/RequirePermission.tsx` | `api/auth.ts` | `RequirePermission.test.tsx` | `no` |
| **Search hook** | debounced global search | `hooks/useGlobalSearch.ts` | `api/*` | `GlobalSearch.test.tsx` | `almost` |
| **UI kit** | shared primitives (Card, Alert, Table, …) | `components/ui.tsx` | — | `ui.test.ts` | `almost` |
| **Shell/nav** | app frame and routing chrome | `components/AppShell.tsx`, `pages/ErrorPages.tsx` | auth | via E2E | `no` |
| **ResourceList / BulkArchive** | generic list + bulk actions | `components/ResourceList.tsx`, `BulkArchiveButton.tsx` | `api/crm.ts` | `.test.tsx` (2) | `almost` |
| **CRM pages** | accounts/contacts/leads/deals/tasks/tickets | `pages/{Accounts,Account,Contacts,Leads,Deals,Tasks,Tickets}Page.tsx` | `api/crm.ts` | E2E | `no` |
| **AssistPanel** | contextual AI surface | `components/AssistPanel.tsx`, `api/assist.ts` | `api/assist.ts` | `AssistPanel.test.tsx` | `almost` |
| **Workflow editor** | step editor + validation | `components/WorkflowEditor.tsx`, `pages/WorkflowsPage.tsx`, `api/workflows.ts` | `api/workflows.ts` | `WorkflowEditor.test.tsx` | `almost` |
| **Simulation page** | Simulation Lab UI | `pages/SimulationPage.tsx`, `api/simulation.ts` | `api/simulation.ts` | `SimulationPage.test.tsx` | `almost` |
| **Intelligence pages** | forecast + risk UI | `pages/{Forecast,Risk}Page.tsx`, `api/intelligence.ts` | `api/intelligence.ts` | `.test.tsx` (2) | `almost` |
| **AI/MCP page** | provider status, tool catalogue | `pages/AiPage.tsx`, `api/ai.ts` | `api/ai.ts` | E2E | `almost` |
| **Import pages** | Import Center UI | `pages/{ImportCenter,ImportDetail}Page.tsx`, `api/imports.ts` | `api/imports.ts` | E2E | `no` |
| **Integrations/Webhooks/Events/Jobs pages** | admin surfaces | `pages/{Integrations,Webhooks,Events,Jobs,Migrations}Page.tsx` + matching api clients | their api clients | E2E | `no` |
| **Admin pages** | audit, users, roles, orgs, teams, security, diagnostics, email, telegram | `pages/admin/*.tsx`, `api/admin.ts` | `api/admin.ts` | `SecurityPage.test.tsx` | `no` |
| **Attachments** | document upload/download | `components/AttachmentPanel.tsx`, `api/documents.ts` | `api/documents.ts` | `AttachmentPanel.test.tsx` | `almost` |

**Frontend totals:** 76 `.ts`/`.tsx` files; 13 unit test files (97 tests); E2E in
`frontend/e2e/platform.spec.ts` (43 passing, 1 conditionally skipped).

---

## Summary

- **Backend packages inventoried:** 33 (`internal/*`), plus 3 commands.
- **Frontend layers inventoried:** 16.
- **Cycles:** none (verified by `go list` + DFS; see `DEPENDENCY_MAP.md`).
- **Packages with ≥8 internal deps:** `api` (26), `wiring` (22), `importx` (10),
  `ai` (9), `events` (8); `cmd/forgecrm-worker` (17) and `cmd/forgecrm-api` (11)
  are composition roots, not modules.
- **Real in-process fakes that exist today:** `objectstore.Memory` (`NewMemory`,
  re-exported as `storage.NewMemory`), `ratelimit.MemoryStore`. Documented injection
  seams without a shipped fake: `ai.Provider`, `events.Poster`,
  `integrations.Adapter`, `migratex.Source`, `agent.Executor`,
  `database.Querier`, `objectstore.ObjectStore`, `ratelimit.Store`.
- **Extract status:** thirty-three modules are `yes` — `analytics`, `ratelimit`,
  `netguard`, `objectstore`, `secrets`, `logging`, `httpx`, `importx/safe`,
  `importx/parsers`, `database`, `rbac`, `audit`, `outbox`, `retention`, `agent`,
  `jobs`, `crm`, `storage`, `integrations`, `config`, `workflow`, `events`,
  `migratex`, `simulate`, `forecast`, `risk`, `notify`, `ai`, `importx`,
  `migrations`, `migrate`, `authn` and `constructor` —
  because they were
  actually extracted, built, tested and run in a clean module, now by the
  automated harness in `scripts/extract-test/`. The statuses here match
  `EXTRACT_READY.md` exactly.
- **Gaps found:** the packages with **no `_test.go` at all** are `audit`, `crm`,
  `events`, `forecast`, `jobs`, `migrate`, `migratex`, `migrations`, `notify`,
  `risk`, `simulate`, `wiring`. Of these, `crm`, `events`, `forecast`, `risk` and
  `simulate` have integration coverage through `internal/testsupport`; `audit`,
  `jobs`, `notify` and `wiring` are covered only indirectly. `logging` and
  `secrets` gained in-package tests in the extraction slice, which is what let
  them be marked `READY`. `ai` has two real `Provider` implementations but no
  in-process fake; there is no `NoopStore` in `ratelimit` and no `AllowAll`
  authorizer anywhere (authorization is enforced inline in `api` middleware plus
  `crm` ownership checks).
