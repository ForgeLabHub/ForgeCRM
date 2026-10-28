# ADR 0003 — Detour: multi-replica safety verification

- **Status:** Accepted
- **Date:** 2026-10-28
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0001 (hybrid architecture), ADR 0002 (service extraction triggers)
- **Scope:** migrations 0007 and 0008, `internal/events`, `internal/outbox`, `internal/ratelimit`

## Context

The roadmap places distributed operation inside Phase 8 ("automation and
infrastructure") and the agentic system in Phase 4. Both are downstream of the
foundation, and the foundation was believed to be sound: one API process, one
worker, PostgreSQL, a job queue built on `FOR UPDATE SKIP LOCKED`.

That belief was not tested. Two things changed it:

1. **A defect class that only appears with two processes.** The outbox drain and
   the webhook dispatcher both read their work queue without claiming it. With a
   single worker and a quiet queue the code is correct by accident: nothing else
   is polling, so the read-modify-write never interleaves. With two workers it
   duplicates. A test that runs one worker passes against the broken code, which
   makes the existing test suite not merely incomplete but *misleading* — it
   reported green on a system that would double-deliver every webhook the moment
   anyone scaled it.

2. **The mission asks for both modes.** The brief requires a working monolithic
   mode *and* a working distributed mode, with correct job claiming and safe event
   re-delivery, and it requires evidence rather than assertion. A single-process
   test cannot supply that evidence.

The alternative was to note the risk and continue with the roadmap. That was
rejected: a known double-delivery defect in a component whose whole purpose is
reliable delivery is a data-integrity problem, and data integrity outranks
schedule in the ordering rule (§11.1 of the master plan). Leaving it also blocks
Phase 4 — an agent acting on CRM data through a delivery path that can duplicate
side effects is not something to build on.

## Decision

Run a **bounded detour** before continuing the roadmap, scoped to multi-replica
correctness of the delivery and scheduling paths. The detour is closed and the
roadmap resumes at its previous position; it does not become a new phase.

### What the detour covered

| # | Change | Why it could not wait |
|---|---|---|
| 1 | Outbox drain claims with `UPDATE ... FOR UPDATE SKIP LOCKED` | Two drains delivered the same event twice |
| 2 | Abandoned claims released on worker start | A crashed worker stranded its claim permanently |
| 3 | Bounded outbox retries with a dead-letter state | A permanently failing event was retried forever |
| 4 | Webhook delivery claim + `(subscription_id, outbox_event_id)` unique index | Two workers POSTed the same delivery to the customer |
| 5 | The retention sweep is actually enqueued, hourly and idempotently | Delivered rows, finished jobs and logs grew without bound |
| 6 | Distributed rate limiting (Redis-backed store, opt-in) | Per-process counters meant N replicas allowed N× the limit |
| 7 | `X-Forwarded-For` no longer trusted from any caller | A client chose its own rate-limit bucket and log identity |
| 8 | Dead-letter transition for webhook deliveries: one audit entry, `dead_lettered_at`, three metrics | A stopped subscriber was invisible; there was nothing to alert on |

### What it deliberately did not cover

- **No new infrastructure.** No message broker, no Temporal, no service split.
  Durable waiting and at-least-once delivery are built on PostgreSQL and the
  existing job queue, per ADR 0001.
- **No exactly-once claim.** Delivery over a network is at-least-once. The
  guarantee is that duplicates are *detectable* (a stable `event_id` in every
  payload) and that the delivery machinery does not manufacture them.
- **No change to the phases.** This is a detour, not Phase 8 pulled forward. The
  work is closed and the roadmap continues at Phase 4.

### Rule adopted

A defect is detour-worthy when it is (a) a data-integrity or security defect,
(b) in a component that is already claimed as working, and (c) not detectable by
the existing tests. All three held here. The rule is recorded so the next such
defect is handled the same way, and so that "we found something while reading the
code" does not become a licence to wander.

## Consequences

- **Positive.** Multi-replica operation is now tested with two real processes
  against a real PostgreSQL server, which is the only configuration that exposes
  the defect class. The distributed mode required by the mission has evidence
  behind it rather than a design argument.
- **Positive.** The dead-letter state gives operators a signal where there was
  none, and the audit entry makes a stopped subscriber a durable fact rather than
  a transient log line.
- **Negative.** The detour consumed a cycle that the roadmap had allocated to
  Phase 4 groundwork. Phase 4 starts one cycle later than planned.
- **Negative.** Redis becomes an *optional* dependency for distributed rate
  limiting. It is off by default and enabled by profile, so the default
  deployment still runs on PostgreSQL alone — but the operational surface now has
  a second configuration axis to document and test.
- **Neutral.** Migration numbering advanced to 0008. Phase 4's agent tables start
  at 0009, so the detour is visible in the migration history, which is where a
  future reader will look.

## References

- `CHANGELOG.md` — the defects in detail
- `docs/operations/README.md` — rate limiting and webhook delivery runbooks
- `backend/internal/testsupport/concurrency_test.go` — the two-worker evidence
- ADR 0005 — HITL agent execution model (the next consumer of this path)
