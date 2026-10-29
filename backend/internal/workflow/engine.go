package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Engine executes workflow runs. It depends on the crm.Service for every action,
// so it can never do more than the workflow's author could do through the API.
type Engine struct {
	db     *database.DB
	store  *Store
	crm    *crm.Service
	rbac   *rbac.Service
	audit  *audit.Logger
	outbox *outbox.Store
	log    *slog.Logger

	// clock is overridable in tests so a delay can be resumed without sleeping.
	now func() time.Time
}

// Config configures the engine.
type Config struct {
	DB     *database.DB
	CRM    *crm.Service
	RBAC   *rbac.Service
	Audit  *audit.Logger
	Outbox *outbox.Store
	Log    *slog.Logger
}

// NewEngine builds an engine.
func NewEngine(cfg Config) *Engine {
	// The template catalog is validated against the event vocabulary the engine can
	// actually be triggered by. Registering it here means every process that builds
	// an engine — the API and the worker — has it, without either importing the
	// outbox package into this one (which would be a cycle).
	RegisterKnownEvents(outbox.EventTypes())
	return &Engine{
		db:     cfg.DB,
		store:  NewStore(cfg.DB),
		crm:    cfg.CRM,
		rbac:   cfg.RBAC,
		audit:  cfg.Audit,
		outbox: cfg.Outbox,
		log:    cfg.Log,
		now:    time.Now,
	}
}

// Store exposes the store for the API layer.
func (e *Engine) Store() *Store { return e.store }

// RunResult summarises a completed or paused run for the caller.
type RunResult struct {
	RunID   uuid.UUID `json:"run_id"`
	Status  string    `json:"status"`
	Summary string    `json:"summary"`
}

// principalFor rebuilds the author's principal from the database at execution time.
//
// This is deliberate: a workflow acts as the user who created it, and that user's
// permissions are resolved fresh on every run. If the author is blocked, removed
// from the organization, or has a role revoked, the workflow loses that authority
// immediately rather than carrying a stale permission set captured at authoring
// time. A workflow with no valid author fails rather than running with no authority.
func (e *Engine) principalFor(ctx context.Context, orgID, userID uuid.UUID) (*rbac.Principal, error) {
	const q = `
		SELECT u.email, u.display_name, u.status, u.is_platform_owner
		FROM users u
		JOIN memberships m ON m.user_id = u.id AND m.organization_id = $2
		WHERE u.id = $1`
	var email, name, status string
	var isOwner bool
	if err := e.db.Q(ctx).QueryRow(ctx, q, userID, orgID).Scan(&email, &name, &status, &isOwner); err != nil {
		return nil, fmt.Errorf("the workflow author is not a member of this organization")
	}
	if status != "active" {
		return nil, fmt.Errorf("the workflow author's account is not active")
	}
	perms, err := e.rbac.EffectivePermissions(ctx, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("resolve the workflow author's permissions: %w", err)
	}
	p := rbac.NewPrincipal(userID, email, name, orgID, perms)
	p.IsPlatformOwner = isOwner
	return p, nil
}

// HandleEvent matches a committed event against the organization's event-triggered
// workflows and runs each match. It never returns an error to its caller: a
// workflow failure is recorded on the run, not propagated, so one broken workflow
// cannot stop the outbox drain or the other workflows watching the same event.
func (e *Engine) HandleEvent(ctx context.Context, ev outbox.Event) {
	if ev.OrganizationID == nil {
		return
	}
	matches, err := e.store.MatchEventWorkflows(ctx, *ev.OrganizationID, ev.Type)
	if err != nil {
		e.warn("could not match workflows for an event", "event_id", ev.ID, "error", err)
		return
	}
	for _, w := range matches {
		if _, err := e.RunEventWorkflow(ctx, w, ev); err != nil {
			e.warn("workflow run failed to start", "workflow_id", w.ID, "event_id", ev.ID, "error", err)
		}
	}
}

