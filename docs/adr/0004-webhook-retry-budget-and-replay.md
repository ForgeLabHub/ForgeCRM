# ADR 0004 — Webhook delivery retry budget and replay semantics

- **Status:** Accepted
- **Date:** 2026-10-28
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0003 (multi-replica safety detour)
- **Scope:** `internal/events/delivery.go`, `internal/events/events.go`, migration 0008

## Context

A webhook delivery can be attempted again for two unrelated reasons:

1. **Automatic retry** after a failed attempt, up to the subscription's
   `max_attempts`, with exponential backoff. This is the machine deciding the
   endpoint may recover on its own.
2. **Operator replay** after the delivery has been dead-lettered. This is a human
   deciding the endpoint is fixed and the event should be delivered now.

Both reset the attempt count, and the first implementation conflated them. The
draft computed the retry budget as

```
budget = max_attempts * (replay_count + 1)
```

while `attempts` was *not* reset on replay. That is wrong in two ways at once, and
neither is obvious from reading a single function:

- `attempts` is what the sender compares against the budget. If it is never reset,
  a replayed delivery starts already at `max_attempts` and is dead on its next
  failure — the replay grants nothing.
- If the budget is multiplied by `replay_count` while `attempts` *is* reset, then
  the budget for the second window is `2 * max_attempts`, which silently doubles
  the retries the operator configured. The subscription says five attempts; the
  system performs ten.

The defect was found on review of the code, not by a test, and not by reading a
document — because the formula existed nowhere except in the implementation. That
is the specific failure this ADR prevents: a non-obvious rule that lives only in
code gets written wrong, and the error is invisible until someone traces the
arithmetic.

## Decision

Define the two counters and the budget explicitly, and give each counter one job.

### The two counters

| Column | Meaning | Reset on replay? |
|---|---|---|
| `attempts` | Attempts in the **current window**. The sender compares it against `max_attempts`. | **Yes** — this is what grants a fresh budget. |
| `replay_count` | Lifetime number of windows. A monotonic record of operator intervention. | **No** — never reset. |

### The budget

```
budget = max_attempts
```

The budget is the subscription's configured `max_attempts`, always. It does not
depend on `replay_count`, because `attempts` is reset to zero on replay, so each
window starts with a full budget. A replay therefore grants exactly one more
window of `max_attempts` attempts — not more, not fewer.

The alternative — a budget that scales with `replay_count` — was rejected because
it makes the retry policy depend on history rather than on configuration, so the
number of attempts a subscription performs is not derivable from its own settings.
An operator who configures five attempts must be able to rely on five.

### The dead-letter transition

`attempts >= budget` moves the delivery to `dead`. The transition and its guard
are one statement:

```sql
UPDATE webhook_deliveries
SET status = 'dead', dead_lettered_at = now(), ...
WHERE id = $1 AND status <> 'dead'
RETURNING organization_id, subscription_id, event_type
```

`WHERE status <> 'dead'` makes "exactly one audit entry per transition" a database
property rather than a code convention. Two workers that both decide the delivery
is exhausted: the first updates a row and writes an entry, the second updates no
row and writes none. A `SELECT`-then-`UPDATE` would let both read `failed` and
both write, and no integration test would catch it reliably — which is the reason
the guard is in the statement and not in Go.

A replay sets `status = 'pending'` and clears `dead_lettered_at`. A delivery that
dies again after a replay is a **genuine second transition** and is audited again.
That is the distinction the guard draws: one entry per time the delivery actually
reaches the dead state, not one per attempt and not one ever.

### Replay is idempotent in the safe direction

Replay refuses a delivery that is already in flight. "In flight" covers both
`pending` (queued, not yet claimed) and `delivering` (claimed by a worker and being
sent right now), so the guard is `status NOT IN ('pending', 'delivering')`.

The `delivering` half is not optional. A worker that has claimed a delivery holds
the row in that state while the POST is in progress; replaying it there would grant
a second window while the first attempt is still running, so the operator's fresh
budget would be consumed by the attempt already underway and one event could
produce two sends. A guard of `status <> 'pending'` — the first form of this
rule — allowed exactly that, because a claimed delivery is not `pending`. The
mistake is instructive: the guard named one in-flight state when the model has two.

Two replays of one delivery cannot hand the sender a budget the operator did not
intend, and concurrent replays behave the same as a single one: the guard lives in
the `UPDATE`'s `WHERE` clause, so the row lock makes the second writer observe the
new state and affect no rows.

### Replay is scoped by subscription, not only by organization

The route nests a delivery under its subscription
(`/webhooks/{id}/deliveries/{deliveryId}/replay`), and the OpenAPI document marks
both path parameters required, so the subscription is part of the delivery's
identity. The `UPDATE`'s `WHERE` clause therefore filters on `organization_id`
*and* `subscription_id`, both in the statement that mutates the row. A handler
that read only `deliveryId` and ignored `{id}` accepted a request whose path named
a subscription the delivery did not belong to — the same request would then mean
two different things depending on which half of the path the handler happened to
read. Refusing it keeps the path honest, and the two predicates together make a
mismatched id indistinguishable from one that does not exist.

## Consequences

- **Positive.** The number of attempts a subscription performs is a function of
  its configuration alone. A replay grants exactly one more window.
- **Positive.** `replay_count` is a durable, monotonic record of how often an
  operator has intervened, which is what a support engineer needs to judge whether
  an endpoint is broken or was briefly down.
- **Positive.** "Exactly one audit entry per transition" is enforced where the
  race is, so it holds under concurrency without relying on caller discipline.
- **Negative.** `attempts` no longer carries lifetime information, so anything
  that wants to display "total attempts ever" must read `replay_count` as well.
  This is documented here and in `docs/operations/README.md` so it is not
  rediscovered.
- **Negative.** The guard now enumerates both in-flight states rather than the one
  that was obvious. A future state added to `webhook_deliveries.status` must be
  considered here: if it also means "a worker holds this row", it belongs in the
  refusal list, and the `CHECK` constraint is the place to look for the full set.
- **Neutral.** A replay is not a retry in the automatic sense and does not inherit
  the backoff schedule; it is scheduled immediately, because the operator is
  asserting the endpoint is ready.

## References

- `backend/internal/migrations/0008_webhook_delivery_lifecycle.up.sql`
- `backend/internal/events/delivery.go` — `retryOrFail`, `deadLetter`
- `backend/internal/events/events.go` — `Replay`
- `backend/internal/testsupport/webhook_deadletter_test.go` — the state-machine evidence
- `backend/internal/api/webhook_replay_http_test.go` — the HTTP-level evidence,
  including the in-flight refusal and the subscription scoping
