//go:build integration

package ratelimit

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests exercise a real Redis, because the properties they check —
// atomicity across concurrent callers and a window that survives being read by
// another process — are exactly the properties a fake would get right by
// construction and the real server might not.
//
// They skip when FORGECRM_TEST_REDIS_URL is unset, the same way the database
// integration tests skip without FORGECRM_TEST_DATABASE_URL, so the default test
// run needs no Redis.

func redisURLForTest(t *testing.T) string {
	t.Helper()
	url := os.Getenv("FORGECRM_TEST_REDIS_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_REDIS_URL is not set; skipping Redis-backed limiter tests")
	}
	return url
}

// newRedisStoreForTest builds a store on a fresh database index and clears it,
// so tests do not inherit counters from a previous run. Redis persists across
// test processes, so without the flush a rerun within the same minute would see
// a spent budget and fail for a reason unrelated to the code.
func newRedisStoreForTest(t *testing.T) *RedisStore {
	t.Helper()

	store := newRedisStoreFromURL(t, redisURLForTest(t))
	t.Cleanup(func() {
		ctx := context.Background()
		if err := store.Client().FlushDB(ctx).Err(); err != nil {
			t.Logf("flush test database: %v", err)
		}
		_ = store.Close()
	})
	return store
}

func newRedisStoreFromURL(t *testing.T, url string) *RedisStore {
	t.Helper()

	opts, err := parseTestRedisURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	store, err := NewRedisStore(context.Background(), opts)
	if err != nil {
		t.Fatalf("connect to redis: %v", err)
	}
	return store
}

// parseTestRedisURL mirrors the application's URL parsing closely enough for a
// test: it accepts redis://[user:password@]host:port[/db].
func parseTestRedisURL(raw string) (RedisOptions, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return RedisOptions{}, err
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return RedisOptions{}, fmt.Errorf("test redis url must use redis:// or rediss://, got %q", u.Scheme)
	}
	if u.Host == "" {
		return RedisOptions{}, fmt.Errorf("test redis url has no host")
	}

	opts := RedisOptions{Addr: u.Host}
	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			opts.Password = pw
		}
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return RedisOptions{}, fmt.Errorf("test redis url has a non-numeric database %q", db)
		}
		opts.DB = n
	}
	return opts, nil
}

func TestRedisStoreIncrementIsAtomicAcrossGoroutines(t *testing.T) {
	store := newRedisStoreForTest(t)
	now := time.Now()

	const goroutines = 32
	const perGoroutine = 50
	const total = goroutines * perGoroutine

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if _, _, err := store.Increment(context.Background(), "atomic", time.Minute, now); err != nil {
					t.Errorf("increment: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	count, _, err := store.Increment(context.Background(), "atomic", time.Minute, now)
	if err != nil {
		t.Fatalf("final increment: %v", err)
	}
	if count != total+1 {
		t.Fatalf("count is %d after %d increments; an increment was lost, so the script is not atomic", count, total)
	}
}

// TestRedisStoreWindowIsSharedBetweenInstances is the reason a distributed store
// exists. Two Limiter values built over separate connections stand in for two
// API replicas: the budget must be counted once across both, not once each.
func TestRedisStoreWindowIsSharedBetweenInstances(t *testing.T) {
	first := newRedisStoreForTest(t)
	second := newRedisStoreFromURL(t, redisURLForTest(t))
	t.Cleanup(func() { _ = second.Close() })

	policy := Policy{Name: "api", Limit: 5, Window: time.Minute, KeyPrefix: "shared"}
	a, err := New(first, policy)
	if err != nil {
		t.Fatalf("build limiter a: %v", err)
	}
	b, err := New(second, policy)
	if err != nil {
		t.Fatalf("build limiter b: %v", err)
	}

	now := time.Now()
	allowed := 0
	for i := 0; i < 10; i++ {
		// Alternate between the two instances, as a load balancer would.
		limiter := a
		if i%2 == 1 {
			limiter = b
		}
		if limiter.Allow(context.Background(), "203.0.113.7", now).Allowed {
			allowed++
		}
	}

	if allowed != 5 {
		t.Fatalf("%d of 10 requests were allowed across two instances; the limit of 5 is per instance, "+
			"so N replicas would grant N times the configured budget", allowed)
	}
}

func TestRedisStoreReportsResetTime(t *testing.T) {
	store := newRedisStoreForTest(t)
	now := time.Now()

	if _, resetIn, err := store.Increment(context.Background(), "ttl", 90*time.Second, now); err != nil {
		t.Fatalf("increment: %v", err)
	} else if resetIn <= 0 || resetIn > 90*time.Second {
		t.Fatalf("reset time is %s, want a positive value within the 90s window", resetIn)
	}
}

// TestRedisStoreReportsUnavailable covers the fail mode's input: an unreachable
// server must produce ErrUnavailable so the policy decides, rather than a
// plausible-looking count that silently allows or denies everything.
func TestRedisStoreReportsUnavailable(t *testing.T) {
	// Port 1 is reserved and nothing listens there.
	store, err := NewRedisStore(context.Background(), RedisOptions{
		Addr:        "127.0.0.1:1",
		DialTimeout: 500 * time.Millisecond,
	})
	if err == nil {
		_ = store.Close()
		t.Fatal("connecting to a closed port succeeded; the startup check is not verifying the connection")
	}
}

func TestNewRedisStoreRejectsEmptyAddress(t *testing.T) {
	if _, err := NewRedisStore(context.Background(), RedisOptions{}); err == nil {
		t.Fatal("an empty address was accepted")
	}
}