// RunScheduled runs a schedule-triggered workflow. It starts a fresh correlation
// chain, because a scheduled run is not a continuation of whatever chain last ran.
func (e *Engine) RunScheduled(ctx context.Context, w Workflow) (*RunResult, error) {
	corr := uuid.NewString()
	ctx = logging.WithCorrelationID(ctx, corr)
	run, err := e.store.CreateRun(ctx, &w, TriggerSchedule, nil, 0, corr)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"event_type": "schedule", "source": "forgecrm"}
	if w.ScheduleCron != nil {
		fields["schedule_cron"] = *w.ScheduleCron
	}
	return e.execute(ctx, &w, run, NewEvalContext(fields, nil), 0)
}

// ResumeDue resumes paused runs whose delay has elapsed. It returns how many runs
// it resumed. Each resume is claimed atomically, so two replicas never resume the
// same run.
func (e *Engine) ResumeDue(ctx context.Context, limit int) (int, error) {
	resumed := 0
	for i := 0; i < limit; i++ {
		run, w, err := e.store.ClaimRunForResume(ctx)
		if err != nil {
			return resumed, err
		}
		if run == nil {
			break
		}
		if _, err := e.ResumeRun(ctx, *w, *run); err != nil {
			e.warn("could not resume a paused workflow run", "run_id", run.ID, "error", err)
			continue
		}
		resumed++
	}
	return resumed, nil
}

// RunEventWorkflow runs one event-triggered workflow for one committed event.
//
// It is the entry point the worker calls after draining the outbox. De-duplication
// happens first: ClaimEventRun inserts the (workflow, event) row, and if it already
// exists this returns without any effect. Only after the claim does the run begin,
// so a replayed event cannot produce a second effect.
func (e *Engine) RunEventWorkflow(ctx context.Context, w Workflow, ev outbox.Event) (*RunResult, error) {
	// The chain this event belongs to. An event emitted by a workflow step carries
	// that run's correlation id; an event with none starts its own chain, keyed by
	// its own id so the first run of the chain counts as depth 0.
	corr := ev.CorrelationID
	if corr == "" {
		corr = ev.ID.String()
	}

	// The chain depth is counted before this run is inserted.
	depth, err := e.store.CountChainRuns(ctx, corr)
	if err != nil {
		return nil, err
	}

	// Claim the event and create the run in one transaction. A duplicate delivery
	// commits nothing at all, so the history stays one run per event.
	run, claimed, err := e.store.CreateEventRun(ctx, &w, ev.ID, depth, corr)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return &RunResult{Status: "skipped", Summary: "duplicate event delivery"}, nil
	}

	// A chain that has reached the workflow's depth limit is recorded as skipped
	// rather than executed. This is what stops two workflows that watch each other.
	if depth >= w.MaxDepth {
		if err := e.store.FinishRun(ctx, run.ID, "skipped",
			strPtr(fmt.Sprintf("chain depth %d reached the limit of %d", depth, w.MaxDepth)), nil, 0, 0, 0); err != nil {
			return nil, err
		}
		return &RunResult{RunID: run.ID, Status: "skipped", Summary: "chain depth limit reached"}, nil
	}

	env := NewEvalContext(envelopeFields(ev), ev.Payload)
	return e.execute(ctx, &w, run, env, 0)
}

// RunManual runs a workflow on demand. It records the trigger as manual and starts
// from a fresh correlation, so a manual run is a new chain rather than a
// continuation of whatever chain last ran.
func (e *Engine) RunManual(ctx context.Context, p *rbac.Principal, w Workflow) (*RunResult, error) {
	corr := logging.CorrelationID(ctx)
	if corr == "" {
		corr = uuid.NewString()
		ctx = logging.WithCorrelationID(ctx, corr)
	}
	run, err := e.store.CreateRun(ctx, &w, TriggerManual, nil, 0, corr)
	if err != nil {
		return nil, err
	}
	env := NewEvalContext(map[string]any{
		"event_type": "manual",
		"source":     "forgecrm",
	}, nil)
	return e.execute(ctx, &w, run, env, 0)
}

