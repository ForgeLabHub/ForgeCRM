//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// These tests cover the account sub-resource endpoints the customer card reads:
// the activity history and the account-scoped task list. Both existed on the
// server before the interface used them, so the risk being tested is not that
// the endpoints are new but that the page now depends on their behaviour —
// including their scoping, which the interface trusts and does not re-apply.

// seedActivity inserts one activity row for an account.
func (h *harness) seedActivity(orgID, accountID uuid.UUID, kind, subject string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	err := h.db.Pool().QueryRow(context.Background(),
		`INSERT INTO activities (organization_id, account_id, type, subject)
                 VALUES ($1, $2, $3, $4) RETURNING id`,
		orgID, accountID, kind, subject).Scan(&id)
	if err != nil {
		h.t.Fatalf("seed activity %q: %v", subject, err)
	}
	return id
}

// seedTask inserts one task row, optionally scoped to an account.
func (h *harness) seedTask(orgID uuid.UUID, accountID *uuid.UUID, title string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	err := h.db.Pool().QueryRow(context.Background(),
		`INSERT INTO tasks (organization_id, account_id, title, status)
                 VALUES ($1, $2, $3, 'open') RETURNING id`,
		orgID, accountID, title).Scan(&id)
	if err != nil {
		h.t.Fatalf("seed task %q: %v", title, err)
	}
	return id
}

// decodeItems reads the `items` array from a list response.
func decodeItems(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode list response: %v (body %s)", err, string(body))
	}
	return decoded.Items
}

// TestAccountActivityHistoryIsReturned proves the card's history section has a
// source: activities written for an account come back through the endpoint the
// page calls, in the shape the page reads.
func TestAccountActivityHistoryIsReturned(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Activity History")
	account := h.seedAccount(org, "Acme Ltd")
	h.seedActivity(org, account, "call", "Intro call")
	h.seedActivity(org, account, "email", "Sent the proposal")

	userID := h.seedUser("history@example.test", "History-2026-Example!", "History User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("history@example.test", "History-2026-Example!")

	rec := h.do(user, http.MethodGet, "/api/v1/accounts/"+account.String()+"/activities", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	items := decodeItems(t, rec.Body.Bytes())
	if len(items) != 2 {
		t.Fatalf("expected 2 activities, got %d: %s", len(items), rec.Body.String())
	}
	// The page reads `subject` and `type`; assert they survive serialisation
	// rather than only that rows came back.
	subjects := map[string]bool{}
	for _, item := range items {
		if _, ok := item["occurred_at"]; !ok {
			t.Errorf("an activity has no occurred_at, which the card renders: %v", item)
		}
		if s, ok := item["subject"].(string); ok {
			subjects[s] = true
		}
	}
	if !subjects["Intro call"] || !subjects["Sent the proposal"] {
		t.Errorf("the activity subjects are wrong: %v", subjects)
	}
}

// TestAccountActivityHistoryDoesNotCrossOrganizations proves the endpoint is
// scoped. The card presents history as a record of what happened with this
// customer, so another tenant's interactions appearing here would be both a
// leak and a false statement about the customer.
func TestAccountActivityHistoryDoesNotCrossOrganizations(t *testing.T) {
	h := newHarness(t)

	orgA := h.seedOrganization("Tenant A")
	accountA := h.seedAccount(orgA, "Acme A")
	h.seedActivity(orgA, accountA, "call", "Tenant A call")

	orgB := h.seedOrganization("Tenant B")
	userID := h.seedUser("b@tenant.test", "Tenant-B-Example-2026!", "Tenant B User")
	h.joinOrganization(userID, orgB, "sales_manager")
	userB := h.login("b@tenant.test", "Tenant-B-Example-2026!")

	rec := h.do(userB, http.MethodGet, "/api/v1/accounts/"+accountA.String()+"/activities", "")
	if rec.Code == http.StatusOK {
		items := decodeItems(t, rec.Body.Bytes())
		if len(items) != 0 {
			t.Fatalf("tenant B saw %d activities from tenant A: %s", len(items), rec.Body.String())
		}
	}
	// A 404 is the other acceptable answer: the account does not exist as far
	// as this tenant is concerned. Either way no tenant-A row may be returned.
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 200 with no items or 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAccountTaskListIsScopedToTheAccount proves the task filter the card relies
// on actually filters: tasks belonging to another account in the same
// organization must not appear on this customer's card.
func TestAccountTaskListIsScopedToTheAccount(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Account Tasks")
	account := h.seedAccount(org, "Acme Ltd")
	other := h.seedAccount(org, "Other Ltd")
	h.seedTask(org, &account, "Call Acme back")
	h.seedTask(org, &other, "Unrelated work")

	userID := h.seedUser("tasks@account.test", "Account-Tasks-2026!", "Task User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("tasks@account.test", "Account-Tasks-2026!")

	rec := h.do(user, http.MethodGet,
		"/api/v1/tasks?account_id="+account.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	items := decodeItems(t, rec.Body.Bytes())
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 task for the account, got %d: %s",
			len(items), rec.Body.String())
	}
	if title, _ := items[0]["title"].(string); title != "Call Acme back" {
		t.Fatalf("the wrong task was returned: %q", title)
	}
}

// TestAccountSubResourcesRequireTheirOwnPermission proves the cards are gated on
// the permission the endpoint enforces, not on the account permission alone.
//
// Every seeded role that holds accounts.read also holds activities.read, so the
// denial cannot be reached by choosing a different role: the grant is removed
// from this organization's copy of the role instead. Roles are data, so this is
// the same operation an administrator performs when they narrow a role, and it
// exercises the real enforcement path rather than a fixture shortcut.
func TestAccountSubResourcesRequireTheirOwnPermission(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Permission Gate")
	account := h.seedAccount(org, "Acme Ltd")
	h.seedActivity(org, account, "call", "Intro call")

	userID := h.seedUser("limited@example.test", "Limited-Example-2026!", "Limited User")
	h.joinOrganization(userID, org, "employee")

	// Narrow this organization's copy of the role: keep account access, drop
	// activity access. Scoped to the organization so no other test is affected.
	if _, err := h.db.Pool().Exec(context.Background(),
		`DELETE FROM role_permissions rp
                 USING roles r
                 WHERE rp.role_id = r.id
                   AND r.organization_id = $1
                   AND r.key = 'employee'
                   AND rp.permission_key = 'activities.read'`, org); err != nil {
		t.Fatalf("narrow the role: %v", err)
	}

	user := h.login("limited@example.test", "Limited-Example-2026!")

	// Assert the premise rather than trusting it: if the grant survived, the
	// 403 below would be testing nothing.
	if !h.hasPermission(user, "accounts.read") {
		t.Fatal("the fixture lost accounts.read; the test premise does not hold")
	}
	if h.hasPermission(user, "activities.read") {
		t.Fatal("the revocation did not take effect; the 403 below would be vacuous")
	}

	rec := h.do(user, http.MethodGet, "/api/v1/accounts/"+account.String()+"/activities", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without activities.read, got %d: %s", rec.Code, rec.Body.String())
	}

	// The account itself must remain readable: the denial is specific to the
	// activity history, not a side effect of the role edit.
	rec = h.do(user, http.MethodGet, "/api/v1/accounts/"+account.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the account should still be readable, got %d: %s", rec.Code, rec.Body.String())
	}
}
