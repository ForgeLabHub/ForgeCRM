package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

// Options configures the standalone bot service.
type Options struct {
	// PollTimeout is how long getUpdates holds the connection open. Long polling
	// is what makes the bot responsive without a public webhook.
	PollTimeout time.Duration
	// PollInterval is the pause between polls when Telegram returns no updates.
	PollInterval time.Duration
	// DeliverJobs makes this process send queued task notifications as well as
	// answer commands. When false the worker owns delivery.
	DeliverJobs bool
	// WorkerID labels this process's job claims and heartbeats.
	WorkerID string
	// Version and Commit are reported in heartbeats for fleet visibility.
	Version string
	Commit  string
}

// UpdateProcessor is the part of a Dispatcher the service uses. The service
// depends on the interface so a test can drive the poll loop with a fake, and so
// the transport is decoupled from how a reply is produced.
type UpdateProcessor interface {
	ProcessUpdate(ctx context.Context, update Incoming) error
	DeliverReply(ctx context.Context, updateID int64) error
}

// Service is the standalone Telegram bot: a long-poll loop, an optional delivery
// loop, and the periodic maintenance that keeps receipts and codes bounded.
type Service struct {
	bot        *BotAPI
	dispatcher UpdateProcessor
	db         *database.DB
	telegram   *notify.TelegramService
	outbox     *outbox.Store
	queue      *jobs.Queue
	log        *slog.Logger
	opts       Options
}

// NewService builds the service. It returns an error when the pieces the bot
// cannot run without are missing, so a misconfigured deployment fails at startup
// rather than silently doing nothing.
func NewService(bot *BotAPI, dispatcher UpdateProcessor, db *database.DB, tg *notify.TelegramService,
	outboxStore *outbox.Store, queue *jobs.Queue, log *slog.Logger, opts Options) (*Service, error) {
	if bot == nil || dispatcher == nil || db == nil || tg == nil || !tg.Enabled() {
		return nil, errors.New("telegram service requires an enabled bot and dispatcher")
	}
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = 30 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.WorkerID == "" {
		opts.WorkerID = "telegram"
	}
	return &Service{bot: bot, dispatcher: dispatcher, db: db, telegram: tg, outbox: outboxStore, queue: queue, log: log, opts: opts}, nil
}

// Run polls until the context is cancelled. It validates the token, clears any
// stale webhook (a bot cannot poll while one is set), publishes the command menu,
// then runs the loops concurrently.
func (s *Service) Run(ctx context.Context) error {
	me, err := s.bot.GetMe(ctx)
	if err != nil {
		return err
	}
	s.log.Info("telegram bot authenticated", "bot_id", me.ID, "username", me.Username)

	// A previously registered webhook blocks getUpdates. Clearing it makes the
	// poller self-sufficient; an operator who wants webhook mode registers it
	// again deliberately.
	if err := s.bot.DeleteWebhook(ctx, false); err != nil {
		s.log.Warn("could not clear a previously registered webhook", "error", err)
	}
	if err := s.bot.SetMyCommands(ctx, commandMenu()); err != nil {
		s.log.Warn("could not publish the command menu", "error", err)
	}

	var wg sync.WaitGroup
	loops := []func(context.Context){
		s.pollLoop,
		s.maintenanceLoop,
	}
	if s.opts.DeliverJobs {
		loops = append(loops, s.deliveryLoop, s.heartbeatLoop)
	}
	for _, loop := range loops {
		wg.Add(1)
		go func(fn func(context.Context)) {
			defer wg.Done()
			fn(ctx)
		}(loop)
	}
	wg.Wait()
	s.log.Info("telegram service stopped")
	return nil
}

// pollLoop is the long-poll loop. It advances the offset past every update it has
// seen, so Telegram does not resend them, and it backs off on a transient error
// rather than spinning.
func (s *Service) pollLoop(ctx context.Context) {
	var offset *int64
	for ctx.Err() == nil {
		updates, err := s.bot.GetUpdates(ctx, offset, s.opts.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) && !apiErr.Retryable() {
				// A permanent error (bad token, revoked) is logged once and the loop
				// stops: retrying cannot fix it.
				s.log.Error("telegram polling stopped on a permanent error", "status", apiErr.StatusCode)
				return
			}
			wait := s.opts.PollInterval
			if errors.As(err, &apiErr) && apiErr.RetryAfter() > 0 {
				wait = apiErr.RetryAfter()
			}
			s.log.Warn("telegram poll failed; backing off", "retry_in", wait.String())
			sleepCtx(ctx, wait)
			continue
		}
		for _, raw := range updates {
			update, ok := ParseIncoming(raw)
			if !ok {
				// A malformed update is still acknowledged by advancing past it.
				s.advance(&offset, raw)
				continue
			}
			if err := s.dispatcher.ProcessUpdate(ctx, update); err != nil {
				s.log.Warn("could not process a telegram update", "update_id", *update.UpdateID)
				continue
			}
			if err := s.dispatcher.DeliverReply(ctx, *update.UpdateID); err != nil {
				s.log.Warn("could not deliver a telegram reply", "update_id", *update.UpdateID)
			}
			offset = update.UpdateID
		}
		if len(updates) == 0 {
			sleepCtx(ctx, s.opts.PollInterval)
		}
	}
}

