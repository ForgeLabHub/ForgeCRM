//go:build integration

// These tests are the evidence for trace propagation across the webhook boundary:
// a delivery that leaves the platform carries a W3C `traceparent` header, and a
// retry of the same delivery keeps the same trace id so the whole history of one
// unit of work is followable in a tracing system.
//
// They run against a real PostgreSQL server because the claim-and-send loop is a
// database property (an atomic claim, a durable delivery id) and against a real
// local HTTP server because the header is only meaningful if it actually reaches
// the wire.
package testsupport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/tracectx"
)

// traceSubscriber is a local HTTP server that records the headers of every request
// it receives, so a test can assert on what the sender actually sent.
type traceSubscriber struct {
	mu      sync.Mutex
	headers []http.Header
	server  *httptest.Server
}

func newTraceSubscriber(t *testing.T) *traceSubscriber {
	t.Helper()
	ts := &traceSubscriber{}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.headers = append(ts.headers, r.Header.Clone())
		ts.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.server.Close)
	return ts
}

func (ts *traceSubscriber) count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.headers)
}

func (ts *traceSubscriber) last() http.Header {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.headers) == 0 {
		return nil
	}
	return ts.headers[len(ts.headers)-1]
}

// traceDeliveryFixture creates a subscription pointed at a loopback test server,
// plus one pending delivery, and returns a sender that delivers to it.
func traceDeliveryFixture(t *testing.T) (*events.DeliverySender, uuid.UUID, uuid.UUID, *traceSubscriber) {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "trace-"+uuid.NewString()[:8])

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	subscriber := newTraceSubscriber(t)
	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions
			(organization_id, name, target_url, event_types, secret_encrypted, enabled, max_attempts)
		VALUES ($1, 'sub', $2, '{}', $3, true, 3) RETURNING id`,
		org, subscriber.server.URL+"/hook", enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var deliveryID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, event_type, payload, status)
		VALUES ($1, $2, 'contact.created', '{"id":"1"}'::jsonb, 'pending') RETURNING id`,
		subID, org).Scan(&deliveryID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	// The production sender refuses loopback, so the test injects the unguarded
	// poster it exposes for exactly this case.
	sender := events.NewDeliverySenderWithPoster(db, audit.New(db), keyring,
		events.NewPlainPoster(5*time.Second))
	return sender, org, deliveryID, subscriber
}

// TestDeliveryCarriesATraceParent proves a delivery leaves the platform with a
// valid W3C traceparent, so a subscriber that records it can be joined to our
// own logs for the same operation.
func TestDeliveryCarriesATraceParent(t *testing.T) {
	sender, _, deliveryID, subscriber := traceDeliveryFixture(t)

	if err := sender.SendDelivery(context.Background(), deliveryID); err != nil {
		t.Fatalf("send: %v", err)
	}
	if subscriber.count() != 1 {
		t.Fatalf("subscriber received %d requests, want 1", subscriber.count())
	}
	header := subscriber.last().Get(tracectx.Header)
	if header == "" {
		t.Fatal("the delivery carried no traceparent header")
	}
	if _, ok := tracectx.Parse(header); !ok {
		t.Fatalf("the delivered traceparent is not a valid W3C value: %q", header)
	}
}

// TestRetriedDeliveryKeepsTheSameTraceId proves the seed is durable: a delivery
// re-sent after a failure must carry the same trace id, so the first attempt and
// the retry are one operation in a tracing system rather than two unrelated ones.
func TestRetriedDeliveryKeepsTheSameTraceId(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "trace-retry-"+uuid.NewString()[:8])

	keyring, err := secrets.NewKeyring("test-platform-secret-key-32-bytes")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	enc, err := keyring.Encrypt([]byte("subscriber-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// The first attempt fails (loopback refused by the guarded client), the second
	// succeeds against a real local server.
	var subID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_subscriptions
			(organization_id, name, target_url, event_types, secret_encrypted, enabled, max_attempts)
		VALUES ($1, 'sub', 'http://127.0.0.1:1/hook', '{}', $2, true, 3) RETURNING id`,
		org, enc).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	var deliveryID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, event_type, payload, status)
		VALUES ($1, $2, 'contact.created', '{"id":"1"}'::jsonb, 'pending') RETURNING id`,
		subID, org).Scan(&deliveryID); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	// First attempt through the guarded sender: it fails and re-arms the row.
	failing := events.NewDeliverySender(db, audit.New(db), keyring)
	if err := failing.SendDelivery(ctx, deliveryID); err != nil {
		t.Fatalf("first send: %v", err)
	}

	// Now point the same delivery at a live server and send again through the
	// unguarded poster, which is the only thing that changed.
	subscriber := newTraceSubscriber(t)
	if _, err := db.Pool().Exec(ctx,
		`UPDATE webhook_subscriptions SET target_url = $1 WHERE id = $2`,
		subscriber.server.URL+"/hook", subID); err != nil {
		t.Fatalf("retarget: %v", err)
	}
	// Make it due immediately.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("make due: %v", err)
	}
	sending := events.NewDeliverySenderWithPoster(db, audit.New(db), keyring,
		events.NewPlainPoster(5*time.Second))
	if err := sending.SendDelivery(ctx, deliveryID); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if subscriber.count() != 1 {
		t.Fatalf("subscriber received %d requests, want 1", subscriber.count())
	}

	// The trace id is a pure function of the delivery id, so the second attempt's
	// trace id must equal the first's.
	header := subscriber.last().Get(tracectx.Header)
	got, ok := tracectx.Parse(header)
	if !ok {
		t.Fatalf("the retried traceparent is not valid: %q", header)
	}
	want := tracectx.FromSeed(deliveryID.String()).TraceID
	if got.TraceID != want {
		t.Errorf("retry trace id: got %q, want %q (the id seeded from the delivery)", got.TraceID, want)
	}
}
