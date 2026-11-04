//go:build integration

package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
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
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

func TestTelegramOwnTasksPostgres(t *testing.T) {
	raw := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(strings.ToLower(parsed.Path), "_test") {
		t.Fatal("refusing to reset non-test database")
	}
	ctx := context.Background()
	db, err := database.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(raw, slog.Default()); err != nil {
		t.Fatal(err)
	}
	var org, otherOrg, user, peer uuid.UUID
	if err := db.Pool().QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('Tasks','tg-tasks') RETURNING id`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('Other','tg-other') RETURNING id`).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO users(email,display_name) VALUES('tasks@example.test','One') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO users(email,display_name) VALUES('peer@example.test','Peer') RETURNING id`).Scan(&peer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO memberships(user_id,organization_id) VALUES($1,$2),($3,$2),($1,$4)`, user, org, peer, otherOrg); err != nil {
		t.Fatal(err)
	}
	p := rbac.NewPrincipal(user, "tasks@example.test", "One", org, []rbac.Permission{rbac.PermTasksRead, rbac.PermTasksUpdate, rbac.PermTasksAssign})
	noUpdate := rbac.NewPrincipal(user, "", "", org, []rbac.Permission{rbac.PermTasksRead})
	noRead := rbac.NewPrincipal(user, "", "", org, nil)
	svc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	day := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	insert := func(orgID, assignee uuid.UUID, due time.Time) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.Pool().QueryRow(ctx, `INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,due_at,created_by) VALUES($1,'Private task','open','normal',$2,$3,$2) RETURNING id`, orgID, assignee, due).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mine := insert(org, user, day.Add(time.Hour))
	peers := insert(org, peer, day.Add(time.Hour))
	otherTenant := insert(otherOrg, user, day.Add(time.Hour))
	future := insert(org, user, day.Add(48*time.Hour))
	next := day.Add(24 * time.Hour)
	items, err := svc.ListMyOpenTasks(ctx, p, &day, &next, 10)
	if err != nil || len(items) != 1 || items[0].ID != mine {
		t.Fatalf("own today list: %+v %v", items, err)
	}
	if _, err := svc.ListMyOpenTasks(ctx, noRead, nil, nil, 10); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("read permission: %v", err)
	}
	for _, id := range []uuid.UUID{peers, otherTenant} {
		if task, err := svc.GetMyTask(ctx, p, id); !errors.Is(err, crm.ErrNotFound) || task != nil {
			t.Fatalf("exposed other task: %+v %v", task, err)
		}
		if _, err := svc.CompleteMyTask(ctx, p, id); !errors.Is(err, crm.ErrNotFound) {
			t.Fatalf("updated other task: %v", err)
		}
	}
	if _, err := svc.CompleteMyTask(ctx, noUpdate, mine); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("update permission: %v", err)
	}
	due := day.Add(3 * time.Hour)
	if task, err := svc.RescheduleMyTask(ctx, p, mine, due); err != nil || task.DueAt == nil || !task.DueAt.Equal(due) || task.Title != "Private task" {
		t.Fatalf("reschedule: %+v %v", task, err)
	}
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.CompleteMyTask(ctx, p, mine); err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("completion failures: %d", failures.Load())
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type='crm.task.completed' AND payload->>'task_id'=$1`, mine.String()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("completion events %d: %v", count, err)
	}
	if _, err := svc.RescheduleMyTask(ctx, p, mine, due); !errors.Is(err, crm.ErrConflict) {
		t.Fatalf("rescheduled completed task: %v", err)
	}
	// A concurrent reassignment wins the row lock. The command must wait, then
	// see the new assignee rather than updating on a stale authorization decision.
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE tasks SET assignee_user_id=$1 WHERE id=$2`, peer, future); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := svc.CompleteMyTask(ctx, p, future); result <- err }()
	select {
	case err := <-result:
		t.Fatalf("operation did not wait for reassignment: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("reassigned task was writable: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE memberships SET status='suspended' WHERE user_id=$1 AND organization_id=$2`, user, org); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteMyTask(ctx, p, future); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("inactive member: %v", err)
	}

	// A failed database lookup must not be reported as an unlinked user or a
	// successfully processed command. In particular, do not include the code in
	// a user-facing reply.
	tg := notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-only"}, db, slog.Default(), audit.New(db), nil, nil)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for _, command := range []string{"/tasks", "/link " + strings.Repeat("a", 43)} {
		reply, err := tg.HandleCommand(cancelled, 99, command)
		if err == nil || reply != "" {
			t.Fatalf("database error was swallowed: reply=%q err=%v", reply, err)
		}
	}
	secret := strings.Repeat("a", 43)
	reply, err := tg.HandleCommand(ctx, 99, "/link "+secret)
	if err != nil || reply == "" || strings.Contains(reply, secret) {
		t.Fatalf("invalid code should be a generic reply: %q %v", reply, err)
	}
}
