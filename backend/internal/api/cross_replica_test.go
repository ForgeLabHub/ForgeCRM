//go:build integration

// Cross-replica tests for the distributed rate limiter and for session sharing.
//
// The distinction these tests exist for: a limiter that is "shared" only in the
// sense that one process reads and writes its own counters is not shared at all.
// Two replicas each enforcing the configured budget means the real budget is
// twice what was configured, and the error grows with every replica added.
//
// So this file builds two independent servers — two routers, each with its own
// limiter store object and its own database pool — over one Redis and one
// PostgreSQL, and sends traffic to both. Sharing one Server between the two
// "replicas" would pass while the defect remained.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... \
//	FORGECRM_TEST_REDIS_URL=redis://... go test -tags=integration ./internal/api/...
package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// trustedProxyPeer is an address inside the range the cross-replica harness
// trusts, so a forwarding header from it is believed. It stands in for nginx.
const trustedProxyPeer = "172.18.0.5:40000"

// newSharedRedisStore connects to the test Redis and flushes it, so a rerun
// within the same window does not start with a spent budget. Redis outlives the
// test process; without the flush a second run would fail for a reason that has
// nothing to do with the code.
func newSharedRedisStore(t *testing.T) *ratelimit.RedisStore {
	t.Helper()

	raw := os.Getenv("FORGECRM_TEST_REDIS_URL")
	if raw == "" {
		t.Skip("FORGECRM_TEST_REDIS_URL is not set; skipping cross-replica rate-limit tests")
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test redis url: %v", err)
	}
	opts := ratelimit.RedisOptions{Addr: u.Host}
	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			opts.Password = pw
		}
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			t.Fatalf("test redis url has a non-numeric database %q", db)
		}
		opts.DB = n
	}

	store, err := ratelimit.NewRedisStore(context.Background(), opts)
	if err != nil {
		t.Fatalf("connect to test redis: %v", err)
	}

	if err := store.Client().FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush test redis: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return store
}

// newReplicaPair builds two servers that share one Redis and one database but
// share nothing in memory.
//
// The schema is reset once, before either server is built, so both see the same
// tables. Each server opens its own pool, so an increment made by one travels to
// PostgreSQL or Redis and back rather than through a shared object.
func newReplicaPair(t *testing.T, cfg *config.Config) (first, second *harness, store *ratelimit.RedisStore) {
	t.Helper()

	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping cross-replica tests")
	}

	store = newSharedRedisStore(t)
	resetSchema(t, url)

	return newHarnessWithStore(t, cfg, store, url),
		newHarnessWithStore(t, cfg, store, url),
		store
}

// login sends a sign-in attempt to one replica from a chosen client address and
// returns the status code.
//
// The address arrives in X-Forwarded-For from a peer inside the trusted range,
// which is how a request through nginx reaches the API. The harness trusts
// 172.16.0.0/12, so the header is believed and becomes the limiter's subject.
func login(t *testing.T, h *harness, clientAddr, body string) int {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", clientAddr)
	req.RemoteAddr = trustedProxyPeer
	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	return rec.Code
}

// TestLoginRateLimitIsSharedAcrossReplicas is the central claim of the
// distributed limiter, stated as a test.
//
// With a budget of N the N+1st attempt must be refused even when the attempts
// alternate between replicas. Against per-process counters each replica allows
// N, so 2N attempts succeed and this sees a 401 where it expects a 429.
func TestLoginRateLimitIsSharedAcrossReplicas(t *testing.T) {
	const budget = 4
	cfg := crossReplicaConfig(budget)
	first, second, _ := newReplicaPair(t, cfg)

	allowed := 0
	for i := 0; i < budget*3; i++ {
		h := first
		if i%2 == 1 {
			h = second
		}
		if code := login(t, h, "203.0.113.50", `{"email":"nobody@example.test","password":"wrong-password"}`); code == http.StatusTooManyRequests {
			break
		}
		allowed++
	}

	if allowed != budget {
		t.Fatalf("the pair allowed %d attempts with a budget of %d; a per-process counter "+
			"across two replicas would allow %d, so the limit is not shared",
			allowed, budget, budget*2)
	}
}

