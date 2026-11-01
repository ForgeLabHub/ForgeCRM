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
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/retention"
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

	// DatabaseMaxConns bounds one process's database connection pool.
	//
	// It is configurable because the number that matters is replicas x pool, and
	// only the operator knows the replica count and the server's max_connections.
	// A value that would exhaust the server is rejected at startup rather than
	// clamped, so the misconfiguration is visible immediately.
	DatabaseMaxConns int32
	// LoginRateLimitPerMin bounds authentication attempts per client IP per minute.
	// It is configurable because the right value is deployment-specific: an office
	// behind one NAT address shares a single bucket, so a budget tuned for one
	// person locks out everybody.
	LoginRateLimitPerMin int
	RateLimitPerMin      int

	// RateLimitStore selects where limiter counters live. "memory" keeps them in
	// this process, which is correct for one replica and means each replica of a
	// larger deployment enforces its own copy of the budget. "redis" shares them,
	// which is required once more than one replica serves traffic.
	RateLimitStore string
	// RedisURL is the connection string for the shared limiter store, for example
	// redis://localhost:6379/0. It is required when RateLimitStore is "redis".
	RedisURL string
	// TrustedProxies lists the CIDR ranges or addresses whose X-Forwarded-For and
	// X-Real-Ip headers are believed. Empty trusts nothing, so the peer address is
	// used and the headers are ignored. A deployment behind a reverse proxy must
	// name that proxy here; otherwise every request is attributed to the proxy's
	// address and one client's traffic is counted against another's budget.
	TrustedProxies string

	SecretKey string

	WorkerConcurrency int
	WorkerPollEvery   time.Duration
	JobMaxAttempts    int
	// WorkerShutdownGrace bounds how long the worker waits for an in-flight job to
	// finish after a shutdown signal. Past it the job's context is cancelled and the
	// run is abandoned, so the process still exits inside its container stop window.
	WorkerShutdownGrace time.Duration

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

	Bootstrap BootstrapConfig
	Email     EmailConfig
	Telegram  TelegramConfig
	AI        AIConfig
	OIDC      OIDCConfig
	Retention RetentionConfig
	Storage   StorageConfig
	MCP       MCPConfig

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

// MCPConfig configures credentials for external MCP clients.
type MCPConfig struct {
	// RequestsPerMin bounds tool calls made through one credential, per minute. It is
	// a per-credential budget rather than a per-process one, so a noisy client cannot
	// spend another client's allowance, and the limit holds across replicas when a
	// shared limiter store is configured.
	RequestsPerMin int
}

// RetentionConfig configures the data-retention sweeps.
//
// Retention is opt-in and previews before it deletes: a deployment that sets
// nothing keeps every row. Two switches are separate on purpose — Enabled starts
// measuring, and AllowAudit is the second, deliberate step for evidence tables.
// DryRun defaults to true, so enabling retention does not by itself delete data.
type RetentionConfig struct {
	// Enabled turns the sweep on. When false the runner is constructed but refuses
	// to run, and the worker logs that retention is off.
	Enabled bool
	// DryRun measures without deleting.
	DryRun bool
	// AllowAudit permits the protected audit and security tables to be swept. It is
	// separate from Enabled because removing the record of who did what is a
	// different decision from removing a finished job.
	AllowAudit bool
	// Windows overrides the default window for a policy key, for example
	// {"jobs_finished": "720h"}. A key the registry does not know is refused at
	// startup rather than ignored, so a typo cannot silently do nothing.
	Windows map[string]time.Duration
}

