package retention

import (
	"strings"
	"testing"
	"time"
)

// TestEveryPolicyIsWellFormed guards the registry itself. A policy with a missing
// table, column or batch size would build a broken statement, and a protected
// policy with a non-positive minimum would let an operator configure a window
// that removes evidence immediately.
func TestEveryPolicyIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Policies() {
		switch {
		case p.Key == "":
			t.Fatalf("a policy has no key: %+v", p)
		case p.Table == "":
			t.Errorf("policy %q has no table", p.Key)
		case p.TimeColumn == "":
			t.Errorf("policy %q has no time column", p.Key)
		case p.BatchSize <= 0:
			t.Errorf("policy %q has a non-positive batch size", p.Key)
		case p.Reason == "":
			t.Errorf("policy %q has no stated reason", p.Key)
		}
		if seen[p.Key] {
			t.Errorf("policy key %q is declared twice", p.Key)
		}
		seen[p.Key] = true
		if p.Min < 0 {
			t.Errorf("policy %q has a negative minimum", p.Key)
		}
		if p.Default != 0 && p.Default < p.Min {
			t.Errorf("policy %q has a default below its minimum", p.Key)
		}
	}
}

// TestProtectedPoliciesHaveALongMinimum asserts that evidence tables cannot be
// configured to expire quickly. The minimum is what stops a well-meaning operator
// from setting a one-day audit window.
func TestProtectedPoliciesHaveALongMinimum(t *testing.T) {
	for _, p := range Policies() {
		if p.AppendOnly {
			// An append-only table cannot be swept whatever the configuration, so it
			// carries no usable window.
			continue
		}
		if !p.Protected {
			continue
		}
		if p.Min < 90*24*time.Hour {
			t.Errorf("protected policy %q has a minimum of %s, want at least 90 days", p.Key, p.Min)
		}
		if p.Default != 0 {
			t.Errorf("protected policy %q has a default window of %s, want 0 (off)", p.Key, p.Default)
		}
	}
}

// TestWindowResolution checks that a configured override wins and that a policy
// with no override falls back to its default.
func TestWindowResolution(t *testing.T) {
	r := New(nil, nil, nil, Options{
		Enabled: true,
		Windows: map[string]time.Duration{"jobs_finished": 720 * time.Hour},
	})

	var jobs, logs Policy
	for _, p := range Policies() {
		switch p.Key {
		case "jobs_finished":
			jobs = p
		case "app_logs":
			logs = p
		}
	}

	if got := r.window(jobs); got != 720*time.Hour {
		t.Errorf("jobs_finished window = %s, want the configured 720h", got)
	}
	if got := r.window(logs); got != logs.Default {
		t.Errorf("app_logs window = %s, want the default %s", got, logs.Default)
	}
}

// TestDisabledRunnerRefusesToRun asserts the opt-in property: a disabled runner
// does nothing and says so rather than silently returning an empty result set that
// looks like "nothing to delete".
func TestDisabledRunnerRefusesToRun(t *testing.T) {
	r := New(nil, nil, nil, Options{Enabled: false})
	if r.Enabled() {
		t.Fatal("a runner constructed with Enabled=false reports itself enabled")
	}
	if _, err := r.Run(nil); err != ErrDisabled {
		t.Fatalf("Run on a disabled runner returned %v, want ErrDisabled", err)
	}
}

// TestDryRunIsTheDefaultPosture documents the safe default: a runner built without
// an explicit DryRun value measures rather than deletes.
func TestDryRunIsTheDefaultPosture(t *testing.T) {
	r := New(nil, nil, nil, Options{Enabled: true})
	if !r.DryRun() {
		t.Fatal("a runner defaults to deleting; dry-run must be the default")
	}
}

// TestNilRunnerIsSafe verifies the optional-subsystem contract: a nil runner
// reports itself disabled and dry rather than panicking.
func TestNilRunnerIsSafe(t *testing.T) {
	var r *Runner
	if r.Enabled() {
		t.Error("a nil runner reports itself enabled")
	}
	if !r.DryRun() {
		t.Error("a nil runner reports itself as a real run")
	}
}

// TestStatusClauseOnlyAppendsWhenPresent checks the SQL fragment builder, which is
// the one place a predicate is concatenated rather than bound.
func TestStatusClauseOnlyAppendsWhenPresent(t *testing.T) {
	if got := statusClause(""); got != "" {
		t.Errorf("empty predicate produced %q, want empty", got)
	}
	if got := statusClause("status = 'delivered'"); got != " AND status = 'delivered'" {
		t.Errorf("predicate produced %q", got)
	}
}

// TestValidateWindowsRejectsUnknownKeys guards the startup check. A key not in
// the registry is refused rather than ignored, because an ignored key silently
// uses the default window and an operator who meant to shorten one would not
// find out.
func TestValidateWindowsRejectsUnknownKeys(t *testing.T) {
	if err := ValidateWindows(nil); err != nil {
		t.Fatalf("nil windows rejected: %v", err)
	}
	// Every registry key is accepted.
	all := map[string]time.Duration{}
	for _, p := range Policies() {
		all[p.Key] = 24 * time.Hour
	}
	if err := ValidateWindows(all); err != nil {
		t.Fatalf("a window for every known policy was rejected: %v", err)
	}
	// An unknown key is refused, and the message names it.
	err := ValidateWindows(map[string]time.Duration{"not_a_policy": time.Hour})
	if err == nil {
		t.Fatal("an unknown policy key was accepted")
	}
	if !strings.Contains(err.Error(), "not_a_policy") {
		t.Fatalf("the error does not name the unknown key: %v", err)
	}
}
