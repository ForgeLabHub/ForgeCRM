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

	"github.com/geff1778/forgecrm/internal/agent"
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
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/retention"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/workflow"
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

	db, err := database.ConnectWithPool(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
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

	retentionRunner := retention.New(db, auditLog, log, retention.Options{
		Enabled:    cfg.Retention.Enabled,
		Delete:     cfg.Retention.Enabled && !cfg.Retention.DryRun,
		AllowAudit: cfg.Retention.AllowAudit,
		Windows:    cfg.Retention.Windows,
	})
	registry := &handlerRegistry{
		db:         db,
		log:        log,
		imports:    imports,
		outbox:     outboxStore,
		dispatcher: dispatcher,
		sender:     events.NewDeliverySender(db, auditLog, keyring),
		email:      emailSvc,
		telegram:   telegramSvc,
		hub:        hub,
		crm:        crmSvc,
		retention:  retentionRunner,
	}
	if cfg.Retention.Enabled {
		mode := "delete"
		if cfg.Retention.DryRun {
			mode = "dry-run (nothing will be removed)"
		}
		log.Info("retention sweeps enabled", "mode", mode, "allow_audit", cfg.Retention.AllowAudit)
	} else {
		log.Info("retention sweeps are disabled; no records will be pruned")
	}

	// The agent runtime needs an executor to run approved actions. The worker builds
	// its own tool registry and executor rather than sharing the API's, because the
	// worker's job is the sweep; the executor is only consulted if an approved action
	// is executed from this process, which the API normally does.
	toolRegistry := ai.NewToolRegistry(crmSvc, auditLog)
	agentSvc, err := agent.NewService(db, auditLog, ai.NewAgentExecutor(db, rbac.NewService(db), toolRegistry),
		agent.Config{PlatformSecret: cfg.SecretKey, DeniedTools: ai.DeniedToolNames()})
	if err != nil {
		return fmt.Errorf("initialise the agent service: %w", err)
	}
	registry.agent = agentSvc

	// The workflow automation engine runs committed events through the
	// organization's event-triggered workflows. It is built from the same
	// crm.Service the API uses, so a workflow action is authorized exactly like the
	// equivalent API call.
	wfEngine := workflow.NewEngine(workflow.Config{
		DB:     db,
		CRM:    crmSvc,
		RBAC:   rbac.NewService(db),
		Audit:  auditLog,
		Outbox: outboxStore,
		Log:    log,
	})
	registry.workflows = wfEngine

	workerID := hostnameOrRandom()

	// Two contexts, because "stop taking work" and "abandon the work in hand" are
	// different moments. The pollers and the claim step watch loopCtx and stop the
	// instant the signal arrives; a job that has already been claimed keeps running
	// on jobCtx, which is cancelled only after the shutdown grace period so the
	// process cannot be held open indefinitely by one slow job.
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	jobCtx, jobCancel := context.WithCancel(context.WithoutCancel(ctx))
	defer jobCancel()
	grace := cfg.WorkerShutdownGrace
	if grace <= 0 {
		grace = 30 * time.Second
	}

	var loops sync.WaitGroup
	var workers sync.WaitGroup

	// The outbox drain runs alongside the job workers: it is what turns committed
	// domain events into webhook deliveries.
	loops.Add(1)
	go func() {
		defer loops.Done()
		runOutboxLoop(loopCtx, log, outboxStore, dispatcher, wfEngine, workerID+"-outbox", cfg.WorkerPollEvery)
	}()

	// Stale job recovery: a worker that died mid-job would otherwise leave the row
	// running forever.
	loops.Add(1)
	go func() {
		defer loops.Done()
		runRequeueLoop(loopCtx, log, queue)
	}()

	// Scheduled maintenance: the retention sweep handler existed but nothing ever
	// enqueued it, so finished jobs, delivered webhooks and application logs grew
	// without bound. This loop is what makes the retention policy real.
	loops.Add(1)
	go func() {
		defer loops.Done()
		runMaintenanceLoop(loopCtx, log, queue)
	}()

	// Webhook delivery: the loop that actually sends the rows the dispatcher
	// creates. Without it a subscription was registered, matching events were
	// recorded as deliveries, and the endpoint was never called.
	loops.Add(1)
	go func() {
		defer loops.Done()
		runDeliveryLoop(loopCtx, log, registry.sender, cfg.WorkerPollEvery)
	}()

	// Workflow scheduler: fires due schedule-triggered workflows and resumes
	// paused runs whose delay has elapsed. Both use claim-based selection, so
	// several worker replicas can run this without double-firing.
	loops.Add(1)
	go func() {
		defer loops.Done()
		runWorkflowSchedulerLoop(loopCtx, log, wfEngine, cfg.WorkerPollEvery)
	}()

	concurrency := cfg.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			workerLoop(loopCtx, jobCtx, log, queue, registry.handle, fmt.Sprintf("%s-%d", workerID, n), cfg.WorkerPollEvery)
		}(i)
	}

	<-ctx.Done()

	// Stop the long-running loops, but let an in-flight job finish: a job that has
	// already been claimed is not safe to abort mid-write, and its row is 'running'
	// so nothing else will pick it up. The loops watch loopCtx; the job goroutines
	// watch jobCtx, cancelled only once the grace period has elapsed.
	log.Info("shutdown signal received; waiting for in-flight jobs", "grace", grace)
	loopCancel()
	loops.Wait()
	drained := make(chan struct{})
	go func() {
		workers.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		log.Info("worker shutdown complete; all in-flight jobs finished")
	case <-time.After(grace):
		log.Warn("shutdown grace elapsed; abandoning in-flight jobs", "grace", grace)
		jobCancel()
		<-drained
	}
	return nil
}

