//go:build integration

// Integration tests for the workflow automation engine.
//
// These run the real engine against a real PostgreSQL schema and the real
// crm.Service. Nothing is mocked, because the properties that matter are properties
// of the interaction between the code and the database:
//
//   - de-duplication is a UNIQUE constraint, so "a replayed event cannot run a
//     workflow twice" is a claim about inserts, not about a Go map;
//   - the permission check reads the author's effective permissions from the
//     database at execution time, so "revoking a permission stops the workflow" is a
//     claim about what the tables hold;
//   - the delay mechanism writes a resume instant, so "a paused run resumes" is a
//     claim about what the scheduler can select.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// workflowFixture is a fully wired workflow engine over a migrated database.
type workflowFixture struct {
	db     *database.DB
	engine *workflow.Engine
	store  *workflow.Store
	rbac   *rbac.Service
	org    uuid.UUID
	userID uuid.UUID
}

// newWorkflowFixture builds the engine the wiring package builds, over the test
// schema. The author is seeded with the given role, which the permission checks
// resolve against.
func newWorkflowFixture(t *testing.T, orgSlug, roleKey string) *workflowFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	auditLog := audit.New(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	rbacSvc := rbac.NewService(db)

	engine := workflow.NewEngine(workflow.Config{
		DB:     db,
		CRM:    crmSvc,
		RBAC:   rbacSvc,
		Audit:  auditLog,
		Outbox: outbox.NewStore(db),
		Log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})

	org := insertOrganization(t, db, orgSlug)
	userID := seedMember(t, db, org, roleKey)

	return &workflowFixture{
		db:     db,
		engine: engine,
		store:  engine.Store(),
		rbac:   rbacSvc,
		org:    org,
		userID: userID,
	}
}

// principal resolves the author's effective permissions from the database, which is
// what the API does for a request and what the engine does for a run.
func (f *workflowFixture) principal(t *testing.T) *rbac.Principal {
	t.Helper()
	perms, err := f.rbac.EffectivePermissions(context.Background(), f.userID, f.org)
	if err != nil {
		t.Fatalf("resolve effective permissions: %v", err)
	}
	return rbac.NewPrincipal(f.userID, "author@example.test", "Author", f.org, perms)
}

// create makes a workflow through the store, so validation and the denormalised
// trigger columns are exercised exactly as the API would.
func (f *workflowFixture) create(t *testing.T, name string, in workflow.CreateInput) *workflow.Workflow {
	t.Helper()
	in.Name = name
	wf, err := f.store.Create(context.Background(), f.principal(t), in)
	if err != nil {
		t.Fatalf("create workflow %q: %v", name, err)
	}
	return wf
}

