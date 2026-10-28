//go:build integration

// Session sharing across replicas.
//
// A session is an opaque token whose hash lives in PostgreSQL, so a session
// created by one replica is resolvable by any replica that reads the same
// database. That is the property a second replica depends on: without it, a
// load-balanced deployment signs a user out on every request that lands on the
// other process.
//
// These tests establish it through the real HTTP stack and the real schema,
// using two independently built servers over one database.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// signInAs creates an account with a known password, makes it a member of a
// fresh organization with the tenant_admin role, and signs it in on the given
// replica. It returns the session cookie and the CSRF token.
//
// The membership is required: /auth/me refuses an account that is not in any
// active organization, which is a deliberate rule and not something a session
// test should work around.
func signInAs(t *testing.T, h *harness, email, password string) (token, csrf string) {
	t.Helper()

	userID := h.seedUser(email, password, "Cross Replica User")
	orgID := h.seedOrganization("cross-replica-" + strings.SplitN(email, "@", 2)[0])
	h.joinOrganization(userID, orgID, "tenant_admin")

	rec := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login on this replica: status %d, body %s", rec.Code, rec.Body.String())
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == h.cfg.SessionCookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("login did not set a session cookie")
	}

	me := h.do(&actor{token: token}, http.MethodGet, "/api/v1/auth/me", "")
	if me.Code != http.StatusOK {
		t.Fatalf("auth/me: status %d, body %s", me.Code, me.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode auth/me: %v", err)
	}
	return token, body.CSRFToken
}

// TestSessionCreatedOnOneReplicaWorksOnTheOther is the core claim. A token
// issued by replica A must be accepted by replica B, otherwise a load balancer
// without sticky sessions signs users out at random.
func TestSessionCreatedOnOneReplicaWorksOnTheOther(t *testing.T) {
	cfg := crossReplicaConfig(100000)
	first, second, _ := newReplicaPair(t, cfg)

	token, _ := signInAs(t, first, "shared-session@example.test", "correct-horse-battery")

	rec := second.do(&actor{token: token}, http.MethodGet, "/api/v1/auth/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("replica B rejected a session issued by replica A: status %d, body %s",
			rec.Code, rec.Body.String())
	}
}

// TestLogoutOnOneReplicaRevokesOnTheOther covers revocation. Signing out has to
// take effect everywhere, otherwise a stolen token stays valid on the replica
// the user did not sign out from.
func TestLogoutOnOneReplicaRevokesOnTheOther(t *testing.T) {
	cfg := crossReplicaConfig(100000)
	first, second, _ := newReplicaPair(t, cfg)

	token, csrf := signInAs(t, first, "logout-across@example.test", "correct-horse-battery")

	// Sanity: the token works on the other replica before logout.
	if rec := second.do(&actor{token: token}, http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("precondition failed: replica B rejected the session before logout: %d", rec.Code)
	}

	out := first.do(&actor{token: token, csrf: csrf}, http.MethodPost, "/api/v1/auth/logout", "")
	if out.Code != http.StatusOK && out.Code != http.StatusNoContent {
		t.Fatalf("logout on replica A: status %d, body %s", out.Code, out.Body.String())
	}

	if rec := second.do(&actor{token: token}, http.MethodGet, "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replica B still accepted a revoked session: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestSessionRevocationFromTheOtherReplica covers the admin path: revoking a
// session by id from replica B must invalidate the token replica A issued.
func TestSessionRevocationFromTheOtherReplica(t *testing.T) {
	cfg := crossReplicaConfig(100000)
	first, second, _ := newReplicaPair(t, cfg)

	token, csrf := signInAs(t, first, "revoke-across@example.test", "correct-horse-battery")

	// List the sessions from the other replica and revoke the one in use.
	list := second.do(&actor{token: token}, http.MethodGet, "/api/v1/auth/sessions", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list sessions on replica B: status %d, body %s", list.Code, list.Body.String())
	}
	var body struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sessions: %v (body %s)", err, list.Body.String())
	}
	sessions := body.Items

	var target string
	for _, s := range sessions {
		if s.Current {
			target = s.ID
		}
	}
	if target == "" {
		// The listing may not mark the current session; fall back to the only one.
		if len(sessions) == 1 {
			target = sessions[0].ID
		} else {
			t.Fatalf("could not identify the current session among %d", len(sessions))
		}
	}

	rec := second.do(&actor{token: token, csrf: csrf}, http.MethodDelete, "/api/v1/auth/sessions/"+target, "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("revoke session from replica B: status %d, body %s", rec.Code, rec.Body.String())
	}

	if after := first.do(&actor{token: token}, http.MethodGet, "/api/v1/auth/me", ""); after.Code != http.StatusUnauthorized {
		t.Fatalf("replica A still accepted a session revoked on replica B: status %d", after.Code)
	}
}

// TestAnInventedTokenIsRejectedOnEveryReplica guards against the session check
// being satisfied by anything other than a real row — the failure mode that
// would make every other test in this file meaningless.
func TestAnInventedTokenIsRejectedOnEveryReplica(t *testing.T) {
	cfg := crossReplicaConfig(100000)
	first, second, _ := newReplicaPair(t, cfg)

	forged := strings.Repeat("a", 64)
	for name, h := range map[string]*harness{"a": first, "b": second} {
		rec := h.do(&actor{token: forged}, http.MethodGet, "/api/v1/auth/me", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("replica %s accepted an invented token: status %d", name, rec.Code)
		}
	}
}

var _ = context.Background
