package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// Decision is an operator's verdict on a proposed action.
type Decision struct {
	// ArgumentsHash is the hash of the arguments the operator saw rendered. The
	// server compares it with the stored hash before accepting the decision, which
	// is what closes the window between display and decision.
	ArgumentsHash string
	Approve       bool
	Comment       string
}

// Approval is a stored decision.
type Approval struct {
	ID            uuid.UUID `json:"id"`
	ActionID      uuid.UUID `json:"action_id"`
	DecidedBy     uuid.UUID `json:"decided_by"`
	Decision      string    `json:"decision"`
	Comment       *string   `json:"comment,omitempty"`
	ArgumentsHash string    `json:"arguments_hash"`
	Signature     string    `json:"signature"`
	DecidedAt     time.Time `json:"decided_at"`
}

// Decide records an operator's verdict on a proposed action and, on approval,
// performs the action.
//
// The order of checks matters and is deliberate:
//
//  1. The action is loaded FOR UPDATE within the caller's organization, so two
//     decisions cannot interleave and an id from another organization is a
//     not-found rather than a distinguishable error.
//  2. The status must be `proposed`. A decision on an already-decided action is
//     refused, which is what makes a double click safe.
//  3. The deadline must not have passed.
//  4. The submitted arguments hash must equal the stored one. This is the
//     time-of-check/time-of-use guard: if the action changed after the operator
//     saw it, the decision does not apply to what they saw and is refused.
//
// Only after all four does the decision become durable and, for an approval, the
// effect run.
func (s *Service) Decide(ctx context.Context, d Decider, actionID uuid.UUID, decision Decision) (*Action, *Approval, error) {
	var (
		action Action
		appr   Approval
	)
	verdict := "reject"
	if decision.Approve {
		verdict = "approve"
	}

	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// Step 1: load and lock, scoped to the organization.
		const lockQ = `
			SELECT id, run_id, organization_id, tool_name, arguments, arguments_hash,
			       risk_tier, summary, status, idempotency_key, result, error,
			       expires_at, proposed_at, decided_at, executed_at
			FROM agent_actions
			WHERE id = $1 AND organization_id = $2
			FOR UPDATE`
		if err := scanAction(s.db.Q(ctx).QueryRow(ctx, lockQ, actionID, d.OrganizationID), &action); err != nil {
			if rowNotFound(err) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("lock action: %w", err)
		}

		// Step 2: only a proposed action can be decided.
		if action.Status != ActionProposed {
			return ErrAlreadyDecided
		}

		// Step 3: the deadline.
		if time.Now().After(action.ExpiresAt) {
			if _, err := s.db.Q(ctx).Exec(ctx,
				`UPDATE agent_actions SET status = $2 WHERE id = $1 AND status = 'proposed'`,
				action.ID, ActionExpired); err != nil {
				return fmt.Errorf("expire action: %w", err)
			}
			return ErrExpired
		}

		// Step 4: the action must be what the operator saw.
		if subtle.ConstantTimeCompare([]byte(decision.ArgumentsHash), []byte(action.ArgumentsHash)) != 1 {
			return ErrActionChanged
		}

		// Step 5: a destructive action needs a second factor. The check is here, after
		// the action is loaded, because the tier is a property of the stored action —
		// not of the request, where a caller could claim it.
		if action.RiskTier == TierDestructive && !d.StepUpVerified {
			return ErrStepUpRequired
		}

		// Truncated to microseconds because that is the precision PostgreSQL stores
		// for timestamptz. The signature is computed over this value and the row is
		// inserted with it; if the signed value carried nanoseconds the read-back
		// value would differ and no stored signature would ever verify.
		decidedAt := time.Now().UTC().Truncate(time.Microsecond)
		sig := decisionSignature(s.key, action.ID, action.OrganizationID, action.ArgumentsHash,
			d.UserID, verdict, decidedAt)

		const insertQ = `
			INSERT INTO agent_approvals
			    (action_id, organization_id, decided_by, decision, comment, arguments_hash, signature, decided_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, action_id, decided_by, decision, comment, arguments_hash, signature, decided_at`
		var comment *string
		if decision.Comment != "" {
			comment = &decision.Comment
		}
		if err := s.db.Q(ctx).QueryRow(ctx, insertQ,
			action.ID, action.OrganizationID, d.UserID, verdict, comment,
			action.ArgumentsHash, sig, decidedAt).
			Scan(&appr.ID, &appr.ActionID, &appr.DecidedBy, &appr.Decision, &appr.Comment,
				&appr.ArgumentsHash, &appr.Signature, &appr.DecidedAt); err != nil {
			return fmt.Errorf("insert approval: %w", err)
		}

		nextStatus := ActionRejected
		auditAction := audit.ActionAgentActionRejected
		if decision.Approve {
			nextStatus = ActionApproved
			auditAction = audit.ActionAgentActionApproved
		}
		const updQ = `
			UPDATE agent_actions SET status = $2, decided_at = $3
			WHERE id = $1 AND status = 'proposed'`
		tag, err := s.db.Q(ctx).Exec(ctx, updQ, action.ID, nextStatus, decidedAt)
		if err != nil {
			return fmt.Errorf("update action status: %w", err)
		}
		if tag.RowsAffected() == 0 {
			// The row was locked, so this can only mean a concurrent decision won a
			// race the lock should have serialised. Treat it as already decided.
			return ErrAlreadyDecided
		}
		action.Status = nextStatus
		action.DecidedAt = &decidedAt

		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &action.OrganizationID, ActorUserID: &d.UserID, ActorType: "user",
			Action: string(auditAction), EntityType: "agent_action", EntityID: action.ID.String(),
			Metadata: map[string]any{
				"decision": verdict, "tool": action.ToolName,
				"arguments_hash": action.ArgumentsHash, "signature": sig,
			},
		})
	})
	if err != nil {
		return nil, nil, err
	}

	// On rejection there is nothing more to do: the absence of an execution is the
	// point. On approval the effect runs, in its own transaction, so a failure to
	// execute does not undo the durable record that a human approved.
	if decision.Approve {
		executed, execErr := s.ExecuteApprovedAction(ctx, action.ID, d)
		if execErr != nil {
			// The decision stands; the execution did not. Report the action with its
			// post-execution status so the caller sees what actually happened.
			reloaded, getErr := s.GetAction(ctx, action.OrganizationID, action.ID)
			if getErr == nil {
				return reloaded, &appr, execErr
			}
			return &action, &appr, execErr
		}
		return executed, &appr, nil
	}
	return &action, &appr, nil
}

