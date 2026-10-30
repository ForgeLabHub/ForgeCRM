# ForgeCRM — Target Architecture

> Status: **living document**. Sections marked _(planned)_ describe agreed design that is
> not yet implemented. See `ROADMAP.md` for implementation status per module.

## 1. Principles

1. **One stable foundation, many adaptable solutions.** A single deployment serves CRM,
   imports, integrations and AI, each independently switchable.
2. **Works without AI, without external APIs, without optional integrations.** Optional
   subsystems degrade to `disabled`; the core never depends on them.
3. **Modular monolith.** One process family, one schema, hard package boundaries.
   Extractable to services later; not prematurely distributed.
4. **Explicit transactional boundaries.** Anything that writes data *and* emits an event
   does so inside one transaction via the outbox.
5. **Idempotency by default.** Repeatable operations carry an idempotency key.
6. **Permissions are data, checked on the server.** No UI-only gating.
7. **No cyclic dependencies.** Dependency direction is strictly `api → domain → platform`.

## 2. Component map

```
                    ┌──────────────────────────┐
   browser  ───────▶│  nginx (reverse proxy)   │
                    └───────┬──────────┬───────┘
                            │          │
                    ┌───────▼───┐  ┌───▼─────────┐
                    │ frontend  │  │  forgecrm-api│  Go, chi
                    │ (static)  │  │  /api/v1     │
                    └───────────┘  └───┬──────────┘
                                       │  same schema, same packages
                                 ┌─────▼─────────────┐
                                 │ forgecrm-worker   │  Go, jobs + outbox drain
                                 └─────┬─────────────┘
                                       │
                              ┌────────▼────────┐
                              │  PostgreSQL 17  │  state, jobs, outbox, audit
                              └─────────────────┘
                                       ▲
                              (optional) S3-compatible object store for import artefacts
```

### Backend package layout

```
backend/
  cmd/forgecrm-api        API entrypoint
  cmd/forgecrm-worker     worker entrypoint
  internal/
    config        env-driven configuration, validation, redaction
    logging       slog setup, request/correlation IDs, secret redaction
    database      pgx pool, tx helpers
    migrate       embedded migration runner (golang-migrate)
    migrations    *.sql, embedded
    httpx         middleware, error envelope, pagination, JSON helpers
    authn         password hashing, sessions, CSRF, MFA, lockout
    rbac          permission catalog, role defaults, enforcement helpers
    audit         append-only audit trail
    jobs          Postgres-backed queue (SKIP LOCKED)
    outbox        transactional outbox
    crm           contacts, accounts, leads, deals, pipelines, tasks,
                  activities, tickets, tags, custom fields, search, reports
    importx       parsers, safe archive extraction, mapping, execution
    migratex      source adapters (Postgres, SQLite, REST) + reconciliation
    events        event gateway, webhook subscriptions + delivery
    integrations  adapter interface, hub, SSRF guard
    notify        SMTP email + Telegram adapters, templates, send log
    ai            provider interface, Ollama + OpenAI-compatible, MCP tools
    analytics     pure scenario maths: exact money, ratios, funnel/revenue/retention
    simulate      Simulation Lab: saved scenarios, runs, catalog, API service
    api           router and handlers (thin; no business logic)
```

Dependency rule: `api` may import `crm`, `importx`, …; those may import `audit`, `jobs`,
`rbac`, `database`; **none** of the platform packages may import `api`. No domain package
imports another domain package except through an interface it declares itself.
`analytics` is the exception that proves the rule: it imports nothing but the standard
library, so the arithmetic can be reviewed and unit-tested with no database, no clock and
no service. `simulate` depends on `analytics` and on `crm` for the read-only baseline.

## 3. Identity, sessions, authorization

- **Passwords**: Argon2id, per-user random salt, tuned parameters stored per-hash so they
  can be raised later.
- **Sessions**: opaque 256-bit random token. The database stores only a SHA-256 hash of
  the token, never the token itself. Delivered in a `HttpOnly`, `SameSite=Lax`,
  `Secure` (in production) cookie. No tokens in `localStorage`.
- **CSRF**: double-submit token bound to the session, required on all state-changing
  requests. Login is additionally protected by the same mechanism.
