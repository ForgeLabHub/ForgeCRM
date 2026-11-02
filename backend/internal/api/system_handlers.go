package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/metrics"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// ctxOrBackground returns the request context. It exists so handlers read
// naturally while still passing an explicit context to the database layer.
func ctxOrBackground(r *http.Request) context.Context { return r.Context() }

// notifyMessage builds an email message for an account-level send: mail to the
// person themselves about their own account, with no organization in context. The
// purpose must not require a recorded grant, which is why these callers say
// transactional and the transport enforces that choice.
func notifyMessage(to, subject, htmlBody, template string) notify.Message {
	return notify.Message{
		To:       []string{to},
		Subject:  subject,
		HTMLBody: htmlBody,
		Template: template,
		Purpose:  notify.PurposeTransactional,
	}
}

// ---------------------------------------------------------------------------
// System
// ---------------------------------------------------------------------------

// systemInfoResponse describes the instance without revealing any secret.
type systemInfoResponse struct {
	Service       string    `json:"service"`
	Version       string    `json:"version"`
	Commit        string    `json:"commit,omitempty"`
	Environment   string    `json:"environment"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Database      string    `json:"database"`
	ServerTime    time.Time `json:"server_time"`
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	if err := s.deps.DB.Ping(r.Context()); err != nil {
		dbStatus = "unreachable"
	}
	httpx.WriteJSON(w, systemInfoResponse{
		Service:       "forgecrm",
		Version:       s.deps.Version,
		Commit:        s.deps.BuildCommit,
		Environment:   s.deps.Config.Env,
		StartedAt:     s.deps.StartedAt,
		UptimeSeconds: int64(time.Since(s.deps.StartedAt).Seconds()),
		Database:      dbStatus,
		ServerTime:    time.Now().UTC(),
	})
}

// capabilitiesResponse tells a client which optional subsystems are usable, so
// the UI can hide features that are not configured instead of showing broken
// screens.
//
// The flags answer "can this be used now", not "does configuration exist". A
// setting that is present but has no implementation behind it must report false,
// or the interface offers a feature that cannot work. Each field therefore names
// the condition it actually tests.
type capabilitiesResponse struct {
	Email    bool `json:"email"`
	Telegram bool `json:"telegram"`
	AI       bool `json:"ai"`
	MFA      bool `json:"mfa"`
	Webhooks bool `json:"webhooks"`
	ToolAPI  bool `json:"tool_api"`
	// Storage reports whether file attachments can be stored. It is true only when
	// an object store is actually configured: with no endpoint the document routes
	// answer 503, so a client that read a bare "documents are supported" would
	// offer an upload that cannot succeed. It tests the running service, not the
	// presence of settings.
	Storage bool `json:"storage"`
	// MaxUploadBytes is the configured single-object limit, so a client can refuse
	// an oversized file before spending the upload. Zero when storage is off.
	MaxUploadBytes int64 `json:"max_upload_bytes"`
	// MCP reports whether a standard MCP client can connect. It can: the protocol
	// transport (JSON-RPC 2.0, initialize, tools/list, tools/call) is implemented
	// in internal/ai/mcp_protocol.go. It is separate from ToolAPI because the two
	// are different promises — tool_api is this application's own HTTP shape,
	// which no MCP client speaks.
	MCP bool `json:"mcp"`

	// OIDCConfigured reports whether the deployment-level OIDC settings are
	// present. It is deliberately not called "oidc": settings alone do not mean a
	// sign-in can succeed, because a provider must also be configured and enabled.
	// A client that wants to offer single sign-on reads
	// GET /api/v1/auth/oidc/providers, which reports the providers a signed-out
	// visitor may actually choose from.
	OIDCConfigured bool `json:"oidc_configured"`

	Imports          []string `json:"import_formats"`
	Archives         []string `json:"archive_formats"`
	MigrationSources []string `json:"migration_sources"`

	// EventTypes is the canonical vocabulary a webhook subscription may select
	// from. A client must build the picker from this list rather than a local
	// copy: the interface once offered shortened names ("contact.created") while
	// the dispatcher matched "crm.contact.created", so every subscription created
	// from the interface matched no events and received nothing.
	EventTypes []string `json:"event_types"`
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, capabilitiesResponse{
		Email:    s.deps.Email.Enabled(),
		Telegram: s.deps.Telegram.Enabled(),
		AI:       s.deps.AI.Enabled(),
		// MFA is unconditional: TOTP enrolment and verification are implemented in
		// internal/authn and always available.
		MFA: true,
		// Webhook subscriptions and delivery are implemented in internal/events.
		Webhooks: true,
		// The tool API exposes tools/list and tools/call over HTTP in this
		// application's own shape. It is kept separate from the MCP flag below: this
		// is not the protocol, and a client that needs the protocol must read `mcp`.
		ToolAPI: true,
		// Storage is true only when an object store is configured. With no
		// endpoint the service reports itself off and the document routes answer
		// 503, so the interface must read this flag rather than assume the routes
		// work.
		Storage:        s.deps.Storage.Enabled(),
		MaxUploadBytes: s.deps.Storage.MaxObjectBytes(),
		// A standard MCP client can connect: the JSON-RPC 2.0 transport, the
		// initialize handshake and tools/list and tools/call are implemented.
		MCP: true,
		// Named for what it reports: the presence of deployment-level settings, not
		// that a sign-in can succeed. The authorisation-code flow itself lives in
		// internal/oidc and is reachable at /api/v1/auth/oidc/start; this flag stays
		// narrow because a provider must also be configured and enabled.
		OIDCConfigured: s.deps.Config.OIDC.Enabled,

		Imports:          []string{"csv", "tsv", "json", "jsonl", "xlsx", "xml"},
		Archives:         []string{"zip", "tar", "tar.gz", "tar.bz2", "tar.xz", "gzip", "bzip2", "xz"},
		MigrationSources: migratex.ImplementedSources(),
		EventTypes:       outbox.EventTypes(),
	})
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// metricsSnapshot is every value the metrics surfaces report.
//
// It is computed once and rendered twice — as JSON for the diagnostics page and
// as Prometheus text for a scraper — so the two cannot disagree about what the
// platform is doing. Adding a value means adding it here, and both surfaces gain
// it; there is no second list to forget.
type metricsSnapshot struct {
	UptimeSeconds int64
	JobsByStatus  map[string]int64
	Version       string
	Commit        string
	Subsystems    map[string]bool

	AuditEntries      int64
	SecurityEvents    int64
	ImportJobs        int64
	WebhookDeliveries int64
	// WebhookDeliveriesDead is a gauge: it falls when a dead delivery is replayed
	// or swept, so it answers "is anything broken right now".
	WebhookDeliveriesDead int64
	// WebhookDeliveriesDeadLetteredTotal is a counter read from the audit trail. It
	// only rises, so it is the value to alert on and to compute a rate from. It is
	// durable and shared across replicas because it is read from the database, not a
	// per-process variable.
	WebhookDeliveriesDeadLetteredTotal int64
	// WebhookDeliveriesPending is a gauge: deliveries still eligible for an attempt.
	// It carries no _total suffix for the same reason as the dead gauge.
	WebhookDeliveriesPending int64
	// RetentionRunsTotal and RetentionDeletedTotal are counters read from the audit
	// trail, so they survive a restart and are shared by every replica.
	RetentionRunsTotal    int64
	RetentionDeletedTotal int64
}

// collectMetrics reads the snapshot.
//
// Every read is best-effort except the job counts: an operator looking at a
// dashboard would rather see the numbers that were available than a blank page
// because one count failed. The failure is visible in the server log through the
// query layer, and the endpoint is not the place to report a partial outage of
// its own dependencies.
func (s *Server) collectMetrics(ctx context.Context) (metricsSnapshot, error) {
	stats, err := s.deps.Jobs.Stats(ctx, nil)
	if err != nil {
		return metricsSnapshot{}, err
	}

	snap := metricsSnapshot{
		UptimeSeconds: int64(time.Since(s.deps.StartedAt).Seconds()),
		JobsByStatus:  stats,
		Version:       s.deps.Version,
		Commit:        s.deps.BuildCommit,
		Subsystems: map[string]bool{
			"email":     s.deps.Email.Enabled(),
			"telegram":  s.deps.Telegram.Enabled(),
			"ai":        s.deps.AI.Enabled(),
			"retention": s.deps.Config.Retention.Enabled,
		},
	}

	q := s.deps.DB.Q(ctx)
	_ = q.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&snap.AuditEntries)
	_ = q.QueryRow(ctx, `SELECT count(*) FROM security_events`).Scan(&snap.SecurityEvents)
	_ = q.QueryRow(ctx, `SELECT count(*) FROM import_jobs`).Scan(&snap.ImportJobs)
	_ = q.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&snap.WebhookDeliveries)
	_ = q.QueryRow(ctx,
		`SELECT count(*) FROM webhook_deliveries WHERE status = 'dead'`).Scan(&snap.WebhookDeliveriesDead)
	_ = q.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1`,
		string(audit.ActionWebhookDeadLettered)).Scan(&snap.WebhookDeliveriesDeadLetteredTotal)
	_ = q.QueryRow(ctx,
		`SELECT count(*) FROM webhook_deliveries WHERE status IN ('pending','failed','delivering')`).
		Scan(&snap.WebhookDeliveriesPending)
	_ = q.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1`,
		string(audit.ActionRetentionSweep)).Scan(&snap.RetentionRunsTotal)
	_ = q.QueryRow(ctx,
		`SELECT COALESCE(sum((metadata->>'deleted')::bigint), 0) FROM audit_log WHERE action = $1`,
		string(audit.ActionRetentionSweep)).Scan(&snap.RetentionDeletedTotal)

	return snap, nil
}

// checkMetricsToken enforces the shared token on the JSON surface.
//
// It is optional there because the route already requires a session holding
// metrics.read; the token is a second lock for deployments that want one. When no
// token is configured the session permission is the whole gate.
func (s *Server) checkMetricsToken(w http.ResponseWriter, r *http.Request) bool {
	token := s.deps.Config.MetricsToken
	if token == "" {
		return true
	}
	if metricsTokenMatches(r, token) {
		return true
	}
	httpx.WriteError(w, r, httpx.ErrForbidden)
	return false
}

// metricsTokenMatches compares the presented token with the configured one.
//
// It is a secret comparison, so it is constant time: a byte-by-byte comparison
// would let the endpoint be used as an oracle to guess the token.
func metricsTokenMatches(r *http.Request, token string) bool {
	return secrets.ConstantTimeEqual([]byte(r.Header.Get("X-Metrics-Token")), []byte(token))
}

// handleMetrics exposes the JSON metrics surface used by the diagnostics page.
//
// It is available only to a caller holding metrics.read, and optionally requires
// a shared token.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.checkMetricsToken(w, r) {
		return
	}
	snap, err := s.collectMetrics(r.Context())
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, snap.asJSON())
}

// asJSON renders the snapshot in the shape the diagnostics page reads.
//
// The keys are the metric names, so a value the JSON page shows and a value a
// scraper reads are the same value under the same name.
func (m metricsSnapshot) asJSON() map[string]any {
	return map[string]any{
		"uptime_seconds":     m.UptimeSeconds,
		"jobs_by_status":     m.JobsByStatus,
		"audit_entries":      m.AuditEntries,
		"security_events":    m.SecurityEvents,
		"import_jobs":        m.ImportJobs,
		"webhook_deliveries": m.WebhookDeliveries,
		// The dead-letter counters, named as the operations runbook refers to them.
		// Both are unlabelled scalars by design.
		"webhook_deliveries_dead":                m.WebhookDeliveriesDead,
		"webhook_deliveries_dead_lettered_total": m.WebhookDeliveriesDeadLetteredTotal,
		"webhook_deliveries_pending":             m.WebhookDeliveriesPending,
		"retention_runs_total":                   m.RetentionRunsTotal,
		"retention_deleted_total":                m.RetentionDeletedTotal,
		"subsystems":                             m.Subsystems,
	}
}

// asSamples renders the snapshot as Prometheus samples.
//
// Naming follows the convention the runbook's alert depends on: a value that only
// rises ends in _total and is a counter; a value that can fall is a gauge and
// carries no suffix. The per-status job counts become one metric with a status
// label rather than one metric per status, which is what a query wants.
func (m metricsSnapshot) asSamples() []metrics.Sample {
	labels := func(k, v string) map[string]string { return map[string]string{k: v} }

	samples := []metrics.Sample{
		{Name: "uptime_seconds", Help: "Seconds since this instance started.", Kind: metrics.Gauge, Value: float64(m.UptimeSeconds)},
		{Name: "build_info", Help: "Build information for the running instance; the value is always 1.", Kind: metrics.Gauge,
			Labels: map[string]string{"version": m.Version, "commit": m.Commit}, Value: 1},
		{Name: "audit_entries", Help: "Rows in the audit trail.", Kind: metrics.Gauge, Value: float64(m.AuditEntries)},
		{Name: "security_events", Help: "Rows in the security event log.", Kind: metrics.Gauge, Value: float64(m.SecurityEvents)},
		{Name: "import_jobs", Help: "Import jobs recorded.", Kind: metrics.Gauge, Value: float64(m.ImportJobs)},
		{Name: "webhook_deliveries", Help: "Webhook deliveries recorded.", Kind: metrics.Gauge, Value: float64(m.WebhookDeliveries)},
		{Name: "webhook_deliveries_dead", Help: "Webhook deliveries in the terminal dead state right now.", Kind: metrics.Gauge, Value: float64(m.WebhookDeliveriesDead)},
		{Name: "webhook_deliveries_dead_lettered_total", Help: "Transitions into the dead-letter state, read from the audit trail.", Kind: metrics.Counter, Value: float64(m.WebhookDeliveriesDeadLetteredTotal)},
		{Name: "webhook_deliveries_pending", Help: "Webhook deliveries still eligible for an attempt.", Kind: metrics.Gauge, Value: float64(m.WebhookDeliveriesPending)},
		{Name: "retention_runs_total", Help: "Retention sweeps that removed rows, read from the audit trail.", Kind: metrics.Counter, Value: float64(m.RetentionRunsTotal)},
		{Name: "retention_deleted_total", Help: "Rows removed by retention, read from the audit trail.", Kind: metrics.Counter, Value: float64(m.RetentionDeletedTotal)},
	}
	for _, status := range sortedStatuses(m.JobsByStatus) {
		samples = append(samples, metrics.Sample{
			Name: "jobs", Help: "Background jobs by status.", Kind: metrics.Gauge,
			Labels: labels("status", status), Value: float64(m.JobsByStatus[status]),
		})
	}
	for _, name := range sortedSubsystems(m.Subsystems) {
		samples = append(samples, metrics.Sample{
			Name: "subsystem_enabled", Help: "Whether an optional subsystem is configured, 1 or 0.", Kind: metrics.Gauge,
			Labels: labels("subsystem", name), Value: boolValue(m.Subsystems[name]),
		})
	}
	return samples
}

// handleMetricsPrometheus serves the same snapshot in the Prometheus text
// exposition format.
//
// It is separate from the JSON endpoint because a scraper cannot present a
// session cookie, and because the two bodies have different content types. Because
// it is reachable without a session, it is closed unless a token is configured:
// with no token it answers 503 rather than exposing an unauthenticated view of
// platform internals on the public interface. That is a deliberate refusal, not a
// misconfiguration — the endpoint is opt-in.
func (s *Server) handleMetricsPrometheus(w http.ResponseWriter, r *http.Request) {
	token := s.deps.Config.MetricsToken
	if token == "" {
		httpx.WriteError(w, r, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeDisabled,
			"metrics scraping is not enabled; set FORGECRM_METRICS_TOKEN to enable it"))
		return
	}
	if !metricsTokenMatches(r, token) {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}

	snap, err := s.collectMetrics(r.Context())
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	// The body is a snapshot, not a document: a proxy or browser must not serve a
	// stale one to the next scrape.
	w.Header().Set("Cache-Control", "no-store")
	if err := metrics.Render(w, snap.asSamples()); err != nil {
		// The header is already written, so the status cannot change. The error is
		// logged instead of swallowed: it means a metric name or help text is wrong,
		// which is a programming error, not a runtime condition.
		s.deps.Log.Error("render prometheus metrics", "error", err)
	}
}

// sortedStatuses orders the job statuses so the rendered body is stable.
func sortedStatuses(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedSubsystems orders the subsystem names so the rendered body is stable.
func sortedSubsystems(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
