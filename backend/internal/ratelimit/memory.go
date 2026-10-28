package ratelimit

import (
	"context"
	"sync"
	"time"
)

// defaultMaxKeys bounds the number of distinct subjects tracked at once. Without
// a bound, an attacker rotating source addresses grows the map until the process
// runs out of memory — a denial of service that costs the attacker nothing.
const defaultMaxKeys = 100_000

// MemoryStore is a fixed-window counter store held in this process.
//
// It is correct for a single replica and is the default when no shared store is
// configured. It is not shared between replicas: with N replicas behind a load
// balancer, each holds its own counters, so the effective limit is N times the
// configured budget. Set a shared store when running more than one replica.
//
// Unlike the map it replaces, every access is guarded by a mutex. That is what
// makes it safe to call from the per-request goroutines net/http spawns.
type MemoryStore struct {
	mu         sync.Mutex
	hits       map[string]*window
	maxKeys    int
	sweepEvery time.Duration
	stop       chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	// sweeperStarted records whether a background sweeper owns the stop channel,
	// so Close does not block on a goroutine that was never started.
	sweeperStarted bool
}

type window struct {
	count     int
	expiresAt time.Time
}

// NewMemoryStore returns a store with a background sweeper that collects expired
// windows. Call Close to stop it; a server that never closes leaks one goroutine,
// which is bounded and harmless, but a test that starts many would notice.
func NewMemoryStore() *MemoryStore {
	m := newMemoryStore(time.Minute, defaultMaxKeys)
	m.startSweeper()
	return m
}

// newMemoryStore builds a store without a background sweeper. Tests use it and
// pass an explicit `now` to Increment, so window boundaries are exact rather
// than dependent on wall-clock timing.
func newMemoryStore(sweepEvery time.Duration, maxKeys int) *MemoryStore {
	if sweepEvery <= 0 {
		sweepEvery = time.Minute
	}
	if maxKeys <= 0 {
		maxKeys = defaultMaxKeys
	}
	return &MemoryStore{
		hits:       make(map[string]*window),
		maxKeys:    maxKeys,
		sweepEvery: sweepEvery,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Increment consumes one unit for key and reports the count and remaining time.
func (m *MemoryStore) Increment(_ context.Context, key string, windowLen time.Duration, now time.Time) (int, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.hits[key]; ok && now.Before(w.expiresAt) {
		w.count++
		return w.count, w.expiresAt.Sub(now), nil
	}

	// A new subject, or one whose window has elapsed. The previous entry is
	// replaced rather than incremented, which is what makes the window fixed
	// rather than sliding.
	if len(m.hits) >= m.maxKeys {
		m.evictLocked(now)
	}
	m.hits[key] = &window{count: 1, expiresAt: now.Add(windowLen)}
	return 1, windowLen, nil
}

// evictLocked frees room when the store is at capacity.
//
// It drops expired entries first. If none are expired the store is genuinely
// full, so it drops the entry closest to expiry — the one that would have been
// swept a moment later anyway. The cost of that choice is that a subject whose
// counter is evicted starts a fresh window and may exceed its budget; the
// alternative is unbounded memory, and under a flood of distinct subjects the
// limit is already being spread across attackers rather than enforced per
// attacker. Bounding memory is the safer failure.
func (m *MemoryStore) evictLocked(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for k, w := range m.hits {
		if !now.Before(w.expiresAt) {
			delete(m.hits, k)
			continue
		}
		if oldestKey == "" || w.expiresAt.Before(oldest) {
			oldestKey, oldest = k, w.expiresAt
		}
	}
	if oldestKey != "" && len(m.hits) >= m.maxKeys {
		delete(m.hits, oldestKey)
	}
}

// Sweep removes expired windows. It is exported so a caller without the
// background sweeper can drive it, and so tests can assert on it directly.
func (m *MemoryStore) Sweep(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, w := range m.hits {
		if !now.Before(w.expiresAt) {
			delete(m.hits, k)
		}
	}
}

// Len reports how many subjects are tracked, for diagnostics and tests.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.hits)
}

func (m *MemoryStore) startSweeper() {
	m.sweeperStarted = true
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(m.sweepEvery)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.Sweep(time.Now())
			}
		}
	}()
}

// Close stops the background sweeper. It is safe to call more than once and safe
// to call on a store that never started one.
func (m *MemoryStore) Close() error {
	if !m.sweeperStarted {
		return nil
	}
	m.stopOnce.Do(func() { close(m.stop) })
	<-m.done
	return nil
}

// String names the store for logs and diagnostics.
func (m *MemoryStore) String() string { return "memory" }
