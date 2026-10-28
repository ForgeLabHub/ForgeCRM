# ForgeCRM — Master Plan

- **Status:** living document
- **Owner:** project owner
- **Last updated:** 2026-10-28
- **Architecture:** hybrid / modular monolith — see `docs/adr/0001-hybrid-architecture.md`

This is the strategy document. For what is actually built and verified, see
`docs/PROJECT_AUDIT.md`. For what is next, see `docs/ROADMAP.md`.

Status vocabulary used throughout the docs:

| Status | Means |
|---|---|
| **Implemented** | Code exists, compiles, and is reachable from a running process. |
| **Tested** | Covered by passing automated tests, including negative cases. |
| **Verified** | Tested **and** exercised against a real dependency (database, browser, provider) with the command and result recorded. |
| **Partial** | Some of the described behaviour exists; the gap is stated. |
| **Planned** | Designed, not built. |
| **Blocked** | Cannot proceed without something outside our control. The blocker is named. |
| **Deprecated** | Superseded; kept only for migration. |

A feature is never described as Verified because a package, page or config flag
carries its name. Verification requires recorded evidence.

---

## 1. Product definition

ForgeCRM is a self-hosted, open-source CRM platform with a modular architecture,
built for:

- sales and customer management;
- customer support;
- corporate business processes;
- migration from existing CRM systems;
- connecting to company databases;
- ERP and external API integration;
- event- and rule-based automation;
- AI assistance and MCP;
- fast adaptation to unknown hackathon scenarios.

**Core principle:** one stable foundation, many adaptable business solutions.

**Hard requirement:** the product works on its own, with no AI, no external APIs
and no optional integrations. Every integration is switchable and configurable.

### The four directions

| Direction | Scope |
|---|---|
| **Universal CRM** | Contacts, accounts, leads, deals, tasks, activities, tickets, customer profiles. |
| **Adaptive Platform** | Tenant configuration, custom objects, fields, roles, pipelines, modules. |
| **AI Intelligence** | MCP, copilot, autonomous agents, extraction, explainable recommendations. |
| **Automation & Integrations** | Workflows, events, webhooks, SDK, external systems. |

All four use one security model, one audit trail and one set of domain rules.
That is a constraint, not an aspiration: it is why the architecture is a modular
monolith (ADR 0001) and why extraction needs a measured trigger (ADR 0002).

---

## 2. Architecture principles

1. **Modular monolith first.** Clear module boundaries; independent services only
   when a trigger in ADR 0002 is met and recorded.
2. **One implementation of every rule.** API, MCP, worker and future SDK call the
   same domain operations and the same access policies.
3. **No import cycles.** Enforced by tests in `backend/internal/archtest`.
4. **Explicit transaction boundaries.** A domain operation either commits or does
   not.
5. **Idempotency for repeatable operations.** Jobs, imports and events are
   designed to be retried, not assumed to run once.
6. **Versioned APIs and events.** Breaking a contract requires a version and a
   migration path.
7. **PostgreSQL is the source of truth.** Nothing else becomes a second source.
8. **Optional components stay optional.** Redis, a message broker and object
   storage are introduced for a named need, never as a baseline dependency.
9. **Fail visibly.** A misconfiguration is a startup error naming the fix, not a
   silent clamp that surfaces later as unexplained timeouts.
10. **Security is server-side.** Hiding a UI element is not authorization.

---

## 3. Security model

Non-negotiable invariants. Tenant configuration can never disable these:

| Invariant | Enforcement |
|---|---|
| Every request is authenticated | `requireSession` middleware, single place identity is established |
| Every authorization decision is server-side | `rbac.Principal` resolved from the session; handlers check permissions, not the UI |
| Tenant isolation on every query | Organization scope in the query, not a post-filter |
| Audit for privileged and security-relevant actions | Append-only `audit` package |
| No secrets in logs, responses or the repository | Redaction in `logging`; secret scan in CI |
| No raw SQL, shell or filesystem access through AI or MCP | Tool allowlist with per-tool permission checks |

Threat model, trust boundaries and AI safety are detailed in `docs/SECURITY.md`
and `docs/AI_SAFETY.md`.

---

## 4. Phases and Definition of Done

Phases are priority order, not a prohibition on parallel work. Each phase ends
with a published, verified increment.

| Phase | Scope | Exit criterion |
|---|---|---|
| **0. Safe start** | Audit, master docs, ADRs, critical blockers | Docs describe verified reality; blockers named |
| **1. Contracts and platform base** | OpenAPI 3.1, RFC 9457 contract, contract tests, API reference, distributed rate limiting, pool sizing, diagnostics | API contract is machine-checkable and CI fails when code and contract diverge |
| **2. Identity and security** | OIDC Authorization Code + PKCE, test IdP, RBAC and tenant checks, consent management | A real IdP flow passes integration tests; consent is enforced in every channel |
| **3. MCP and AI-native UX** | MCP protocol server, read and safe-write tools, MCP UI, contextual AI, calibrated confidence, graceful failure, human handoff | An external MCP client connects and is correctly denied without permission |
| **4. Agentic system** | Agent runtime, budgets, scope, observe/suggest/act-with-approval, three initial agents | Budget and scope violations are refused and audited |
| **5. Tenant adaptation** | Tenant config, bootstrap, custom objects, SDK, CLI | A custom object works end to end through API, UI and RBAC |
| **6. Customer data and collaboration** | Identity resolution, golden record, merge UI, real-time updates | Merge is transactional, audited and tenant-safe |
| **7. Mobile and analytics** | Responsive UI, PWA, offline read, offline write queue, embedded analytics | Offline task creation syncs with conflict detection |
| **8. Automation and infrastructure** | Workflow builder, event subscriptions, webhook debugger, object storage, backup verification, operations dashboard | Restore is verified on a clean isolated database |
| **9. Polish and documentation** | Design system, unified UI, accessibility, animations, README, docs site | A new developer starts the project from the docs on a clean machine |

