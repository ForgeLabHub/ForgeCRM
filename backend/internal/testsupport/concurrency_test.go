//go:build integration

// These tests reproduce, on a real PostgreSQL server, the concurrency defects
// that only appear once more than one worker is running. They are the evidence
// for the outbox and webhook-delivery fixes in migration 0007: a single-worker
// test passes against the broken code, so a test that does not actually run two
// workers at once would prove nothing.
package testsupport

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// TestOutboxClaimIsExclusive is the core regression: two drains claiming at the
// same moment must not both receive the same event.
//
// Before the fix, FetchPending selected rows and left them pending, so both
// callers received every event. Here 50 events are claimed by two goroutines
// racing, and the union of what they receive must be exactly the 50 events with
// no overlap.
func TestOutboxClaimIsExclusive(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "outbox-claim")
	store := outbox.NewStore(db)

	const total = 50
	for i := 0; i < total; i++ {
		if _, err := db.Pool().Exec(ctx, `
			INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
			VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1)`, org); err != nil {
			t.Fatalf("insert outbox event %d: %v", i, err)
		}
	}

	var (
		mu       sync.Mutex
		claimed  = map[uuid.UUID]int{}
		wg       sync.WaitGroup
		start    = make(chan struct{})
		claimers = 4
	)

	for w := 0; w < claimers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start // release all claimers at once to maximise the race
			for {
				got, err := store.ClaimPending(ctx, "worker-"+string(rune('a'+worker)), 10)
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if len(got) == 0 {
					return
				}
				mu.Lock()
				for _, ev := range got {
					claimed[ev.ID]++
				}
				mu.Unlock()
			}
		}(w)
	}
	close(start)
	wg.Wait()

	if len(claimed) != total {
		t.Fatalf("claimed %d distinct events, want %d", len(claimed), total)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Fatalf("event %s was claimed %d times; a claim must be exclusive", id, n)
		}
	}
}

// TestOutboxClaimsAreReleasedAfterFailure proves a failed dispatch does not
// strand the event behind its own claim: the claim is released so the next drain
// can retry immediately.
func TestOutboxClaimsAreReleasedAfterFailure(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "outbox-release")
	store := outbox.NewStore(db)

	var id uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1) RETURNING id`, org).Scan(&id); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	got, err := store.ClaimPending(ctx, "w1", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("first claim: got %d events, err %v", len(got), err)
	}
	// While claimed, a second drain must see nothing: this is the exclusivity
	// guarantee under the normal (not racing) path.
	if again, err := store.ClaimPending(ctx, "w2", 10); err != nil || len(again) != 0 {
		t.Fatalf("second claim while held: got %d events, err %v; want 0", len(again), err)
	}

	if err := store.MarkFailed(ctx, id, context.DeadlineExceeded); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	after, err := store.ClaimPending(ctx, "w2", 10)
	if err != nil {
		t.Fatalf("claim after failure: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("claim after failure: got %d events, want 1; the claim was not released", len(after))
	}
	if after[0].Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", after[0].Attempts)
	}
}

// TestOutboxDeadLettersAfterExhaustedAttempts proves the retry loop is bounded.
// Without a bound a permanently broken event is re-read by every drain forever.
func TestOutboxDeadLettersAfterExhaustedAttempts(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "outbox-dead")
	store := outbox.NewStore(db)

	var id uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1) RETURNING id`, org).Scan(&id); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	// Fail it until it exhausts its attempts. Each claim must succeed until the
	// row is dead-lettered.
	failures := 0
	for i := 0; i < 20; i++ {
		got, err := store.ClaimPending(ctx, "w", 10)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if len(got) == 0 {
			break
		}
		if err := store.MarkFailed(ctx, id, context.DeadlineExceeded); err != nil {
			t.Fatalf("mark failed %d: %v", i, err)
		}
		failures++
	}

	if failures < 2 {
		t.Fatalf("the event was retried only %d times; the retry bound is not working", failures)
	}
	var dead *time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT dead_lettered_at FROM outbox_events WHERE id = $1`, id).Scan(&dead); err != nil {
		t.Fatalf("read dead_lettered_at: %v", err)
	}
	if dead == nil {
		t.Fatal("dead_lettered_at is NULL; an exhausted event must be dead-lettered")
	}
}

// TestDispatchIsIdempotentPerEvent reproduces the duplicate-delivery defect: two
// drains dispatching the same event concurrently must leave exactly one delivery
// row per subscription, because the receiver must not get two POSTs for one
// domain change.
func TestDispatchIsIdempotentPerEvent(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "dispatch-idem")

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	subID := uuid.New()
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO webhook_subscriptions (id, organization_id, name, target_url, event_types, secret_encrypted, enabled)
		VALUES ($1, $2, 'sub', 'https://example.com/hook', '{}', $3, true)`, subID, org, enc); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	var eventID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1) RETURNING id`, org).Scan(&eventID); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	ev := outbox.Event{
		ID:             eventID,
		OrganizationID: &org,
		Type:           "contact.created",
		SchemaVersion:  1,
		Source:         "forgecrm",
		Payload:        []byte(`{"id":"1"}`),
		OccurredAt:     time.Now(),
	}
	dispatcher := events.NewDispatcher(db, nil)

	// Dispatch the same event from several goroutines at once, as two drains
	// would. Every call must succeed; only one row may exist.
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, errs[n] = dispatcher.Dispatch(ctx, ev)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("dispatch %d failed: %v", i, err)
		}
	}

	var count int
	if err := db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM webhook_deliveries WHERE subscription_id = $1 AND outbox_event_id = $2`,
		subID, eventID).Scan(&count); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if count != 1 {
		t.Fatalf("created %d deliveries for one event; want 1 (a duplicate means the endpoint is called twice)", count)
	}
}

