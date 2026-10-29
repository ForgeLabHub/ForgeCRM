# ADR 0007: Workflow automation engine

Status: accepted
Date: 2026-10-29

## Context

ForgeCRM already had the parts an automation engine needs and no engine to use
them: a transactional outbox that commits domain events with the write that caused
them, a worker that drains that outbox, a `crm.Service` that owns every CRM write,
and an RBAC service that resolves a user's effective permissions. What was missing
was a way for an organization to say "when this happens, do that" without writing
code, and a way to do it that could not be used to exceed the author's own
authority.

The requirement is not a general-purpose orchestrator. It is the automation that a
corporate CRM needs: react to a record changing, run something on a schedule, create
a follow-up task, move a deal, raise a ticket, tell someone, and wait a while before
the next step.

## Decision

Add a workflow engine as a module in the monolith, driven by the existing outbox and
scheduled by the existing worker.

A workflow is a trigger plus an ordered list of steps, stored as a JSON document in
`workflows.definition` with denormalised trigger columns beside it for matching. The
denormalised columns exist so the engine does not parse every definition on every
event; the application keeps them in sync in one transaction on write.

### The engine acts as its author, re-checked at execution time

Every step runs through the same `crm.Service` the HTTP API uses, as the user who
created the workflow. That user's permissions are resolved from the database on
every run, not captured when the workflow was saved. If the author is blocked,
removed from the organization, or has a role revoked, the workflow loses that
authority immediately. A workflow whose author is no longer a valid member fails
rather than running with no authority.

### The engine enforces the coarse permission itself

This is the subtle one. `crm.Service` enforces tenant scoping and record ownership,
but the coarse "may you do this at all" permission (`tasks.create`,
`deals.change_stage`, …) is enforced by the HTTP middleware on the API path and by
the tool registry on the MCP path — the service layer does not check it. The engine
calls the service directly, so without its own check a user holding
`workflows.manage` but not `tasks.create` could author a workflow that creates
tasks. The engine therefore maps each step type to the permission it requires and
checks it against the freshly resolved principal before running the step.

The alternative — teaching `crm.Service` to check the coarse permission — was
rejected because it would make the service layer's contract depend on the caller's
identity in a way the existing callers do not expect, and because the API already
enforces it. The engine is a new caller of the service and takes on the obligation
that comes with bypassing the middleware.

### Delivery is at-least-once, and de-duplication is a database constraint

The outbox can deliver the same event more than once. The engine de-duplicates on
`(workflow_id, event_id)`: the claim is a primary-key insert into
`workflow_run_events`, and the run row is created in the same transaction. A
duplicate delivery commits nothing at all — no run row, not even a skipped one — so
the history stays one run per event.

An earlier ordering created the run first and marked the duplicate `skipped`. That
was wrong: under an outbox that retries, it accumulated one orphan run per
redelivery. The test that caught it delivers one event three times and asserts one
run.

### A delay does not hold a worker

A `delay` step writes a resume instant and the next step index onto the run and
returns. The worker's scheduler selects paused runs whose instant has passed and
resumes them from that index. No worker is blocked waiting for a delay, and a
restart does not lose the pause because it is a row.

### A chain a workflow causes is bounded

A step that writes a record emits an event through the outbox. That event inherits
the correlation id of the run that caused it, so runs in one causal chain share a
correlation id. The chain depth is the number of runs already recorded for that
correlation. A run at the workflow's `max_depth` is recorded as `skipped` rather
than executed. This is what stops two workflows that watch each other from running
forever, and it is a property of the data rather than of a timer.

### Multi-replica safety

The scheduler claims each due workflow by advancing its `next_run_at` inside the
same transaction that selects it, so two replicas cannot both fire the same due
time. Paused runs are claimed the same way.

## Consequences

- A workflow cannot exceed its author's authority, and a revoked permission takes
  effect on the next run. This is tested by revoking a permission and asserting the
  next run fails without creating anything.
- A redelivered event has no effect the second time, and leaves no trace. This is
  tested by delivering an event three times.
- Two workflows that watch each other stop at the depth limit instead of looping.
- The engine adds no new event mechanism, no new queue, and no external dependency:
  it is a consumer of the outbox the platform already had.
- The engine must be given the same permission checks the middleware performs, and
  that obligation is now explicit in the code and in this document. A future step
  type added without a `requiredPermission` entry would be a bug, so the map is
  the place to look when adding one.

## Not decided here

- A visual workflow editor. The API and the JSON document are the contract; a
  builder UI is a frontend concern that can be added without changing the engine.
- Rollback of a partially completed run. A step that wrote a record cannot be
  automatically undone, and pretending otherwise would be worse than not offering
  it. A run reports which steps succeeded so an operator can act.
- A distributed scheduler. The claim-by-update makes several replicas safe, which is
  enough; a separate scheduling service would add operational cost without a
  correctness gain at this scale.
