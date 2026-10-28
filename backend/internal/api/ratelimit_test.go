package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
)

// newRouterForTest builds only the router. Requests to /health and /ready do not
// touch any dependency, so nil services are fine for probing those routes.
func newRouterForTest(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	return New(&Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
}

// TestHealthEndpointsAreNotRateLimited guards a specific regression. Health
// probes share the process' rate limiter with the API. When the budget is spent
// and the probe is throttled, the orchestrator reads a 429 as an unhealthy
// container and restarts it, so a traffic spike becomes an outage. The probes
// must therefore stay outside the limiter.
func TestHealthEndpointsAreNotRateLimited(t *testing.T) {
	cfg := &config.Config{RateLimitPerMin: 1, LoginRateLimitPerMin: 1}
	h := newRouterForTest(t, cfg)

	// Exhaust the API budget well past its one-per-minute allowance.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/__rate_limit_probe__", nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	for _, path := range []string{"/health", "/ready"} {
		for i := 0; i < 10; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("%s returned 429 on request %d; health probes must bypass the rate limiter", path, i+1)
			}
		}
	}
}

// TestAPISubtreeIsRateLimited confirms the exemption above did not accidentally
// disable limiting for the API itself.
func TestAPISubtreeIsRateLimited(t *testing.T) {
	cfg := &config.Config{RateLimitPerMin: 1, LoginRateLimitPerMin: 1}
	h := newRouterForTest(t, cfg)

	var saw429 bool
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/__rate_limit_probe__", nil))
		if rec.Code == http.StatusTooManyRequests {
			saw429 = true
			break
		}
	}
	if !saw429 {
		t.Fatal("the API subtree was never rate limited; the limiter is not covering /api/v1")
	}
}

// TestLoginRateLimitIsConfigurable proves the login budget honours configuration
// rather than a constant baked into the router. A shared NAT address consumes
// one bucket for everyone behind it, so an operator must be able to raise this.
func TestLoginRateLimitIsConfigurable(t *testing.T) {
	budget := 3
	cfg := &config.Config{RateLimitPerMin: 10_000, LoginRateLimitPerMin: budget}
	h := newRouterForTest(t, cfg)

	attempts := 0
	for i := 0; i < budget+5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		// A distinct remote address per attempt would not be realistic; a real
		// office shares one, which is exactly why the limit is configurable.
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			break
		}
		attempts++
	}

	if attempts != budget {
		t.Fatalf("login limiter allowed %d attempts, expected exactly %d from LoginRateLimitPerMin", attempts, budget)
	}
}
