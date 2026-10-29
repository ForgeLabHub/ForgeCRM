//go:build integration

// These tests are the evidence for migration 0008: a delivery that exhausts its
// attempts is dead-lettered exactly once and audited, and a replay re-arms it
// without erasing how many times the endpoint has already failed.
//
// They run against a real PostgreSQL server because every property under test is a
// database property — a partial UPDATE's affected-row count, a CHECK constraint,
// an append-only audit table — and a fake would satisfy all of them by
// construction, which is exactly the failure mode that makes a mock test worthless
// here.
package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// deadLetterFixture builds a subscription whose target the SSRF guard refuses, so
// every attempt fails without real network traffic, plus one pending delivery.
func deadLetterFixture(t *testing.T, maxAttempts int) (*events.DeliverySender, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "dead-letter-"+uuid.NewString()[:8])

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// 127.0.0.1 is a blocked range, so the production sender refuses it and the
	// attempt fails deterministically.
	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions
			(organization_id, name, target_url, event_types, secret_encrypted, enabled, max_attempts)
		VALUES ($1, 'sub', 'http://127.0.0.1:1/hook', '{}', $2, true, $3) RETURNING id`,
		org, enc, maxAttempts).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var deliveryID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, event_type, payload, status)
		VALUES ($1, $2, 'contact.created', '{"id":"1"}'::jsonb, 'pending') RETURNING id`,
		subID, org).Scan(&deliveryID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	sender := events.NewDeliverySender(db, audit.New(db), keyring)
	return sender, org, subID, deliveryID
}

