//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/geff1778/forgecrm/internal/rbac"
)

// These tests drive the MCP protocol through the real HTTP stack: a real session
// cookie, the real permission middleware and the real tool registry. They are the
// counterpart to the unit tests in internal/ai, which prove the transport in
// isolation.
//
// The distinction matters here more than usual. A unit test can show that the
// protocol decodes a tools/call message correctly; only an HTTP-level test can
// show that the session middleware has resolved the right principal by the time
// the tool runs, and that a caller without the tool's permission is refused
// rather than served. MCP is a new way to reach the same operations, so it is a
// new way to reach them without authorization if the wiring is wrong.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/api/...

// mcpJSONRPC sends one JSON-RPC message to the protocol endpoint and decodes the
// reply.
func (h *harness) mcpJSONRPC(a *actor, body string) (map[string]any, *httptestResponse) {
	h.t.Helper()
	rec := h.do(a, http.MethodPost, "/api/v1/mcp", body)
	var decoded map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			h.t.Fatalf("decode MCP response: %v (status %d, body %s)", err, rec.Code, rec.Body.String())
		}
	}
	return decoded, &httptestResponse{code: rec.Code, body: rec.Body.String()}
}

type httptestResponse struct {
	code int
	body string
}

// mcpErrorCode reads the JSON-RPC error code from a decoded response.
func mcpErrorCode(t *testing.T, resp map[string]any) int {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("the response has no error member: %v", resp)
	}
	code, ok := errObj["code"].(float64)
	if !ok {
		t.Fatalf("the error has no numeric code: %v", errObj)
	}
	return int(code)
}

// TestMCPProtocolHandshakeOverHTTP proves a standard client can connect.
//
// This is the claim the `mcp` capability flag makes. It is checked here, against
// the running server, rather than by reading the source.
func TestMCPProtocolHandshakeOverHTTP(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("MCP Handshake")
	userID := h.seedUser("mcp@handshake.test", "MCP-Handshake-2026!", "MCP User")
	// sales_manager, not tenant_admin: the tenant administrator template excludes
	// the whole `ai` category by design, so it does not hold ai.tools.execute and
	// this test would fail on the fixture rather than on the transport.
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("mcp@handshake.test", "MCP-Handshake-2026!")

	if !h.hasPermission(user, rbac.PermAIToolsExecute) {
		t.Fatal("the fixture did not grant ai.tools.execute; the role template is missing it")
	}

	t.Run("initialize negotiates a protocol version", func(t *testing.T) {
		resp, meta := h.mcpJSONRPC(user,
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
		if meta.code != http.StatusOK && meta.code != http.StatusCreated {
			t.Fatalf("status %d, body %s", meta.code, meta.body)
		}
		if _, hasError := resp["error"]; hasError {
			t.Fatalf("initialize returned an error: %v", resp["error"])
		}
		result, _ := resp["result"].(map[string]any)
		if result == nil {
			t.Fatalf("initialize returned no result: %v", resp)
		}
		if result["protocolVersion"] == nil {
			t.Error("initialize did not report a protocol version")
		}
		if _, ok := result["serverInfo"]; !ok {
			t.Error("initialize did not report serverInfo")
		}
	})

	t.Run("the request id is echoed back", func(t *testing.T) {
		resp, _ := h.mcpJSONRPC(user, `{"jsonrpc":"2.0","id":"abc","method":"ping"}`)
		if resp["id"] != "abc" {
			t.Errorf("id came back as %v, want abc", resp["id"])
		}
	})

	t.Run("tools/list returns the catalogue", func(t *testing.T) {
		resp, _ := h.mcpJSONRPC(user, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		result, _ := resp["result"].(map[string]any)
		tools, _ := result["tools"].([]any)
		if len(tools) == 0 {
			t.Fatalf("tools/list returned nothing: %v", resp)
		}
		first, _ := tools[0].(map[string]any)
		if _, ok := first["inputSchema"]; !ok {
			t.Errorf("a tool has no inputSchema in the protocol shape: %v", first)
		}
	})

	t.Run("a notification is acknowledged without a body", func(t *testing.T) {
		rec := h.do(user, http.MethodPost, "/api/v1/mcp",
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		if rec.Code != http.StatusAccepted {
			t.Errorf("status %d, want 202 for a notification", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("a notification received a body: %s", rec.Body.String())
		}
	})
}

// TestMCPRequiresASession checks that the protocol is not an open door.
//
// An unauthenticated caller must be refused before any tool logic runs. A
// protocol endpoint that answers an anonymous caller would expose the whole CRM
// to anyone who can reach the port.
func TestMCPRequiresASession(t *testing.T) {
	h := newHarness(t)

	t.Run("initialize without a session is refused", func(t *testing.T) {
		rec := h.do(nil, http.MethodPost, "/api/v1/mcp",
			`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status %d, want 401", rec.Code)
		}
	})

	t.Run("tools/list without a session is refused", func(t *testing.T) {
		rec := h.do(nil, http.MethodPost, "/api/v1/mcp",
			`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status %d, want 401", rec.Code)
		}
	})

	t.Run("a tool call without a session is refused", func(t *testing.T) {
		rec := h.do(nil, http.MethodPost, "/api/v1/mcp",
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_accounts","arguments":{"query":"acme"}}}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status %d, want 401", rec.Code)
		}
	})
}

// TestMCPEnforcesTheToolsPermission is the escalation check.
//
// A user without ai.tools.execute must not reach a tool by speaking the protocol
// instead of the REST API. The registry denies it; this proves the denial survives
// the HTTP round trip, which is where a missing middleware would show up.
func TestMCPEnforcesTheToolsPermission(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("MCP Denied")
	// readonly_auditor is the narrowest role in the system; it must not hold
	// ai.tools.execute, or this test would prove nothing.
	userID := h.seedUser("denied@mcp.test", "MCP-Denied-2026!", "Denied User")
	h.joinOrganization(userID, org, "readonly_auditor")
	user := h.login("denied@mcp.test", "MCP-Denied-2026!")

	if h.hasPermission(user, rbac.PermAIToolsExecute) {
		t.Fatal("readonly_auditor holds ai.tools.execute; the fixture is wrong for this test")
	}

	rec := h.do(user, http.MethodPost, "/api/v1/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_accounts","arguments":{"query":"acme"}}}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: a caller without ai.tools.execute reached a tool. body %s",
			rec.Code, rec.Body.String())
	}
}