// StorageConfig configures the optional S3-compatible object store used for CRM
// file attachments.
//
// The subsystem is opt-in: with no endpoint configured the server starts without
// object storage and the document routes report that it is unavailable, rather
// than accepting an upload and failing later. Credentials are read from the
// environment, never from the database, and are never echoed back in diagnostics.
type StorageConfig struct {
	// Enabled turns object storage on. When false every document route refuses with
	// "not configured" rather than a storage error.
	Enabled bool
	// Endpoint is the S3-compatible base URL, for example https://s3.example.com or
	// http://127.0.0.1:9000 for a local MinIO. It is one provider-agnostic URL, not a
	// cloud-specific setting.
	Endpoint string
	// Region is required by the AWS signing algorithm. MinIO and most S3-compatible
	// servers accept any value as long as it is consistent.
	Region string
	// Bucket holds the objects.
	Bucket string
	// AccessKeyID and SecretAccessKey are the credentials. They are read from the
	// environment so a deployment can inject them from a secret manager, and they are
	// never written to a log or returned by an API.
	AccessKeyID     string
	SecretAccessKey string
	// UsePathStyle uses path-style addressing (host/bucket/key). MinIO and many
	// self-hosted gateways require it; AWS prefers virtual-hosted style.
	UsePathStyle bool
	// AllowPrivateEndpoint permits an endpoint whose host resolves to a private or
	// loopback address. It is off by default because an operator-supplied endpoint is
	// an SSRF target, the same reasoning as the integration guard. A deployment that
	// runs MinIO on its own network turns it on deliberately.
	AllowPrivateEndpoint bool
	// MaxObjectBytes caps the size of a single object.
	MaxObjectBytes int64
	// PresignTTL is how long a download link stays valid.
	PresignTTL time.Duration
	// Timeout bounds a single storage operation.
	Timeout time.Duration
}

// BootstrapConfig carries the first-administrator provisioning values.
//
// They are read from the environment so the password never appears in the
// process list. The API only acts on them when no platform owner exists yet; an
// operator can leave them set, and they cannot be used to overwrite an owner
// that already exists.
type BootstrapConfig struct {
	// AdminEmail and AdminPassword provision the first platform owner. Both must
	// be set together; setting only one is a configuration error, because a
	// half-specified account is a typo rather than a request.
	AdminEmail    string
	AdminPassword string
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

// firstEnv returns the first of the named variables that is set to a non-empty
// value, so a renamed setting can keep accepting its old name during a migration.
func firstEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// defaultBaseURL resolves the public origin. An explicit FORGECRM_BASE_URL wins;
// otherwise the hosting platform's published URL is used; otherwise the local
// development default.
//
// The platform value matters for production: a Blueprint that sets
// FORGECRM_ENV=production but no FORGECRM_BASE_URL would otherwise fall back to
// plain http, and the production check below refuses to start on a non-https
// base URL. Render injects RENDER_EXTERNAL_URL as the service's own https URL,
// so reading it keeps a Blueprint deployable without a hardcoded hostname.
func defaultBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("FORGECRM_BASE_URL")); v != "" {
		return v
	}
	if v := platformExternalURL(); v != "" {
		return v
	}
	return "http://localhost:8080"
}