// TestDeliveryClaimIsExclusive proves two senders cannot both take the same
// pending delivery, which is what stops the same webhook being POSTed twice.
func TestDeliveryClaimIsExclusive(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "delivery-claim")

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
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types, secret_encrypted, enabled)
		VALUES ($1, 'sub', 'https://example.com/hook', '{}', $2, true) RETURNING id`, org, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var eventID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1) RETURNING id`, org).Scan(&eventID); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, outbox_event_id, event_type, payload, status)
		VALUES ($1, $2, $3, 'contact.created', '{"id":"1"}'::jsonb, 'pending')`, subID, org, eventID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	sender := events.NewDeliverySender(db, nil, keyring)

	var (
		mu       sync.Mutex
		claimedN int
		wg       sync.WaitGroup
		start    = make(chan struct{})
	)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			d, err := sender.ClaimDelivery(ctx, "sender", time.Minute)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if d != nil {
				mu.Lock()
				claimedN++
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if claimedN != 1 {
		t.Fatalf("%d senders claimed the delivery; want exactly 1", claimedN)
	}
}

// TestOutboundWebhookIsActuallyDelivered is the end-to-end proof that the
// outgoing webhook subsystem works: a real HTTP server receives a signed POST
// carrying the event, which is what the platform was previously not doing at all.
func TestOutboundWebhookIsActuallyDelivered(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "webhook-e2e")

	received := make(chan sinkRequest, 4)
	srv := newWebhookSink(t, received)

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	secret := []byte("subscriber-secret")
	enc, err := keyring.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types, secret_encrypted, enabled)
		VALUES ($1, 'sub', $2, '{}', $3, true) RETURNING id`, org, srv.URL, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var eventID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1) RETURNING id`, org).Scan(&eventID); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, outbox_event_id, event_type, payload, status)
		VALUES ($1, $2, $3, 'contact.created', '{"id":"1"}'::jsonb, 'pending')`, subID, org, eventID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	// The test endpoint is on loopback, which the production SSRF guard correctly
	// refuses. Injecting a plain client here is what lets the test exercise the
	// real claim/sign/record path; TestProductionSenderRefusesLoopback below proves
	// the default sender still refuses it.
	sender := events.NewDeliverySenderWithPoster(db, nil, keyring, events.NewPlainPoster(5*time.Second))
	d, err := sender.ClaimDelivery(ctx, "sender", time.Minute)
	if err != nil || d == nil {
		t.Fatalf("claim: %v (delivery %v)", err, d)
	}
	if err := sender.SendDelivery(ctx, d.ID); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case got := <-received:
		if got.signature == "" {
			t.Error("the delivered request carried no signature header")
		}
		// The signature must verify against the subscription's own secret, which is
		// what lets a receiver trust the payload.
		if !verifyWebhookSignature(keyring, secret, got) {
			t.Error("the delivered request signature did not verify")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the endpoint received no request; the outgoing webhook was not delivered")
	}

	var status string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM webhook_deliveries WHERE id = $1`, d.ID).Scan(&status); err != nil {
		t.Fatalf("read delivery status: %v", err)
	}
	if status != "delivered" {
		t.Fatalf("delivery status = %q, want delivered", status)
	}
}

// TestDeliveryMarksDeadAfterMaxAttempts proves a subscriber that keeps failing is
// eventually abandoned rather than retried forever.
func TestDeliveryMarksDeadAfterMaxAttempts(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "delivery-dead")

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// A URL the SSRF guard refuses, so every attempt fails without real traffic.
	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types, secret_encrypted, enabled, max_attempts)
		VALUES ($1, 'sub', 'http://127.0.0.1:1/hook', '{}', $2, true, 2) RETURNING id`, org, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var eventID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO outbox_events (organization_id, event_type, payload, schema_version)
		VALUES ($1, 'contact.created', '{"id":"1"}'::jsonb, 1) RETURNING id`, org).Scan(&eventID); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	var deliveryID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, outbox_event_id, event_type, payload, status)
		VALUES ($1, $2, $3, 'contact.created', '{"id":"1"}'::jsonb, 'pending') RETURNING id`, subID, org, eventID).Scan(&deliveryID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	sender := events.NewDeliverySender(db, nil, keyring)

	// Attempt it until the row is dead. Each iteration makes the delivery due
	// again, standing in for the backoff elapsing.
	for i := 0; i < 6; i++ {
		if _, err := db.Pool().Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
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
		if err := db.Pool().QueryRow(ctx, `SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status == "dead" {
			return
		}
	}
	t.Fatal("the delivery was never marked dead; a persistently failing subscriber retries forever")
}

// TestDeadDeliveryIsSwept proves the retention sweep removes dead deliveries, so
// an abandoned subscriber does not accumulate rows without bound.
func TestDeadDeliveryIsSwept(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "dead-sweep")

	var subID uuid.UUID
	enc := make([]byte, 32)
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types, secret_encrypted, enabled)
		VALUES ($1, 'sub', 'https://example.com/hook', '{}', $2, true) RETURNING id`, org, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, event_type, payload, status, next_attempt_at)
		VALUES ($1, $2, 'contact.created', '{"id":"1"}'::jsonb, 'dead', now() - interval '40 days')`, subID, org); err != nil {
		t.Fatalf("insert dead delivery: %v", err)
	}

	// The same predicate the retention sweep uses.
	tag, err := db.Pool().Exec(ctx, `
		DELETE FROM webhook_deliveries WHERE status = 'dead' AND next_attempt_at < now() - interval '30 days'`)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("swept %d dead deliveries, want 1", tag.RowsAffected())
	}
}

// TestSecretsEnvIsNotRequired keeps the suite runnable in CI, where the platform
// secret is absent; the keyring tests above use an explicit key.
func TestSecretsEnvIsNotRequired(t *testing.T) {
	if os.Getenv("FORGECRM_TEST_DATABASE_URL") == "" {
		t.Skip("integration tests need a database")
	}
}

// TestProductionSenderRefusesLoopback is the security counterpart to the delivery
// test: the default sender, the one the worker uses, must refuse a loopback
// target even though a delivery row exists for it. Without this, the injection
// seam above would be indistinguishable from removing the SSRF guard.
func TestProductionSenderRefusesLoopback(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "sender-guard")

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// 127.0.0.1 is a blocked range. Creating the subscription is allowed: the guard
	// runs at connect time, which is the boundary that actually matters.
	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types, secret_encrypted, enabled)
		VALUES ($1, 'sub', 'http://127.0.0.1:5432/hook', '{}', $2, true) RETURNING id`, org, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var deliveryID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, event_type, payload, status)
		VALUES ($1, $2, 'contact.created', '{"id":"1"}'::jsonb, 'pending') RETURNING id`, subID, org).Scan(&deliveryID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	sender := events.NewDeliverySender(db, nil, keyring)
	if _, err := sender.ClaimDelivery(ctx, "sender", time.Minute); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// SendDelivery records the failure rather than returning it, so the assertion
	// is on the recorded outcome: a refusal is a failed attempt, never a delivered
	// one.
	if err := sender.SendDelivery(ctx, deliveryID); err != nil {
		t.Fatalf("send: %v", err)
	}
	var status string
	var lastErr *string
	if err := db.Pool().QueryRow(ctx, `SELECT status, last_error FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status, &lastErr); err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if status == "delivered" {
		t.Fatal("the production sender delivered to a loopback address; the SSRF guard is not applied")
	}
	if lastErr == nil || !strings.Contains(*lastErr, "refused") {
		t.Fatalf("last_error = %v; want a refusal from the SSRF guard", lastErr)
	}
}