// ResumeRun continues a paused run from its stored next step index.
func (e *Engine) ResumeRun(ctx context.Context, w Workflow, run Run) (*RunResult, error) {
	def, err := ParseDefinition(w.Definition)
	if err != nil {
		return nil, err
	}
	env := NewEvalContext(map[string]any{"event_type": "resume", "source": "forgecrm"}, nil)
	return e.executeFrom(ctx, &w, def, run, env, run.NextStepIndex)
}

// execute runs a definition from step 0.
func (e *Engine) execute(ctx context.Context, w *Workflow, run *Run, env *EvalContext, start int) (*RunResult, error) {
	def, err := ParseDefinition(w.Definition)
	if err != nil {
		_ = e.store.FinishRun(ctx, run.ID, "failed", nil, strPtr("the stored definition is invalid"), 0, 0, 0)
		return nil, err
	}
	return e.executeFrom(ctx, w, def, *run, env, start)
}

// executeFrom is the step loop. It walks the top-level steps from `start`,
// recording each outcome. A delay pauses and returns; a failing step stops the run
// with status failed, leaving the remaining steps unrun and the history showing
// exactly where it stopped.
func (e *Engine) executeFrom(ctx context.Context, w *Workflow, def *Definition, run Run, env *EvalContext, start int) (*RunResult, error) {
	p, err := e.principalFor(ctx, w.OrganizationID, w.CreatedBy)
	if err != nil {
		_ = e.store.FinishRun(ctx, run.ID, "failed", nil, strPtr(err.Error()), run.StepsSucceeded, run.StepsFailed, start)
		return nil, err
	}
	// Bind the author's identity to the context so every outbox event the steps emit
	// carries the run's correlation id, which is what makes the chain countable.
	ctx = logging.WithCorrelationID(ctx, deref(run.CorrelationID))

	succeeded, failed := run.StepsSucceeded, run.StepsFailed
	steps := def.Steps
	// items holds the result of the most recent find_deals step, which a following
	// foreach iterates. It lives only for the run, never in a payload.
	var items []map[string]any

	for i := start; i < len(steps); i++ {
		step := &steps[i]

		if step.Type == StepFindDeals {
			found, detail, err := e.findDeals(ctx, p, step, env)
			if err != nil {
				return e.failStep(ctx, w, run, i, step, succeeded, failed, err)
			}
			items = found
			if err := e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "succeeded", detail, nil); err != nil {
				return nil, err
			}
			succeeded++
			continue
		}

		if step.Type == StepForeach {
			if len(items) == 0 {
				// Nothing selected. This is a normal outcome for a scheduled sweep, so
				// the step is recorded as skipped rather than failed and the run continues.
				if err := e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "skipped",
					map[string]any{"reason": "no items to repeat"}, nil); err != nil {
					return nil, err
				}
				continue
			}
			iterations := items
			truncated := false
			if len(iterations) > maxForeachItems {
				iterations = iterations[:maxForeachItems]
				truncated = true
			}
			actions := 0
			for _, item := range iterations {
				childEnv := itemContext(env, item)
				for j := range step.Do {
					if err := e.runAction(ctx, w, run, p, i, &step.Do[j], childEnv); err != nil {
						return e.failStep(ctx, w, run, i, &step.Do[j], succeeded, failed, err)
					}
					succeeded++
					actions++
				}
			}
			if err := e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "succeeded",
				map[string]any{"items": len(iterations), "actions": actions, "truncated": truncated}, nil); err != nil {
				return nil, err
			}
			continue
		}

		if step.Type == StepDelay {
			d, err := delayFromArgs(step.Args)
			if err != nil {
				return e.failStep(ctx, w, run, i, step, succeeded, failed, err)
			}
			resumeAt := e.now().Add(d)
			if err := e.store.PauseRun(ctx, run.ID, i+1, resumeAt); err != nil {
				return nil, err
			}
			_ = e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "succeeded",
				map[string]any{"resume_at": resumeAt, "delay_seconds": int(d.Seconds())}, nil)
			return &RunResult{RunID: run.ID, Status: "paused",
				Summary: fmt.Sprintf("paused until %s", resumeAt.UTC().Format(time.RFC3339))}, nil
		}

		if step.Type == StepCondition {
			if !step.Condition.Evaluate(env) {
				_ = e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "skipped",
					map[string]any{"reason": "condition not met"}, nil)
				summary := fmt.Sprintf("stopped at condition %q", step.ID)
				if err := e.store.FinishRun(ctx, run.ID, "skipped", &summary, nil, succeeded, failed, i+1); err != nil {
					return nil, err
				}
				return &RunResult{RunID: run.ID, Status: "skipped", Summary: summary}, nil
			}
			_ = e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "succeeded",
				map[string]any{"reason": "condition met"}, nil)
			continue
		}

		if step.Type == StepBranch {
			path := step.Then
			if !step.Condition.Evaluate(env) {
				path = step.Else
			}
			_ = e.store.RecordStep(ctx, run.ID, w.OrganizationID, i, step.ID, step.Type, "succeeded",
				map[string]any{"branch": branchName(step, env), "steps": len(path)}, nil)
			for j := range path {
				if err := e.runAction(ctx, w, run, p, i, &path[j], env); err != nil {
					return e.failStep(ctx, w, run, i, &path[j], succeeded, failed, err)
				}
				succeeded++
			}
			continue
		}

		if err := e.runAction(ctx, w, run, p, i, step, env); err != nil {
			return e.failStep(ctx, w, run, i, step, succeeded, failed, err)
		}
		succeeded++
	}

	summary := fmt.Sprintf("%d step(s) succeeded", succeeded)
	if err := e.store.FinishRun(ctx, run.ID, "succeeded", &summary, nil, succeeded, failed, len(steps)); err != nil {
		return nil, err
	}
	e.auditRun(ctx, w, run.ID, "workflow.run.succeeded", summary)
	return &RunResult{RunID: run.ID, Status: "succeeded", Summary: summary}, nil
}