// TestWorkflowManualRunExecutesAStep proves the end-to-end path: a workflow defined
// through the store, run through the engine, writes a real CRM record and records a
// successful step. This is the check that the whole vertical slice works.
func TestWorkflowManualRunExecutesAStep(t *testing.T) {
	f := newWorkflowFixture(t, "wf-manual", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[
		{"id":"t1","type":"create_task","args":{"title":"Follow up with a customer","priority":"high"}}
	]}`)
	wf := f.create(t, "On new account", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	result, err := f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run the workflow: %v", err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("run status = %q, want succeeded", result.Status)
	}

	var title string
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT title FROM tasks WHERE organization_id = $1`, f.org).Scan(&title); err != nil {
		t.Fatalf("the task the workflow claimed to create is not in the database: %v", err)
	}
	if title == "" {
		t.Fatal("the created task has an empty title")
	}

	var stepType, stepStatus string
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT step_type, status FROM workflow_step_runs WHERE run_id = $1`, result.RunID).
		Scan(&stepType, &stepStatus); err != nil {
		t.Fatalf("read the step run: %v", err)
	}
	if stepType != "create_task" || stepStatus != "succeeded" {
		t.Fatalf("step run = (%q,%q), want (create_task,succeeded)", stepType, stepStatus)
	}
}

// TestWorkflowEventTriggerIsDeDuplicated is the at-least-once safety property. The
// outbox redelivers on failure, so the same event can reach the engine twice; the
// unique constraint must make the second delivery a no-op rather than a second task.
func TestWorkflowEventTriggerIsDeDuplicated(t *testing.T) {
	f := newWorkflowFixture(t, "wf-dedup", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[
		{"id":"t1","type":"create_task","args":{"title":"Called from an event"}}
	]}`)
	f.create(t, "On account created", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerEvent,
		EventType:   strPtrTest("account.created"),
	})

	ev := outbox.Event{
		ID:             uuid.New(),
		OrganizationID: &f.org,
		Type:           "account.created",
		Source:         "forgecrm",
		SchemaVersion:  1,
		Payload:        json.RawMessage(`{"account_name":"Acme"}`),
	}

	f.engine.HandleEvent(ctx, ev)
	f.engine.HandleEvent(ctx, ev)
	f.engine.HandleEvent(ctx, ev)

	var tasks int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id = $1`, f.org).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 1 {
		t.Fatalf("tasks created = %d, want 1: a redelivered event ran the workflow again", tasks)
	}

	var runs int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM workflow_runs WHERE organization_id = $1`, f.org).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runs != 1 {
		t.Fatalf("workflow runs = %d, want 1", runs)
	}
}

// TestWorkflowPermissionIsRecheckedAtExecutionTime proves that authoring a workflow
// cannot be used to exceed the author's own authority, and that revoking a permission
// takes effect on the next run. This is the privilege-escalation test for the engine:
// the API middleware and the MCP registry enforce the coarse permission, but the
// engine calls the service directly and must enforce it itself.
func TestWorkflowPermissionIsRecheckedAtExecutionTime(t *testing.T) {
	f := newWorkflowFixture(t, "wf-perm", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[
		{"id":"t1","type":"create_task","args":{"title":"Requires tasks.create"}}
	]}`)
	wf := f.create(t, "Needs tasks.create", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	result, err := f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run with the permission held: %v", err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("run status = %q, want succeeded", result.Status)
	}

	// Revoke tasks.create from every role the author holds, as an administrator would.
	if _, err := f.db.Pool().Exec(ctx, `
DELETE FROM role_permissions rp
USING roles r, user_roles ur
WHERE rp.role_id = r.id AND ur.role_id = r.id AND ur.user_id = $1
  AND rp.permission_key = 'tasks.create'`, f.userID); err != nil {
		t.Fatalf("revoke tasks.create: %v", err)
	}

	before := countTasks(t, f, ctx)

	result, err = f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run after revocation returned a transport error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("run status after revocation = %q, want failed", result.Status)
	}

	if after := countTasks(t, f, ctx); after != before {
		t.Fatalf("tasks after revocation = %d, want %d: a revoked permission did not stop the action", after, before)
	}
}

// TestWorkflowCannotNotifyAcrossOrganizations proves the recipient check: a workflow
// may only address a member of its own organization. Without this, an author could
// write into another tenant's notification feed by guessing a user id.
func TestWorkflowCannotNotifyAcrossOrganizations(t *testing.T) {
	f := newWorkflowFixture(t, "wf-notify", "sales_manager")
	ctx := context.Background()

	otherOrg := insertOrganization(t, f.db, "wf-notify-other")
	outsider := seedMember(t, f.db, otherOrg, "sales_representative")

	def := json.RawMessage(`{"steps":[
		{"id":"n1","type":"notify","args":{"user_id":"` + outsider.String() + `","title":"Cross-tenant"}}
	]}`)
	wf := f.create(t, "Cross-tenant notify", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	result, err := f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run returned a transport error: %v", err)
	}
	if result.Status != "failed" {
		t.Fatalf("run status = %q, want failed: a cross-organization notification was accepted", result.Status)
	}

	var leaked int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE organization_id = $1`, otherOrg).Scan(&leaked); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("notifications written into the other organization = %d, want 0", leaked)
	}
}