// platformExternalURL returns the public origin a hosting platform publishes for
// this service, or "" when there is none.
func platformExternalURL() string {
	if v := strings.TrimSpace(os.Getenv("RENDER_EXTERNAL_URL")); v != "" {
		return v
	}
	return ""
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
		Env: envName,
		HTTPAddr: func() string {
			if port := env("PORT", ""); port != "" {
				if strings.HasPrefix(port, ":") {
					return "0.0.0.0" + port
				}
				return "0.0.0.0:" + port
			}
			return env("FORGECRM_HTTP_ADDR", ":8080")
		}(),
		BaseURL:     strings.TrimRight(defaultBaseURL(), "/"),
		DatabaseURL: env("FORGECRM_DATABASE_URL", env("DATABASE_URL", "")),

		DatabaseMaxConns: int32(envIntOr("FORGECRM_DATABASE_MAX_CONNS", int(database.DefaultMaxConns))),

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

		LockoutThreshold:     envIntOr("FORGECRM_LOCKOUT_THRESHOLD", 8),
		LoginRateLimitPerMin: envIntOr("FORGECRM_LOGIN_RATE_LIMIT_PER_MIN", 60),
		RateLimitPerMin:      envIntOr("FORGECRM_RATE_LIMIT_PER_MIN", 300),
		RateLimitStore:       env("FORGECRM_RATE_LIMIT_STORE", "memory"),
		RedisURL:             env("FORGECRM_REDIS_URL", ""),
		TrustedProxies:       env("FORGECRM_TRUSTED_PROXIES", ""),

		Bootstrap: BootstrapConfig{
			AdminEmail: env("FORGECRM_BOOTSTRAP_ADMIN_EMAIL", ""),
			// FORGECRM_BOOTSTRAP_PASSWORD is the original name and is still
			// accepted so an existing deploy script keeps working; the new name
			// wins when both are present.
			AdminPassword: firstEnv("FORGECRM_BOOTSTRAP_ADMIN_PASSWORD", "FORGECRM_BOOTSTRAP_PASSWORD"),
		},

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
	if c.WorkerShutdownGrace, err = envDuration("FORGECRM_WORKER_SHUTDOWN_GRACE", 30*time.Second); err != nil {
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

	c.MCP = MCPConfig{
		RequestsPerMin: envIntOr("FORGECRM_MCP_REQUESTS_PER_MIN", 120),
	}

	c.Retention = RetentionConfig{
		Enabled:    envBool("FORGECRM_RETENTION_ENABLED", false),
		DryRun:     envBool("FORGECRM_RETENTION_DRY_RUN", true),
		AllowAudit: envBool("FORGECRM_RETENTION_ALLOW_AUDIT", false),
		Windows:    map[string]time.Duration{},
	}
	if raw := env("FORGECRM_RETENTION_WINDOWS", ""); raw != "" {
		windows, err := parseRetentionWindows(raw)
		if err != nil {
			return nil, err
		}
		// A key the registry does not know is refused here, where the operator can
		// still see the error, rather than ignored so a typo silently keeps the
		// default window.
		if err := retention.ValidateWindows(windows); err != nil {
			return nil, fmt.Errorf("FORGECRM_RETENTION_WINDOWS: %w", err)
		}
		c.Retention.Windows = windows
	}

	c.Storage = StorageConfig{
		Enabled:              envBool("FORGECRM_STORAGE_ENABLED", false),
		Endpoint:             env("FORGECRM_STORAGE_ENDPOINT", ""),
		Region:               env("FORGECRM_STORAGE_REGION", "us-east-1"),
		Bucket:               env("FORGECRM_STORAGE_BUCKET", ""),
		AccessKeyID:          env("FORGECRM_STORAGE_ACCESS_KEY_ID", ""),
		SecretAccessKey:      env("FORGECRM_STORAGE_SECRET_ACCESS_KEY", ""),
		UsePathStyle:         envBool("FORGECRM_STORAGE_USE_PATH_STYLE", true),
		AllowPrivateEndpoint: envBool("FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT", false),
		MaxObjectBytes:       envInt64Or("FORGECRM_STORAGE_MAX_OBJECT_BYTES", 64<<20),
	}
	if c.Storage.PresignTTL, err = envDuration("FORGECRM_STORAGE_PRESIGN_TTL", 15*time.Minute); err != nil {
		return nil, err
	}
	if c.Storage.Timeout, err = envDuration("FORGECRM_STORAGE_TIMEOUT", 30*time.Second); err != nil {
		return nil, err
	}
	// An enabled subsystem with a missing setting is a configuration error, not a
	// silently disabled one: the operator asked for storage and would otherwise find
	// out when the first upload fails.
	if c.Storage.Enabled {
		switch {
		case c.Storage.Endpoint == "":
			return nil, fmt.Errorf("FORGECRM_STORAGE_ENDPOINT is required when FORGECRM_STORAGE_ENABLED=true")
		case c.Storage.Bucket == "":
			return nil, fmt.Errorf("FORGECRM_STORAGE_BUCKET is required when FORGECRM_STORAGE_ENABLED=true")
		case c.Storage.AccessKeyID == "" || c.Storage.SecretAccessKey == "":
			return nil, fmt.Errorf("FORGECRM_STORAGE_ACCESS_KEY_ID and FORGECRM_STORAGE_SECRET_ACCESS_KEY are required when FORGECRM_STORAGE_ENABLED=true")
		case c.Storage.MaxObjectBytes <= 0:
			return nil, fmt.Errorf("FORGECRM_STORAGE_MAX_OBJECT_BYTES must be positive")
		}
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

// parseRetentionWindows reads the retention window overrides.
//
// The value is a comma-separated list of key=duration pairs, for example
// "jobs_finished=720h,app_logs=168h". A malformed pair or a duplicate key is an
// error rather than a silent skip: an operator who mistypes a window must find out
// at startup, not discover months later that nothing was swept — or worse, that a
// default they meant to override was used instead.
func parseRetentionWindows(raw string) (map[string]time.Duration, error) {
	out := map[string]time.Duration{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("FORGECRM_RETENTION_WINDOWS: %q is not a key=duration pair", pair)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("FORGECRM_RETENTION_WINDOWS: %q is given twice", key)
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("FORGECRM_RETENTION_WINDOWS: %q has an invalid duration: %w", key, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("FORGECRM_RETENTION_WINDOWS: %q must be positive, got %s", key, d)
		}
		out[key] = d
	}
	return out, nil
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
	if c.DatabaseMaxConns > database.MaxAllowedConns {
		return fmt.Errorf(
			"FORGECRM_DATABASE_MAX_CONNS is %d, above the maximum of %d; size each "+
				"replica's pool so that replicas x pool stays below the server's max_connections",
			c.DatabaseMaxConns, database.MaxAllowedConns)
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
	// The limit is validated here so a bad value is a startup failure next to the
	// setting, not a wiring error later. Zero means "do not limit"; a negative
	// value is a mistake, not a way to spell unlimited.
	if c.AI.MaxRequestsMin < 0 {
		return fmt.Errorf("FORGECRM_AI_MAX_REQUESTS_PER_MIN must not be negative, got %d", c.AI.MaxRequestsMin)
	}
	if c.MCP.RequestsPerMin < 0 {
		return fmt.Errorf("FORGECRM_MCP_REQUESTS_PER_MIN must not be negative, got %d", c.MCP.RequestsPerMin)
	}
	if c.OIDC.Enabled && (c.OIDC.Issuer == "" || c.OIDC.ClientID == "") {
		return fmt.Errorf("OIDC enabled but FORGECRM_OIDC_ISSUER or FORGECRM_OIDC_CLIENT_ID is empty")
	}

	// The limiter store is validated here rather than at first use, so a typo in the
	// store name is a startup failure next to the setting that caused it instead of a
	// silent fall back to per-process counting — which is the failure this setting
	// exists to prevent, and which would be invisible until someone counted requests.
	switch c.RateLimitStore {
	case "memory":
		if c.RedisURL != "" {
			return fmt.Errorf("FORGECRM_REDIS_URL is set but FORGECRM_RATE_LIMIT_STORE is \"memory\"; set it to \"redis\" or clear the URL")
		}
	case "redis":
		if c.RedisURL == "" {
			return fmt.Errorf("FORGECRM_RATE_LIMIT_STORE is \"redis\" but FORGECRM_REDIS_URL is empty")
		}
	default:
		return fmt.Errorf("FORGECRM_RATE_LIMIT_STORE must be \"memory\" or \"redis\", got %q", c.RateLimitStore)
	}

	// A malformed trusted-proxy list is rejected here. Left unchecked it would make
	// every request look untrusted, which silently disables the proxy-aware client
	// address and collapses all traffic into the proxy's own bucket.
	if _, err := httpx.TrustedProxiesFrom(c.TrustedProxies); err != nil {
		return fmt.Errorf("FORGECRM_TRUSTED_PROXIES is invalid: %w", err)
	}

	// URLs are parsed, not merely checked for emptiness, so a typo ("htp://",
	// a missing host) is a startup failure rather than a redirect or connection
	// error at the first request that uses it.
	if err := validateHTTPURL("FORGECRM_BASE_URL", c.BaseURL); err != nil {
		return err
	}
	if err := validatePostgresURL("FORGECRM_DATABASE_URL", c.DatabaseURL); err != nil {
		return err
	}
	if c.RedisURL != "" {
		if err := validateRedisURL("FORGECRM_REDIS_URL", c.RedisURL); err != nil {
			return err
		}
	}
	if c.Storage.Enabled && c.Storage.Endpoint != "" {
		if err := validateHTTPURL("FORGECRM_STORAGE_ENDPOINT", c.Storage.Endpoint); err != nil {
			return err
		}
	}
	if c.OIDC.Enabled && c.OIDC.Issuer != "" {
		if err := validateHTTPURL("FORGECRM_OIDC_ISSUER", c.OIDC.Issuer); err != nil {
			return err
		}
	}

	// Production hardenings. These are errors, not warnings, because each one is a
	// silent downgrade of a security property if it slips through.
	if c.IsProduction() {
		if len(c.SecretKey) < 32 {
			// The value is never printed; only its length is mentioned.
			return fmt.Errorf("FORGECRM_SECRET_KEY must be at least 32 characters in production (got %d)", len(c.SecretKey))
		}
		if !c.CookieSecure {
			return fmt.Errorf("FORGECRM_COOKIE_SECURE must be true when FORGECRM_ENV=production, so the session cookie is never sent over plain HTTP")
		}
		if strings.HasPrefix(c.BaseURL, "http://") {
			return fmt.Errorf("FORGECRM_BASE_URL must use https in production, got %q", c.BaseURL)
		}
	}

	return nil
}

// validateHTTPURL rejects a value that is not an absolute http(s) URL with a host.
func validateHTTPURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must be an http or https URL, got scheme %q", name, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s is missing a host", name)
	}
	return nil
}

// validatePostgresURL rejects a database URL that is not a PostgreSQL connection
// string. The password is never included in the error.
func validatePostgresURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", name, err)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
	default:
		return fmt.Errorf("%s must be a postgres:// or postgresql:// URL, got scheme %q", name, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s is missing a host", name)
	}
	return nil
}

