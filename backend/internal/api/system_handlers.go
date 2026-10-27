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

// capabilitiesResponse tells a client which optional subsystems are live, so the
// UI can hide features that are not configured instead of showing broken screens.
type capabilitiesResponse struct {
	Email            bool     `json:"email"`
	Telegram         bool     `json:"telegram"`
	AI               bool     `json:"ai"`
	MCP              bool     `json:"mcp"`
	OIDC             bool     `json:"oidc"`
	Imports          []string `json:"import_formats"`
	Archives         []string `json:"archive_formats"`
	MigrationSources []string `json:"migration_sources"`
	MFA              bool     `json:"mfa"`
	Webhooks         bool     `json:"webhooks"`
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, capabilitiesResponse{
		Email:            s.deps.Email.Enabled(),
		Telegram:         s.deps.Telegram.Enabled(),
		AI:               s.deps.AI.Enabled(),
		MCP:              true,
		OIDC:             s.deps.Config.OIDC.Enabled,
		Imports:          []string{"csv", "tsv", "json", "jsonl", "xlsx", "xml"},
		Archives:         []string{"zip", "tar", "tar.gz", "tar.bz2", "tar.xz", "gzip", "bzip2", "xz"},
		MigrationSources: []string{"postgres", "sqlite", "rest_api"},
		MFA:              true,
		Webhooks:         true,
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
