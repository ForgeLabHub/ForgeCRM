// Package ratelimit holds the shared state a request limiter needs when more
// than one API replica serves traffic.
//
// The limiter this replaces kept its counters in a plain map on the Server
// struct. That map was read and written from every request goroutine without
// synchronisation, which is both a data race and, on a bad interleaving, an
// unrecoverable "concurrent map writes" panic that takes the process down. It
// was also per-process, so N replicas granted N times the configured budget,
// and its Sweep method had no caller, so expired windows were never collected.
//
// The design here separates three concerns that the old type conflated:
//
//   - a Store, which is where counters live and is the only part that changes
//     when more than one replica shares them;
//   - a Policy, which says how much a category of traffic may spend and what to
//     do when the store cannot answer;
//   - a Limiter, which pairs the two and returns a decision the caller can turn
//     into a response.
//
// The failure mode is per-policy on purpose. See FailMode.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnavailable reports that the store could not answer. It is not a client
// error and never becomes a 429; it is what the policy's fail mode decides on.
var ErrUnavailable = errors.New("ratelimit: store unavailable")

// Decision is the outcome of one Allow call.
type Decision struct {
	// Allowed is whether the request may proceed.
	Allowed bool
	// Limit is the policy's budget for the window.
	Limit int
	// Remaining is the budget left after this request, never below zero.
	Remaining int
	// RetryAfter is how long until the window resets. It is zero when Allowed.
	RetryAfter time.Duration
	// Degraded reports that the store failed and the policy's fail mode decided
	// the outcome rather than a count. Callers should log it: a persistent
	// Degraded is how an operator learns the shared store is down and that the
	// limit is not actually being enforced.
	Degraded bool
}

// FailMode says what to do when the store cannot answer.
//
// The two modes exist because "deny on doubt" and "allow on doubt" are each
// correct for a different category, and picking one globally would be wrong for
// half the system.
type FailMode int

const (
	// FailOpen allows the request when the store is unreachable.
	//
	// It is right for ordinary API traffic: the limit is a fairness and abuse
	// control, and refusing every read because a counter is unavailable turns a
	// degraded cache into a total outage. Availability wins.
	FailOpen FailMode = iota

	// FailClosed rejects the request when the store is unreachable.
	//
	// It is right for authentication: the limit there is the only thing standing
	// between an attacker and unlimited password guesses. If the counter cannot
	// be read, "allow" means "unlimited", so the safe answer is to refuse. A
	// brief login outage is a smaller harm than an unbounded brute force.
	FailClosed
)

func (m FailMode) String() string {
	if m == FailClosed {
		return "fail-closed"
	}
	return "fail-open"
}

// Policy describes one category of limited traffic.
type Policy struct {
	// Name identifies the policy in logs and metric labels. It must be stable;
	// renaming it splits an operator's dashboards.
	Name string
	// Limit is the number of requests allowed per Window.
	Limit int
	// Window is the length of the fixed window.
	Window time.Duration
	// FailMode decides the outcome when the store is unreachable.
	FailMode FailMode
	// KeyPrefix namespaces this policy's keys in a shared store, so two policies
	// with the same subject cannot consume each other's budget.
	KeyPrefix string
}

// Validate reports whether the policy is usable. A limiter built from an invalid
// policy would either deny everything or allow everything, so this is checked at
// construction rather than discovered in production.
//
// A Limit of zero is accepted and means the policy is disabled, not that it
// denies everything. That is a real operator choice — a deployment that limits
// at its edge proxy does not need the application to limit again — and stating
// it as "off" is clearer than requiring a sentinel. New refuses a negative
// limit, which is never intentional.
func (p Policy) Validate() error {
	if p.Name == "" {
		return errors.New("ratelimit: policy name is required")
	}
	if p.Limit < 0 {
		return fmt.Errorf("ratelimit: policy %q has limit %d; it must not be negative", p.Name, p.Limit)
	}
	if p.Window <= 0 {
		return fmt.Errorf("ratelimit: policy %q has window %s; it must be positive", p.Name, p.Window)
	}
	return nil
}

// Enabled reports whether the policy limits anything. A disabled policy is a
// no-op, and its disabled state is something the caller should surface at
// startup rather than leave for an operator to infer from the absence of 429s.
func (p Policy) Enabled() bool { return p.Limit > 0 }

// Store is the counter backend.
//
// Implementations must be safe for concurrent use: net/http serves each request
// in its own goroutine, so Increment is called from many goroutines at once.
type Store interface {
	// Increment consumes one unit for key within a window of length window and
	// reports the count after this call together with how long the window has
	// left. Implementations must be atomic: two concurrent calls for the same key
	// must yield two distinct counts.
	//
	// now is passed in rather than read from the clock so that tests can drive
	// window boundaries deterministically.
	Increment(ctx context.Context, key string, window time.Duration, now time.Time) (count int, resetIn time.Duration, err error)

	// Close releases any resources held by the store.
	Close() error

	// String names the store in logs and diagnostics.
	String() string
}

// Limiter pairs a Policy with a Store.
type Limiter struct {
	store  Store
	policy Policy
}

// New builds a Limiter. It returns an error rather than panicking so that a
// misconfigured policy is reported at startup, next to the setting that caused
// it, instead of on the first request that hits it.
func New(store Store, policy Policy) (*Limiter, error) {
	if store == nil {
		return nil, fmt.Errorf("ratelimit: policy %q has no store", policy.Name)
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Limiter{store: store, policy: policy}, nil
}

// Policy returns the limiter's policy, for diagnostics.
func (l *Limiter) Policy() Policy { return l.policy }

// Allow decides whether one request for subject may proceed.
//
// subject is the thing being limited — a client address or a user id. It is
// always derived server-side; see the callers, which never take it from a header
// or body the client controls.
func (l *Limiter) Allow(ctx context.Context, subject string, now time.Time) Decision {
	// A disabled policy answers without touching the store. Counting traffic that
	// is not being limited would still work, but it would spend a round trip on
	// every request to produce a number nobody reads, and it would keep the store
	// as a dependency of a deployment that deliberately does not need it.
	if !l.policy.Enabled() {
		return Decision{Allowed: true}
	}

	key := l.policy.KeyPrefix + ":" + subject

	count, resetIn, err := l.store.Increment(ctx, key, l.policy.Window, now)
	if err != nil {
		// A store failure is reported as degraded, never as a 429: a 429 tells the
		// client to slow down, which is the wrong instruction when the truth is
		// that the server cannot count.
		return Decision{
			Allowed:    l.policy.FailMode == FailOpen,
			Limit:      l.policy.Limit,
			Remaining:  0,
			RetryAfter: 0,
			Degraded:   true,
		}
	}

	remaining := l.policy.Limit - count
	if remaining < 0 {
		remaining = 0
	}

	if count > l.policy.Limit {
		return Decision{
			Allowed:    false,
			Limit:      l.policy.Limit,
			Remaining:  0,
			RetryAfter: resetIn,
		}
	}

	return Decision{
		Allowed:   true,
		Limit:     l.policy.Limit,
		Remaining: remaining,
	}
}

// Close releases the underlying store.
func (l *Limiter) Close() error { return l.store.Close() }
