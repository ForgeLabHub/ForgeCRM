// Command forgecrm-worker runs background jobs: imports, exports, outbox
// dispatch, webhook delivery and maintenance.
//
// The worker is a separate binary so that a slow import cannot consume the API's
// request budget, and so the two can be scaled independently.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/secrets"
)

var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "forgecrm-worker: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting forgecrm-worker", "version", version, "concurrency", cfg.WorkerConcurrency)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	defer db.Close()

	keyring, err := secrets.NewKeyring(cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("initialise the encryption keyring: %w", err)
	}

	auditLog := audit.New(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	queue := jobs.NewQueue(db, log)
	outboxStore := outbox.NewStore(db)
	dispatcher := events.NewDispatcher(db, auditLog)
	emailSvc := notify.NewEmailService(&cfg.Email, db, log)
	telegramSvc := notify.NewTelegramService(&cfg.Telegram, db, log, auditLog, keyring)
	hub := integrations.NewHub(db, auditLog, keyring)

	imports, err := importx.NewService(db, cfg, auditLog, outboxStore, crmSvc, queue, cfg.ImportStorageDir)
	if err != nil {
		return fmt.Errorf("initialise the import service: %w", err)
	}

	registry := &handlerRegistry{
		db:         db,
		log:        log,
		imports:    imports,
		outbox:     outboxStore,
		dispatcher: dispatcher,
		email:      emailSvc,
		telegram:   telegramSvc,
		hub:        hub,
		crm:        crmSvc,
	}

	workerID := hostnameOrRandom()

	// The outbox drain runs alongside the job workers: it is what turns committed
	// domain events into webhook deliveries.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runOutboxLoop(ctx, log, outboxStore, dispatcher, cfg.WorkerPollEvery)
	}()

	// Stale job recovery: a worker that died mid-job would otherwise leave the row
	// running forever.
	wg.Add(1)
	go func() {
		defer wg.Done()
		runRequeueLoop(ctx, log, queue)
	}()

	concurrency := cfg.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			workerLoop(ctx, log, queue, registry, fmt.Sprintf("%s-%d", workerID, n), cfg.WorkerPollEvery)
		}(i)
	}

	<-ctx.Done()
	log.Info("shutdown signal received; waiting for in-flight jobs")
	wg.Wait()
	log.Info("worker shutdown complete")
	return nil
}

// handlerRegistry maps a job kind to its handler.
type handlerRegistry struct {
	db         *database.DB
	log        *slog.Logger
	imports    *importx.Service
	outbox     *outbox.Store
	dispatcher *events.Dispatcher
	email      *notify.EmailService
	telegram   *notify.TelegramService
	hub        *integrations.Hub
	crm        *crm.Service
}

// handle dispatches one job. An unknown kind is a hard failure rather than a
// silent success, so a misconfigured queue cannot lose work quietly.
func (r *handlerRegistry) handle(ctx context.Context, queue *jobs.Queue, job *jobs.Job) error {
	switch job.Kind {
	case jobs.KindImportExecute:
		return r.imports.RunJob(ctx, queue, job)

	case jobs.KindImportDryRun:
		// A dry run is executed synchronously by the API, but the kind exists so a
		// queued dry run can be handled here too.
		return r.imports.RunJob(ctx, queue, job)

	case jobs.KindOutboxDrain:
		return r.drainOutbox(ctx)

	case jobs.KindWebhookDeliver:
		var payload struct {
			DeliveryID uuid.UUID `json:"delivery_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode webhook delivery payload: %w", err)
		}
		return r.deliverWebhook(ctx, payload.DeliveryID)

	case jobs.KindEmailSend:
		var payload struct {
			To       []string   `json:"to"`
			Subject  string     `json:"subject"`
			HTMLBody string     `json:"html_body"`
			TextBody string     `json:"text_body"`
			Template string     `json:"template"`
			OrgID    *uuid.UUID `json:"organization_id"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode email payload: %w", err)
		}
		if !r.email.Enabled() {
			// A queued email with email disabled is dropped with a log line, not an
			// error: the platform is expected to run without SMTP.
			r.log.Info("dropping queued email because SMTP is not configured", "job_id", job.ID)
			return nil
		}
		return r.email.Send(ctx, notify.Message{
			To: payload.To, Subject: payload.Subject, HTMLBody: payload.HTMLBody,
			TextBody: payload.TextBody, Template: payload.Template, OrgID: payload.OrgID,
		})

	case jobs.KindRetentionSweep:
		return r.retentionSweep(ctx)

	default:
		return fmt.Errorf("no handler is registered for job kind %q", job.Kind)
	}
}

