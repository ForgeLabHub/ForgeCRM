//go:build integration

// Integration tests for the organization configuration HTTP surface. They drive
// the real router, the real RBAC middleware and the real database, because what
// matters is that a tenant's own defaults are reachable, permission-checked and
// isolated at the boundary — not merely present in the domain package.
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// tenantConfigHarness is a tenant with an administrator who may change the
// configuration, an analyst who may only read it, and a second tenant whose
// configuration must stay invisible.
type tenantConfigHarness struct {
	*harness
	admin    *actor
	reader   *actor
	otherOrg *actor
}

func newTenantConfigHarness(t *testing.T) *tenantConfigHarness {
	t.Helper()
	h := newHarness(t)

	org := h.seedOrganization("Config Co")
	adminID := h.seedUser("admin@config.test", "correct-horse-battery", "Admin")
	h.joinOrganization(adminID, org, "tenant_admin")
	readerID := h.seedUser("analyst@config.test", "correct-horse-battery", "Analyst")
	h.joinOrganization(readerID, org, "analyst")

	other := h.seedOrganization("Other Co")
	otherID := h.seedUser("admin@other.test", "correct-horse-battery", "Other Admin")
	h.joinOrganization(otherID, other, "tenant_admin")

	return &tenantConfigHarness{
		harness:  h,
		admin:    h.login("admin@config.test", "correct-horse-battery"),
		reader:   h.login("analyst@config.test", "correct-horse-battery"),
		otherOrg: h.login("admin@other.test", "correct-horse-battery"),
	}
}

