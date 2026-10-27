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

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/api"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
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

	db, err := database.Connect(ctx, cfg.DatabaseURL)
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

	services, err := buildServices(cfg, db, keyring, log)
	if err != nil {
		return err
	}

	server := api.New(&api.Deps{
		Config:       cfg,
		Log:          log,
		DB:           db,
		Keyring:      keyring,
		Audit:        services.audit,
		Auth:         services.auth,
		RBAC:         services.rbac,
		CRM:          services.crm,
		Jobs:         services.jobs,
		Outbox:       services.outbox,
		Imports:      services.imports,
		Migrations:   services.migrations,
		Integrations: services.integrations,
		Events:       services.events,
		Dispatcher:   services.dispatcher,
		Email:        services.email,
		Telegram:     services.telegram,
		AI:           services.ai,
		Tools:        services.tools,
		StartedAt:    time.Now(),
		Version:      version,
		BuildCommit:  commit,
	})

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

// services bundles the constructed dependencies.
type services struct {
	audit        *audit.Logger
	auth         *authn.Service
	rbac         *rbac.Service
	crm          *crm.Service
	jobs         *jobs.Queue
	outbox       *outbox.Store
	imports      *importx.Service
	migrations   *migratex.Service
	integrations *integrations.Hub
	events       *events.Gateway
	dispatcher   *events.Dispatcher
	email        *notify.EmailService
	telegram     *notify.TelegramService
	ai           *ai.Service
	tools        *ai.ToolRegistry
}

func buildServices(cfg *config.Config, db *database.DB, keyring *secrets.Keyring,
	log *slog.Logger) (*services, error) {

	auditLog := audit.New(db)
	rbacSvc := rbac.NewService(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	queue := jobs.NewQueue(db, log)

	imports, err := importx.NewService(db, cfg, auditLog, outbox.NewStore(db), crmSvc, queue, cfg.ImportStorageDir)
	if err != nil {
		return nil, fmt.Errorf("initialise the import service: %w", err)
	}

	return &services{
		audit:        auditLog,
		auth:         authn.NewService(db, cfg, keyring, auditLog, rbacSvc),
		rbac:         rbacSvc,
		crm:          crmSvc,
		jobs:         queue,
		outbox:       outbox.NewStore(db),
		imports:      imports,
		migrations:   migratex.NewService(db, auditLog, keyring),
		integrations: integrations.NewHub(db, auditLog, keyring),
		events:       events.NewGateway(db, auditLog),
		dispatcher:   events.NewDispatcher(db, auditLog),
		email:        notify.NewEmailService(&cfg.Email, db, log),
		telegram:     notify.NewTelegramService(&cfg.Telegram, db, log, auditLog, keyring),
		ai:           ai.NewService(&cfg.AI, db, auditLog),
		tools:        ai.NewToolRegistry(crmSvc, auditLog),
	}, nil
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
