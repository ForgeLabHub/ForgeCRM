// Command forgecrm-api runs the ForgeCRM HTTP API.
//
// Startup order matters and is deliberate: configuration is validated first, then
// the database, then migrations, then the HTTP listener. A failure at any step
// exits non-zero with a structured log line, so an orchestrator can tell the
// difference between "not configured" and "crashed".
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/geff1778/forgecrm/internal/api"
	"github.com/geff1778/forgecrm/internal/bootstrap"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/wiring"
)

// version is overridden at build time with -ldflags. When a build host does not
// pass the linker flags (a platform that builds the repository root and has no
// notion of build args), FORGECRM_VERSION is used instead, so the value reported
// by /ready and `-version` is the deployed version rather than the placeholder.
var version = "0.1.0-dev"

// commit is overridden at build time with -ldflags, and falls back to
// FORGECRM_COMMIT for the same reason as version. An empty or "dev" value is
// replaced, so a missing flag cannot masquerade as a real build.
var commit = "unknown"

// buildInfo returns the effective version and commit. Linker-set values win; the
// environment is the fallback; a placeholder is replaced by an explicit marker so
// an operator can tell "no metadata supplied" from a genuine value.
//
// A platform that builds the repository root without passing build args (Render)
// still knows the revision it checked out, so RENDER_GIT_COMMIT is consulted for
// the commit. That is what turns a log line of commit=unknown into the real SHA
// without a manual variable.
func buildInfo() (string, string) {
	v, c := version, commit
	if v == "" || v == "0.1.0-dev" || v == "dev" {
		if env := strings.TrimSpace(os.Getenv("FORGECRM_VERSION")); env != "" {
			v = env
		}
	}
	if c == "" || c == "unknown" || c == "dev" {
		if env := strings.TrimSpace(firstNonEmpty(os.Getenv("FORGECRM_COMMIT"), os.Getenv("RENDER_GIT_COMMIT"))); env != "" {
			c = env
		}
	}
	return v, c
}

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet, so report to stderr as well.
		fmt.Fprintln(os.Stderr, "forgecrm-api: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		runMigrations  = flag.Bool("migrate", false, "apply database migrations and exit")
		showVersion    = flag.Bool("version", false, "print the version and exit")
		bootstrapCmd   = flag.Bool("bootstrap-admin", false, "create the first platform administrator and exit")
		bootstrapEmail = flag.String("bootstrap-email", "", "email address for the bootstrap administrator")
		bootstrapName  = flag.String("bootstrap-name", "", "display name for the bootstrap administrator")
	)
	flag.Parse()

	if *showVersion {
		v, c := buildInfo()
		fmt.Printf("forgecrm-api %s (%s)\n", v, c)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	buildVersion, buildCommit := buildInfo()
	log.Info("starting forgecrm-api",
		"version", buildVersion, "commit", buildCommit, "env", cfg.Env)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	// Log which settings are present and which subsystems are on, so a deployment
	// can be inspected without a secret value ever reaching the log. The password
	// and every other secret is reported as "set" or "unset", never echoed.
	log.Info("configuration resolved", cfg.Redacted()...)

	if *runMigrations {
		applied, err := migrate.Up(cfg.DatabaseURL, log)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		log.Info("migrations applied", "count", applied)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.ConnectWithPool(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	defer db.Close()

	// Apply migrations on startup so a fresh deployment needs one command. In
	// production this is usually disabled and run as a separate step.
	if cfg.RunMigrationsOnStart {
		applied, err := migrate.Up(cfg.DatabaseURL, log)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		log.Info("migrations applied on startup", "count", applied)
	}

	// The schema gate. A reachable database is not a migrated database, and
	// serving before the schema exists is exactly what produced the production
	// failure this guard prevents: the process listened, the readiness probe
	// passed, and the first login died on `relation "users" does not exist`.
	// The check runs before the listener opens, so an un-migrated deployment fails
	// fast and loudly instead of accepting traffic it cannot serve.
	if err := verifySchema(ctx, db, cfg, log); err != nil {
		return err
	}

	keyring, err := secrets.NewKeyring(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("initialise the encryption keyring: %w", err)
	}

	// The bootstrap command runs the same code path the environment-driven
	// bootstrap uses, so a one-off invocation and a deployment variable cannot
	// diverge. Flags win over the environment for a single explicit run.
	if *bootstrapCmd {
		opts := bootstrap.Options{
			Email:       firstNonEmpty(*bootstrapEmail, cfg.Bootstrap.AdminEmail),
			Password:    cfg.Bootstrap.AdminPassword,
			DisplayName: *bootstrapName,
		}
		if strings.TrimSpace(opts.Email) == "" {
			return errors.New("-bootstrap-email is required (or set FORGECRM_BOOTSTRAP_ADMIN_EMAIL)")
		}
		_, err := bootstrap.Admin(ctx, db, log, opts)
		return err
	}

	// A deployment that sets the bootstrap variables expects the first owner to
	// exist after startup. Running it here means a fresh database becomes usable
	// without a separate manual step, and the no-op when an owner already exists
	// makes a restart harmless.
	if err := cfg.ValidateBootstrap(); err != nil {
		return fmt.Errorf("invalid bootstrap configuration: %w", err)
	}
	if _, err := bootstrap.Admin(ctx, db, log, bootstrap.Options{
		Email:    cfg.Bootstrap.AdminEmail,
		Password: cfg.Bootstrap.AdminPassword,
	}); err != nil {
		return fmt.Errorf("bootstrap the first administrator: %w", err)
	}

	// The limiter store is built before the services so that an unreachable Redis is
	// a startup failure, and so the AI limit shares the same counters as the API and
	// login limits. The alternative — starting anyway and discovering it on the
	// first request — reports the problem as a warning about an unenforced limit,
	// which is easy to miss and means the limit silently is not applied.
	limiterStore, err := api.NewLimiterStore(ctx, cfg)
	if err != nil {
		return fmt.Errorf("rate limit store: %w", err)
	}

	services, err := wiring.Build(cfg, db, keyring, log, limiterStore)
	if err != nil {
		return err
	}

	trustedProxies, err := httpx.TrustedProxiesFrom(cfg.TrustedProxies)
	if err != nil {
		// config.Validate already checked this; a failure here means the value changed
		// after validation, and continuing would silently ignore forwarding headers.
		return fmt.Errorf("trusted proxies: %w", err)
	}
	if trustedProxies.Len() == 0 {
		log.Info("no trusted proxies configured; client address is taken from the connection",
			"note", "set FORGECRM_TRUSTED_PROXIES when running behind a reverse proxy")
	}
	if trustedProxies.TrustsAny() {
		// Trusting every peer means any caller can choose its own address via a header.
		// That is a legitimate topology when the API is unreachable except through a
		// proxy, but it must be visible rather than assumed.
		log.Warn("FORGECRM_TRUSTED_PROXIES trusts every peer; " +
			"X-Forwarded-For is accepted from any caller and rate limits can be evaded")
	}
	log.Info("rate limiter configured",
		"store", limiterStore.String(),
		"api_per_min", cfg.RateLimitPerMin,
		"login_per_min", cfg.LoginRateLimitPerMin,
	)

	server, err := api.New(&api.Deps{
		Config:         cfg,
		Log:            log,
		DB:             db,
		Keyring:        keyring,
		Audit:          services.Audit,
		Auth:           services.Auth,
		RBAC:           services.RBAC,
		CRM:            services.CRM,
		Jobs:           services.Jobs,
		Outbox:         services.Outbox,
		Imports:        services.Imports,
		Migrations:     services.Migrations,
		Integrations:   services.Integrations,
		Events:         services.Events,
		Dispatcher:     services.Dispatcher,
		Email:          services.Email,
		Telegram:       services.Telegram,
		AI:             services.AI,
		Tools:          services.Tools,
		MCP:            services.MCP,
		Agent:          services.Agent,
		Workflows:      services.Workflows,
		Simulation:     services.Simulation,
		Forecast:       services.Forecast,
		Risk:           services.Risk,
		Storage:        services.Storage,
		OIDC:           services.OIDC,
		Views:          services.Views,
		Consent:        services.Consent,
		TenantConfig:   services.TenantConfig,
		MCPCredentials: services.MCPCredentials,
		Idempotency:    services.Idempotency,
		StartedAt:      time.Now(),
		Version:        buildVersion,
		BuildCommit:    buildCommit,
	}, limiterStore, trustedProxies)
	if err != nil {
		return err
	}
	// The store holds a connection pool when Redis is configured; closing it on the
	// way out releases those sockets rather than waiting for the process to exit.
	defer server.Close()

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: server.Handler(),
		// Timeouts are set explicitly: a server without them is trivially held
		// open by a slow client.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listener started", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Graceful shutdown drains in-flight requests before exiting.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Warn("graceful shutdown did not complete", "error", err)
		return err
	}
	log.Info("shutdown complete")
	return nil
}

// verifySchema refuses to start when the database does not yet carry the schema
// the binary expects.
//
// This is the guard that closes the production failure this change set is about.
// Before it, the API connected, applied migrations only when told to, and then
// listened regardless: with FORGECRM_MIGRATE_ON_START unset on a fresh database
// the process served happily while every query failed with `relation "users" does
// not exist`. The distinction it enforces is between "the database is reachable"
// and "the schema is ready", which the readiness probe alone did not make.
//
// When migrations are configured to run on startup they have already run by the
// time this is called, so a failure here is a real schema problem and not a
// missing flag. When they are not, the error names the two ways to apply them.
func verifySchema(ctx context.Context, db *database.DB, cfg *config.Config, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := migrate.Check(ctx, db.Pool()); err != nil {
		if migrate.IsSchemaNotReady(err) && !cfg.RunMigrationsOnStart {
			return fmt.Errorf("%w (FORGECRM_MIGRATE_ON_START is false, so the API did not apply them; "+
				"set it to true or run `forgecrm-api -migrate` as a deploy step)", err)
		}
		return err
	}

	state, err := migrate.Status(cfg.DatabaseURL)
	if err != nil {
		// The tables exist, so this is informational rather than fatal: the version
		// table may be absent when the schema was built by another tool.
		log.Warn("schema is present but its migration version could not be read", "error", err)
		return nil
	}
	if state.Dirty {
		return fmt.Errorf("database schema is dirty at version %d; a migration failed partway "+
			"and must be repaired before the API can serve", state.Version)
	}
	log.Info("schema ready", "version", state.Version)
	return nil
}

// firstNonEmpty returns the first non-blank value, used to let an explicit flag
// override an environment variable without repeating the blank check.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
