package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountAIStream registers the streaming variants of the AI endpoints.
//
// Streaming is a separate route rather than a flag on the buffered one so that a
// proxy, a load balancer or a client that does not understand server-sent events
// can keep using the buffered route unchanged. Both routes run the same service
// call, so the permission checks, the rate cap and the ai_tasks record are
// identical; only the transport differs.
func (s *Server) mountAIStream(r chi.Router) {
	r.Route("/ai/stream", func(r chi.Router) {
		r.With(s.perm(rbac.PermAIToolsExecute)).Post("/chat", s.handleAIStreamChat)
		r.With(s.perm(rbac.PermAIToolsExecute)).Post("/assist/{id}", s.handleAIStreamAssist)
	})
}

// sseWriter writes server-sent events.
//
// It flushes after every event: a stream that buffers defeats its own purpose, and
// the response writer's own buffering is invisible to this code. The header is set
// once, before the first write, so a handler that later discovers an error can
// still send it as an event — the status line is already committed by then.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	broken  bool
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	// A proxy that caches or buffers an event stream breaks it. These headers are
	// the conventional defence: no caching, and no buffering by an intermediary.
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	return &sseWriter{w: w, flusher: flusher}
}

// event writes one named event with a JSON payload.
func (s *sseWriter) event(name string, payload any) error {
	if s.broken {
		return errors.New("the client connection is closed")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// A payload containing a newline would be read as two events, so a JSON encoder
	// is used and the marshalled bytes never contain a bare newline. Writing the
	// name and the data as separate fields keeps the event self-describing.
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		s.broken = true
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// fail reports a terminal error as an event. The status line is already sent, so
// the error travels in the stream rather than as an HTTP status. The code matches
// the error codes the buffered routes use, so a client can handle both the same
// way.
func (s *sseWriter) fail(err error) {
	code, message, retryable := streamErrorFields(err)
	_ = s.event("error", map[string]any{
		"code":      code,
		"message":   message,
		"retryable": retryable,
	})
}

// streamErrorFields maps a domain error to the code the buffered routes would
// have produced. Keeping the mapping here means a streaming client sees the same
// codes as a buffered one.
func streamErrorFields(err error) (code, message string, retryable bool) {
	switch {
	case errors.Is(err, ai.ErrDisabled):
		return string(httpx.CodeDisabled), "No AI provider is configured.", false
	case errors.Is(err, ai.ErrRateLimited):
		return string(httpx.CodeRateLimited), "The AI request rate limit was reached. Retry shortly.", true
	case errors.Is(err, ai.ErrProviderUnavailable):
		return string(httpx.CodeUnavailable), "The AI provider could not be reached. Retry.", true
	}
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return string(apiErr.Code), apiErr.Message, retryableStatusFor(apiErr.Status)
	}
	return string(httpx.CodeInternal), "The AI request failed.", false
}

type aiStreamChatRequest struct {
	Prompt      string  `json:"prompt"`
	System      string  `json:"system"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

// handleAIStreamChat streams a free-form completion.
//
// It mirrors handleCreateAITask: the same default system prompt assembled from the
// tool registry, the same validation, the same rate cap. The only difference is
// that the reply arrives as `delta` events and the closing `done` event carries
// the usage the buffered route returns in its body.
func (s *Server) handleAIStreamChat(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req aiStreamChatRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"prompt": "prompt is required"}))
		return
	}
	if req.MaxTokens <= 0 || req.MaxTokens > 4096 {
		req.MaxTokens = 1024
	}
	system := req.System
	if system == "" {
		system = ai.AssembleSystemPrompt(ai.PromptInput{
			Tools: s.deps.Tools.PromptDescriptors(),
		})
	}

	stream := newSSEWriter(w)
	// The subsystem and the rate cap are checked before the first byte, so a client
	// that cannot be served is told with a normal HTTP error rather than a stream
	// that opens and immediately fails. Once the stream is open, failures travel as
	// events.
	if !s.deps.AI.Enabled() {
		stream.fail(ai.ErrDisabled)
		return
	}

	resp, err := s.deps.AI.Stream(r.Context(), principalAdapter{p}, ai.CompletionRequest{
		System:      system,
		Messages:    []ai.Message{{Role: "user", Content: req.Prompt}},
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}, func(delta string) error {
		return stream.event("delta", map[string]any{"text": delta})
	})
	if err != nil {
		stream.fail(err)
		return
	}
	_ = stream.event("done", map[string]any{
		"model":         resp.Model,
		"input_tokens":  resp.InputTokens,
		"output_tokens": resp.OutputTokens,
	})
}

type aiStreamAssistRequest struct {
	Kind          string `json:"kind"`
	Question      string `json:"question"`
	Refresh       bool   `json:"refresh"`
	RecipientHint string `json:"recipient_hint"`
}

// handleAIStreamAssist streams contextual assistance for one entity.
//
// The final `done` event carries the whole Insight, including the facts and the
// grounding confidence. The `delta` events carry the model's raw text as it
// arrives; a client that wants the parsed structure uses the `done` event, and one
// that wants to show the reply forming uses the deltas. The entity's read
// permission is enforced inside the service, exactly as on the buffered route.
func (s *Server) handleAIStreamAssist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	entityType, entityID, ok := aiAssistEntity(w, r)
	if !ok {
		return
	}
	var req aiStreamAssistRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	kind := ai.Kind(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = ai.KindSummary
	}
	if !kind.Valid() {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"kind": "unsupported assistance kind"}))
		return
	}
	if kind == ai.KindAsk && strings.TrimSpace(req.Question) == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"question": "a question is required for this kind"}))
		return
	}

	stream := newSSEWriter(w)
	if !s.deps.AI.Enabled() {
		stream.fail(ai.ErrDisabled)
		return
	}

	insight, err := s.deps.AI.AssistStream(r.Context(), p, ai.AssistRequest{
		EntityType:    entityType,
		EntityID:      entityID,
		Kind:          kind,
		Question:      req.Question,
		Refresh:       req.Refresh,
		RecipientHint: req.RecipientHint,
	}, func(delta string) error {
		return stream.event("delta", map[string]any{"text": delta})
	})
	if err != nil {
		stream.fail(err)
		return
	}
	_ = stream.event("done", insight)
}

// retryableStatusFor mirrors httpx's own rule, which is unexported there. Keeping
// the streaming error envelope consistent with the buffered one matters more than
// avoiding the small duplication: a client must not see a retryable failure
// described as permanent just because it arrived over a stream.
func retryableStatusFor(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout || status == http.StatusBadGateway
}
