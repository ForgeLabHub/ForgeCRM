package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// cspReportServer builds a server whose logger writes into buf, so a test can
// assert the sink recorded the report.
func cspReportServer(t *testing.T, buf *bytes.Buffer) *Server {
	t.Helper()
	return newTestServer(t, &Deps{
		Config: &config.Config{},
		Log:    slog.New(slog.NewTextHandler(buf, nil)),
	})
}

// TestCSPReportSinkRecordsTheLegacyShape checks the report-uri document shape
// ({"csp-report": {...}}) is accepted, logged, and answered 204.
func TestCSPReportSinkRecordsTheLegacyShape(t *testing.T) {
	var buf bytes.Buffer
	srv := cspReportServer(t, &buf)

	body := `{"csp-report":{"document-uri":"https://crm.example.com/login",` +
		`"violated-directive":"style-src","blocked-uri":"inline"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Body.String(); got != "" {
		t.Errorf("body = %q, want empty", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "CSP violation reported") {
		t.Fatalf("report was not logged: %s", logged)
	}
	if !strings.Contains(logged, "violated-directive") {
		t.Errorf("the logged report did not carry the violation: %s", logged)
	}
	// The wrapper key must not leak into the logged line; the inner report is what
	// an operator reads.
	if strings.Contains(logged, `\"csp-report\"`) {
		t.Errorf("the log line kept the csp-report wrapper: %s", logged)
	}
}

// TestCSPReportSinkRecordsTheReportingAPIShape checks the newer Reporting API
// shape (an array of report objects) is accepted too, since which shape a browser
// sends depends on its version.
func TestCSPReportSinkRecordsTheReportingAPIShape(t *testing.T) {
	var buf bytes.Buffer
	srv := cspReportServer(t, &buf)

	body := `[{"type":"csp-violation","body":{"blockedURL":"inline","effectiveDirective":"style-src"}}]`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !strings.Contains(buf.String(), "style-src") {
		t.Errorf("the Reporting API report was not logged: %s", buf.String())
	}
}

// TestCSPReportSinkIsSilentOnGarbage checks a non-JSON body is not a client error:
// the browser ignores the response anyway, and a report endpoint that answers
// differently for a malformed body is a probe.
func TestCSPReportSinkIsSilentOnGarbage(t *testing.T) {
	var buf bytes.Buffer
	srv := cspReportServer(t, &buf)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader("not json at all"))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("the malformed report was not logged as such: %s", buf.String())
	}
}

// TestCSPReportSinkBoundsTheBody checks an oversized report is refused without
// buffering it all and still answered 204. The cap is what stops the endpoint
// being a way to fill the log from one request.
func TestCSPReportSinkBoundsTheBody(t *testing.T) {
	var buf bytes.Buffer
	srv := cspReportServer(t, &buf)

	huge := strings.Repeat("a", maxCSPReportBytes+1)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(huge))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !strings.Contains(buf.String(), "CSP report rejected") {
		t.Errorf("the oversized report was not logged as rejected: %s", buf.String())
	}
}

// TestCSPReportSinkIsMounted checks the route exists on the real router and
// answers the method the browser uses. A handler that is never mounted would pass
// every test above.
func TestCSPReportSinkIsMounted(t *testing.T) {
	srv := cspReportServer(t, &bytes.Buffer{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(`{"csp-report":{}}`))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("POST /csp-report is not mounted: status %d", rec.Code)
	}
}

// TestSecurityHeadersEnforceSwitchesTheHeader checks the opt-in flips the
// delivery from report-only to enforcing, and that exactly one of the two headers
// is ever set.
func TestSecurityHeadersEnforceSwitchesTheHeader(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enforce bool
	}{
		{"report-only by default", false},
		{"enforcing when opted in", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := httpx.SecurityHeadersMiddlewareWithOptions(httpx.SecurityHeadersOptions{Enforce: tc.enforce})
			rec := httptest.NewRecorder()
			h(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			enforcing := rec.Header().Get("Content-Security-Policy")
			reportOnly := rec.Header().Get("Content-Security-Policy-Report-Only")
			if tc.enforce {
				if enforcing != httpx.CSP {
					t.Errorf("Content-Security-Policy = %q, want the policy", enforcing)
				}
				if reportOnly != "" {
					t.Errorf("Report-Only must be absent when enforcing, got %q", reportOnly)
				}
			} else {
				if reportOnly != httpx.CSP {
					t.Errorf("Content-Security-Policy-Report-Only = %q, want the policy", reportOnly)
				}
				if enforcing != "" {
					t.Errorf("Content-Security-Policy must be absent by default, got %q", enforcing)
				}
			}
		})
	}
}