// deliverWebhook sends one pending webhook delivery.
func (r *handlerRegistry) deliverWebhook(ctx context.Context, deliveryID uuid.UUID) error {
	const sel = `
		SELECT d.id, d.subscription_id, d.payload, s.target_url, s.secret_encrypted, s.max_attempts, d.attempts
		FROM webhook_deliveries d
		JOIN webhook_subscriptions s ON s.id = d.subscription_id
		WHERE d.id = $1`
	var (
		id          uuid.UUID
		subID       uuid.UUID
		payload     []byte
		targetURL   string
		secretEnc   []byte
		maxAttempts int
		attempts    int
	)
	if err := r.db.Pool().QueryRow(ctx, sel, deliveryID).Scan(&id, &subID, &payload,
		&targetURL, &secretEnc, &maxAttempts, &attempts); err != nil {
		return fmt.Errorf("load webhook delivery: %w", err)
	}

	keyring, err := secrets.NewKeyring(platformSecretFromEnv())
	if err != nil {
		return err
	}
	secret, err := keyring.Decrypt(secretEnc)
	if err != nil {
		return fmt.Errorf("decrypt webhook secret: %w", err)
	}

	timestamp := fmt.Sprint(time.Now().Unix())
	signature := keyring.WebhookSign(secret, timestamp, payload)

	headers := map[string]string{
		"X-Signature-256":       "sha256=" + signature,
		"X-Signature-Timestamp": timestamp,
		"X-ForgeCRM-Delivery":   id.String(),
	}
	// The guard refuses an internal or metadata destination on every hop.
	client := integrations.NewGuardedClient(20 * time.Second)
	status, body, err := client.PostJSON(ctx, targetURL, payload, headers)

	attempts++
	if err != nil {
		// Exponential backoff, capped, so a persistent failure does not hammer a
		// failing endpoint.
		backoff := time.Duration(1<<uint(min(attempts, 8))) * time.Second
		if attempts >= maxAttempts {
			_, _ = r.db.Pool().Exec(ctx, `
				UPDATE webhook_deliveries
				SET status='failed', attempts=$2, last_error=$3
				WHERE id=$1`, id, attempts, truncate(err.Error(), 1000))
			return nil
		}
		_, _ = r.db.Pool().Exec(ctx, `
			UPDATE webhook_deliveries
			SET status='pending', attempts=$2, last_error=$3, next_attempt_at=now() + $4::interval
			WHERE id=$1`, id, attempts, truncate(err.Error(), 1000), fmt.Sprintf("%d seconds", int(backoff.Seconds())))
		return nil
	}

	if status >= 400 {
		msg := fmt.Sprintf("the endpoint returned HTTP %d", status)
		if attempts >= maxAttempts {
			_, _ = r.db.Pool().Exec(ctx, `
				UPDATE webhook_deliveries SET status='failed', attempts=$2, last_error=$3 WHERE id=$1`,
				id, attempts, msg)
			return nil
		}
		backoff := time.Duration(1<<uint(min(attempts, 8))) * time.Second
		_, _ = r.db.Pool().Exec(ctx, `
			UPDATE webhook_deliveries
			SET status='pending', attempts=$2, last_error=$3, next_attempt_at=now() + $4::interval
			WHERE id=$1`, id, attempts, msg, fmt.Sprintf("%d seconds", int(backoff.Seconds())))
		return nil
	}

	_ = body
	_, _ = r.db.Pool().Exec(ctx, `
		UPDATE webhook_deliveries SET status='delivered', attempts=$2, delivered_at=now(), response_status=$3, last_error=NULL
		WHERE id=$1`, id, attempts, status)
	return nil
}

