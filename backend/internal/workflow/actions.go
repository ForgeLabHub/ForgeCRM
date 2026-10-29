package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// The action steps. Each calls crm.Service with the author's principal, so the
// permission check, the tenant scope and the audit entry are exactly those of the
// equivalent API call. Nothing here writes SQL of its own; if a workflow can do it,
// a user with the same rights can do it through the UI.

// doCreateTask creates a task. Recognised args: title (required), description,
// priority, due_in_hours, assignee_user_id, account_id, contact_id, deal_id.
func (e *Engine) doCreateTask(ctx context.Context, p *rbac.Principal, step *Step, env *EvalContext) (map[string]any, error) {
	title, ok := argString(step.Args, "title", env)
	if !ok || strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("create_task needs a title")
	}
	in := crm.TaskInput{Title: title, Status: "open", Priority: "normal"}
	if v, ok := argString(step.Args, "description", env); ok && v != "" {
		in.Description = &v
	}
	if v, ok := argString(step.Args, "priority", env); ok && v != "" {
		in.Priority = v
	}
	if in.Priority != "low" && in.Priority != "normal" && in.Priority != "high" && in.Priority != "urgent" {
		return nil, fmt.Errorf("create_task priority %q is not one of low, normal, high, urgent", in.Priority)
	}
	if hours, ok := numberArg(step.Args, "due_in_hours"); ok && hours > 0 {
		due := e.now().Add(time.Duration(hours * float64(time.Hour)))
		in.DueAt = &due
	}
	var err error
	if in.AssigneeUserID, err = argUUID(step.Args, "assignee_user_id", env); err != nil {
		return nil, err
	}
	if in.AccountID, err = argUUID(step.Args, "account_id", env); err != nil {
		return nil, err
	}
	if in.ContactID, err = argUUID(step.Args, "contact_id", env); err != nil {
		return nil, err
	}
	if in.DealID, err = argUUID(step.Args, "deal_id", env); err != nil {
		return nil, err
	}
	task, err := e.crm.CreateTask(ctx, p, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task_id": task.ID.String(), "title": task.Title}, nil
}

// doCreateTicket opens a support ticket. Args: subject (required), description,
// priority, account_id, contact_id, assignee_user_id.
func (e *Engine) doCreateTicket(ctx context.Context, p *rbac.Principal, step *Step, env *EvalContext) (map[string]any, error) {
	subject, ok := argString(step.Args, "subject", env)
	if !ok || strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("create_ticket needs a subject")
	}
	in := crm.TicketInput{Subject: subject, Priority: "normal", Status: "open"}
	if v, ok := argString(step.Args, "description", env); ok && v != "" {
		in.Description = &v
	}
	if v, ok := argString(step.Args, "priority", env); ok && v != "" {
		in.Priority = v
	}
	var err error
	if in.AccountID, err = argUUID(step.Args, "account_id", env); err != nil {
		return nil, err
	}
	if in.ContactID, err = argUUID(step.Args, "contact_id", env); err != nil {
		return nil, err
	}
	if in.AssigneeUserID, err = argUUID(step.Args, "assignee_user_id", env); err != nil {
		return nil, err
	}
	ticket, err := e.crm.CreateTicket(ctx, p, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ticket_id": ticket.ID.String(), "subject": ticket.Subject}, nil
}

// doNotify creates an in-app notification. Args: title (required), body,
// user_id (required — the recipient), link.
//
// The recipient must be an explicit user id rather than "everyone", because a
// notification is not a broadcast channel and a workflow that notifies an
// unbounded audience is a way to leak data to people who should not see it.
func (e *Engine) doNotify(ctx context.Context, p *rbac.Principal, step *Step, env *EvalContext) (map[string]any, error) {
	title, ok := argString(step.Args, "title", env)
	if !ok || strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("notify needs a title")
	}
	userID, err := argUUID(step.Args, "user_id", env)
	if err != nil {
		return nil, err
	}
	if userID == nil {
		return nil, fmt.Errorf("notify needs a user_id")
	}
	var body *string
	if v, ok := argString(step.Args, "body", env); ok && v != "" {
		body = &v
	}
	// The recipient must be a member of the same organization. The notifications
	// table references users and organizations separately, so without this check a
	// workflow could address a user who belongs to a different tenant and write into
	// their feed. The membership is verified here rather than relied on from a
	// constraint that does not exist.
	var member bool
	if err := e.db.Q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM memberships WHERE user_id=$1 AND organization_id=$2 AND status='active')`,
		*userID, p.OrganizationID).Scan(&member); err != nil {
		return nil, fmt.Errorf("verify the notification recipient: %w", err)
	}
	if !member {
		return nil, fmt.Errorf("notify recipient is not an active member of this organization")
	}
	if err := e.crm.CreateNotification(ctx, p.OrganizationID, *userID, "workflow", title, body, nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"notified_user_id": userID.String()}, nil
}

// doUpdateDeal moves a deal to a new stage, or changes its amount. Args: deal_id
// (required), stage_id, amount.
//
// Moving a stage requires deals.change_stage on the author; changing an amount
// requires deals.update. Both are checked by crm.Service, not here.
func (e *Engine) doUpdateDeal(ctx context.Context, p *rbac.Principal, step *Step, env *EvalContext) (map[string]any, error) {
	dealID, err := argUUID(step.Args, "deal_id", env)
	if err != nil {
		return nil, err
	}
	if dealID == nil {
		return nil, fmt.Errorf("update_deal needs a deal_id")
	}
	stageID, err := argUUID(step.Args, "stage_id", env)
	if err != nil {
		return nil, err
	}
	if stageID == nil {
		return nil, fmt.Errorf("update_deal needs a stage_id")
	}
	note, _ := argString(step.Args, "note", env)
	deal, err := e.crm.MoveDealStage(ctx, p, *dealID, *stageID, note)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deal_id": deal.ID.String(), "stage_id": deal.StageID.String()}, nil
}

// numberArg reads a numeric argument.
func numberArg(args map[string]any, key string) (float64, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	f, ok := toFloat(v)
	return f, ok
}
