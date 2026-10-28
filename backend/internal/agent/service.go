package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// StartRun opens a new run for a goal, acting as userID within the organization.
//
// The budget is enforced from the first token: a run with a non-positive budget is
// refused rather than silently given an unlimited one, because an unlimited run is
// exactly the failure the budget exists to prevent.
func (s *Service) StartRun(ctx context.Context, orgID, userID uuid.UUID, goal string, budgetTokens, actionCap int) (*Run, error) {
	if budgetTokens <= 0 {
		return nil, httpx.ValidationError(map[string]string{"budget_tokens": "must be greater than zero"})
	}
	if actionCap <= 0 {
		actionCap = 50
	}
	var run Run
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO agent_runs (organization_id, user_id, goal, status, budget_tokens, action_cap)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, organization_id, user_id, goal, status, budget_tokens, tokens_used,
			          action_cap, actions_used, error, created_at, updated_at, finished_at`
		if err := scanRun(s.db.Q(ctx).QueryRow(ctx, q, orgID, userID, goal, RunRunning, budgetTokens, actionCap), &run); err != nil {
			return fmt.Errorf("start run: %w", err)
		}
		// Write-before-execute: the run's existence is recorded before any action it
		// might take, in the same transaction that created it.
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &orgID, ActorUserID: &userID, ActorType: "ai_agent",
			Action: string(audit.ActionAgentRunStarted), EntityType: "agent_run", EntityID: run.ID.String(),
			Metadata: map[string]any{"budget_tokens": budgetTokens, "action_cap": actionCap},
		})
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// GetRun returns a run scoped to its organization, or a not-found error.
func (s *Service) GetRun(ctx context.Context, orgID, runID uuid.UUID) (*Run, error) {
	const q = `
		SELECT id, organization_id, user_id, goal, status, budget_tokens, tokens_used,
		       action_cap, actions_used, error, created_at, updated_at, finished_at
		FROM agent_runs WHERE id = $1 AND organization_id = $2`
	var run Run
	if err := scanRun(s.db.Q(ctx).QueryRow(ctx, q, runID, orgID), &run); err != nil {
		if rowNotFound(err) {
			return nil, httpx.ErrNotFound
		}
		return nil, fmt.Errorf("get run: %w", err)
	}
	return &run, nil
}

// ListRuns returns the organization's runs, newest first.
func (s *Service) ListRuns(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]Run, int64, error) {
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM agent_runs WHERE organization_id = $1 AND ($2 = '' OR status = $2)`,
		orgID, status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}
	const q = `
		SELECT id, organization_id, user_id, goal, status, budget_tokens, tokens_used,
		       action_cap, actions_used, error, created_at, updated_at, finished_at
		FROM agent_runs
		WHERE organization_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID, status, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var run Run
		if err := scanRun(rows, &run); err != nil {
			return nil, 0, err
		}
		out = append(out, run)
	}
	return out, total, rows.Err()
}

// AddTokens records token usage against a run and fails the run if the budget is
// exceeded.
//
// The check and the increment are one statement: `WHERE tokens_used + $3 <=
// budget_tokens`. Two concurrent calls therefore cannot both observe headroom and
// both spend it, which a read-then-write would allow.
func (s *Service) AddTokens(ctx context.Context, orgID, runID uuid.UUID, tokens int) (*Run, error) {
	if tokens < 0 {
		return nil, httpx.ValidationError(map[string]string{"tokens": "must not be negative"})
	}
	const q = `
		UPDATE agent_runs SET tokens_used = tokens_used + $3
		WHERE id = $1 AND organization_id = $2 AND tokens_used + $3 <= budget_tokens
		RETURNING id, organization_id, user_id, goal, status, budget_tokens, tokens_used,
		          action_cap, actions_used, error, created_at, updated_at, finished_at`
	var run Run
	err := scanRun(s.db.Q(ctx).QueryRow(ctx, q, runID, orgID, tokens), &run)
	if err != nil {
		if !rowNotFound(err) {
			return nil, fmt.Errorf("add tokens: %w", err)
		}
		// Either the run is not this organization's, or the budget is exhausted.
		// Distinguish by looking once, so the caller learns the real reason without
		// a second code path that would leak another organization's ids.
		existing, getErr := s.GetRun(ctx, orgID, runID)
		if getErr != nil {
			return nil, getErr
		}
		_ = s.failRun(ctx, existing, ErrBudgetExceeded.Error())
		return nil, ErrBudgetExceeded
	}
	return &run, nil
}

