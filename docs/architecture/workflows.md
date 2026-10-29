# Workflow automation

A workflow is a trigger plus an ordered list of steps. It reacts to a committed
event, runs on a schedule, or runs when someone asks for it. Steps act on CRM
records through the same service the HTTP API uses, as the user who created the
workflow, with that user's permissions resolved on every run.

For the reasoning behind the design, see
[ADR 0007](../adr/0007-workflow-automation-engine.md).

## Triggers

| Trigger | When it runs | Notes |
| --- | --- | --- |
| `event` | An event of the named type is committed to the outbox | De-duplicated per event |
| `schedule` | A 5-field cron expression matches, in the workflow's timezone | Claimed by `next_run_at` |
| `manual` | `POST /api/v1/workflows/{id}/run` | Synchronous |

## Steps

| Type | What it does | Required permission |
| --- | --- | --- |
| `condition` | Continues only when its expression is true | — |
| `branch` | Chooses a step list by a condition | — |
| `create_task` | Creates a task | `tasks.create` |
| `create_ticket` | Creates a support ticket | `tickets.create` |
| `update_deal` | Moves a deal to a stage | `deals.change_stage` |
| `notify` | Notifies a member of the same organization | `notifications.read` |
| `delay` | Pauses the run and resumes it later | — |

A step's arguments can reference data from the event that triggered the run with
`{{path}}`, for example `{{account_name}}` or `{{payload.deal_id}}`.

### Definition shape

```json
{
  "steps": [
    {"id": "check", "type": "condition", "args": {"left": "{{amount}}", "op": "gt", "right": 10000}},
    {"id": "task", "type": "create_task", "args": {"title": "Call {{account_name}}", "priority": "high"}},
    {"id": "wait", "type": "delay", "args": {"minutes": 60}},
    {"id": "tell", "type": "notify", "args": {"user_id": "…", "title": "Follow-up due"}}
  ]
}
```

Limits are enforced at save time: 50 steps per definition, a branch depth of 5, and
a delay of at most 30 days.

## Execution model

- **At-least-once, de-duplicated.** The outbox can deliver an event more than once.
  The engine claims `(workflow_id, event_id)` with a primary-key insert and creates
  the run in the same transaction. A duplicate delivery commits nothing, so there is
  one run per event.
- **Permissions at execution time.** The author's effective permissions are read
  from the database on every run. A revoked permission stops the next run.
- **Delays do not hold a worker.** A `delay` writes a resume instant and the next
  step index; the scheduler resumes the run from there.
- **Chains are bounded.** An event caused by a run inherits the run's correlation
  id, and the chain depth is the number of runs already recorded for it. A run at
  `max_depth` is recorded as `skipped` rather than executed, which stops two
  workflows that watch each other.
- **Multi-replica safe.** The scheduler claims a due workflow by advancing its
  `next_run_at` in the same transaction that selects it.

## API

| Method | Path | Permission |
| --- | --- | --- |
| `GET` | `/api/v1/workflows` | `workflows.read` |
| `POST` | `/api/v1/workflows` | `workflows.manage` |
| `GET` | `/api/v1/workflows/{id}` | `workflows.read` |
| `PUT` | `/api/v1/workflows/{id}` | `workflows.manage` |
| `PUT` | `/api/v1/workflows/{id}/enabled` | `workflows.manage` |
| `DELETE` | `/api/v1/workflows/{id}` | `workflows.manage` |
| `POST` | `/api/v1/workflows/{id}/run` | `workflows.run` |
| `GET` | `/api/v1/workflows/{id}/runs` | `workflows.read` |
| `GET` | `/api/v1/workflow-runs/{id}` | `workflows.read` |
| `GET` | `/api/v1/workflow-runs/{id}/steps` | `workflows.read` |

A manual run returns `200` with the run result, or `202` with the run id when the
run paused on a delay.

## Data model

| Table | Purpose |
| --- | --- |
| `workflows` | The definition and its denormalised trigger fields |
| `workflow_runs` | One execution: trigger, status, counters, pause state |
| `workflow_step_runs` | One row per step per run, appended as it finishes |
| `workflow_run_events` | The de-duplication ledger, keyed `(workflow_id, event_id)` |

## Verified behaviour

Integration tests run the real engine against a real PostgreSQL schema
(`internal/testsupport/workflow_engine_test.go`, build tag `integration`):

| Test | What it proves |
| --- | --- |
| `TestWorkflowManualRunExecutesAStep` | A run writes a real task and records a successful step |
| `TestWorkflowEventTriggerIsDeDuplicated` | Three deliveries of one event produce one run and one task |
| `TestWorkflowPermissionIsRecheckedAtExecutionTime` | Revoking `tasks.create` fails the next run and creates nothing |
| `TestWorkflowCannotNotifyAcrossOrganizations` | A recipient in another organization is refused and nothing is written |
| `TestWorkflowDelayPausesAndResumes` | Nothing runs before the instant; the run completes after it |
| `TestWorkflowDepthLimitStopsLoops` | A chained event at the depth limit is skipped |

## Not implemented

- A visual workflow editor. The API and the JSON document are the contract.
- Rollback of a partially completed run. A run reports which steps succeeded so an
  operator can act; it does not claim to undo a write.
- A separate scheduling service. Claim-by-update makes several replicas safe, which
  is enough at this scale.
