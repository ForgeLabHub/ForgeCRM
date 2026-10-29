package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Task is an actionable item.
type Task struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Title          string     `json:"title"`
	Description    *string    `json:"description,omitempty"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	AssigneeUserID *uuid.UUID `json:"assignee_user_id,omitempty"`
	TeamID         *uuid.UUID `json:"team_id,omitempty"`
	AccountID      *uuid.UUID `json:"account_id,omitempty"`
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	DealID         *uuid.UUID `json:"deal_id,omitempty"`
	TicketID       *uuid.UUID `json:"ticket_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

// TaskInput is the writable projection of a task.
type TaskInput struct {
	Title          string     `json:"title"`
	Description    *string    `json:"description"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority"`
	DueAt          *time.Time `json:"due_at"`
	AssigneeUserID *uuid.UUID `json:"assignee_user_id"`
	TeamID         *uuid.UUID `json:"team_id"`
	AccountID      *uuid.UUID `json:"account_id"`
	ContactID      *uuid.UUID `json:"contact_id"`
	DealID         *uuid.UUID `json:"deal_id"`
	TicketID       *uuid.UUID `json:"ticket_id"`
}

var taskStatuses = map[string]bool{"open": true, "in_progress": true, "completed": true, "cancelled": true}
var taskPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