func branchName(step *Step, env *EvalContext) string {
	if step.Condition.Evaluate(env) {
		return "then"
	}
	return "else"
}

// failStep records a failed step and terminates the run.
func (e *Engine) failStep(ctx context.Context, w *Workflow, run Run, index int, step *Step, succeeded, failed int, cause error) (*RunResult, error) {
	msg := cause.Error()
	_ = e.store.RecordStep(ctx, run.ID, w.OrganizationID, index, step.ID, step.Type, "failed", map[string]any{}, &msg)
	failed++
	summary := fmt.Sprintf("step %q failed", step.ID)
	if err := e.store.FinishRun(ctx, run.ID, "failed", &summary, &msg, succeeded, failed, index+1); err != nil {
		return nil, err
	}
	e.auditRun(ctx, w, run.ID, "workflow.run.failed", msg)
	// A step failure is reported as a run outcome, not as a Go error: the run was
	// recorded and the failure is visible in the history, so the caller (the worker)
	// must not treat it as a job failure and retry the whole thing.
	return &RunResult{RunID: run.ID, Status: "failed", Summary: msg}, nil
}

func (e *Engine) auditRun(ctx context.Context, w *Workflow, runID uuid.UUID, action, detail string) {
	if e.audit == nil {
		return
	}
	_ = e.audit.Record(ctx, audit.Entry{
		OrganizationID: &w.OrganizationID,
		ActorUserID:    &w.CreatedBy,
		ActorType:      "system",
		Action:         action,
		EntityType:     "workflow_run",
		EntityID:       runID.String(),
		Metadata:       map[string]any{"workflow_id": w.ID.String(), "detail": detail},
	})
}

