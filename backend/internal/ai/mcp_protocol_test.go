package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/rbac"
)

// newTestMCP builds a protocol server over a registry with no CRM service.
//
// The methods exercised here — initialize, ping, tools/list and the rejection
// paths of tools/call — do not reach the database. A test that needed a live CRM
// to prove the *transport* would be testing the wrong layer.
func newTestMCP() *MCPServer {
	return NewMCPServer(NewToolRegistry(nil, nil))
}

func testPrincipal(perms ...rbac.Permission) *rbac.Principal {
	return rbac.NewPrincipal(uuid.New(), "agent@example.test", "Agent", uuid.New(), perms)
}

// call sends one message and decodes the response.
func call(t *testing.T, s *MCPServer, p *rbac.Principal, body string) (Response, bool) {
	t.Helper()
	return s.Handle(context.Background(), p, []byte(body))
}

// TestInitializeNegotiatesTheRequestedVersion checks the handshake.
//
// The specification requires the server to echo a version it supports, so a
// client learns what it is talking to instead of assuming it got what it asked
// for. An unsupported version is answered with the server's own, not with an
// error: refusing would lock out a client whose messages are otherwise understood.
func TestInitializeNegotiatesTheRequestedVersion(t *testing.T) {
	s := newTestMCP()
	p := testPrincipal()

	t.Run("a supported version is echoed", func(t *testing.T) {
		resp, reply := call(t, s, p,
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
		if !reply {
			t.Fatal("initialize must produce a response")
		}
		got := resultMap(t, resp)["protocolVersion"]
		if got != "2024-11-05" {
			t.Errorf("negotiated %v, want the requested 2024-11-05", got)
		}
	})

	t.Run("an unsupported version falls back to the server's own", func(t *testing.T) {
		resp, _ := call(t, s, p,
			`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
		if resp.Error != nil {
			t.Fatalf("an unsupported version must not be an error: %+v", resp.Error)
		}
		if got := resultMap(t, resp)["protocolVersion"]; got != MCPProtocolVersion {
			t.Errorf("negotiated %v, want the server's own %s", got, MCPProtocolVersion)
		}
	})

	t.Run("the result carries the fields a client requires", func(t *testing.T) {
		resp, _ := call(t, s, p, `{"jsonrpc":"2.0","id":3,"method":"initialize"}`)
		result := resultMap(t, resp)
		for _, field := range []string{"protocolVersion", "capabilities", "serverInfo"} {
			if _, ok := result[field]; !ok {
				t.Errorf("initialize result has no %q", field)
			}
		}
		caps, _ := result["capabilities"].(map[string]any)
		if _, ok := caps["tools"]; !ok {
			t.Error("capabilities does not advertise tools, which are implemented")
		}
		// Advertising a capability that is not implemented would have a client call
		// a method this server answers with "not implemented".
		for _, absent := range []string{"resources", "prompts"} {
			if _, ok := caps[absent]; ok {
				t.Errorf("capabilities advertises %q, but no such method is implemented", absent)
			}
		}
	})
}

// TestNotificationGetsNoReply checks the framing rule that a client enforces
// strictly: a message with no id must not be answered.
func TestNotificationGetsNoReply(t *testing.T) {
	s := newTestMCP()
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":null,"method":"notifications/initialized"}`,
	} {
		if _, reply := call(t, s, testPrincipal(), body); reply {
			t.Errorf("a notification was answered: %s", body)
		}
	}
}

// TestRequestIDIsEchoedVerbatim checks that the id survives unchanged.
//
// The specification allows a string or a number. Decoding it into any Go type
// would turn a large integer into a float and break the client's correlation, so
// the raw form is preserved.
func TestRequestIDIsEchoedVerbatim(t *testing.T) {
	s := newTestMCP()
	for _, id := range []string{`1`, `"abc"`, `9007199254740993`, `0`} {
		resp, _ := call(t, s, testPrincipal(), `{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`)
		if string(resp.ID) != id {
			t.Errorf("id %s came back as %s", id, resp.ID)
		}
	}
}

// TestProtocolErrorsAreClassified checks that each failure gets the code the
// specification assigns it, so a client can tell a broken envelope from a bad
// argument.
func TestProtocolErrorsAreClassified(t *testing.T) {
	s := newTestMCP()
	cases := []struct {
		name string
		body string
		want int
	}{
		{"malformed JSON", `{`, CodeParseError},
		{"a wrong jsonrpc version", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, CodeInvalidRequest},
		{"no method", `{"jsonrpc":"2.0","id":1}`, CodeInvalidRequest},
		{"an unknown method", `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, CodeMethodNotFound},
		{"an unknown tool", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope"}}`, CodeMethodNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, reply := call(t, s, testPrincipal(), tc.body)
			if !reply {
				t.Fatal("an error must be reported, not treated as a notification")
			}
			if resp.Error == nil {
				t.Fatalf("expected an error, got result %v", resp.Result)
			}
			if resp.Error.Code != tc.want {
				t.Errorf("code %d, want %d (%s)", resp.Error.Code, tc.want, resp.Error.Message)
			}
		})
	}
}

// TestToolsListUsesTheProtocolFieldNames checks the published schema.
//
// MCP names the field `inputSchema`. The admin UI reads `input_schema` from a
// different endpoint. Serving one shape to both would leave one of them silently
// unable to find the schema.
func TestToolsListUsesTheProtocolFieldNames(t *testing.T) {
	s := newTestMCP()
	resp, _ := call(t, s, testPrincipal(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %+v", resp.Error)
	}
	result := resultMap(t, resp)
	tools, ok := result["tools"].([]map[string]any)
	if !ok || len(tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}
	for _, tool := range tools {
		if _, ok := tool["inputSchema"]; !ok {
			t.Errorf("tool %v has no inputSchema", tool["name"])
		}
		if _, ok := tool["input_schema"]; ok {
			t.Errorf("tool %v uses input_schema, which is not the protocol field name", tool["name"])
		}
		// A null schema is rejected by some clients, so it must be an object.
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Errorf("tool %v has a non-object inputSchema", tool["name"])
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %v schema type is %v, want object", tool["name"], schema["type"])
		}
	}
}

// TestMutatingToolsPublishTheirConfirmationRequirement checks that the
// acknowledgement a write requires is visible before the call.
//
// The check exists in the server, but a client that cannot see it receives a
// refusal it cannot explain. Publishing it in the schema is what makes the
// requirement discoverable rather than a trap.
func TestMutatingToolsPublishTheirConfirmationRequirement(t *testing.T) {
	s := newTestMCP()
	resp, _ := call(t, s, testPrincipal(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result := resultMap(t, resp)
	tools, ok := result["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools is %T, want a list of objects", result["tools"])
	}

	checkedMutating := 0
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if !isDangerous(s, name) {
			continue
		}
		checkedMutating++
		schema, _ := tool["inputSchema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props["confirm"]; !ok {
			t.Errorf("mutating tool %q does not publish a confirm argument", name)
		}
		required, _ := schema["required"].([]string)
		found := false
		for _, r := range required {
			if r == "confirm" {
				found = true
			}
		}
		if !found {
			t.Errorf("mutating tool %q does not list confirm as required", name)
		}
	}
	// A guard against a vacuous pass: if no tool were dangerous, the loop above
	// would assert nothing and still report success.
	if checkedMutating == 0 {
		t.Fatal("no mutating tool was found; the test proved nothing")
	}
}

// TestMutatingToolRefusesWithoutConfirmation checks the rule at the call site.
func TestMutatingToolRefusesWithoutConfirmation(t *testing.T) {
	s := newTestMCP()
	p := testPrincipal(rbac.PermTasksCreate)

	resp, _ := call(t, s, p,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_task","arguments":{"title":"x"}}}`)
	if resp.Error == nil {
		t.Fatal("a mutating tool without confirm must be refused")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("code %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

// TestToolPermissionIsEnforcedThroughTheProtocol checks that the protocol is not
// a privilege escalation path.
//
// The registry denies a caller without the tool's permission. Reaching the same
// registry through JSON-RPC must produce the same denial; if it did not, speaking
// the protocol would be a way around the REST API's authorization.
func TestToolPermissionIsEnforcedThroughTheProtocol(t *testing.T) {
	s := newTestMCP()
	// No permissions at all.
	resp, _ := call(t, s, testPrincipal(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_accounts","arguments":{"query":"acme"}}}`)
	if resp.Error == nil {
		t.Fatal("a caller without the tool's permission was allowed to call it")
	}
	// A permission failure must not be reported as a bad argument: that would tell
	// the client to fix its request when the problem is its authority.
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("code %d, want %d for a permission failure", resp.Error.Code, CodeInvalidRequest)
	}
	data, _ := resp.Error.Data.(map[string]any)
	// The status is an int in-process and a number once serialised, so accept
	// either rather than pinning the representation.
	switch got := data["status"].(type) {
	case int:
		if got != 403 {
			t.Errorf("error data status is %v, want 403", got)
		}
	case float64:
		if got != 403 {
			t.Errorf("error data status is %v, want 403", got)
		}
	default:
		t.Errorf("error data has no status: %+v", resp.Error.Data)
	}
}

// TestUnknownToolIsRefused checks the not-found path.
func TestUnknownToolIsRefused(t *testing.T) {
	s := newTestMCP()
	resp, _ := call(t, s, testPrincipal(rbac.PermTasksCreate),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"drop_table"}}`)
	if resp.Error == nil {
		t.Fatal("an unknown tool must be refused")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("code %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

// TestMCPResponseNeverCarriesBothResultAndError checks the envelope invariant.
func TestMCPResponseNeverCarriesBothResultAndError(t *testing.T) {
	s := newTestMCP()
	bodies := []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`,
		`{`,
	}
	for _, body := range bodies {
		resp, reply := call(t, s, testPrincipal(), body)
		if !reply {
			continue
		}
		if resp.JSONRPC != "2.0" {
			t.Errorf("%s: jsonrpc is %q, want 2.0", body, resp.JSONRPC)
		}
		if len(resp.ID) == 0 {
			t.Errorf("%s: a response has no id member", body)
		}
		if resp.Result != nil && resp.Error != nil {
			t.Errorf("%s: a response carries both result and error", body)
		}
		if resp.Result == nil && resp.Error == nil {
			t.Errorf("%s: a response carries neither result nor error", body)
		}
	}
}

func resultMap(t *testing.T, resp Response) map[string]any {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is %T, want an object", resp.Result)
	}
	return m
}

func isDangerous(s *MCPServer, name string) bool {
	tool, ok := s.tools.Get(name)
	return ok && tool.Dangerous
}

// TestMCPDescriptorsDoNotMutateTheToolSchema checks that publishing the protocol
// view does not change the schema the admin UI reads.
//
// The confirmation argument is added to the protocol shape. Adding it in place
// would leak it into the other endpoint, which has no confirmation flag, and the
// two views would then disagree about the same tool.
func TestMCPDescriptorsDoNotMutateTheToolSchema(t *testing.T) {
	r := NewToolRegistry(nil, nil)
	before := len(r.Descriptors())

	r.MCPDescriptors()
	r.MCPDescriptors()

	after := r.Descriptors()
	if len(after) != before {
		t.Fatalf("the tool count changed from %d to %d", before, len(after))
	}
	for _, d := range after {
		if d["dangerous"] != true {
			continue
		}
		schema, _ := d["input_schema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props["confirm"]; ok {
			t.Errorf("tool %v: the confirm argument leaked into the admin schema", d["name"])
		}
	}
}

// TestDescriptorJSONIsStable is a light guard that the descriptors serialise.
func TestDescriptorJSONIsStable(t *testing.T) {
	s := newTestMCP()
	resp, _ := call(t, s, testPrincipal(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if _, err := json.Marshal(resp); err != nil {
		t.Fatalf("the response does not serialise: %v", err)
	}
}
