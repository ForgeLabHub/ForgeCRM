# ADR 0001 — Hybrid architecture: one deployable, many boundaries

- **Status:** Accepted
- **Date:** 2026-10-28
- **Deciders:** project owner, engineering agent
- **Supersedes:** none

## Context

ForgeCRM is a self-hosted CRM that must run for a single company on one server and
still absorb requirements nobody has written down yet. The mission asks for CRM,
an admin console, import and migration, integrations, events, notifications, MCP,
an AI layer, observability and reproducible deployment.

That list is long enough that the obvious move is to split it into services: an
import service, an integration service, an AI service, a notification service. The
pressure is real, because these workloads differ — importing a 2 GiB archive and
reading one contact should not compete for the same resources.

The counter-pressure is equally real. Every process boundary turns a function call
into a network call with its own failure modes, and turns a database transaction
into a saga. This domain is unusually transactional: archiving a contact,
converting a lead, moving a deal and writing the audit entry must all happen or
none of them. Across services that means distributed transactions — either
two-phase commit or eventual consistency plus reconciliation, a large amount of
machinery whose only job is to undo a design decision.

The platform must also be operable. The realistic target is one VM or a small
cluster run by a team with no dedicated platform group. Every extra process is
another thing to configure, monitor, upgrade and back up.

## Decision

Adopt a **hybrid architecture**: a modular monolith with enforced module
boundaries and independently runnable processes sharing one codebase.

1. **One Go module, one database, one set of domain rules.** The CRM domain,
   transactions and business rules live in the monolith. Every business rule has
   exactly one implementation.

2. **Multiple processes, not multiple services.** `forgecrm-api` serves HTTP;
   `forgecrm-worker` runs background jobs. Separate binaries from one module,
   sharing packages. The split exists to isolate *workloads*, not to separate
   *domains*: a runaway import cannot exhaust the API's connection budget, and the
   API can be replicated without duplicating the worker.

3. **Boundaries are enforced by dependency direction, not by the network.**
   The domain is layered: `crm` is the core; `importx`, `migratex`, `ai`, `events`
   and `notify` are application services that orchestrate it; `api` is the
   composition layer on top. The core never imports an application service, and no
   package imports `api`. These rules are stated in `docs/ARCHITECTURE.md` and
   enforced by tests in `backend/internal/archtest`, which read the real import
   graph through `go list`.

4. **Cross-cutting concerns are shared code, not shared services.** Audit, RBAC,
   consent and event publication are libraries invoked in-process. A new caller
   cannot bypass them by calling a different service, because there is no
   different service.

5. **Extraction is a measured decision with stated triggers**, recorded in ADR
   0002. Until a trigger is met, a workload stays in the monolith.

## Consequences

### Positive

- Business rules exist once. An MCP tool, an HTTP handler and a background job all
  call the same `crm.Service` method, so they cannot disagree about permissions or
  validation. This is the property the mission's "no double implementation" rule
  requires, and it is cheap here and expensive after extraction.
- Transactions stay transactions. Archiving a contact and writing its audit entry
  commit together.
- One artifact to build, version, back up and reason about.
- The operator runs two processes and one database.

### Negative

- A bad deploy affects HTTP and background work together; there is no per-service
  rollout.
- One Go module means a shared-package change rebuilds everything.
- Nothing physically stops a developer importing across a boundary; the rule is
  enforced by tests and review, not by the compiler.
- Resource isolation is coarse: API and worker scale separately, but the shared
  database remains a shared bottleneck.

### Neutral

- The architecture is deliberately reversible in the direction that is hard to
  reverse later: splitting a modular monolith is far easier than merging services.

## Alternatives considered

**Microservices from the start.** Rejected. The domain is transactional, the team
is small, and the operational budget is one server. The cost is paid immediately
and in full while the benefit — independent scaling — is speculative until a
measured trigger is met.

**Serverless per endpoint.** Rejected. Long-running imports with checkpointing and
streaming archive extraction fit a request-scoped model poorly, and cold-start and
execution-time limits would become product constraints.

**Monolith with no process split.** Rejected, narrowly. It is the simplest option
and sufficient for a small deployment, but import and AI workloads are bursty and
memory-hungry, and sharing a process lets a large import starve the API. Two
processes from one codebase buy most of the isolation for a fraction of the cost of
services.

## Verification

| Claim | Evidence |
|---|---|
| Two processes | `backend/cmd/forgecrm-api`, `backend/cmd/forgecrm-worker` |
| Shared domain | Both call `internal/crm.Service`; MCP tools in `internal/ai/mcp.go` call the same service, not a parallel path |
| One-way dependency into the core | `TestApplicationServicesDependOnTheCoreAndNotTheReverse` |
| Nothing imports `api` | `TestNoPackageImportsAPI` |
| Core free of the composition layer | `TestCoreDoesNotImportCompositionOrHTTP` |

## Known deviations

`crm` imports `httpx` for `APIError`, `Pagination` and `SortSpec` (29 uses). A
domain error type does not belong in a package named for HTTP. It is
one-directional, so it is not a cycle and the rules are still written once. The
tests pin what must stay true while the debt exists and it is recorded in
`docs/PROJECT_AUDIT.md`.
