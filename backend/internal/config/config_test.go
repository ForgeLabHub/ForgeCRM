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

// Retention is the setting most able to destroy data, so its defaults are pinned
// here: off unless asked for, previewing unless deletion is explicitly allowed,
// and refusing a malformed window rather than silently ignoring it.

func TestRetentionIsOffByDefault(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Retention.Enabled {
		t.Fatal("retention is enabled by default; it must be opt-in")
	}
	if !cfg.Retention.DryRun {
		t.Fatal("retention is not a dry run by default; enabling it must not delete data")
	}
	if cfg.Retention.AllowAudit {
		t.Fatal("the audit trail is swept by default; it must be opt-in")
	}
}

func TestRetentionWindowsParse(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_ENABLED", "true")
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "jobs_finished=720h, app_logs=168h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Retention.Windows["jobs_finished"]; got.Hours() != 720 {
		t.Fatalf("jobs_finished window = %v, want 720h", got)
	}
	if got := cfg.Retention.Windows["app_logs"]; got.Hours() != 168 {
		t.Fatalf("app_logs window = %v, want 168h", got)
	}
}

func TestRetentionWindowWithoutADurationIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "jobs_finished")

	if _, err := Load(); err == nil {
		t.Fatal("a window with no duration was accepted")
	} else if !strings.Contains(err.Error(), "FORGECRM_RETENTION_WINDOWS") {
		t.Fatalf("the error does not name the setting: %v", err)
	}
}

func TestRetentionWindowWithAnInvalidDurationIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "jobs_finished=soon")

	if _, err := Load(); err == nil {
		t.Fatal("an invalid duration was accepted")
	}
}

func TestRetentionWindowDuplicatedIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "app_logs=24h,app_logs=48h")

	if _, err := Load(); err == nil {
		t.Fatal("a duplicated window key was accepted; the second would silently win")
	}
}

func TestRetentionWindowMustBePositive(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "app_logs=0h")

	if _, err := Load(); err == nil {
		t.Fatal("a zero window was accepted; it would mean delete everything")
	}
}

// TestRetentionWindowWithAnUnknownPolicyIsRejected pins the promise the
// RetentionConfig comment makes: a key the registry does not know is refused at
// startup. Without this check a typo such as "simulation_run" (missing the s)
// would be accepted, the default window would be used instead of the operator's,
// and the mistake would surface only when data they meant to prune remained.
func TestRetentionWindowWithAnUnknownPolicyIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "simulation_run=720h")

	err := func() error {
		_, err := Load()
		return err
	}()
	if err == nil {
		t.Fatal("a window for an unknown policy was accepted; the typo would silently keep the default")
	}
	if !strings.Contains(err.Error(), "unknown retention policy") {
		t.Fatalf("the error does not name the problem: %v", err)
	}
}

// TestRetentionWindowForANewPolicyIsAccepted confirms the keys the registry
// gained are usable, so the check above does not reject the real policies.
func TestRetentionWindowForANewPolicyIsAccepted(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_RETENTION_WINDOWS", "forecast_runs=48h,risk_signals=48h,simulation_runs=48h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Retention.Windows["forecast_runs"]; got.Hours() != 48 {
		t.Fatalf("forecast_runs window = %v, want 48h", got)
	}
}

func TestDatabaseURLFallbackAndPort(t *testing.T) {
	t.Setenv("FORGECRM_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "postgres://dbuser:pass@dbhost:5432/fallbackdb")
	t.Setenv("PORT", "9090")
	t.Setenv("FORGECRM_SECRET_KEY", "devsecretkey1234567890123456789012")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseURL != "postgres://dbuser:pass@dbhost:5432/fallbackdb" {
		t.Errorf("expected DatabaseURL fallback to DATABASE_URL, got %q", cfg.DatabaseURL)
	}
	if cfg.HTTPAddr != "0.0.0.0:9090" {
		t.Errorf("expected HTTPAddr to be 0.0.0.0:9090, got %q", cfg.HTTPAddr)
	}
}
