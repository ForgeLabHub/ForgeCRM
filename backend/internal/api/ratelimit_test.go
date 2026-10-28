package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// newRouterForTest builds only the router. Requests to /health and /ready do not
// touch any dependency, so nil services are fine for probing those routes.
func newRouterForTest(t *testing.T, cfg *config.Config) http.Handler {
	t.Helper()
	return newTestServer(t, &Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
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

// TestRateLimitHeadersAreEmitted proves the client is told the budget it is
// spending against. Without them a client can only discover the limit by being
// refused, and an operator debugging a 429 has no way to see the number the
// server was applying.
func TestRateLimitHeadersAreEmitted(t *testing.T) {
	cfg := &config.Config{RateLimitPerMin: 10, LoginRateLimitPerMin: 10}
	h := newRouterForTest(t, cfg)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/__rate_limit_probe__", nil))

	if got := rec.Header().Get("RateLimit-Limit"); got != "10" {
		t.Errorf("RateLimit-Limit is %q, want 10", got)
	}
	if got := rec.Header().Get("RateLimit-Remaining"); got == "" {
		t.Error("RateLimit-Remaining is absent; a client cannot see its budget")
	}
	// The older field names are emitted alongside the current ones because clients
	// exist for both.
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "10" {
		t.Errorf("X-RateLimit-Limit is %q, want 10", got)
	}
}

// TestRefusalAdvertisesWhenToRetry covers the instruction a refused client needs.
func TestRefusalAdvertisesWhenToRetry(t *testing.T) {
	cfg := &config.Config{RateLimitPerMin: 1, LoginRateLimitPerMin: 1}
	h := newRouterForTest(t, cfg)

	var rec *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/__rate_limit_probe__", nil))
		if rec.Code == http.StatusTooManyRequests {
			break
		}
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the API was never refused; got status %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("a 429 carried no Retry-After; the client cannot know when to try again")
	}
	if got := rec.Header().Get("RateLimit-Remaining"); got != "0" {
		t.Errorf("a 429 reported RateLimit-Remaining=%q, want 0", got)
	}
}

// TestForwardedForCannotChooseTheRateLimitBucket is the end-to-end form of the
// spoofing test in httpx: a caller outside the trusted proxy range must not be
// able to escape its budget by rotating the header.
//
// The trust list is empty here, which is the default configuration, so the
// header is ignored entirely.
func TestForwardedForCannotChooseTheRateLimitBucket(t *testing.T) {
	cfg := &config.Config{RateLimitPerMin: 3, LoginRateLimitPerMin: 3}
	h := newRouterForTest(t, cfg)

	refusals := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/__rate_limit_probe__", nil)
		// A distinct invented address on every request.
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			refusals++
		}
	}

	if refusals == 0 {
		t.Fatal("rotating X-Forwarded-For evaded the limit entirely; the header is being believed " +
			"from an untrusted peer, so the limit exists only in the code")
	}
}

// TestTrustedProxyHeaderIsHonouredEndToEnd is the positive half: when the peer is
// a configured proxy, its forwarded client address is what gets counted, so
// distinct clients behind it keep distinct budgets.
func TestTrustedProxyHeaderIsHonouredEndToEnd(t *testing.T) {
	cfg := &config.Config{RateLimitPerMin: 1, LoginRateLimitPerMin: 1}
	trusted, err := httpx.TrustedProxiesFrom("127.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	srv, err := New(&Deps{Config: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		ratelimit.NewMemoryStore(), trusted)
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	h := srv.Handler()

	probe := func(client string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/__rate_limit_probe__", nil)
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// The first request from each client spends its one-unit budget.
	if got := probe("203.0.113.1"); got == http.StatusTooManyRequests {
		t.Fatal("the first request from a new client was refused")
	}
	// A different client must still be allowed: with the header honoured they have
	// separate buckets.
	if got := probe("203.0.113.2"); got == http.StatusTooManyRequests {
		t.Fatal("a second client behind the proxy shared the first client's budget; " +
			"the forwarded address is not being used as the subject")
	}
	// And the first client's budget really was spent.
	if got := probe("203.0.113.1"); got != http.StatusTooManyRequests {
		t.Fatalf("the first client was allowed past its budget; got status %d", got)
	}
}
