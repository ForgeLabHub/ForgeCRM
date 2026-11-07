package main

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/telegram"
)

// The worker can deliver Telegram notifications, but the canonical implementation
// lives in internal/telegram so the standalone bot service and the worker cannot
// diverge. These adapters keep the worker's outbox drain and job handler working
// unchanged; the job queue's atomic claim and the delivery receipts make it safe
// for both processes to run this path at once.

type telegramTaskJob = telegram.TaskJob

func enqueueTelegramForEvent(ctx context.Context, db *database.DB, tg *notify.TelegramService, ev outbox.Event) error {
	return telegram.EnqueueForEvent(ctx, db, tg, ev)
}

func insertTelegramTaskJob(ctx context.Context, db *database.DB, orgID uuid.UUID, job telegramTaskJob, runAfter time.Time) error {
	return telegram.InsertTaskJob(ctx, db, orgID, job, runAfter)
}

func (r *handlerRegistry) sendTelegramTask(ctx context.Context, queue *jobs.Queue, job *jobs.Job) error {
	h := &telegram.DeliveryHandler{DB: r.db, Telegram: r.telegram}
	return h.Handle(ctx, queue, job)
}

func cleanupTelegram(ctx context.Context, db *database.DB) error {
	return telegram.Cleanup(ctx, db)
}

func sweepTelegramOverdue(ctx context.Context, db *database.DB, tg *notify.TelegramService) error {
	return telegram.SweepOverdue(ctx, db, tg)
}
