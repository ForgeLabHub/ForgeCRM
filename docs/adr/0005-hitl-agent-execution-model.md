# ADR 0005 — Human-in-the-loop agent execution model

- **Status:** Accepted
- **Date:** 2026-10-28
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0001 (hybrid architecture), ADR 0003 (multi-replica safety detour)
- **Scope:** migrations 0009–0011, `internal/agent`, `internal/authn` (step-up),
  `internal/api/agent_handlers.go`, `internal/api/step_up_handlers.go`,
  `frontend/src/pages/ApprovalsPage.tsx`

## Context

The roadmap's Phase 4 requires an agent runtime with "observe / suggest /
act-with-approval" semantics, and its exit criterion is that "budget and scope
violations are refused and audited". The mission adds a sharper requirement: an
agent must be able to act autonomously, but a consequential action must not be
possible without a signed human decision.

The tempting implementation is a prompt that tells the model to ask permission
before acting. It does not work, and it is worth recording why, because it is the
first thing a reasonable engineer reaches for:

- **A prompt is advisory.** The model can be talked out of it by content in the
  very data it is processing — an email, an imported row, a webhook payload. That
  content is untrusted input, and the agent reads it by design.
- **A prompt cannot be verified.** There is no artefact to check afterwards that
  proves a human approved. "The model said it would ask" is not evidence.
- **A prompt is not a boundary.** Even a compliant model can be bypassed by a
  caller who invokes the tool API directly. If approval lives in the prompt, it
  exists only on one code path.

So approval must be a **property of the server**, not an instruction to the model.
A tool that requires approval must be structurally unable to execute until a
signed human decision exists. This ADR fixes that structure.

## Decision

Implement human-in-the-loop execution as a **durable state machine in PostgreSQL**,
reusing the existing job queue, outbox, audit trail and RBAC. No new broker, no
workflow engine.

### 1. An approval-gated tool proposes; it does not execute

When the agent calls a tool whose risk tier is `confirm` or `destructive`, the
runtime does not invoke the handler. It writes an `agent_actions` row with
`status = 'proposed'`, moves the run to `waiting_approval`, and returns a
`pending` result to the agent. The agent stops there.

The only code path that executes an approval-gated tool is `ExecuteApprovedAction`,
and it is reachable only from a decision that passed signature verification.

### 2. Risk tiers are assigned by the tool, on the server

| Tier | Examples | Behaviour |
|---|---|---|
| `safe` | read, search, list, aggregate | Executes immediately; audited |
| `confirm` | create, update, send email, change stage | Proposes; waits for a human |
| `destructive` | delete, bulk-update above a threshold, export off-platform, change permissions | Proposes; waits for a human **and** a re-authentication factor |

The tier is a field on the tool definition in Go. The model cannot read it, lower
it, or argue with it. A tool marked `destructive` stays `destructive` no matter
what the agent's context says.

### 3. Write-before-execute

Every transition writes its audit entry **before** the effect, in the same
transaction as the state change:

- `proposed` → the `agent_actions` row itself is the durable record of intent.
- `approved` → audit `agent.action.approved` with `arguments_hash` and signature.
- `executed` → audit `agent.action.executed` with the result summary.
- `rejected` → audit `agent.action.rejected` with the operator's comment.

The intent is recorded before the effect, so an action can never exist without a
record of why it was allowed. The audit write uses the ambient transaction, which
makes it atomic with the state change it describes — the same property the rest of
the platform relies on.

### 4. Decisions are append-only and signed

`agent_approvals` is a separate table, never an update to `agent_actions`. If an
operator changes their mind, that is a new row, not a rewrite. The decision row
carries an HMAC over the canonical tuple

```
action_id | organization_id | arguments_hash | decided_by | decision | decided_at
```

keyed by a purpose-derived key from the platform secret (`forgecrm:approval:v1`,
following the existing `DeriveCSRF` / `WebhookSign` pattern). The signature is
produced by the server, not supplied by the client. Its purpose is to make the
record **tamper-evident against direct database access**: an attacker who can write
to `agent_approvals` but does not hold the platform secret cannot forge a decision
that verifies. The platform secret lives in the environment, never in the database.

### 5. The decision binds to the action the human saw

At `approve` time the server recomputes the canonical `arguments_hash` from the
stored arguments and compares it with the hash the client submits — the hash of
what the operator actually saw rendered. A mismatch is refused with `409
ACTION_CHANGED`. This closes the time-of-check/time-of-use gap: between the page
loading and the operator clicking approve, the action cannot have been altered
into something else.

