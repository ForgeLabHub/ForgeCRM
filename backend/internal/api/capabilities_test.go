package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
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
	srv := New(&Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
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

// TestCapabilitiesToolAPIIsNotCalledMCP pins the same honesty for the tool surface.
//
// The endpoint exposes tools/list and tools/call over HTTP. It is not a full MCP
// server: there is no initialize handshake, no protocol version negotiation and no
// stdio or SSE transport, so an off-the-shelf MCP client cannot connect. Reporting
// `mcp: true` would tell an integrator otherwise.
func TestCapabilitiesToolAPIIsNotCalledMCP(t *testing.T) {
	cfg := &config.Config{}
	srv := New(&Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})

	rec := httptest.NewRecorder()
	srv.handleCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if _, ok := body["mcp"]; ok {
		t.Error(`the response contains "mcp", which reads as "a full MCP server is available"; ` +
			`only tools/list and tools/call exist, so the field must be named "tool_api"`)
	}
	if _, ok := body["tool_api"]; !ok {
		t.Error(`the response has no "tool_api" field`)
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
	srv := New(&Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})

	rec := httptest.NewRecorder()
	srv.handleCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{"mfa", "webhooks", "tool_api"} {
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
