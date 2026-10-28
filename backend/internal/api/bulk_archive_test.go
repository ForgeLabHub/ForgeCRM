//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Bulk archive is the first operation in the product that changes many records
// from one request, so it is the first place where a mistake is multiplied. These
// tests drive it through the real router, the real permission middleware and a
// real database.

// bulkArchive POSTs a body of ids and returns the decoded response.
func (h *harness) bulkArchive(a *actor, entity string, ids []uuid.UUID) (int, map[string]any, string) {
	h.t.Helper()
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, fmt.Sprintf("%q", id.String()))
	}
	body := `{"ids":[` + strings.Join(quoted, ",") + `]}`
	rec := h.do(a, http.MethodPost, "/api/v1/"+entity+"/bulk-archive", body)

	var decoded map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	}
	return rec.Code, decoded, rec.Body.String()
}

// bulkArchiveRaw POSTs a raw body, for malformed-input cases.
func (h *harness) bulkArchiveRaw(a *actor, entity, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	return h.do(a, http.MethodPost, "/api/v1/"+entity+"/bulk-archive", body)
}

// isArchived reports whether a record carries an archived_at value.
func (h *harness) isArchived(table string, id uuid.UUID) bool {
	h.t.Helper()
	var archived bool
	q := fmt.Sprintf(`SELECT archived_at IS NOT NULL FROM %s WHERE id = $1`, table)
	if err := h.db.Pool().QueryRow(context.Background(), q, id).Scan(&archived); err != nil {
		h.t.Fatalf("read archived state of %s %s: %v", table, id, err)
	}
	return archived
}

// lenFrom returns the length of an array member of the response.
func lenFrom(t *testing.T, decoded map[string]any, member string) int {
	t.Helper()
	raw, ok := decoded[member].([]any)
	if !ok {
		t.Fatalf("the response has no %q array: %v", member, decoded)
	}
	return len(raw)
}

// idsFrom pulls a list of strings out of a decoded response member.
func idsFrom(t *testing.T, decoded map[string]any, member string) []string {
	t.Helper()
	raw, ok := decoded[member].([]any)
	if !ok {
		t.Fatalf("the response has no %q array: %v", member, decoded)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s contains a non-string: %v", member, v)
		}
		out = append(out, s)
	}
	return out
}

