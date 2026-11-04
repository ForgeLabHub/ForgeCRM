package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
)

// The /metrics access model is two distinct refusals, and they are not the same
// failure:
//
//   - 503 `disabled` when no FORGECRM_METRICS_TOKEN is configured. The endpoint is
//     opt-in; this says "not enabled", which is a deployment choice.
//   - 401 when a token is configured and the presented X-Metrics-Token is missing
//     or wrong. This says "authenticate", which is a credential problem.
//
// Conflating them is the defect this pins: answering 401 for the disabled state
// would tell a scraper to retry with credentials when no credential can ever work,
// and answering 503 for a bad token would hide an authentication failure as an
// outage. Both paths return before the database is touched, so this runs without
// integration tags.

func metricsUnitServer(t *testing.T, token string) *Server {
	t.Helper()
	return newTestServer(t, &Deps{
		Config: &config.Config{MetricsToken: token},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestMetricsEndpointRefusesWith503WhenNoTokenIsConfigured(t *testing.T) {
	srv := metricsUnitServer(t, "")

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for the disabled endpoint (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), string("disabled")) {
		t.Errorf("body does not carry the `disabled` code: %s", rec.Body.String())
	}
}

func TestMetricsEndpointRefusesWith401WhenTheTokenIsWrong(t *testing.T) {
	srv := metricsUnitServer(t, "the-token")

	for name, presented := range map[string]string{
		"absent": "",
		"wrong":  "not-the-token",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if presented != "" {
			req.Header.Set("X-Metrics-Token", presented)
		}
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s token: status = %d, want 401", name, rec.Code)
		}
	}
}
