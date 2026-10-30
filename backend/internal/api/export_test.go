//go:build integration

// Exports through the real HTTP stack.
//
// The route streams a file rather than returning a job, and it checks read access
// to the entity on top of the export permission. The interface's client once read
// a JSON body, which this route never sends; these tests hold the route to what it
// actually does — a CSV/JSONL attachment scoped to the caller's organization.
package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestExportStreamsCSVOfTheCallersOrganization(t *testing.T) {
	h := newHarness(t)
	orgA := h.seedOrganization("Export A")
	orgB := h.seedOrganization("Export B")
	user := h.seedUser("export@example.com", "Export-Passw0rd-2026!!!", "E")
	h.joinOrganization(user, orgA, "tenant_admin")
	a := h.login("export@example.com", "Export-Passw0rd-2026!!!")

	h.seedAccount(orgA, "Included Co")
	h.seedAccount(orgB, "Other Tenant Co")

	rec := h.do(a, http.MethodPost, "/api/v1/exports", `{"entity_type":"account","format":"csv"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected a CSV content type, got %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("expected an attachment disposition, got %q", cd)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Included Co") {
		t.Fatalf("the caller's own account is missing from the export:\n%s", body)
	}
	// Tenant isolation: another organization's data must not appear.
	if strings.Contains(body, "Other Tenant Co") {
		t.Fatalf("another organization's account leaked into the export:\n%s", body)
	}
}

func TestExportStreamsJSONLWhenAsked(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Export JSONL")
	user := h.seedUser("jsonl@example.com", "Jsonl-Passw0rd-2026!!!", "J")
	h.joinOrganization(user, org, "tenant_admin")
	a := h.login("jsonl@example.com", "Jsonl-Passw0rd-2026!!!")

	h.seedAccount(org, "Line One")

	rec := h.do(a, http.MethodPost, "/api/v1/exports", `{"entity_type":"account","format":"jsonl"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "ndjson") {
		t.Fatalf("expected a JSONL content type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "Line One") {
		t.Fatalf("the seeded account is missing:\n%s", rec.Body.String())
	}
}

func TestExportRefusesAnUnknownFormat(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Export BadFormat")
	user := h.seedUser("badfmt@example.com", "BadFmt-Passw0rd-2026!!", "B")
	h.joinOrganization(user, org, "tenant_admin")
	a := h.login("badfmt@example.com", "BadFmt-Passw0rd-2026!!")

	rec := h.do(a, http.MethodPost, "/api/v1/exports", `{"entity_type":"account","format":"xml"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown format should be refused, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Exporting an entity the caller cannot read is refused, even though the caller
// holds exports.execute. A support agent may export, but not accounts.
func TestExportNeedsReadAccessToTheEntity(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Export NoRead")
	user := h.seedUser("noread@example.com", "NoRead-Passw0rd-2026!!!", "N")
	h.joinOrganization(user, org, "support_agent")
	a := h.login("noread@example.com", "NoRead-Passw0rd-2026!!!")

	rec := h.do(a, http.MethodPost, "/api/v1/exports", `{"entity_type":"account","format":"csv"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("export without account read access should be 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The export is recorded in the audit log, which is append-only.
func TestExportIsAudited(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Export Audit")
	user := h.seedUser("exaudit@example.com", "ExAudit-Passw0rd-2026!", "E")
	h.joinOrganization(user, org, "tenant_admin")
	a := h.login("exaudit@example.com", "ExAudit-Passw0rd-2026!")

	rec := h.do(a, http.MethodPost, "/api/v1/exports", `{"entity_type":"account","format":"csv"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status %d", rec.Code)
	}

	var n int
	err := h.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log
		 WHERE organization_id = $1 AND entity_type = 'account' AND action LIKE '%export%'`,
		org).Scan(&n)
	if err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if n == 0 {
		t.Fatal("the export was not recorded in the audit log")
	}
}
