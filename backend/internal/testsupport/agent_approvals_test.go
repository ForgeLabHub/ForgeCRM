//go:build integration

// Integration tests for the human-in-the-loop agent execution model (ADR 0005).
//
// These tests drive the real state machine against a real PostgreSQL schema and a
// real tool registry. Nothing is mocked, because every property that matters here
// is a property of the interaction between the code and the database:
//
//   - the approved→executed claim is a guarded UPDATE, so "exactly once" is a
//     claim about concurrent transactions, not about a function's return value;
//   - the decision signature is verified against a stored row, so tamper-evidence
//     is a claim about what the database holds;
//   - the deny list and the budget are enforced in SQL, so they are claims about
//     what can be written.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/agent"
	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// agentFixture is a fully wired agent runtime over a migrated database.
type agentFixture struct {
	db     *database.DB
	svc    *agent.Service
	org    uuid.UUID
	userID uuid.UUID
}

// newAgentFixture builds the service graph the API builds, over the test schema.
func newAgentFixture(t *testing.T, orgSlug, roleKey string) *agentFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	auditLog := audit.New(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	registry := ai.NewToolRegistry(crmSvc, auditLog)
	executor := ai.NewAgentExecutor(db, rbac.NewService(db), registry)

	svc, err := agent.NewService(db, auditLog, executor, agent.Config{
		PlatformSecret: "integration-test-platform-secret",
		DeniedTools:    ai.DeniedToolNames(),
	})
	if err != nil {
		t.Fatalf("build the agent service: %v", err)
	}

	org := insertOrganization(t, db, orgSlug)
	userID := seedMember(t, db, org, roleKey)

	return &agentFixture{db: db, svc: svc, org: org, userID: userID}
}

// seedMember creates an active user who is a member of the organization holding
// the given role template's permissions.
func seedMember(t *testing.T, db *database.DB, org uuid.UUID, roleKey string) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var userID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
INSERT INTO users (email, password_hash, display_name, status)
VALUES ($1, 'x', 'Agent Operator', 'active') RETURNING id`,
		"agent-"+uuid.NewString()[:8]+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO memberships (user_id, organization_id, status) VALUES ($1, $2, 'active')`,
		userID, org); err != nil {
		t.Fatalf("add membership: %v", err)
	}

	// Copy the platform template into the organization, which is what the real
	// provisioning path does, then grant it. A user with no role would have no
	// permissions and every execution would be refused for the wrong reason.
	var roleID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
INSERT INTO roles (organization_id, key, name, is_system)
SELECT $1, key, name, true FROM roles
WHERE organization_id IS NULL AND key = $2
RETURNING id`, org, roleKey).Scan(&roleID); err != nil {
		t.Fatalf("provision role %q: %v", roleKey, err)
	}
	if _, err := db.Pool().Exec(ctx, `
INSERT INTO role_permissions (role_id, permission_key)
SELECT $1, permission_key FROM role_permissions rp
JOIN roles r ON r.id = rp.role_id
WHERE r.organization_id IS NULL AND r.key = $2`, roleID, roleKey); err != nil {
		t.Fatalf("copy role permissions for %q: %v", roleKey, err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id, organization_id) VALUES ($1, $2, $3)`,
		userID, roleID, org); err != nil {
		t.Fatalf("grant role %q: %v", roleKey, err)
	}
	return userID
}

// startRun opens a run with a generous budget.
func (f *agentFixture) startRun(t *testing.T, goal string) *agent.Run {
	t.Helper()
	run, err := f.svc.StartRun(context.Background(), f.org, f.userID, goal, 100000, 50)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return run
}