// handlerRegistry maps a job kind to its handler.
type handlerRegistry struct {
	db         *database.DB
	log        *slog.Logger
	imports    *importx.Service
	outbox     *outbox.Store
	dispatcher *events.Dispatcher
	sender     *events.DeliverySender
	email      *notify.EmailService
	telegram   *notify.TelegramService
	hub        *integrations.Hub
	crm        *crm.Service
	agent      *agent.Service
	workflows  *workflow.Engine
	retention  *retention.Runner
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
		return r.sender.SendDelivery(ctx, payload.DeliveryID)

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

	case jobs.KindAgentSweep:
		return r.agentSweep(ctx)

	default:
		return fmt.Errorf("no handler is registered for job kind %q", job.Kind)
	}
}

// drainOutbox publishes pending events and creates webhook deliveries.
func (r *handlerRegistry) drainOutbox(ctx context.Context) error {
	const batch = 100
	// Claim rather than read: a queued drain runs in a worker that may be racing
	// the drain loop, and reading without claiming let both process the same event.
	events, err := r.outbox.ClaimPending(ctx, "drain-job", batch)
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

// agentSweep expires approval requests past their deadline and fails the runs that
// were waiting on them. Without it, a proposal nobody decided would hold a run in
// waiting_approval indefinitely.
func (r *handlerRegistry) agentSweep(ctx context.Context) error {
	if r.agent == nil {
		return nil
	}
	n, err := r.agent.SweepExpired(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		r.log.Info("expired undecided agent approvals", "count", n)
	}
	return nil
}

// retentionSweep prunes records past their retention window. The policy lives in
// internal/retention, which owns the per-table window, the dry-run posture and the
// protected-table list; this handler only invokes it. A disabled runner is a no-op
// and says so, so the scheduled job is harmless on a deployment that has not
// opted in.
func (r *handlerRegistry) retentionSweep(ctx context.Context) error {
	if r.retention == nil || !r.retention.Enabled() {
		return nil
	}

	results, err := r.retention.Run(ctx)
	if err != nil {
		return err
	}

	var deleted int64
	for _, res := range results {
		deleted += res.Deleted
		switch {
		case res.Error != "":
			r.log.Warn("retention policy failed", "policy", res.Key, "error", res.Error)
		case res.Skipped != "":
			r.log.Debug("retention policy skipped", "policy", res.Key, "reason", res.Skipped)
		default:
			r.log.Info("retention policy applied",
				"policy", res.Key, "candidates", res.Candidates,
				"deleted", res.Deleted, "batches", res.Batches)
		}
	}

	mode := "swept"
	if r.retention.DryRun() {
		mode = "previewed"
	}
	r.log.Info("retention run complete", "mode", mode, "deleted", deleted)
	return nil
}

// ---------------------------------------------------------------------------
// Loops
// ---------------------------------------------------------------------------

// jobHandler runs one claimed job. It is a function rather than a method on the
// registry so a test can drive workerLoop with its own handler and a real queue.
type jobHandler func(ctx context.Context, queue *jobs.Queue, job *jobs.Job) error

// workerLoop claims and runs jobs until loopCtx is cancelled. Job execution and
// the bookkeeping that records its outcome run on jobCtx, which outlives the
// shutdown signal for the grace period: a claimed job must reach a terminal state
// (succeeded or failed) rather than being killed mid-write and left 'running'.
func workerLoop(loopCtx, jobCtx context.Context, log *slog.Logger, queue *jobs.Queue,
	handle jobHandler, workerID string, poll time.Duration) {

	if poll <= 0 {
		poll = 2 * time.Second
	}
	log.Info("worker started", "worker_id", workerID)
	for {
		select {
		case <-loopCtx.Done():
			return
		default:
		}

		job, err := queue.Claim(loopCtx, workerID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Warn("could not claim a job", "error", err)
			sleepCtx(loopCtx, poll)
			continue
		}
		if job == nil {
			sleepCtx(loopCtx, poll)
			continue
		}

		// A running job still stops when the whole process is going down: jobCtx is
		// cancelled once the grace period has elapsed, and the 30-minute cap is the
		// ordinary bound on a single job.
		runCtx, cancel := context.WithTimeout(jobCtx, 30*time.Minute)
		err = handle(runCtx, queue, job)
		cancel()

		// The outcome is recorded on jobCtx, not runCtx: a job whose run context was
		// just cancelled would otherwise fail to mark itself failed, leaving the row
		// 'running' until the requeue sweep notices it.
		if err != nil {
			log.Error("job failed", "job_id", job.ID, "kind", job.Kind, "error", err)
			if ferr := queue.Fail(jobCtx, job, err); ferr != nil {
				log.Error("could not record the job failure", "job_id", job.ID, "error", ferr)
			}
			continue
		}
		if err := queue.Complete(jobCtx, job.ID, map[string]any{"status": "done"}); err != nil {
			log.Error("could not mark the job complete", "job_id", job.ID, "error", err)
		}
	}
}

func runOutboxLoop(ctx context.Context, log *slog.Logger, store *outbox.Store,
	dispatcher *events.Dispatcher, wf *workflow.Engine, workerID string, poll time.Duration) {

	if poll <= 0 {
		poll = 2 * time.Second
	}

	// An event claimed by a worker that died is otherwise stuck: the claim has no
	// owner to release it. Returning them at start makes an unclean shutdown cost a
	// redelivery instead of a lost event.
	if n, err := store.ReleaseStale(ctx, 10*time.Minute); err != nil {
		log.Warn("could not release stale outbox claims", "error", err)
	} else if n > 0 {
		log.Info("released stale outbox claims", "count", n)
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Claim, do not read: reading without claiming is what let two drains
			// dispatch the same event and deliver it twice.
			events, err := store.ClaimPending(ctx, workerID, 100)
			if err != nil {
				log.Warn("could not claim outbox events", "error", err)
				continue
			}
			for _, ev := range events {
				if _, err := dispatcher.Dispatch(ctx, ev); err != nil {
					log.Warn("could not dispatch an event", "event_id", ev.ID, "error", err)
					_ = store.MarkFailed(ctx, ev.ID, err)
					continue
				}
				// Run event-triggered workflows. De-duplication inside the engine makes a
				// redelivery of this event safe.
				if wf != nil {
					wf.HandleEvent(ctx, ev)
				}
				if err := store.MarkPublished(ctx, ev.ID); err != nil {
					log.Warn("could not mark an event published", "event_id", ev.ID, "error", err)
				}
			}
		}
	}
}