// TestTenantSettingsDefaultResolvesToTheCatalog checks that a key with no override
// answers with the catalog default rather than an error or a null. An absent row is
// not a missing setting.
func TestTenantSettingsDefaultResolvesToTheCatalog(t *testing.T) {
	c := newTenantConfigHarness(t)

	rec := c.do(c.admin, http.MethodGet, "/api/v1/settings/crm.default_currency", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status %d, body %s", rec.Code, rec.Body.String())
	}
	var setting struct {
		Key         string `json:"key"`
		Default     string `json:"default"`
		Effective   string `json:"effective"`
		HasOverride bool   `json:"has_override"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setting); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if setting.Effective == "" {
		t.Fatal("a key with no override must resolve to the catalog default")
	}
	if setting.Effective != setting.Default {
		t.Fatalf("effective %q != default %q for a key with no override", setting.Effective, setting.Default)
	}
	if setting.HasOverride {
		t.Fatal("has_override is true with no stored override")
	}
}

// TestTenantSettingsWriteAndReadBack drives the happy path over HTTP: an override is
// stored, then the effective value is the one that was written.
func TestTenantSettingsWriteAndReadBack(t *testing.T) {
	c := newTenantConfigHarness(t)

	rec := c.do(c.admin, http.MethodPut, "/api/v1/settings/crm.default_currency", `{"value":"EUR"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status %d, body %s", rec.Code, rec.Body.String())
	}
	var setting struct {
		Effective   string `json:"effective"`
		HasOverride bool   `json:"has_override"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setting); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if !setting.HasOverride || setting.Effective != "EUR" {
		t.Fatalf("after write: has_override=%v effective=%q, want true EUR", setting.HasOverride, setting.Effective)
	}

	// The write is visible on the next read, not only in the write's own response.
	read := c.do(c.admin, http.MethodGet, "/api/v1/settings/crm.default_currency", "")
	var after struct {
		Effective string `json:"effective"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	if after.Effective != "EUR" {
		t.Fatalf("effective after write = %q, want EUR", after.Effective)
	}
}

// TestTenantSettingsValidationIsReportedPerField checks that a bad value is a 422
// naming the key, not a 500 and not a silent accept.
func TestTenantSettingsValidationIsReportedPerField(t *testing.T) {
	c := newTenantConfigHarness(t)

	rec := c.do(c.admin, http.MethodPut, "/api/v1/settings/crm.default_currency", `{"value":"NOT A CURRENCY"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body.Error.Details["crm.default_currency"]; !ok {
		t.Fatalf("the error does not name the offending key: %v", body.Error.Details)
	}
}

// TestTenantSettingsUnknownKeyIsNotFound checks that a key outside the catalog is a
// 404, so the surface cannot promise a setting the platform does not keep.
func TestTenantSettingsUnknownKeyIsNotFound(t *testing.T) {
	c := newTenantConfigHarness(t)

	if got := c.do(c.admin, http.MethodGet, "/api/v1/settings/does.not.exist", "").Code; got != http.StatusNotFound {
		t.Fatalf("unknown key read: status %d, want 404", got)
	}
	if got := c.do(c.admin, http.MethodPut, "/api/v1/settings/does.not.exist", `{"value":"x"}`).Code; got != http.StatusNotFound {
		t.Fatalf("unknown key write: status %d, want 404", got)
	}
}

// TestTenantSettingsManageIsSeparateFromRead is the permission check: an analyst may
// read the configuration but may not move a default the whole organization inherits.
func TestTenantSettingsManageIsSeparateFromRead(t *testing.T) {
	c := newTenantConfigHarness(t)

	if got := c.do(c.reader, http.MethodGet, "/api/v1/settings/crm.default_currency", "").Code; got != http.StatusOK {
		t.Fatalf("analyst read: status %d, want 200", got)
	}
	if got := c.do(c.reader, http.MethodPut, "/api/v1/settings/crm.default_currency", `{"value":"EUR"}`).Code; got != http.StatusForbidden {
		t.Fatalf("analyst write: status %d, want 403", got)
	}
}

// TestTenantSettingsAreIsolatedBetweenOrganizations is the tenant-isolation check:
// one organization's override must not leak into another's effective value.
func TestTenantSettingsAreIsolatedBetweenOrganizations(t *testing.T) {
	c := newTenantConfigHarness(t)

	if rec := c.do(c.admin, http.MethodPut, "/api/v1/settings/crm.default_currency", `{"value":"EUR"}`); rec.Code != http.StatusOK {
		t.Fatalf("set in org A: status %d, body %s", rec.Code, rec.Body.String())
	}

	rec := c.do(c.otherOrg, http.MethodGet, "/api/v1/settings/crm.default_currency", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get in org B: status %d, body %s", rec.Code, rec.Body.String())
	}
	var setting struct {
		Effective   string `json:"effective"`
		HasOverride bool   `json:"has_override"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setting); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if setting.HasOverride || setting.Effective == "EUR" {
		t.Fatalf("org B sees org A's override: effective=%q has_override=%v", setting.Effective, setting.HasOverride)
	}
}

// TestTenantSettingsResetRestoresTheDefault checks that removing an override puts the
// catalog default back and records the reset in the history.
func TestTenantSettingsResetRestoresTheDefault(t *testing.T) {
	c := newTenantConfigHarness(t)

	if rec := c.do(c.admin, http.MethodPut, "/api/v1/settings/crm.default_currency", `{"value":"EUR"}`); rec.Code != http.StatusOK {
		t.Fatalf("set: status %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := c.do(c.admin, http.MethodDelete, "/api/v1/settings/crm.default_currency", ""); rec.Code != http.StatusOK {
		t.Fatalf("reset: status %d, body %s", rec.Code, rec.Body.String())
	}

	read := c.do(c.admin, http.MethodGet, "/api/v1/settings/crm.default_currency", "")
	var setting struct {
		Effective   string `json:"effective"`
		HasOverride bool   `json:"has_override"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &setting); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if setting.HasOverride {
		t.Fatal("the override survived the reset")
	}

	hist := c.do(c.admin, http.MethodGet, "/api/v1/settings/crm.default_currency/history", "")
	if hist.Code != http.StatusOK {
		t.Fatalf("history: status %d, body %s", hist.Code, hist.Body.String())
	}
	var out struct {
		Items []struct {
			Revision       int  `json:"revision"`
			ResetToDefault bool `json:"reset_to_default"`
		} `json:"items"`
	}
	if err := json.Unmarshal(hist.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode history: %v (body %s)", err, hist.Body.String())
	}
	if len(out.Items) != 2 {
		t.Fatalf("history has %d items, want 2 (a set and a reset)", len(out.Items))
	}
	if !out.Items[0].ResetToDefault {
		t.Fatal("the newest revision is not the reset")
	}
}

// TestTenantDefaultTimezoneReachesAWorkflowCreatedOverHTTP is the wiring check for
// the API process.
//
// The default-timezone setting only does anything if the engine that creates a
// workflow is the one holding the configuration port. The worker and the API build
// their own engines, so wiring the port into one and not the other leaves a setting
// that reads as configured and is silently ignored on the path users actually take —
// creating a workflow through the API. This test drives that path, so the missing
// wiring is a failure rather than a gap.
func TestTenantDefaultTimezoneReachesAWorkflowCreatedOverHTTP(t *testing.T) {
	c := newTenantConfigHarness(t)

	if rec := c.do(c.admin, http.MethodPut, "/api/v1/settings/workflow.default_timezone", `{"value":"Europe/Berlin"}`); rec.Code != http.StatusOK {
		t.Fatalf("set the default timezone: status %d, body %s", rec.Code, rec.Body.String())
	}

	// A schedule that names no timezone of its own must inherit the organization
	// default rather than fall back to UTC.
	body := `{"name":"Nightly sweep","trigger_type":"schedule","schedule_cron":"0 3 * * *",` +
		`"definition":{"steps":[{"id":"t1","type":"create_task","args":{"title":"Nightly sweep"}}]}}`
	rec := c.do(c.admin, http.MethodPost, "/api/v1/workflows", body)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create workflow: status %d, body %s", rec.Code, rec.Body.String())
	}
	var wf struct {
		ScheduleTimezone *string `json:"schedule_timezone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wf); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if wf.ScheduleTimezone == nil || *wf.ScheduleTimezone != "Europe/Berlin" {
		got := "<nil>"
		if wf.ScheduleTimezone != nil {
			got = *wf.ScheduleTimezone
		}
		t.Fatalf("schedule timezone = %s, want Europe/Berlin; the API engine is not wired to the organization configuration", got)
	}
}
