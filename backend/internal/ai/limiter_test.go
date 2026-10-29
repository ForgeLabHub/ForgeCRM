package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/ratelimit"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// The limiter is exercised through allowRequest rather than through Complete so
// the test does not need a database: the counting happens before any record is
// touched, and it is the counting that the defect was in.

func aiServiceWithLimit(t *testing.T, limit int) *Service {
	t.Helper()
	limiter, err := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.Policy{
		Name: "ai", Limit: limit, Window: time.Minute, KeyPrefix: "rl:ai-test",
	})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}
	return NewService(&config.AIConfig{Enabled: true, Provider: "openai_compatible", Model: "m"},
		nil, nil, nil, nil, limiter)
}

func limitPrincipal(orgID uuid.UUID) principal {
	return principal{p: rbac.NewPrincipal(uuid.New(), "u@example.com", "U", orgID, nil)}
}

// TestAILimitIsPerOrganization is the regression for a single global bucket.
//
// The limiter this replaced counted every request in one shared slice, so the
// first organization to spend the minute's budget refused every other tenant on
// the deployment — a noisy or busy customer could deny the assistant to all the
// others. The limit is now keyed by the caller's organization, so one tenant
// reaching its cap must not affect another's.
func TestAILimitIsPerOrganization(t *testing.T) {
	svc := aiServiceWithLimit(t, 2)
	ctx := context.Background()

	orgA := limitPrincipal(uuid.New())
	orgB := limitPrincipal(uuid.New())

	for i := 0; i < 2; i++ {
		if !svc.allowRequest(ctx, orgA) {
			t.Fatalf("org A call %d within budget was refused", i+1)
		}
	}
	if svc.allowRequest(ctx, orgA) {
		t.Fatal("org A's third call exceeded a budget of two but was allowed")
	}
	// B has spent nothing, so its first call must still be allowed.
	if !svc.allowRequest(ctx, orgB) {
		t.Fatal("org B was starved by org A's spent budget")
	}
}

// TestAILimitIsConcurrencySafe exercises the counting the way the API drives it:
// many goroutines at once. Run under -race, it catches the unsynchronized counter
// the limiter replaced, which appended to a shared slice from every request
// goroutine.
func TestAILimitIsConcurrencySafe(t *testing.T) {
	svc := aiServiceWithLimit(t, 100000)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Distinct organizations: the point is to hammer the shared store's
			// bookkeeping, not to trip one bucket's limit.
			if !svc.allowRequest(ctx, limitPrincipal(uuid.New())) {
				t.Error("a concurrent request was refused with a budget of 100000")
			}
		}()
	}
	wg.Wait()
}

// TestAILimitFailsOpenWhenTheStoreIsUnavailable pins the policy: the AI cap is a
// cost and fairness control, not a security barrier, so an unreachable counter
// must not take the assistant down.
func TestAILimitFailsOpenWhenTheStoreIsUnavailable(t *testing.T) {
	limiter, err := ratelimit.New(unavailableStore{}, ratelimit.Policy{
		Name: "ai", Limit: 1, Window: time.Minute, FailMode: ratelimit.FailOpen, KeyPrefix: "rl:ai-down",
	})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}
	svc := NewService(&config.AIConfig{Enabled: true, Provider: "openai_compatible", Model: "m"},
		nil, nil, nil, nil, limiter)

	if !svc.allowRequest(context.Background(), limitPrincipal(uuid.New())) {
		t.Fatal("a degraded counter must not refuse the call")
	}
}

// unavailableStore is a counter store whose every operation fails, standing in
// for a Redis that has gone away.
type unavailableStore struct{}

func (unavailableStore) Increment(context.Context, string, time.Duration, time.Time) (int, time.Duration, error) {
	return 0, 0, errors.New("counter store unavailable")
}

func (unavailableStore) Close() error   { return nil }
func (unavailableStore) String() string { return "unavailable" }
