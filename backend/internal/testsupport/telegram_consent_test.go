//go:build integration

// Integration test for the Telegram transport's consent gate, driven through the
// real consent policy and the real schema.
//
// Telegram is one of the outbound channels the consent model names, and a decision
// recorded against a person's address applies to it as much as to email: agreeing
// to be emailed is not agreeing to be messaged on Telegram, and refusing contact is
// refusing it everywhere. The property under test is that a notification to a linked
// user whose contact has been denied is refused before the bot API is contacted.
// There is no Telegram server in the test, so a message that got past the gate would
// fail with a network error instead of a consent refusal, and the assertions below
// would report that difference.
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/wiring"
)

// seedTelegramLink creates an organization, a user and a linked Telegram account,
// and returns the ids the transport resolves from the link.
func seedTelegramLink(t *testing.T, db *database.DB, slug string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	org := insertOrganization(t, db, slug)
	user := insertUser(t, db, org, slug+"@notify.test")
	chatID := int64(len(slug) + 100000)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO telegram_links (organization_id, user_id, telegram_user_id, linked_at)
		VALUES ($1, $2, $3, now())`, org, user, chatID); err != nil {
		t.Fatalf("insert telegram link: %v", err)
	}
	return org, user
}

// recordDecision inserts a consent decision directly, which is what the transport's
// policy reads. The HTTP surface for recording is covered elsewhere; here the point
// is the read the transport performs.
func recordDecision(t *testing.T, db *database.DB, org uuid.UUID, email, purpose, channel, state string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO consent_records (organization_id, subject_email, purpose, channel, state)
		VALUES ($1, $2, $3, $4, $5)`, org, email, purpose, channel, state); err != nil {
		t.Fatalf("record consent decision: %v", err)
	}
}

// TestTelegramNotifyRefusesADeniedPerson is the guard for the Telegram channel. The
// person has denied contact on Telegram, so the notification must be refused with a
// consent error and the refusal must be audited, exactly as a blocked email is.
func TestTelegramNotifyRefusesADeniedPerson(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org, user := seedTelegramLink(t, db, "telegram-denied")
	recordDecision(t, db, org, "telegram-denied@notify.test", "transactional", "telegram", "denied")

	gate := wiring.NewConsentGate(consent.NewService(db, audit.New(db)))
	svc := notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-token"},
		db, slog.Default(), audit.New(db), nil, gate)

	err := svc.NotifyUser(ctx, org, user, "a notification")
	if !errors.Is(err, notify.ErrConsentBlocked) {
		t.Fatalf("err = %v, want ErrConsentBlocked", err)
	}

	var blocked int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE organization_id = $1 AND action = $2`,
		org, string(audit.ActionConsentBlockedSend)).Scan(&blocked); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if blocked == 0 {
		t.Fatal("the blocked Telegram notification was not recorded in the audit trail")
	}
}

// TestTelegramNotifySendsWhenConsentAllows checks the gate is not a blanket refusal.
// With no denial on record the notification passes the gate and reaches the bot API,
// which is unreachable in the test: the failure is a network one, which is how the
// test knows the message left the gate rather than being stopped by it.
func TestTelegramNotifySendsWhenConsentAllows(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org, user := seedTelegramLink(t, db, "telegram-allowed")

	gate := wiring.NewConsentGate(consent.NewService(db, audit.New(db)))
	svc := notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-token"},
		db, slog.Default(), audit.New(db), nil, gate)

	err := svc.NotifyUser(ctx, org, user, "a notification")
	if errors.Is(err, notify.ErrConsentBlocked) {
		t.Fatalf("an allowed notification was blocked on consent: %v", err)
	}
}
