# ForgeCRM Intelligence Platform — architecture and delivery plan

This document is the architectural plan for the intelligence layer that sits on
top of the existing ForgeCRM. It records what already exists, what this iteration
builds, what is deliberately deferred, and the criteria that decide when each
piece is done. It is written against the real repository, not an intended one.

The plan is a working document. When a section is finished it is marked so, and
the status line names the commit or the test that proves it.

---

## 1. Current state of the project

ForgeCRM is a self-hosted, modular-monolith CRM: Go backend (module
`github.com/geff1778/forgecrm`), PostgreSQL, React + TypeScript + Vite frontend
served same-origin by Nginx. Two binaries share one object graph built in
`internal/wiring`: `forgecrm-api` and `forgecrm-worker`.

Subsystems that already exist and are reused by this plan, verified in code:

| Subsystem | Package | What it gives the intelligence layer |
|---|---|---|
| CRM core | `internal/crm` | Entities, rules, permission-checked reads. `SalesSummary`, `SupportSummary`, `PipelineReportFor` already exist. |
| RBAC | `internal/rbac` | Permission catalog (`rbac.All()`), `Principal`, per-route `s.perm(...)`. |
| Audit | `internal/audit` | Append-only `audit_log` + `security_events`; `Record` uses the ambient transaction. |
| Database | `internal/database` | `DB.Q(ctx)` (transaction-aware), `WithTx`, `Querier`. |
| Jobs | `internal/jobs` | Durable queue with `EnqueuePeriodic`, advisory-lock dedupe, retries. |
| AI layer | `internal/ai` | Provider abstraction (`Provider`, `Complete`), permission-checked contextual assembly (`Assist`). |
| Outbox / events | `internal/outbox`, `internal/events` | Event vocabulary `outbox.EventTypes()`, dispatcher. |
| Workflow engine | `internal/workflow` | Definition validation, step→permission map, run history. |
| Storage | `internal/storage` | S3-compatible attachments. |
| OpenAPI | `internal/api/contract.go`, `internal/openapi` | Generated spec from the live router; `-check` gate in CI. |
| Arch rules | `internal/archtest` | Enforces the dependency arrows. |

### 1.1 Existing analytics capabilities (do not duplicate)

- `crm.SalesSummary(ctx, p, from, to)` — deals created/won/lost, won/lost/open
  value, average deal, win rate.
- `crm.SupportSummary(ctx, p)` — ticket totals, open/resolved, SLA breaches,
  average resolution hours, by-priority counts.
- `crm.PipelineReportFor(ctx, p, pipelineID)` — per-stage deal count, total and
  probability-weighted amount, won/lost totals.

These are the **baseline readers**. The analytics core consumes them; it does not
reimplement them.

### 1.2 Defects found during the audit

1. **Reports page reads fields the API does not return.** The backend returns
   `deals_won`, `won_value`, `open_value`, `win_rate`, `total`, `resolved`,
   `avg_resolution_hours` (confirmed in `docs/api/openapi.json`). The frontend
   types in `src/types/api.ts` declare `won_count`, `won_amount`, `opened`,
   `avg_resolution_minutes`. The page therefore renders `undefined` for every
   headline figure. Fixed in this iteration (§5.4).
2. **Money is carried as `float64`.** `deals.amount` is `numeric(18,2)` (exact),
   but the report structs scan into `float64`. The mission forbids `float64` for
   money. The analytics core introduces an exact `analytics.Money` type; the
   existing report structs are left as-is for API compatibility but are not used
   for simulation arithmetic.

### 1.3 Partially implemented / honest gaps

- The AI layer is provider-agnostic but there is **no** general "run a structured
  model call and parse a typed result" helper; the contextual path is text-in /
  text-out. This iteration does not need one, so it is not built.
- There is no forecasting, no risk scoring, no data-quality scoring, no document
  extraction pipeline. These are roadmap items (§9).

---

## 2. Target architecture