// validateRedisURL rejects a rate-limit store URL that is not redis://.
func validateRedisURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", name, err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return fmt.Errorf("%s must be a redis:// or rediss:// URL, got scheme %q", name, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s is missing a host", name)
	}
	return nil
}

// Redacted is a log-safe rendering of the configuration: which optional
// subsystems are on, and whether each secret-bearing setting is present, never
// its value.
//
// Key names deliberately avoid the substrings the logging package treats as
// sensitive (password, secret, token, key, credential, ...). That is not
// cosmetic: the logger replaces the value of any field whose name looks
// sensitive, so a key like "secret_key" would be printed as [REDACTED] and the
// useful fact — present or absent — would be lost. Naming the field for the fact
// ("signing_material_configured") keeps the diagnostic and still never carries a
// secret.
func (c *Config) Redacted() []any {
	state := func(v string) string {
		if v == "" {
			return "absent"
		}
		return "present"
	}
	return []any{
		"env", c.Env,
		"http_addr", c.HTTPAddr,
		"base_url", c.BaseURL,
		"log_level", c.LogLevel,
		"log_format", c.LogFormat,
		"migrate_on_start", c.RunMigrationsOnStart,
		"rate_limit_store", c.RateLimitStore,
		"trusted_proxies", c.TrustedProxies,
		"signing_material_configured", state(c.SecretKey),
		"metrics_access_configured", state(c.MetricsToken),
		"smtp_enabled", c.Email.Enabled,
		"smtp_auth_configured", state(c.Email.Password),
		"telegram_enabled", c.Telegram.Enabled,
		"telegram_auth_configured", state(c.Telegram.BotToken),
		"ai_enabled", c.AI.Enabled,
		"ai_auth_configured", state(c.AI.APIKey),
		"storage_enabled", c.Storage.Enabled,
		"storage_auth_configured", state(c.Storage.SecretAccessKey),
		"oidc_enabled", c.OIDC.Enabled,
		"oidc_auth_configured", state(c.OIDC.ClientSecret),
		"shared_limiter_configured", state(c.RedisURL),
		"bootstrap_admin_configured", state(c.Bootstrap.AdminEmail),
	}
}

// ValidateBootstrap checks the environment-driven bootstrap pairing. It is a
// separate step from Validate because the pairing rule describes what the process
// will do, not whether the configuration is well-formed: the one-off
// `-bootstrap-admin` command supplies the address through a flag and the password
// through the environment, a valid combination that an all-or-nothing rule in
// Validate would reject before the command ever runs. The server-start path calls
// this; the command path relies on bootstrap.Admin, which performs the same check.
func (c *Config) ValidateBootstrap() error {
	if (c.Bootstrap.AdminEmail == "") == (c.Bootstrap.AdminPassword == "") {
		return nil
	}
	missing := "FORGECRM_BOOTSTRAP_ADMIN_PASSWORD"
	if c.Bootstrap.AdminPassword != "" {
		missing = "FORGECRM_BOOTSTRAP_ADMIN_EMAIL"
	}
	return fmt.Errorf("FORGECRM_BOOTSTRAP_ADMIN_EMAIL and FORGECRM_BOOTSTRAP_ADMIN_PASSWORD must be set together; %s is missing", missing)
}

// IsProduction reports whether the process runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }
