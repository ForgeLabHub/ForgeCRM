package api

import (
	"testing"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// newTestServer builds a Server for a test.
//
// It exists so that a test can construct a server without naming the limiter
// store or the proxy trust list. Both are deployment concerns that no test is
// exercising when it builds a router to inspect routes or call one handler, and
// repeating them at every call site would make an unrelated change to the New
// signature touch a dozen tests.
//
// The store is per-process and the trust list is empty, which is what a
// single-replica deployment with no proxy looks like.
func newTestServer(t *testing.T, deps *Deps) *Server {
	t.Helper()

	srv, err := New(deps, ratelimit.NewMemoryStore(), httpx.TrustedProxies{})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	return srv
}
