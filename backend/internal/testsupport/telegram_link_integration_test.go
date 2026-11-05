//go:build integration

package testsupport

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/testdb"
	"github.com/google/uuid"
)

func TestTelegramLinkPostgres(t *testing.T) {
	ctx := context.Background()
	db := testdb.Connect(t)
	raw := testdb.URL(t)
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(raw, slog.Default()); err != nil {
		t.Fatal(err)
	}
	svc := notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-only"}, db, slog.Default(), audit.New(db), nil, nil)
	newUser := func(slug string) *rbac.Principal {
		t.Helper()
		var org, user uuid.UUID
		if err := db.Pool().QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES($1,$2) RETURNING id`, slug, slug).Scan(&org); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool().QueryRow(ctx, `INSERT INTO users(email,display_name) VALUES($1,$2) RETURNING id`, slug+"@example.test", slug).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO memberships(user_id,organization_id) VALUES($1,$2)`, user, org); err != nil {
			t.Fatal(err)
		}
		return rbac.NewPrincipal(user, slug+"@example.test", slug, org, nil)
	}
	issue := func(p *rbac.Principal) string {
		t.Helper()
		code, err := svc.CreateLinkCode(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	p1, p2 := newUser("tg-first"), newUser("tg-second")
	code1, code2 := issue(p1), issue(p2)
	if len(code1) != 43 || code1 == code2 {
		t.Fatal("codes must be unique 256-bit tokens")
	}
	var hashed bool
	if err := db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telegram_link_codes WHERE code_hash=$1)`, secrets.HashToken(code1)).Scan(&hashed); err != nil || !hashed {
		t.Fatalf("hash not stored: %v", err)
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: code1, TelegramUserID: 101}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: code1, TelegramUserID: 102}); err == nil {
		t.Fatal("reused code accepted")
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: code2, TelegramUserID: 101}); err == nil {
		t.Fatal("cross-tenant claim accepted")
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: code2, TelegramUserID: 202}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE memberships SET status='suspended' WHERE user_id=$1 AND organization_id=$2`, p1.UserID, p1.OrganizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveUser(ctx, 101); err == nil {
		t.Fatal("suspended membership resolved")
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE memberships SET status='active' WHERE user_id=$1 AND organization_id=$2`, p1.UserID, p1.OrganizationID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlink(ctx, p1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveUser(ctx, 101); err == nil {
		t.Fatal("revoked link resolved")
	}
	expired := issue(p1)
	if _, err := db.Pool().Exec(ctx, `UPDATE telegram_link_codes SET expires_at=now()-interval '1 second' WHERE code_hash=$1`, secrets.HashToken(expired)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: expired, TelegramUserID: 101}); err == nil {
		t.Fatal("expired code accepted")
	}
	pending := issue(p1)
	if err := svc.Unlink(ctx, p1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: pending, TelegramUserID: 101}); err == nil {
		t.Fatal("revoked challenge accepted")
	}
	suspended := issue(p1)
	if _, err := db.Pool().Exec(ctx, `UPDATE memberships SET status='suspended' WHERE user_id=$1 AND organization_id=$2`, p1.UserID, p1.OrganizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: suspended, TelegramUserID: 101}); err == nil {
		t.Fatal("suspended membership accepted")
	}
	if _, err := svc.CreateLinkCode(ctx, p1); err == nil {
		t.Fatal("code issued for suspended membership")
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE memberships SET status='active' WHERE user_id=$1 AND organization_id=$2`, p1.UserID, p1.OrganizationID); err != nil {
		t.Fatal(err)
	}
	race := issue(p1)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := svc.Link(ctx, notify.LinkRequest{Code: race, TelegramUserID: int64(1000 + i)}); err == nil {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent redemptions succeeded %d times", successes.Load())
	}
	var active int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM telegram_links WHERE user_id=$1 AND organization_id=$2 AND linked_at IS NOT NULL AND revoked_at IS NULL`, p1.UserID, p1.OrganizationID).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active links=%d: %v", active, err)
	}
}

// Even distinct outstanding challenges for one CRM user must not create two
// active Telegram links when redeemed in parallel.
func TestTelegramMultipleCodesConcurrentPostgres(t *testing.T) {
	ctx := context.Background()
	db := testdb.Connect(t)
	raw := testdb.URL(t)
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(raw, slog.Default()); err != nil {
		t.Fatal(err)
	}
	var org, user uuid.UUID
	if err := db.Pool().QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES('Race Org','tg-race') RETURNING id`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `INSERT INTO users(email,display_name) VALUES('race@example.test','Race') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO memberships(user_id,organization_id) VALUES($1,$2)`, user, org); err != nil {
		t.Fatal(err)
	}
	svc := notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-only"}, db, slog.Default(), audit.New(db), nil, nil)
	p := rbac.NewPrincipal(user, "race@example.test", "Race", org, nil)
	c1, err := svc.CreateLinkCode(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := svc.CreateLinkCode(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i, c := range []string{c1, c2} {
		wg.Add(1)
		go func(i int, c string) {
			defer wg.Done()
			if _, err := svc.Link(ctx, notify.LinkRequest{Code: c, TelegramUserID: int64(i + 400)}); err == nil {
				successes.Add(1)
			}
		}(i, c)
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("different codes produced %d active links", successes.Load())
	}
}
