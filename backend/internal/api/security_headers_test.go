package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// TestHardeningHeadersAreOnEveryResponseThroughTheRouter checks the security
// headers are attached by the router that actually serves traffic, not only by
// the middleware when it is exercised in isolation.
//
// httpx's own tests call the middleware directly, so they keep passing if the
// single mount on the root router is dropped or moved behind a route group —
// the headers would then be absent from real responses, including the SPA
// document, and nothing would fail until a browser was pointed at the app.
// Serving /health here goes through the same root middleware stack the SPA
// document and the API use.
func TestHardeningHeadersAreOnEveryResponseThroughTheRouter(t *testing.T) {
	srv := newTestServer(t, &Deps{
		Config: &config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health through the router = %d, want 200", rec.Code)
	}

	want := map[string]string{
		"X-Content-Type-Options":              "nosniff",
		"X-Frame-Options":                     "DENY",
		"Referrer-Policy":                     "no-referrer",
		"Cross-Origin-Opener-Policy":          "same-origin",
		"Permissions-Policy":                  "geolocation=(), microphone=(), camera=()",
		"Content-Security-Policy-Report-Only": httpx.CSP,
		"Reporting-Endpoints":                 httpx.CSPReportEndpoint + "=\"" + httpx.CSPReportPath + "\"",
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}

	// The policy is Report-Only by default, and HSTS is off by default. Both are
	// asserted absent on the real response so that turning either on is a visible
	// change to this test rather than a silent one.
	if got := rec.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("Content-Security-Policy must be absent by default (Report-Only), got %q", got)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security must be absent by default, got %q", got)
	}
}
