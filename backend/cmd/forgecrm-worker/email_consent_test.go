//go:build integration

// Integration test for the worker's queued-email handler and consent.
//
// This is the test for the bypass the transport gate closes. The handler that
// drains "email.send" jobs called the email transport directly, so a message could
// leave the platform through the queue without the consent policy ever being asked,
// even though the manual endpoint asked it. The property under test is therefore
// about the worker's own path, not about the transport in isolation: a job whose
// recipient has no consent for a gated purpose must not be sent, and the refusal
// must appear in the audit trail.
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./cmd/forgecrm-worker/...
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/wiring"
)

// emailRegistry builds the registry with the real consent policy and the real
// transport, so the test exercises the same gate the worker assembles in main.
func emailRegistry(t *testing.T, db *database.DB, enabled bool) *handlerRegistry {
	t.Helper()
	gate := wiring.NewConsentGate(consent.NewService(db, audit.New(db)))
	return &handlerRegistry{
		db:    db,
		log:   slog.New(slog.NewTextHandler(discard{}, nil)),
		email: notify.NewEmailService(&config.EmailConfig{Enabled: enabled}, db, slog.New(slog.NewTextHandler(discard{}, nil)), gate),
	}
}

// emailJob builds an "email.send" job payload the way a producer would.
func emailJob(t *testing.T, orgID uuid.UUID, to, purpose string) *jobs.Job {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"to":              []string{to},
		"subject":         "Bulk mail",
		"html_body":       "<p>hello</p>",
		"template":        "manual",
		"organization_id": orgID,
		"purpose":         purpose,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &jobs.Job{ID: uuid.New(), Kind: jobs.KindEmailSend, Payload: payload}
}

// TestWorkerQueuedEmailHonoursConsent is the regression guard for the bypass.
//
// A marketing message is queued for an address with no consent on record. Before
// the gate lived in the transport, this job reached the SMTP client; the send is
// now refused. SMTP is switched on in the registry on purpose, so the test would
// pass vacuously if the transport were off: the refusal must come from the policy,
// not from the transport being unavailable.
func TestWorkerQueuedEmailHonoursConsent(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrg(t, db, "worker-email-consent")

	reg := emailRegistry(t, db, true)
	job := emailJob(t, org, "no-consent@worker.test", "marketing")

	if err := reg.handle(ctx, nil, job); err != nil {
		t.Fatalf("a consent-blocked queued send returned an error, which would retry forever: %v", err)
	}

	// No send log row: the message was refused before it was ever attempted.
	var queued int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM email_log WHERE organization_id = $1`, org).Scan(&queued); err != nil {
		t.Fatalf("count email_log: %v", err)
	}
	if queued != 0 {
		t.Fatalf("a blocked send wrote %d email_log rows, want none", queued)
	}

	// The refusal is in the audit trail, so an operator can see it happened.
	var blocked int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE organization_id = $1 AND action = $2`,
		org, string(audit.ActionConsentBlockedSend)).Scan(&blocked); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if blocked == 0 {
		t.Fatal("the blocked send was not recorded in the audit trail")
	}
}

// TestWorkerQueuedEmailAllowsTransactional checks the gate is specific rather than
// a blanket refusal: a transactional message with no marketing record still reaches
// the transport, so the queue remains useful for the mail the platform must send.
func TestWorkerQueuedEmailAllowsTransactional(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrg(t, db, "worker-email-transactional")

	// The transport is off, so a message that passes consent stops at the transport
	// with a log line and no error. The assertion is about consent letting it through,
	// which the absence of a consent-blocked audit row shows.
	reg := emailRegistry(t, db, false)
	job := emailJob(t, org, "receipt@worker.test", "transactional")

	if err := reg.handle(ctx, nil, job); err != nil {
		t.Fatalf("a transactional queued send returned an error: %v", err)
	}

	var blocked int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE organization_id = $1 AND action = $2`,
		org, string(audit.ActionConsentBlockedSend)).Scan(&blocked); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if blocked != 0 {
		t.Fatalf("a transactional send was blocked %d times, want 0", blocked)
	}
}
