# ForgeCRM — Project Audit

- **Audit date:** 2026-10-28
- **Auditor:** engineering agent (OpenHands)
- **Commit audited:** `main` @ `3b1ce12`
- **Repository:** https://github.com/geff1778/ForgeCRM

This records the **verified** state of the repository. It separates confirmed
facts from risks and proposals. Every claim below was checked against the code at
the audited commit; commands and results are recorded.

An earlier version of this document described the repository when it contained a
single 10-byte `README.md`. That state is superseded. The current document
describes what exists now.

---

## 1. Verified state

### 1.1 Repository and toolchain

| Item | Verified state | Evidence |
|---|---|---|
| Branch | `main`, direct push permitted | `git push origin main` succeeded |
| Go | 1.26.10 | `backend/go.mod` |
| Frontend | React 19, TypeScript, Vite 6 | `frontend/package.json` |
| Database | PostgreSQL 17 | `docker-compose.yml`, CI services |
| Migrations | 6 up / 6 down | `backend/internal/migrations/` |
| Backend test functions | 69 | `grep -c "^func Test"` |
| Frontend unit tests | 33 passing | `npx vitest run` |
| E2E scenarios | 11 declared, 22 assertions passing in Chromium | `npx playwright test` |
| CI jobs | 6, all green on `ff83452` and `3b1ce12` | GitHub Actions |

### 1.2 API surface

73 unique route paths across 144 registrations, mounted under `/api/v1` plus
`/health` and `/ready`. Groups: auth, system, CRM, admin, imports, migrations,
integrations, events, jobs, AI/MCP, notifications.

Verified by enumerating the router rather than reading documentation:

```
grep -rhoE '\b(Get|Post|Put|Patch|Delete)\("(/[^"]*)"' backend/internal/api/*.go | sort -u | wc -l
→ 73
```

### 1.3 RBAC

- **81 permissions** declared in `backend/internal/rbac`.
- **14 default roles**, seeded in `0004_rbac_seed.up.sql`: `platform_owner`,
  `platform_admin`, `tenant_admin`, `security_auditor`, `integration_admin`,
  `data_steward`, `executive`, `sales_manager`, `sales_representative`,
  `support_manager`, `support_agent`, `analyst`, `employee`, `readonly_auditor`.
- Roles are permission sets in the database, not conditions scattered through the
  code.
- Server-side enforcement via `requirePermission` middleware; `api` handlers call
  `s.perm(rbac.PermX)`.

### 1.4 Domain packages

| Package | Role |
|---|---|
| `crm` | Core: contacts, accounts, leads, deals, pipelines, tasks, activities, tickets, tags, custom fields, search, reports |
| `importx` | Parsers, safe archive extraction, mapping, execution |
| `migratex` | Source adapters (PostgreSQL, SQLite, REST), reconciliation |
| `integrations` | Adapter interface, hub, SSRF guard |
| `events` | Event gateway, webhook subscriptions and delivery |
| `notify` | SMTP and Telegram adapters, templates, send log |
| `ai` | Provider interface, Ollama and OpenAI-compatible, MCP tool registry |

Layering verified by `internal/archtest`: the core does not import an application
service; nothing imports `api`.

### 1.5 MCP

`internal/ai/mcp.go` implements **8 tools**: `search_accounts`,
`account_summary`, `list_tasks`, `pipeline_overview`, `create_task`,
`create_ticket`, `update_deal_stage`, `get_report`.

Tools call `crm.Service` methods directly — the same code path as HTTP, so
permissions and validation are not reimplemented. Mutating tools require
`confirm=true`.

### 1.6 Import and archive formats

Advertised by `/system/capabilities` and **covered by tests**:

| Category | Formats | Test status |
|---|---|---|
| Tabular | csv, tsv, json, jsonl, xlsx, xml | Tested |
| Archive | zip, tar, tar.gz, gzip | Tested |
| Archive | tar.bz2, tar.xz, bzip2, xz | Tested (added this cycle) |

`internal/importx/safe` enforces entry count, total bytes, per-entry bytes, nesting
depth and compression ratio, and rejects path traversal and absolute paths.

### 1.7 Error contract

Every error response carries RFC 9457 Problem Details members (`type`, `title`,
`status`, `detail`, `instance`, `retryable`) alongside the nested
`error: {code, message, details}` object. Both views derive from one `APIError`.
Documented in `docs/api/errors.md`.

---

## 2. What the previous cycle's report claimed — verified or corrected

The prior report is treated as a list of hypotheses. Each was checked:

