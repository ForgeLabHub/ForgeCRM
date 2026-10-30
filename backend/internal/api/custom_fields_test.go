//go:build integration

// Custom fields through the real HTTP stack.
//
// A definition is tenant-scoped, and the value for a record lives in that
// record's own `custom_fields` map. These tests cover the two halves the
// interface joins: defining a field, and reading back a value a caller stored on
// a record. The permission boundary matters because defining a field is a
// separate grant (`custom_fields.manage`) from editing the record.
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

type fieldDef struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Label     string `json:"label"`
	FieldType string `json:"field_type"`
}

func TestCustomFieldsCanBeDefinedAndListed(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("CF Tenant")
	admin := h.seedUser("cf-admin@example.com", "CF-Admin-Passw0rd-2026!", "Admin")
	h.joinOrganization(admin, org, "tenant_admin")
	a := h.login("cf-admin@example.com", "CF-Admin-Passw0rd-2026!")

	rec := h.do(a, http.MethodPost, "/api/v1/custom-fields",
		`{"entity_type":"account","key":"tier","label":"Account tier","field_type":"select","options":{"choices":["Gold","Silver"]}}`)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create field: status %d, body %s", rec.Code, rec.Body.String())
	}
	var created fieldDef
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode field: %v (body %s)", err, rec.Body.String())
	}
	if created.Key != "tier" || created.FieldType != "select" {
		t.Fatalf("unexpected field: %+v", created)
	}

	rec = h.do(a, http.MethodGet, "/api/v1/custom-fields?entity_type=account", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list fields: status %d, body %s", rec.Code, rec.Body.String())
	}
	items := decodeList[fieldDef](t, rec.Body.Bytes())
	if len(items) != 1 || items[0].Key != "tier" {
		t.Fatalf("expected the defined field, got %+v", items)
	}

	// A verifying reject: an unsupported field type is refused.
	rec = h.do(a, http.MethodPost, "/api/v1/custom-fields",
		`{"entity_type":"account","key":"bad","label":"Bad","field_type":"magic"}`)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected a validation refusal, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCustomFieldDefinitionsDoNotCrossOrganizations(t *testing.T) {
	h := newHarness(t)
	orgA := h.seedOrganization("CF A")
	orgB := h.seedOrganization("CF B")
	adminA := h.seedUser("cfa@example.com", "CF-A-Admin-Passw0rd-2026!", "A")
	adminB := h.seedUser("cfb@example.com", "CF-B-Admin-Passw0rd-2026!", "B")
	h.joinOrganization(adminA, orgA, "tenant_admin")
	h.joinOrganization(adminB, orgB, "tenant_admin")
	a1 := h.login("cfa@example.com", "CF-A-Admin-Passw0rd-2026!")
	a2 := h.login("cfb@example.com", "CF-B-Admin-Passw0rd-2026!")

	if rec := h.do(a1, http.MethodPost, "/api/v1/custom-fields",
		`{"entity_type":"account","key":"tier","label":"Tier","field_type":"text"}`); rec.Code >= 300 {
		t.Fatalf("create: status %d, body %s", rec.Code, rec.Body.String())
	}

	rec := h.do(a2, http.MethodGet, "/api/v1/custom-fields?entity_type=account", "")
	if got := decodeList[fieldDef](t, rec.Body.Bytes()); len(got) != 0 {
		t.Fatalf("cross-tenant definition leaked: %+v", got)
	}
}

func TestCustomFieldValuesRoundTripOnARecord(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("CFV Tenant")
	admin := h.seedUser("cfv-admin@example.com", "CFV-Admin-Passw0rd-2026!", "Admin")
	h.joinOrganization(admin, org, "tenant_admin")
	a := h.login("cfv-admin@example.com", "CFV-Admin-Passw0rd-2026!")

	account := h.seedAccount(org, "Acme")

	// The value rides on the record, so it is written through the account update
	// the interface uses and read back from the account itself.
	body := `{"name":"Acme","custom_fields":{"tier":"Gold","seats":12}}`
	rec := h.do(a, http.MethodPut, "/api/v1/accounts/"+account.String(), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update account: status %d, body %s", rec.Code, rec.Body.String())
	}

	rec = h.do(a, http.MethodGet, "/api/v1/accounts/"+account.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get account: status %d, body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		CustomFields map[string]any `json:"custom_fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode account: %v (body %s)", err, rec.Body.String())
	}
	if got.CustomFields["tier"] != "Gold" {
		t.Fatalf("expected tier Gold, got %+v", got.CustomFields)
	}
	if seats, ok := got.CustomFields["seats"].(float64); !ok || seats != 12 {
		t.Fatalf("expected seats 12, got %+v", got.CustomFields["seats"])
	}
}

func TestCustomFieldDefinitionRequiresTheManagePermission(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("CFP Tenant")
	rep := h.seedUser("cfp-rep@example.com", "CFP-Rep-Passw0rd-2026!", "Rep")
	h.joinOrganization(rep, org, "sales_representative")
	a := h.login("cfp-rep@example.com", "CFP-Rep-Passw0rd-2026!")

	rec := h.do(a, http.MethodPost, "/api/v1/custom-fields",
		`{"entity_type":"account","key":"tier","label":"Tier","field_type":"text"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without custom_fields.manage, got %d: %s", rec.Code, rec.Body.String())
	}
}