```
                    ┌───────────────────────────────────────────────┐
   HTTP /api/v1     │  internal/api  (composition layer)            │
                    │   - route registration + permission per route  │
                    │   - contract table → OpenAPI                   │
                    └───────────────┬───────────────────────────────┘
                                    │ depends on
              ┌─────────────────────┼───────────────────────┐
              ▼                     ▼                       ▼
    ┌──────────────────┐  ┌──────────────────┐  ┌────────────────────┐
    │ internal/simulate│  │ internal/crm      │  │ internal/ai        │
    │  Scenario Lab    │  │  baseline readers │  │  provider layer    │
    │  service         │  │  (funnel, deals)  │  │                    │
    └────────┬─────────┘  └──────────────────┘  └────────────────────┘
             │ uses
             ▼
    ┌──────────────────────────────────────┐
    │ internal/analytics  (pure core)       │
    │  - Money (exact, integer cents)       │
    │  - deterministic scenario maths       │
    │  - no DB, no HTTP, no clock, no AI    │
    └──────────────────────────────────────┘
```

### 2.1 Module responsibility and boundaries

- **`internal/analytics`** — the pure core. Exact money arithmetic, the funnel and
  revenue maths, parameter validation, and result contracts. It imports **only**
  the standard library. No database, no HTTP, no AI, no `time.Now()`. Everything
  here is deterministic and unit-testable with no infrastructure. This is the
  boundary that makes the maths reviewable and reproducible.
- **`internal/simulate`** — the application service. It reads a baseline from the
  CRM through `crm.Service` with the caller's principal (so tenant isolation and
  object permissions are enforced by the reader, not re-implemented), calls the
  pure core, persists runs and scenarios, and writes audit entries. It depends on
  `crm`, `analytics`, `audit`, `database`, `rbac`, `httpx`. It is not imported by
  the core.
- **`internal/api`** — routes, request/response DTOs, contract rows.
- **frontend** — a Simulation Lab page in the existing design system.

This respects the existing arch rules: `analytics` is a leaf like `crm`; `simulate`
is an application service that may depend on the core.

### 2.2 Why a new package and not "more methods on crm.Service"

The maths must be testable without a database, must not touch the clock, and must
not be able to read data on its own. `crm.Service` has a database handle and reads
rows; putting pure arithmetic there would make the arithmetic depend on the schema
and would let a scenario accidentally write. A separate leaf package with a
one-way dependency keeps the arithmetic auditable and keeps the "simulation never
writes" property structural rather than a convention.

---

## 3. Data model and migrations

New tables (migration `0019_simulation_lab`), all tenant-scoped:

- `simulation_scenarios` — a saved scenario: organization, owner, name, the
  scenario `kind`, the parameter set as `jsonb`, the algorithm version, timestamps,
  `archived_at` for soft delete. Unique `(organization_id, name)` among live rows.
- `simulation_runs` — one execution of a scenario: organization, scenario id,
  the **frozen** parameter set and baseline snapshot (`jsonb`), the algorithm
  version, the exact result (`jsonb`), created-by, `created_at`. Immutable.

Money is stored in `jsonb` results as **integer minor units** (`amount_minor` +
`currency`) plus a display string, never as a float, so a stored result is exact
and a reload cannot drift.

Indexes: `simulation_scenarios (organization_id, owner_user_id)`,
`simulation_runs (organization_id, scenario_id, created_at DESC)`.

`up`/`down` migrations are added as a numbered pair. No existing migration is
edited.

Permissions (same change as the Go catalog): `simulation.read`,
`simulation.manage`, `simulation.run`, seeded to roles by a `seed_grant`-style
insert, mirroring migrations 0006/0010/0013.

---

## 4. API plan

All under the existing authenticated group; every route declares a permission.

| Method | Path | Permission | Purpose |
|---|---|---|---|
| GET | `/api/v1/simulation/scenarios` | `simulation.read` | List the catalog + the org's saved scenarios |
| POST | `/api/v1/simulation/scenarios` | `simulation.manage` | Save a scenario |
| GET | `/api/v1/simulation/scenarios/{id}` | `simulation.read` | Open a saved scenario |
| DELETE | `/api/v1/simulation/scenarios/{id}` | `simulation.manage` | Archive a scenario |
| POST | `/api/v1/simulation/scenarios/{id}/runs` | `simulation.run` | Run a saved scenario, persist the result |
| GET | `/api/v1/simulation/scenarios/{id}/runs` | `simulation.read` | Run history for a scenario |
| POST | `/api/v1/simulation/preview` | `simulation.run` | Run an unsaved parameter set (no persistence) |

Every request body and response is named in `internal/api/contract.go`, and the
OpenAPI document is regenerated with `go run ./cmd/openapi-gen`.

---

## 5. Simulation Lab — the vertical slice

### 5.1 Scenarios (three, all real)