// Validate checks the input.
func (in *TaskInput) Validate() error {
	details := map[string]string{}
	if strings.TrimSpace(in.Title) == "" {
		details["title"] = "title is required"
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if !taskStatuses[in.Status] {
		details["status"] = "must be one of open, in_progress, completed, cancelled"
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !taskPriorities[in.Priority] {
		details["priority"] = "must be one of low, normal, high, urgent"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

const taskColumns = `
	id, organization_id, title, description, status, priority, due_at, completed_at,
	assignee_user_id, team_id, account_id, contact_id, deal_id, ticket_id,
	created_at, updated_at, archived_at`

func scanTask(row rowScanner) (*Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.OrganizationID, &t.Title, &t.Description, &t.Status, &t.Priority,
		&t.DueAt, &t.CompletedAt, &t.AssigneeUserID, &t.TeamID, &t.AccountID, &t.ContactID,
		&t.DealID, &t.TicketID, &t.CreatedAt, &t.UpdatedAt, &t.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTasks returns a page of tasks.
func (s *Service) ListTasks(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Task, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if !q.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q.AssigneeID != nil {
		args = append(args, *q.AssigneeID)
		where = append(where, fmt.Sprintf("assignee_user_id = $%d", len(args)))
	}
	if q.DealID != nil {
		args = append(args, *q.DealID)
		where = append(where, fmt.Sprintf("deal_id = $%d", len(args)))
	}
	if q.AccountID != nil {
		args = append(args, *q.AccountID)
		where = append(where, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if q.ContactID != nil {
		args = append(args, *q.ContactID)
		where = append(where, fmt.Sprintf("contact_id = $%d", len(args)))
	}
	if q.TicketID != nil {
		args = append(args, *q.TicketID)
		where = append(where, fmt.Sprintf("ticket_id = $%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf("title ILIKE $%d", len(args)))
	}
	// Record-level rule: without the assign permission, a user only sees tasks
	// assigned to them or to one of their teams.
	if !p.Has(rbac.PermTasksAssign) {
		args = append(args, p.UserID, p.TeamIDs)
		where = append(where, fmt.Sprintf("(assignee_user_id = $%d OR team_id = ANY($%d))", len(args)-1, len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM tasks WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}
	order := q.Sort.OrderClause("due_at ASC NULLS LAST, created_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	rows, err := s.db.Q(ctx).Query(ctx,
		"SELECT "+taskColumns+" FROM tasks WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

// GetTask loads one task in scope.
func (s *Service) GetTask(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Task, error) {
	const q = `SELECT ` + taskColumns + ` FROM tasks WHERE id = $1 AND organization_id = $2`
	t, err := scanTask(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	return t, nil
}

// CreateTask inserts a task.
func (s *Service) CreateTask(ctx context.Context, p *rbac.Principal, in TaskInput) (*Task, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Task
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if in.AccountID != nil {
			if err := s.assertAccountInOrg(ctx, p.OrganizationID, in.AccountID); err != nil {
				return err
			}
		}
		const q = `
			INSERT INTO tasks (organization_id, title, description, status, priority, due_at,
				assignee_user_id, team_id, account_id, contact_id, deal_id, ticket_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			RETURNING ` + taskColumns
		t, err := scanTask(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.Title, in.Description,
			in.Status, in.Priority, in.DueAt, in.AssigneeUserID, in.TeamID, in.AccountID,
			in.ContactID, in.DealID, in.TicketID, p.UserID))
		if err != nil {
			return wrapConstraint(err, "task")
		}
		created = t
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventTaskCreated, map[string]any{
			"task_id": t.ID, "assignee_user_id": t.AssigneeUserID,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "tasks.create", EntityType: "task", EntityID: t.ID.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateTask applies a partial update. Completing a task records the time and
// emits a completion event.
func (s *Service) UpdateTask(ctx context.Context, p *rbac.Principal, id uuid.UUID, in TaskInput) (*Task, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var updated *Task
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		before, err := s.GetTask(ctx, p, id)
		if err != nil {
			return err
		}
		const q = `
			UPDATE tasks SET title=$3, description=$4, status=$5, priority=$6, due_at=$7,
				assignee_user_id=$8, team_id=$9, account_id=$10, contact_id=$11, deal_id=$12,
				ticket_id=$13,
				completed_at = CASE WHEN $5 = 'completed' AND completed_at IS NULL THEN now()
				                    WHEN $5 <> 'completed' THEN NULL
				                    ELSE completed_at END
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + taskColumns
		t, err := scanTask(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.Title,
			in.Description, in.Status, in.Priority, in.DueAt, in.AssigneeUserID, in.TeamID,
			in.AccountID, in.ContactID, in.DealID, in.TicketID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return wrapConstraint(err, "task")
		}
		updated = t
		if before.Status != "completed" && t.Status == "completed" {
			if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventTaskCompleted, map[string]any{
				"task_id": t.ID,
			}); err != nil {
				return err
			}
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "tasks.update", EntityType: "task", EntityID: id.String(),
			Before: map[string]any{"status": before.Status}, After: map[string]any{"status": t.Status},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ---------------------------------------------------------------------------
// Activities
// ---------------------------------------------------------------------------

// Activity is a logged interaction.
type Activity struct {
	ID              uuid.UUID  `json:"id"`
	OrganizationID  uuid.UUID  `json:"organization_id"`
	Type            string     `json:"type"`
	Subject         string     `json:"subject"`
	Body            *string    `json:"body,omitempty"`
	OccurredAt      time.Time  `json:"occurred_at"`
	DurationMinutes *int       `json:"duration_minutes,omitempty"`
	Outcome         *string    `json:"outcome,omitempty"`
	AccountID       *uuid.UUID `json:"account_id,omitempty"`
	ContactID       *uuid.UUID `json:"contact_id,omitempty"`
	DealID          *uuid.UUID `json:"deal_id,omitempty"`
	TicketID        *uuid.UUID `json:"ticket_id,omitempty"`
	UserID          *uuid.UUID `json:"user_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ActivityInput is the writable projection of an activity.
type ActivityInput struct {
	Type            string     `json:"type"`
	Subject         string     `json:"subject"`
	Body            *string    `json:"body"`
	OccurredAt      *time.Time `json:"occurred_at"`
	DurationMinutes *int       `json:"duration_minutes"`
	Outcome         *string    `json:"outcome"`
	AccountID       *uuid.UUID `json:"account_id"`
	ContactID       *uuid.UUID `json:"contact_id"`
	DealID          *uuid.UUID `json:"deal_id"`
	TicketID        *uuid.UUID `json:"ticket_id"`
}

var activityTypes = map[string]bool{
	"call": true, "meeting": true, "note": true, "email": true,
	"task": true, "stage_change": true, "system": true,
}

// Validate checks the input.
func (in *ActivityInput) Validate() error {
	details := map[string]string{}
	if !activityTypes[in.Type] {
		details["type"] = "must be one of call, meeting, note, email, task, stage_change, system"
	}
	if in.DurationMinutes != nil && *in.DurationMinutes < 0 {
		details["duration_minutes"] = "must not be negative"
	}
	if in.AccountID == nil && in.ContactID == nil && in.DealID == nil && in.TicketID == nil {
		details["target"] = "an activity must reference an account, contact, deal or ticket"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

const activityColumns = `
	id, organization_id, type, subject, body, occurred_at, duration_minutes, outcome,
	account_id, contact_id, deal_id, ticket_id, user_id, created_at`

func scanActivity(row rowScanner) (*Activity, error) {
	var a Activity
	err := row.Scan(&a.ID, &a.OrganizationID, &a.Type, &a.Subject, &a.Body, &a.OccurredAt,
		&a.DurationMinutes, &a.Outcome, &a.AccountID, &a.ContactID, &a.DealID, &a.TicketID,
		&a.UserID, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListActivities returns a page of activities.
func (s *Service) ListActivities(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Activity, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if q.AccountID != nil {
		args = append(args, *q.AccountID)
		where = append(where, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if q.DealID != nil {
		args = append(args, *q.DealID)
		where = append(where, fmt.Sprintf("deal_id = $%d", len(args)))
	}
	if q.ContactID != nil {
		args = append(args, *q.ContactID)
		where = append(where, fmt.Sprintf("contact_id = $%d", len(args)))
	}
	if q.TicketID != nil {
		args = append(args, *q.TicketID)
		where = append(where, fmt.Sprintf("ticket_id = $%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("type = $%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf("(subject ILIKE $%d OR COALESCE(body,'') ILIKE $%d)", len(args), len(args)))
	}
	if q.From != nil {
		args = append(args, *q.From)
		where = append(where, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if q.To != nil {
		args = append(args, *q.To)
		where = append(where, fmt.Sprintf("occurred_at <= $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM activities WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count activities: %w", err)
	}
	order := q.Sort.OrderClause("occurred_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	rows, err := s.db.Q(ctx).Query(ctx,
		"SELECT "+activityColumns+" FROM activities WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list activities: %w", err)
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

// CreateActivity records an interaction.
func (s *Service) CreateActivity(ctx context.Context, p *rbac.Principal, in ActivityInput) (*Activity, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Activity
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		occurred := time.Now()
		if in.OccurredAt != nil {
			occurred = *in.OccurredAt
		}
		const q = `
			INSERT INTO activities (organization_id, type, subject, body, occurred_at,
				duration_minutes, outcome, account_id, contact_id, deal_id, ticket_id, user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			RETURNING ` + activityColumns
		a, err := scanActivity(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.Type,
			in.Subject, in.Body, occurred, in.DurationMinutes, in.Outcome, in.AccountID,
			in.ContactID, in.DealID, in.TicketID, p.UserID))
		if err != nil {
			return wrapConstraint(err, "activity")
		}
		created = a
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "activities.create", EntityType: "activity", EntityID: a.ID.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ---------------------------------------------------------------------------
// Tickets
// ---------------------------------------------------------------------------

// Ticket is a support case.
type Ticket struct {
	ID                 uuid.UUID  `json:"id"`
	OrganizationID     uuid.UUID  `json:"organization_id"`
	Number             int64      `json:"number"`
	Subject            string     `json:"subject"`
	Description        *string    `json:"description,omitempty"`
	Status             string     `json:"status"`
	Priority           string     `json:"priority"`
	Channel            string     `json:"channel"`
	AccountID          *uuid.UUID `json:"account_id,omitempty"`
	ContactID          *uuid.UUID `json:"contact_id,omitempty"`
	AssigneeUserID     *uuid.UUID `json:"assignee_user_id,omitempty"`
	TeamID             *uuid.UUID `json:"team_id,omitempty"`
	SLAPolicyID        *uuid.UUID `json:"sla_policy_id,omitempty"`
	FirstResponseDueAt *time.Time `json:"first_response_due_at,omitempty"`
	ResolutionDueAt    *time.Time `json:"resolution_due_at,omitempty"`
	FirstRespondedAt   *time.Time `json:"first_responded_at,omitempty"`
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`
	SLABreached        bool       `json:"sla_breached"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
}

// TicketInput is the writable projection of a ticket.
type TicketInput struct {
	Subject        string     `json:"subject"`
	Description    *string    `json:"description"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority"`
	Channel        string     `json:"channel"`
	AccountID      *uuid.UUID `json:"account_id"`
	ContactID      *uuid.UUID `json:"contact_id"`
	AssigneeUserID *uuid.UUID `json:"assignee_user_id"`
	TeamID         *uuid.UUID `json:"team_id"`
	SLAPolicyID    *uuid.UUID `json:"sla_policy_id"`
}

var ticketStatuses = map[string]bool{"new": true, "open": true, "pending": true, "resolved": true, "closed": true}
var ticketChannels = map[string]bool{"web": true, "email": true, "phone": true, "telegram": true, "api": true}

// Validate checks the input.
func (in *TicketInput) Validate() error {
	details := map[string]string{}
	if strings.TrimSpace(in.Subject) == "" {
		details["subject"] = "subject is required"
	}
	if in.Status == "" {
		in.Status = "new"
	}
	if !ticketStatuses[in.Status] {
		details["status"] = "must be one of new, open, pending, resolved, closed"
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if !taskPriorities[in.Priority] {
		details["priority"] = "must be one of low, normal, high, urgent"
	}
	if in.Channel == "" {
		in.Channel = "web"
	}
	if !ticketChannels[in.Channel] {
		details["channel"] = "must be one of web, email, phone, telegram, api"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

const ticketColumns = `
	id, organization_id, number, subject, description, status, priority, channel,
	account_id, contact_id, assignee_user_id, team_id, sla_policy_id,
	first_response_due_at, resolution_due_at, first_responded_at, resolved_at,
	sla_breached, created_at, updated_at, archived_at`

func scanTicket(row rowScanner) (*Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.OrganizationID, &t.Number, &t.Subject, &t.Description, &t.Status,
		&t.Priority, &t.Channel, &t.AccountID, &t.ContactID, &t.AssigneeUserID, &t.TeamID,
		&t.SLAPolicyID, &t.FirstResponseDueAt, &t.ResolutionDueAt, &t.FirstRespondedAt,
		&t.ResolvedAt, &t.SLABreached, &t.CreatedAt, &t.UpdatedAt, &t.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTickets returns a page of tickets.
func (s *Service) ListTickets(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Ticket, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if !q.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q.AssigneeID != nil {
		args = append(args, *q.AssigneeID)
		where = append(where, fmt.Sprintf("assignee_user_id = $%d", len(args)))
	}
	if q.AccountID != nil {
		args = append(args, *q.AccountID)
		where = append(where, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if q.ContactID != nil {
		args = append(args, *q.ContactID)
		where = append(where, fmt.Sprintf("contact_id = $%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf("subject ILIKE $%d", len(args)))
	}
	if !p.Has(rbac.PermTicketsAssign) {
		args = append(args, p.UserID, p.TeamIDs)
		where = append(where, fmt.Sprintf("(assignee_user_id = $%d OR team_id = ANY($%d))", len(args)-1, len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM tickets WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tickets: %w", err)
	}
	order := q.Sort.OrderClause("created_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	rows, err := s.db.Q(ctx).Query(ctx,
		"SELECT "+ticketColumns+" FROM tickets WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tickets: %w", err)
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

// GetTicket loads one ticket in scope.
func (s *Service) GetTicket(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Ticket, error) {
	const q = `SELECT ` + ticketColumns + ` FROM tickets WHERE id = $1 AND organization_id = $2`
	t, err := scanTicket(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load ticket: %w", err)
	}
	return t, nil
}

// CreateTicket inserts a ticket and applies the matching SLA policy deadlines.
func (s *Service) CreateTicket(ctx context.Context, p *rbac.Principal, in TicketInput) (*Ticket, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Ticket
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if in.AccountID != nil {
			if err := s.assertAccountInOrg(ctx, p.OrganizationID, in.AccountID); err != nil {
				return err
			}
		}

		// Resolve an SLA policy: an explicit one, or the one matching priority.
		var (
			slaID         *uuid.UUID
			firstResponse *time.Time
			resolution    *time.Time
		)
		const slaQ = `
			SELECT id, first_response_minutes, resolution_minutes
			FROM sla_policies
			WHERE organization_id = $1 AND ($2::uuid IS NULL OR id = $2::uuid) AND priority = $3
			ORDER BY (id = $2::uuid) DESC NULLS LAST
			LIMIT 1`
		var (
			id     uuid.UUID
			frMin  int
			resMin int
		)
		err := s.db.Q(ctx).QueryRow(ctx, slaQ, p.OrganizationID, in.SLAPolicyID, in.Priority).
			Scan(&id, &frMin, &resMin)
		if err == nil {
			now := time.Now()
			fr := now.Add(time.Duration(frMin) * time.Minute)
			res := now.Add(time.Duration(resMin) * time.Minute)
			slaID, firstResponse, resolution = &id, &fr, &res
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("resolve sla policy: %w", err)
		}

		const q = `
			INSERT INTO tickets (organization_id, subject, description, status, priority, channel,
				account_id, contact_id, assignee_user_id, team_id, sla_policy_id,
				first_response_due_at, resolution_due_at, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			RETURNING ` + ticketColumns
		t, err := scanTicket(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.Subject,
			in.Description, in.Status, in.Priority, in.Channel, in.AccountID, in.ContactID,
			in.AssigneeUserID, in.TeamID, slaID, firstResponse, resolution, p.UserID))
		if err != nil {
			return wrapConstraint(err, "ticket")
		}
		created = t
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventTicketCreated, map[string]any{
			"ticket_id": t.ID, "number": t.Number, "priority": t.Priority,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "tickets.create", EntityType: "ticket", EntityID: t.ID.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateTicket applies a partial update, stamping first response and resolution
// times so that SLA reporting has real data to work with.
func (s *Service) UpdateTicket(ctx context.Context, p *rbac.Principal, id uuid.UUID, in TicketInput) (*Ticket, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var updated *Ticket
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE tickets SET subject=$3, description=$4, status=$5, priority=$6, channel=$7,
				account_id=$8, contact_id=$9, assignee_user_id=$10, team_id=$11,
				first_responded_at = CASE WHEN first_responded_at IS NULL AND $5 IN ('open','pending','resolved','closed') THEN now() ELSE first_responded_at END,
				resolved_at = CASE WHEN $5 IN ('resolved','closed') AND resolved_at IS NULL THEN now() WHEN $5 NOT IN ('resolved','closed') THEN NULL ELSE resolved_at END,
				sla_breached = CASE
					WHEN resolution_due_at IS NOT NULL AND $5 IN ('resolved','closed') AND now() > resolution_due_at THEN true
					WHEN resolution_due_at IS NOT NULL AND $5 NOT IN ('resolved','closed') AND now() > resolution_due_at THEN true
					ELSE sla_breached END
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + ticketColumns
		t, err := scanTicket(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.Subject,
			in.Description, in.Status, in.Priority, in.Channel, in.AccountID, in.ContactID,
			in.AssigneeUserID, in.TeamID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return wrapConstraint(err, "ticket")
		}
		updated = t
		eventType := outbox.EventTicketUpdated
		if t.Status == "closed" || t.Status == "resolved" {
			eventType = outbox.EventTicketClosed
		}
		if err := s.outbox.Emit(ctx, &p.OrganizationID, eventType, map[string]any{
			"ticket_id": t.ID, "status": t.Status,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "tickets.update", EntityType: "ticket", EntityID: id.String(),
			After: map[string]any{"status": t.Status},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
