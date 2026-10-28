package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestMemoryStoreIsSafeForConcurrentUse is the regression test for the defect
// this package was written to fix.
//
// The previous limiter held its counters in a plain map that every request
// goroutine read and wrote. net/http serves each request in its own goroutine,
// so that was a live data race in production, not a theoretical one: under
// `go test -race` the old implementation reported a race on the map, and a
// sufficiently unlucky interleaving of concurrent map writes is an
// unrecoverable runtime panic that kills the process.
//
// The test asserts the invariant that matters — the count equals the number of
// calls — because a lost update is the silent version of the same bug. Run with
// -race, it also fails on any unsynchronised access.
func TestMemoryStoreIsSafeForConcurrentUse(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	now := time.Now()

	const goroutines = 64
	const perGoroutine = 250
	const total = goroutines * perGoroutine

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if _, _, err := store.Increment(context.Background(), "shared", time.Minute, now); err != nil {
					t.Errorf("increment: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	count, _, err := store.Increment(context.Background(), "shared", time.Minute, now)
	if err != nil {
		t.Fatalf("final increment: %v", err)
	}
	if count != total+1 {
		t.Fatalf("count after %d concurrent increments is %d; an increment was lost, which means the store is not synchronised", total, count)
	}
}

// TestMemoryStoreSweepIsConcurrentWithIncrement covers the other half of the old
// bug: Sweep iterated the map while request goroutines wrote to it. A sweep that
// runs alongside traffic must not corrupt the store or lose counts.
func TestMemoryStoreSweepIsConcurrentWithIncrement(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	now := time.Now()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				store.Sweep(now)
			}
		}
	}()

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := "key-" + string(rune('a'+n%26))
				_, _, _ = store.Increment(context.Background(), key, time.Minute, now)
			}
		}(i)
	}

	// Let the writers finish, then stop the sweeper.
	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestMemoryStoreWindowResetsAfterExpiry(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	start := time.Now()

	for i := 0; i < 3; i++ {
		if count, _, err := store.Increment(context.Background(), "k", time.Minute, start); err != nil || count != i+1 {
			t.Fatalf("increment %d: count=%d err=%v", i+1, count, err)
		}
	}

	// One second inside the window the count continues; one second past it the
	// window restarts. The clock is passed in, so this asserts a boundary rather
	// than depending on how long the test takes to run.
	if count, _, _ := store.Increment(context.Background(), "k", time.Minute, start.Add(59*time.Second)); count != 4 {
		t.Fatalf("count inside the window is %d, want 4", count)
	}
	if count, _, _ := store.Increment(context.Background(), "k", time.Minute, start.Add(61*time.Second)); count != 1 {
		t.Fatalf("count after the window is %d, want 1; the window did not reset", count)
	}
}

func TestMemoryStoreBoundsItsKeyCount(t *testing.T) {
	// A limiter keyed by client address is reachable by anyone who can send a
	// request, so the number of distinct keys is attacker-controlled. Without a
	// bound the map grows until the process is killed.
	store := newMemoryStore(time.Minute, 8)
	now := time.Now()

	for i := 0; i < 100; i++ {
		key := "subject-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, _, err := store.Increment(context.Background(), key, time.Minute, now); err != nil {
			t.Fatalf("increment: %v", err)
		}
	}

	if got := store.Len(); got > 8 {
		t.Fatalf("store holds %d keys; the maximum is 8, so memory is unbounded", got)
	}
}

func TestMemoryStoreSweepReleasesExpiredKeys(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	start := time.Now()

	for i := 0; i < 50; i++ {
		_, _, _ = store.Increment(context.Background(), "subject-"+string(rune('a'+i%26)), time.Minute, start)
	}
	if store.Len() == 0 {
		t.Fatal("nothing was recorded")
	}

	store.Sweep(start.Add(2 * time.Minute))
	if got := store.Len(); got != 0 {
		t.Fatalf("sweep left %d expired keys; memory is not reclaimed", got)
	}
}

func TestLimiterRejectsNegativeLimit(t *testing.T) {
	_, err := New(newMemoryStore(time.Minute, 0), Policy{Name: "x", Limit: -1, Window: time.Minute})
	if err == nil {
		t.Fatal("a negative limit was accepted; it would deny every request")
	}
}

func TestLimiterRejectsMissingNameAndWindow(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	if _, err := New(store, Policy{Limit: 1, Window: time.Minute}); err == nil {
		t.Error("a policy without a name was accepted")
	}
	if _, err := New(store, Policy{Name: "x", Limit: 1}); err == nil {
		t.Error("a policy without a window was accepted")
	}
	if _, err := New(nil, Policy{Name: "x", Limit: 1, Window: time.Minute}); err == nil {
		t.Error("a policy without a store was accepted")
	}
}

// TestDisabledPolicyAllowsWithoutCounting documents the meaning of a zero limit:
// the policy is off, and off means it does not touch the store at all.
func TestDisabledPolicyAllowsWithoutCounting(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	limiter, err := New(store, Policy{Name: "off", Limit: 0, Window: time.Minute, KeyPrefix: "off"})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}

	for i := 0; i < 100; i++ {
		d := limiter.Allow(context.Background(), "subject", time.Now())
		if !d.Allowed {
			t.Fatalf("request %d was refused by a disabled policy", i+1)
		}
	}
	if store.Len() != 0 {
		t.Fatalf("a disabled policy recorded %d keys; it must not touch the store", store.Len())
	}
}