**Funnel** (`funnel`). Inputs: a per-stage conversion multiplier or an explicit
conversion override, and an optional lead-volume multiplier. Baseline: live counts
of deals per pipeline stage and the deal amounts. Output: expected closed-deal
count and expected won value for baseline and variant, plus the delta. The maths
is a stage-by-stage expected-value walk; it is a **deterministic calculation on
stated assumptions**, never called a statistical forecast.

**Revenue** (`revenue`). Inputs: average-ticket multiplier, win-rate multiplier,
deal-count multiplier. Output: expected revenue for baseline and variant and the
relative change. Uses exact money.

**Retention** (`retention`). Inputs: an assumed churn rate (a parameter the user
sets, with a default and an explicit range), and the retained-value basis. The
result is a **parametric** estimate under stated assumptions. The UI and the docs
label it as a parameterised model, not a trained prediction. If the org has no
closed/won history the run reports "insufficient data" instead of a number.

### 5.2 Correctness properties (tested)

- Simulation never writes CRM rows. The service has no CRM write path; the reader
  is read-only. An integration test asserts deal/task/activity counts are
  unchanged across a run.
- Simulation never triggers workflows, webhooks, notifications or emails.
- Determinism: identical input + parameters + algorithm version ⇒ identical
  result. A unit test runs the core twice and compares.
- Zero denominators, negative inputs, out-of-range parameters and empty samples
  produce explicit validation errors, not NaN/Inf.
- Sample size, parameter ranges and per-run row limits are bounded.

### 5.3 Reproducibility and versioning

Each result records `algorithm_version`. The core exposes its version as a
constant; a run stores it. A stored run's `params` and `baseline` are frozen, so a
re-run after a code change is comparable: the difference is attributable to the
version.

### 5.4 Interface

A new **Simulation Lab** entry in the Workspace navigation, using the existing
`ui.tsx` components (PageHeader, Card, Stat, Field, Alert, ErrorState, ConfirmDialog).
States: loading, empty, error, no-provider-not-needed, and validation errors. The
page shows the formula and assumptions for the chosen scenario, and a saved-run
history with re-open and re-run.

The Reports page contract mismatch (§1.2) is fixed here: `src/types/api.ts` is
corrected to the real field names and `ReportsPage.tsx` is updated to match.

---

## 6. Security and performance risks

- **Tenant isolation.** Every read goes through `crm.Service` with the caller's
  principal; every write filters `organization_id`. Tested with a two-org case.
- **Object authorization.** A saved scenario is readable/removable only within its
  organization; a cross-org id returns not-found.
- **No secrets to any model.** This slice does not call a model at all. When AI
  assistance is layered on later, it uses the existing `ai.Service` path which
  masks personal data.
- **Resource limits.** Request bodies are size-capped by the existing decoder;
  parameter ranges and result row counts are bounded in the core; the query is
  bounded by the caller's period and org.
- **Performance.** No claim of load testing is made. The baseline reader is
  aggregate SQL over indexed columns; the result is computed in memory from
  bounded aggregates.

---

## 7. Implementation sequence

1. `internal/analytics` (pure core) + unit tests. *(done — §status)*
2. `crm` baseline reader for the funnel/revenue inputs.
3. `internal/simulate` service + persistence.
4. Migration 0019 + permission seed; Go catalog.
5. API routes + DTOs + contract + OpenAPI regen.
6. Frontend page + types + nav.
7. Reports page fix.
8. Integration tests (PG), frontend tests, E2E.
9. Docs.

## 8. Definition of done for this iteration

1. Three working scenarios with real or clearly-labelled data.
2. Save and re-open works; results are stored exactly and are reproducible.
3. Simulation provably does not modify CRM data (integration test).
4. Algorithmic unit tests with known expected values; boundary tests.
5. Backend integration tests on PostgreSQL; E2E for create→run→save→re-open.
6. No CRM regression: existing suites stay green.
7. Docs and roadmap match the code.
8. `gofmt`, `go vet`, `go test`, OpenAPI `-check`, frontend typecheck/lint/build.

## 9. Not in this iteration (roadmap with criteria)