// ExecuteApprovedAction performs an approved action, exactly once.
//
// The transition `approved → executed` is the claim: it is a single guarded
// UPDATE, so of any number of concurrent or repeated calls exactly one moves the
// row and therefore exactly one performs the effect. A call that finds the row
// already executed returns the recorded result rather than acting again.
//
// The stored approval's signature is re-verified here, before the effect. That is
// what makes the signature load-bearing rather than decorative: an approval row
// forged by direct database access — without the platform secret — does not
// verify, and the action is refused instead of executed.
//
// The effect runs inside this transaction. For a CRM tool that writes to the same
// database, the effect and the status change commit together, so a crash leaves
// neither. A tool whose effect is external (a sent email, an outbound HTTP call)
// cannot be rolled back; such a tool must be idempotent under the action's
// idempotency key, which is recorded on the action for exactly that purpose.
func (s *Service) ExecuteApprovedAction(ctx context.Context, actionID uuid.UUID, decider Decider) (*Action, error) {
	if s.executor == nil {
		return nil, errors.New("agent: no executor configured")
	}
	var action Action
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// Load the action and its approval, scoped to the organization.
		const loadQ = `
			SELECT id, run_id, organization_id, tool_name, arguments, arguments_hash,
			       risk_tier, summary, status, idempotency_key, result, error,
			       expires_at, proposed_at, decided_at, executed_at
			FROM agent_actions
			WHERE id = $1 AND organization_id = $2
			FOR UPDATE`
		if err := scanAction(s.db.Q(ctx).QueryRow(ctx, loadQ, actionID, decider.OrganizationID), &action); err != nil {
			if rowNotFound(err) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load action for execution: %w", err)
		}

		if action.Status == ActionExecuted {
			// Idempotent replay: the effect already happened. Return the recorded
			// result without performing it again.
			return nil
		}
		if action.Status != ActionApproved {
			return fmt.Errorf("%w: status is %s", ErrRunNotActive, action.Status)
		}

		// Re-verify the approving decision's signature.
		const apprQ = `
			SELECT id, action_id, decided_by, decision, comment, arguments_hash, signature, decided_at
			FROM agent_approvals
			WHERE action_id = $1 AND decision = 'approve'
			ORDER BY decided_at DESC LIMIT 1`
		var appr Approval
		if err := s.db.Q(ctx).QueryRow(ctx, apprQ, action.ID).Scan(
			&appr.ID, &appr.ActionID, &appr.DecidedBy, &appr.Decision, &appr.Comment,
			&appr.ArgumentsHash, &appr.Signature, &appr.DecidedAt); err != nil {
			if rowNotFound(err) {
				return ErrBadSignature
			}
			return fmt.Errorf("load approval: %w", err)
		}
		expected := decisionSignature(s.key, appr.ActionID, action.OrganizationID,
			appr.ArgumentsHash, appr.DecidedBy, appr.Decision, appr.DecidedAt)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(appr.Signature)) != 1 {
			return ErrBadSignature
		}
		// The approval must bind to the action's current arguments.
		if subtle.ConstantTimeCompare([]byte(appr.ArgumentsHash), []byte(action.ArgumentsHash)) != 1 {
			return ErrActionChanged
		}

		// Claim: exactly one caller moves approved → executed.
		const claimQ = `
			UPDATE agent_actions SET status = $3, executed_at = now()
			WHERE id = $1 AND organization_id = $2 AND status = 'approved'
			RETURNING executed_at`
		var executedAt time.Time
		if err := s.db.Q(ctx).QueryRow(ctx, claimQ, action.ID, decider.OrganizationID, ActionExecuted).
			Scan(&executedAt); err != nil {
			if rowNotFound(err) {
				// Lost the claim race. Re-read and return the current state.
				return nil
			}
			return fmt.Errorf("claim action for execution: %w", err)
		}
		action.Status = ActionExecuted
		action.ExecutedAt = &executedAt

		// Resolve and run the effect. The handler receives the ambient context, so a
		// transactional tool shares this transaction and commits with the claim.
		exec, err := s.executor.Execution(action.ToolName)
		if err != nil {
			return fmt.Errorf("resolve tool %q: %w", action.ToolName, err)
		}
		result, err := exec(ctx, &action, decider)
		if err != nil {
			// Returning the error rolls the transaction back, so the action stays
			// approved and can be retried. The failure is recorded on a fresh
			// context, because this one is being rolled back.
			s.recordFailure(action, err)
			return fmt.Errorf("execute action: %w", err)
		}

		var raw json.RawMessage
		if result != nil {
			if b, mErr := json.Marshal(result); mErr == nil {
				raw = b
			}
		}
		if _, err := s.db.Q(ctx).Exec(ctx,
			`UPDATE agent_actions SET result = $2 WHERE id = $1`, action.ID, raw); err != nil {
			return fmt.Errorf("record action result: %w", err)
		}
		action.Result = raw

		// Write-before-execute is satisfied: the effect and this audit entry commit
		// together, so an executed action always has its record.
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &action.OrganizationID, ActorUserID: &decider.UserID, ActorType: "user",
			Action: string(audit.ActionAgentActionExecuted), EntityType: "agent_action",
			EntityID: action.ID.String(),
			Metadata: map[string]any{
				"tool": action.ToolName, "risk_tier": string(action.RiskTier),
				"arguments_hash": action.ArgumentsHash, "approved_by": appr.DecidedBy.String(),
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return &action, nil
}