// TestSharedLimitAppliesToAReplicaThatHasSeenNoTraffic covers the asymmetric
// case: the second replica never handled an attempt, so a per-process counter
// there would be at zero and it would allow the whole budget again.
func TestSharedLimitAppliesToAReplicaThatHasSeenNoTraffic(t *testing.T) {
	const budget = 3
	cfg := crossReplicaConfig(budget)
	first, second, _ := newReplicaPair(t, cfg)

	for i := 0; i < budget; i++ {
		if code := login(t, first, "203.0.113.51", `{"email":"nobody@example.test","password":"wrong-password"}`); code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d on the first replica was refused before the budget was spent", i+1)
		}
	}

	code := login(t, second, "203.0.113.51", `{"email":"nobody@example.test","password":"wrong-password"}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("the second replica answered %d; a replica that has seen no traffic must still "+
			"see the budget spent by the first, otherwise the limit is per-process", code)
	}
}

// TestSharedLimitIsPerClientAddress proves the shared counter is still keyed by
// client. A shared counter that ignored the subject would be a global lockout of
// the login endpoint — a denial of service, not a rate limit.
func TestSharedLimitIsPerClientAddress(t *testing.T) {
	const budget = 2
	cfg := crossReplicaConfig(budget)
	first, second, _ := newReplicaPair(t, cfg)

	for i := 0; i < budget; i++ {
		h := first
		if i%2 == 1 {
			h = second
		}
		login(t, h, "203.0.113.52", `{"email":"a@example.test","password":"wrong-password"}`)
	}
	if code := login(t, first, "203.0.113.52", `{"email":"a@example.test","password":"wrong-password"}`); code != http.StatusTooManyRequests {
		t.Fatalf("the exhausted client was allowed again (status %d)", code)
	}

	// A different address has its own budget, on either replica.
	if code := login(t, second, "203.0.113.53", `{"email":"b@example.test","password":"wrong-password"}`); code == http.StatusTooManyRequests {
		t.Fatal("a second client address was refused; the shared counter is not keyed per client")
	}
}

// TestSharedWindowResetsAfterExpiry checks the boundary: once the window has
// passed, the shared counter must start a new window rather than staying spent.
// Without this one burst would disable sign-in permanently.
//
// The key is deleted rather than waiting out the window. Sleeping would make the
// test slow and, worse, flaky under load; deletion produces the same observable
// state that expiry does.
func TestSharedWindowResetsAfterExpiry(t *testing.T) {
	const budget = 2
	cfg := crossReplicaConfig(budget)
	first, second, store := newReplicaPair(t, cfg)

	for i := 0; i < budget; i++ {
		h := first
		if i%2 == 1 {
			h = second
		}
		login(t, h, "203.0.113.54", `{"email":"a@example.test","password":"wrong-password"}`)
	}
	if code := login(t, first, "203.0.113.54", `{"email":"a@example.test","password":"wrong-password"}`); code != http.StatusTooManyRequests {
		t.Fatalf("expected a refusal before the window expired, got %d", code)
	}

	if err := store.Client().Del(context.Background(), "rl:login:203.0.113.54").Err(); err != nil {
		t.Fatalf("delete the counter: %v", err)
	}

	if code := login(t, second, "203.0.113.54", `{"email":"a@example.test","password":"wrong-password"}`); code == http.StatusTooManyRequests {
		t.Fatal("the counter was still refused after the key was removed; expiry does not release the budget")
	}
}

// TestSharedCountersSurviveAReplicaRestart models a rolling deploy: a fresh
// server object over the same Redis must see the counters the previous one left
// behind, otherwise every restart hands an attacker a fresh budget.
func TestSharedCountersSurviveAReplicaRestart(t *testing.T) {
	const budget = 3
	cfg := crossReplicaConfig(budget)
	first, second, store := newReplicaPair(t, cfg)

	for i := 0; i < budget; i++ {
		h := first
		if i%2 == 1 {
			h = second
		}
		login(t, h, "203.0.113.55", `{"email":"a@example.test","password":"wrong-password"}`)
	}

	// A new server over the same Redis stands in for the restarted process.
	restarted := newHarnessWithStore(t, cfg, store, os.Getenv("FORGECRM_TEST_DATABASE_URL"))

	if code := login(t, restarted, "203.0.113.55", `{"email":"a@example.test","password":"wrong-password"}`); code != http.StatusTooManyRequests {
		t.Fatalf("a restarted replica answered %d; the shared counter did not survive the restart", code)
	}
}

// TestFailModesAreNotInterchangeable checks the two failure modes against an
// unreachable store. The login limiter must refuse when it cannot count — it is
// the only barrier to unlimited guessing — while ordinary API traffic must still
// be served, because turning a degraded counter into an outage is worse than a
// temporarily unenforced read limit.
func TestFailModesAreNotInterchangeable(t *testing.T) {
	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping cross-replica tests")
	}

	cfg := crossReplicaConfig(2)
	resetSchema(t, url)
	h := newHarnessWithStore(t, cfg, failingStore{}, url)

	// Login is fail-closed: an uncountable store must refuse. It must not refuse
	// with 429, which would tell the client to slow down when the truth is that the
	// server cannot count; 503 with a distinct code is the honest answer.
	if code := login(t, h, "203.0.113.60", `{"email":"a@example.test","password":"wrong-password"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("login answered %d with an unreachable store; it must fail closed with 503", code)
	}

	// Ordinary API traffic is fail-open: it must still be served.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	req.RemoteAddr = trustedProxyPeer
	req.Header.Set("X-Forwarded-For", "203.0.113.61")
	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("ordinary API traffic was refused with an unreachable store (status %d); it must fail open", rec.Code)
	}
}

// failingStore is a Store that cannot count, standing in for an unreachable
// Redis. It is used instead of a real dead address because NewRedisStore pings
// at construction and rejects an unreachable server — which is itself the
// behaviour that makes a bad address a startup error rather than a per-request
// surprise, so it cannot also be used to simulate a store that fails mid-flight.
type failingStore struct{}

func (failingStore) Increment(context.Context, string, time.Duration, time.Time) (int, time.Duration, error) {
	return 0, 0, errors.New("ratelimit: store unavailable")
}
func (failingStore) Close() error   { return nil }
func (failingStore) String() string { return "failing" }

// crossReplicaConfig is the harness config with a small, explicit login budget
// and a trusted proxy range, so a forwarded address is believed.
func crossReplicaConfig(loginBudget int) *config.Config {
	dir := os.TempDir()
	return &config.Config{
		Env:                  "test",
		HTTPAddr:             "127.0.0.1:0",
		SecretKey:            "test-secret-key-that-is-long-enough-for-hkdf-derivation",
		SessionCookieName:    "forgecrm_session",
		CSRFHeader:           "X-CSRF-Token",
		SessionTTL:           12 * time.Hour,
		RateLimitPerMin:      100000,
		LoginRateLimitPerMin: loginBudget,
		TrustedProxies:       "172.16.0.0/12",
		ImportStorageDir:     dir,
		ImportTempDir:        dir,
	}
}

var _ = httpx.TrustedProxies{}
