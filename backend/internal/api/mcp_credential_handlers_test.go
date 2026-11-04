package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBearerTokenParsing checks the header parsing in isolation. A credential is
// only ever read from a well-formed `Bearer` header; anything else is treated as no
// credential, so a malformed header cannot be mistaken for a valid one.
func TestBearerTokenParsing(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"absent", "", ""},
		{"well formed", "Bearer fcmcp_abc123", "fcmcp_abc123"},
		{"lowercase scheme", "bearer fcmcp_abc123", "fcmcp_abc123"},
		{"surrounding space", "Bearer   fcmcp_abc123  ", "fcmcp_abc123"},
		{"wrong scheme", "Basic dXNlcjpwYXNz", ""},
		{"scheme only", "Bearer ", ""},
		{"scheme without separator", "Bearer", ""},
		{"raw token", "fcmcp_abc123", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := bearerToken(r); got != tc.want {
				t.Fatalf("bearerToken(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestMCPTokenHeaderPrecedence(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", nil)
	r.Header.Set("X-API-Key", " fcmcp_key ")
	if got := mcpToken(r); got != "fcmcp_key" {
		t.Fatalf("API key = %q", got)
	}
	r.Header.Set("Authorization", "Bearer fcmcp_bearer")
	if got := mcpToken(r); got != "fcmcp_bearer" {
		t.Fatalf("bearer = %q", got)
	}
	r.Header.Set("Authorization", "Basic invalid")
	if got := mcpToken(r); got != "" {
		t.Fatalf("malformed Authorization must not fall back: %q", got)
	}
}
