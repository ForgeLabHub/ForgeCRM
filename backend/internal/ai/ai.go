// Package ai implements the AI layer and the MCP tool surface.
//
// The layer is provider-agnostic: the CRM never depends on a specific vendor. A
// provider is selected by configuration, and with no provider configured the
// whole subsystem reports itself disabled while the rest of the platform works
// normally.
//
// Prompt-injection stance: text found in documents, emails, imported rows or
// external API responses is data. It can be summarised or quoted, but it can
// never change the caller's permission set, the tool allowlist, or the system
// instructions. Authorization is decided by the authenticated principal, never by
// content.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// Provider abstracts a model backend.
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Healthy(ctx context.Context) error
}

// Message is one turn of a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is a model invocation.
type CompletionRequest struct {
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	// Kind labels the record in ai_tasks, e.g. "chat" or an assistance kind. It
	// is what lets an operator see how much of the AI spend went on contextual
	// assistance rather than on the free-form chat endpoint.
	Kind string `json:"kind,omitempty"`
}

// CompletionResponse is a model reply with usage metadata.
type CompletionResponse struct {
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	Model        string `json:"model"`
}

// ErrDisabled is returned when no provider is configured.
var ErrDisabled = errors.New("the AI subsystem is not configured")

// ErrRateLimited is returned when the per-minute request cap is reached.
var ErrRateLimited = errors.New("AI request rate limit reached")

// ErrProviderUnavailable is returned when the configured provider could not be
// reached or did not answer in time. It is distinct from ErrDisabled, which says
// no provider is configured at all: here one is configured and the request may
// succeed if it is sent again, so it maps to a retryable 503 rather than to the
// generic 500 an unclassified failure would produce. A caller that saw a 500
// would reasonably assume a defect and stop retrying; a caller that sees this can
// wait and try again.
var ErrProviderUnavailable = errors.New("the AI provider is unavailable")

// Service is the AI layer.
type Service struct {
	cfg      *config.AIConfig
	db       *database.DB
	audit    *audit.Logger
	provider Provider
	// limiter bounds how many model calls an organization may make per minute. It
	// is the shared ratelimit.Limiter rather than a counter held here, because the
	// request path is concurrent: the type this replaced kept a slice of timestamps
	// and appended to it from every request goroutine with no lock, which is a data
	// race and, on a bad interleaving, a slice-write panic that takes the process
	// down. The shared limiter is also keyed by subject, so one organization
	// spending the whole budget cannot starve the others.
	limiter *ratelimit.Limiter
	// crm is the domain service the contextual path reads through. Every read
	// goes through it with the caller's principal, so the AI layer cannot see
	// more than the caller can.
	crm *crm.Service
	// log is optional; the contextual path logs a cache-write failure without
	// failing the request, and must tolerate a service built without a logger.
	log *slog.Logger
	// now is overridable in tests.
	now func() time.Time
}

// NewService builds the AI service. A disabled configuration yields a service
// whose methods report ErrDisabled.
//
// crmSvc is the domain service the contextual path reads through and may be nil,
// in which case only the free-form completion path is usable. It is a parameter
// rather than a field set afterwards so a caller cannot forget it and ship a
// contextual path that panics on first use.
//
// limiter bounds model calls per organization per minute. It is passed in rather
// than built here so the counters live in the same store as the rest of the
// platform's limits — shared across replicas when Redis is configured, and the
// same policy object the API already uses. A nil limiter means "do not limit",
// which is how a test that does not exercise the limit builds the service.
func NewService(cfg *config.AIConfig, db *database.DB, auditLog *audit.Logger, crmSvc *crm.Service, log *slog.Logger, limiter *ratelimit.Limiter) *Service {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Service{cfg: cfg, db: db, audit: auditLog, crm: crmSvc, log: log, now: time.Now, limiter: limiter}
	if cfg != nil && cfg.Enabled {
		switch cfg.Provider {
		case "ollama":
			s.provider = &OllamaProvider{baseURL: cfg.BaseURL, model: cfg.Model, timeout: cfg.Timeout}
		case "openai", "openai_compatible":
			s.provider = &OpenAICompatibleProvider{
				baseURL: cfg.BaseURL, apiKey: cfg.APIKey, model: cfg.Model, timeout: cfg.Timeout,
			}
		}
	}
	return s
}

// Enabled reports whether the AI subsystem is configured.
// Enabled reports whether a provider is configured.
//
// It tolerates a nil receiver so that AI being switched off is reported as "off"
// rather than panicking. The platform must work without AI.
func (s *Service) Enabled() bool { return s != nil && s.provider != nil }

