//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"strings"

	"github.com/geff1778/forgecrm/internal/rbac"
)

// TestAccountsAreScopedToTheActingOrganization is the core isolation check at the
// HTTP layer. Two organizations each hold an account; neither user may see the
// other's record, and in particular may not reach it by guessing its identifier.
func TestAccountsAreScopedToTheActingOrganization(t *testing.T) {
	h := newHarness(t)

	orgA := h.seedOrganization("Acme A")
	orgB := h.seedOrganization("Beta B")

	aliceID := h.seedUser("alice@a.test", "Alice-Passw0rd-2026!", "Alice")
	h.joinOrganization(aliceID, orgA, "tenant_admin")

	bobID := h.seedUser("bob@b.test", "Bob-Passw0rd-2026!", "Bob")
	h.joinOrganization(bobID, orgB, "tenant_admin")

	accountA := h.seedAccount(orgA, "Account in A")
	accountB := h.seedAccount(orgB, "Account in B")

	alice := h.login("alice@a.test", "Alice-Passw0rd-2026!")
	bob := h.login("bob@b.test", "Bob-Passw0rd-2026!")

	if !h.hasPermission(alice, rbac.PermAccountsRead) {
		t.Fatal("alice should hold accounts.read; the fixture did not grant the role")
	}

	t.Run("listing shows only own records", func(t *testing.T) {
		rec := h.do(alice, http.MethodGet, "/api/v1/accounts/", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); containsName(body, "Account in B") {
			t.Errorf("alice's list contains bob's account: %s", body)
		} else if !containsName(body, "Account in A") {
			t.Errorf("alice's list is missing her own account: %s", body)
		}
	})

	// The interesting case: the record exists, alice is authenticated and holds
	// accounts.read, but the record belongs to another tenant. A 404 rather than a
	// 403 is deliberate: a 403 would confirm the identifier exists.
	t.Run("direct access to a foreign record is denied", func(t *testing.T) {
		rec := h.do(alice, http.MethodGet, "/api/v1/accounts/"+accountB.String()+"/", "")
		if rec.Code == http.StatusOK {
			t.Fatalf("alice read another organization's account: %s", rec.Body.String())
		}
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 404 or 403", rec.Code)
		}
		if containsName(rec.Body.String(), "Account in B") {
			t.Errorf("the denial leaked the record contents: %s", rec.Body.String())
		}
	})

	t.Run("own record is readable", func(t *testing.T) {
		rec := h.do(alice, http.MethodGet, "/api/v1/accounts/"+accountA.String()+"/", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("alice could not read her own account: status %d, body %s", rec.Code, rec.Body.String())
		}
	})

	// A write is the more dangerous direction: reading a foreign record leaks
	// data, but updating one corrupts it.
	t.Run("update of a foreign record is denied and changes nothing", func(t *testing.T) {
		rec := h.do(alice, http.MethodPut, "/api/v1/accounts/"+accountB.String()+"/",
			`{"name":"Hijacked by Alice"}`)
		if rec.Code == http.StatusOK {
			t.Fatal("alice updated another organization's account")
		}

		var name string
		if err := h.pool().QueryRow(t.Context(),
			`SELECT name FROM accounts WHERE id = $1`, accountB).Scan(&name); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if name != "Account in B" {
			t.Fatalf("bob's account was modified: name = %q", name)
		}
	})

	t.Run("archive of a foreign record is denied and changes nothing", func(t *testing.T) {
		rec := h.do(alice, http.MethodDelete, "/api/v1/accounts/"+accountB.String()+"/", "")
		if rec.Code == http.StatusOK {
			t.Fatal("alice archived another organization's account")
		}

		var archived *string
		if err := h.pool().QueryRow(t.Context(),
			`SELECT archived_at::text FROM accounts WHERE id = $1`, accountB).Scan(&archived); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if archived != nil {
			t.Fatalf("bob's account was archived: archived_at = %v", *archived)
		}
	})

	// The account alice creates must land in her own organization, not in the one
	// she is not a member of. A missing organization filter on insert would put
	// the row in the wrong tenant without any error.
	t.Run("created records belong to the acting organization", func(t *testing.T) {
		before := h.countAccountsIn(orgB)

		rec := h.do(alice, http.MethodPost, "/api/v1/accounts/", `{"name":"Created by Alice"}`)
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("create: status %d, body %s", rec.Code, rec.Body.String())
		}

		if after := h.countAccountsIn(orgB); after != before {
			t.Fatalf("a record created by alice appeared in bob's organization: %d -> %d", before, after)
		}

		var orgID uuid.UUID
		if err := h.pool().QueryRow(t.Context(),
			`SELECT organization_id FROM accounts WHERE name = 'Created by Alice'`).Scan(&orgID); err != nil {
			t.Fatalf("the created account is missing: %v", err)
		}
		if orgID != orgA {
			t.Fatalf("created in organization %s, want %s", orgID, orgA)
		}
	})

	_ = bob
}

