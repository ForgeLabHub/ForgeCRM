# ADR 0002 — Measurable triggers for extracting a service

- **Status:** Accepted
- **Date:** 2026-10-28
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0001 (hybrid architecture)

## Context

ADR 0001 keeps every workload in one module and two processes, and defers
extraction. "We will extract it when it hurts" is not a decision anyone can act
on, and it is how a monolith becomes a distributed system by accident — usually
during an incident, under time pressure, with no measurement to justify it.

Extraction has a real, permanent cost:

- The transaction boundary becomes a consistency protocol. A domain operation
  that commits today becomes a saga with compensating actions.
- Every cross-service call needs its own timeout, retry, circuit breaker and
  idempotency key.
- Observability must span the boundary: a trace, a correlation ID and a log
  pipeline that follows a request across two deployments.
- Two things to deploy, version, back up and monitor instead of one.
- Version skew becomes possible, so every API needs compatibility rules.

That cost is worth paying for a measured reason and not otherwise. This ADR
records the reasons, so the decision is a threshold rather than a matter of taste.

## Decision

A workload is extracted into its own service **only** when at least one trigger
below is met and recorded in an ADR that supersedes the relevant part of ADR 0001.
Each trigger has a number, a way to measure it, and a reason it indicates a real
constraint rather than an inconvenience.

### T1 — Resource contention with a measured impact

The workload's CPU or memory use measurably degrades the API's latency, and
scaling the API does not fix it because the workload competes for the same
process.

- **Measure:** p95 API latency during a representative import or AI batch,
  compared with the same latency at idle; plus process RSS/CPU for both.
- **Threshold:** p95 latency at least 2x the idle baseline, sustained across three
  runs, with the workload and the API in the same process.
- **Why:** this is the one thing the API/worker split does not already solve,
  because the contention is *within* a process.

### T2 — Independent scaling need with divergent ratios

Demand for the workload's capacity diverges from the API's by a large, sustained
factor, so scaling them together wastes resources.

- **Measure:** required replica count for each, over a week of production
  traffic, expressed as a ratio.
- **Threshold:** a sustained ratio of 4:1 or greater.
- **Why:** the current split already lets API and worker scale separately. This
  trigger applies to a *third* workload that needs its own ratio.

### T3 — A hard isolation requirement from a non-negotiable constraint

A security, compliance or blast-radius requirement cannot be met in-process.

- **Measure:** the specific control that cannot be implemented, and why
  in-process isolation is insufficient.
- **Threshold:** an identified requirement with no in-process implementation.
- **Why:** this is the strongest trigger and the only one that may justify
  extraction without a load measurement. It still requires the ADR, because the
  cost is the same regardless of the reason.

### T4 — A language or runtime boundary that a real workload needs

A workload genuinely requires a different runtime — for example, untrusted archive
processing that needs strict resource limits, or a numerically heavy transform.

- **Measure:** the specific requirement, and the evidence that Go cannot meet it.
- **Threshold:** a demonstrated failure of the Go implementation against a stated
  limit.
- **Why:** the mission allows Rust for isolated processing of untrusted archives,
  but only with a stated reason, interface, maintenance cost and evidence. This
  trigger is that bar. "Rust is faster" is not evidence; a profile showing the
  limit is exceeded is.

### T5 — Independent release cadence required by an external party

A workload must be released on a schedule that cannot be coordinated with the
core, because an external party depends on it.

- **Measure:** the required cadence and the coordination cost of not meeting it.
- **Threshold:** a documented external requirement.
- **Why:** rare, and worth naming so it is not silently absorbed.

## What is deliberately not a trigger

These come up in every extraction discussion and are not sufficient on their own:

| Argument | Why it is insufficient |
|---|---|
| "It is a distinct domain" | Domain separation is achieved by package boundaries and enforced by `internal/archtest`, at no transactional cost. |
| "It will be easier to test" | A well-bounded package is already testable in isolation. Extraction usually makes testing harder, because integration tests now need a network. |
| "It might need to scale" | Speculation. Wait for T2. |
| "It has a different tech stack in theory" | Wait for T4 and its evidence. |
| "Microservices are the industry norm" | Not a measurement. |

## Process when a trigger is met

1. Record the measurement, not the impression: the query, the window and the
   result.
2. Write an ADR that states which trigger was met, the interface, the consistency
   model, the failure modes, the on-call impact and the rollback plan.
3. Define the contract first — a versioned API or event schema — and keep the
   in-process implementation behind it, so the extraction is a deployment change
   rather than a rewrite.
4. Only then move the code.

## Current assessment

No trigger is met. Specifically:

- The API/worker split already isolates the two largest workloads, so T1's
  remaining scope is narrow.
- Deployment is single-tenant by design, so no scaling ratio has been observed
  (T2).
- No compliance requirement has been identified that in-process controls cannot
  meet (T3).
- Archive extraction is bounded in-process by `internal/importx/safe`, with
  limits on entries, total bytes, entry size, depth and compression ratio. No
  measurement yet shows those limits are insufficient (T4).
- No external party depends on an independent cadence (T5).

**Consequence:** the import, AI, notification and integration workloads stay in
the monolith. This will be revisited when a trigger is met, with the measurement
recorded here.

## Verification

- ADR 0001 records the architecture these triggers modify.
- `backend/internal/archtest` keeps the module extractable, so meeting a trigger
  later is a deployment change rather than an untangling exercise.
- This ADR states no measurement as met. When one is, it is added above with its
  query, window and result.