// TestBulkArchiveArchivesEveryRequestedRecord is the base case: two records in,
// two records archived, both reported.
func TestBulkArchiveArchivesEveryRequestedRecord(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Bulk Archive")
	first := h.seedAccount(org, "Acme One")
	second := h.seedAccount(org, "Acme Two")

	userID := h.seedUser("bulk@example.test", "Bulk-Archive-2026!", "Bulk User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("bulk@example.test", "Bulk-Archive-2026!")

	code, decoded, body := h.bulkArchive(user, "accounts", []uuid.UUID{first, second})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	archived := idsFrom(t, decoded, "archived")
	if len(archived) != 2 {
		t.Fatalf("expected 2 archived, got %v: %s", archived, body)
	}
	if n := lenFrom(t, decoded, "failed"); n != 0 {
		t.Fatalf("expected no failures, got %d: %s", n, body)
	}

	// The claim is that the records are archived, so that is checked in the
	// database rather than inferred from the response.
	if !h.isArchived("accounts", first) || !h.isArchived("accounts", second) {
		t.Fatalf("the response said archived but the rows disagree: %s", body)
	}
}

// TestBulkArchiveReportsPartialFailure proves a failing record does not stop the
// rest. A caller archiving a page of results should not have to retry the page
// because one row was already archived.
func TestBulkArchiveReportsPartialFailure(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Partial Failure")
	live := h.seedAccount(org, "Still Live")
	already := h.seedAccount(org, "Already Gone")
	if _, err := h.db.Pool().Exec(context.Background(),
		`UPDATE accounts SET archived_at = now() WHERE id = $1`, already); err != nil {
		t.Fatalf("pre-archive: %v", err)
	}

	userID := h.seedUser("partial@example.test", "Partial-Bulk-2026!", "Partial User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("partial@example.test", "Partial-Bulk-2026!")

	code, decoded, body := h.bulkArchive(user, "accounts", []uuid.UUID{live, already})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	archived := idsFrom(t, decoded, "archived")
	if len(archived) != 1 || archived[0] != live.String() {
		t.Fatalf("expected only the live account archived, got %v: %s", archived, body)
	}

	rawFailed, ok := decoded["failed"].([]any)
	if !ok || len(rawFailed) != 1 {
		t.Fatalf("expected exactly one failure, got %v: %s", decoded["failed"], body)
	}
	entry, _ := rawFailed[0].(map[string]any)
	if got, _ := entry["id"].(string); got != already.String() {
		t.Fatalf("the failure names the wrong record: %v", entry)
	}
	if reason, _ := entry["reason"].(string); reason == "" {
		t.Fatalf("the failure gives no reason: %v", entry)
	}

	// The already-archived record must not have been touched again: re-archiving
	// would move its archived_at forward and lose the original time.
	if !h.isArchived("accounts", already) {
		t.Fatal("the already-archived record lost its archived state")
	}
}

// TestBulkArchiveCannotReachAnotherOrganization is the security case that matters
// most here. A caller who guesses ids must not be able to archive records in
// another tenant, and must not learn whether those records exist.
func TestBulkArchiveCannotReachAnotherOrganization(t *testing.T) {
	h := newHarness(t)

	orgA := h.seedOrganization("Tenant A")
	victim := h.seedAccount(orgA, "Tenant A Account")

	orgB := h.seedOrganization("Tenant B")
	userID := h.seedUser("b@bulk.test", "Tenant-B-Bulk-2026!", "Tenant B User")
	h.joinOrganization(userID, orgB, "sales_manager")
	userB := h.login("b@bulk.test", "Tenant-B-Bulk-2026!")

	code, decoded, body := h.bulkArchive(userB, "accounts", []uuid.UUID{victim})
	if code != http.StatusOK {
		t.Fatalf("expected 200 with a per-record failure, got %d: %s", code, body)
	}
	if archived := idsFrom(t, decoded, "archived"); len(archived) != 0 {
		t.Fatalf("tenant B archived tenant A's account: %v", archived)
	}
	if n := lenFrom(t, decoded, "failed"); n != 1 {
		t.Fatalf("expected one reported failure, got %d: %s", n, body)
	}

	// The record itself is the real evidence. A response is only a claim.
	if h.isArchived("accounts", victim) {
		t.Fatal("tenant A's account was archived by tenant B")
	}

	// The reason must not disclose that the record exists elsewhere. "not found"
	// and "not yours" are deliberately the same answer.
	rawFailed, _ := decoded["failed"].([]any)
	entry, _ := rawFailed[0].(map[string]any)
	reason, _ := entry["reason"].(string)
	if strings.Contains(strings.ToLower(reason), "organization") ||
		strings.Contains(strings.ToLower(reason), "tenant") {
		t.Fatalf("the failure reason discloses another tenant: %q", reason)
	}
}

// TestBulkArchiveRequiresTheArchivePermission proves the route is not a way
// around the permission the single-record operation enforces. Read access alone
// must not be enough.
func TestBulkArchiveRequiresTheArchivePermission(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Bulk Permission")
	account := h.seedAccount(org, "Acme Ltd")

	userID := h.seedUser("narrow@example.test", "Narrow-Bulk-2026!", "Narrow User")
	h.joinOrganization(userID, org, "employee")

	// Remove accounts.archive from this organization's copy of the role. Roles
	// are data, so this is the edit an administrator makes; scoped to the
	// organization so no other test is affected.
	if _, err := h.db.Pool().Exec(context.Background(),
		`DELETE FROM role_permissions rp
		 USING roles r
		 WHERE rp.role_id = r.id
		   AND r.organization_id = $1
		   AND r.key = 'employee'
		   AND rp.permission_key = 'accounts.archive'`, org); err != nil {
		t.Fatalf("narrow the role: %v", err)
	}

	user := h.login("narrow@example.test", "Narrow-Bulk-2026!")

	if h.hasPermission(user, "accounts.archive") {
		t.Fatal("the revocation did not take effect; the 403 below would be vacuous")
	}
	if !h.hasPermission(user, "accounts.read") {
		t.Fatal("the fixture lost accounts.read; the test premise does not hold")
	}

	code, _, body := h.bulkArchive(user, "accounts", []uuid.UUID{account})
	if code != http.StatusForbidden {
		t.Fatalf("expected 403 without accounts.archive, got %d: %s", code, body)
	}
	if h.isArchived("accounts", account) {
		t.Fatal("the account was archived despite the 403")
	}
}

// TestBulkArchiveRejectsMalformedRequests covers the input rules. Each of these
// is refused before any record is touched.
func TestBulkArchiveRejectsMalformedRequests(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Bulk Validation")
	userID := h.seedUser("valid@example.test", "Validate-Bulk-2026!", "Validate User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("valid@example.test", "Validate-Bulk-2026!")

	// Two hundred and one ids: one over the documented limit. The limit exists so
	// one request cannot become an unbounded scan.
	tooMany := make([]uuid.UUID, 0, 201)
	for i := 0; i < 201; i++ {
		tooMany = append(tooMany, uuid.New())
	}

	quoted := make([]string, 0, len(tooMany))
	for _, id := range tooMany {
		quoted = append(quoted, fmt.Sprintf("%q", id.String()))
	}

	cases := []struct {
		name string
		body string
		want int
	}{
		// A body that decodes but breaks a rule: Validate refuses it, 422.
		{"empty list", `{"ids":[]}`, http.StatusUnprocessableEntity},
		{"missing ids", `{}`, http.StatusUnprocessableEntity},
		{"over the limit", `{"ids":[` + strings.Join(quoted, ",") + `]}`, http.StatusUnprocessableEntity},
		{"nil uuid", `{"ids":["00000000-0000-0000-0000-000000000000"]}`, http.StatusUnprocessableEntity},
		// A body that is not JSON at all never reaches Validate: the decoder refuses
		// it with 400. Asserting 422 here would demand the wrong layer answer.
		{"malformed uuid", `{"ids":["not-a-uuid"]}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.bulkArchiveRaw(user, "accounts", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}

	// Duplicates are rejected rather than collapsed. Archiving the same record
	// twice in one request would report it once and archive it once, which would
	// make the counts in the response disagree with the request.
	same := uuid.New().String()
	rec := h.bulkArchiveRaw(user, "accounts", `{"ids":["`+same+`","`+same+`"]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for duplicate ids, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestBulkArchiveRouteIsNotShadowed proves the literal segment wins over the
// {id} route, so the request reaches the bulk handler rather than being decoded
// as an id of "bulk-archive". A 422 with a uuid complaint would mean the wrong
// handler answered.
func TestBulkArchiveRouteIsNotShadowed(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Bulk Routing")
	account := h.seedAccount(org, "Acme Ltd")
	userID := h.seedUser("route@example.test", "Route-Bulk-2026!", "Route User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("route@example.test", "Route-Bulk-2026!")

	code, decoded, body := h.bulkArchive(user, "accounts", []uuid.UUID{account})
	if code != http.StatusOK {
		t.Fatalf("expected the bulk handler to answer with 200, got %d: %s", code, body)
	}
	if _, ok := decoded["archived"]; !ok {
		t.Fatalf("the response is not a bulk archive response: %s", body)
	}
}

// TestBulkArchiveUnsupportedEntityIsNotRouted proves tasks, tickets and leads do
// not silently accept a bulk archive they cannot perform. They have an
// archived_at column but no archive operation, so the route must not exist.
func TestBulkArchiveUnsupportedEntityIsNotRouted(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Unsupported Bulk")
	userID := h.seedUser("unsup@example.test", "Unsup-Bulk-2026!", "Unsup User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("unsup@example.test", "Unsup-Bulk-2026!")

	for _, entity := range []string{"tasks", "tickets", "leads"} {
		rec := h.bulkArchiveRaw(user, entity, `{"ids":["`+uuid.New().String()+`"]}`)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s/bulk-archive answered %d; it should not be routed: %s",
				entity, rec.Code, rec.Body.String())
		}
	}
}
