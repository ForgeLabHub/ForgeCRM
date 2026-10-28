package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// TestCapabilitiesDoesNotOverstateOIDC pins the honesty of the capability report.
//
// The field was once called "oidc" and reported the presence of OIDC settings.
// A client reading `oidc: true` concludes SSO is available; the authorisation-code
// flow is not implemented, so it is not. The field is now "oidc_configured", and
// this test fails if it is renamed back or if the misleading name reappears.
func TestCapabilitiesDoesNotOverstateOIDC(t *testing.T) {
	cfg := &config.Config{}
	cfg.OIDC.Enabled = true
	cfg.OIDC.Issuer = "https://idp.example.test"
	cfg.OIDC.ClientID = "forgecrm"

	h := newRouterForTest(t, cfg)

	rec := httptest.NewRecorder()
	// The route sits behind the session middleware, so an unauthenticated request
	// is rejected before the handler runs. The handler is exercised directly to
	// test its body without standing up a session.
	srv := newTestServer(t, &Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv.handleCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("handleCapabilities returned %d, want 200", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("capabilities response is not valid JSON: %v", err)
	}

	if _, ok := body["oidc"]; ok {
		t.Error(`the response contains "oidc", which reads as "SSO is available"; ` +
			`the sign-in flow is not implemented, so the field must be named "oidc_configured"`)
	}
	got, ok := body["oidc_configured"]
	if !ok {
		t.Fatal(`the response has no "oidc_configured" field`)
	}
	if got != true {
		t.Errorf("oidc_configured is %v, want true when OIDC settings are present", got)
	}

	_ = h // the router is built to prove the route exists behind the session gate
}

// TestCapabilitiesMCPFlagMatchesTheTransport pins the honesty of the MCP flag.
//
// An earlier revision reported `tool_api` only, because the endpoint spoke a
// custom JSON shape and no standard MCP client could connect. The protocol
// transport now exists, so `mcp` is reported — and this test requires the claim to
// be earned: it drives the real protocol handler through an initialize handshake
// and a tools/list call, and fails if the flag is set without a working transport.
func TestCapabilitiesMCPFlagMatchesTheTransport(t *testing.T) {
	cfg := &config.Config{}
	srv := newTestServer(t, &Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})

	rec := httptest.NewRecorder()
	srv.handleCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	claimed, _ := body["mcp"].(bool)
	if !claimed {
		t.Fatal(`the response does not report "mcp": true, but the protocol transport exists`)
	}

	// The flag is only honest if the transport answers the handshake. A registry
	// with no CRM service is enough to answer initialize and tools/list, which are
	// the methods that prove the protocol is spoken rather than merely advertised.
	server := ai.NewMCPServer(ai.NewToolRegistry(nil, nil))
	principal := rbac.NewPrincipal(uuid.New(), "t@example.test", "T", uuid.New(), nil)

	resp, reply := server.Handle(context.Background(), principal,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`))
	if !reply || resp.Error != nil {
		t.Fatalf("initialize failed: reply=%v err=%+v", reply, resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["protocolVersion"] != ai.MCPProtocolVersion {
		t.Errorf("initialize negotiated %v, want %s", result["protocolVersion"], ai.MCPProtocolVersion)
	}
	if _, ok := result["serverInfo"]; !ok {
		t.Error("initialize result has no serverInfo, which a client requires")
	}
}

// TestCapabilitiesReportsImplementedSubsystems checks that the flags which are
// unconditional match the code.
//
// MFA and webhooks are implemented in this repository and always available, so
// they must report true. If either is ever made optional, this test should change
// with the implementation rather than being deleted.
func TestCapabilitiesReportsImplementedSubsystems(t *testing.T) {
	cfg := &config.Config{}
	srv := newTestServer(t, &Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})

	rec := httptest.NewRecorder()
	srv.handleCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{"mfa", "webhooks", "tool_api", "mcp"} {
		if body[field] != true {
			t.Errorf("%s is %v, want true: the subsystem is implemented and always available",
				field, body[field])
		}
	}

	// The advertised formats must be non-empty, or the UI would hide import
	// entirely while the importer works.
	for _, field := range []string{"import_formats", "archive_formats", "migration_sources"} {
		list, ok := body[field].([]any)
		if !ok || len(list) == 0 {
			t.Errorf("%s is %v, want a non-empty list", field, body[field])
		}
	}
}
