// Package config loads and validates ForgeCRM configuration from the environment.
//
// Every setting has a safe development default except the database URL and the
// bootstrap credentials, which must be supplied explicitly. Configuration is
// validated at startup so that a misconfigured deployment fails fast rather than
// at the first request.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	Env         string
	HTTPAddr    string
	BaseURL     string
	DatabaseURL string

	SessionTTL        time.Duration
	SessionCookieName string
	CookieSecure      bool
	CookieDomain      string
	CSRFHeader        string
	LockoutThreshold  int
	LockoutWindow     time.Duration
	RateLimitPerMin   int
	SecretKey         string

	WorkerConcurrency int
	WorkerPollEvery   time.Duration
	JobMaxAttempts    int

	ImportMaxUploadBytes   int64
	ImportMaxUnpackedBytes int64
	ImportMaxArchiveFiles  int
	ImportMaxArchiveDepth  int
	ImportMaxRatio         float64
	ImportBatchSize        int
	ImportTempDir          string
	// ImportStorageDir is the private directory where uploaded files are kept.
	// It must never be served over HTTP.
	ImportStorageDir string

	// RunMigrationsOnStart applies pending migrations during startup. It is a
	// convenience for a single-node deployment and should be off when migrations
	// are run as a separate step.
	RunMigrationsOnStart bool

	Email    EmailConfig
	Telegram TelegramConfig
	AI       AIConfig
	OIDC     OIDCConfig

	LogLevel     string
	LogFormat    string
	MetricsToken string
}

// EmailConfig configures the SMTP notification transport.
type EmailConfig struct {
	Enabled   bool
	Host      string
	Port      int
	Username  string
	Password  string
	From      string
	FromName  string
	UseTLS    bool
	StartTLS  bool
	Timeout   time.Duration
	PublicURL string
}

// TelegramConfig configures the optional Telegram bot.
type TelegramConfig struct {
	Enabled       bool
	BotToken      string
	WebhookSecret string
}

// AIConfig configures the optional AI layer.
type AIConfig struct {
	Enabled        bool
	Provider       string
	BaseURL        string
	APIKey         string
	Model          string
	Timeout        time.Duration
	MaxRequestsMin int
}