| Claim | Verdict | Evidence |
|---|---|---|
| `main` was at `7cf51de` | **Correct at the time** | Superseded; `main` is now `3b1ce12` |
| `feat/platform-foundation` at `386ffb7` | **Correct** | Branch exists at that SHA |
| PRs #1, #2, #3 open | **Correct** | All three open, base `main` |
| PR #2 contains RFC 9457 handling | **Correct** | `httpx.go` emits the members |
| PR #3 contains configurable pool | **Correct** | `FORGECRM_DATABASE_MAX_CONNS` |
| CI green on those commits | **Correct** | Runs confirmed for each SHA |
| No full OIDC Authorization Code Flow | **Correct, and worse than stated** | See finding A |
| No OpenAPI specification | **Correct** | No OpenAPI, Swagger or equivalent file exists |
| No distributed rate limiter | **Correct** | `httpx.RateLimiter` holds `hits map[string]*window` in process memory; no Redis in `go.mod` |
| API and worker share codebase and DB | **Correct** | Both import `internal/crm` and connect to one database |
| No license chosen | **Correct** | No `LICENSE` file |

**Corrections to the prior report's framing:**

- The report described the MCP surface as a "server". It was then an HTTP endpoint
  speaking a custom JSON shape, not the MCP protocol. Finding B records the fix;
  the endpoint now speaks the protocol, and the admin UI's own shape is a separate
  endpoint.
- The report listed three PRs as containing "unique changes". They are one linear
  stack, so all three were integrated by a single fast-forward. See §3.

---

## 3. Findings

### A. `/system/capabilities` reports `oidc: true` when no OIDC flow exists — **Medium**

`backend/internal/api/system_handlers.go:80` sets `OIDC: s.deps.Config.OIDC.Enabled`,
which reflects the `FORGECRM_OIDC_ENABLED` environment variable. No authorization
code flow exists: there is no `golang.org/x/oauth2` or `coreos/go-oidc` dependency,
and no route handles a callback.

A client reading `capabilities` would conclude that corporate SSO is available. It
is not. This is exactly the failure mode the mission's rule 14 warns about.

**Status:** Fixed. The field is renamed `oidc_configured` and reports exactly what
it tests: that OIDC settings are present. `capabilities_test.go` fails if the field
is renamed back to `oidc`, so the misleading name cannot return. The
authorisation-code flow is still not implemented (Phase 2, item 2.1); the fix makes
the report honest, it does not add SSO.

### B. MCP is a custom JSON shape, not the MCP protocol — **Medium**

`POST /api/v1/mcp` accepts `{method, params, confirm}` and dispatches `tools/list`
and `tools/call`. It does not speak JSON-RPC 2.0, has no `initialize` handshake,
negotiates no `protocolVersion`, and is not reachable by any standard MCP client.

The tool registry, permission checks and audit are real and reused from the domain
layer, so the *logic* is sound. The *transport* is not MCP.

**Status:** Fixed. `POST /api/v1/mcp` now speaks JSON-RPC 2.0: `initialize` with
`protocolVersion` negotiation, `notifications/initialized`, `ping`, `tools/list`
and `tools/call`, with the specification's error codes for a parse failure, an
invalid request, an unknown method and invalid parameters. Notifications are
answered with an empty `202`. The protocol view publishes `inputSchema`; the
admin view keeps `input_schema`. `internal/ai/mcp_protocol.go` implements the
transport and `internal/api/ai_handlers.go` wires it. Verified by
`internal/ai/mcp_protocol_test.go` and, over the real HTTP stack with a real
session and database, by `internal/api/mcp_integration_test.go`. What remains is a
dedicated MCP credential (Phase 3, item 3.3): the transport authenticates with the
caller's browser session, so there is no separate revocable key with its own
read/write scope. See `docs/mcp/`.

### C. Rate limiting is per process — **Medium**

`httpx.RateLimiter` keeps counters in a `map[string]*window` guarded by a mutex,
in process memory. With N replicas the effective limit is N times the configured
value. Login, API, import and AI budgets all scale the same way.

There is no Redis dependency. This is the same multiplication problem as the
database pool, and it is the last thing preventing safe horizontal scaling.

**Status:** Open. Tracked as Phase 1 work.

### D. No OpenAPI specification — **High**

73 routes are described in prose only. The mission's target of a typed SDK,
generated clients and a contract test all depend on a machine-readable contract.
Without it, an API change cannot be detected as breaking.

**Status:** Open. Tracked as Phase 1 work; highest-value single item.

### E. `crm` imports `httpx` for error types — **Low**