// requiredPermission maps a step type to the permission its author must hold.
//
// This check is the engine's own, and it is not redundant. The crm.Service enforces
// tenant scoping and record ownership, but the coarse "may you do this at all"
// permission is enforced by the HTTP middleware on the API path and by the tool
// registry on the MCP path. The engine calls the service directly, so without this
// table a user who could author a workflow could use one to perform an action their
// own role forbids — for example, someone with workflows.manage but not tasks.create
// creating tasks. The check is made against the author's permissions, resolved fresh
// at execution time, so a revoked permission takes effect on the next run.
var requiredPermission = map[string]rbac.Permission{
	StepCreateTask:   rbac.PermTasksCreate,
	StepCreateTicket: rbac.PermTicketsCreate,
	StepUpdateDeal:   rbac.PermDealsChangeStage,
	// A notification is delivered only to a named user in the same organization, and
	// what that user can then act on is governed by their own permissions. The bar is
	// therefore "this author participates in notifications", not a write right.
	StepNotify: rbac.PermNotificationsRead,
	// find_deals reads deals, so it needs the read permission. It is not an action
	// step, but the query it runs is scoped by the author's principal all the same, and
	// requiring the permission up front makes a workflow that cannot read deals fail
	// with a clear message rather than running an empty sweep forever.
	StepFindDeals: rbac.PermDealsRead,
}

// findDeals selects the deals a find_deals step names, as the workflow's author.
//
// It calls crm.ListDeals, so the query is scoped to the author's organization and
// to the records their role can see; a workflow cannot use a find step to read a
// deal its author could not open in the UI. The result is bounded and the bound is
// reported, so a definition that would select an unbounded set is truncated
// visibly rather than costing an unbounded amount of work.
func (e *Engine) findDeals(ctx context.Context, p *rbac.Principal, step *Step, env *EvalContext) ([]map[string]any, map[string]any, error) {
	if !p.IsPlatformOwner && !p.Has(rbac.PermDealsRead) {
		return nil, nil, fmt.Errorf("the workflow author does not hold %s, which step %q requires", rbac.PermDealsRead, step.ID)
	}
	limit := maxFindResults
	if n, ok := numberArg(step.Args, "limit"); ok && n > 0 {
		limit = int(n)
		if limit > maxFindResults {
			limit = maxFindResults
		}
	}

	q := crm.ListQuery{
		Pagination: httpx.Pagination{Limit: limit},
		Sort:       httpx.SortSpec{Column: "expected_close_date"},
	}
	if v, ok := argString(step.Args, "status", env); ok && v != "" {
		q.Status = v
	}
	if v, ok := argString(step.Args, "search", env); ok && v != "" {
		q.Search = v
	}
	if id, err := argUUID(step.Args, "owner_id", env); err != nil {
		return nil, nil, err
	} else if id != nil {
		q.OwnerID = id
	}
	if id, err := argUUID(step.Args, "account_id", env); err != nil {
		return nil, nil, err
	} else if id != nil {
		q.AccountID = id
	}
	if id, err := argUUID(step.Args, "pipeline_id", env); err != nil {
		return nil, nil, err
	} else if id != nil {
		q.PipelineID = id
	}
	// A close date more than this many days in the past means the deal is overdue.
	if days, ok := numberArg(step.Args, "close_date_within_days"); ok && days >= 0 {
		cutoff := e.now().AddDate(0, 0, int(days))
		q.ExpectedCloseBefore = &cutoff
	}
	// No activity for this many days is the "nothing is happening" test. It uses the
	// same predicate as the deal's own list view, so the workflow and the UI agree on
	// what "stale" means.
	if days, ok := numberArg(step.Args, "stale_days"); ok && days > 0 {
		cutoff := e.now().AddDate(0, 0, -int(days))
		q.NoActivitySince = &cutoff
	}

	deals, total, err := e.crm.ListDeals(ctx, p, q)
	if err != nil {
		return nil, nil, err
	}
	items := make([]map[string]any, 0, len(deals))
	for _, d := range deals {
		item := map[string]any{
			"id":     d.ID.String(),
			"title":  d.Title,
			"amount": d.Amount,
			"status": d.Status,
		}
		if d.AccountID != nil {
			item["account_id"] = d.AccountID.String()
		}
		if d.OwnerUserID != nil {
			item["owner_user_id"] = d.OwnerUserID.String()
		}
		if d.ExpectedCloseDate != nil {
			item["expected_close_date"] = *d.ExpectedCloseDate
		}
		items = append(items, item)
	}
	detail := map[string]any{
		"entity": FindEntityDeals,
		"count":  len(items),
		"total":  total,
	}
	if total > int64(len(items)) {
		detail["truncated"] = true
	}
	return items, detail, nil
}

