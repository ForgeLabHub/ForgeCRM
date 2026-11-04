package crm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// lockActiveMember keeps account or tenant deactivation from racing an operation.
// The principal's permissions must have been resolved freshly by the caller.
func (s *Service) lockActiveMember(ctx context.Context, p *rbac.Principal) error {
	if p == nil || p.UserID == uuid.Nil || p.OrganizationID == uuid.Nil {
		return rbac.ErrForbidden
	}
	var active bool
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT true FROM memberships m
		JOIN users u ON u.id=m.user_id JOIN organizations o ON o.id=m.organization_id
		WHERE m.user_id=$1 AND m.organization_id=$2 AND m.status='active'
		AND u.status='active' AND o.status='active' FOR SHARE OF m,u,o`, p.UserID, p.OrganizationID).Scan(&active)
	if database.IsNoRows(err) {
		return rbac.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("check task membership: %w", err)
	}
	return nil
}

// ListMyOpenTasks uses the ordinary CRM list path but always narrows it to the
// caller's own assignments, even for administrators. Optional bounds are a
// half-open interval on the due timestamp; callers choose the timezone.
func (s *Service) ListMyOpenTasks(ctx context.Context, p *rbac.Principal, from, before *time.Time, limit int) ([]Task, error) {
	if p == nil || !p.Has(rbac.PermTasksRead) {
		return nil, rbac.ErrForbidden
	}
	if limit <= 0 || limit > httpx.MaxPageSize || (from != nil && before != nil && !from.Before(*before)) {
		return nil, httpx.ErrBadRequest
	}
	var tasks []Task
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockActiveMember(ctx, p); err != nil {
			return err
		}
		var err error
		tasks, _, err = s.ListTasks(ctx, p, ListQuery{
			Pagination: httpx.Pagination{Limit: limit}, AssigneeID: &p.UserID,
			OpenTasksOnly: true, TaskDueFrom: from, TaskDueBefore: before,
		})
		return err
	})
	return tasks, err
}

// ListMyOverdueTasks returns the caller's own tasks whose deadline has passed and
// which are still open. It is the same ownership-narrowed list path as
// ListMyOpenTasks, with the due bound pinned to "before now" rather than supplied
// by the caller, so the bot cannot be talked into another window.
func (s *Service) ListMyOverdueTasks(ctx context.Context, p *rbac.Principal, now time.Time, limit int) ([]Task, error) {
	if p == nil || !p.Has(rbac.PermTasksRead) {
		return nil, rbac.ErrForbidden
	}
	if limit <= 0 || limit > httpx.MaxPageSize {
		return nil, httpx.ErrBadRequest
	}
	cutoff := now.UTC()
	var tasks []Task
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockActiveMember(ctx, p); err != nil {
			return err
		}
		var err error
		tasks, _, err = s.ListTasks(ctx, p, ListQuery{
			Pagination: httpx.Pagination{Limit: limit}, AssigneeID: &p.UserID,
			OpenTasksOnly: true, TaskDueBefore: &cutoff,
		})
		return err
	})
	return tasks, err
}

// GetMyTask does not reveal whether another user's or another tenant's task exists.
func (s *Service) GetMyTask(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Task, error) {
	if p == nil || !p.Has(rbac.PermTasksRead) {
		return nil, rbac.ErrForbidden
	}
	var task *Task
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockActiveMember(ctx, p); err != nil {
			return err
		}
		var err error
		task, err = s.GetTask(ctx, p, id)
		if err != nil {
			return err
		}
		if task.ArchivedAt != nil || task.AssigneeUserID == nil || *task.AssigneeUserID != p.UserID {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

// CompleteMyTask uses the existing CRM update path (including audit and outbox).
// Repeating completion is a no-op and does not emit a second completion event.
func (s *Service) CompleteMyTask(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Task, error) {
	return s.updateMyTask(ctx, p, id, nil, true)
}

// RescheduleMyTask changes only the due timestamp of an open assigned task.
func (s *Service) RescheduleMyTask(ctx context.Context, p *rbac.Principal, id uuid.UUID, due time.Time) (*Task, error) {
	if due.IsZero() {
		return nil, httpx.ErrBadRequest
	}
	return s.updateMyTask(ctx, p, id, &due, false)
}

func (s *Service) updateMyTask(ctx context.Context, p *rbac.Principal, id uuid.UUID, due *time.Time, complete bool) (*Task, error) {
	if p == nil || !p.HasAll(rbac.PermTasksRead, rbac.PermTasksUpdate) {
		return nil, rbac.ErrForbidden
	}
	var result *Task
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockActiveMember(ctx, p); err != nil {
			return err
		}
		// Lock before checking the assignee and before the CRM service reads the row.
		// This prevents a reassignment or archive from slipping between authorization
		// and the update. A missing or non-owned task produces the same result.
		var locked uuid.UUID
		err := s.db.Q(ctx).QueryRow(ctx, `SELECT id FROM tasks
			WHERE id=$1 AND organization_id=$2 AND assignee_user_id=$3 AND archived_at IS NULL
			FOR UPDATE`, id, p.OrganizationID, p.UserID).Scan(&locked)
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock assigned task: %w", err)
		}
		task, err := s.GetTask(ctx, p, id)
		if err != nil {
			return err
		}
		if complete && task.Status == "completed" {
			result = task
			return nil
		}
		if task.Status != "open" && task.Status != "in_progress" {
			return ErrConflict
		}
		in := TaskInput{
			Title: task.Title, Description: task.Description, Status: task.Status,
			Priority: task.Priority, DueAt: task.DueAt, AssigneeUserID: task.AssigneeUserID,
			TeamID: task.TeamID, AccountID: task.AccountID, ContactID: task.ContactID,
			DealID: task.DealID, TicketID: task.TicketID,
		}
		if complete {
			in.Status = "completed"
		} else {
			in.DueAt = due
		}
		result, err = s.UpdateTask(ctx, p, id, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
