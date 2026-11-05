//go:build integration

// Integration test for the Telegram assistant's per-account notification
// preferences and self-service unlink, against the real schema.
//
// Preferences are the person's own choice of which notification types reach their
// Telegram account. The property under test is that a preference is per account,
// survives unlink/relink, defaults to enabled, and that a delivery-path lookup for
// an unlinked user keeps the default rather than silencing anything. Unlink is
// tested because it is a destructive action a person initiates from the bot.
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/notify"
)

func newTelegramService(t *testing.T, db *database.DB) *notify.TelegramService {
	t.Helper()
	return notify.NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-token"},
		db, slog.Default(), audit.New(db), nil, nil)
}

// TestTelegramNotificationPreferences checks the default, an explicit mute, the
// round-trip through the delivery-path lookup, and that an unknown kind is refused.
func TestTelegramNotificationPreferences(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()
	org, user := seedTelegramLink(t, db, "telegram-prefs")
	svc := newTelegramService(t, db)
	chatID := int64(len("telegram-prefs") + 100000)

	prefs, err := svc.NotificationPreferences(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range notify.NotificationKinds {
		if !prefs[kind] {
			t.Fatalf("kind %q should default to enabled", kind)
		}
	}

	if err := svc.SetNotificationPreference(ctx, chatID, notify.KindOverdue, false); err != nil {
		t.Fatal(err)
	}
	prefs, err = svc.NotificationPreferences(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if prefs[notify.KindOverdue] {
		t.Fatal("overdue should be muted")
	}
	if !prefs[notify.KindAssignment] {
		t.Fatal("muting one kind must not mute another")
	}

	enabled, err := svc.NotificationEnabled(ctx, org, user, notify.KindOverdue)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("delivery lookup should report overdue as muted")
	}
	enabled, err = svc.NotificationEnabled(ctx, org, user, notify.KindAssignment)
	if err != nil || !enabled {
		t.Fatalf("assignment should stay enabled: %v %v", enabled, err)
	}

	if err := svc.SetNotificationPreference(ctx, chatID, notify.NotificationKind("bogus"), false); err == nil {
		t.Fatal("unknown kind must be refused")
	}
}

// TestTelegramNotificationEnabledDefaultsForUnlinkedUser verifies that the
// delivery-path lookup does not silence a user who has no link (and therefore no
// preferences): the default is enabled, so a missing link never mutes anyone.
func TestTelegramNotificationEnabledDefaultsForUnlinkedUser(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()
	svc := newTelegramService(t, db)
	enabled, err := svc.NotificationEnabled(ctx, uuid.New(), uuid.New(), notify.KindAssignment)
	if err != nil || !enabled {
		t.Fatalf("unlinked user should default to enabled: %v %v", enabled, err)
	}
}

// TestTelegramPreferencesSurviveRelink checks that a preference is keyed by the
// Telegram account, so it is kept across an unlink and relink.
func TestTelegramPreferencesSurviveRelink(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()
	_, _ = seedTelegramLink(t, db, "telegram-relink")
	svc := newTelegramService(t, db)
	chatID := int64(len("telegram-relink") + 100000)

	if err := svc.SetNotificationPreference(ctx, chatID, notify.KindCompleted, false); err != nil {
		t.Fatal(err)
	}
	principal, err := svc.ResolveUser(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlink(ctx, principal); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveUser(ctx, chatID); err == nil {
		t.Fatal("a revoked link must not resolve")
	}
	prefs, err := svc.NotificationPreferences(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if prefs[notify.KindCompleted] {
		t.Fatal("a preference must survive an unlink")
	}
	// Relink the same account and confirm the preference is still in force.
	code, err := svc.CreateLinkCode(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Link(ctx, notify.LinkRequest{Code: code, TelegramUserID: chatID}); err != nil {
		t.Fatal(err)
	}
	prefs, err = svc.NotificationPreferences(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if prefs[notify.KindCompleted] {
		t.Fatal("a preference must survive a relink")
	}
}

// TestTelegramUnlinkRevokesAccess checks that unlinking stops the account from
// resolving to a principal, so the bot can no longer act on its behalf.
func TestTelegramUnlinkRevokesAccess(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()
	_, _ = seedTelegramLink(t, db, "telegram-unlink")
	svc := newTelegramService(t, db)
	chatID := int64(len("telegram-unlink") + 100000)

	principal, err := svc.ResolveUser(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlink(ctx, principal); err != nil {
		t.Fatal(err)
	}
	_, err = svc.ResolveUser(ctx, chatID)
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("unlinked account should be forbidden, got %v", err)
	}
}