// CompleteRun marks a run finished successfully.
func (s *Service) CompleteRun(ctx context.Context, orgID, runID uuid.UUID) error {
	const q = `
		UPDATE agent_runs SET status = $3, finished_at = now()
		WHERE id = $1 AND organization_id = $2 AND status IN ('running', 'waiting_approval')`
	tag, err := s.db.Q(ctx).Exec(ctx, q, runID, orgID, RunCompleted)
	if err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	s.record(ctx, audit.Entry{
		OrganizationID: &orgID, ActorType: "ai_agent",
		Action: string(audit.ActionAgentRunCompleted), EntityType: "agent_run", EntityID: runID.String(),
	})
	return nil
}

// CancelRun cancels a run and expires the proposals it left waiting.
//
// The proposals are expired in the same transaction as the cancellation, so a run
// can never be cancelled while leaving a proposal that looks decidable.
func (s *Service) CancelRun(ctx context.Context, orgID, runID uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const cancelQ = `
			UPDATE agent_runs SET status = $3, finished_at = now()
			WHERE id = $1 AND organization_id = $2
			  AND status NOT IN ('completed', 'failed', 'cancelled')`
		tag, err := s.db.Q(ctx).Exec(ctx, cancelQ, runID, orgID, RunCancelled)
		if err != nil {
			return fmt.Errorf("cancel run: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		const expireQ = `
			UPDATE agent_actions SET status = $2
			WHERE run_id = $1 AND status = 'proposed'`
		if _, err := s.db.Q(ctx).Exec(ctx, expireQ, runID, ActionExpired); err != nil {
			return fmt.Errorf("expire pending actions: %w", err)
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &orgID, ActorType: "ai_agent",
			Action: string(audit.ActionAgentRunFailed), EntityType: "agent_run", EntityID: runID.String(),
			Metadata: map[string]any{"cancelled": true},
		})
	})
}

// failRun moves a run to the failed state with a reason.
func (s *Service) failRun(ctx context.Context, run *Run, reason string) error {
	const q = `
		UPDATE agent_runs SET status = $3, error = $4, finished_at = now()
		WHERE id = $1 AND organization_id = $2 AND status NOT IN ('completed', 'failed', 'cancelled')`
	_, err := s.db.Q(ctx).Exec(ctx, q, run.ID, run.OrganizationID, RunFailed, reason)
	if err != nil {
		return fmt.Errorf("fail run: %w", err)
	}
	s.record(ctx, audit.Entry{
		OrganizationID: &run.OrganizationID, ActorType: "ai_agent",
		Action: string(audit.ActionAgentRunFailed), EntityType: "agent_run", EntityID: run.ID.String(),
		Metadata: map[string]any{"reason": reason},
	})
	return nil
}

// Propose records an action the agent wants to perform and, when the tier requires
// it, moves the run to waiting_approval.
//
// A safe-tier action is not proposed: it is executed immediately by the caller,
// because recording a decision for an action that needs none would fill the
// approvals queue with noise and train operators to click through it.
func (s *Service) Propose(ctx context.Context, p Proposal) (*Action, error) {
	if !p.RiskTier.Valid() {
		return nil, httpx.ValidationError(map[string]string{"risk_tier": "must be safe, confirm or destructive"})
	}
	if s.Denied(p.ToolName) {
		return nil, httpx.NewError(403, httpx.CodeForbidden,
			fmt.Sprintf("the tool %q is not available to agents", p.ToolName))
	}
	if p.IdempotencyKey == "" {
		return nil, httpx.ValidationError(map[string]string{"idempotency_key": "is required"})
	}
	hash, err := ArgumentsHash(p.Arguments)
	if err != nil {
		return nil, err
	}
	canonical, err := CanonicalArguments(p.Arguments)
	if err != nil {
		return nil, err
	}
	ttl := p.TTL
	if ttl <= 0 {
		ttl = s.defaultTTL
	}

	var action Action
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// The action cap is enforced in the same statement that reserves a slot, so
		// concurrent proposals cannot both see room and both proceed.
		const capQ = `
			UPDATE agent_runs SET actions_used = actions_used + 1
			WHERE id = $1 AND organization_id = $2
			  AND status IN ('running', 'waiting_approval')
			  AND actions_used + 1 <= action_cap
			RETURNING actions_used`
		var used int
		if err := s.db.Q(ctx).QueryRow(ctx, capQ, p.RunID, p.OrganizationID).Scan(&used); err != nil {
			if rowNotFound(err) {
				return ErrBudgetExceeded
			}
			return fmt.Errorf("reserve action slot: %w", err)
		}

		const insertQ = `
			INSERT INTO agent_actions
			    (run_id, organization_id, tool_name, arguments, arguments_hash, risk_tier,
			     summary, status, idempotency_key, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now() + $10::interval)
			ON CONFLICT (idempotency_key) DO NOTHING
			RETURNING id, run_id, organization_id, tool_name, arguments, arguments_hash,
			          risk_tier, summary, status, idempotency_key, result, error,
			          expires_at, proposed_at, decided_at, executed_at`
		row := s.db.Q(ctx).QueryRow(ctx, insertQ,
			p.RunID, p.OrganizationID, p.ToolName, canonical, hash, string(p.RiskTier),
			p.Summary, ActionProposed, p.IdempotencyKey, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
		if err := scanAction(row, &action); err != nil {
			if rowNotFound(err) {
				// The idempotency key already exists: a retried proposal. Return the
				// existing action rather than creating a second one.
				return s.loadByIdempotencyKey(ctx, p.IdempotencyKey, &action)
			}
			return fmt.Errorf("insert action: %w", err)
		}

		// Only a tier that requires a decision moves the run into waiting.
		if p.RiskTier.RequiresApproval() {
			if _, err := s.db.Q(ctx).Exec(ctx,
				`UPDATE agent_runs SET status = $3 WHERE id = $1 AND organization_id = $2
				 AND status = 'running'`,
				p.RunID, p.OrganizationID, RunWaitingApproval); err != nil {
				return fmt.Errorf("move run to waiting: %w", err)
			}
		}

		// Write-before-execute: the intent is recorded in the same transaction that
		// created the proposal, before any effect can exist.
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorType: "ai_agent",
			Action: string(audit.ActionAgentActionProposed), EntityType: "agent_action",
			EntityID: action.ID.String(),
			Metadata: map[string]any{
				"tool": p.ToolName, "risk_tier": string(p.RiskTier),
				"arguments_hash": hash, "run_id": p.RunID.String(),
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return &action, nil
}

// loadByIdempotencyKey loads an existing action by its unique key.
func (s *Service) loadByIdempotencyKey(ctx context.Context, key string, out *Action) error {
	const q = `
		SELECT id, run_id, organization_id, tool_name, arguments, arguments_hash,
		       risk_tier, summary, status, idempotency_key, result, error,
		       expires_at, proposed_at, decided_at, executed_at
		FROM agent_actions WHERE idempotency_key = $1`
	if err := scanAction(s.db.Q(ctx).QueryRow(ctx, q, key), out); err != nil {
		return fmt.Errorf("load action by idempotency key: %w", err)
	}
	return nil
}

// GetAction returns an action scoped to its organization.
func (s *Service) GetAction(ctx context.Context, orgID, actionID uuid.UUID) (*Action, error) {
	const q = `
		SELECT id, run_id, organization_id, tool_name, arguments, arguments_hash,
		       risk_tier, summary, status, idempotency_key, result, error,
		       expires_at, proposed_at, decided_at, executed_at
		FROM agent_actions WHERE id = $1 AND organization_id = $2`
	var action Action
	if err := scanAction(s.db.Q(ctx).QueryRow(ctx, q, actionID, orgID), &action); err != nil {
		if rowNotFound(err) {
			return nil, httpx.ErrNotFound
		}
		return nil, fmt.Errorf("get action: %w", err)
	}
	return &action, nil
}

// ListActions returns actions in an organization, newest first, optionally
// filtered by status.
func (s *Service) ListActions(ctx context.Context, orgID uuid.UUID, status string, limit, offset int) ([]Action, int64, error) {
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM agent_actions WHERE organization_id = $1 AND ($2 = '' OR status = $2)`,
		orgID, status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count actions: %w", err)
	}
	const q = `
		SELECT id, run_id, organization_id, tool_name, arguments, arguments_hash,
		       risk_tier, summary, status, idempotency_key, result, error,
		       expires_at, proposed_at, decided_at, executed_at
		FROM agent_actions
		WHERE organization_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY proposed_at DESC LIMIT $3 OFFSET $4`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID, status, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		if err := scanAction(rows, &a); err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// SweepExpired moves proposals past their deadline to expired and fails the runs
// that were waiting on them. It returns the number of proposals expired.
//
// This is the bound that stops "waiting for approval" from becoming "hung
// forever". It is safe to run concurrently: the status guard means a proposal is
// expired once even if several workers sweep at the same moment.
func (s *Service) SweepExpired(ctx context.Context) (int64, error) {
	const q = `
		UPDATE agent_actions SET status = $1
		WHERE status = 'proposed' AND expires_at < now()`
	tag, err := s.db.Q(ctx).Exec(ctx, q, ActionExpired)
	if err != nil {
		return 0, fmt.Errorf("sweep expired actions: %w", err)
	}
	n := tag.RowsAffected()
	if n == 0 {
		return 0, nil
	}
	// A run waiting on a proposal that expired can make no progress, so it is
	// failed rather than left waiting for a decision that can no longer arrive.
	const runQ = `
		UPDATE agent_runs r SET status = $1, error = $2, finished_at = now()
		WHERE r.status = 'waiting_approval'
		  AND NOT EXISTS (
		      SELECT 1 FROM agent_actions a
		      WHERE a.run_id = r.id AND a.status = 'proposed'
		  )`
	if _, err := s.db.Q(ctx).Exec(ctx, runQ, RunFailed, "approval request expired"); err != nil {
		return n, fmt.Errorf("fail runs whose proposals expired: %w", err)
	}
	return n, nil
}

// scanRun reads a run row.
func scanRun(row interface{ Scan(...any) error }, run *Run) error {
	return row.Scan(&run.ID, &run.OrganizationID, &run.UserID, &run.Goal, &run.Status,
		&run.BudgetTokens, &run.TokensUsed, &run.ActionCap, &run.ActionsUsed,
		&run.Error, &run.CreatedAt, &run.UpdatedAt, &run.FinishedAt)
}

// scanAction reads an action row.
func scanAction(row interface{ Scan(...any) error }, a *Action) error {
	return row.Scan(&a.ID, &a.RunID, &a.OrganizationID, &a.ToolName, &a.Arguments,
		&a.ArgumentsHash, &a.RiskTier, &a.Summary, &a.Status, &a.IdempotencyKey,
		&a.Result, &a.Error, &a.ExpiresAt, &a.ProposedAt, &a.DecidedAt, &a.ExecutedAt)
}

// record writes an audit entry, tolerating a nil logger so a partially wired
// service does not panic on a refusal path.
func (s *Service) record(ctx context.Context, e audit.Entry) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Record(ctx, e)
}

// now is indirected so tests can reason about expiry without sleeping.
var now = time.Now
