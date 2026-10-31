package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// ErrNotFound is returned when a workflow or run does not exist, or is not visible
// to the caller's organization.
var ErrNotFound = errors.New("workflow not found")

// Workflow is a stored workflow definition together with its denormalised trigger
// fields.
type Workflow struct {
	ID                      uuid.UUID       `json:"id"`
	OrganizationID          uuid.UUID       `json:"organization_id"`
	Name                    string          `json:"name"`
	Description             *string         `json:"description,omitempty"`
	Definition              json.RawMessage `json:"definition"`
	DefinitionSchemaVersion int             `json:"definition_schema_version"`
	Enabled                 bool            `json:"enabled"`
	TriggerType             string          `json:"trigger_type"`
	EventType               *string         `json:"event_type,omitempty"`
	ScheduleCron            *string         `json:"schedule_cron,omitempty"`
	ScheduleTimezone        *string         `json:"schedule_timezone,omitempty"`
	NextRunAt               *time.Time      `json:"next_run_at,omitempty"`
	CreatedBy               uuid.UUID       `json:"created_by"`
	MaxDepth                int             `json:"max_depth"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
	ArchivedAt              *time.Time      `json:"archived_at,omitempty"`
}

// Run is one execution of a workflow.
type Run struct {
	ID             uuid.UUID  `json:"id"`
	WorkflowID     uuid.UUID  `json:"workflow_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	TriggerEventID *uuid.UUID `json:"trigger_event_id,omitempty"`
	TriggerType    string     `json:"trigger_type"`
	Status         string     `json:"status"`
	RunDepth       int        `json:"run_depth"`
	CorrelationID  *string    `json:"correlation_id,omitempty"`
	Summary        *string    `json:"summary,omitempty"`
	Error          *string    `json:"error,omitempty"`
	StepsTotal     int        `json:"steps_total"`
	StepsSucceeded int        `json:"steps_succeeded"`
	StepsFailed    int        `json:"steps_failed"`
	NextStepIndex  int        `json:"next_step_index"`
	PausedUntil    *time.Time `json:"paused_until,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// StepRun is the recorded outcome of a single step.
type StepRun struct {
	ID             uuid.UUID       `json:"id"`
	RunID          uuid.UUID       `json:"run_id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	StepIndex      int             `json:"step_index"`
	StepID         string          `json:"step_id"`
	StepType       string          `json:"step_type"`
	Status         string          `json:"status"`
	Detail         json.RawMessage `json:"detail"`
	Error          *string         `json:"error,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
}

// Store is the persistence layer for workflows. Every method that touches
// tenant-owned rows takes the caller's organization and filters on it, so a
// guessed id from another tenant returns not-found rather than data.
type Store struct {
	db *database.DB
	// defaults is the organization's own configuration, read for the timezone a
	// schedule runs in when the workflow does not name one. It is a narrow local
	// interface, so the store holds no dependency on the configuration service.
	defaults OrgDefaults
}

// OrgDefaults answers the organization's effective configuration. It is the one
// question this package asks: the default timezone for a schedule.
type OrgDefaults interface {
	EffectiveString(ctx context.Context, orgID uuid.UUID, key string) string
}

// SetOrgDefaults attaches the organization configuration. A nil port leaves the
// model default (UTC) in force, so existing callers and the extraction harness are
// unaffected.
func (s *Store) SetOrgDefaults(d OrgDefaults) { s.defaults = d }

// SettingDefaultTimezone is the key read for a schedule that names no timezone. It
// is spelled here so this package imports no configuration code; the spelling is
// pinned to the catalog by a test.
const SettingDefaultTimezone = "workflow.default_timezone"

// NewStore builds a workflow store.
func NewStore(db *database.DB) *Store { return &Store{db: db} }

const workflowColumns = `
	id, organization_id, name, description, definition, definition_schema_version,
	enabled, trigger_type, event_type, schedule_cron, schedule_timezone, next_run_at,
	created_by, max_depth, created_at, updated_at, archived_at`

func scanWorkflow(row interface{ Scan(...any) error }) (*Workflow, error) {
	var w Workflow
	err := row.Scan(&w.ID, &w.OrganizationID, &w.Name, &w.Description, &w.Definition,
		&w.DefinitionSchemaVersion, &w.Enabled, &w.TriggerType, &w.EventType, &w.ScheduleCron,
		&w.ScheduleTimezone, &w.NextRunAt, &w.CreatedBy, &w.MaxDepth, &w.CreatedAt, &w.UpdatedAt,
		&w.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// CreateInput is the request to create or replace a workflow.
type CreateInput struct {
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	Definition       json.RawMessage `json:"definition"`
	TriggerType      string          `json:"trigger_type"`
	EventType        *string         `json:"event_type,omitempty"`
	ScheduleCron     *string         `json:"schedule_cron,omitempty"`
	ScheduleTimezone *string         `json:"schedule_timezone,omitempty"`
	MaxDepth         int             `json:"max_depth,omitempty"`
}

// Create validates and inserts a workflow.
//
// The validation here is the authoritative one for the HTTP path: the definition
// must parse and the trigger must be coherent (an event trigger names an event
// type, a schedule trigger has a parseable cron). The denormalised columns are
// written from the same parsed values, so they cannot drift from the document.
func (s *Store) Create(ctx context.Context, p *rbac.Principal, in CreateInput) (*Workflow, error) {
	if !p.IsPlatformOwner && !p.Has(rbac.PermWorkflowsManage) {
		return nil, rbac.ErrForbidden
	}
	if _, err := ParseDefinition(in.Definition); err != nil {
		return nil, httpx.ValidationError(map[string]string{"definition": err.Error()})
	}
	in.ScheduleTimezone = s.resolveTimezone(ctx, p.OrganizationID, in.ScheduleTimezone)
	next, err := s.validateTrigger(in)
	if err != nil {
		return nil, err
	}
	if in.MaxDepth == 0 {
		in.MaxDepth = 5
	}
	const q = `
		INSERT INTO workflows (organization_id, name, description, definition,
			definition_schema_version, trigger_type, event_type, schedule_cron,
			schedule_timezone, next_run_at, created_by, max_depth)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING` + workflowColumns
	row := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.Name, in.Description, in.Definition,
		DefinitionSchemaVersion, in.TriggerType, in.EventType, in.ScheduleCron, in.ScheduleTimezone,
		next, p.UserID, in.MaxDepth)
	w, err := scanWorkflow(row)
	if err != nil {
		return nil, fmt.Errorf("create workflow: %w", err)
	}
	return w, nil
}

// validateTrigger checks the trigger fields and returns the initial next_run_at for
// a schedule trigger.
func (s *Store) validateTrigger(in CreateInput) (*time.Time, error) {
	switch in.TriggerType {
	case TriggerEvent:
		if in.EventType == nil || *in.EventType == "" {
			return nil, httpx.ValidationError(map[string]string{"event_type": "an event trigger needs an event type"})
		}
		if in.ScheduleCron != nil {
			return nil, httpx.ValidationError(map[string]string{"schedule_cron": "not allowed on an event trigger"})
		}
		return nil, nil
	case TriggerSchedule:
		if in.ScheduleCron == nil || *in.ScheduleCron == "" {
			return nil, httpx.ValidationError(map[string]string{"schedule_cron": "a schedule trigger needs a cron expression"})
		}
		spec, err := ParseCron(*in.ScheduleCron)
		if err != nil {
			return nil, httpx.ValidationError(map[string]string{"schedule_cron": err.Error()})
		}
		loc, err := scheduleLocation(in.ScheduleTimezone)
		if err != nil {
			return nil, httpx.ValidationError(map[string]string{"schedule_timezone": err.Error()})
		}
		next, ok := spec.Next(time.Now(), loc)
		if !ok {
			return nil, httpx.ValidationError(map[string]string{"schedule_cron": "no run time found in the next five years"})
		}
		return &next, nil
	case TriggerManual:
		return nil, nil
	default:
		return nil, httpx.ValidationError(map[string]string{"trigger_type": "must be event, schedule or manual"})
	}
}

func scheduleLocation(tz *string) (*time.Location, error) {
	if tz == nil || *tz == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q", *tz)
	}
	return loc, nil
}

// resolveTimezone fills in the organization's default timezone for a schedule that
// named none, so the stored row carries the zone the run was actually scheduled in.
// A workflow with its own schedule_timezone keeps it, and a nil port leaves the
// model default in force.
func (s *Store) resolveTimezone(ctx context.Context, orgID uuid.UUID, tz *string) *string {
	if tz != nil && *tz != "" {
		return tz
	}
	if s.defaults != nil {
		if v := s.defaults.EffectiveString(ctx, orgID, SettingDefaultTimezone); v != "" {
			return &v
		}
	}
	return tz
}

// Get returns one workflow scoped to the caller's organization.
func (s *Store) Get(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Workflow, error) {
	const q = `SELECT` + workflowColumns + ` FROM workflows WHERE id = $1 AND organization_id = $2`
	w, err := scanWorkflow(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	return w, nil
}

// List returns the organization's workflows, newest first.
func (s *Store) List(ctx context.Context, p *rbac.Principal, includeArchived bool, limit, offset int) ([]Workflow, int64, error) {
	where := `organization_id = $1`
	if !includeArchived {
		where += ` AND archived_at IS NULL`
	}
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM workflows WHERE `+where, p.OrganizationID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count workflows: %w", err)
	}
	q := `SELECT` + workflowColumns + ` FROM workflows WHERE ` + where +
		` ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *w)
	}
	return out, total, rows.Err()
}

// Update replaces the editable fields of a workflow.
func (s *Store) Update(ctx context.Context, p *rbac.Principal, id uuid.UUID, in CreateInput) (*Workflow, error) {
	if !p.IsPlatformOwner && !p.Has(rbac.PermWorkflowsManage) {
		return nil, rbac.ErrForbidden
	}
	if _, err := ParseDefinition(in.Definition); err != nil {
		return nil, httpx.ValidationError(map[string]string{"definition": err.Error()})
	}
	in.ScheduleTimezone = s.resolveTimezone(ctx, p.OrganizationID, in.ScheduleTimezone)
	next, err := s.validateTrigger(in)
	if err != nil {
		return nil, err
	}
	if in.MaxDepth == 0 {
		in.MaxDepth = 5
	}
	const q = `
		UPDATE workflows SET name=$3, description=$4, definition=$5, trigger_type=$6,
			event_type=$7, schedule_cron=$8, schedule_timezone=$9, next_run_at=$10, max_depth=$11
		WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL
		RETURNING` + workflowColumns
	w, err := scanWorkflow(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.Name, in.Description,
		in.Definition, in.TriggerType, in.EventType, in.ScheduleCron, in.ScheduleTimezone, next, in.MaxDepth))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update workflow: %w", err)
	}
	return w, nil
}

// SetEnabled turns a workflow on or off. Disabling clears next_run_at so a
// disabled schedule does not accumulate due times.
func (s *Store) SetEnabled(ctx context.Context, p *rbac.Principal, id uuid.UUID, enabled bool) (*Workflow, error) {
	if !p.IsPlatformOwner && !p.Has(rbac.PermWorkflowsManage) {
		return nil, rbac.ErrForbidden
	}
	const q = `
		UPDATE workflows SET enabled = $3,
			next_run_at = CASE WHEN $3 THEN next_run_at ELSE NULL END
		WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL
		RETURNING` + workflowColumns
	w, err := scanWorkflow(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("set workflow enabled: %w", err)
	}
	return w, nil
}

// Archive soft-deletes a workflow. Runs and their history are kept, because an
// audit trail that disappears when its subject is archived is not an audit trail.
func (s *Store) Archive(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	if !p.IsPlatformOwner && !p.Has(rbac.PermWorkflowsManage) {
		return rbac.ErrForbidden
	}
	const q = `UPDATE workflows SET archived_at = now(), enabled = false, next_run_at = NULL
		WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL`
	tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
	if err != nil {
		return fmt.Errorf("archive workflow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

// CreateRun inserts a run row and returns it. It is the durable record that a run
// began, written before any step has an effect.
func (s *Store) CreateRun(ctx context.Context, w *Workflow, triggerType string, triggerEventID *uuid.UUID, depth int, correlationID string) (*Run, error) {
	const q = `
		INSERT INTO workflow_runs (workflow_id, organization_id, trigger_event_id, trigger_type,
			run_depth, correlation_id, steps_total)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7)
		RETURNING id, workflow_id, organization_id, trigger_event_id, trigger_type, status,
			run_depth, correlation_id, summary, error, steps_total,
			steps_succeeded, steps_failed, next_step_index, paused_until, started_at, finished_at`
	row := s.db.Q(ctx).QueryRow(ctx, q, w.ID, w.OrganizationID, triggerEventID, triggerType, depth,
		correlationID, len(mustDefinition(w).Steps))
	return scanRun(row)
}

func mustDefinition(w *Workflow) *Definition {
	def, err := ParseDefinition(w.Definition)
	if err != nil {
		// A stored definition was validated on write; a parse failure here means the
		// row was altered outside the application. Treat it as an empty definition so
		// the run is recorded and fails visibly rather than panicking.
		return &Definition{}
	}
	return def
}

func scanRun(row interface{ Scan(...any) error }) (*Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.WorkflowID, &r.OrganizationID, &r.TriggerEventID, &r.TriggerType,
		&r.Status, &r.RunDepth, &r.CorrelationID, &r.Summary, &r.Error,
		&r.StepsTotal, &r.StepsSucceeded, &r.StepsFailed, &r.NextStepIndex, &r.PausedUntil,
		&r.StartedAt, &r.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

const runColumns = `
	id, workflow_id, organization_id, trigger_event_id, trigger_type, status, run_depth,
	correlation_id, summary, error, steps_total, steps_succeeded,
	steps_failed, next_step_index, paused_until, started_at, finished_at`

// CountChainRuns returns how many runs already exist for a correlation chain.
//
// This is the depth of the chain that produced an event: a step's emitted event
// inherits the run's correlation id, so a run started by that event sees the
// runs that came before it. The count is taken before the new run is inserted, so
// the first run of a chain sees 0.
func (s *Store) CountChainRuns(ctx context.Context, correlationID string) (int, error) {
	if correlationID == "" {
		return 0, nil
	}
	var n int
	if err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM workflow_runs WHERE correlation_id = $1`, correlationID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count chain runs: %w", err)
	}
	return n, nil
}