// runWorkflowSchedulerLoop fires due schedule-triggered workflows and resumes
// paused runs. Both selections claim their row before acting, so a second replica
// running the same loop skips what the first took rather than double-firing.
func runWorkflowSchedulerLoop(ctx context.Context, log *slog.Logger, engine *workflow.Engine, poll time.Duration) {
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
			due, err := engine.Store().DueScheduledWorkflows(ctx, 50)
			if err != nil {
				log.Warn("could not claim due workflows", "error", err)
			}
			for i := range due {
				if _, err := engine.RunScheduled(ctx, due[i]); err != nil {
					log.Warn("scheduled workflow run failed to start", "workflow_id", due[i].ID, "error", err)
				}
			}

			resumed, err := engine.ResumeDue(ctx, 50)
			if err != nil {
				log.Warn("could not resume paused workflow runs", "error", err)
			} else if resumed > 0 {
				log.Info("resumed paused workflow runs", "count", resumed)
			}
		}
	}
}

// runDeliveryLoop sends pending webhook deliveries.
//
// This is the consumer the outgoing webhook subsystem was missing: the dispatcher
// only ever created delivery rows, so a subscription was registered, matching
// events were recorded, and the target endpoint was never called. Claiming rather
// than reading is what allows more than one worker to run this without sending the
// same delivery twice.
func runDeliveryLoop(ctx context.Context, log *slog.Logger, sender *events.DeliverySender, poll time.Duration) {
	if poll <= 0 {
		poll = 2 * time.Second
	}

	if n, err := sender.ReleaseStaleDeliveries(ctx, 5*time.Minute); err != nil {
		log.Warn("could not release stale webhook deliveries", "error", err)
	} else if n > 0 {
		log.Info("released stale webhook deliveries", "count", n)
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				delivery, err := sender.ClaimDelivery(ctx, "delivery-loop", 2*time.Minute)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					log.Warn("could not claim a webhook delivery", "error", err)
					break
				}
				if delivery == nil {
					break
				}
				if err := sender.SendDelivery(ctx, delivery.ID); err != nil {
					log.Warn("could not send a webhook delivery", "delivery_id", delivery.ID, "error", err)
				}
			}
		}
	}
}

// runMaintenanceLoop enqueues the periodic retention sweep.
//
// It enqueues rather than sweeping inline so the sweep runs on the same job
// machinery as everything else: it is retried on failure, visible in the job log
// and bounded by the queue's own concurrency. EnqueuePeriodic is idempotent per
// kind, so a slow sweep is never doubled up by the next tick.
func runMaintenanceLoop(ctx context.Context, log *slog.Logger, queue *jobs.Queue) {
	const every = time.Hour
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := queue.EnqueuePeriodic(ctx, jobs.KindRetentionSweep, map[string]any{})
			if err != nil {
				log.Warn("could not enqueue the retention sweep", "error", err)
			} else if job != nil {
				log.Info("enqueued the retention sweep", "job_id", job.ID)
			}
			// The same tick also expires undecided agent approvals, so a proposal
			// nobody decided does not hold its run open forever.
			sweep, err := queue.EnqueuePeriodic(ctx, jobs.KindAgentSweep, map[string]any{})
			if err != nil {
				log.Warn("could not enqueue the agent approval sweep", "error", err)
			} else if sweep != nil {
				log.Info("enqueued the agent approval sweep", "job_id", sweep.ID)
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