| Direction | Status | Ready when |
|---|---|---|
| Forecasting | **Delivered** | A time-split evaluation harness exists (`internal/analytics`), the method's out-of-sample error is reported next to the naive baseline it must beat, MAE is a first-class result, and insufficient history is reported rather than a fabricated figure. See §10. |
| Risk Lab | **Delivered** | Each signal cites a type, a severity and the evidence rows behind it; thresholds are editable and validated server-side; a scan reconciles open signals against the live CRM. See §10. |
| Data Quality | Deferred | Duplicate/missing/conflict detection with a transparent score and a preview-before-apply path. |
| Document Intelligence | Deferred | Extraction of structured fields from a supported type, required-field checks, human confirmation before a CRM write. |
| Knowledge Graph | Deferred | Edges derived from existing FKs only, with provenance and per-object access control. |
| Security Intelligence | Deferred | Anomaly detection over `security_events` with an explanation per signal and a suspicious-vs-confirmed distinction. |
| AI Workflows | Partially exists | Uses the workflow engine; needs an AI-tool step type with step/time limits and HITL confirmation. |
| MySQL/MariaDB adapter | Deferred | See ADR 0008; measured blockers documented. |

## 10. Status

- **§1 audit** — done, this document.
- **§2–§5 analytics core + Simulation Lab** — done. The pure core
  (`internal/analytics`, exact money and ratios, `simulate-v1`) landed first; the
  `crm` baseline reader, the `internal/simulate` service, migration
  `0019_simulation_lab` with the three permission seeds, the API surface
  (scenario CRUD, run, preview, a session-only catalog) and the OpenAPI
  regeneration followed. The frontend is a `Simulation Lab` entry in the
  Workspace navigation (`frontend/src/pages/SimulationPage.tsx`,
  `frontend/src/api/simulation.ts`).
- **Tests** — `internal/testsupport/simulation_test.go` (PostgreSQL) covers
  scenario CRUD and archiving, the per-organization unique name, parameter
  validation on save, tenant isolation, the permission check inside the service,
  a hand-computed funnel result, preview not persisting, insufficient-data being
  reported, reproducibility, the down migration, and the read-only property
  (deals/tasks/activities/jobs/outbox unchanged across a run).
  `frontend/src/pages/SimulationPage.test.tsx` covers the page; the Chromium
  suite (`e2e/platform.spec.ts`) covers create → save → run → history and the
  preview path against the real stack.
- **Defect found by the tests** — `avg(amount)` over `numeric(18,2)` returns a
  higher scale (`500.0000000000000000`), which the exact money parser refuses
  rather than truncating; every revenue and retention scenario failed at run
  time. The baseline reader now rounds the average to the money scale where it is
  produced. The parser stays strict.
- **§6 Forecasting & Risk Lab** — delivered. The pure cores live in
  `internal/analytics` (`forecast-v1`; methods naive, mean, moving average, trend,
  bounded by `MinHistoryPeriods=3`, `MaxHistoryPeriods=240`, `MaxForecastHorizon=36`)
  and a risk-signal core. The services `internal/forecast` and `internal/risk`
  read the CRM through `crm.Service` under the caller's principal and persist to
  `forecast_runs`, `risk_signals` and `risk_thresholds` (migration
  `0020_forecasting_and_risk`) with the `forecast.read`/`forecast.run` and
  `risk.read`/`risk.manage` permissions. The API adds forecast runs and a catalog,
  and risk signals, scan, status transitions and thresholds; the frontend adds
  `Forecasting` and `Risk` pages (`frontend/src/pages/ForecastPage.tsx`,
  `RiskPage.tsx`, `frontend/src/api/intelligence.ts`).
- **Forecasting design rule** — a projection is never shown alone: the observed
  series, the measured out-of-sample error and the naive baseline sit beside it,
  and a method that fails to beat the baseline says so. Too-short history is an
  error, not a number.
- **Tests** — `internal/testsupport/intelligence_test.go` (PostgreSQL) covers the
  reader SQL, scan reconciliation against real rows, the partial unique index on
  live signals, the permission seeds and the two service-side validation gaps
  found while writing them (a too-short forecast window is refused before compute;
  a threshold out of the column's range returns 422 with a reason). The Chromium
  suite covers compute → accuracy → observed series and scan → thresholds against
  the real stack.
- **Defect found by the E2E suite** — the API binary assembled `api.Deps` without
  copying the `Forecast` and `Risk` services `internal/wiring` builds, so every
  intelligence handler dereferenced a nil service and the recovery middleware
  answered an opaque 500. The dependency is now copied, and the routes are mounted
  behind a guard that answers 503 naming the subsystem when a service is missing,
  so the same mistake is legible next time (`TestIntelligenceRoutesRefuseWhenUnwired`).

<!-- STATUS: updated per commit. -->