### 6. A destructive decision needs a second factor, resolved from the session

The `destructive` tier requires more than an authenticated session, because the
actions in it are the ones where a single compromised session must not be enough.
The requirement is enforced in `Decide`, and the factor is a property of the
**session**, never of the request:

- `sessions.step_up_at` records when a session last presented a factor. It is set
  at login — the caller has just presented a password, and a TOTP code when MFA is
  enabled — and refreshed by `POST /api/v1/auth/step-up`.
- `authn.Session.SteppedUp()` reports whether that timestamp is within
  `StepUpWindow` (15 minutes). The window is short on purpose: the point of a
  step-up is that the operator is present *now*, not that they authenticated at
  some earlier point in a long-lived session.
- The handler resolves the flag from the session and passes it in `agent.Decider`.
  The decision body has no field for it, so a client cannot assert it. `Decide`
  loads the action first and consults the flag only when the stored tier is
  `destructive`; a refusal leaves the action `proposed` and executes nothing.
- The endpoint re-verifies a real factor — a TOTP or recovery code with MFA, the
  password without — and records which kind backed the decision in the audit
  entry. A connection check or a client-side flag would not be a factor.

Why `mfa_verified` was not reused: it is set once at login and stays true for the
whole session, so a session opened in the morning would satisfy "a second factor"
all day. Recency needs its own column.

### 7. Execution is idempotent

`agent_actions.idempotency_key` is unique. Execution transitions
`approved → executed` with a guard on the current status, so a double submit, a
retry or a reconnecting client produces **one** execution. A second attempt finds
the row already `executed` and returns the recorded result rather than performing
the effect again.

### 8. Waiting is durable and bounded

A run in `waiting_approval` holds no connection and no CPU. The state is a row, so
it survives a restart, a redeploy, or a multi-hour pause. `agent_actions.expires_at`
bounds the wait; an expiry sweeper moves overdue proposals to `expired` and fails
the run. Without a bound, "waiting" degrades into "hung forever".

### 9. Limits are enforced, not advised

- **Token budget per run** (`agent_runs.budget_tokens`). Exceeding it fails the run.
- **Action cap per run.** A run cannot propose without bound.
- **Scope.** The agent acts as `user_id`; every tool call goes through the same
  RBAC check as a human request. An approval does not and cannot grant a
  permission the user lacks.
- **Tenant isolation.** `organization_id` is checked in the same statement that
  mutates each row, so an id from another organization is indistinguishable from
  one that does not exist.
- **Denied tools.** A configured denylist (`users.delete`,
  `integrations.rotate_secret`, …) is unreachable by the agent regardless of
  approval.

### 10. No workflow engine

Durable waiting is a database row plus the existing job queue. Temporal or a
message broker would add an operational component and a new failure surface for a
state machine that fits in three tables. The threshold for revisiting this is
recorded in ADR 0002: a workflow that genuinely spans days with branching
compensation logic, which this is not.

## Consequences

- **Positive.** Approval cannot be bypassed by prompt injection, by calling the
  tool API directly, or by editing the action between display and decision. The
  only path to execution runs through a verified, signed decision.
- **Positive.** Every decision and execution is a durable, append-only, tamper-
  evident record, which is what makes the audit trail usable for an incident
  review rather than merely present.
- **Positive.** No new infrastructure. The feature ships in the existing two
  processes and one database.
- **Negative.** Three new tables and a new state machine to maintain, with their
  own expiry and sweep jobs.
- **Negative.** `destructive` actions need a second factor, which means an
  additional interaction the UI must implement and the API must enforce. Both
  sides exist: `POST /api/v1/auth/step-up` and the step-up prompt on the approvals
  page. The cost is a migration (`0011_session_step_up`) and one column, not a new
  component.
- **Neutral.** The agent runtime is a *consumer* of the tool registry, not a
  parallel implementation. It inherits the tool permission model rather than
  restating it, so the two cannot drift.

## Alternatives considered

- **Prompt-based approval.** Rejected: advisory, unverifiable, bypassable.
- **Approval as a column on `agent_actions`.** Rejected: rewriting a decision
  destroys the history the audit exists to preserve.
- **Client-supplied signature.** Rejected: a client that can sign can forge. The
  server signs; the client only proves it saw the same arguments.
- **Temporal / broker.** Rejected for now, per ADR 0002's extraction threshold.

## References

- `backend/internal/migrations/0009_agent_approvals.up.sql`
- `backend/internal/agent/` — the state machine
- `docs/SECURITY.md` — trust boundaries
- ADR 0004 — the same "guard in the statement" discipline applied to webhooks
