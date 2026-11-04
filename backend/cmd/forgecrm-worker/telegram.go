package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

type telegramTaskJob struct {
	DedupeKey  string     `json:"dedupe_key"`
	Kind       string     `json:"kind"`
	TaskID     uuid.UUID  `json:"task_id"`
	UserID     uuid.UUID  `json:"user_id"`
	DueAt      *time.Time `json:"due_at,omitempty"`
	ScheduleID uuid.UUID  `json:"schedule_id"`
}

// enqueueTelegramForEvent projects an outbox event into the existing jobs queue.
// A durable receipt keeps both drain paths and retries idempotent even after job retention.
func enqueueTelegramForEvent(ctx context.Context, db *database.DB, telegram *notify.TelegramService, ev outbox.Event) error {
	if telegram == nil || !telegram.Enabled() || ev.OrganizationID == nil {
		return nil
	}
	kind := ""
	switch ev.Type {
	case outbox.EventTaskCreated, outbox.EventTaskAssigned:
		kind = "assignment"
	case outbox.EventTaskDueChanged:
		kind = "due_changed"
	case outbox.EventTaskCompleted:
		kind = "completed"
	default:
		return nil
	}
	var payload struct {
		TaskID     uuid.UUID  `json:"task_id"`
		UserID     *uuid.UUID `json:"assignee_user_id"`
		DueAt      *time.Time `json:"due_at"`
		ScheduleID uuid.UUID  `json:"schedule_id"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return errors.New("invalid task event")
	}
	if payload.TaskID == uuid.Nil || payload.UserID == nil || *payload.UserID == uuid.Nil {
		return nil
	}
	// Older outbox events predate revisions. Only project them if their owner
	// still matches; derive the current due date rather than using a stale value.
	if payload.ScheduleID == uuid.Nil {
		err := db.Q(ctx).QueryRow(ctx, `SELECT telegram_due_revision,due_at FROM tasks WHERE id=$1 AND organization_id=$2 AND assignee_user_id=$3`, payload.TaskID, *ev.OrganizationID, *payload.UserID).Scan(&payload.ScheduleID, &payload.DueAt)
		if database.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	job := telegramTaskJob{DedupeKey: "event:" + ev.ID.String(), Kind: kind, TaskID: payload.TaskID, UserID: *payload.UserID, DueAt: payload.DueAt, ScheduleID: payload.ScheduleID}
	if err := insertTelegramTaskJob(ctx, db, *ev.OrganizationID, job, time.Now()); err != nil {
		return err
	}
	if kind != "completed" && payload.DueAt != nil {
		due := payload.DueAt.UTC()
		overdue := telegramTaskJob{DedupeKey: "overdue:" + payload.ScheduleID.String(), Kind: "overdue", TaskID: payload.TaskID, UserID: *payload.UserID, DueAt: &due, ScheduleID: payload.ScheduleID}
		if err := insertTelegramTaskJob(ctx, db, *ev.OrganizationID, overdue, due); err != nil {
			return err
		}
	}
	return nil
}

func insertTelegramTaskJob(ctx context.Context, db *database.DB, orgID uuid.UUID, job telegramTaskJob, runAfter time.Time) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return errors.New("encode telegram task notification")
	}
	return db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		result, err := db.Q(ctx).Exec(ctx, `INSERT INTO telegram_task_delivery_receipts(dedupe_key) VALUES($1) ON CONFLICT DO NOTHING`, job.DedupeKey)
		if err != nil {
			return errors.New("record telegram task notification")
		}
		if result.RowsAffected() == 0 {
			return nil
		}
		var jobID uuid.UUID
		err = db.Q(ctx).QueryRow(ctx, `INSERT INTO jobs(organization_id,kind,payload,priority,max_attempts,run_after)
			VALUES($1,$2,$3,100,5,$4) RETURNING id`, orgID, jobs.KindTelegramTaskNotification, payload, runAfter).Scan(&jobID)
		if err != nil {
			return errors.New("enqueue telegram task notification")
		}
		_, err = db.Q(ctx).Exec(ctx, `UPDATE telegram_task_delivery_receipts SET job_id=$2 WHERE dedupe_key=$1`, job.DedupeKey, jobID)
		return err
	})
}

func (r *handlerRegistry) sendTelegramTask(ctx context.Context, queue *jobs.Queue, job *jobs.Job) error {
	if job.OrganizationID == nil || r.telegram == nil {
		return errors.New("invalid telegram task job")
	}
	var checkpoint struct {
		Done       bool   `json:"done"`
		Status     string `json:"status"`
		ImageError string `json:"image_error"`
	}
	if len(job.Checkpoint) > 0 {
		_ = json.Unmarshal(job.Checkpoint, &checkpoint)
	}
	var payload telegramTaskJob
	if json.Unmarshal(job.Payload, &payload) != nil || payload.TaskID == uuid.Nil || payload.UserID == uuid.Nil {
		return errors.New("invalid telegram task job")
	}
	if checkpoint.Done {
		return recordTelegramTaskOutcome(ctx, r.db, payload.DedupeKey, checkpoint.Status, checkpoint.ImageError)
	}
	if !r.telegram.Enabled() {
		return errors.New("telegram transport disabled")
	}
	var result notify.TaskDeliveryResult
	err := r.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// Use the same link-before-task lock order as incoming commands.
		var linkID uuid.UUID
		err := r.db.Q(ctx).QueryRow(ctx, `SELECT id FROM telegram_links WHERE organization_id=$1 AND user_id=$2 AND linked_at IS NOT NULL AND revoked_at IS NULL FOR SHARE`, *job.OrganizationID, payload.UserID).Scan(&linkID)
		if database.IsNoRows(err) {
			result.Status = "no_recipient"
			return nil
		}
		if err != nil {
			return err
		}
		// Lock the task until the outbound API decision, so reassignment, archival
		// or due-date edits cannot race a send of private task data to the old user.
		var title, status, priority, locale string
		var due *time.Time
		var scheduleID uuid.UUID
		err = r.db.Q(ctx).QueryRow(ctx, `SELECT t.title,t.status,t.priority,t.due_at,u.locale,t.telegram_due_revision
			FROM tasks t JOIN users u ON u.id=t.assignee_user_id
			WHERE t.id=$1 AND t.organization_id=$2 AND t.assignee_user_id=$3 AND t.archived_at IS NULL
			FOR SHARE OF t,u`, payload.TaskID, *job.OrganizationID, payload.UserID).Scan(&title, &status, &priority, &due, &locale, &scheduleID)
		if database.IsNoRows(err) {
			result.Status = "stale"
			return nil
		}
		if err != nil {
			return err
		}
		if payload.Kind != "completed" && payload.ScheduleID != uuid.Nil && payload.ScheduleID != scheduleID {
			result.Status = "stale"
			return nil
		}
		if payload.Kind == "completed" {
			if status != "completed" {
				result.Status = "stale"
				return nil
			}
		} else if status != "open" && status != "in_progress" {
			result.Status = "stale"
			return nil
		}
		if payload.Kind == "overdue" || payload.Kind == "due_changed" {
			if (due == nil) != (payload.DueAt == nil) || (due != nil && !due.Equal(*payload.DueAt)) {
				result.Status = "stale"
				return nil
			}
		}
		if payload.Kind == "overdue" && due == nil {
			result.Status = "stale"
			return nil
		}
		if payload.Kind == "overdue" && due.After(time.Now()) {
			// Clock skew between the database and worker must not silently drop it.
			return errors.New("overdue notification not due yet")
		}
		perms, err := rbac.NewService(r.db).EffectivePermissions(ctx, payload.UserID, *job.OrganizationID)
		if err != nil {
			return err
		}
		principal := rbac.NewPrincipal(payload.UserID, "", "", *job.OrganizationID, perms)
		if !principal.Has(rbac.PermTasksRead) {
			result.Status = "forbidden"
			return nil
		}
		result, err = r.telegram.NotifyTask(ctx, *job.OrganizationID, payload.UserID, notify.TaskCard{Kind: payload.Kind, Locale: locale, Title: title, Priority: priority, DueAt: due})
		return err
	})
	if err != nil {
		return errors.New("telegram task delivery failed")
	}
	// The existing jobs queue records retries and final status. The checkpoint
	// prevents a retried job from sending again after a successful API response.
	if err := queue.SaveCheckpoint(ctx, job.ID, map[string]any{"done": true, "status": result.Status, "image_error": result.ImageError}, map[string]any{"status": result.Status, "image_error": result.ImageError}); err != nil {
		return errors.New("record telegram task delivery status")
	}
	return recordTelegramTaskOutcome(ctx, r.db, payload.DedupeKey, result.Status, result.ImageError)
}

func recordTelegramTaskOutcome(ctx context.Context, db *database.DB, key, status, imageError string) error {
	_, err := db.Q(ctx).Exec(ctx, `UPDATE telegram_task_delivery_receipts SET outcome=$2,image_error=$3,finished_at=now() WHERE dedupe_key=$1`, key, status, imageError)
	if err != nil {
		return errors.New("record telegram task delivery status")
	}
	return nil
}

// Keep short-lived challenges and unsent encrypted reply bodies bounded while
// retaining update IDs permanently to prevent replay. Rows are batch limited.
func cleanupTelegram(ctx context.Context, db *database.DB) error {
	_, err := db.Q(ctx).Exec(ctx, `DELETE FROM telegram_link_codes WHERE code_hash IN
		(SELECT code_hash FROM telegram_link_codes WHERE expires_at < now() - interval '1 day' ORDER BY expires_at LIMIT 1000 FOR UPDATE SKIP LOCKED)`)
	if err != nil {
		return errors.New("clean expired telegram link codes")
	}
	_, err = db.Q(ctx).Exec(ctx, `UPDATE telegram_update_receipts SET reply_ciphertext=NULL, outcome='unavailable', finished_at=now() WHERE update_id IN
		(SELECT update_id FROM telegram_update_receipts WHERE reply_ciphertext IS NOT NULL AND processed_at < now() - interval '7 days' ORDER BY processed_at LIMIT 1000 FOR UPDATE SKIP LOCKED)`)
	if err != nil {
		return errors.New("clean expired telegram pending replies")
	}
	return nil
}

// Discover already overdue tasks that existed before Telegram was enabled.
// Receipts survive job retention, so repeated scans don't resend reminders.
func sweepTelegramOverdue(ctx context.Context, db *database.DB, telegram *notify.TelegramService) error {
	if telegram == nil || !telegram.Enabled() {
		return nil
	}
	rows, err := db.Q(ctx).Query(ctx, `SELECT t.organization_id,t.id,t.assignee_user_id,t.due_at,t.telegram_due_revision
		FROM tasks t JOIN telegram_links l ON l.organization_id=t.organization_id AND l.user_id=t.assignee_user_id
		WHERE t.archived_at IS NULL AND t.status IN ('open','in_progress') AND t.due_at <= now()
		AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM telegram_task_delivery_receipts r WHERE r.dedupe_key='overdue:' || t.telegram_due_revision::text)
		ORDER BY t.due_at,t.id LIMIT 1000`)
	if err != nil {
		return errors.New("scan overdue telegram tasks")
	}
	type candidate struct {
		org, task, user, revision uuid.UUID
		due                       time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.org, &c.task, &c.user, &c.due, &c.revision); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range candidates {
		job := telegramTaskJob{DedupeKey: "overdue:" + c.revision.String(), Kind: "overdue", TaskID: c.task, UserID: c.user, DueAt: &c.due, ScheduleID: c.revision}
		if err := insertTelegramTaskJob(ctx, db, c.org, job, c.due); err != nil {
			return err
		}
	}
	return nil
}
