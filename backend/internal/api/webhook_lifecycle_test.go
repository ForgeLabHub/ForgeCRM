//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// These tests drive the webhook lifecycle through the real HTTP stack: create a
// subscription, let a domain event fan out into a delivery, watch the delivery
// die after exhausting its attempts, and replay it. The service-level tests in
// internal/testsupport prove the state machine against the schema; these prove
// the endpoints are wired to it, which is a different failure surface — a route
// mounted without a permission check, a replay that does not reach the row, an
// audit entry that never gets written.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/api/...

// createWebhook subscribes through the API and returns the subscription id. The
// target is refused by the SSRF guard, so every delivery attempt fails without
// real network traffic.
func (h *harness) createWebhook(a *actor, name string, eventTypes []string, maxAttempts ...int) uuid.UUID {
	h.t.Helper()

	max := 5
	if len(maxAttempts) > 0 {
		max = maxAttempts[0]
	}
	body, _ := json.Marshal(map[string]any{
		"name":         name,
		"target_url":   "http://127.0.0.1:1/hook",
		"event_types":  eventTypes,
		"max_attempts": max,
	})
	rec := h.do(a, http.MethodPost, "/api/v1/webhooks", string(body))
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create webhook: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Subscription struct {
			ID uuid.UUID `json:"id"`
		} `json:"subscription"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		h.t.Fatalf("decode create webhook: %v (body %s)", err, rec.Body.String())
	}
	if resp.Subscription.ID == uuid.Nil {
		h.t.Fatalf("create webhook returned no subscription id: %s", rec.Body.String())
	}
	return resp.Subscription.ID
}

// dispatchOneEvent emits a domain event and fans it out into deliveries, the way
// the worker's outbox drain does.
func (h *harness) dispatchOneEvent(org uuid.UUID, eventType string) int {
	h.t.Helper()
	ctx := context.Background()

	store := outbox.NewStore(h.db)
	if err := store.Emit(ctx, &org, eventType, map[string]any{"id": uuid.NewString()}); err != nil {
		h.t.Fatalf("emit %s: %v", eventType, err)
	}
	claimed, err := store.ClaimPending(ctx, "test-drain", 10)
	if err != nil {
		h.t.Fatalf("claim outbox: %v", err)
	}
	if len(claimed) == 0 {
		h.t.Fatalf("the %s event was not claimable", eventType)
	}
	dispatcher := events.NewDispatcher(h.db, audit.New(h.db))
	n, err := dispatcher.Dispatch(ctx, claimed[0])
	if err != nil {
		h.t.Fatalf("dispatch %s: %v", eventType, err)
	}
	return n
}

// deliveriesOf reads the delivery history through the API.
func (h *harness) deliveriesOf(a *actor, subID uuid.UUID) []map[string]any {
	h.t.Helper()
	rec := h.do(a, http.MethodGet, "/api/v1/webhooks/"+subID.String()+"/deliveries", "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("list deliveries: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		h.t.Fatalf("decode deliveries: %v (body %s)", err, rec.Body.String())
	}
	return resp.Items
}

// auditCount reads the audit trail through the API and counts entries with the
// given action, so the assertion is about what an operator can actually see.
func (h *harness) auditCount(a *actor, action string) int {
	h.t.Helper()
	rec := h.do(a, http.MethodGet, "/api/v1/audit?action="+action+"&limit=100", "")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("list audit: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		h.t.Fatalf("decode audit: %v (body %s)", err, rec.Body.String())
	}
	return len(resp.Items)
}

// TestWebhookLifecycleOverHTTP is the end-to-end path a customer takes: the
// subscription is created through the API, a matching event produces a delivery,
// the delivery exhausts its attempts and dies, and the operator replays it. Every
// step is observed through an endpoint, not by reading the table directly, so a
// handler that fails to reach the service is caught.
func TestWebhookLifecycleOverHTTP(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	org := h.seedOrganization("Webhook Lifecycle")
	userID := h.seedUser("hooks-owner@example.test", "Hooks-Owner-2026!", "Hooks Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("hooks-owner@example.test", "Hooks-Owner-2026!")

	if !h.hasPermission(user, rbac.PermWebhooksManage) {
		t.Fatal("the fixture role cannot manage webhooks; the test would not exercise the endpoint")
	}

	subID := h.createWebhook(user, "Lifecycle Sink", []string{"crm.contact.created"}, 2)

	// A matching event fans out into exactly one delivery.
	if n := h.dispatchOneEvent(org, "crm.contact.created"); n != 1 {
		t.Fatalf("dispatch created %d deliveries, want 1", n)
	}

	items := h.deliveriesOf(user, subID)
	if len(items) != 1 {
		t.Fatalf("the API reports %d deliveries, want 1", len(items))
	}
	deliveryID, _ := uuid.Parse(items[0]["id"].(string))
	if deliveryID == uuid.Nil {
		t.Fatalf("delivery has no id: %v", items[0])
	}

	// Drive the delivery to the dead state, the way the worker's send loop does.
	keyring, err := secrets.NewKeyring(h.cfg.SecretKey)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	sender := events.NewDeliverySender(h.db, audit.New(h.db), keyring)
	for i := 0; i < 6; i++ {
		if _, err := h.pool().Exec(ctx,
			`UPDATE webhook_deliveries SET next_attempt_at = now() WHERE id = $1`, deliveryID); err != nil {
			t.Fatalf("reset next_attempt_at: %v", err)
		}
		d, err := sender.ClaimDelivery(ctx, "test-sender", time.Minute)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if d == nil {
			break
		}
		if err := sender.SendDelivery(ctx, d.ID); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	items = h.deliveriesOf(user, subID)
	if got := items[0]["status"]; got != "dead" {
		t.Fatalf("delivery status is %v, want dead (attempts=%v)", got, items[0]["attempts"])
	}

	// The dead transition is audited, and the audit is readable through the API.
	audited := h.auditCount(user, "webhooks.delivery_dead_lettered")
	if audited != 1 {
		t.Fatalf("audit has %d dead-letter entries, want 1", audited)
	}

	// Replay through the endpoint: the delivery is re-armed and the replay is
	// counted and audited.
	rec := h.do(user, http.MethodPost,
		"/api/v1/webhooks/"+subID.String()+"/deliveries/"+deliveryID.String()+"/replay", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("replay: status %d, body %s", rec.Code, rec.Body.String())
	}

	items = h.deliveriesOf(user, subID)
	if got := items[0]["status"]; got != "pending" {
		t.Fatalf("after replay the delivery status is %v, want pending", got)
	}
	if got := items[0]["replay_count"]; got != float64(1) {
		t.Fatalf("replay_count is %v, want 1", got)
	}
	if got := h.auditCount(user, "webhooks.replayed"); got != 1 {
		t.Fatalf("audit has %d replay entries, want 1", got)
	}

	// Replaying an already-pending delivery is refused rather than silently
	// counted a second time.
	again := h.do(user, http.MethodPost,
		"/api/v1/webhooks/"+subID.String()+"/deliveries/"+deliveryID.String()+"/replay", "")
	if again.Code != http.StatusNotFound {
		t.Fatalf("replay of a pending delivery: status %d, want 404", again.Code)
	}
}

// TestWebhookEventVocabularyMatchesTheDispatcher pins the advertised event list
// to what the dispatcher actually matches.
//
// The interface once built its subscription form from a local, shortened list
// ("contact.created" rather than "crm.contact.created"), so a subscription made
// from the form matched no event and the endpoint silently received nothing. This
// test drives a real dispatch for each type the capabilities endpoint advertises
// and fails if any of them cannot produce a delivery.
func TestWebhookEventVocabularyMatchesTheDispatcher(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Event Vocabulary")
	userID := h.seedUser("events-owner@example.test", "Events-Owner-2026!", "Events Owner")
	h.joinOrganization(userID, org, "tenant_admin")
	user := h.login("events-owner@example.test", "Events-Owner-2026!")

	rec := h.do(user, http.MethodGet, "/api/v1/system/capabilities", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities: status %d, body %s", rec.Code, rec.Body.String())
	}
	var caps struct {
		EventTypes []string `json:"event_types"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if len(caps.EventTypes) == 0 {
		t.Fatal("capabilities advertises no event types; the subscription form would be empty")
	}

	// Every type the outbox can emit must be advertised, or a subscription could
	// not select it from the form.
	advertised := map[string]bool{}
	for _, et := range caps.EventTypes {
		advertised[et] = true
	}
	for _, et := range outbox.EventTypes() {
		if !advertised[et] {
			t.Errorf("the outbox emits %q but capabilities does not advertise it", et)
		}
	}

	// Each advertised type must actually reach a subscription that selects it.
	for _, et := range caps.EventTypes {
		subID := h.createWebhook(user, "Sink "+et, []string{et})
		if n := h.dispatchOneEvent(org, et); n != 1 {
			t.Errorf("advertised type %q produced %d deliveries, want 1; "+
				"a subscription selecting it from the form would receive nothing", et, n)
		}
		if items := h.deliveriesOf(user, subID); len(items) != 1 {
			t.Errorf("advertised type %q recorded %d deliveries, want 1", et, len(items))
		}
	}
}