// drainOutbox publishes pending events and creates webhook deliveries.
func (r *handlerRegistry) drainOutbox(ctx context.Context) error {
	const batch = 100
	events, err := r.outbox.FetchPending(ctx, batch)
	if err != nil {
		return err
	}
	for _, ev := range events {
		// Create one delivery per matching subscription, then mark the outbox
		// entry published. Delivery is at-least-once, so a crash between the two
		// steps produces a retry rather than a lost event.
		if _, err := r.dispatcher.Dispatch(ctx, ev); err != nil {
			_ = r.outbox.MarkFailed(ctx, ev.ID, err)
			continue
		}
		if err := r.outbox.MarkPublished(ctx, ev.ID); err != nil {
			return err
		}
	}
	return nil
}

// retentionSweep prunes records past their retention window.
func (r *handlerRegistry) retentionSweep(ctx context.Context) error {
	// Failed deliveries and finished jobs are pruned; audit and security records
	// are never deleted by an automatic sweep.
	stmts := []string{
		`DELETE FROM webhook_deliveries WHERE status = 'delivered' AND delivered_at < now() - interval '30 days'`,
		`DELETE FROM jobs WHERE status IN ('succeeded','cancelled') AND finished_at < now() - interval '30 days'`,
		`DELETE FROM ai_tasks WHERE finished_at < now() - interval '90 days'`,
		`DELETE FROM app_logs WHERE created_at < now() - interval '14 days'`,
	}
	for _, s := range stmts {
		if _, err := r.db.Pool().Exec(ctx, s); err != nil {
			r.log.Warn("retention sweep statement failed", "error", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Loops
// ---------------------------------------------------------------------------

func workerLoop(ctx context.Context, log *slog.Logger, queue *jobs.Queue,
	registry *handlerRegistry, workerID string, poll time.Duration) {

	if poll <= 0 {
		poll = 2 * time.Second
	}
	log.Info("worker started", "worker_id", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job, err := queue.Claim(ctx, workerID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Warn("could not claim a job", "error", err)
			sleepCtx(ctx, poll)
			continue
		}
		if job == nil {
			sleepCtx(ctx, poll)
			continue
		}

		jobCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		err = registry.handle(jobCtx, queue, job)
		cancel()

		if err != nil {
			log.Error("job failed", "job_id", job.ID, "kind", job.Kind, "error", err)
			if ferr := queue.Fail(ctx, job, err); ferr != nil {
				log.Error("could not record the job failure", "job_id", job.ID, "error", ferr)
			}
			continue
		}
		if err := queue.Complete(ctx, job.ID, map[string]any{"status": "done"}); err != nil {
			log.Error("could not mark the job complete", "job_id", job.ID, "error", err)
		}
	}
}

func runOutboxLoop(ctx context.Context, log *slog.Logger, store *outbox.Store,
	dispatcher *events.Dispatcher, poll time.Duration) {

	if poll <= 0 {
		poll = 2 * time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := store.FetchPending(ctx, 100)
			if err != nil {
				log.Warn("could not read the outbox", "error", err)
				continue
			}
			for _, ev := range events {
				if _, err := dispatcher.Dispatch(ctx, ev); err != nil {
					log.Warn("could not dispatch an event", "event_id", ev.ID, "error", err)
					_ = store.MarkFailed(ctx, ev.ID, err)
					continue
				}
				if err := store.MarkPublished(ctx, ev.ID); err != nil {
					log.Warn("could not mark an event published", "event_id", ev.ID, "error", err)
				}
			}
		}
	}
}

func runRequeueLoop(ctx context.Context, log *slog.Logger, queue *jobs.Queue) {
	// A job whose worker died is returned to the queue after this window.
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := queue.RequeueStale(ctx, 15*time.Minute)
			if err != nil {
				log.Warn("could not requeue stale jobs", "error", err)
				continue
			}
			if n > 0 {
				log.Info("requeued stale jobs", "count", n)
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func hostnameOrRandom() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return uuid.NewString()[:8]
}

func platformSecretFromEnv() string { return os.Getenv("FORGECRM_SECRET_KEY") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = ai.ErrDisabled
var _ = strings.TrimSpace
