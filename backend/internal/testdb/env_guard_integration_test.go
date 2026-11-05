//go:build integration

package testdb

import (
	"os"
	"testing"
)

// TestIntegrationEnvironmentIsConfigured is a canary, not a test of behaviour.
//
// Every database-backed integration test skips itself when
// FORGECRM_TEST_DATABASE_URL is unset, which is right for a developer's plain
// `go test ./...` but wrong for CI: a job that runs with `-tags=integration` and
// no database would report success while running none of the integration suite.
// This test turns that silent skip into a loud failure, so the only way the
// `-tags=integration` run passes is if the services it needs are actually present.
//
// It deliberately does not skip: skipping is the failure mode it exists to catch.
func TestIntegrationEnvironmentIsConfigured(t *testing.T) {
	if os.Getenv(URLEnv) == "" {
		t.Fatalf("%s is unset while the integration build tag is on; the integration suite would run nothing and report success", URLEnv)
	}
	// Redis backs the distributed rate limiter and the cross-replica tests. Those
	// tests skip without it for the same reason, so the same guard applies: a CI run
	// that claims to exercise the integration suite must provide it.
	if os.Getenv(RedisURLEnv) == "" {
		t.Fatalf("%s is unset while the integration build tag is on; the Redis-backed tests would skip and report success", RedisURLEnv)
	}
}
