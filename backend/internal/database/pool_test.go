package database

import (
	"context"
	"strings"
	"testing"
)

// The pool bound is enforced before any connection is attempted, so these run
// without a database. That is deliberate: a guard that needs a database to test
// is a guard that does not get tested in the environments where the
// misconfiguration actually occurs.
func TestConnectWithPoolRejectsAnExcessivePool(t *testing.T) {
	_, err := ConnectWithPool(context.Background(),
		"postgres://user:pass@127.0.0.1:5432/db?sslmode=disable", MaxAllowedConns+1)
	if err == nil {
		t.Fatal("expected an over-large pool to be rejected")
	}
	// The message has to say what to do, because the operator sees it at startup
	// with no other context.
	for _, want := range []string{"exceeds the maximum", "max_connections"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// A value at the bound is legitimate: the limit must reject what is above it,
// not what is equal to it.
func TestConnectWithPoolAcceptsTheBound(t *testing.T) {
	// No server is listening, so the call fails — but it must fail while
	// connecting, not on the size check. Distinguishing the two is the point:
	// a bound that also rejected the boundary value would be off by one.
	_, err := ConnectWithPool(context.Background(),
		"postgres://user:pass@127.0.0.1:1/db?sslmode=disable", MaxAllowedConns)
	if err == nil {
		t.Skip("a server unexpectedly answered on port 1")
	}
	if strings.Contains(err.Error(), "exceeds the maximum") {
		t.Errorf("the boundary value %d should be accepted, got: %v", MaxAllowedConns, err)
	}
}

// A non-positive size means "use the default" rather than "use zero", because
// a zero-connection pool would deadlock on the first query.
func TestConnectWithPoolTreatsZeroAsDefault(t *testing.T) {
	_, err := ConnectWithPool(context.Background(),
		"postgres://user:pass@127.0.0.1:1/db?sslmode=disable", 0)
	if err == nil {
		t.Skip("a server unexpectedly answered on port 1")
	}
	if strings.Contains(err.Error(), "exceeds the maximum") {
		t.Errorf("zero should select the default, not be rejected: %v", err)
	}
}

// The default must itself be within the bound, or Connect would always fail.
func TestDefaultPoolIsWithinTheBound(t *testing.T) {
	if DefaultMaxConns <= 0 {
		t.Fatalf("DefaultMaxConns must be positive, got %d", DefaultMaxConns)
	}
	if DefaultMaxConns > MaxAllowedConns {
		t.Fatalf("DefaultMaxConns (%d) exceeds MaxAllowedConns (%d); Connect would always fail",
			DefaultMaxConns, MaxAllowedConns)
	}
}

// A malformed URL must be reported as such rather than surfacing as a size error,
// so that the two failure modes stay distinguishable in a startup log.
func TestConnectWithPoolReportsABadURL(t *testing.T) {
	_, err := ConnectWithPool(context.Background(), "not-a-url", DefaultMaxConns)
	if err == nil {
		t.Fatal("expected a malformed url to be rejected")
	}
	if strings.Contains(err.Error(), "exceeds the maximum") {
		t.Errorf("a bad url should not be reported as a pool size problem: %v", err)
	}
}