// ProviderName returns the configured provider name, or "none".
func (s *Service) ProviderName() string {
	if s.provider == nil {
		return "none"
	}
	return s.provider.Name()
}

// ModelName returns the configured model, or "".
func (s *Service) ModelName() string {
	if s.provider == nil {
		return ""
	}
	return s.provider.Model()
}

// Status reports the subsystem's state for the admin page.
func (s *Service) Status(ctx context.Context) map[string]any {
	out := map[string]any{
		"enabled":  s.Enabled(),
		"provider": s.ProviderName(),
		"model":    s.ModelName(),
	}
	if !s.Enabled() {
		out["reason"] = "no AI provider is configured"
		return out
	}
	// A health probe is reported separately from configuration so an unreachable
	// provider is visible without disabling the subsystem in configuration.
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.provider.Healthy(probeCtx); err != nil {
		out["healthy"] = false
		out["error"] = err.Error()
	} else {
		out["healthy"] = true
	}
	return out
}

// Complete runs a completion, enforcing the rate cap and recording the operation.
//
// The prompt is summarised rather than stored in full, so the AI task log does
// not become a second copy of customer data.
func (s *Service) Complete(ctx context.Context, p principalLike, req CompletionRequest) (*CompletionResponse, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	if !s.allowRequest(ctx, p) {
		return nil, ErrRateLimited
	}
	timeout := s.cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	taskID := s.startTask(ctx, p, req)
	started := time.Now()
	resp, err := s.provider.Complete(callCtx, req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		s.finishTask(ctx, taskID, "failed", 0, 0, latency, err)
		return nil, fmt.Errorf("model call failed: %w", classifyProviderError(callCtx, err))
	}
	s.finishTask(ctx, taskID, "succeeded", resp.InputTokens, resp.OutputTokens, latency, nil)
	return resp, nil
}

// principalLike is the minimal shape the AI layer needs from an authenticated
// caller, which keeps this package free of a dependency on the HTTP layer.
type principalLike interface {
	GetUserID() uuid.UUID
	GetOrganizationID() uuid.UUID
}

func (s *Service) startTask(ctx context.Context, p principalLike, req CompletionRequest) uuid.UUID {
	id := uuid.New()
	summary := summarisePrompt(req)
	var (
		orgID  *uuid.UUID
		userID *uuid.UUID
	)
	if p != nil {
		o := p.GetOrganizationID()
		u := p.GetUserID()
		if o != uuid.Nil {
			orgID = &o
		}
		if u != uuid.Nil {
			userID = &u
		}
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "chat"
	}
	const q = `
		INSERT INTO ai_tasks (id, organization_id, user_id, provider, model, kind, prompt_summary,
			status, correlation_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'running',$8)`
	_, _ = s.db.Pool().Exec(ctx, q, id, orgID, userID, s.provider.Name(), s.provider.Model(),
		kind, summary, logging.CorrelationID(ctx))
	return id
}

// logger returns the service logger, never nil, so a caller can log without
// checking. A nil logger is a valid service: the AI layer is optional and may be
// built without one.
func (s *Service) logger() *slog.Logger {
	if s == nil || s.log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return s.log
}

func (s *Service) finishTask(ctx context.Context, id uuid.UUID, status string,
	inTokens, outTokens int, latencyMS int64, cause error) {
	var errText *string
	if cause != nil {
		t := cause.Error()
		if len(t) > 1000 {
			t = t[:1000]
		}
		errText = &t
	}
	const q = `
		UPDATE ai_tasks SET status=$2, input_tokens=$3, output_tokens=$4, latency_ms=$5,
			error=$6, finished_at=now() WHERE id=$1`
	_, _ = s.db.Pool().Exec(ctx, q, id, status, inTokens, outTokens, latencyMS, errText)
}

// summarisePrompt stores a short, non-sensitive description of the request.
func summarisePrompt(req CompletionRequest) string {
	if len(req.Messages) == 0 {
		return ""
	}
	last := req.Messages[len(req.Messages)-1].Content
	last = strings.TrimSpace(last)
	if len(last) > 200 {
		last = last[:200] + "..."
	}
	return last
}