// TestWorkflowDelayPausesAndResumes proves that a delay does not block a worker: the
// run is written as paused with a resume instant, and the scheduler resumes it once
// that instant has passed.
func TestWorkflowDelayPausesAndResumes(t *testing.T) {
	f := newWorkflowFixture(t, "wf-delay", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[
		{"id":"d1","type":"delay","args":{"minutes":30}},
		{"id":"t1","type":"create_task","args":{"title":"After the delay"}}
	]}`)
	wf := f.create(t, "Delayed follow-up", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	result, err := f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != "paused" {
		t.Fatalf("run status = %q, want paused", result.Status)
	}
	if n := countTasks(t, f, ctx); n != 0 {
		t.Fatalf("tasks created before the delay elapsed = %d, want 0", n)
	}

	due, err := f.engine.ResumeDue(ctx, 10)
	if err != nil {
		t.Fatalf("resume sweep: %v", err)
	}
	if due != 0 {
		t.Fatalf("runs resumed before their delay elapsed = %d, want 0", due)
	}

	// Bring the resume instant forward, as the passage of time would.
	if _, err := f.db.Pool().Exec(ctx,
		`UPDATE workflow_runs SET paused_until = now() - interval '1 second' WHERE id = $1`,
		result.RunID); err != nil {
		t.Fatalf("advance the pause: %v", err)
	}

	due, err = f.engine.ResumeDue(ctx, 10)
	if err != nil {
		t.Fatalf("second resume sweep: %v", err)
	}
	if due != 1 {
		t.Fatalf("runs resumed = %d, want 1", due)
	}
	if n := countTasks(t, f, ctx); n != 1 {
		t.Fatalf("tasks created after the resume = %d, want 1", n)
	}

	var status string
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT status FROM workflow_runs WHERE id = $1`, result.RunID).Scan(&status); err != nil {
		t.Fatalf("read the run status: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("run status after resuming = %q, want succeeded", status)
	}
}

// TestWorkflowDepthLimitStopsLoops proves the loop-breaker. An event caused by a run
// carries that run's correlation id; the chain depth is the number of runs already
// recorded for it. A run that would exceed the workflow's max_depth is recorded as
// skipped rather than executed, which is what stops two workflows that watch each
// other from running forever.
func TestWorkflowDepthLimitStopsLoops(t *testing.T) {
	f := newWorkflowFixture(t, "wf-loop", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[
		{"id":"t1","type":"create_task","args":{"title":"Chained task"}}
	]}`)
	wf := f.create(t, "Chain watcher", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerEvent,
		EventType:   strPtrTest("deal.updated"),
		MaxDepth:    1,
	})

	// The first event starts a chain. Its correlation id is the event's own id.
	first := outbox.Event{
		ID:             uuid.New(),
		OrganizationID: &f.org,
		Type:           "deal.updated",
		Source:         "forgecrm",
		SchemaVersion:  1,
		Payload:        json.RawMessage(`{}`),
	}
	f.engine.HandleEvent(ctx, first)

	if n := countTasks(t, f, ctx); n != 1 {
		t.Fatalf("tasks created by the first event = %d, want 1", n)
	}

	// A second event, caused by the first run, carries the same correlation id. Its
	// chain depth is 1, which reaches the limit of 1, so it must be skipped.
	second := first
	second.ID = uuid.New()
	second.CorrelationID = first.ID.String()

	run, err := f.engine.RunEventWorkflow(ctx, *wf, second)
	if err != nil {
		t.Fatalf("run the chained event: %v", err)
	}
	if run.Status != "skipped" {
		t.Fatalf("chained run status = %q, want skipped", run.Status)
	}
	if n := countTasks(t, f, ctx); n != 1 {
		t.Fatalf("tasks after the chained event = %d, want 1: the depth limit did not stop the chain", n)
	}
}

func countTasks(t *testing.T, f *workflowFixture, ctx context.Context) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id = $1`, f.org).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

func strPtrTest(s string) *string { return &s }