- **Session fixation**: a fresh session token is issued on successful authentication.
- **MFA**: TOTP (RFC 6238), verified as a second factor; recovery codes hashed.
- **Brute-force**: per-account failed-attempt counter with lockout window, plus per-IP
  rate limiting. Counters live in a `ratelimit.Store`: `MemoryStore` for one process,
  `RedisStore` for several replicas. Authentication uses fail-closed, general API traffic
  fail-open.
- **Client address**: resolved once per request by `ClientIPMiddleware` from the
  connection address, unless the peer is listed in `FORGECRM_TRUSTED_PROXIES`. Forwarding
  headers are not read otherwise, so a caller cannot choose the address it is limited or
  logged under.
- **Bootstrap**: the first platform owner is created by an explicit, one-time
  `bootstrap` command that requires an operator-supplied password (or generates a strong
  random one printed once). **No default credentials ship in the code.**
- **RBAC**: `permissions` (catalog) × `roles` (named sets) × `role_permissions` ×
  `user_roles`. Enforcement is a server-side `RequirePermission("deals.update")` on every
  handler. Record-level rules (owner / team) are applied as query predicates.
- **Tenant isolation**: every tenant-scoped table carries `organization_id`; every query
  in the tenant context includes that predicate. Cross-tenant access tests are part of
  the suite.

## 4. Data model (high level)

Core: `organizations`, `users`, `memberships`, `teams`, `team_members`, `roles`,
`permissions`, `role_permissions`, `user_roles`, `sessions`, `mfa_factors`,
`password_reset_tokens`, `login_attempts`.

CRM: `accounts`, `contacts`, `leads`, `pipelines`, `pipeline_stages`, `deals`,
`deal_stage_history`, `tasks`, `activities`, `tickets`, `sla_policies`, `contracts`,
`documents`, `tags`, `taggables`, `custom_field_definitions`,
`comments`, `notifications`, `approvals`.

Platform: `audit_log`, `jobs`, `outbox_events`, `events_inbox`, `webhook_subscriptions`,
`webhook_deliveries`, `integrations`, `integration_runs`, `import_jobs`,
`import_templates`, `import_row_errors`, `export_jobs`, `migration_jobs`,
`migration_reconciliation`, `email_log`, `telegram_links`, `ai_tasks`.

All external identifiers live in dedicated `external_id` / `external_source` columns with
a unique constraint scoped to `(organization_id, external_source, external_id)` — never
reused as primary keys.

## 5. Import & Migration Center

Pipeline: **source → detect format → safe extract → parse → infer schema → map → validate →
dry-run → execute (batched, checkpointed) → report → reconcile**.

Untrusted-input rules enforced in code, not documentation:

- input size cap, total-uncompressed-size cap, entry-count cap, nesting-depth cap;
- decompression-ratio (zip-bomb) guard;
- every entry path is normalised and rejected if absolute, containing `..`, or escaping
  the extraction root; symlinks/hardlinks are never followed;
- extraction streams to a private temp dir, never an attacker-chosen location;
- checksum verification on archive entries where the format provides one;
- SQL inside dumps is **never auto-executed**;
- temp dirs are always removed, including on error;
- the original file's SHA-256, the processing config and the final report are recorded.

Migration sources default to **read-only** access. Reconciliation compares row counts and
where possible checksums, and reports divergence. Incremental sync uses a watermark
column; CDC is a documented adapter slot, not implemented this cycle.

## 6. Events, webhooks, jobs

- **Outbox**: domain writes and event insertion share one transaction; a worker drains the
  outbox to the dispatcher. Delivery is *at-least-once*; consumers must be idempotent.
  The brief's "no exactly-once over the network" constraint is honoured explicitly.
- **Envelope**: `event_id`, `event_type`, `occurred_at`, `source`, `schema_version`,
  `correlation_id`, `tenant_id`.
- **Inbound**: HTTP endpoints with schema validation, HMAC signature, timestamp window,
  replay protection, idempotency keys, size and rate limits, versioned types, audit.
- **Outbound**: subscriptions with HMAC-signed payloads, exponential backoff, delivery
  history, manual replay, dead-letter view, disable/revoke.
- **Loop prevention**: events carry their origin; a delivery that originated from an
  external system is not re-emitted to that same subscription.