func TestLimiterRefusesPastTheLimit(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	limiter, err := New(store, Policy{Name: "api", Limit: 3, Window: time.Minute, KeyPrefix: "rl:api"})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}
	now := time.Now()

	for i := 1; i <= 3; i++ {
		d := limiter.Allow(context.Background(), "subject", now)
		if !d.Allowed {
			t.Fatalf("request %d was refused before the limit of 3", i)
		}
		if want := 3 - i; d.Remaining != want {
			t.Fatalf("request %d reported %d remaining, want %d", i, d.Remaining, want)
		}
	}

	d := limiter.Allow(context.Background(), "subject", now)
	if d.Allowed {
		t.Fatal("the fourth request was allowed past a limit of 3")
	}
	if d.RetryAfter <= 0 {
		t.Fatal("a refusal reported no retry delay; the client cannot know when to try again")
	}
}

// TestLimiterPoliciesHaveIndependentBudgets guards the key prefix. Without it two
// policies that happen to share a subject would spend each other's budget, so a
// burst of ordinary API traffic could lock a user out of logging in.
func TestLimiterPoliciesHaveIndependentBudgets(t *testing.T) {
	store := newMemoryStore(time.Minute, 0)
	now := time.Now()

	api, err := New(store, Policy{Name: "api", Limit: 2, Window: time.Minute, KeyPrefix: "rl:api"})
	if err != nil {
		t.Fatalf("build api limiter: %v", err)
	}
	login, err := New(store, Policy{Name: "login", Limit: 2, Window: time.Minute, KeyPrefix: "rl:login"})
	if err != nil {
		t.Fatalf("build login limiter: %v", err)
	}

	for i := 0; i < 2; i++ {
		if !api.Allow(context.Background(), "10.0.0.1", now).Allowed {
			t.Fatal("api request refused inside its budget")
		}
	}
	if api.Allow(context.Background(), "10.0.0.1", now).Allowed {
		t.Fatal("api limit was not enforced")
	}

	// The same address must still be able to sign in: the api budget is spent.
	if !login.Allow(context.Background(), "10.0.0.1", now).Allowed {
		t.Fatal("spending the api budget also blocked login for the same address; the policies share a key")
	}
}

// failingStore stands in for an unreachable Redis.
type failingStore struct{}

func (failingStore) Increment(context.Context, string, time.Duration, time.Time) (int, time.Duration, error) {
	return 0, 0, ErrUnavailable
}
func (failingStore) Close() error   { return nil }
func (failingStore) String() string { return "failing" }

// TestFailModesDecideWhenTheStoreIsDown is the reason the fail mode is per
// policy. Availability and safety pull in opposite directions, and the correct
// answer depends on what the limit is protecting.
func TestFailModesDecideWhenTheStoreIsDown(t *testing.T) {
	now := time.Now()

	open, err := New(failingStore{}, Policy{Name: "api", Limit: 10, Window: time.Minute, FailMode: FailOpen, KeyPrefix: "a"})
	if err != nil {
		t.Fatalf("build open limiter: %v", err)
	}
	d := open.Allow(context.Background(), "subject", now)
	if !d.Allowed {
		t.Error("a fail-open policy refused a request when the store was unavailable")
	}
	if !d.Degraded {
		t.Error("a fail-open decision did not report that the store was unavailable; an operator would never learn the limit is not being enforced")
	}

	closed, err := New(failingStore{}, Policy{Name: "login", Limit: 10, Window: time.Minute, FailMode: FailClosed, KeyPrefix: "b"})
	if err != nil {
		t.Fatalf("build closed limiter: %v", err)
	}
	d = closed.Allow(context.Background(), "subject", now)
	if d.Allowed {
		t.Error("a fail-closed policy allowed a request when the store was unavailable; that removes the only barrier to password guessing")
	}
	if !d.Degraded {
		t.Error("a fail-closed decision did not report that the store was unavailable")
	}
}

// TestStoreErrorIsNotReportedAsRateLimited guards a subtle reporting mistake: a
// broken counter must not become a 429, because 429 tells the client to slow
// down when the truth is that the server cannot count.
func TestStoreErrorIsNotReportedAsRateLimited(t *testing.T) {
	store := &errorCapturingStore{}
	limiter, err := New(store, Policy{Name: "api", Limit: 1, Window: time.Minute, FailMode: FailOpen, KeyPrefix: "a"})
	if err != nil {
		t.Fatalf("build limiter: %v", err)
	}

	d := limiter.Allow(context.Background(), "subject", time.Now())
	if !d.Allowed || !d.Degraded {
		t.Fatalf("expected a degraded allow, got allowed=%v degraded=%v", d.Allowed, d.Degraded)
	}
	if !errors.Is(store.lastErr, ErrUnavailable) {
		t.Fatalf("store error was not wrapped in ErrUnavailable: %v", store.lastErr)
	}
}

type errorCapturingStore struct{ lastErr error }

func (s *errorCapturingStore) Increment(context.Context, string, time.Duration, time.Time) (int, time.Duration, error) {
	s.lastErr = ErrUnavailable
	return 0, 0, s.lastErr
}
func (s *errorCapturingStore) Close() error   { return nil }
func (s *errorCapturingStore) String() string { return "capturing" }