29 uses of `httpx.APIError`, `httpx.Pagination` and `httpx.SortSpec` in the core
domain. A domain error type does not belong in a package named for HTTP.

The dependency is one-directional, so it is not a cycle and the rules are still
written once. It is a layering smell, not a defect.

**Status:** Open, bounded. `TestCoreDoesNotImportCompositionOrHTTP` pins the two
properties that must hold while the debt exists (`api` is never imported; `httpx`
never imports `crm`). Fixing it means extracting the error types to a neutral
package, which is a mechanical change with a large diff and no behavioural
benefit; it is deferred deliberately.

### F. No `LICENSE` file — **Blocker (owner decision)**

The distribution model is the owner's decision, not an engineering one. CI
deliberately does not require the file.

**Status:** Blocked on the owner.

### G. No consent model — **Medium**

`grep -ri consent backend/internal` returns nothing. No entity, no policy service,
no enforcement point. Any channel — email, Telegram, webhook — can send without
consulting a consent record.

**Status:** Open. Tracked as Phase 2 work.

### H. No tenant configuration mechanism — **Medium**

Tenant settings are not centralized. There is no versioned config schema, no
preview, no audit of configuration changes, no bootstrap beyond migrations.

**Status:** Open. Tracked as Phase 5 work.

---

## 4. Verified baseline results

Run at `3b1ce12` on the audit machine.

| Check | Command | Result |
|---|---|---|
| Backend format | `gofmt -l ./cmd ./internal` | 0 files |
| Backend vet | `go vet ./...` | clean |
| Backend build | `go build ./...` | ok |
| Backend unit | `go test -count=1 ./...` | all pass |
| Backend integration | `go test -count=1 -p 1 -tags=integration ./...` | all pass against PostgreSQL 17 |
| Frontend lint | `npx eslint .` | 0 errors, 4 pre-existing warnings |
| Frontend types | `npx tsc --noEmit` | clean |
| Frontend unit | `npx vitest run` | 33 pass |
| E2E | `npx playwright test` | 22 pass in real Chromium |
| CI | GitHub Actions on `ff83452`, `3b1ce12` | 6/6 jobs green |

### Known non-blocking issues

- 4 ESLint warnings about fast-refresh exports in files that export both a
  component and a helper. Cosmetic; fixing them means splitting those modules.
- Integration tests must run with `-p 1` because they share one test database.

---

## 5. Technical debt register

| # | Debt | Severity | Cost to fix | Status |
|---|---|---|---|---|
| 1 | No OpenAPI contract | High | Medium | Planned, Phase 1 |
| 2 | Per-process rate limiting | Medium | Medium | Planned, Phase 1 |
| 3 | MCP is not the MCP protocol | Medium | Medium | Planned, Phase 3 |
| 4 | `capabilities.oidc` misreports | Medium | Low | Planned, Phase 2 |
| 5 | No consent model | Medium | High | Planned, Phase 2 |
| 6 | No tenant configuration | Medium | High | Planned, Phase 5 |
| 7 | `crm` → `httpx` layering | Low | Medium | Deferred, bounded by test |
| 8 | 4 ESLint fast-refresh warnings | Low | Low | Deferred |
| 9 | No `LICENSE` | — | None | Blocked on owner |

---

## 6. Risks

| Risk | Impact | Mitigation in place |
|---|---|---|
| Horizontal scaling multiplies rate limits | Login and AI abuse limits are weaker than configured | Documented; Redis limiter is Phase 1 |
| API contract drift | A breaking change ships undetected | None yet; OpenAPI + contract test is Phase 1 |
| Client believes SSO is available | Integration effort wasted, security expectation unmet | Documented here; Phase 2 implements the flow |
| Consent absent | Messages can be sent without a lawful basis | Documented; Phase 2 |
| Archive handling is the main untrusted-input surface | A parsing flaw is remotely reachable | Limits, traversal rejection, ratio cap, and round-trip plus corrupt-input tests for all 8 formats |
| `crm` → `httpx` becomes a cycle | Module stops building | Pinned by `TestCoreDoesNotImportCompositionOrHTTP` |

---

## 7. Next priorities

1. **OpenAPI 3.1 from the real routes** plus a contract test and CI validation
   (finding D). Unblocks SDK, MCP and integrations.
2. **Distributed rate limiting** (finding C). The last scaling blocker.
3. **Real OIDC Authorization Code Flow with PKCE** and a test IdP (finding A).

Findings 1 and 2 are independent and touch different files, so they can proceed
in parallel. Finding 3 needs a test IdP first.
