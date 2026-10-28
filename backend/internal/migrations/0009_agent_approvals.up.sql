-- 0009_agent_approvals: the durable state machine for approval-gated agent actions.
--
-- The model is set out in ADR 0005. Three tables, and the reasons they are three
-- rather than one:
--
--   1. A run produces many proposed actions, so actions are their own table keyed
--      to the run rather than fields on it.
--
--   2. A decision is append-only. If an operator changes their mind, that is a new
--      row in agent_approvals, never an update to the action. Rewriting a decision
--      destroys the history the audit trail exists to preserve, so the decision
--      cannot live as a column on agent_actions.
--
--   3. The intent is recorded before the effect. A row in agent_actions with
--      status 'proposed' is the durable record that the agent wanted to act; the
--      audit entries for approved and executed reference it by id. An action can
--      therefore never exist without a record of why it was allowed.

-- 1. A run: one working cycle of an agent acting as a user.
CREATE TABLE agent_runs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- The user the agent acts as. Every tool call is authorized as this user, so an
    -- approval can never grant a permission the user does not have.
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    goal           text NOT NULL CHECK (length(btrim(goal)) BETWEEN 1 AND 4000),
    status         text NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running', 'waiting_approval', 'completed', 'failed', 'cancelled')),
    -- The token budget is a hard limit, not a target: the run fails when it is
    -- exceeded rather than continuing.
    budget_tokens  integer NOT NULL DEFAULT 100000 CHECK (budget_tokens > 0),
    tokens_used    integer NOT NULL DEFAULT 0 CHECK (tokens_used >= 0),
    -- The action cap bounds how many proposals a single run can create, which is
    -- what stops a looping agent from filling the approvals queue.
    action_cap     integer NOT NULL DEFAULT 50 CHECK (action_cap > 0),
    actions_used   integer NOT NULL DEFAULT 0 CHECK (actions_used >= 0),
    error          text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz
);

-- A run is always read scoped to its organization and user.
CREATE INDEX agent_runs_org_idx ON agent_runs (organization_id, created_at DESC);
CREATE INDEX agent_runs_user_idx ON agent_runs (user_id, created_at DESC);
-- The expiry sweeper and the approvals queue both scan by status.
CREATE INDEX agent_runs_status_idx ON agent_runs (status) WHERE status IN ('running', 'waiting_approval');

CREATE TRIGGER agent_runs_updated_at BEFORE UPDATE ON agent_runs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- 2. A proposed action: what the agent wants to do, before it has done it.
CREATE TABLE agent_actions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id         uuid NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- The tool name resolves against the server-side registry, so an unknown or
    -- denied tool can never be proposed in the first place.
    tool_name      text NOT NULL,
    -- Canonical JSON. The hash below is computed over this exact representation, so
    -- a decision binds to the arguments the operator saw.
    arguments      jsonb NOT NULL DEFAULT '{}'::jsonb,
    arguments_hash text NOT NULL,
    -- The tier is copied from the tool definition at propose time, so it cannot be
    -- changed by a later edit of the tool or by anything the model says.
    risk_tier      text NOT NULL CHECK (risk_tier IN ('safe', 'confirm', 'destructive')),
    -- The human-readable summary is written by the server from the tool and its
    -- arguments. It is never model prose, so it cannot be used to mislead an
    -- operator into approving something the diff does not show.
    summary        text NOT NULL,
    status         text NOT NULL DEFAULT 'proposed'
                   CHECK (status IN ('proposed', 'approved', 'rejected', 'expired', 'executed', 'failed')),
    -- Unique, so a retried or duplicated execution cannot perform the effect twice.
    idempotency_key text NOT NULL UNIQUE,
    result         jsonb,
    error          text,
    -- Bounds the wait. A proposal that is never decided expires; without this,
    -- "waiting for approval" becomes "hung forever".
    expires_at     timestamptz NOT NULL DEFAULT now() + interval '24 hours',
    proposed_at    timestamptz NOT NULL DEFAULT now(),
    decided_at     timestamptz,
    executed_at    timestamptz
);

-- The approvals queue: proposed actions, newest first, within an organization.
CREATE INDEX agent_actions_queue_idx ON agent_actions (organization_id, proposed_at DESC)
    WHERE status = 'proposed';
CREATE INDEX agent_actions_run_idx ON agent_actions (run_id, proposed_at DESC);
-- The expiry sweeper scans proposals past their deadline.
CREATE INDEX agent_actions_expiry_idx ON agent_actions (expires_at) WHERE status = 'proposed';

-- 3. A decision: append-only, signed, never updated.
CREATE TABLE agent_approvals (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id      uuid NOT NULL REFERENCES agent_actions(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    decided_by     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    decision       text NOT NULL CHECK (decision IN ('approve', 'reject')),
    comment        text,
    -- The hash of the arguments the operator saw at decision time. Compared with
    -- the stored arguments_hash at execution to close the TOCTOU window.
    arguments_hash text NOT NULL,
    -- HMAC over the canonical decision tuple, keyed by a purpose-derived key from
    -- the platform secret. It makes the record tamper-evident against direct
    -- database access: an attacker without the secret cannot forge a decision that
    -- verifies. The secret lives in the environment, never in this table.
    signature      text NOT NULL,
    decided_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX agent_approvals_action_idx ON agent_approvals (action_id, decided_at DESC);
CREATE INDEX agent_approvals_org_idx ON agent_approvals (organization_id, decided_at DESC);
