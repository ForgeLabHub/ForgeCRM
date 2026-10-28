package api

import (
	"context"
	"net/http"
	"time"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/notify"
)

// ctxOrBackground returns the request context. It exists so handlers read
// naturally while still passing an explicit context to the database layer.
func ctxOrBackground(r *http.Request) context.Context { return r.Context() }

// notifyMessage builds an email message.
func notifyMessage(to, subject, htmlBody, template string) notify.Message {
	return notify.Message{
		To:       []string{to},
		Subject:  subject,
		HTMLBody: htmlBody,
		Template: template,
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
	// MCP reports whether a standard MCP client can connect. It can: the protocol
	// transport (JSON-RPC 2.0, initialize, tools/list, tools/call) is implemented
	// in internal/ai/mcp_protocol.go. It is separate from ToolAPI because the two
	// are different promises — tool_api is this application's own HTTP shape,
	// which no MCP client speaks.
	MCP bool `json:"mcp"`

	// OIDCConfigured reports whether OIDC settings are present. It is deliberately
	// not called "oidc": the sign-in flow is not implemented, so a client must not
	// offer SSO on the strength of this flag. See docs/ROADMAP.md.
	OIDCConfigured bool `json:"oidc_configured"`

	Imports          []string `json:"import_formats"`
	Archives         []string `json:"archive_formats"`
	MigrationSources []string `json:"migration_sources"`
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
		// A standard MCP client can connect: the JSON-RPC 2.0 transport, the
		// initialize handshake and tools/list and tools/call are implemented.
		MCP: true,
		// Reported, but the authorisation-code flow that would use it is not
		// implemented. Named for what it is so the interface cannot mistake a
		// setting for a working feature.
		OIDCConfigured: s.deps.Config.OIDC.Enabled,

		Imports:          []string{"csv", "tsv", "json", "jsonl", "xlsx", "xml"},
		Archives:         []string{"zip", "tar", "tar.gz", "tar.bz2", "tar.xz", "gzip", "bzip2", "xz"},
		MigrationSources: []string{"postgres", "sqlite", "rest_api"},
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
		auditCount    int64
		securityCount int64
		importCount   int64
		deliveryCount int64
	)
	ctx := r.Context()
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&auditCount)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM security_events`).Scan(&securityCount)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM import_jobs`).Scan(&importCount)
	_ = s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries`).Scan(&deliveryCount)

	httpx.WriteJSON(w, map[string]any{
		"uptime_seconds":     int64(time.Since(s.deps.StartedAt).Seconds()),
		"jobs_by_status":     stats,
		"audit_entries":      auditCount,
		"security_events":    securityCount,
		"import_jobs":        importCount,
		"webhook_deliveries": deliveryCount,
		"subsystems": map[string]bool{
			"email":    s.deps.Email.Enabled(),
			"telegram": s.deps.Telegram.Enabled(),
			"ai":       s.deps.AI.Enabled(),
		},
	})
}
