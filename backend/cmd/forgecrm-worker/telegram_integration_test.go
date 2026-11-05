//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/testdb"
)

type taskTelegramMock struct {
	photo, text atomic.Int32
	failAll     atomic.Bool
	photoOK     atomic.Bool
}

type telegramTestGate struct{ blocked atomic.Bool }

func (g *telegramTestGate) AllowSend(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _, _, _ string) (bool, string, error) {
	return !g.blocked.Load(), "test policy", nil
}

func (m *taskTelegramMock) RoundTrip(r *http.Request) (*http.Response, error) {
	status := 200
	body := `{"ok":true}`
	if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
		m.photo.Add(1)
		if !m.photoOK.Load() {
			body = `{"ok":false,"description":"do not log this"}`
		}
	} else if strings.HasSuffix(r.URL.Path, "/sendMessage") {
		m.text.Add(1)
	} else {
		return nil, io.ErrUnexpectedEOF
	}
	if m.failAll.Load() {
		status = 429
		body = `{"ok":false}`
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestTelegramWorkerPostgres(t *testing.T) {
	if os.Getenv(testdb.URLEnv) == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	db := testdb.Connect(t)
	raw := testdb.URL(t)
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(raw, slog.Default()); err != nil {
		t.Fatal(err)
	}
	var org, user, task uuid.UUID
	if err := db.Pool().QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('Worker','telegram-worker') RETURNING id`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO users(email,display_name,locale) VALUES('worker@example.test','Worker','ru') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO memberships(user_id,organization_id) VALUES($1,$2)`, user, org); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO user_roles(user_id,organization_id,role_id) SELECT $1,$2,id FROM roles WHERE organization_id IS NULL AND key='tenant_admin'`, user, org); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO telegram_links(organization_id,user_id,telegram_user_id,linked_at) VALUES($1,$2,123,now())`, org, user); err != nil {
		t.Fatal(err)
	}
	mock := &taskTelegramMock{}
	previous := http.DefaultTransport
	http.DefaultTransport = mock
	t.Cleanup(func() { http.DefaultTransport = previous })
	gate := &telegramTestGate{}
	tg := notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-token"}, db, slog.Default(), audit.New(db), nil, gate)
	store := outbox.NewStore(db)
	crmSvc := crm.NewService(db, audit.New(db), store)
	queue := jobs.NewQueue(db, slog.Default())
	r := &handlerRegistry{db: db, telegram: tg}
	due := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	p := rbac.NewPrincipal(user, "worker@example.test", "Worker", org, nil)
	created, err := crmSvc.CreateTask(ctx, p, crm.TaskInput{Title: "Проверить договор", Status: "open", Priority: "normal", AssigneeUserID: &user, DueAt: &due})
	if err != nil {
		t.Fatal(err)
	}
	task = created.ID
	events, err := store.FetchPending(ctx, 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("outbox: %d %v", len(events), err)
	}
	if err := enqueueTelegramForEvent(ctx, db, tg, events[0]); err != nil {
		t.Fatal(err)
	}
	if err := enqueueTelegramForEvent(ctx, db, tg, events[0]); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind=$1`, jobs.KindTelegramTaskNotification).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate jobs: %d %v", count, err)
	}
	job, err := queue.Claim(ctx, "worker-test")
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	if err := r.handle(ctx, queue, job); err != nil {
		t.Fatal(err)
	}
	job, err = queue.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	var progress struct {
		Status     string `json:"status"`
		ImageError string `json:"image_error"`
	}
	if err := json.Unmarshal(job.Progress, &progress); err != nil || progress.Status != "text_accepted" || progress.ImageError == "" || strings.Contains(progress.ImageError, "do not log") || strings.Contains(progress.ImageError, "test-token") {
		t.Fatalf("fallback status: %+v %v", progress, err)
	}
	if err := r.handle(ctx, queue, job); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != 1 || mock.text.Load() != 1 {
		t.Fatalf("checkpoint did not prevent duplicate: %d/%d", mock.photo.Load(), mock.text.Load())
	}
	if err := queue.Complete(ctx, job.ID, nil); err != nil {
		t.Fatal(err)
	}
	// A due-date change emits an outbox event and the new overdue schedule. A
	// complete outage produces a retry on the existing queue with backoff.
	newDue := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Microsecond)
	_, err = crmSvc.UpdateTask(ctx, p, task, crm.TaskInput{Title: created.Title, Status: "open", Priority: "normal", AssigneeUserID: &user, DueAt: &newDue})
	if err != nil {
		t.Fatal(err)
	}
	events, err = store.FetchPending(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if err := enqueueTelegramForEvent(ctx, db, tg, ev); err != nil {
			t.Fatal(err)
		}
	}
	job, err = queue.Claim(ctx, "worker-test")
	if err != nil || job == nil {
		t.Fatalf("claim due-change: %v", err)
	}
	mock.failAll.Store(true)
	err = r.handle(ctx, queue, job)
	if err == nil || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("failure not sanitized: %v", err)
	}
	if err := queue.Fail(ctx, job, err); err != nil {
		t.Fatal(err)
	}
	job, err = queue.Get(ctx, job.ID)
	if err != nil || job.Status != jobs.StatusQueued || !job.RunAfter.After(time.Now()) || job.Attempts != 1 {
		t.Fatalf("retry/backoff: %+v %v", job, err)
	}
	// Old overdue job must not deliver after the deadline changes.
	var oldID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `SELECT id FROM jobs WHERE kind=$1 AND payload->>'kind'='overdue' AND (payload->>'due_at')::timestamptz=$2`, jobs.KindTelegramTaskNotification, due).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	old, err := queue.Get(ctx, oldID)
	if err != nil {
		t.Fatal(err)
	}
	before := mock.photo.Load()
	if err := r.handle(ctx, queue, old); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != before {
		t.Fatal("stale overdue reminder sent")
	}
	// Successful PNG delivery never sends the text fallback.
	mock.failAll.Store(false)
	mock.photoOK.Store(true)
	var revision uuid.UUID
	if err := db.Pool().QueryRow(ctx, `SELECT telegram_due_revision FROM tasks WHERE id=$1`, task).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	makeJob := func(key, kind string) *jobs.Job {
		t.Helper()
		p := telegramTaskJob{DedupeKey: key, Kind: kind, TaskID: task, UserID: user, ScheduleID: revision}
		if err := insertTelegramTaskJob(ctx, db, org, p, time.Now()); err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := db.Pool().QueryRow(ctx, `SELECT job_id FROM telegram_task_delivery_receipts WHERE dedupe_key=$1`, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		j, err := queue.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	photoBefore, textBefore := mock.photo.Load(), mock.text.Load()
	photoJob := makeJob("test:photo", "assignment")
	if err := r.handle(ctx, queue, photoJob); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != photoBefore+1 || mock.text.Load() != textBefore {
		t.Fatal("successful photo unexpectedly fell back")
	}
	var outcome string
	if err := db.Pool().QueryRow(ctx, `SELECT outcome FROM telegram_task_delivery_receipts WHERE dedupe_key='test:photo'`).Scan(&outcome); err != nil || outcome != "photo_accepted" {
		t.Fatalf("photo outcome %s: %v", outcome, err)
	}
	// The durable receipt prevents a duplicate even after the finished job is pruned.
	if _, err := db.Pool().Exec(ctx, `DELETE FROM jobs WHERE id=$1`, photoJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := insertTelegramTaskJob(ctx, db, org, telegramTaskJob{DedupeKey: "test:photo", Kind: "assignment", TaskID: task, UserID: user}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs WHERE payload->>'dedupe_key'='test:photo'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("receipt failed after job retention: %d %v", count, err)
	}
	// Both consent withdrawal and revoked tasks.read prevent any transport call.
	gate.blocked.Store(true)
	blocked := makeJob("test:blocked", "assignment")
	photoBefore = mock.photo.Load()
	if err := r.handle(ctx, queue, blocked); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != photoBefore {
		t.Fatal("consent bypass")
	}
	if err := db.Pool().QueryRow(ctx, `SELECT outcome FROM telegram_task_delivery_receipts WHERE dedupe_key='test:blocked'`).Scan(&outcome); err != nil || outcome != "blocked" {
		t.Fatalf("blocked outcome %s: %v", outcome, err)
	}
	gate.blocked.Store(false)
	if _, err := db.Pool().Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND organization_id=$2`, user, org); err != nil {
		t.Fatal(err)
	}
	forbidden := makeJob("test:forbidden", "assignment")
	if err := r.handle(ctx, queue, forbidden); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != photoBefore {
		t.Fatal("RBAC bypass")
	}
	if err := db.Pool().QueryRow(ctx, `SELECT outcome FROM telegram_task_delivery_receipts WHERE dedupe_key='test:forbidden'`).Scan(&outcome); err != nil || outcome != "forbidden" {
		t.Fatalf("forbidden outcome %s: %v", outcome, err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO user_roles(user_id,organization_id,role_id) SELECT $1,$2,id FROM roles WHERE organization_id IS NULL AND key='tenant_admin'`, user, org); err != nil {
		t.Fatal(err)
	}
	// A per-account preference silences one type without touching consent or RBAC:
	// the job is a no-op, recorded as "muted", and no transport call happens.
	if err := tg.SetNotificationPreference(ctx, 123, notify.KindAssignment, false); err != nil {
		t.Fatal(err)
	}
	muted := makeJob("test:muted", "assignment")
	photoBefore = mock.photo.Load()
	if err := r.handle(ctx, queue, muted); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != photoBefore {
		t.Fatal("muted preference was ignored")
	}
	if err := db.Pool().QueryRow(ctx, `SELECT outcome FROM telegram_task_delivery_receipts WHERE dedupe_key='test:muted'`).Scan(&outcome); err != nil || outcome != "muted" {
		t.Fatalf("muted outcome %s: %v", outcome, err)
	}
	// Re-enabling restores delivery.
	if err := tg.SetNotificationPreference(ctx, 123, notify.KindAssignment, true); err != nil {
		t.Fatal(err)
	}
	// The card language follows the assistant choice, then the CRM locale, then
	// English. The user row was seeded with locale 'ru'.
	readLocale := func() string {
		t.Helper()
		var got string
		if err := db.Pool().QueryRow(ctx, `SELECT COALESCE(ts.locale,u.locale,'en')
			FROM tasks t
			JOIN users u ON u.id=t.assignee_user_id
			LEFT JOIN telegram_links l ON l.organization_id=t.organization_id AND l.user_id=t.assignee_user_id
				AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL
			LEFT JOIN telegram_user_settings ts ON ts.telegram_user_id=l.telegram_user_id
			WHERE t.id=$1`, task).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := readLocale(); got != "ru" {
		t.Fatalf("card locale with no assistant setting = %q, want ru", got)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO telegram_user_settings(telegram_user_id,locale) VALUES(123,'en') ON CONFLICT (telegram_user_id) DO UPDATE SET locale='en'`); err != nil {
		t.Fatal(err)
	}
	if got := readLocale(); got != "en" {
		t.Fatalf("assistant locale must win over CRM locale, got %q", got)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM telegram_user_settings WHERE telegram_user_id=123`); err != nil {
		t.Fatal(err)
	}
	if got := readLocale(); got != "ru" {
		t.Fatalf("card locale must fall back to CRM locale, got %q", got)
	}
	// Completion uses the current CRM state and a real photo card.
	if _, err := crmSvc.UpdateTask(ctx, p, task, crm.TaskInput{Title: created.Title, Status: "completed", Priority: "normal", AssigneeUserID: &user, DueAt: &newDue}); err != nil {
		t.Fatal(err)
	}
	completed := makeJob("test:completed", "completed")
	if err := r.handle(ctx, queue, completed); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != photoBefore+1 {
		t.Fatal("completed photo not sent")
	}
	// Expired challenge material and old encrypted replies are removed in
	// bounded batches without deleting update replay receipts.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO telegram_link_codes(code_hash,organization_id,user_id,expires_at) VALUES(decode(repeat('ab',32),'hex'),$1,$2,now()-interval '2 days'),(decode(repeat('cd',32),'hex'),$1,$2,now()+interval '1 hour')`, org, user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO telegram_update_receipts(update_id,reply_ciphertext,processed_at) VALUES(9001,'secret',now()-interval '8 days'),(9002,'secret',now())`); err != nil {
		t.Fatal(err)
	}
	if err := cleanupTelegram(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM telegram_link_codes`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("code cleanup %d: %v", count, err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM telegram_update_receipts WHERE update_id=9001 AND reply_ciphertext IS NULL AND outcome='unavailable'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("reply cleanup %d: %v", count, err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM telegram_update_receipts WHERE update_id=9002 AND reply_ciphertext IS NOT NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recent reply lost %d: %v", count, err)
	}
	// A pre-existing overdue task is discovered once even by concurrent sweeps.
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	var existing, firstRevision uuid.UUID
	if err := db.Pool().QueryRow(ctx, `INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,due_at,created_by) VALUES($1,'Existing','open','normal',$2,$3,$2) RETURNING id,telegram_due_revision`, org, user, past).Scan(&existing, &firstRevision); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errCh <- sweepTelegramOverdue(ctx, db, tg) }()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM telegram_task_delivery_receipts WHERE dedupe_key=$1`, "overdue:"+firstRevision.String()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent overdue scheduling %d %v", count, err)
	}
	var overdueJobID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `SELECT job_id FROM telegram_task_delivery_receipts WHERE dedupe_key=$1`, "overdue:"+firstRevision.String()).Scan(&overdueJobID); err != nil {
		t.Fatal(err)
	}
	overdueJob, err := queue.Get(ctx, overdueJobID)
	if err != nil {
		t.Fatal(err)
	}
	photoBefore = mock.photo.Load()
	if err := r.handle(ctx, queue, overdueJob); err != nil {
		t.Fatal(err)
	}
	if mock.photo.Load() != photoBefore+1 {
		t.Fatal("overdue photo not sent")
	}
	// Going away from, then back to, the same deadline is a new schedule.
	if _, err := db.Pool().Exec(ctx, `UPDATE tasks SET due_at=$1 WHERE id=$2`, newDue, existing); err != nil {
		t.Fatal(err)
	}
	var secondRevision uuid.UUID
	if err := db.Pool().QueryRow(ctx, `UPDATE tasks SET due_at=$1 WHERE id=$2 RETURNING telegram_due_revision`, past, existing).Scan(&secondRevision); err != nil {
		t.Fatal(err)
	}
	if firstRevision == secondRevision {
		t.Fatal("schedule revision did not change")
	}
	if err := sweepTelegramOverdue(ctx, db, tg); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM telegram_task_delivery_receipts WHERE dedupe_key=$1`, "overdue:"+secondRevision.String()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("repeated deadline not rescheduled %d %v", count, err)
	}

}