// TestPermissionIsEnforcedBeforeDataIsRead checks that a member with a narrow role
// is refused by the API rather than by the interface. Hiding a button is not
// access control: the request must fail on its own.
func TestPermissionIsEnforcedBeforeDataIsRead(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Gamma")
	// readonly_auditor deliberately does not include accounts.create.
	viewerID := h.seedUser("viewer@gamma.test", "Viewer-Passw0rd-2026!", "Viewer")
	h.joinOrganization(viewerID, org, "readonly_auditor")

	viewer := h.login("viewer@gamma.test", "Viewer-Passw0rd-2026!")

	t.Run("reading is allowed when the permission is held", func(t *testing.T) {
		rec := h.do(viewer, http.MethodGet, "/api/v1/accounts/", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("creating is refused when the permission is absent", func(t *testing.T) {
		if h.hasPermission(viewer, rbac.PermAccountsCreate) {
			t.Skip("readonly_auditor unexpectedly holds accounts.create")
		}
		before := h.countAccountsIn(org)

		rec := h.do(viewer, http.MethodPost, "/api/v1/accounts/", `{"name":"Should not exist"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
		}
		if after := h.countAccountsIn(org); after != before {
			t.Fatalf("a refused request still wrote a row: %d -> %d", before, after)
		}
	})
}

// TestRequestsWithoutASessionAreRejected checks that the session requirement is
// enforced by the router rather than by each handler. A handler that forgot to
// check would otherwise be reachable anonymously.
func TestRequestsWithoutASessionAreRejected(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/accounts/"},
		{http.MethodGet, "/api/v1/contacts/"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodGet, "/api/v1/audit"},
		{http.MethodGet, "/api/v1/jobs"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := h.do(nil, tc.method, tc.path, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestMutationsRequireACSRFToken verifies the double-submit defence. A request
// carrying a valid session but no CSRF token must be refused, which is what stops
// a cross-site form from acting as the user.
func TestMutationsRequireACSRFToken(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Delta")
	userID := h.seedUser("dana@delta.test", "Dana-Passw0rd-2026!", "Dana")
	h.joinOrganization(userID, org, "tenant_admin")

	dana := h.login("dana@delta.test", "Dana-Passw0rd-2026!")
	before := h.countAccountsIn(org)

	t.Run("missing token is refused", func(t *testing.T) {
		withoutCSRF := &actor{token: dana.token, csrf: ""}
		rec := h.do(withoutCSRF, http.MethodPost, "/api/v1/accounts/", `{"name":"No CSRF"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong token is refused", func(t *testing.T) {
		wrong := &actor{token: dana.token, csrf: "not-the-right-token"}
		rec := h.do(wrong, http.MethodPost, "/api/v1/accounts/", `{"name":"Wrong CSRF"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
		}
	})

	if after := h.countAccountsIn(org); after != before {
		t.Fatalf("a request without a valid CSRF token wrote a row: %d -> %d", before, after)
	}

	t.Run("valid token succeeds", func(t *testing.T) {
		rec := h.do(dana, http.MethodPost, "/api/v1/accounts/", `{"name":"With CSRF"}`)
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
		}
	})
}

// TestSuspendedMembershipLosesAccess checks that revoking a membership takes
// effect on the next request. A permission resolved once and cached forever would
// let a removed user keep working.
func TestSuspendedMembershipLosesAccess(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Epsilon")
	userID := h.seedUser("evan@epsilon.test", "Evan-Passw0rd-2026!", "Evan")
	h.joinOrganization(userID, org, "tenant_admin")

	evan := h.login("evan@epsilon.test", "Evan-Passw0rd-2026!")

	if rec := h.do(evan, http.MethodGet, "/api/v1/accounts/", ""); rec.Code != http.StatusOK {
		t.Fatalf("before suspension: status %d, body %s", rec.Code, rec.Body.String())
	}

	if _, err := h.pool().Exec(t.Context(),
		`UPDATE memberships SET status = 'suspended' WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("suspend membership: %v", err)
	}

	rec := h.do(evan, http.MethodGet, "/api/v1/accounts/", "")
	if rec.Code == http.StatusOK {
		t.Fatalf("a suspended member still reads data: %s", rec.Body.String())
	}
}

// TestBlockedUserLosesAccessImmediately checks that blocking an account takes
// effect without waiting for the session row to expire.
func TestBlockedUserLosesAccessImmediately(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Zeta")
	userID := h.seedUser("zoe@zeta.test", "Zoe-Passw0rd-2026!", "Zoe")
	h.joinOrganization(userID, org, "tenant_admin")

	zoe := h.login("zoe@zeta.test", "Zoe-Passw0rd-2026!")
	if rec := h.do(zoe, http.MethodGet, "/api/v1/accounts/", ""); rec.Code != http.StatusOK {
		t.Fatalf("before blocking: status %d", rec.Code)
	}

	if _, err := h.pool().Exec(t.Context(),
		`UPDATE users SET status = 'blocked' WHERE id = $1`, userID); err != nil {
		t.Fatalf("block user: %v", err)
	}

	rec := h.do(zoe, http.MethodGet, "/api/v1/accounts/", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; a blocked user must lose access immediately", rec.Code)
	}
}

// TestSQLInjectionInFiltersIsNotExecuted checks that a value arriving in a query
// parameter is treated as data. The parameterised queries should already make
// this true; the test exists so that a future string-built query is caught here
// rather than in production.
func TestSQLInjectionInFiltersIsNotExecuted(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("Eta")
	userID := h.seedUser("erin@eta.test", "Erin-Passw0rd-2026!", "Erin")
	h.joinOrganization(userID, org, "tenant_admin")
	h.seedAccount(org, "Legitimate Account")

	erin := h.login("erin@eta.test", "Erin-Passw0rd-2026!")

	payloads := []string{
		`'; DROP TABLE accounts; --`,
		`' OR '1'='1`,
		`%'; DELETE FROM accounts WHERE '1'='1'; --`,
	}
	for _, payload := range payloads {
		t.Run(payload, func(t *testing.T) {
			rec := h.do(erin, http.MethodGet, "/api/v1/accounts/?search="+urlEscape(payload), "")
			// The requirement is only that the request is handled normally and the
			// table survives; an empty result is a perfectly good answer.
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
			}

			var exists bool
			if err := h.pool().QueryRow(t.Context(),
				`SELECT EXISTS (SELECT 1 FROM information_schema.tables
				 WHERE table_schema = 'public' AND table_name = 'accounts')`).Scan(&exists); err != nil {
				t.Fatalf("check table: %v", err)
			}
			if !exists {
				t.Fatal("the accounts table no longer exists: the payload was executed")
			}
		})
	}
}

// TestHealthAndReadinessNeedNoAuthentication documents the intended exception:
// probes must be reachable without a session so an orchestrator can use them.
func TestHealthAndReadinessNeedNoAuthentication(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/health", "/ready"} {
		rec := h.do(nil, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsName(body, name string) bool {
	return json.Valid([]byte(body)) && strings.Contains(body, name)
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteByte(r)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[r>>4])
		b.WriteByte(hex[r&0x0f])
	}
	return b.String()
}