// proposeCreateTask proposes the confirm-tier create_task tool.
func (f *agentFixture) proposeCreateTask(t *testing.T, run *agent.Run, title, key string) *agent.Action {
	t.Helper()
	args := map[string]any{"title": title}
	hash, err := agent.ArgumentsHash(args)
	if err != nil {
		t.Fatalf("hash arguments: %v", err)
	}
	action, err := f.svc.Propose(context.Background(), agent.Proposal{
		RunID:          run.ID,
		OrganizationID: f.org,
		ToolName:       "create_task",
		Arguments:      args,
		RiskTier:       agent.TierConfirm,
		Summary:        "Create a task",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("propose action: %v", err)
	}
	if action.ArgumentsHash != hash {
		t.Fatalf("stored hash %q does not match the computed hash %q", action.ArgumentsHash, hash)
	}
	return action
}

// countTasks counts tasks in the organization.
func (f *agentFixture) countTasks(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM tasks WHERE organization_id = $1`, f.org).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

// TestAgentApprovalExecutesOnce is the core property: an approved action performs
// its effect, a repeated execution does not perform it again, and the task the
// agent asked for exists exactly once.
func TestAgentApprovalExecutesOnce(t *testing.T) {
	f := newAgentFixture(t, "agent-happy", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a follow-up task")
	action := f.proposeCreateTask(t, run, "Follow up with Acme", "happy-1")

	// The run must be waiting: a confirm-tier proposal suspends it.
	waiting, err := f.svc.GetRun(ctx, f.org, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if waiting.Status != agent.RunWaitingApproval {
		t.Fatalf("run status = %q, want %q", waiting.Status, agent.RunWaitingApproval)
	}

	decider := agent.Decider{UserID: f.userID, OrganizationID: f.org}
	decided, _, err := f.svc.Decide(ctx, decider, action.ID, agent.Decision{
		Approve: true, ArgumentsHash: action.ArgumentsHash,
	})
	if err != nil {
		t.Fatalf("decide approve: %v", err)
	}
	if decided.Status != agent.ActionExecuted {
		t.Fatalf("action status = %q, want %q", decided.Status, agent.ActionExecuted)
	}
	if n := f.countTasks(t); n != 1 {
		t.Fatalf("task count = %d, want 1", n)
	}

	// A second execution attempt must be a no-op: the effect already happened.
	replayed, err := f.svc.ExecuteApprovedAction(ctx, action.ID, decider)
	if err != nil {
		t.Fatalf("replay execution: %v", err)
	}
	if replayed.Status != agent.ActionExecuted {
		t.Fatalf("replayed status = %q, want %q", replayed.Status, agent.ActionExecuted)
	}
	if n := f.countTasks(t); n != 1 {
		t.Fatalf("task count after replay = %d, want 1: the effect ran twice", n)
	}
}

// TestAgentDecisionRefusesChangedArguments is the time-of-check/time-of-use guard.
// If the action changes after the operator saw it, the decision does not apply to
// what they saw and must be refused.
func TestAgentDecisionRefusesChangedArguments(t *testing.T) {
	f := newAgentFixture(t, "agent-toctou", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	action := f.proposeCreateTask(t, run, "Original title", "toctou-1")

	decider := agent.Decider{UserID: f.userID, OrganizationID: f.org}
	// The operator submits a hash that does not match the stored one: they approved
	// something else.
	_, _, err := f.svc.Decide(ctx, decider, action.ID, agent.Decision{
		Approve: true, ArgumentsHash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	if !errors.Is(err, agent.ErrActionChanged) {
		t.Fatalf("decide with a mismatched hash: err = %v, want ErrActionChanged", err)
	}
	if n := f.countTasks(t); n != 0 {
		t.Fatalf("task count = %d, want 0: a refused decision must not execute", n)
	}

	// The action must still be proposed, so a correct decision can follow.
	still, err := f.svc.GetAction(ctx, f.org, action.ID)
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if still.Status != agent.ActionProposed {
		t.Fatalf("action status = %q, want it to remain proposed", still.Status)
	}
}

// TestAgentActionCannotBeDecidedTwice makes a double click safe.
func TestAgentActionCannotBeDecidedTwice(t *testing.T) {
	f := newAgentFixture(t, "agent-twice", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	action := f.proposeCreateTask(t, run, "Decide me once", "twice-1")

	decider := agent.Decider{UserID: f.userID, OrganizationID: f.org}
	if _, _, err := f.svc.Decide(ctx, decider, action.ID, agent.Decision{
		Approve: true, ArgumentsHash: action.ArgumentsHash,
	}); err != nil {
		t.Fatalf("first decision: %v", err)
	}

	_, _, err := f.svc.Decide(ctx, decider, action.ID, agent.Decision{
		Approve: false, ArgumentsHash: action.ArgumentsHash,
	})
	if !errors.Is(err, agent.ErrAlreadyDecided) {
		t.Fatalf("second decision: err = %v, want ErrAlreadyDecided", err)
	}
	if n := f.countTasks(t); n != 1 {
		t.Fatalf("task count = %d, want 1", n)
	}
}

// TestAgentCrossOrganizationActionIsInvisible is the tenant-isolation check. A
// decider in another organization must not be able to decide this action, and must
// not be able to tell it exists.
func TestAgentCrossOrganizationActionIsInvisible(t *testing.T) {
	f := newAgentFixture(t, "agent-iso-a", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	action := f.proposeCreateTask(t, run, "Tenant A task", "iso-1")

	otherOrg := insertOrganization(t, f.db, "agent-iso-b")
	otherUser := seedMember(t, f.db, otherOrg, "sales_representative")

	_, _, err := f.svc.Decide(ctx, agent.Decider{UserID: otherUser, OrganizationID: otherOrg}, action.ID,
		agent.Decision{Approve: true, ArgumentsHash: action.ArgumentsHash})
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("cross-organization decision: err = %v, want ErrNotFound", err)
	}
	if n := f.countTasks(t); n != 0 {
		t.Fatalf("task count = %d, want 0", n)
	}
}

// TestAgentExecutionRefusesForgedSignature is what makes the signature
// load-bearing rather than decorative. An approval row written by direct database
// access, without the platform secret, does not verify and the action is refused
// instead of executed.
func TestAgentExecutionRefusesForgedSignature(t *testing.T) {
	f := newAgentFixture(t, "agent-forge", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	action := f.proposeCreateTask(t, run, "Forged approval", "forge-1")

	decider := agent.Decider{UserID: f.userID, OrganizationID: f.org}
	if _, _, err := f.svc.Decide(ctx, decider, action.ID, agent.Decision{
		Approve: true, ArgumentsHash: action.ArgumentsHash,
	}); err != nil {
		t.Fatalf("decide approve: %v", err)
	}

	// Simulate an attacker with write access to the database but not the secret:
	// they flip the approval to a decision they can produce a plausible signature
	// for, and reset the action so it can be executed again.
	if _, err := f.db.Pool().Exec(ctx,
		`UPDATE agent_approvals SET signature = 'forged' WHERE action_id = $1`, action.ID); err != nil {
		t.Fatalf("forge the signature: %v", err)
	}
	if _, err := f.db.Pool().Exec(ctx,
		`UPDATE agent_actions SET status = 'approved' WHERE id = $1`, action.ID); err != nil {
		t.Fatalf("reset the action: %v", err)
	}

	if _, err := f.svc.ExecuteApprovedAction(ctx, action.ID, decider); !errors.Is(err, agent.ErrBadSignature) {
		t.Fatalf("execute with a forged signature: err = %v, want ErrBadSignature", err)
	}
	if n := f.countTasks(t); n != 1 {
		t.Fatalf("task count = %d, want 1: the forged approval must not execute a second effect", n)
	}
}

// TestAgentSweepExpiresUndecidedProposals checks that "waiting for approval"
// cannot become "waiting forever": an undecided proposal expires and the run that
// was waiting on it fails rather than hanging.
func TestAgentSweepExpiresUndecidedProposals(t *testing.T) {
	f := newAgentFixture(t, "agent-sweep", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task nobody decides")
	action := f.proposeCreateTask(t, run, "Never decided", "sweep-1")

	// Age the proposal past its deadline.
	if _, err := f.db.Pool().Exec(ctx,
		`UPDATE agent_actions SET expires_at = now() - interval '1 hour' WHERE id = $1`, action.ID); err != nil {
		t.Fatalf("age the proposal: %v", err)
	}

	n, err := f.svc.SweepExpired(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d proposals, want 1", n)
	}

	swept, err := f.svc.GetAction(ctx, f.org, action.ID)
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if swept.Status != agent.ActionExpired {
		t.Fatalf("action status = %q, want %q", swept.Status, agent.ActionExpired)
	}
	// A decision on an expired proposal must be refused.
	if _, _, err := f.svc.Decide(ctx, agent.Decider{UserID: f.userID, OrganizationID: f.org}, action.ID,
		agent.Decision{Approve: true, ArgumentsHash: action.ArgumentsHash}); err == nil {
		t.Fatal("a decision on an expired proposal was accepted")
	}

	after, err := f.svc.GetRun(ctx, f.org, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if after.Status != agent.RunFailed {
		t.Fatalf("run status = %q, want %q: a run waiting on an expired proposal must not hang",
			after.Status, agent.RunFailed)
	}
}

// TestAgentDeniedToolCannotBeProposed checks the deny list is consulted before the
// insert, so a denied tool cannot even enter the queue to be approved later.
func TestAgentDeniedToolCannotBeProposed(t *testing.T) {
	f := newAgentFixture(t, "agent-deny", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Try a denied tool")
	_, err := f.svc.Propose(ctx, agent.Proposal{
		RunID:          run.ID,
		OrganizationID: f.org,
		ToolName:       "users.delete",
		Arguments:      map[string]any{"id": "someone"},
		RiskTier:       agent.TierDestructive,
		Summary:        "Delete a user",
		IdempotencyKey: "deny-1",
	})
	if err == nil {
		t.Fatal("a denied tool was proposed")
	}
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("proposing a denied tool: err = %v, want a 403", err)
	}
}

// TestAgentActionCapIsEnforced checks the cap bounds how many proposals a looping
// agent can create, which is what stops it filling the approvals queue.
func TestAgentActionCapIsEnforced(t *testing.T) {
	f := newAgentFixture(t, "agent-cap", "sales_representative")
	ctx := context.Background()

	run, err := f.svc.StartRun(ctx, f.org, f.userID, "Loop", 100000, 1)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	f.proposeCreateTask(t, run, "First", "cap-1")

	_, err = f.svc.Propose(ctx, agent.Proposal{
		RunID:          run.ID,
		OrganizationID: f.org,
		ToolName:       "create_task",
		Arguments:      map[string]any{"title": "Second"},
		RiskTier:       agent.TierConfirm,
		Summary:        "Second task",
		IdempotencyKey: "cap-2",
	})
	if !errors.Is(err, agent.ErrBudgetExceeded) {
		t.Fatalf("second proposal: err = %v, want ErrBudgetExceeded", err)
	}
}

// TestAgentTokenBudgetFailsTheRun checks the token budget is a hard limit: the run
// fails when it is exceeded rather than continuing to spend.
func TestAgentTokenBudgetFailsTheRun(t *testing.T) {
	f := newAgentFixture(t, "agent-budget", "sales_representative")
	ctx := context.Background()

	run, err := f.svc.StartRun(ctx, f.org, f.userID, "Spend", 100, 50)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	if _, err := f.svc.AddTokens(ctx, f.org, run.ID, 60); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if _, err := f.svc.AddTokens(ctx, f.org, run.ID, 60); !errors.Is(err, agent.ErrBudgetExceeded) {
		t.Fatalf("overspend: err = %v, want ErrBudgetExceeded", err)
	}

	after, err := f.svc.GetRun(ctx, f.org, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if after.Status != agent.RunFailed {
		t.Fatalf("run status = %q, want %q after the budget was exceeded", after.Status, agent.RunFailed)
	}
}

// TestAgentExecutionUsesCurrentPermissions is the "no stale authority" check: a
// role revoked between the proposal and the approval takes effect, so an approval
// cannot be used to exercise a permission the user no longer holds.
func TestAgentExecutionUsesCurrentPermissions(t *testing.T) {
	f := newAgentFixture(t, "agent-revoke", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	action := f.proposeCreateTask(t, run, "After revocation", "revoke-1")

	// Revoke the user's roles after the proposal was made.
	if _, err := f.db.Pool().Exec(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND organization_id = $2`, f.userID, f.org); err != nil {
		t.Fatalf("revoke roles: %v", err)
	}

	decider := agent.Decider{UserID: f.userID, OrganizationID: f.org}
	_, _, err := f.svc.Decide(ctx, decider, action.ID, agent.Decision{
		Approve: true, ArgumentsHash: action.ArgumentsHash,
	})
	if err == nil {
		t.Fatal("execution succeeded with a revoked permission set")
	}
	if n := f.countTasks(t); n != 0 {
		t.Fatalf("task count = %d, want 0: a revoked permission must block the effect", n)
	}

	// The decision itself stands: the human did decide, and the audit trail must
	// record that. It is the effect that was refused.
	after, err := f.svc.GetAction(ctx, f.org, action.ID)
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if after.Status == agent.ActionExecuted {
		t.Fatal("the action is marked executed though the effect was refused")
	}
	approvals, err := f.svc.ListApprovals(ctx, f.org, action.ID)
	if err != nil {
		t.Fatalf("list approvals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Decision != "approve" {
		t.Fatalf("approvals = %+v, want exactly one recorded approval", approvals)
	}
}

// TestAgentDestructiveActionRequiresStepUp is the ADR 0005 requirement that the
// destructive tier needs more than an authenticated session: a second factor. The
// decision from a session that has not presented one must be refused, and the
// refusal must not be a claim the caller can make — it is a property of the
// decider the server resolved.
func TestAgentDestructiveActionRequiresStepUp(t *testing.T) {
	f := newAgentFixture(t, "agent-stepup", "sales_manager")
	ctx := context.Background()

	run := f.startRun(t, "Export everything off-platform")
	args := map[string]any{"account_id": uuid.NewString()}
	hash, err := agent.ArgumentsHash(args)
	if err != nil {
		t.Fatalf("hash arguments: %v", err)
	}
	action, err := f.svc.Propose(ctx, agent.Proposal{
		RunID:          run.ID,
		OrganizationID: f.org,
		ToolName:       "export_account",
		Arguments:      args,
		RiskTier:       agent.TierDestructive,
		Summary:        "Export an account off-platform",
		IdempotencyKey: "stepup-1",
	})
	if err != nil {
		t.Fatalf("propose destructive action: %v", err)
	}
	if action.ArgumentsHash != hash {
		t.Fatalf("stored hash %q != computed %q", action.ArgumentsHash, hash)
	}

	// Without a step-up the decision is refused, and nothing is executed.
	_, _, err = f.svc.Decide(ctx, agent.Decider{UserID: f.userID, OrganizationID: f.org},
		action.ID, agent.Decision{Approve: true, ArgumentsHash: action.ArgumentsHash})
	if !errors.Is(err, agent.ErrStepUpRequired) {
		t.Fatalf("destructive decision without step-up: err = %v, want ErrStepUpRequired", err)
	}
	still, err := f.svc.GetAction(ctx, f.org, action.ID)
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if still.Status != agent.ActionProposed {
		t.Fatalf("action status = %q, want it to remain proposed after a refused decision", still.Status)
	}

	// With a step-up the same decision is accepted. The action then proceeds to
	// execution and fails on the unknown tool, which is a different failure from the
	// step-up refusal: the point is that the step-up gate was passed.
	_, _, err = f.svc.Decide(ctx, agent.Decider{UserID: f.userID, OrganizationID: f.org, StepUpVerified: true},
		action.ID, agent.Decision{Approve: true, ArgumentsHash: action.ArgumentsHash})
	if errors.Is(err, agent.ErrStepUpRequired) {
		t.Fatalf("the step-up gate refused a decision that carried a verified step-up")
	}
	decided, err := f.svc.GetAction(ctx, f.org, action.ID)
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if decided.Status == agent.ActionProposed {
		t.Fatalf("the action is still proposed after a verified step-up decision: err = %v", err)
	}
}

// TestAgentConfirmActionNeedsNoStepUp checks the requirement is scoped to the
// destructive tier. A confirm-tier action is decided by an authenticated session
// alone; requiring a step-up for every task creation would train operators to
// click through it.
func TestAgentConfirmActionNeedsNoStepUp(t *testing.T) {
	f := newAgentFixture(t, "agent-nostepup", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	action := f.proposeCreateTask(t, run, "No step-up needed", "nostepup-1")

	_, _, err := f.svc.Decide(ctx, agent.Decider{UserID: f.userID, OrganizationID: f.org},
		action.ID, agent.Decision{Approve: true, ArgumentsHash: action.ArgumentsHash})
	if err != nil {
		t.Fatalf("confirm-tier decision without a step-up was refused: %v", err)
	}
	if n := f.countTasks(t); n != 1 {
		t.Fatalf("task count = %d, want 1", n)
	}
}

// TestAgentProposalIsIdempotentUnderItsKey checks that a retried proposal returns
// the existing action rather than creating a second one, so a retry cannot put two
// copies of the same effect in the queue.
func TestAgentProposalIsIdempotentUnderItsKey(t *testing.T) {
	f := newAgentFixture(t, "agent-idem", "sales_representative")
	ctx := context.Background()

	run := f.startRun(t, "Create a task")
	first := f.proposeCreateTask(t, run, "Idempotent", "idem-1")

	second, err := f.svc.Propose(ctx, agent.Proposal{
		RunID:          run.ID,
		OrganizationID: f.org,
		ToolName:       "create_task",
		Arguments:      map[string]any{"title": "Idempotent"},
		RiskTier:       agent.TierConfirm,
		Summary:        "Create a task",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("retried proposal: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("the retried proposal created a second action: %s != %s", second.ID, first.ID)
	}
}
