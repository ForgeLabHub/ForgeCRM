package config

import (
	"strings"
	"testing"
)

// baseEnv supplies the settings Load requires, so each test can set only the
// ones it is about. Without this the tests would depend on whatever happens to
// be in the developer's shell, which is how they passed locally against an
// exported FORGECRM_DATABASE_URL and failed in CI.
func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("FORGECRM_DATABASE_URL", "postgres://user:pass@127.0.0.1:5432/forgecrm?sslmode=disable")
}

// The rate-limit store setting decides whether one replica or many enforce a
// budget. Getting it wrong is not a crash: the limit silently becomes
// "configured value times the replica count", which is invisible until someone
// counts requests. These tests pin the validation that turns that into a startup
// error.

func TestRateLimitStoreDefaultsToMemory(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load with no environment: %v", err)
	}
	if cfg.RateLimitStore != "memory" {
		t.Fatalf("default store is %q, want memory", cfg.RateLimitStore)
	}
	if cfg.RedisURL != "" {
		t.Fatalf("default Redis URL is %q, want empty", cfg.RedisURL)
	}
	if cfg.TrustedProxies != "" {
		t.Fatalf("default trusted proxies is %q, want empty so no header is believed", cfg.TrustedProxies)
	}
}

func TestRateLimitStoreRedisRequiresAURL(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RATE_LIMIT_STORE", "redis")
	t.Setenv("FORGECRM_REDIS_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("a redis store without a URL was accepted; it would fail on the first request instead of at startup")
	}
	if !strings.Contains(err.Error(), "FORGECRM_REDIS_URL") {
		t.Fatalf("the error does not name the missing setting: %v", err)
	}
}

func TestMemoryStoreRejectsAStrayRedisURL(t *testing.T) {
	baseEnv(t)
	// A URL left in the environment with the store still set to memory is a
	// half-finished switch. Accepting it would leave an operator believing the
	// limit is shared when it is per-process.
	t.Setenv("FORGECRM_RATE_LIMIT_STORE", "memory")
	t.Setenv("FORGECRM_REDIS_URL", "redis://127.0.0.1:6379/0")

	_, err := Load()
	if err == nil {
		t.Fatal("a Redis URL alongside the memory store was accepted; the configured store is ambiguous")
	}
}

func TestUnknownRateLimitStoreIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RATE_LIMIT_STORE", "memcached")

	_, err := Load()
	if err == nil {
		t.Fatal("an unknown store name was accepted; it would fall back to something the operator did not choose")
	}
}

func TestMalformedTrustedProxiesIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_TRUSTED_PROXIES", "10.0.0.0/8,not-a-cidr")

	_, err := Load()
	if err == nil {
		t.Fatal("a malformed trusted-proxy list was accepted; every request would look untrusted " +
			"and the proxy-aware client address would be silently disabled")
	}
}

func TestValidTrustedProxiesIsAccepted(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.0.0/16, 172.16.0.1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.TrustedProxies == "" {
		t.Fatal("the trusted-proxy list was not retained")
	}
}

func TestRedisStoreWithURLIsAccepted(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RATE_LIMIT_STORE", "redis")
	t.Setenv("FORGECRM_REDIS_URL", "redis://:secret@redis.internal:6379/3")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RateLimitStore != "redis" {
		t.Fatalf("store is %q, want redis", cfg.RateLimitStore)
	}
}