// TestMCPToolCallReachesTheSameDataAsTheAPI checks that a tool returns the
// caller's own tenant's data and nothing else.
//
// A tool that returned another organization's accounts would be a data leak
// through a new surface. The account is seeded for this tenant, so a correct
// result contains it; an empty result would mean the tool is not wired to the CRM
// service at all, which is the other way this test can fail.
func TestMCPToolCallReachesTheSameDataAsTheAPI(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("MCP Data")
	otherOrg := h.seedOrganization("MCP Other")

	userID := h.seedUser("data@mcp.test", "MCP-Data-2026!", "Data User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("data@mcp.test", "MCP-Data-2026!")

	if !h.hasPermission(user, rbac.PermAIToolsExecute) {
		t.Fatal("the fixture did not grant ai.tools.execute")
	}

	h.seedAccount(org, "Findable Account")
	h.seedAccount(otherOrg, "Foreign Account")

	resp, meta := h.mcpJSONRPC(user,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_accounts","arguments":{"query":"Account"}}}`)
	if meta.code != http.StatusOK && meta.code != http.StatusCreated {
		t.Fatalf("status %d, body %s", meta.code, meta.body)
	}
	if _, hasError := resp["error"]; hasError {
		t.Fatalf("search_accounts returned an error: %v", resp["error"])
	}

	encoded, _ := json.Marshal(resp)
	if !containsName(string(encoded), "Findable Account") {
		t.Errorf("the tool did not return the caller's own account: %s", encoded)
	}
	if containsName(string(encoded), "Foreign Account") {
		t.Errorf("the tool returned another organization's account: %s", encoded)
	}
}

// TestMCPMutatingToolRequiresConfirmation checks the acknowledgement over HTTP.
//
// create_task writes to the database. Without confirm=true it must be refused and
// must not write; with confirm=true it must succeed. Checking only the refusal
// would leave open the possibility that the tool never writes at all.
func TestMCPMutatingToolRequiresConfirmation(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("MCP Confirm")
	userID := h.seedUser("confirm@mcp.test", "MCP-Confirm-2026!", "Confirm User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("confirm@mcp.test", "MCP-Confirm-2026!")

	if !h.hasPermission(user, rbac.PermAIToolsExecute) {
		t.Fatal("the fixture did not grant ai.tools.execute")
	}

	countTasks := func() int {
		var n int
		if err := h.pool().QueryRow(context.Background(),
			`SELECT count(*) FROM tasks WHERE organization_id = $1`, org).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}

	t.Run("without confirmation the call is refused and writes nothing", func(t *testing.T) {
		before := countTasks()
		resp, _ := h.mcpJSONRPC(user,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Unconfirmed"}}}`)
		if _, hasError := resp["error"]; !hasError {
			t.Fatalf("a mutating tool ran without confirmation: %v", resp)
		}
		if after := countTasks(); after != before {
			t.Errorf("a refused call wrote a task: %d before, %d after", before, after)
		}
	})

	t.Run("with confirmation the task is created", func(t *testing.T) {
		before := countTasks()
		resp, meta := h.mcpJSONRPC(user,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"Confirmed Task","confirm":true}}}`)
		if _, hasError := resp["error"]; hasError {
			t.Fatalf("a confirmed call failed: %v (status %d)", resp["error"], meta.code)
		}
		if after := countTasks(); after != before+1 {
			t.Errorf("task count is %d, want %d", after, before+1)
		}
	})
}

// TestMCPToolCatalogueEndpointMatchesTheProtocol checks the two views agree.
//
// The admin UI reads /api/v1/mcp/tools and the protocol client reads tools/list.
// They name the schema field differently on purpose, but they must describe the
// same tools, or one interface offers something the other refuses.
func TestMCPToolCatalogueEndpointMatchesTheProtocol(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("MCP Catalogue")
	userID := h.seedUser("cat@mcp.test", "MCP-Catalogue-2026!", "Catalogue User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("cat@mcp.test", "MCP-Catalogue-2026!")

	rec := h.do(user, http.MethodGet, "/api/v1/mcp/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	var catalogue struct {
		Tools []struct {
			Name       string `json:"name"`
			Dangerous  bool   `json:"dangerous"`
			Permission string `json:"permission"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalogue); err != nil {
		t.Fatalf("decode catalogue: %v (body %s)", err, rec.Body.String())
	}
	if len(catalogue.Tools) == 0 {
		t.Fatal("the catalogue is empty")
	}

	resp, _ := h.mcpJSONRPC(user, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result, _ := resp["result"].(map[string]any)
	protocolTools, _ := result["tools"].([]any)

	protocolNames := map[string]bool{}
	for _, raw := range protocolTools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		protocolNames[name] = true
	}

	for _, tool := range catalogue.Tools {
		if !protocolNames[tool.Name] {
			t.Errorf("tool %q is in the catalogue but not in the protocol list", tool.Name)
		}
	}
	if len(protocolNames) != len(catalogue.Tools) {
		t.Errorf("the protocol lists %d tools and the catalogue %d; they must agree",
			len(protocolNames), len(catalogue.Tools))
	}
}

// TestMCPRejectsOversizedBodies checks the bound on the request body.
func TestMCPRejectsOversizedBodies(t *testing.T) {
	h := newHarness(t)

	org := h.seedOrganization("MCP Oversized")
	userID := h.seedUser("big@mcp.test", "MCP-Oversized-2026!", "Big User")
	h.joinOrganization(userID, org, "sales_manager")
	user := h.login("big@mcp.test", "MCP-Oversized-2026!")

	// A body larger than the endpoint's limit. The padding makes it exceed the
	// bound; the message is otherwise well-formed.
	big := make([]byte, 1<<20+64)
	for i := range big {
		big[i] = 'x'
	}
	rec := h.do(user, http.MethodPost, "/api/v1/mcp", string(big))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d, want 413", rec.Code)
	}
}
