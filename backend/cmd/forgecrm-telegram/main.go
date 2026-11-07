// Command forgecrm-telegram runs the ForgeCRM Telegram bot as a standalone
// service.
//
// It is deliberately separate from the API and the worker. The bot reaches
// Telegram by long polling, so it needs no public webhook and no inbound port,
// and it can be deployed, scaled and restarted on its own. It reuses the same
// dispatcher, consent gate and CRM service the API uses, so a command handled
// here is authorised exactly like one handled through the webhook.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/telegram"
	"github.com/geff1778/forgecrm/internal/tenantconfig"
	"github.com/geff1778/forgecrm/internal/wiring"
)

var version = "0.1.0-dev"

var commit = "unknown"

func buildVersion() string {
	if version != "" && version != "0.1.0-dev" && version != "dev" {
		return version
	}
	if env := strings.TrimSpace(os.Getenv("FORGECRM_VERSION")); env != "" {
		return env
	}
	return version
}

func buildCommit() string {
	if commit != "" && commit != "unknown" && commit != "dev" {
		return commit
	}
	if env := strings.TrimSpace(firstNonEmpty(os.Getenv("FORGECRM_COMMIT"), os.Getenv("RENDER_GIT_COMMIT"))); env != "" {
		return env
	}
	return commit
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "forgecrm-telegram: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting forgecrm-telegram", "version", buildVersion(), "commit", buildCommit())

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	if !cfg.Telegram.Enabled || cfg.Telegram.BotToken == "" {
		return errors.New("telegram is not enabled: set FORGECRM_TELEGRAM_ENABLED=true and FORGECRM_TELEGRAM_BOT_TOKEN")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.ConnectWithPool(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	defer db.Close()

	// The service does not own migrations; the API does. It must not consume
	// updates before the schema it writes to exists, so the wait is bounded.
	schemaCtx, cancelSchema := context.WithTimeout(ctx, cfg.WorkerSchemaWait)
	defer cancelSchema()
	if err := migrate.WaitForSchema(schemaCtx, db.Pool(), cfg.WorkerPollEvery, log); err != nil {
		return fmt.Errorf("schema readiness check: %w", err)
	}
	log.Info("database schema readiness check passed")

	keyring, err := secrets.NewKeyring(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("initialise the encryption keyring: %w", err)
	}

	auditLog := audit.New(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	tenantConfigSvc := tenantconfig.NewService(db, auditLog)
	crmSvc.SetOrgDefaults(tenantConfigSvc)

	// The same consent gate the API and worker build, so a bot message is subject
	// to the same policy as a manual send.
	consentGate := wiring.NewConsentGate(consent.NewService(db, auditLog))
	tgSvc := notify.NewTelegramService(&cfg.Telegram, db, log, auditLog, keyring, consentGate)

	dispatcher := telegram.NewDispatcher(db, tgSvc, crmSvc, keyring, log)
	if dispatcher == nil {
		return errors.New("could not build the telegram dispatcher")
	}

	bot := telegram.NewBotAPI(cfg.Telegram.BotToken, cfg.Telegram.APIBase, &http.Client{Timeout: cfg.Telegram.PollTimeout + 30*time.Second})
	queue := jobs.NewQueue(db, log)
	svc, err := telegram.NewService(bot, dispatcher, db, tgSvc, outbox.NewStore(db), queue, log, telegram.Options{
		PollTimeout:  cfg.Telegram.PollTimeout,
		PollInterval: cfg.Telegram.PollInterval,
		DeliverJobs:  cfg.Telegram.DeliverInService,
		WorkerID:     hostnameOrRandom(),
		Version:      buildVersion(),
		Commit:       buildCommit(),
	})
	if err != nil {
		return fmt.Errorf("initialise the telegram service: %w", err)
	}

	// A dedicated health server so an orchestrator can restart the bot if it
	// wedges. It is optional: an empty address leaves the service with no port.
	if cfg.Telegram.HealthAddr != "" {
		startHealthServer(ctx, cfg.Telegram.HealthAddr, db, log)
	}

	return svc.Run(ctx)
}

// startHealthServer serves liveness and readiness for the bot. Readiness checks
// the database, because a bot that cannot reach the database cannot answer.
func startHealthServer(ctx context.Context, addr string, db *database.DB, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Pool().Ping(pingCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn("telegram health server stopped", "error", err)
		}
	}()
	log.Info("telegram health server listening", "addr", addr)
}

func hostnameOrRandom() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return "telegram-" + h
	}
	return "telegram-" + uuid.NewString()[:8]
}
