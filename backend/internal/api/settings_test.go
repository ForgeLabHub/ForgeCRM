//go:build integration

// Platform settings through the real HTTP stack.
//
// The route reads the setting key from the `key` query parameter and only the
// value from the body. The interface's client once put the key in the body, so a
// save was rejected as a missing key; this test holds the route to the contract
// the client must follow, and checks the permission gate and the credential
// guard the handler applies.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

type settingItem struct {
	Value     any    `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

func (h *harness) settingValue(key string) (any, bool) {
	h.t.Helper()
	var raw []byte
	err := h.pool().QueryRow(context.Background(),
		`SELECT value FROM app_settings WHERE key = $1`, key).Scan(&raw)
	if err != nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		h.t.Fatalf("decode stored setting %q: %v", key, err)
	}
	return v, true
}

func TestASettingIsWrittenWithTheKeyInTheQuery(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Settings Tenant")
	user := h.seedUser("settings@example.com", "Settings-Passw0rd-2026!!", "S")
	h.joinOrganization(user, org, "platform_admin")
	a := h.login("settings@example.com", "Settings-Passw0rd-2026!!")

	rec := h.do(a, http.MethodPut, "/api/v1/admin/settings?key=crm.default_currency",
		`{"value":"EUR"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("write setting: status %d, body %s", rec.Code, rec.Body.String())
	}

	// The value is persisted, not merely echoed.
	if v, ok := h.settingValue("crm.default_currency"); !ok || v != "EUR" {
		t.Fatalf("setting not persisted: value=%v present=%v", v, ok)
	}

	// And it is returned by the list the interface reads.
	rec = h.do(a, http.MethodGet, "/api/v1/admin/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list settings: status %d, body %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Items map[string]settingItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode settings: %v (body %s)", err, rec.Body.String())
	}
	if got, ok := listed.Items["crm.default_currency"]; !ok || got.Value != "EUR" {
		t.Fatalf("list did not include the written setting: %+v", listed.Items)
	}
}

func TestASettingWithoutTheKeyIsRejected(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Settings NoKey")
	user := h.seedUser("nokey@example.com", "NoKey-Passw0rd-2026!!!", "N")
	h.joinOrganization(user, org, "platform_admin")
	a := h.login("nokey@example.com", "NoKey-Passw0rd-2026!!!")

	// The old client shape: the key in the body and no query parameter. The
	// route must reject it rather than silently storing nothing.
	rec := h.do(a, http.MethodPut, "/api/v1/admin/settings",
		`{"key":"crm.default_currency","value":"EUR"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("key in body should be rejected, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWritingASettingNeedsThePermission(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Settings NoPerm")
	user := h.seedUser("noperm@example.com", "NoPerm-Passw0rd-2026!!!", "N")
	h.joinOrganization(user, org, "employee")
	a := h.login("noperm@example.com", "NoPerm-Passw0rd-2026!!!")

	rec := h.do(a, http.MethodPut, "/api/v1/admin/settings?key=crm.default_currency",
		`{"value":"EUR"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without system.settings, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok := h.settingValue("crm.default_currency"); ok {
		t.Fatal("a setting was written without the permission")
	}
}

func TestASecretLookingSettingKeyIsRefused(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Settings Secret")
	user := h.seedUser("secret@example.com", "Secret-Passw0rd-2026!!!", "S")
	h.joinOrganization(user, org, "platform_admin")
	a := h.login("secret@example.com", "Secret-Passw0rd-2026!!!")

	rec := h.do(a, http.MethodPut, "/api/v1/admin/settings?key=stripe_api_key",
		`{"value":"sk_live_xxx"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a credential-looking key should be refused, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok := h.settingValue("stripe_api_key"); ok {
		t.Fatal("a credential-looking setting was stored")
	}
}
