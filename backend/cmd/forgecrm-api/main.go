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

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/api"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/wiring"
)

// version is overridden at build time with -ldflags.
var version = "0.1.0-dev"

// commit is overridden at build time with -ldflags.
var commit = "unknown"

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
		bootstrap      = flag.Bool("bootstrap-admin", false, "create the first platform administrator and exit")
		bootstrapEmail = flag.String("bootstrap-email", "", "email address for the bootstrap administrator")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("forgecrm-api %s (%s)\n", version, commit)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting forgecrm-api",
		"version", version, "commit", commit, "env", cfg.Env)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

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

	keyring, err := secrets.NewKeyring(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("initialise the encryption keyring: %w", err)
	}

	if *bootstrap {
		return bootstrapAdmin(ctx, db, keyring, cfg, log, *bootstrapEmail)
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
		MCPCredentials: services.MCPCredentials,
		StartedAt:      time.Now(),
		Version:        version,
		BuildCommit:    commit,
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

// bootstrapAdmin creates the first platform administrator.
//
// Design choices:
//   - the account is created only if no platform owner exists, so the command is
//     safe to leave in a deployment script;
//   - the password is read from the environment, never from an argument, so it
//     does not appear in the process list or shell history;
//   - a password is never defaulted: an operator must supply one.
func bootstrapAdmin(ctx context.Context, db *database.DB, keyring *secrets.Keyring,
	cfg *config.Config, log *slog.Logger, email string) error {

	if strings.TrimSpace(email) == "" {
		return errors.New("--bootstrap-email is required")
	}
	password := os.Getenv("FORGECRM_BOOTSTRAP_PASSWORD")
	if password == "" {
		return errors.New("FORGECRM_BOOTSTRAP_PASSWORD must be set; there is no default password")
	}
	if err := authn.ValidatePassword(password); err != nil {
		return fmt.Errorf("the bootstrap password is not acceptable: %w", err)
	}

	var existing int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM users WHERE is_platform_owner`).Scan(&existing); err != nil {
		return fmt.Errorf("check for an existing platform owner: %w", err)
	}
	if existing > 0 {
		log.Info("a platform owner already exists; nothing to do")
		return nil
	}

	hash, err := secrets.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash the bootstrap password: %w", err)
	}

	auditLog := audit.New(db)
	rbacSvc := rbac.NewService(db)
	email = strings.ToLower(strings.TrimSpace(email))

	return db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		var userID uuid.UUID
		const insUser = `
			INSERT INTO users (email, password_hash, display_name, status, is_platform_owner)
			VALUES ($1, $2, $3, 'active', true)
			RETURNING id`
		if err := db.Q(ctx).QueryRow(ctx, insUser, email, hash, "Platform owner").Scan(&userID); err != nil {
			return fmt.Errorf("create the bootstrap user: %w", err)
		}

		// Give the owner its own organization so the platform is usable at once.
		var orgID uuid.UUID
		const insOrg = `
			INSERT INTO organizations (name, slug) VALUES ('Default organization', 'default')
			ON CONFLICT (slug) DO UPDATE SET name = organizations.name
			RETURNING id`
		if err := db.Q(ctx).QueryRow(ctx, insOrg).Scan(&orgID); err != nil {
			return fmt.Errorf("create the default organization: %w", err)
		}
		const insMember = `
			INSERT INTO memberships (user_id, organization_id, is_org_owner)
			VALUES ($1, $2, true) ON CONFLICT (user_id, organization_id) DO UPDATE SET is_org_owner = true`
		if _, err := db.Q(ctx).Exec(ctx, insMember, userID, orgID); err != nil {
			return fmt.Errorf("add the owner to the organization: %w", err)
		}
		if _, err := rbacSvc.ClonePlatformRoles(ctx, orgID); err != nil {
			return fmt.Errorf("seed tenant roles: %w", err)
		}
		const grant = `
			INSERT INTO user_roles (user_id, role_id, organization_id)
			SELECT $1, r.id, $2 FROM roles r
			WHERE r.organization_id = $2 AND r.key = 'tenant_admin'
			ON CONFLICT (user_id, role_id) DO NOTHING`
		if _, err := db.Q(ctx).Exec(ctx, grant, userID, orgID); err != nil {
			return fmt.Errorf("grant the tenant administrator role: %w", err)
		}
		return auditLog.Record(ctx, audit.Entry{
			OrganizationID: &orgID, ActorUserID: &userID, ActorType: "system",
			Action: "platform.bootstrap_owner_created", EntityType: "user", EntityID: userID.String(),
		})
	})
}
