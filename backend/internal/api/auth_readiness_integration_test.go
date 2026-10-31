//go:build integration

// Authentication and readiness over the real HTTP stack.
//
// The scenario the task describes — a clean database, migrations, a first
// administrator, sign-in, /auth/me, sign-out, sign-in again, a wrong password, a
// disabled account and a revoked session — is walked end to end here. These are
// the paths that were failing in production, and they are the ones a mock cannot
// exercise: the failure was a missing table, which only a real database reports.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/geff1778/forgecrm/internal/bootstrap"
	"github.com/geff1778/forgecrm/internal/logging"
)

// TestReadyRefusesAnUnmigratedDatabase is the readiness half of the production
// incident. /health reports the process is up; /ready must refuse while the
// schema is absent, so an orchestrator never routes traffic to an instance whose
// every query would fail with `relation "users" does not exist`.
func TestReadyRefusesAnUnmigratedDatabase(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}

	health := h.do(nil, http.MethodGet, "/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("/health on an un-migrated database = %d, want 200 (the process is alive)", health.Code)
	}

	ready := h.do(nil, http.MethodGet, "/ready", "")
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready on an un-migrated database = %d, want 503", ready.Code)
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(ready.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /ready: %v (body %s)", err, ready.Body.String())
	}
	if body.Status != "unavailable" || body.Reason != "database schema is not ready" {
		t.Fatalf("/ready body = %+v, want an unavailable status naming the schema", body)
	}
}

// TestReadySucceedsOnAMigratedDatabase keeps the fix from over-correcting: a
// healthy deployment must still report ready.
func TestReadySucceedsOnAMigratedDatabase(t *testing.T) {
	h := newHarness(t)

	ready := h.do(nil, http.MethodGet, "/ready", "")
	if ready.Code != http.StatusOK {
		t.Fatalf("/ready on a migrated database = %d, body %s", ready.Code, ready.Body.String())
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(ready.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /ready: %v", err)
	}
	if body.Status != "ready" {
		t.Fatalf("/ready status = %q, want ready", body.Status)
	}
}

// TestFullSignInLifecycle walks the whole authentication scenario the task lists,
// including the negative cases. It is one test rather than several because the
// steps share state — the account created by the bootstrap is the one that signs
// in, is disabled and is signed out again — and splitting it would hide the
// ordering that makes each step meaningful.
func TestFullSignInLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// 1-3. Clean database, migrations applied by the harness, first administrator
	// created through the same bootstrap path an operator uses.
	const email = "owner@example.com"
	const password = "Str0ng-Owner-Passw0rd!"
	created, err := bootstrap.Admin(ctx, h.db, logging.New("error", "text"), bootstrap.Options{Email: email, Password: password})
	if err != nil {
		t.Fatalf("bootstrap the first owner: %v", err)
	}
	if !created.Created {
		t.Fatal("the first bootstrap did not create an owner")
	}

	// 4. Sign in.
	login := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d, body %s", login.Code, login.Body.String())
	}
	var sessionCookie string
	for _, c := range login.Result().Cookies() {
		if c.Name == h.cfg.SessionCookieName {
			sessionCookie = c.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	// 5. The current user.
	me := h.do(&actor{token: sessionCookie}, http.MethodGet, "/api/v1/auth/me", "")
	if me.Code != http.StatusOK {
		t.Fatalf("auth/me = %d, body %s", me.Code, me.Body.String())
	}
	var meBody struct {
		User struct {
			Email           string `json:"email"`
			IsPlatformOwner bool   `json:"is_platform_owner"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		t.Fatalf("decode auth/me: %v", err)
	}
	if meBody.User.Email != email {
		t.Fatalf("auth/me email = %q, want %q", meBody.User.Email, email)
	}
	if !meBody.User.IsPlatformOwner {
		t.Fatal("the bootstrapped account is not a platform owner")
	}
	if meBody.CSRFToken == "" {
		t.Fatal("auth/me did not return a CSRF token")
	}

	// 7. Sign out.
	logout := h.do(&actor{token: sessionCookie, csrf: meBody.CSRFToken},
		http.MethodPost, "/api/v1/auth/logout", "")
	if logout.Code != http.StatusOK {
		t.Fatalf("logout = %d, body %s", logout.Code, logout.Body.String())
	}

	// 11a. The signed-out session is no longer accepted.
	afterLogout := h.do(&actor{token: sessionCookie}, http.MethodGet, "/api/v1/auth/me", "")
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("auth/me after logout = %d, want 401", afterLogout.Code)
	}

	// 8. Sign in again with the same credentials.
	again := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if again.Code != http.StatusOK {
		t.Fatalf("second login = %d, body %s", again.Code, again.Body.String())
	}

	// 9. A wrong password is rejected and issues no session.
	wrong := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"not-the-password"}`)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("login with a wrong password = %d, want 401", wrong.Code)
	}
	for _, c := range wrong.Result().Cookies() {
		if c.Name == h.cfg.SessionCookieName && c.Value != "" {
			t.Fatal("a failed login set a session cookie")
		}
	}

	// 10. A suspended account cannot sign in even with the right password.
	if _, err := h.db.Pool().Exec(ctx,
		`UPDATE users SET status = 'suspended' WHERE email = $1`, email); err != nil {
		t.Fatalf("suspend the account: %v", err)
	}
	disabled := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if disabled.Code == http.StatusOK {
		t.Fatalf("a suspended account signed in: %d, body %s", disabled.Code, disabled.Body.String())
	}

	// 11b. A revoked session is refused. Re-enable the account first so the only
	// reason for the refusal is the revocation.
	if _, err := h.db.Pool().Exec(ctx,
		`UPDATE users SET status = 'active' WHERE email = $1`, email); err != nil {
		t.Fatalf("re-enable the account: %v", err)
	}
	fresh := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if fresh.Code != http.StatusOK {
		t.Fatalf("login after re-enabling = %d, body %s", fresh.Code, fresh.Body.String())
	}
	var liveCookie string
	for _, c := range fresh.Result().Cookies() {
		if c.Name == h.cfg.SessionCookieName {
			liveCookie = c.Value
		}
	}
	if _, err := h.db.Pool().Exec(ctx, `DELETE FROM sessions`); err != nil {
		t.Fatalf("revoke sessions: %v", err)
	}
	revoked := h.do(&actor{token: liveCookie}, http.MethodGet, "/api/v1/auth/me", "")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("auth/me with a revoked session = %d, want 401", revoked.Code)
	}
}
