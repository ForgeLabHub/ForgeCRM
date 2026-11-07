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
	t.Setenv("FORGECRM_SECRET_KEY", "")

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

// TestCSPDeliveryAndHSTSOptIn covers the two switches that change a browser
// security header, and their defaults. Both default to the safe side — the CSP
// is Report-Only and HSTS is off — because an enforcing policy that blocks the
// bundle is an outage and HSTS is cached for its whole max-age. Nothing else
// asserted these fields, so a change of default would have gone unnoticed.
func TestCSPDeliveryAndHSTSOptIn(t *testing.T) {
	t.Run("safe defaults", func(t *testing.T) {
		baseEnv(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.CSPEnforce {
			t.Errorf("CSPEnforce = true by default; the policy must ship Report-Only")
		}
		if cfg.HSTSMaxAge != 0 {
			t.Errorf("HSTSMaxAge = %s by default; HSTS must be off", cfg.HSTSMaxAge)
		}
	})

	t.Run("opt-in is honoured", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("FORGECRM_CSP_ENFORCE", "true")
		t.Setenv("FORGECRM_HSTS_MAX_AGE", "24h")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.CSPEnforce {
			t.Errorf("CSPEnforce = false, want true when FORGECRM_CSP_ENFORCE=true")
		}
		if cfg.HSTSMaxAge.Hours() != 24 {
			t.Errorf("HSTSMaxAge = %s, want 24h", cfg.HSTSMaxAge)
		}
	})

	t.Run("a negative max-age is rejected, not read as off", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("FORGECRM_HSTS_MAX_AGE", "-1h")
		_, err := Load()
		if err == nil {
			t.Fatal("Load accepted a negative FORGECRM_HSTS_MAX_AGE; it must be a startup error")
		}
		if !strings.Contains(err.Error(), "FORGECRM_HSTS_MAX_AGE") {
			t.Errorf("error %q does not name FORGECRM_HSTS_MAX_AGE", err)
		}
	})
}

func TestCookieConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, sameSite, secure, partitioned string
		wantErr                             bool
	}{
		{"default", "lax", "false", "false", false},
		{"strict", "strict", "false", "false", false},
		{"none requires secure", "none", "false", "false", true},
		{"partitioned requires secure", "lax", "false", "true", true},
		{"secure partitioned", "none", "true", "true", false},
		{"unknown mode", "maybe", "true", "false", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			t.Setenv("FORGECRM_COOKIE_SAME_SITE", tc.sameSite)
			t.Setenv("FORGECRM_COOKIE_SECURE", tc.secure)
			t.Setenv("FORGECRM_COOKIE_PARTITIONED", tc.partitioned)
			cfg, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && (cfg.CookieSameSite != tc.sameSite || cfg.CookiePartitioned != (tc.partitioned == "true")) {
				t.Fatalf("unexpected cookie settings: %+v", cfg)
			}
		})
	}
}

// The AI provider kind is a closed set. An unrecognised value used to build no
// provider at all while FORGECRM_AI_ENABLED still read as on, so the subsystem
// looked configured and answered "provider unavailable" on every call. These
// tests pin the validation that turns that into a startup error, and the default
// base URL that lets a hosted provider be named without repeating its endpoint.

func TestUnknownAIProviderIsRejectedByTheClosedSet(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_PROVIDER", "grok")

	_, err := Load()
	if err == nil {
		t.Fatal("an unknown AI provider was accepted; the AI layer would build nothing while reading as enabled")
	}
	if !strings.Contains(err.Error(), "FORGECRM_AI_PROVIDER") {
		t.Fatalf("the error does not name the setting: %v", err)
	}
}

func TestAIProviderNoneIsAcceptedAndDisablesAI(t *testing.T) {
	baseEnv(t)
	// Enabled is asked for, but a "none" provider must still switch the layer off
	// rather than build nothing and report itself on.
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_PROVIDER", "none")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Enabled {
		t.Fatal("AI stayed enabled with provider=none")
	}
}

func TestGroqProviderFillsInItsPublicEndpoint(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_PROVIDER", "groq")
	t.Setenv("FORGECRM_AI_MODEL", "openai/gpt-oss-20b")
	// Deliberately leave FORGECRM_AI_BASE_URL unset: naming the provider is enough.
	t.Setenv("FORGECRM_AI_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.BaseURL != "https://api.groq.com/openai/v1" {
		t.Fatalf("groq base URL = %q, want the public endpoint", cfg.AI.BaseURL)
	}
	if !cfg.AI.Enabled {
		t.Fatal("AI is not enabled with a groq provider and a model")
	}
}

func TestExplicitAIBaseURLOverridesTheProviderDefault(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_PROVIDER", "groq")
	t.Setenv("FORGECRM_AI_MODEL", "openai/gpt-oss-20b")
	// A self-hosted gateway that speaks the same protocol must win over the
	// hosted default.
	t.Setenv("FORGECRM_AI_BASE_URL", "https://gateway.internal.example/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.BaseURL != "https://gateway.internal.example/v1" {
		t.Fatalf("explicit base URL was ignored: %q", cfg.AI.BaseURL)
	}
}

func TestAIProviderIsNormalised(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_PROVIDER", "  Groq ")
	t.Setenv("FORGECRM_AI_MODEL", "openai/gpt-oss-20b")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v; the value should be trimmed and lowercased, not rejected", err)
	}
	if cfg.AI.Provider != "groq" {
		t.Fatalf("provider = %q, want groq", cfg.AI.Provider)
	}
}

// TestOpenRouterProviderFillsInItsPublicEndpoint pins the OpenRouter-specific
// default: a deployment that names the provider and a key but omits the base URL
// must still reach OpenRouter. The groq default and the generic override are
// covered above; this is the one hosted provider whose endpoint the loader
// normalises rather than reading from the base-URL table.
func TestOpenRouterProviderFillsInItsPublicEndpoint(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_PROVIDER", "openrouter")
	t.Setenv("FORGECRM_AI_API_KEY", "sk-or-test")
	t.Setenv("FORGECRM_AI_MODEL", "google/gemma-4-31b-it:free")
	t.Setenv("FORGECRM_AI_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("openrouter base URL = %q, want the public endpoint", cfg.AI.BaseURL)
	}
}
