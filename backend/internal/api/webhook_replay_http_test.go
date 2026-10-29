//go:build integration

// HTTP-level coverage of the webhook replay path.
//
// TestWebhookLifecycleOverHTTP in webhook_lifecycle_test.go proves the happy path
// end to end. These tests cover the edges that path does not reach: what the
// replay response actually contains, how the lifetime replay counter behaves
// across several windows, whether the audit entry carries enough context to
// answer "who replayed what, and how many times", and the refusals — a delivery
// that is in flight, a delivery that belongs to another subscription, another
// organization, or a caller without the permission.
//
// They go through the HTTP stack rather than calling the service, because the
// failures worth catching here are wiring failures: a path parameter the handler
// forgets to read, a permission that is not applied, an audit entry the service
// writes but the endpoint never reaches.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/api/...
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// driveDeliveryToDead runs the real sender until the delivery exhausts its
// attempts and reaches the terminal dead state, the way the worker's send loop
// does. The target is the SSRF-blocked loopback address the fixture uses, so no
// network traffic is produced.
func (h *harness) driveDeliveryToDead(subID, deliveryID uuid.UUID) {
	h.t.Helper()
	ctx := context.Background()

	keyring, err := secrets.NewKeyring(h.cfg.SecretKey)
	if err != nil {
		h.t.Fatalf("keyring: %v", err)
	}
	sender := events.NewDeliverySender(h.db, audit.New(h.db), keyring)
	for i := 0; i < 8; i++ {
		if _, err := h.pool().Exec(ctx,
			`UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
			h.t.Fatalf("reset next_attempt_at: %v", err)
		}
		d, err := sender.ClaimDelivery(ctx, "test-sender", time.Minute)
		if err != nil {
			h.t.Fatalf("claim %d: %v", i, err)
		}
		if d == nil {
			break
		}
		if err := sender.SendDelivery(ctx, d.ID); err != nil {
			h.t.Fatalf("send %d: %v", i, err)
		}
	}

	var status string
	if err := h.pool().QueryRow(ctx,
		`SELECT status FROM webhook_deliveries WHERE id = $1`, deliveryID).Scan(&status); err != nil {
		h.t.Fatalf("read status: %v", err)
	}
	if status != "dead" {
		h.t.Fatalf("delivery status = %q, want dead; the fixture did not exhaust its attempts", status)
	}
}

// replay issues the replay request as the given actor and returns the recorder.
func (h *harness) replay(a *actor, subID, deliveryID uuid.UUID) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.do(a, http.MethodPost,
		"/api/v1/webhooks/"+subID.String()+"/deliveries/"+deliveryID.String()+"/replay", "")
}

// deliveryRow reads one delivery straight from the table. The API exposes the
// delivery list, but an invariant like "replay_count did not move" is a database
// fact, and reading it directly keeps the assertion independent of the list
// endpoint's projection.
func (h *harness) deliveryRow(deliveryID uuid.UUID) (status string, attempts, replayCount int, deadLettered bool) {
	h.t.Helper()
	var deadAt *time.Time
	if err := h.pool().QueryRow(context.Background(), `
		SELECT status, attempts, replay_count, dead_lettered_at
		FROM webhook_deliveries WHERE id = $1`, deliveryID).
		Scan(&status, &attempts, &replayCount, &deadAt); err != nil {
		h.t.Fatalf("read delivery row: %v", err)
	}
	return status, attempts, replayCount, deadAt != nil
}

// replayAuditEntries returns the replay audit entries for one delivery, as the
// audit API reports them, so the assertion is about what an operator can read.
func (h *harness) replayAuditEntries(a *actor, deliveryID uuid.UUID) []map[string]any {
	h.t.Helper()
	rec := h.do(a, http.MethodGet,
		"/api/v1/audit?action=webhooks.replayed&entity_id="+deliveryID.String()+"&limit=100", "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("list audit: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		h.t.Fatalf("decode audit: %v (body %s)", err, rec.Body.String())
	}
	return resp.Items
}

// TestWebhookReplayResponseAndHistoryOverHTTP pins the replay contract an
// operator and the web UI depend on: a successful replay returns 200 with a
// body, re-arms the same row rather than creating a new one, and preserves the
// identity of the delivery.
func TestWebhookReplayResponseAndHistoryOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Response")
	userID := h.seedUser("replay-owner@example.test", "Replay-Owner-2026!", "Replay Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("replay-owner@example.test", "Replay-Owner-2026!")

	subID := h.createWebhook(user, "Replay Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	items := h.deliveriesOf(user, subID)
	deliveryID, _ := uuid.Parse(items[0]["id"].(string))
	eventType := items[0]["event_type"]

	h.driveDeliveryToDead(subID, deliveryID)

	rec := h.replay(user, subID, deliveryID)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay: status %d, body %s", rec.Code, rec.Body.String())
	}
	// The response is JSON; a client that parses it must not be handed an empty
	// body or an error envelope on success.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("replay response is not JSON: %v (body %s)", err, rec.Body.String())
	}
	if len(body) == 0 {
		t.Fatalf("replay returned an empty JSON object: %s", rec.Body.String())
	}

	status, attempts, replayCount, deadLettered := h.deliveryRow(deliveryID)
	if status != "pending" {
		t.Fatalf("status after replay = %q, want pending", status)
	}
	if attempts != 0 {
		t.Fatalf("attempts after replay = %d, want 0 (a replay grants a fresh window)", attempts)
	}
	if replayCount != 1 {
		t.Fatalf("replay_count = %d, want 1", replayCount)
	}
	if deadLettered {
		t.Fatalf("dead_lettered_at is still set after a replay; the dead state was not cleared")
	}

	// The row is the same row: replay re-arms a delivery, it does not create a
	// second one, and it does not rewrite the event it carried.
	after := h.deliveriesOf(user, subID)
	if len(after) != 1 {
		t.Fatalf("the subscription now has %d deliveries, want 1 (replay must not duplicate the row)", len(after))
	}
	if got := after[0]["id"]; got != deliveryID.String() {
		t.Fatalf("delivery id changed across replay: %v -> %v", deliveryID, got)
	}
	if got := after[0]["event_type"]; got != eventType {
		t.Fatalf("event_type changed across replay: %v -> %v", eventType, got)
	}
}

// TestWebhookReplayCountsLifetimeWindowsOverHTTP proves replay_count is the
// lifetime number of replay windows and is never reset: two deaths and two
// replays leave the counter at 2, so an operator can still tell an endpoint that
// just failed once from one that has already exhausted two budgets.
func TestWebhookReplayCountsLifetimeWindowsOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Counter")
	userID := h.seedUser("counter-owner@example.test", "Counter-Owner-2026!", "Counter Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("counter-owner@example.test", "Counter-Owner-2026!")

	subID := h.createWebhook(user, "Counter Sink", []string{"crm.contact.created"}, 1)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(user, subID)[0]["id"].(string))

	// First death, first replay.
	h.driveDeliveryToDead(subID, deliveryID)
	if rec := h.replay(user, subID, deliveryID); rec.Code != http.StatusOK {
		t.Fatalf("first replay: status %d, body %s", rec.Code, rec.Body.String())
	}
	if _, _, n, _ := h.deliveryRow(deliveryID); n != 1 {
		t.Fatalf("replay_count after one replay = %d, want 1", n)
	}

	// Second death, second replay: the counter accumulates rather than restarting.
	h.driveDeliveryToDead(subID, deliveryID)
	if rec := h.replay(user, subID, deliveryID); rec.Code != http.StatusOK {
		t.Fatalf("second replay: status %d, body %s", rec.Code, rec.Body.String())
	}
	if _, _, n, _ := h.deliveryRow(deliveryID); n != 2 {
		t.Fatalf("replay_count after two replays = %d, want 2", n)
	}

	// Two windows means two audited replays, each carrying the count it produced.
	entries := h.replayAuditEntries(user, deliveryID)
	if len(entries) != 2 {
		t.Fatalf("replay audit entries = %d, want 2 (one per replay)", len(entries))
	}
}

// TestWebhookReplayAuditCarriesContextOverHTTP proves the audit entry answers the
// questions an investigation asks: which delivery, which organization, who did
// it, and how many replays the delivery has now had.
func TestWebhookReplayAuditCarriesContextOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Audit")
	userID := h.seedUser("audit-owner@example.test", "Audit-Owner-2026!", "Audit Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("audit-owner@example.test", "Audit-Owner-2026!")

	subID := h.createWebhook(user, "Audit Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(user, subID)[0]["id"].(string))
	h.driveDeliveryToDead(subID, deliveryID)

	if rec := h.replay(user, subID, deliveryID); rec.Code != http.StatusOK {
		t.Fatalf("replay: status %d, body %s", rec.Code, rec.Body.String())
	}

	entries := h.replayAuditEntries(user, deliveryID)
	if len(entries) != 1 {
		t.Fatalf("replay audit entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if got := e["entity_type"]; got != "webhook_delivery" {
		t.Errorf("audit entity_type = %v, want webhook_delivery", got)
	}
	if got := e["entity_id"]; got != deliveryID.String() {
		t.Errorf("audit entity_id = %v, want %v", got, deliveryID)
	}
	if got := e["organization_id"]; got != org.String() {
		t.Errorf("audit organization_id = %v, want %v", got, org)
	}
	if got := e["actor_user_id"]; got != userID.String() {
		t.Errorf("audit actor_user_id = %v, want %v (the acting user, not the system)", got, userID)
	}
	// The metadata is what lets an operator see how often the endpoint has already
	// been re-armed without reading the delivery table.
	meta, ok := e["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("audit metadata is not an object: %v", e["metadata"])
	}
	if got := meta["replay_count"]; got != float64(1) {
		t.Errorf("audit metadata replay_count = %v, want 1", got)
	}
}

// TestWebhookReplayRefusesInFlightDeliveryOverHTTP is the regression for a
// delivery a worker is actively sending.
//
// A claimed delivery is in the `delivering` state. Replaying it would hand the
// sender a second budget while it still holds the row, so the in-flight attempt
// and the replayed attempt could both run, and the fresh window the operator
// asked for would be consumed by the attempt already in progress. ADR 0004 says
// a delivery that is already in flight is refused; the guard must therefore
// exclude `delivering`, not only `pending`.
func TestWebhookReplayRefusesInFlightDeliveryOverHTTP(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	org := h.seedOrganization("Replay In Flight")
	userID := h.seedUser("inflight-owner@example.test", "Inflight-Owner-2026!", "Inflight Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("inflight-owner@example.test", "Inflight-Owner-2026!")

	subID := h.createWebhook(user, "Inflight Sink", []string{"crm.contact.created"}, 3)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(user, subID)[0]["id"].(string))

	// Model a worker that has claimed the delivery and is mid-send.
	if _, err := h.pool().Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'delivering', locked_by = 'worker-1', locked_at = now()
		WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("model an in-flight delivery: %v", err)
	}

	rec := h.replay(user, subID, deliveryID)
	if rec.Code == http.StatusOK {
		t.Fatalf("replaying an in-flight delivery succeeded (status %d, body %s); "+
			"it must be refused so the attempt in progress is not doubled", rec.Code, rec.Body.String())
	}

	status, _, replayCount, _ := h.deliveryRow(deliveryID)
	if status != "delivering" {
		t.Fatalf("status = %q after a refused replay, want delivering (unchanged)", status)
	}
	if replayCount != 0 {
		t.Fatalf("replay_count = %d after a refused replay, want 0", replayCount)
	}
	if entries := h.replayAuditEntries(user, deliveryID); len(entries) != 0 {
		t.Fatalf("a refused replay wrote %d audit entries, want 0", len(entries))
	}
}

// TestWebhookReplayUnderWrongSubscriptionIsRefusedOverHTTP covers the path's
// subscription half.
//
// The replay route is nested under a subscription, and the OpenAPI document marks
// both path parameters required, so the subscription is part of the resource's
// identity. A delivery that does not belong to the subscription named in the path
// must be refused rather than replayed, or the same request means different
// things depending on which half of the path a handler happens to read.
func TestWebhookReplayUnderWrongSubscriptionIsRefusedOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Wrong Sub")
	userID := h.seedUser("wrongsub-owner@example.test", "Wrongsub-Owner-2026!", "Wrongsub Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("wrongsub-owner@example.test", "Wrongsub-Owner-2026!")

	// Two subscriptions in the same organization.
	subA := h.createWebhook(user, "Sink A", []string{"crm.contact.created"}, 2)
	subB := h.createWebhook(user, "Sink B", []string{"crm.contact.created"}, 2)

	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 2 {
		t.Fatalf("dispatch created %d deliveries, want 2 (one per subscription)", n)
	}
	items := h.deliveriesOf(user, subA)
	if len(items) != 1 {
		t.Fatalf("subscription A has %d deliveries, want 1", len(items))
	}
	deliveryOfA, _ := uuid.Parse(items[0]["id"].(string))

	h.driveDeliveryToDead(subA, deliveryOfA)

	// Replay A's delivery while naming B's subscription in the path.
	rec := h.replay(user, subB, deliveryOfA)
	if rec.Code == http.StatusOK {
		t.Fatalf("a delivery was replayed under the wrong subscription (status %d, body %s); "+
			"the subscription in the path must be part of what identifies the delivery", rec.Code, rec.Body.String())
	}

	status, _, replayCount, _ := h.deliveryRow(deliveryOfA)
	if status != "dead" {
		t.Fatalf("status = %q after a refused replay, want dead (unchanged)", status)
	}
	if replayCount != 0 {
		t.Fatalf("replay_count = %d after a refused replay, want 0", replayCount)
	}
	if entries := h.replayAuditEntries(user, deliveryOfA); len(entries) != 0 {
		t.Fatalf("a refused replay wrote %d audit entries, want 0", len(entries))
	}
}

// TestWebhookReplayConcurrentIsAppliedOnceOverHTTP proves the replay guard holds
// under concurrency: a burst of replays of one dead delivery re-arms it once, not
// once per request. The guard lives in the UPDATE's WHERE clause, so the row lock
// makes the second writer observe the pending state and affect no row.
func TestWebhookReplayConcurrentIsAppliedOnceOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Concurrent")
	userID := h.seedUser("conc-owner@example.test", "Conc-Owner-2026!", "Conc Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("conc-owner@example.test", "Conc-Owner-2026!")

	subID := h.createWebhook(user, "Concurrent Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(user, subID)[0]["id"].(string))
	h.driveDeliveryToDead(subID, deliveryID)

	const racers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			rec := h.replay(user, subID, deliveryID)
			if rec.Code == http.StatusOK {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if _, _, replayCount, _ := h.deliveryRow(deliveryID); replayCount != 1 {
		t.Fatalf("replay_count = %d after %d concurrent replays, want 1", replayCount, racers)
	}
	if accepted != 1 {
		t.Fatalf("%d of %d concurrent replays reported success, want exactly 1", accepted, racers)
	}
	if entries := h.replayAuditEntries(user, deliveryID); len(entries) != 1 {
		t.Fatalf("replay audit entries = %d after concurrent replays, want 1", len(entries))
	}
}

// TestWebhookReplayRetryIsIdempotentOverHTTP models a client that retries after a
// dropped response. The first replay succeeds; the retry arrives against a now
// pending delivery and must be refused, so a lost response cannot silently grant
// a second window or duplicate the audit record.
func TestWebhookReplayRetryIsIdempotentOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Retry")
	userID := h.seedUser("retry-owner@example.test", "Retry-Owner-2026!", "Retry Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("retry-owner@example.test", "Retry-Owner-2026!")

	subID := h.createWebhook(user, "Retry Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(user, subID)[0]["id"].(string))
	h.driveDeliveryToDead(subID, deliveryID)

	if rec := h.replay(user, subID, deliveryID); rec.Code != http.StatusOK {
		t.Fatalf("first replay: status %d, body %s", rec.Code, rec.Body.String())
	}
	// The client did not see the response and tries again.
	if rec := h.replay(user, subID, deliveryID); rec.Code == http.StatusOK {
		t.Fatalf("a retried replay succeeded again (status %d); a lost response must not "+
			"grant a second window", rec.Code)
	}

	if _, _, replayCount, _ := h.deliveryRow(deliveryID); replayCount != 1 {
		t.Fatalf("replay_count = %d after a retried replay, want 1", replayCount)
	}
	if entries := h.replayAuditEntries(user, deliveryID); len(entries) != 1 {
		t.Fatalf("replay audit entries = %d after a retried replay, want 1", len(entries))
	}
}

// TestWebhookReplayIsScopedToTheOrganizationOverHTTP proves a caller from another
// organization cannot re-arm a delivery they do not own, even with the correct
// ids, and that the refusal has no side effect.
func TestWebhookReplayIsScopedToTheOrganizationOverHTTP(t *testing.T) {
	h := newHarness(t)

	orgA := h.seedOrganization("Tenant A")
	ownerA := h.seedUser("tenant-a@example.test", "Tenant-A-Owner-2026!", "Tenant A Owner")
	h.joinOrganization(ownerA, orgA, "tenant_admin")
	userA := h.login("tenant-a@example.test", "Tenant-A-Owner-2026!")

	orgB := h.seedOrganization("Tenant B")
	ownerB := h.seedUser("tenant-b@example.test", "Tenant-B-Owner-2026!", "Tenant B Owner")
	h.joinOrganization(ownerB, orgB, "tenant_admin")
	userB := h.login("tenant-b@example.test", "Tenant-B-Owner-2026!")

	subID := h.createWebhook(userA, "Tenant A Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(orgA, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(userA, subID)[0]["id"].(string))
	h.driveDeliveryToDead(subID, deliveryID)

	// Tenant B knows both ids and tries anyway.
	if rec := h.replay(userB, subID, deliveryID); rec.Code != http.StatusNotFound {
		t.Fatalf("a foreign organization's replay: status %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	// Viewing another tenant's delivery history must also be refused.
	if rec := h.do(userB, http.MethodGet, "/api/v1/webhooks/"+subID.String()+"/deliveries", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a foreign organization listing deliveries: status %d, want 404", rec.Code)
	}

	status, _, replayCount, _ := h.deliveryRow(deliveryID)
	if status != "dead" {
		t.Fatalf("status = %q after a refused cross-tenant replay, want dead (unchanged)", status)
	}
	if replayCount != 0 {
		t.Fatalf("replay_count = %d after a refused cross-tenant replay, want 0", replayCount)
	}
}

// TestWebhookReplayRequiresPermissionOverHTTP proves the endpoint is guarded by
// webhooks.manage on the server. A role that can work with CRM records but not
// manage integrations must be refused, and the refusal must not touch the row.
func TestWebhookReplayRequiresPermissionOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay Permission")
	adminID := h.seedUser("perm-admin@example.test", "Perm-Admin-2026!", "Perm Admin")
	h.joinOrganization(adminID, org, "tenant_admin")
	admin := h.login("perm-admin@example.test", "Perm-Admin-2026!")

	// sales_representative does not hold webhooks.manage.
	repID := h.seedUser("perm-rep@example.test", "Perm-Rep-2026!", "Perm Rep")
	h.joinOrganization(repID, org, "sales_representative")
	rep := h.login("perm-rep@example.test", "Perm-Rep-2026!")

	if h.hasPermission(rep, rbac.PermWebhooksManage) {
		t.Fatal("the fixture role unexpectedly holds webhooks.manage; the test would not exercise the check")
	}

	subID := h.createWebhook(admin, "Permission Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(admin, subID)[0]["id"].(string))
	h.driveDeliveryToDead(subID, deliveryID)

	if rec := h.replay(rep, subID, deliveryID); rec.Code != http.StatusForbidden {
		t.Fatalf("a caller without webhooks.manage: status %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	status, _, replayCount, _ := h.deliveryRow(deliveryID)
	if status != "dead" {
		t.Fatalf("status = %q after a forbidden replay, want dead (unchanged)", status)
	}
	if replayCount != 0 {
		t.Fatalf("replay_count = %d after a forbidden replay, want 0", replayCount)
	}
}

// TestWebhookReplayFailedAttemptLeavesNoTraceOverHTTP is the shared invariant
// behind the refusal cases: a replay that is not applied must leave the delivery
// exactly as it was and must not write an audit entry that claims otherwise. A
// false audit entry is worse than a missing one, because an operator reading the
// trail would conclude the event was re-sent when it was not.
func TestWebhookReplayFailedAttemptLeavesNoTraceOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Replay No Trace")
	userID := h.seedUser("notrace-owner@example.test", "Notrace-Owner-2026!", "Notrace Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("notrace-owner@example.test", "Notrace-Owner-2026!")

	subID := h.createWebhook(user, "No Trace Sink", []string{"crm.contact.created"}, 2)
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}
	deliveryID, _ := uuid.Parse(h.deliveriesOf(user, subID)[0]["id"].(string))

	// The delivery is pending, so the replay is refused.
	before := h.replayAuditEntries(user, deliveryID)
	rec := h.replay(user, subID, deliveryID)
	if rec.Code == http.StatusOK {
		t.Fatalf("replaying a pending delivery succeeded (status %d)", rec.Code)
	}

	status, attempts, replayCount, deadLettered := h.deliveryRow(deliveryID)
	if status != "pending" {
		t.Fatalf("status = %q after a refused replay, want pending (unchanged)", status)
	}
	if attempts != 0 || replayCount != 0 || deadLettered {
		t.Fatalf("a refused replay changed the row: attempts=%d replay_count=%d dead_lettered=%v",
			attempts, replayCount, deadLettered)
	}
	if after := h.replayAuditEntries(user, deliveryID); len(after) != len(before) {
		t.Fatalf("a refused replay wrote an audit entry (%d -> %d)", len(before), len(after))
	}
}
