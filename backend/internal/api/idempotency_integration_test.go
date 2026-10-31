//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/idempotency"
)

// Idempotency is a claim the API makes in its documentation: a creating request
// that carries an `Idempotency-Key` runs at most once, and a retry returns the first
// result. These tests drive it through the real router, the real session middleware
// and a real database, because the property being checked is exactly the one a unit
// test of the middleware cannot show — that the claim is scoped to the caller and
// reaches the store.

// postWithKey sends a request with an idempotency key.
func (h *harness) postWithKey(a *actor, path, key, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(idempotency.Header, key)
	req.AddCookie(&http.Cookie{Name: h.cfg.SessionCookieName, Value: a.token})
	req.Header.Set(h.cfg.CSRFHeader, a.csrf)

	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	return rec
}

// A retried create returns the first result and does not create a second row.
func TestIdempotentCreateRunsOnce(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Idempotency")
	userID := h.seedUser("idem@example.test", "Idempotency-2026!", "Idem User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("idem@example.test", "Idempotency-2026!")

	before := h.countAccountsIn(org)

	first := h.postWithKey(user, "/api/v1/accounts", "create-account-1", `{"name":"Acme"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create = %d: %s", first.Code, first.Body.String())
	}

	// The retry carries the same key. It must be answered from the stored result.
	second := h.postWithKey(user, "/api/v1/accounts", "create-account-1", `{"name":"Acme"}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("replay = %d, want 201: %s", second.Code, second.Body.String())
	}
	if second.Header().Get(idempotency.ReplayHeader) != "true" {
		t.Fatal("the replay was not marked with the Idempotent-Replay header")
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("the replay body differs from the original:\nfirst:  %s\nsecond: %s",
			first.Body.String(), second.Body.String())
	}

	// The claim is about the database, not the response, so the count is checked.
	if after := h.countAccountsIn(org); after != before+1 {
		t.Fatalf("accounts went from %d to %d; the retry created a second row", before, after)
	}
}

// A different key is a different request, so it creates a second row. Without this
// the middleware would be deduplicating everything, which would be worse than the
// bug it fixes.
func TestIdempotencyIsPerKey(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Idempotency Keys")
	userID := h.seedUser("idem2@example.test", "Idempotency-2026!", "Idem User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("idem2@example.test", "Idempotency-2026!")

	before := h.countAccountsIn(org)

	if rec := h.postWithKey(user, "/api/v1/accounts", "key-a", `{"name":"One"}`); rec.Code != http.StatusCreated {
		t.Fatalf("first create = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := h.postWithKey(user, "/api/v1/accounts", "key-b", `{"name":"Two"}`); rec.Code != http.StatusCreated {
		t.Fatalf("second create = %d: %s", rec.Code, rec.Body.String())
	}

	if after := h.countAccountsIn(org); after != before+2 {
		t.Fatalf("accounts went from %d to %d; two distinct keys should create two rows", before, after)
	}
}

// A key is scoped to its caller, so one user's key cannot shadow another's. Both
// requests run, and each gets its own result.
func TestIdempotencyKeysAreScopedToTheCaller(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Idempotency Scopes")

	firstID := h.seedUser("idem-a@example.test", "Idempotency-2026!", "User A")
	h.joinOrganization(firstID, org, "sales_manager")
	first := h.login("idem-a@example.test", "Idempotency-2026!")

	secondID := h.seedUser("idem-b@example.test", "Idempotency-2026!", "User B")
	h.joinOrganization(secondID, org, "sales_representative")
	second := h.login("idem-b@example.test", "Idempotency-2026!")

	before := h.countAccountsIn(org)

	// The same key, used by two different users, is two different requests.
	if rec := h.postWithKey(first, "/api/v1/accounts", "shared-key", `{"name":"A"}`); rec.Code != http.StatusCreated {
		t.Fatalf("first user = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := h.postWithKey(second, "/api/v1/accounts", "shared-key", `{"name":"B"}`); rec.Code != http.StatusCreated {
		t.Fatalf("second user = %d: %s", rec.Code, rec.Body.String())
	}

	if after := h.countAccountsIn(org); after != before+2 {
		t.Fatalf("accounts went from %d to %d; two users sharing a key must not collide", before, after)
	}
}

// A response that carries a one-time secret must not be written to the idempotency
// table. The second-factor enrolment returns the TOTP seed exactly once; if the
// middleware stored the response, the seed would sit in plaintext in
// `idempotency_keys.response` and be handed to a later request that reuses the key,
// turning a single reveal into a durable one. The handler marks the response
// `Cache-Control: no-store` and the middleware honours it.
func TestIdempotencyDoesNotStoreASecretResponse(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Idempotency Secrets")
	userID := h.seedUser("idem-secret@example.test", "Idempotency-2026!", "Secret User")
	h.joinOrganization(userID, org, "employee")
	user := h.login("idem-secret@example.test", "Idempotency-2026!")

	// Enrolling a second factor mints a seed and returns it in the body.
	rec := h.postWithKey(user, "/api/v1/auth/mfa/enroll", "enroll-key", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mfa enroll = %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the enrolment response is not marked no-store, so a secret response could be cached")
	}

	// The claim must have been released, not completed: a stored row would mean the
	// seed was persisted.
	var stored []byte
	err := h.db.Pool().QueryRow(context.Background(),
		`SELECT response FROM idempotency_keys WHERE key = $1`, "enroll-key").Scan(&stored)
	if err == nil {
		t.Fatalf("the secret response was persisted in idempotency_keys: %s", stored)
	}
}

// A failed request releases the key, so a corrected retry with the same key works.
// Pinning a failure would make a correctable request impossible to retry.
func TestIdempotencyReleasesTheKeyAfterAFailure(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Idempotency Failures")
	userID := h.seedUser("idem-fail@example.test", "Idempotency-2026!", "Idem User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("idem-fail@example.test", "Idempotency-2026!")

	// A missing name is a validation failure.
	failed := h.postWithKey(user, "/api/v1/accounts", "retry-me", `{}`)
	if failed.Code < 400 {
		t.Fatalf("an invalid body was accepted: %d %s", failed.Code, failed.Body.String())
	}

	// The same key with a corrected body must now succeed.
	fixed := h.postWithKey(user, "/api/v1/accounts", "retry-me", `{"name":"Fixed"}`)
	if fixed.Code != http.StatusCreated {
		t.Fatalf("retry after a failure = %d, want 201: %s", fixed.Code, fixed.Body.String())
	}
}

// The stored response is the one the client saw, so the replayed body decodes to
// the same record rather than to an empty or re-rendered one.
func TestIdempotentReplayReturnsTheSameRecord(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Idempotency Body")
	userID := h.seedUser("idem-body@example.test", "Idempotency-2026!", "Idem User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("idem-body@example.test", "Idempotency-2026!")

	first := h.postWithKey(user, "/api/v1/accounts", "body-key", `{"name":"Body Co"}`)
	second := h.postWithKey(user, "/api/v1/accounts", "body-key", `{"name":"Body Co"}`)

	var a, b map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if a["id"] != b["id"] {
		t.Fatalf("the replayed record has a different id: %v vs %v", a["id"], b["id"])
	}
}
