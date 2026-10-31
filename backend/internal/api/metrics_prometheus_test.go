//go:build integration

// These tests exercise the Prometheus metrics endpoint through the real HTTP
// stack: the real router, the real token gate, and a real database. They are the
// counterpart to the renderer's unit tests in internal/metrics, which pin the
// format but cannot prove the endpoint is mounted, gated, or wired to the
// snapshot the JSON surface reports.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/api/...
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
)

// metricsHarness builds a harness whose server has a metrics token configured,
// which is what enables the scrape endpoint.
func metricsHarness(t *testing.T, token string) *harness {
	t.Helper()

	cfg := testConfig(t)
	cfg.MetricsToken = token
	return newHarnessWithConfig(t, cfg)
}

// TestMetricsEndpointRequiresTheToken checks the gate on the scrape endpoint.
//
// The route is mounted outside the session middleware, so it cannot require a
// cookie; the token is the only barrier. This asserts it is enforced, including
// against the empty and prefix-wrong values an attacker would try first.
func TestMetricsEndpointRequiresTheToken(t *testing.T) {
	h := metricsHarness(t, "s3cret-metrics-token")

	for name, presented := range map[string]string{
		"absent":  "",
		"wrong":   "not-the-token",
		"prefix":  "s3cret-metrics-toke",
		"longer":  "s3cret-metrics-tokenX",
		"subcase": "S3CRET-METRICS-TOKEN",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if presented != "" {
			req.Header.Set("X-Metrics-Token", presented)
		}
		h.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s token: status %d, want 401", name, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-Metrics-Token", "s3cret-metrics-token")
	h.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestMetricsEndpointIsClosedWhenUnconfigured checks that with no token the
// endpoint refuses rather than serving an unauthenticated view of platform
// internals. The refusal is a 503 with the `disabled` code, which tells an
// operator the endpoint is off, not broken.
func TestMetricsEndpointIsClosedWhenUnconfigured(t *testing.T) {
	h := newHarnessWithConfig(t, testConfig(t)) // no MetricsToken

	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 when no token is configured (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "disabled") {
		t.Errorf("body does not name the disabled state: %s", rec.Body.String())
	}
}

// TestMetricsEndpointServesPrometheusFormat checks the body is the exposition
// format, not JSON: the content type, the HELP/TYPE headers, and the metric the
// operations runbook's alert is built on.
func TestMetricsEndpointServesPrometheusFormat(t *testing.T) {
	h := metricsHarness(t, "token")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-Metrics-Token", "token")
	h.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type is %q, want text/plain", ct)
	}

	body := rec.Body.String()
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("body looks like JSON, not the exposition format:\n%s", body)
	}
	// The alert in docs/operations/README.md is built on this metric and its _total
	// suffix, so its presence and its type are part of the runbook's contract.
	if !strings.Contains(body, "# TYPE webhook_deliveries_dead_lettered_total counter") {
		t.Errorf("the dead-letter counter is missing or not typed as a counter:\n%s", body)
	}
	// A gauge must not carry a _total suffix: rate() over one is meaningless, and the
	// suffix invites exactly that mistake.
	if strings.Contains(body, "webhook_deliveries_pending_total") {
		t.Errorf("a gauge is exported with a _total suffix:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE webhook_deliveries_pending gauge") {
		t.Errorf("the pending gauge is missing or not typed as a gauge:\n%s", body)
	}
}

// TestMetricsSurfacesAgree checks that the JSON diagnostics surface and the
// Prometheus surface report the same value under the same name.
//
// They share one snapshot precisely so they cannot drift. This is the assertion
// that the sharing holds: a value read from the database is seeded, then found in
// both bodies.
func TestMetricsSurfacesAgree(t *testing.T) {
	h := metricsHarness(t, "token")

	// A dead-letter transition, read from the audit trail by both surfaces.
	org := h.seedOrganization("metrics-" + uuid.NewString()[:8])
	if _, err := h.db.Pool().Exec(context.Background(), `
		INSERT INTO audit_log (organization_id, actor_type, action, entity_type, created_at)
		VALUES ($1, 'system', $2, 'webhook_delivery', now())`,
		org, string(audit.ActionWebhookDeadLettered)); err != nil {
		t.Fatalf("seed dead-letter audit entry: %v", err)
	}

	// A real session holding metrics.read reads the JSON surface. The harness has a
	// metrics token configured, so the JSON surface requires it too: that is the
	// second lock, and presenting it is what a deployment with one does.
	admin := h.seedUser("metrics-admin@example.test", "correct-horse-battery", "Metrics Admin")
	h.joinOrganization(admin, org, "security_auditor")
	actor := h.login("metrics-admin@example.test", "correct-horse-battery")

	jsonRec := h.doWithHeaders(actor, http.MethodGet, "/api/v1/admin/metrics", "",
		map[string]string{"X-Metrics-Token": "token"})
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("JSON metrics: status %d, body %s", jsonRec.Code, jsonRec.Body.String())
	}
	var jsonBody map[string]any
	if err := json.Unmarshal(jsonRec.Body.Bytes(), &jsonBody); err != nil {
		t.Fatalf("decode JSON metrics: %v (body %s)", err, jsonRec.Body.String())
	}
	fromJSON, ok := jsonBody["webhook_deliveries_dead_lettered_total"]
	if !ok {
		t.Fatalf("the JSON surface does not report the dead-letter counter: %s", jsonRec.Body.String())
	}
	if fromJSON.(float64) != 1 {
		t.Errorf("JSON surface reports %v dead-letter transitions, want 1", fromJSON)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-Metrics-Token", "token")
	h.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// The Prometheus surface must report the same value the JSON surface did. The
	// metric name is the JSON key, so the two are the same value under the same name.
	if !strings.Contains(rec.Body.String(), "webhook_deliveries_dead_lettered_total 1") {
		t.Errorf("the two surfaces disagree; Prometheus body:\n%s", rec.Body.String())
	}
}
