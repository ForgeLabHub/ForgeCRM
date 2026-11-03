package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// okHandler is the trivial downstream the middleware wraps. It writes nothing, so
// the assertions are purely about what the middleware set on the response.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

// serveHeaders runs one request through the middleware and returns the response.
func serveHeaders(t *testing.T, hstsMaxAge time.Duration) *http.Response {
	t.Helper()
	srv := httptest.NewServer(SecurityHeadersMiddleware(hstsMaxAge)(okHandler()))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("request through the middleware: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestSecurityHeadersArePinned pins the whole header set, so a future edit cannot
// silently drop one. Each of these is a browser-hardening guarantee; a missing
// header is not a compile error and would otherwise go unnoticed.
func TestSecurityHeadersArePinned(t *testing.T) {
	resp := serveHeaders(t, 0)

	want := map[string]string{
		"X-Content-Type-Options":              "nosniff",
		"X-Frame-Options":                     "DENY",
		"Referrer-Policy":                     "no-referrer",
		"Cross-Origin-Opener-Policy":          "same-origin",
		"Permissions-Policy":                  "geolocation=(), microphone=(), camera=()",
		"Content-Security-Policy-Report-Only": CSPReportOnly,
		"Reporting-Endpoints":                 CSPReportEndpoint + "=\"/csp-report\"",
	}
	for name, value := range want {
		if got := resp.Header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}

	// The policy is delivered Report-Only. An enforcing Content-Security-Policy
	// header appearing here would be a change of behaviour, not a rename, so it is
	// asserted absent explicitly rather than left implicit in the map above.
	if got := resp.Header.Get("Content-Security-Policy"); got != "" {
		t.Errorf("Content-Security-Policy must not be set (policy is Report-Only), got %q", got)
	}
}

// TestHSTSIsOffByDefault asserts the default configuration emits no HSTS header.
// HSTS is cached by the browser for its whole max-age, so switching it on is a
// deliberate operator action, never an accident of the default.
func TestHSTSIsOffByDefault(t *testing.T) {
	resp := serveHeaders(t, 0)
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security must be absent when max-age is 0, got %q", got)
	}
}

// TestHSTSIsEmittedWhenConfigured asserts the header appears, with the configured
// max-age, once an operator opts in.
func TestHSTSIsEmittedWhenConfigured(t *testing.T) {
	resp := serveHeaders(t, 24*time.Hour)
	if got, want := resp.Header.Get("Strict-Transport-Security"), "max-age=86400"; got != want {
		t.Errorf("Strict-Transport-Security = %q, want %q", got, want)
	}
}