// ListTasks returns recent AI task records.
func (s *Service) ListTasks(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]map[string]any, int64, error) {
	var total int64
	const countQ = `SELECT count(*) FROM ai_tasks WHERE organization_id = $1`
	if err := s.db.Pool().QueryRow(ctx, countQ, orgID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ai tasks: %w", err)
	}
	const q = `
		SELECT id, provider, model, kind, status, input_tokens, output_tokens, latency_ms,
		       error, created_at, finished_at
		FROM ai_tasks WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := s.db.Pool().Query(ctx, q, orgID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list ai tasks: %w", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id         uuid.UUID
			provider   string
			model      string
			kind       string
			status     string
			inTok      *int
			outTok     *int
			latency    *int
			errText    *string
			createdAt  time.Time
			finishedAt *time.Time
		)
		if err := rows.Scan(&id, &provider, &model, &kind, &status, &inTok, &outTok,
			&latency, &errText, &createdAt, &finishedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"id": id, "provider": provider, "model": model, "kind": kind, "status": status,
			"input_tokens": inTok, "output_tokens": outTok, "latency_ms": latency,
			"error": errText, "created_at": createdAt, "finished_at": finishedAt,
		})
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// OllamaProvider talks to a local Ollama server.
type OllamaProvider struct {
	baseURL string
	model   string
	timeout time.Duration
}

// Name implements Provider.
func (p *OllamaProvider) Name() string { return "ollama" }

// Model implements Provider.
func (p *OllamaProvider) Model() string { return p.model }

// Complete implements Provider.
func (p *OllamaProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	body := map[string]any{
		"model":    p.model,
		"stream":   false,
		"messages": append([]Message{{Role: "system", Content: req.System}}, req.Messages...),
	}
	if req.MaxTokens > 0 {
		body["options"] = map[string]any{"num_predict": req.MaxTokens}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(p.baseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: p.timeout}).Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	return &CompletionResponse{
		Content:      parsed.Message.Content,
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
		Model:        p.model,
	}, nil
}

// Healthy implements Provider.
func (p *OllamaProvider) Healthy(ctx context.Context) error {
	url := strings.TrimRight(p.baseURL, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// OpenAICompatibleProvider talks to any OpenAI-compatible chat completions API.
type OpenAICompatibleProvider struct {
	baseURL string
	apiKey  string
	model   string
	timeout time.Duration
}

// Name implements Provider.
func (p *OpenAICompatibleProvider) Name() string { return "openai_compatible" }

// Model implements Provider.
func (p *OpenAICompatibleProvider) Model() string { return p.model }

// Complete implements Provider.
func (p *OpenAICompatibleProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	msgs := make([]Message, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)

	body := map[string]any{"model": p.model, "messages": msgs}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := (&http.Client{Timeout: p.timeout}).Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("the provider returned HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("decode provider response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("the provider returned no choices")
	}
	return &CompletionResponse{
		Content:      parsed.Choices[0].Message.Content,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		Model:        p.model,
	}, nil
}

// Healthy implements Provider.
func (p *OpenAICompatibleProvider) Healthy(ctx context.Context) error {
	url := strings.TrimRight(p.baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("the provider returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// allowRequest consumes one unit of the caller's organization budget and reports
// whether the request may proceed. A nil limiter (a service built without one)
// allows every request.
//
// The subject is keyed by organization rather than held globally: a shared bucket
// lets one organization spend the whole minute's budget and starve every other
// tenant on the deployment. It is derived server-side from the authenticated
// principal, never from anything the caller sends, so a caller cannot pick a key
// to escape its own limit.
func (s *Service) allowRequest(ctx context.Context, p principalLike) bool {
	if s.limiter == nil {
		return true
	}
	subject := "anonymous"
	if p != nil && p.GetOrganizationID() != uuid.Nil {
		subject = p.GetOrganizationID().String()
	}
	return s.limiter.Allow(ctx, subject, s.now()).Allowed
}

// HTTPErrorFrom maps an AI error to the API error shape.
func HTTPErrorFrom(err error) error {
	switch {
	case errors.Is(err, ErrDisabled):
		return httpx.NewError(503, httpx.CodeDisabled, "the AI subsystem is not configured")
	case errors.Is(err, ErrRateLimited):
		return httpx.NewError(429, httpx.CodeRateLimited, "AI request rate limit reached")
	case errors.Is(err, ErrProviderUnavailable):
		return httpx.NewError(503, httpx.CodeUnavailable, "the AI provider is unavailable; try again shortly")
	default:
		return err
	}
}

// classifyProviderError turns a raw provider failure into ErrProviderUnavailable
// when the request never got an answer. A deadline or a cancelled context and a
// transport-level error both mean the provider could not be reached, which is a
// transient condition the caller can retry; anything the provider itself returned
// as an application error is passed through unchanged so it is not disguised as
// "try again".
func classifyProviderError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	return err
}