// advance moves the offset past an update whose id could not be parsed as a
// validated update, so a single bad payload cannot wedge the poller.
func (s *Service) advance(offset **int64, raw []byte) {
	var probe struct {
		UpdateID *int64 `json:"update_id"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.UpdateID != nil {
		*offset = probe.UpdateID
	}
}

// deliveryLoop sends queued task notifications. It is the same handler the worker
// uses; the queue's atomic claim means two processes never send the same job.
func (s *Service) deliveryLoop(ctx context.Context) {
	if s.queue == nil {
		return
	}
	handler := &DeliveryHandler{DB: s.db, Telegram: s.telegram}
	if n, err := s.queue.RequeueStale(ctx, 15*time.Minute); err != nil {
		s.log.Warn("could not requeue stale telegram jobs", "error", err)
	} else if n > 0 {
		s.log.Info("requeued stale jobs", "count", n)
	}
	// A separate job loop: the service claims only telegram notifications so it
	// does not compete with the worker for imports and exports.
	for ctx.Err() == nil {
		job, err := s.queue.ClaimKind(ctx, s.opts.WorkerID, jobs.KindTelegramTaskNotification)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("could not claim a telegram job", "error", err)
			sleepCtx(ctx, s.opts.PollInterval)
			continue
		}
		if job == nil {
			sleepCtx(ctx, s.opts.PollInterval)
			continue
		}
		if err := handler.Handle(ctx, s.queue, job); err != nil {
			s.log.Warn("telegram task delivery failed", "job_id", job.ID)
			if ferr := s.queue.Fail(ctx, job, err); ferr != nil {
				s.log.Error("could not record the job failure", "job_id", job.ID)
			}
			continue
		}
		if err := s.queue.Complete(ctx, job.ID, map[string]any{"status": "done"}); err != nil {
			s.log.Error("could not mark the job complete", "job_id", job.ID)
		}
	}
}

// heartbeatLoop writes the liveness beat the /metrics gauge reads, so a bot that
// exited or deadlocked is visible instead of silent.
func (s *Service) heartbeatLoop(ctx context.Context) {
	if s.queue == nil {
		return
	}
	s.queue.RunBeats(ctx, jobs.Heartbeat{
		WorkerID: s.opts.WorkerID,
		Role:     "telegram",
		Version:  s.opts.Version,
		Commit:   s.opts.Commit,
	}, jobs.HeartbeatInterval)
}

// maintenanceLoop runs the periodic cleanup and the overdue sweep. Both are
// idempotent, so running them here as well as in the worker is safe.
func (s *Service) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := Cleanup(ctx, s.db); err != nil && ctx.Err() == nil {
			s.log.Warn("could not clean expired telegram data")
		}
		if err := SweepOverdue(ctx, s.db, s.telegram); err != nil && ctx.Err() == nil {
			s.log.Warn("could not schedule overdue telegram tasks")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// commandMenu is the menu Telegram shows for the bot. It mirrors the commands the
// dispatcher answers, so the published list and the handled list stay aligned.
func commandMenu() []BotCommand {
	descriptions := map[string]string{
		"/start":         telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyHelp),
		"/help":          telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyHelp),
		"/menu":          telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyMenuTitle),
		"/tasks":         telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyOpenTasksHeader),
		"/today":         telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyTodayTasksHeader),
		"/overdue":       telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyOverdueHeader),
		"/deals":         telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyDealsHeader),
		"/pipeline":      telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyPipelineHeader),
		"/search":        telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeySearchUsage),
		"/link":          telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyLinkUsage),
		"/language":      telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyChooseLanguage),
		"/notifications": telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyNotificationsHdr),
	}
	order := []string{"/start", "/help", "/menu", "/tasks", "/today", "/overdue", "/deals", "/pipeline", "/search", "/link", "/language", "/notifications"}
	out := make([]BotCommand, 0, len(order))
	for _, cmd := range order {
		desc := descriptions[cmd]
		if len(desc) > 256 {
			desc = desc[:256]
		}
		out = append(out, BotCommand{Command: cmd, Description: desc})
	}
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