- **Jobs**: one Postgres table, `FOR UPDATE SKIP LOCKED`, attempt counters, exponential
  backoff, cancellation, checkpoint column for resumable imports. An hourly maintenance
  tick enqueues the periodic sweeps idempotently (`EnqueuePeriodic` takes an advisory lock
  and inserts only when the kind is not already queued or running): `maintenance.retention`
  prunes rows past their window, `agent.sweep_expired` closes approval requests nobody
  decided, `maintenance.import_temp` reclaims extraction directories an import
  abandoned when its worker died, `maintenance.import_files` reclaims uploaded files no
  `import_jobs` row references, and `maintenance.storage_reconcile` repairs a
  disagreement between the object store and the `documents` table. The last two are file
  and object maintenance rather than row retention, so neither is part of the retention
  registry; both inherit the retention engine's dry-run posture and delete nothing on a
  deployment that has not opted into deletion.

## 7. Integration hub

Adapter lifecycle interface:

```go
type Adapter interface {
    ValidateConfig(ctx context.Context) error
    TestConnection(ctx context.Context) error
    DiscoverSchema(ctx context.Context) (Schema, error)
    ReadRecords(ctx context.Context, req ReadRequest) (RecordStream, error)
    TransformRecords(ctx context.Context, in RecordStream, m Mapping) (RecordStream, error)
    WriteRecords(ctx context.Context, in RecordStream) (WriteResult, error) // only if allowed
    GetStatus(ctx context.Context) (Status, error)
    Close() error
}
```

Cross-cutting: per-operation timeouts, retry with backoff, circuit breaker, idempotency,
concurrency limits, quotas, secret masking in every log/response, config-change audit.

**SSRF guard** for all outbound calls: resolve DNS and reject loopback, link-local,
private, multicast and cloud-metadata ranges (169.254.169.254 / fd00:ec2::254); re-check
after every redirect; cap redirects; refuse non-HTTP(S) schemes.

## 8. AI & MCP

- `ai.Provider` interface with `OllamaProvider` (local) and `OpenAICompatibleProvider`.
  Provider selection, timeouts, request caps and cost metadata are configuration; the CRM
  works with AI entirely disabled.
- MCP tools (`search_accounts`, `account_summary`, `list_tasks`, `pipeline_overview`,
  `create_task`, `create_ticket`, `update_deal_stage`, `get_report`) are thin
  wrappers over the **same** domain services and the **same** authorization checks
  as the REST API. No raw SQL, shell, filesystem or unbounded export is exposed.
- The transport is JSON-RPC 2.0 (`internal/ai/jsonrpc.go`, `mcp_protocol.go`):
  `initialize` with protocol-version negotiation, `ping`, `tools/list`, `tools/call`,
  and notifications answered with an empty `202`. A standard MCP client can connect.
  `MCPServer` wraps `ToolRegistry` rather than reimplementing it, so the protocol
  cannot grant more authority than the registry enforces. A tool that changes data
  requires `confirm: true`, and the requirement is published in the tool's
  `inputSchema` so a client sees it before calling. The admin UI keeps its own
  shape at `POST /api/v1/mcp/call`, which is not the protocol. Authentication is
  the caller's session; a dedicated scoped credential is Phase 3, item 3.3.
- Prompt-injection stance: content found in documents, emails, imported rows or external
  responses is **data**. It never changes the agent's permission set, tool allowlist or
  system instructions. Tool calls are authorized per-caller, not per-content.

## 9. Observability

Separated streams: application logs, user audit trail, auth/security log, job history,
integration log, event/webhook history, metrics. Structured JSON logs with levels
`debug|info|warn|error`, correlation IDs, and field-level redaction of passwords, tokens,
keys and sensitive payloads. `/metrics` is only exposed in a protected context. Logs are
size-capped, rotated and retention-policied; the public API cannot read arbitrary files,
run OS commands or dump unbounded internal logs.

## 10. Rust: decision

**Not used in this cycle.** The brief explicitly forbids creating a Rust service just to
use a second language, and requires a stated reason, interface, maintenance cost and
verification criteria per extracted service. The candidate with the strongest case is
isolated untrusted-archive processing; it is deferred because:

- the Go implementation already enforces the full safety envelope with tests;
- a second toolchain roughly doubles build/CI/deployment surface for one module;
- the performance ceiling is not yet measured, so extraction would be premature.

Entry criteria for revisiting: a measured profile showing archive/transform work as the
dominant cost, *and* a requirement that cannot be met inside the resource limits already
enforced in Go.