// insertPrincipalUser inserts a real user and returns a principal for it.
//
// The principal's user id must reference an existing row because audit_log has a
// foreign key to users; a synthetic uuid would make the audit insert fail, which is
// the schema correctly refusing to attribute an action to a user who does not
// exist.
func insertPrincipalUser(t *testing.T, db *database.DB, org uuid.UUID) *rbac.Principal {
	t.Helper()
	var userID uuid.UUID
	if err := db.Pool().QueryRow(context.Background(), `
INSERT INTO users (email, password_hash, display_name, status)
VALUES ($1, 'x', 'Owner', 'active') RETURNING id`,
		"owner-"+uuid.NewString()[:8]+"@example.test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return rbac.NewPrincipal(userID, "owner@example.test", "Owner", org, nil)
}

// TestDeliveryDeadLetterIsAuditedOnce is the core 0008 regression.
//
// A delivery that exhausts its attempts must reach the dead state and leave
// exactly one audit entry for that transition, no matter how many times the
// sender observes the exhausted state. Before 0008 there was no audit entry at
// all, so an operator had no durable record that a customer's endpoint had
// stopped receiving events.
func TestDeliveryDeadLetterIsAuditedOnce(t *testing.T) {
	sender, org, _, deliveryID := deadLetterFixture(t, 2)
	db := connectOrSkip(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := db.Pool().Exec(ctx,
			`UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
			t.Fatalf("reset next_attempt_at: %v", err)
		}
		d, err := sender.ClaimDelivery(ctx, "sender", time.Minute)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if d == nil {
			break
		}
		if err := sender.SendDelivery(ctx, d.ID); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		var status string
		if err := db.Pool().QueryRow(ctx,
			`SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status == "dead" {
			break
		}
	}

	var status string
	var deadAt *time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT status, dead_lettered_at FROM webhook_deliveries WHERE id = $1`, deliveryID).
		Scan(&status, &deadAt); err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if status != "dead" {
		t.Fatalf("status = %q, want dead", status)
	}
	if deadAt == nil {
		t.Fatal("dead_lettered_at is NULL; the terminal transition was not recorded")
	}

	// Exactly one audit entry, and it names the delivery. The count is the point:
	// an alert built on this value must fire once per failure, not once per attempt.
	var entries int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1 AND entity_id = $2`,
		string(audit.ActionWebhookDeadLettered), deliveryID.String()).Scan(&entries); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if entries != 1 {
		t.Fatalf("audit entries = %d, want exactly 1", entries)
	}

	// The audit entry must be attributable to the organization, so a tenant-scoped
	// audit view shows it to the right operator and no one else.
	var auditOrg *uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		SELECT organization_id FROM audit_log
		WHERE action = $1 AND entity_id = $2`,
		string(audit.ActionWebhookDeadLettered), deliveryID.String()).Scan(&auditOrg); err != nil {
		t.Fatalf("read audit organization: %v", err)
	}
	if auditOrg == nil || *auditOrg != org {
		t.Fatalf("audit organization = %v, want %v", auditOrg, org)
	}
}

// TestReplayGrantsFreshBudgetAndCountsReplays proves a replay re-arms a dead
// delivery, is reflected in replay_count, and clears the dead-letter marker, while
// leaving the operator's ability to see that the endpoint has failed before intact.
func TestReplayGrantsFreshBudgetAndCountsReplays(t *testing.T) {
	sender, org, subID, deliveryID := deadLetterFixture(t, 1)
	db := connectOrSkip(t)
	ctx := context.Background()

	// One attempt with max_attempts=1 is enough to kill it.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("reset next_attempt_at: %v", err)
	}
	d, err := sender.ClaimDelivery(ctx, "sender", time.Minute)
	if err != nil || d == nil {
		t.Fatalf("claim: delivery=%v err=%v", d, err)
	}
	if err := sender.SendDelivery(ctx, d.ID); err != nil {
		t.Fatalf("send: %v", err)
	}

	var status string
	if err := db.Pool().QueryRow(ctx,
		`SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "dead" {
		t.Fatalf("status = %q, want dead", status)
	}

	// The organization owner replays it.
	gw := events.NewGateway(db, audit.New(db))
	principal := insertPrincipalUser(t, db, org)
	if err := gw.Replay(ctx, principal, subID, deliveryID); err != nil {
		t.Fatalf("replay: %v", err)
	}

	var (
		replayedStatus string
		replayCount    int
		attempts       int
		deadAt         *time.Time
	)
	if err := db.Pool().QueryRow(ctx, `
		SELECT status, replay_count, attempts, dead_lettered_at
		FROM webhook_deliveries WHERE id = $1`, deliveryID).
		Scan(&replayedStatus, &replayCount, &attempts, &deadAt); err != nil {
		t.Fatalf("read replayed delivery: %v", err)
	}
	if replayedStatus != "pending" {
		t.Fatalf("status after replay = %q, want pending", replayedStatus)
	}
	if replayCount != 1 {
		t.Fatalf("replay_count = %d, want 1", replayCount)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (a replay grants a fresh window)", attempts)
	}
	if deadAt != nil {
		t.Fatalf("dead_lettered_at = %v, want NULL after a replay", deadAt)
	}

	// The replay is audited, which is what lets an operator tell a customer "we
	// re-sent it" with a record behind the claim.
	var replayEntries int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1 AND entity_id = $2`,
		string(audit.ActionWebhookReplayed), deliveryID.String()).Scan(&replayEntries); err != nil {
		t.Fatalf("count replay audit entries: %v", err)
	}
	if replayEntries != 1 {
		t.Fatalf("replay audit entries = %d, want 1", replayEntries)
	}
}

// TestReplayIsScopedToTheOrganization proves a caller cannot re-arm another
// organization's delivery by guessing its id.
func TestReplayIsScopedToTheOrganization(t *testing.T) {
	sender, _, subID, deliveryID := deadLetterFixture(t, 1)
	db := connectOrSkip(t)
	ctx := context.Background()

	// Kill it so there is a real row to attempt to replay.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("reset next_attempt_at: %v", err)
	}
	d, err := sender.ClaimDelivery(ctx, "sender", time.Minute)
	if err != nil || d == nil {
		t.Fatalf("claim: delivery=%v err=%v", d, err)
	}
	if err := sender.SendDelivery(ctx, d.ID); err != nil {
		t.Fatalf("send: %v", err)
	}

	// A principal from a different organization.
	otherOrg := insertOrganization(t, db, "other-org-"+uuid.NewString()[:8])
	gw := events.NewGateway(db, audit.New(db))
	other := insertPrincipalUser(t, db, otherOrg)
	if err := gw.Replay(ctx, other, subID, deliveryID); err == nil {
		t.Fatal("a foreign organization replayed another organization's delivery")
	}

	// And the row is untouched: a failed authorization must have no side effect.
	var status string
	if err := db.Pool().QueryRow(ctx,
		`SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "dead" {
		t.Fatalf("status = %q after a refused replay, want dead (unchanged)", status)
	}
}

// TestReplayOfPendingDeliveryIsRefused proves replaying a delivery that is already
// in flight does not reset its attempt count a second time. Resetting it would
// hand the sender a budget the operator did not intend and make two concurrent
// replays of one delivery behave differently from one.
func TestReplayOfPendingDeliveryIsRefused(t *testing.T) {
	_, org, subID, deliveryID := deadLetterFixture(t, 5)
	db := connectOrSkip(t)
	ctx := context.Background()

	// The fixture row starts pending. A first replay must be refused.
	gw := events.NewGateway(db, audit.New(db))
	principal := insertPrincipalUser(t, db, org)
	if err := gw.Replay(ctx, principal, subID, deliveryID); err == nil {
		t.Fatal("a pending delivery was replayed; it must be refused rather than reset")
	}

	var replayCount int
	if err := db.Pool().QueryRow(ctx,
		`SELECT replay_count FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&replayCount); err != nil {
		t.Fatalf("read replay_count: %v", err)
	}
	if replayCount != 0 {
		t.Fatalf("replay_count = %d after a refused replay, want 0", replayCount)
	}
}

// ---------------------------------------------------------------------------
// Worker recovery: a claim abandoned by a crashed sender
// ---------------------------------------------------------------------------

// TestClaimRecoversDeliveryAbandonedByCrashedWorker is the recovery half of the
// claim design, and it had no coverage.
//
// A worker claims a delivery by moving it to 'delivering' and then dies before it
// can send, so the row is left claimed and nothing in the send path will ever pick
// it up again. ClaimDelivery's second eligibility clause — a 'delivering' row whose
// lock is older than the lease — is the only thing that recovers it. Without that
// clause the delivery is stranded forever: it is not pending, not failed, and no
// worker will look at it. The test drives a real claim, then ages the lock and
// asserts a later claim takes the same row back.
func TestClaimRecoversDeliveryAbandonedByCrashedWorker(t *testing.T) {
	sender, _, _, deliveryID := deadLetterFixture(t, 5)
	db := connectOrSkip(t)
	ctx := context.Background()

	// A worker claims the delivery, as the send loop does, and then "crashes":
	// nothing records an outcome, so the row stays 'delivering'.
	claimed, err := sender.ClaimDelivery(ctx, "crashed-worker", time.Minute)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed == nil || claimed.ID != deliveryID {
		t.Fatalf("first claim took %v, want the fixture delivery %v", claimed, deliveryID)
	}

	var (
		status   string
		lockedBy *string
	)
	if err := db.Pool().QueryRow(ctx,
		`SELECT status, locked_by FROM webhook_deliveries WHERE id = $1`, deliveryID).
		Scan(&status, &lockedBy); err != nil {
		t.Fatalf("read after the claim: %v", err)
	}
	if status != "delivering" {
		t.Fatalf("status after the claim = %q, want delivering", status)
	}

	// While the lease is live, a second worker must not take the row: this is the
	// exclusivity the claim exists to provide, and it is what stops two workers
	// POSTing the same delivery.
	if again, err := sender.ClaimDelivery(ctx, "second-worker", time.Minute); err != nil {
		t.Fatalf("claim while the lease is live: %v", err)
	} else if again != nil {
		t.Fatalf("a live claim was stolen by another worker: %v", again)
	}

	// The lease expires with no outcome recorded. Age the lock past the lease the
	// next claim uses, then claim again.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE webhook_deliveries SET locked_at = now() - interval '10 minutes' WHERE id = $1`,
		deliveryID); err != nil {
		t.Fatalf("age the lock: %v", err)
	}

	recovered, err := sender.ClaimDelivery(ctx, "recovery-worker", time.Minute)
	if err != nil {
		t.Fatalf("claim after the lease expired: %v", err)
	}
	if recovered == nil {
		t.Fatal("the abandoned delivery was never reclaimed; a crashed worker strands it forever")
	}
	if recovered.ID != deliveryID {
		t.Fatalf("reclaimed %v, want the abandoned delivery %v", recovered.ID, deliveryID)
	}
	if recovered.Status != "delivering" {
		t.Fatalf("reclaimed status = %q, want delivering", recovered.Status)
	}
	if err := db.Pool().QueryRow(ctx,
		`SELECT locked_by FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&lockedBy); err != nil {
		t.Fatalf("read locked_by: %v", err)
	}
	if lockedBy == nil || *lockedBy != "recovery-worker" {
		t.Fatalf("locked_by = %v, want recovery-worker: the lease was not transferred", lockedBy)
	}
}

// TestReleaseStaleDeliveriesRecoversOnStartup is the other recovery path: the
// worker calls ReleaseStaleDeliveries at start so an unclean shutdown does not
// leave rows claimed. It must return only claims older than the cutoff — a row a
// healthy worker is holding right now must be left alone — and the rows it
// releases must be immediately claimable.
func TestReleaseStaleDeliveriesRecoversOnStartup(t *testing.T) {
	sender, _, _, staleID := deadLetterFixture(t, 5)
	db := connectOrSkip(t)
	ctx := context.Background()

	org := insertOrganization(t, db, "release-stale-"+uuid.NewString()[:8])
	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions
			(organization_id, name, target_url, event_types, secret_encrypted, enabled, max_attempts)
		VALUES ($1, 'sub', 'http://127.0.0.1:1/hook', '{}', $2, true, 5) RETURNING id`,
		org, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// A live claim a healthy worker holds now, and a stale claim left by a crash.
	var liveID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_deliveries
			(subscription_id, organization_id, event_type, payload, status, locked_at, locked_by)
		VALUES ($1, $2, 'contact.created', '{"id":"1"}'::jsonb, 'delivering', now(), 'live-worker')
		RETURNING id`, subID, org).Scan(&liveID); err != nil {
		t.Fatalf("insert live claim: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'delivering', locked_at = now() - interval '1 hour', locked_by = 'dead-worker'
		WHERE id = $1`, staleID); err != nil {
		t.Fatalf("mark the stale claim: %v", err)
	}

	n, err := sender.ReleaseStaleDeliveries(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("release stale: %v", err)
	}
	if n != 1 {
		t.Fatalf("released %d deliveries, want 1: only the stale claim may be released", n)
	}

	var staleStatus string
	var staleLockedBy *string
	if err := db.Pool().QueryRow(ctx,
		`SELECT status, locked_by FROM webhook_deliveries WHERE id = $1`, staleID).
		Scan(&staleStatus, &staleLockedBy); err != nil {
		t.Fatalf("read the released row: %v", err)
	}
	if staleStatus != "pending" {
		t.Fatalf("released status = %q, want pending", staleStatus)
	}
	if staleLockedBy != nil {
		t.Fatalf("released locked_by = %v, want NULL", staleLockedBy)
	}

	// The live claim must be untouched: releasing it would let a second worker
	// send a delivery that is being sent right now.
	var liveStatus string
	var liveLockedBy *string
	if err := db.Pool().QueryRow(ctx,
		`SELECT status, locked_by FROM webhook_deliveries WHERE id = $1`, liveID).
		Scan(&liveStatus, &liveLockedBy); err != nil {
		t.Fatalf("read the live row: %v", err)
	}
	if liveStatus != "delivering" || liveLockedBy == nil || *liveLockedBy != "live-worker" {
		t.Fatalf("live claim was disturbed: status=%q locked_by=%v", liveStatus, liveLockedBy)
	}

	// The released row is immediately claimable, which is what makes the recovery
	// real rather than cosmetic.
	recovered, err := sender.ClaimDelivery(ctx, "restarted-worker", time.Minute)
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if recovered == nil || recovered.ID != staleID {
		t.Fatalf("claim after release = %v, want the released delivery %v", recovered, staleID)
	}
}
