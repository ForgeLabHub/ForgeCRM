package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// brokenStore is a counter store that cannot answer, standing in for an
// unreachable Redis.
type brokenStore struct{}

func (brokenStore) Increment(context.Context, string, time.Duration, time.Time) (int, time.Duration, error) {
	return 0, 0, errors.New("store unavailable")
}
func (brokenStore) Close() error   { return nil }
func (brokenStore) String() string { return "broken" }

// TestDegradedFailClosedIsNotReportedAs429 is the reason the degraded path has
// its own status.
//
// A store failure under FailClosed refuses the request, and the refusal used to
// be written as 429. That is wrong twice over: 429 tells the client to slow down
// and come back, which is advice it should not follow when the server cannot
// count, and it makes the store outage indistinguishable from a real limit in
// every log and dashboard. 503 with a distinct code is the honest answer.
func TestDegradedFailClosedIsNotReportedAs429(t *testing.T) {
	limiter, err := ratelimit.New(brokenStore{}, ratelimit.Policy{
		Name:      "login",
		Limit:     10,
		Window:    time.Minute,
		FailMode:  ratelimit.FailClosed,
		KeyPrefix: "rl:login",
	})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}

	var reached bool
	handler := RateLimitMiddleware(RateLimitOptions{
		Limiter: limiter,
		Subject: func(*http.Request) string { return "203.0.113.1" },
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))

	if reached {
		t.Fatal("the request reached the handler; a fail-closed store must refuse")
	}
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("a store failure was reported as 429; a client would back off from a limit that is not being applied")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q on a store failure; the window is not what the client is waiting for", got)
	}
}

// TestDegradedFailOpenServesTheRequest is the other half of the policy: an
// ordinary read must not be turned into an outage by a counter that is down.
func TestDegradedFailOpenServesTheRequest(t *testing.T) {
	limiter, err := ratelimit.New(brokenStore{}, ratelimit.Policy{
		Name:      "api",
		Limit:     10,
		Window:    time.Minute,
		FailMode:  ratelimit.FailOpen,
		KeyPrefix: "rl:api",
	})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}

	var reached bool
	handler := RateLimitMiddleware(RateLimitOptions{
		Limiter: limiter,
		Subject: func(*http.Request) string { return "203.0.113.1" },
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil))

	if !reached {
		t.Fatal("the request was refused by a fail-open policy; a degraded counter must not become an outage")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