// OIDCConfig configures optional corporate SSO.
type OIDCConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt64Or(key string, def int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envFloatOr(key string, def float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	envName := env("FORGECRM_ENV", "development")
	prod := envName == "production"

	c := &Config{
		Env:         envName,
		HTTPAddr:    env("FORGECRM_HTTP_ADDR", ":8080"),
		BaseURL:     strings.TrimRight(env("FORGECRM_BASE_URL", "http://localhost:8080"), "/"),
		DatabaseURL: env("FORGECRM_DATABASE_URL", ""),

		SessionCookieName: env("FORGECRM_SESSION_COOKIE", "forgecrm_session"),
		CookieSecure:      envBool("FORGECRM_COOKIE_SECURE", prod),
		CookieDomain:      env("FORGECRM_COOKIE_DOMAIN", ""),
		CSRFHeader:        env("FORGECRM_CSRF_HEADER", "X-CSRF-Token"),
		SecretKey:         env("FORGECRM_SECRET_KEY", ""),

		WorkerConcurrency: envIntOr("FORGECRM_WORKER_CONCURRENCY", 4),
		JobMaxAttempts:    envIntOr("FORGECRM_JOB_MAX_ATTEMPTS", 5),

		ImportMaxUploadBytes:   envInt64Or("FORGECRM_IMPORT_MAX_UPLOAD_BYTES", 256<<20),
		ImportMaxUnpackedBytes: envInt64Or("FORGECRM_IMPORT_MAX_UNPACKED_BYTES", 2<<30),
		ImportMaxArchiveFiles:  envIntOr("FORGECRM_IMPORT_MAX_ARCHIVE_FILES", 5000),
		ImportMaxArchiveDepth:  envIntOr("FORGECRM_IMPORT_MAX_ARCHIVE_DEPTH", 8),
		ImportMaxRatio:         envFloatOr("FORGECRM_IMPORT_MAX_RATIO", 200),
		ImportBatchSize:        envIntOr("FORGECRM_IMPORT_BATCH_SIZE", 500),
		ImportTempDir:          env("FORGECRM_IMPORT_TEMP_DIR", ""),
		ImportStorageDir:       env("FORGECRM_IMPORT_STORAGE_DIR", ""),
		RunMigrationsOnStart:   envBool("FORGECRM_MIGRATE_ON_START", false),

		LockoutThreshold: envIntOr("FORGECRM_LOCKOUT_THRESHOLD", 8),
		RateLimitPerMin:  envIntOr("FORGECRM_RATE_LIMIT_PER_MIN", 300),

		LogLevel:     env("FORGECRM_LOG_LEVEL", "info"),
		LogFormat:    env("FORGECRM_LOG_FORMAT", "json"),
		MetricsToken: env("FORGECRM_METRICS_TOKEN", ""),
	}

	var err error
	if c.SessionTTL, err = envDuration("FORGECRM_SESSION_TTL", 12*time.Hour); err != nil {
		return nil, err
	}
	if c.LockoutWindow, err = envDuration("FORGECRM_LOCKOUT_WINDOW", 15*time.Minute); err != nil {
		return nil, err
	}
	if c.WorkerPollEvery, err = envDuration("FORGECRM_WORKER_POLL", 2*time.Second); err != nil {
		return nil, err
	}

	c.Email = EmailConfig{
		Enabled:   envBool("FORGECRM_SMTP_ENABLED", false),
		Host:      env("FORGECRM_SMTP_HOST", ""),
		Port:      envIntOr("FORGECRM_SMTP_PORT", 587),
		Username:  env("FORGECRM_SMTP_USERNAME", ""),
		Password:  env("FORGECRM_SMTP_PASSWORD", ""),
		From:      env("FORGECRM_SMTP_FROM", ""),
		FromName:  env("FORGECRM_SMTP_FROM_NAME", "ForgeCRM"),
		UseTLS:    envBool("FORGECRM_SMTP_TLS", true),
		StartTLS:  envBool("FORGECRM_SMTP_STARTTLS", true),
		PublicURL: env("FORGECRM_SMTP_PUBLIC_URL", ""),
	}
	if c.Email.Timeout, err = envDuration("FORGECRM_SMTP_TIMEOUT", 15*time.Second); err != nil {
		return nil, err
	}

	c.Telegram = TelegramConfig{
		Enabled:       envBool("FORGECRM_TELEGRAM_ENABLED", false),
		BotToken:      env("FORGECRM_TELEGRAM_BOT_TOKEN", ""),
		WebhookSecret: env("FORGECRM_TELEGRAM_WEBHOOK_SECRET", ""),
	}
	if c.Telegram.Enabled && c.Telegram.BotToken == "" {
		// A token-less bot disables itself rather than breaking startup.
		c.Telegram.Enabled = false
	}

	c.AI = AIConfig{
		Enabled:        envBool("FORGECRM_AI_ENABLED", false),
		Provider:       env("FORGECRM_AI_PROVIDER", "none"),
		BaseURL:        env("FORGECRM_AI_BASE_URL", ""),
		APIKey:         env("FORGECRM_AI_API_KEY", ""),
		Model:          env("FORGECRM_AI_MODEL", ""),
		MaxRequestsMin: envIntOr("FORGECRM_AI_MAX_REQUESTS_PER_MIN", 30),
	}
	if c.AI.Timeout, err = envDuration("FORGECRM_AI_TIMEOUT", 60*time.Second); err != nil {
		return nil, err
	}
	if c.AI.Provider == "none" || c.AI.Provider == "" {
		c.AI.Enabled = false
	}

	c.OIDC = OIDCConfig{
		Enabled:      envBool("FORGECRM_OIDC_ENABLED", false),
		Issuer:       env("FORGECRM_OIDC_ISSUER", ""),
		ClientID:     env("FORGECRM_OIDC_CLIENT_ID", ""),
		ClientSecret: env("FORGECRM_OIDC_CLIENT_SECRET", ""),
		RedirectURL:  env("FORGECRM_OIDC_REDIRECT_URL", ""),
		Scopes:       strings.Fields(env("FORGECRM_OIDC_SCOPES", "openid email profile")),
	}

	if c.SecretKey == "" {
		if prod {
			return nil, fmt.Errorf("FORGECRM_SECRET_KEY is required when FORGECRM_ENV=production")
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate dev secret: %w", err)
		}
		c.SecretKey = base64.RawURLEncoding.EncodeToString(buf)
	}

	// Both import directories default under the OS temporary directory. They are
	// private working areas, never served over HTTP.
	if c.ImportTempDir == "" {
		c.ImportTempDir = filepath.Join(os.TempDir(), "forgecrm-import-tmp")
	}
	if c.ImportStorageDir == "" {
		c.ImportStorageDir = filepath.Join(os.TempDir(), "forgecrm-import-storage")
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

// Validate checks internal consistency of the configuration.
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("FORGECRM_DATABASE_URL is required")
	}
	switch c.Env {
	case "development", "test", "production":
	default:
		return fmt.Errorf("FORGECRM_ENV must be development, test or production, got %q", c.Env)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("FORGECRM_LOG_LEVEL must be debug, info, warn or error, got %q", c.LogLevel)
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("FORGECRM_LOG_FORMAT must be json or text, got %q", c.LogFormat)
	}
	if c.SessionTTL <= 0 {
		return fmt.Errorf("FORGECRM_SESSION_TTL must be positive")
	}
	if c.ImportMaxUploadBytes <= 0 || c.ImportMaxUnpackedBytes <= 0 {
		return fmt.Errorf("import size limits must be positive")
	}
	if c.ImportMaxUnpackedBytes < c.ImportMaxUploadBytes {
		return fmt.Errorf("FORGECRM_IMPORT_MAX_UNPACKED_BYTES must be >= FORGECRM_IMPORT_MAX_UPLOAD_BYTES")
	}
	if c.ImportMaxArchiveFiles <= 0 || c.ImportMaxArchiveDepth <= 0 {
		return fmt.Errorf("archive entry and depth limits must be positive")
	}
	if c.ImportMaxRatio <= 0 {
		return fmt.Errorf("FORGECRM_IMPORT_MAX_RATIO must be positive")
	}
	if c.ImportBatchSize <= 0 || c.ImportBatchSize > 100000 {
		return fmt.Errorf("FORGECRM_IMPORT_BATCH_SIZE must be between 1 and 100000")
	}
	if c.Email.Enabled && (c.Email.Host == "" || c.Email.From == "") {
		return fmt.Errorf("SMTP enabled but FORGECRM_SMTP_HOST or FORGECRM_SMTP_FROM is empty")
	}
	if c.AI.Enabled && (c.AI.BaseURL == "" || c.AI.Model == "") {
		return fmt.Errorf("AI enabled but FORGECRM_AI_BASE_URL or FORGECRM_AI_MODEL is empty")
	}
	if c.OIDC.Enabled && (c.OIDC.Issuer == "" || c.OIDC.ClientID == "") {
		return fmt.Errorf("OIDC enabled but FORGECRM_OIDC_ISSUER or FORGECRM_OIDC_CLIENT_ID is empty")
	}
	return nil
}

// IsProduction reports whether the process runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }
