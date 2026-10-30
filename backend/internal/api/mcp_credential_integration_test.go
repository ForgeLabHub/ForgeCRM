//go:build integration

// Integration tests for the MCP credential HTTP surface. They exercise the real
// router, the real middleware and the real database, because what matters here is
// wiring: that the protocol endpoint accepts a bearer credential, that a revoked
// credential stops working at the HTTP boundary, and that the lifecycle routes are
// permission-checked rather than merely hidden.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// jsonBody wraps a JSON string as a request body reader.
func jsonBody(s string) *strings.Reader { return strings.NewReader(s) }

// containsAll reports whether haystack contains every needle.
func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}

// credHarness is a tenant with an administrator who can manage credentials.
type credHarness struct {
	*harness
	admin *actor
}

func newCredHarness(t *testing.T) *credHarness {
	t.Helper()
	h := newHarness(t)
	org := h.seedOrganization("Credential Co")
	userID := h.seedUser("admin@credential.test", "correct-horse-battery", "Admin")
	h.joinOrganization(userID, org, "platform_admin")
	return &credHarness{harness: h, admin: h.login("admin@credential.test", "correct-horse-battery")}
}

// mint creates a credential through the API and returns the plaintext token.
func (c *credHarness) mint(t *testing.T, name string, perms []string) (string, uuid.UUID) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "permissions": perms})
	rec := c.do(c.admin, http.MethodPost, "/api/v1/mcp/credentials", string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint credential: status %d, body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Credential struct {
			ID uuid.UUID `json:"id"`
		} `json:"credential"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode created credential: %v (body %s)", err, rec.Body.String())
	}
	if out.Token == "" {
		t.Fatal("the create response carried no token")
	}
	return out.Token, out.Credential.ID
}

// TestMCPCredentialLifecycleOverHTTP drives create, list, rotate and revoke through
// the real routes with a session cookie and CSRF token.
func TestMCPCredentialLifecycleOverHTTP(t *testing.T) {
	c := newCredHarness(t)

	token, id := c.mint(t, "laptop agent", []string{"accounts.read"})

	// The list shows the credential but never the secret.
	rec := c.do(c.admin, http.MethodGet, "/api/v1/mcp/credentials", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d, body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !containsAll(body, `"token_prefix"`, id.String()) || containsAll(body, token) {
		t.Fatalf("list body is wrong or leaks the token: %s", body)
	}

	// Rotate issues a new token; the old one stops working.
	rec = c.do(c.admin, http.MethodPost, "/api/v1/mcp/credentials/"+id.String()+"/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: status %d, body %s", rec.Code, rec.Body.String())
	}
	var rotated struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("decode rotated credential: %v", err)
	}
	if rotated.Token == "" || rotated.Token == token {
		t.Fatalf("rotate returned an empty or unchanged token")
	}

	// Revoke, then confirm the route reports not-found on a second attempt.
	rec = c.do(c.admin, http.MethodDelete, "/api/v1/mcp/credentials/"+id.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status %d, body %s", rec.Code, rec.Body.String())
	}
	rec = c.do(c.admin, http.MethodDelete, "/api/v1/mcp/credentials/"+id.String(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second revoke: status %d, want 404", rec.Code)
	}
}

// TestMCPProtocolAcceptsBearerCredential is the property the credential exists for:
// the protocol endpoint authenticates a client that has no session.
func TestMCPProtocolAcceptsBearerCredential(t *testing.T) {
	c := newCredHarness(t)
	token, _ := c.mint(t, "headless client", []string{"accounts.read", "ai.tools.execute"})

	// A tools/list call is the simplest protocol request; it needs no permission
	// beyond being authenticated.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp",
		jsonBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list with a credential: status %d, body %s", rec.Code, rec.Body.String())
	}

	// With no credential and no session the same request is refused.
	rec = httptest.NewRecorder()
	c.server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mcp",
		jsonBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tools/list without a credential: status %d, want 401", rec.Code)
	}
}

// TestMCPProtocolRejectsRevokedCredential confirms revocation takes effect at the
// HTTP boundary, not merely in the store.
func TestMCPProtocolRejectsRevokedCredential(t *testing.T) {
	c := newCredHarness(t)
	token, id := c.mint(t, "revocable client", []string{"accounts.read", "ai.tools.execute"})

	if rec := c.do(c.admin, http.MethodDelete, "/api/v1/mcp/credentials/"+id.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status %d, body %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp",
		jsonBody(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked credential: status %d, want 401", rec.Code)
	}
}

// TestMCPProtocolCredentialCannotEscalate confirms a credential scoped to reads
// cannot invoke a write tool even though its owner is an administrator. This is the
// escalation guard the whole model turns on.
func TestMCPProtocolCredentialCannotEscalate(t *testing.T) {
	c := newCredHarness(t)
	token, _ := c.mint(t, "read only", []string{"accounts.read", "ai.tools.execute"})

	// create_task needs tasks.create, which the credential was not granted.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/call",
		jsonBody(`{"tool":"create_task","arguments":{"title":"escalation attempt"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	c.server.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("a read-only credential invoked a write tool: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestMCPCredentialRoutesRequirePermission confirms a user without the manage
// permission cannot mint a credential, even though they are authenticated.
func TestMCPCredentialRoutesRequirePermission(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("No Credential Co")
	// A support agent has CRM access but not credential management.
	userID := h.seedUser("agent@nocred.test", "correct-horse-battery", "Agent")
	h.joinOrganization(userID, org, "support_agent")
	agent := h.login("agent@nocred.test", "correct-horse-battery")

	rec := h.do(agent, http.MethodPost, "/api/v1/mcp/credentials",
		`{"name":"sneaky","permissions":["accounts.read"]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mint without permission: status %d, want 403", rec.Code)
	}
}

// TestMCPCredentialIsolationBetweenOrganizations confirms one tenant cannot see or
// revoke another tenant's credentials through the API.
func TestMCPCredentialIsolationBetweenOrganizations(t *testing.T) {
	h := newHarness(t)

	orgA := h.seedOrganization("Tenant A")
	userA := h.seedUser("a@iso.test", "correct-horse-battery", "A")
	h.joinOrganization(userA, orgA, "tenant_admin")
	adminA := h.login("a@iso.test", "correct-horse-battery")

	orgB := h.seedOrganization("Tenant B")
	userB := h.seedUser("b@iso.test", "correct-horse-battery", "B")
	h.joinOrganization(userB, orgB, "tenant_admin")
	adminB := h.login("b@iso.test", "correct-horse-battery")

	ca := &credHarness{harness: h, admin: adminA}
	_, id := ca.mint(t, "tenant a credential", []string{"accounts.read"})

	// Tenant B lists nothing and cannot revoke tenant A's credential.
	rec := h.do(adminB, http.MethodGet, "/api/v1/mcp/credentials", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant B list: status %d, body %s", rec.Code, rec.Body.String())
	}
	if containsAll(rec.Body.String(), id.String()) {
		t.Fatal("tenant B can see tenant A's credential")
	}
	rec = h.do(adminB, http.MethodDelete, "/api/v1/mcp/credentials/"+id.String(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant B revoke of tenant A's credential: status %d, want 404", rec.Code)
	}
}