### Definition of Done

A task is complete only when every applicable box is checked:

- [ ] Real implementation complete
- [ ] Code formatted (`gofmt`, Prettier)
- [ ] Backend compiles, `go vet` clean
- [ ] Frontend lint and TypeScript typecheck clean
- [ ] Unit tests pass
- [ ] Integration test for the success path
- [ ] Negative tests
- [ ] Tenant isolation checked
- [ ] Permissions checked
- [ ] Error, timeout and cancellation paths checked where applicable
- [ ] E2E added for critical user flows
- [ ] OpenAPI updated when the API changed
- [ ] Documentation updated
- [ ] Roadmap updated
- [ ] Migrations verified on a clean **and** an upgraded database
- [ ] No new secrets, unsafe stubs or stray files
- [ ] CI verified against the specific commit SHA
- [ ] Changes published to the allowed branch
- [ ] The completion message matches the real result

If a check cannot run because of an external dependency, the exact reason is
stated and the check is not reported as passed.

---

## 5. Delivery rules

These are constraints on how work is done, not preferences:

1. No empty endpoints.
2. No buttons without a working action.
3. No stubs standing in for promised logic.
4. No mock substituted for a real integration in production.
5. Tests are never changed to make a result green.
6. Security checks are never disabled to pass CI.
7. Failing tests are never hidden.
8. No dependency without a stated reason.
9. No unrelated refactors.
10. No API contract change without a compatibility check.
11. No destructive migration without a data-migration plan.
12. No deletion of data, files or branches without checking the consequences.
13. A cloud service is never reported as working until a real connection is
    tested.
14. SSO is never reported as done on the strength of configuration alone.
15. A modular monolith is never described as microservices.
16. No claim of GDPR or other compliance without sufficient grounds.
17. No secrets in Git, logs, documentation or screenshots.
18. No paid AI requests without an available budget and explicit permission.
19. No real emails, Telegram messages or webhook calls in testing without an
    isolated environment.
20. No irreversible action in production.
21. No invented performance metrics, test results or publication status.
22. A phase does not start while the previous phase's critical checks are failing.

---

## 6. Documentation map

| Document | Purpose |
|---|---|
| `docs/MASTER_PLAN.md` | This document: strategy, principles, phases, DoD |
| `docs/PROJECT_AUDIT.md` | Verified state, limitations, technical debt |
| `docs/ARCHITECTURE.md` | Application architecture, modules, API, worker, database |
| `docs/ROADMAP.md` | Statuses, priorities, next tasks |
| `docs/ACCEPTANCE_CRITERIA.md` | What "done" means per feature |
| `docs/SECURITY.md` | Threat model, trust boundaries, tenant isolation |
| `docs/AI_SAFETY.md` | Tool permissions, confirmation, prompt-injection defence |
| `docs/CONSENT_MANAGEMENT.md` | Consent model and data-use rules |
| `docs/MCP.md` | Connecting AI clients; MCP security |
| `docs/SDK.md` | Designed and implemented SDK |
| `docs/IMPORT_CENTER.md` | Formats, limits, security, scenarios |
| `docs/OPERATIONS.md` | Local run, diagnostics, monitoring, recovery |
| `docs/adr/` | Architecture decision records |
| `AGENTS.md` | Instructions for AI agents working in this repository |

Documentation states a status per feature. Where docs and code disagree, the code
is the fact and the documentation is corrected.

---

## 7. Definition of platform completion

ForgeCRM is not complete until every category below is verified:

- [ ] Core CRM scenarios
- [ ] Tenant isolation
- [ ] RBAC and audit
- [ ] Real OIDC flow
- [ ] OpenAPI and contract tests
- [ ] Distributed rate limiting across instances
- [ ] MCP with permission checks
- [ ] Consent enforcement in every supported channel
- [ ] AI UX with graceful failure
- [ ] Agent budgets and dangerous-action control
- [ ] Identity resolution and safe contact merge
- [ ] Real-time updates
- [ ] Responsiveness and PWA
- [ ] Offline sync with conflict resolution
- [ ] Embedded analytics on real data
- [ ] SDK and a safe extension system
- [ ] Import Center with file and archive protection
- [ ] Backup/restore verified on a clean database
- [ ] Working integrations and webhooks
- [ ] Documentation site and README
- [ ] Reproducible build and CI
- [ ] No open critical security findings

Anything unchecked is labelled Partial, Planned or Blocked, with the reason
stated. See `docs/PROJECT_AUDIT.md` for the current position.
