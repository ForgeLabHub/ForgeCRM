-- 0012_workflows: the workflow automation engine.
--
-- A workflow is a trigger plus an ordered list of steps. It runs when its trigger
-- matches a committed platform event, or on a schedule. Steps act on CRM records
-- through the same service layer the HTTP API uses, so a workflow can never do
-- anything a user could not do — every action re-checks permissions as its author.
--
-- Four tables, and why:
--
--   1. workflows — the definition. `definition` is the full JSON document (trigger,
--      steps, conditions) and is the source of truth. The scalar columns beside it
--      (trigger_type, event_type, enabled, …) are denormalised copies used for
--      matching and listing, so the engine does not have to parse every definition
--      on every event. They are kept in sync by the application in one transaction.
--
--   2. workflow_runs — one execution attempt of a workflow. Carries the trigger
--      event, status, the per-step outcome log and the error. This is what an
--      operator watches and what the manual replay acts on.
--
--   3. workflow_step_runs — one row per step per run, appended as the step finishes.
--      Kept separate from workflow_runs because a run is a header and the steps are
--      its detail: a single JSON blob would make "which step failed, and how often"
--      a parse rather than an index lookup.
--
--   4. workflow_run_events — the de-duplication ledger. The outbox delivers at least
--      once, so the same event can be seen twice; this table makes the second sighting
--      a no-op. The unique key is (workflow_id, event_id), which is exactly the
--      "this workflow already ran for this event" fact.

-- 1. The workflow definition.
CREATE TABLE workflows (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    description      text,
    -- The full definition document. `schema_version` versions the document shape,
    -- not the workflow, so a future field can be added without rewriting old rows.
    definition       jsonb NOT NULL,
    definition_schema_version integer NOT NULL DEFAULT 1,
    enabled          boolean NOT NULL DEFAULT true,
    -- Denormalised trigger fields, for matching without parsing `definition`.
    trigger_type     text NOT NULL CHECK (trigger_type IN ('event', 'schedule', 'manual')),
    -- Set when trigger_type = 'event'. Null for schedule and manual triggers.
    event_type       text,
    -- Set when trigger_type = 'schedule'. A 5-field cron expression, evaluated in
    -- `schedule_timezone`.
    schedule_cron    text,
    schedule_timezone text,
    -- The next instant the scheduler should consider this workflow. Maintained by
    -- the application; the scheduler scans this column with an index.
    next_run_at      timestamptz,
    -- The user whose permissions every action runs under. A workflow is authored by
    -- someone and acts as them; if that person loses a permission, the workflow
    -- loses it too, because the check happens at execution time.
    created_by       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Bounds how much work one event can cause. A workflow whose steps emit events
    -- that match another workflow can chain; this counter stops a chain that never
    -- terminates. See the comment on run_depth.
    max_depth        integer NOT NULL DEFAULT 5 CHECK (max_depth BETWEEN 1 AND 20),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    archived_at      timestamptz
);

CREATE INDEX workflows_org_idx ON workflows (organization_id, created_at DESC)
    WHERE archived_at IS NULL;
-- Event-trigger matching: enabled, non-archived workflows watching one event type.
CREATE INDEX workflows_event_idx ON workflows (organization_id, event_type)
    WHERE enabled AND archived_at IS NULL AND trigger_type = 'event';
-- The scheduler's scan: due, enabled, non-archived schedule workflows.
CREATE INDEX workflows_schedule_idx ON workflows (next_run_at)
    WHERE enabled AND archived_at IS NULL AND trigger_type = 'schedule';

CREATE TRIGGER workflows_updated_at BEFORE UPDATE ON workflows
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 2. One execution of a workflow.
CREATE TABLE workflow_runs (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id      uuid NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- What caused this run. Null for a manual run and for a scheduled run with no
    -- originating event.
    trigger_event_id uuid,
    trigger_type     text NOT NULL CHECK (trigger_type IN ('event', 'schedule', 'manual')),
    status           text NOT NULL DEFAULT 'running'
                     CHECK (status IN ('running', 'succeeded', 'failed', 'skipped', 'paused')),
    -- How far the chain that produced this run had already gone. Derived from the
    -- correlation chain, not threaded through payloads: every run in a chain shares
    -- one correlation_id, because the events a step causes inherit the context's
    -- correlation id. The depth is the number of runs already recorded for that
    -- correlation. A run at or above the workflow's max_depth is recorded as skipped
    -- rather than executed, which is what breaks an event feedback loop.
    run_depth        integer NOT NULL DEFAULT 0 CHECK (run_depth >= 0),
    -- Correlation with the request or the event that started the run. The chain
    -- breaker counts runs by this value, so it is indexed below.
    correlation_id   text,
    -- A short, server-written summary of what the run did. Never model prose.
    summary          text,
    error            text,
    steps_total      integer NOT NULL DEFAULT 0,
    steps_succeeded  integer NOT NULL DEFAULT 0,
    steps_failed     integer NOT NULL DEFAULT 0,
    -- A `delay` step pauses the run instead of blocking a worker. The index of the
    -- next step to run is stored, so the scheduler resumes from exactly there rather
    -- than re-running the steps that already had their effect.
    next_step_index  integer NOT NULL DEFAULT 0 CHECK (next_step_index >= 0),
    paused_until     timestamptz,
    started_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz
);

CREATE INDEX workflow_runs_workflow_idx ON workflow_runs (workflow_id, started_at DESC);
CREATE INDEX workflow_runs_org_idx ON workflow_runs (organization_id, started_at DESC);
-- The chain breaker counts runs per correlation, so this is the index it reads.
CREATE INDEX workflow_runs_correlation_idx ON workflow_runs (correlation_id)
    WHERE correlation_id IS NOT NULL;
-- The run list defaults to the running ones, which is what an operator watches.
CREATE INDEX workflow_runs_status_idx ON workflow_runs (organization_id, status, started_at DESC)
    WHERE status = 'running';
-- The scheduler resumes paused runs whose delay has elapsed.
CREATE INDEX workflow_runs_paused_idx ON workflow_runs (paused_until)
    WHERE status = 'paused';

-- 3. One step of a run.
CREATE TABLE workflow_step_runs (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id           uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- The 0-based position of the step in the definition, so the log lines up with
    -- the editor even if a step is later renamed.
    step_index       integer NOT NULL CHECK (step_index >= 0),
    step_id          text NOT NULL,
    step_type        text NOT NULL,
    status           text NOT NULL CHECK (status IN ('succeeded', 'failed', 'skipped')),
    -- Redacted: never carries a secret, only ids and counts.
    detail           jsonb NOT NULL DEFAULT '{}'::jsonb,
    error            text,
    started_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz
);

CREATE INDEX workflow_step_runs_run_idx ON workflow_step_runs (run_id, step_index);

-- 4. The de-duplication ledger for event-triggered runs.
CREATE TABLE workflow_run_events (
    workflow_id      uuid NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    -- No foreign key to outbox_events: outbox rows are pruned by retention, and the
    -- ledger must outlive the event it recorded. The id is kept as a value.
    event_id         uuid NOT NULL,
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    run_id           uuid NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- The whole point: a second delivery of the same event cannot insert, so it
    -- cannot start a second run.
    PRIMARY KEY (workflow_id, event_id)
);

CREATE INDEX workflow_run_events_org_idx ON workflow_run_events (organization_id, created_at DESC);