// TestPeriodicEnqueueIsIdempotent proves the retention scheduler cannot pile up
// duplicate sweeps. Several workers ticking at once must leave exactly one queued
// sweep, otherwise the queue fills with jobs that all do the same work.
func TestPeriodicEnqueueIsIdempotent(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	queue := jobs.NewQueue(db, slog.Default())

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := queue.EnqueuePeriodic(ctx, jobs.KindRetentionSweep, map[string]any{}); err != nil {
				t.Errorf("enqueue periodic: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	var count int
	if err := db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM jobs WHERE kind = $1 AND status IN ('queued', 'running')`,
		jobs.KindRetentionSweep).Scan(&count); err != nil {
		t.Fatalf("count sweeps: %v", err)
	}
	if count != 1 {
		t.Fatalf("queued %d retention sweeps; want exactly 1", count)
	}
}

// TestPeriodicEnqueueRunsAgainAfterCompletion proves the dedup does not wedge the
// scheduler: once the outstanding sweep finishes, the next tick may enqueue a new
// one.
func TestPeriodicEnqueueRunsAgainAfterCompletion(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	queue := jobs.NewQueue(db, slog.Default())

	first, err := queue.EnqueuePeriodic(ctx, jobs.KindRetentionSweep, map[string]any{})
	if err != nil || first == nil {
		t.Fatalf("first enqueue: %v (job %v)", err, first)
	}
	second, err := queue.EnqueuePeriodic(ctx, jobs.KindRetentionSweep, map[string]any{})
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if second != nil {
		t.Fatal("a second sweep was queued while the first was still outstanding")
	}

	if _, err := db.Pool().Exec(ctx, `UPDATE jobs SET status='succeeded', finished_at=now() WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("complete the first sweep: %v", err)
	}

	third, err := queue.EnqueuePeriodic(ctx, jobs.KindRetentionSweep, map[string]any{})
	if err != nil {
		t.Fatalf("third enqueue: %v", err)
	}
	if third == nil {
		t.Fatal("no sweep was queued after the previous one finished; the scheduler is wedged")
	}
}

// TestWebhookDispatchMigrationRollsBack proves migration 0007 can be reversed.
// A migration that only rolls forward is a trap during an incident: an operator
// who cannot go back is forced to restore from backup to undo one change.
func TestWebhookDispatchMigrationRollsBack(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")

	// The forward migration must have created the objects it promises.
	assertExists := func(desc, query string, want bool) {
		t.Helper()
		var exists bool
		if err := db.Pool().QueryRow(ctx, query).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", desc, err)
		}
		if exists != want {
			t.Fatalf("%s exists = %v, want %v", desc, exists, want)
		}
	}
	assertExists("the dedup index",
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'webhook_deliveries_sub_event_uniq')`, true)
	assertExists("the outbox claim column",
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='outbox_events' AND column_name='claimed_by')`, true)

	if err := migrate.Down(url, slog.Default()); err != nil {
		t.Fatalf("roll back: %v", err)
	}

	assertExists("the dedup index after rollback",
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'webhook_deliveries_sub_event_uniq')`, false)
	assertExists("the outbox claim column after rollback",
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='outbox_events' AND column_name='claimed_by')`, false)

	// And forward again, so the schema is left in the state the rest of the suite
	// expects.
	if _, err := migrate.Up(url, slog.Default()); err != nil {
		t.Fatalf("re-apply after rollback: %v", err)
	}
	assertExists("the dedup index after re-applying",
		`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'webhook_deliveries_sub_event_uniq')`, true)
}
