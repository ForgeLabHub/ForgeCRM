//go:build integration

// Notifications through the real HTTP stack.
//
// A notification is addressed to one user and one tenant. The interface lists a
// user's own notifications and marks one read; these tests hold the server to
// both scopes, because a leak here would show one user another's messages.
package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type notificationItem struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	ReadAt *string `json:"read_at"`
}

func (h *harness) seedNotification(orgID, userID uuid.UUID, title string) uuid.UUID {
	t := h.t
	t.Helper()
	var id uuid.UUID
	err := h.pool().QueryRow(context.Background(),
		`INSERT INTO notifications (organization_id, user_id, type, title)
		 VALUES ($1,$2,'workflow',$3) RETURNING id`, orgID, userID, title).Scan(&id)
	if err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return id
}

func TestNotificationsAreListedAndMarkedRead(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Notify Tenant")
	user := h.seedUser("notify@example.com", "Notify-Passw0rd-2026!!", "Notify")
	h.joinOrganization(user, org, "tenant_admin")
	a := h.login("notify@example.com", "Notify-Passw0rd-2026!!")

	id := h.seedNotification(org, a.userID, "Deal moved to Won")

	rec := h.do(a, http.MethodGet, "/api/v1/notifications", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d, body %s", rec.Code, rec.Body.String())
	}
	items := decodeList[notificationItem](t, rec.Body.Bytes())
	if len(items) != 1 || items[0].Title != "Deal moved to Won" {
		t.Fatalf("expected the seeded notification, got %+v", items)
	}
	if items[0].ReadAt != nil {
		t.Fatalf("a fresh notification should be unread, got read_at %v", *items[0].ReadAt)
	}

	// Unread-only framing the interface uses for its badge.
	rec = h.do(a, http.MethodGet, "/api/v1/notifications?unread_only=true", "")
	if got := decodeList[notificationItem](t, rec.Body.Bytes()); len(got) != 1 {
		t.Fatalf("unread filter dropped a fresh notification: %+v", got)
	}

	rec = h.do(a, http.MethodPost, "/api/v1/notifications/"+id.String()+"/read", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("mark read: status %d, body %s", rec.Code, rec.Body.String())
	}

	rec = h.do(a, http.MethodGet, "/api/v1/notifications?unread_only=true", "")
	if got := decodeList[notificationItem](t, rec.Body.Bytes()); len(got) != 0 {
		t.Fatalf("a read notification is still counted unread: %+v", got)
	}
}

func TestNotificationsAreNotVisibleAcrossUsersOrOrganizations(t *testing.T) {
	h := newHarness(t)
	orgA := h.seedOrganization("Notify A")
	orgB := h.seedOrganization("Notify B")
	userA := h.seedUser("na@example.com", "NA-Passw0rd-2026!!!!", "A")
	peerA := h.seedUser("npa@example.com", "NPA-Passw0rd-2026!!!", "A2")
	userB := h.seedUser("nb@example.com", "NB-Passw0rd-2026!!!!", "B")
	h.joinOrganization(userA, orgA, "employee")
	h.joinOrganization(peerA, orgA, "sales_representative")
	h.joinOrganization(userB, orgB, "employee")

	aBrief := h.login("na@example.com", "NA-Passw0rd-2026!!!!")
	peerBrief := h.login("npa@example.com", "NPA-Passw0rd-2026!!!")
	bBrief := h.login("nb@example.com", "NB-Passw0rd-2026!!!!")

	id := h.seedNotification(orgA, aBrief.userID, "For A only")

	// Same organization, different user: still not visible.
	rec := h.do(peerBrief, http.MethodGet, "/api/v1/notifications", "")
	if got := decodeList[notificationItem](t, rec.Body.Bytes()); len(got) != 0 {
		t.Fatalf("a peer saw another user's notification: %+v", got)
	}
	// And a peer cannot mark someone else's notification read.
	if rec := h.do(peerBrief, http.MethodPost, "/api/v1/notifications/"+id.String()+"/read", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("peer marking another user's notification read: got %d, want 404", rec.Code)
	}

	// Another organization sees nothing.
	rec = h.do(bBrief, http.MethodGet, "/api/v1/notifications", "")
	if got := decodeList[notificationItem](t, rec.Body.Bytes()); len(got) != 0 {
		t.Fatalf("cross-organization notification leaked: %+v", got)
	}
}

func TestMarkingAnUnknownNotificationReadIsNotFound(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Notify 404")
	user := h.seedUser("n404@example.com", "N404-Passw0rd-2026!!!", "N")
	h.joinOrganization(user, org, "tenant_admin")
	a := h.login("n404@example.com", "N404-Passw0rd-2026!!!")

	rec := h.do(a, http.MethodPost, "/api/v1/notifications/"+uuid.New().String()+"/read", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown notification, got %d: %s", rec.Code, rec.Body.String())
	}
}
