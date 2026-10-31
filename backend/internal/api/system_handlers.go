package api

import (
	"context"
	"net/http"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
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
		// Reported, but the authorisation-code flow that would use it is not
		// implemented. Named for what it is so the interface cannot mistake a
		// setting for a working feature.
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

// handleMetrics exposes a small, dependency-free metrics surface.
//
// It is available only to a caller holding metrics.read, and optionally requires
// a shared token, so it can live in a protected context rather than being public.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if token := s.deps.Config.MetricsToken; token != "" {
		if r.Header.Get("X-Metrics-Token") != token {
			httpx.WriteError(w, r, httpx.ErrForbidden)
			return
		}
	}
	stats, err := s.deps.Jobs.Stats(r.Context(), nil)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	var (
		auditCount        int64
		securityCount     int64
		importCount       int64
		deliveryCount     int64
		deadDeliveries    int64
		deadLetteredEver  int64
		pendingDeliveries int64
		retentionRuns     int64
		retentionDeleted  int64
	)
	ctx := r.Context()
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&auditCount)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM security_events`).Scan(&securityCount)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM import_jobs`).Scan(&importCount)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&deliveryCount)
	// Dead-letter observability. Two distinct questions an operator asks:
	//
	//   webhook_deliveries_dead is how many deliveries are in the terminal dead
	//   state right now. It falls when a dead delivery is replayed successfully or
	//   swept by retention, so it answers "is anything broken at the moment". It
	//   carries no _total suffix because it is a gauge, not a counter: rate() over
	//   it would be meaningless, and the suffix would invite exactly that mistake.
	//
	//   webhook_deliveries_dead_lettered_total is how many transitions into the
	//   dead state have ever happened, read from the audit trail. It only rises, so
	//   it is the value to alert on and to compute a rate from. It is durable and
	//   shared across replicas because it is read from the database rather than a
	//   per-process counter, which is what makes it correct when several workers
	//   and API replicas run at once.
	//
	// Neither is labelled. A tenant id, a target URL or an event id in a metric
	// label would multiply the series without bound and leak a customer's endpoint
	// into a monitoring system; the per-organization view belongs in the audit
	// page, not in a counter.
	_ = s.deps.DB.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM webhook_deliveries WHERE status = 'dead'`).Scan(&deadDeliveries)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1`,
		string(audit.ActionWebhookDeadLettered)).Scan(&deadLetteredEver)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM webhook_deliveries WHERE status IN ('pending','failed','delivering')`).Scan(&pendingDeliveries)
	// Retention observability. The counters are read from the audit trail rather than
	// a per-process variable, so they survive a restart and are shared by every
	// replica. retention_runs_total only rises (one entry per sweep that removed
	// rows), which is what makes it usable as a counter; the deleted total answers
	// "how much has the policy actually removed".
	_ = s.deps.DB.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1`,
		string(audit.ActionRetentionSweep)).Scan(&retentionRuns)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx,
		`SELECT COALESCE(sum((metadata->>'deleted')::bigint), 0) FROM audit_log WHERE action = $1`,
		string(audit.ActionRetentionSweep)).Scan(&retentionDeleted)
	httpx.WriteJSON(w, map[string]any{
		"uptime_seconds":     int64(time.Since(s.deps.StartedAt).Seconds()),
		"jobs_by_status":     stats,
		"audit_entries":      auditCount,
		"security_events":    securityCount,
		"import_jobs":        importCount,
		"webhook_deliveries": deliveryCount,
		// The dead-letter counters, named as the operations runbook refers to them.
		// Both are unlabelled scalars by design.
		"webhook_deliveries_dead":                deadDeliveries,
		"webhook_deliveries_dead_lettered_total": deadLetteredEver,
		"webhook_deliveries_pending_total":       pendingDeliveries,
		"retention_runs_total":                   retentionRuns,
		"retention_deleted_total":                retentionDeleted,
		"subsystems": map[string]bool{
			"email":     s.deps.Email.Enabled(),
			"telegram":  s.deps.Telegram.Enabled(),
			"ai":        s.deps.AI.Enabled(),
			"retention": s.deps.Config.Retention.Enabled,
		},
	})
}