// recordFailure writes the failure audit entry on a detached context, so it
// survives the rollback of the execution transaction.
func (s *Service) recordFailure(action Action, cause error) {
	if s.audit == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.audit.Record(ctx, audit.Entry{
		OrganizationID: &action.OrganizationID, ActorType: "system",
		Action: string(audit.ActionAgentActionFailed), EntityType: "agent_action",
		EntityID: action.ID.String(),
		Metadata: map[string]any{"tool": action.ToolName, "error": cause.Error()},
	})
}

// ListApprovals returns the decisions recorded for an action, newest first.
func (s *Service) ListApprovals(ctx context.Context, orgID, actionID uuid.UUID) ([]Approval, error) {
	const q = `
		SELECT id, action_id, decided_by, decision, comment, arguments_hash, signature, decided_at
		FROM agent_approvals
		WHERE action_id = $1 AND organization_id = $2
		ORDER BY decided_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, actionID, orgID)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var a Approval
		if err := rows.Scan(&a.ID, &a.ActionID, &a.DecidedBy, &a.Decision, &a.Comment,
			&a.ArgumentsHash, &a.Signature, &a.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// VerifyApproval re-checks a stored approval's signature. It exists so an operator
// or an auditor can confirm that a decision record has not been altered.
func (s *Service) VerifyApproval(orgID uuid.UUID, a Approval) bool {
	expected := decisionSignature(s.key, a.ActionID, orgID, a.ArgumentsHash,
		a.DecidedBy, a.Decision, a.DecidedAt)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(a.Signature)) == 1
}