// itemContext returns a context for one foreach iteration. The item's fields are
// exposed under "item." so a nested step reads them as {{item.title}} and cannot
// accidentally read the trigger event's fields under the same bare names.
func itemContext(parent *EvalContext, item map[string]any) *EvalContext {
	fields := map[string]any{}
	if parent != nil {
		for k, v := range parent.Fields {
			fields[k] = v
		}
	}
	for k, v := range item {
		fields["item."+k] = v
	}
	return &EvalContext{Fields: fields}
}

// runAction executes one non-control step.
func (e *Engine) runAction(ctx context.Context, w *Workflow, run Run, p *rbac.Principal, index int, step *Step, env *EvalContext) error {
	if required, ok := requiredPermission[step.Type]; ok {
		if !p.IsPlatformOwner && !p.Has(required) {
			return fmt.Errorf("the workflow author does not hold %s, which step %q requires", required, step.ID)
		}
	}
	var detail map[string]any
	var err error
	switch step.Type {
	case StepCreateTask:
		detail, err = e.doCreateTask(ctx, p, step, env)
	case StepCreateTicket:
		detail, err = e.doCreateTicket(ctx, p, step, env)
	case StepNotify:
		detail, err = e.doNotify(ctx, p, step, env)
	case StepUpdateDeal:
		detail, err = e.doUpdateDeal(ctx, p, step, env)
	default:
		err = fmt.Errorf("%w: %q", ErrUnknownStepType, step.Type)
	}
	if err != nil {
		return err
	}
	return e.store.RecordStep(ctx, run.ID, w.OrganizationID, index, step.ID, step.Type, "succeeded", detail, nil)
}

// argString reads a string argument, interpolating {{path}} references against the
// evaluation context so a step can use data from the event that triggered it.
func argString(args map[string]any, key string, env *EvalContext) (string, bool) {
	raw, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	return interpolate(s, env), true
}

// interpolate replaces {{path}} with the value at path in the context. An unknown
// path is left as an empty string, so a template that references a field the event
// does not carry produces an empty value rather than the literal placeholder.
func interpolate(s string, env *EvalContext) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	for {
		start := strings.Index(s, "{{")
		if start < 0 {
			b.WriteString(s)
			break
		}
		end := strings.Index(s[start:], "}}")
		if end < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		path := strings.TrimSpace(s[start+2 : start+end])
		if v, ok := env.Fields[path]; ok && v != nil {
			b.WriteString(stringify(v))
		}
		s = s[start+end+2:]
	}
	return b.String()
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// Render whole numbers without a trailing .0, which is what an operator
		// expects when they interpolate an amount or a count into a title.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// argUUID reads a UUID argument that may be a literal id or a {{path}} reference to
// an id in the event payload.
func argUUID(args map[string]any, key string, env *EvalContext) (*uuid.UUID, error) {
	s, ok := argString(args, key, env)
	if !ok || s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("argument %q is not a valid id: %q", key, s)
	}
	return &id, nil
}

func envelopeFields(ev outbox.Event) map[string]any {
	fields := map[string]any{
		"event_id":       ev.ID.String(),
		"event_type":     ev.Type,
		"source":         ev.Source,
		"schema_version": float64(ev.SchemaVersion),
		"occurred_at":    ev.OccurredAt.UTC().Format(time.RFC3339),
	}
	if ev.OrganizationID != nil {
		fields["tenant_id"] = ev.OrganizationID.String()
	}
	if ev.CorrelationID != "" {
		fields["correlation_id"] = ev.CorrelationID
	}
	return fields
}

func strPtr(s string) *string { return &s }

func (e *Engine) warn(msg string, args ...any) {
	if e.log != nil {
		e.log.Warn(msg, args...)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