// FinishRun records the terminal state of a run.
func (s *Store) FinishRun(ctx context.Context, runID uuid.UUID, status string, summary *string, runErr *string, succeeded, failed int, nextIndex int) error {
	const q = `
		UPDATE workflow_runs SET status=$2, summary=$3, error=$4, steps_succeeded=$5,
			steps_failed=$6, next_step_index=$7, finished_at=now()
		WHERE id=$1`
	if _, err := s.db.Q(ctx).Exec(ctx, q, runID, status, summary, runErr, succeeded, failed, nextIndex); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

// PauseRun records that a delay step paused the run at nextIndex until resumeAt.
func (s *Store) PauseRun(ctx context.Context, runID uuid.UUID, nextIndex int, resumeAt time.Time) error {
	const q = `UPDATE workflow_runs SET status='paused', next_step_index=$2, paused_until=$3 WHERE id=$1`
	if _, err := s.db.Q(ctx).Exec(ctx, q, runID, nextIndex, resumeAt); err != nil {
		return fmt.Errorf("pause run: %w", err)
	}
	return nil
}

// ResumeRun clears the pause and marks the run running again, claiming it so only
// one resumer proceeds. It returns ok=false when another replica already took it.
func (s *Store) ResumeRun(ctx context.Context, runID uuid.UUID) (bool, error) {
	const q = `UPDATE workflow_runs SET status='running', paused_until=NULL
		WHERE id=$1 AND status='paused'`
	tag, err := s.db.Q(ctx).Exec(ctx, q, runID)
	if err != nil {
		return false, fmt.Errorf("resume run: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ClaimRunForResume atomically claims a paused run whose delay has elapsed,
// returning the workflow so it can continue. It uses FOR UPDATE SKIP LOCKED so two
// replicas never resume the same run.
func (s *Store) ClaimRunForResume(ctx context.Context) (*Run, *Workflow, error) {
	var run *Run
	var wf *Workflow
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const claim = `
			SELECT` + runColumns + ` FROM workflow_runs
			WHERE status='paused' AND paused_until <= now()
			ORDER BY paused_until
			LIMIT 1
			FOR UPDATE SKIP LOCKED`
		r, err := scanRun(s.db.Q(ctx).QueryRow(ctx, claim))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim paused run: %w", err)
		}
		const update = `UPDATE workflow_runs SET status='running', paused_until=NULL WHERE id=$1`
		if _, err := s.db.Q(ctx).Exec(ctx, update, r.ID); err != nil {
			return fmt.Errorf("claim paused run: %w", err)
		}
		w, err := scanWorkflow(s.db.Q(ctx).QueryRow(ctx,
			`SELECT`+workflowColumns+` FROM workflows WHERE id=$1`, r.WorkflowID))
		if err != nil {
			return fmt.Errorf("load workflow for paused run: %w", err)
		}
		run, wf = r, w
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if run == nil {
		return nil, nil, nil
	}
	return run, wf, nil
}

// RecordStep appends a step outcome.
func (s *Store) RecordStep(ctx context.Context, runID, orgID uuid.UUID, index int, stepID, stepType, status string, detail any, stepErr *string) error {
	body, err := json.Marshal(detail)
	if err != nil {
		body = []byte(`{}`)
	}
	const q = `
		INSERT INTO workflow_step_runs (run_id, organization_id, step_index, step_id, step_type,
			status, detail, error, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())`
	if _, err := s.db.Q(ctx).Exec(ctx, q, runID, orgID, index, stepID, stepType, status, body, stepErr); err != nil {
		return fmt.Errorf("record step: %w", err)
	}
	return nil
}

// ListRuns returns the runs of one workflow, newest first.
func (s *Store) ListRuns(ctx context.Context, p *rbac.Principal, workflowID uuid.UUID, status string, limit, offset int) ([]Run, int64, error) {
	where := `organization_id = $1 AND workflow_id = $2`
	args := []any{p.OrganizationID, workflowID}
	if status != "" {
		where += fmt.Sprintf(` AND status = $%d`, len(args)+1)
		args = append(args, status)
	}
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM workflow_runs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}
	q := `SELECT` + runColumns + ` FROM workflow_runs WHERE ` + where +
		fmt.Sprintf(` ORDER BY started_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.Q(ctx).Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

// GetRun returns one run scoped to the organization.
func (s *Store) GetRun(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Run, error) {
	const q = `SELECT` + runColumns + ` FROM workflow_runs WHERE id=$1 AND organization_id=$2`
	r, err := scanRun(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}

// ListRunSteps returns the steps of a run in order.
func (s *Store) ListRunSteps(ctx context.Context, p *rbac.Principal, runID uuid.UUID) ([]StepRun, error) {
	const q = `
		SELECT id, run_id, organization_id, step_index, step_id, step_type, status, detail, error, started_at, finished_at
		FROM workflow_step_runs WHERE run_id=$1 AND organization_id=$2 ORDER BY step_index`
	rows, err := s.db.Q(ctx).Query(ctx, q, runID, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list run steps: %w", err)
	}
	defer rows.Close()
	var out []StepRun
	for rows.Next() {
		var sr StepRun
		if err := rows.Scan(&sr.ID, &sr.RunID, &sr.OrganizationID, &sr.StepIndex, &sr.StepID,
			&sr.StepType, &sr.Status, &sr.Detail, &sr.Error, &sr.StartedAt, &sr.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Event-triggered matching and de-duplication
// ---------------------------------------------------------------------------

// MatchEventWorkflows returns the enabled, event-triggered workflows in an
// organization that watch the given event type.
func (s *Store) MatchEventWorkflows(ctx context.Context, orgID uuid.UUID, eventType string) ([]Workflow, error) {
	const q = `SELECT` + workflowColumns + `
		FROM workflows
		WHERE organization_id=$1 AND enabled AND archived_at IS NULL
		  AND trigger_type='event' AND event_type=$2
		ORDER BY created_at`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID, eventType)
	if err != nil {
		return nil, fmt.Errorf("match event workflows: %w", err)
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// CreateEventRun atomically claims an event and creates the run that will execute it.
//
// The de-duplication ledger is the arbiter, not the run table: workflow_run_events
// has a primary key on (workflow_id, event_id), and a redelivered event cannot
// insert a second row. The run row is inserted first only because the ledger's
// run_id column references it, so a delivery that loses the claim deletes the run it
// just made. Both statements are in one transaction, so no observer ever sees the
// orphan: the losing delivery commits nothing at all.
//
// The alternative ordering -- create the run, then claim, and mark the loser
// "skipped" -- leaves one orphan run row per redelivery. That is what the outbox
// guarantees will happen under retry, so it is not a corner case.
//
// claimed is false when this delivery is a duplicate; run is then nil and the caller
// returns without executing anything.
func (s *Store) CreateEventRun(ctx context.Context, w *Workflow, eventID uuid.UUID, depth int, correlationID string) (run *Run, claimed bool, err error) {
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const runQ = `
			INSERT INTO workflow_runs (workflow_id, organization_id, trigger_event_id, trigger_type,
				run_depth, correlation_id, steps_total)
			VALUES ($1,$2,$3,'event',$4,NULLIF($5,''),$6)
			RETURNING ` + runColumns
		row := s.db.Q(ctx).QueryRow(ctx, runQ, w.ID, w.OrganizationID, eventID, depth, correlationID,
			len(mustDefinition(w).Steps))
		created, scanErr := scanRun(row)
		if scanErr != nil {
			return fmt.Errorf("create event run: %w", scanErr)
		}

		claimTag, execErr := s.db.Q(ctx).Exec(ctx, `
			INSERT INTO workflow_run_events (workflow_id, event_id, organization_id, run_id)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (workflow_id, event_id) DO NOTHING`,
			w.ID, eventID, w.OrganizationID, created.ID)
		if execErr != nil {
			return fmt.Errorf("claim event run: %w", execErr)
		}
		if claimTag.RowsAffected() != 1 {
			// A duplicate delivery. Drop the run we just made so the transaction commits
			// nothing and the history stays one run per event.
			if _, delErr := s.db.Q(ctx).Exec(ctx,
				`DELETE FROM workflow_runs WHERE id = $1`, created.ID); delErr != nil {
				return fmt.Errorf("discard the duplicate event run: %w", delErr)
			}
			claimed = false
			return nil
		}
		run = created
		claimed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return run, claimed, nil
}

// DueScheduledWorkflows returns enabled schedule workflows whose next_run_at has
// passed. It claims them by advancing next_run_at in the same transaction, so two
// replicas do not both fire the same due time.
func (s *Store) DueScheduledWorkflows(ctx context.Context, limit int) ([]Workflow, error) {
	var out []Workflow
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const claim = `
			SELECT` + workflowColumns + ` FROM workflows
			WHERE enabled AND archived_at IS NULL AND trigger_type='schedule'
			  AND next_run_at IS NOT NULL AND next_run_at <= now()
			ORDER BY next_run_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED`
		rows, err := s.db.Q(ctx).Query(ctx, claim, limit)
		if err != nil {
			return fmt.Errorf("claim due workflows: %w", err)
		}
		for rows.Next() {
			w, err := scanWorkflow(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, *w)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out {
			next, err := s.advanceNextRun(&out[i])
			if err != nil {
				return err
			}
			if _, err := s.db.Q(ctx).Exec(ctx,
				`UPDATE workflows SET next_run_at=$2 WHERE id=$1`, out[i].ID, next); err != nil {
				return fmt.Errorf("advance next run: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// advanceNextRun computes the following fire time from now, so a workflow whose
// schedule was missed while the worker was down fires once and moves on rather than
// firing once for every missed interval.
func (s *Store) advanceNextRun(w *Workflow) (*time.Time, error) {
	if w.ScheduleCron == nil {
		return nil, nil
	}
	spec, err := ParseCron(*w.ScheduleCron)
	if err != nil {
		return nil, fmt.Errorf("workflow %s has an unparseable cron: %w", w.ID, err)
	}
	loc, err := scheduleLocation(w.ScheduleTimezone)
	if err != nil {
		loc = time.UTC
	}
	next, ok := spec.Next(time.Now(), loc)
	if !ok {
		return nil, nil
	}
	return &next, nil
}

// SetNextRun sets a workflow's next run instant. Used when a schedule is edited.
func (s *Store) SetNextRun(ctx context.Context, id uuid.UUID, next *time.Time) error {
	if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE workflows SET next_run_at=$2 WHERE id=$1`, id, next); err != nil {
		return fmt.Errorf("set next run: %w", err)
	}
	return nil
}

// ResetScheduleForTest is used by tests to force a workflow due immediately.
func (s *Store) ResetScheduleForTest(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.SetNextRun(ctx, id, &at)
}
