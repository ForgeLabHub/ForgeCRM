//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAppearancePreferencesArePersonalAndRoleDefaultsAreRecomputed(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Appearance Test")
	aliceID := h.seedUser("appearance-alice@example.test", "a-test-password-123", "Alice")
	bobID := h.seedUser("appearance-bob@example.test", "a-test-password-123", "Bob")
	h.joinOrganization(aliceID, org, "sales_representative")
	h.joinOrganization(bobID, org, "executive")
	alice := h.login("appearance-alice@example.test", "a-test-password-123")
	bob := h.login("appearance-bob@example.test", "a-test-password-123")

	get := func(a *actor) appearanceResponse {
		t.Helper()
		r := h.do(a, http.MethodGet, "/api/v1/auth/appearance", "")
		if r.Code != http.StatusOK {
			t.Fatalf("get appearance: %d %s", r.Code, r.Body.String())
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("missing no-store header")
		}
		var got appearanceResponse
		if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := get(alice); got.Style != nil || got.EffectiveStyle != "cyberpunk" || got.Mode != "system" {
		t.Fatalf("Alice default: %+v", got)
	}
	if got := get(bob); got.Style != nil || got.EffectiveStyle != "premium" {
		t.Fatalf("Bob default: %+v", got)
	}
	body := `{"style":"glass","mode":"dark","accent":"cyan","density":"compact"}`
	if r := h.do(alice, http.MethodPut, "/api/v1/auth/appearance", body); r.Code != http.StatusOK {
		t.Fatalf("put appearance: %d %s", r.Code, r.Body.String())
	}
	// A separate session for the same user sees the saved settings.
	aliceAgain := h.login("appearance-alice@example.test", "a-test-password-123")
	if got := get(aliceAgain); got.Style == nil || *got.Style != "glass" || got.EffectiveStyle != "glass" || got.Mode != "dark" || got.Accent != "cyan" || got.Density != "compact" {
		t.Fatalf("persisted: %+v", got)
	}
	if got := get(bob); got.Style != nil || got.EffectiveStyle != "premium" {
		t.Fatalf("other user changed: %+v", got)
	}
	if r := h.do(alice, http.MethodPut, "/api/v1/auth/appearance", `{"style":"invalid","mode":"dark","accent":"cyan","density":"compact"}`); r.Code != http.StatusUnprocessableEntity && r.Code != http.StatusBadRequest {
		t.Fatalf("invalid style: %d %s", r.Code, r.Body.String())
	}
	if r := h.do(alice, http.MethodPut, "/api/v1/auth/appearance", `{"user_id":"`+bobID.String()+`","style":"minimal","mode":"system","accent":"theme","density":"standard"}`); r.Code != http.StatusBadRequest {
		t.Fatalf("foreign user field: %d %s", r.Code, r.Body.String())
	}
	noCSRF := *alice
	noCSRF.csrf = ""
	if r := h.do(&noCSRF, http.MethodPut, "/api/v1/auth/appearance", body); r.Code != http.StatusForbidden {
		t.Fatalf("without CSRF: %d %s", r.Code, r.Body.String())
	}
	if r := h.do(nil, http.MethodGet, "/api/v1/auth/appearance", ""); r.Code != http.StatusUnauthorized {
		t.Fatalf("without session: %d", r.Code)
	}
	if got := get(alice); got.Style == nil || *got.Style != "glass" {
		t.Fatalf("invalid write changed saved style: %+v", got)
	}
	if _, err := h.db.Pool().Exec(context.Background(), `
		UPDATE user_roles SET role_id = (SELECT id FROM roles WHERE key = 'executive' AND organization_id = $1)
		WHERE user_id = $2 AND organization_id = $1`, org, aliceID); err != nil {
		t.Fatal(err)
	}
	if got := get(alice); got.DefaultStyle != "premium" || got.EffectiveStyle != "glass" {
		t.Fatalf("explicit choice did not survive role change: %+v", got)
	}
	if r := h.do(alice, http.MethodPut, "/api/v1/auth/appearance", `{"style":null,"mode":"system","accent":"theme","density":"standard"}`); r.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", r.Code, r.Body.String())
	}
	if got := get(aliceAgain); got.Style != nil || got.EffectiveStyle != "premium" {
		t.Fatalf("reset did not recompute: %+v", got)
	}
}
