package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DeltaFunc receives one piece of a streamed reply. It is called in order, on the
// request's goroutine, and its error stops the stream: a client that has gone away
// must not keep a provider call alive, so the error is propagated rather than
// ignored.
type DeltaFunc func(string) error

// StreamProvider is the optional interface a provider implements when it can
// deliver a reply incrementally. The base Provider contract does not require it:
// a provider that cannot stream simply does not implement it, and the service
// falls back to a single Complete call whose whole reply is emitted as one delta.
//
// The returned CompletionResponse is the same one Complete would return, so a
// caller gets identical usage accounting either way.
type StreamProvider interface {
	StreamComplete(ctx context.Context, req CompletionRequest, onDelta DeltaFunc) (*CompletionResponse, error)
}

// StreamComplete streams a reply from Ollama.
//
// Ollama's streaming chat endpoint answers with one JSON object per line, the
// last of which carries the token counts. A line that fails to decode is skipped
// rather than fatal: a provider that appends a stray blank line must not break a
// reply the user is already reading.
func (p *OllamaProvider) StreamComplete(ctx context.Context, req CompletionRequest, onDelta DeltaFunc) (*CompletionResponse, error) {
	body := map[string]any{
		"model":    p.model,
		"stream":   true,
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

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}

	var (
		out   strings.Builder
		final struct {
			PromptEvalCount int
			EvalCount       int
		}
	)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done            bool   `json:"done"`
			PromptEvalCount int    `json:"prompt_eval_count"`
			EvalCount       int    `json:"eval_count"`
			Error           string `json:"error"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return nil, fmt.Errorf("ollama reported an error: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			out.WriteString(chunk.Message.Content)
			if err := onDelta(chunk.Message.Content); err != nil {
				return nil, err
			}
		}
		if chunk.Done {
			final.PromptEvalCount = chunk.PromptEvalCount
			final.EvalCount = chunk.EvalCount
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &CompletionResponse{
		Content:      out.String(),
		InputTokens:  final.PromptEvalCount,
		OutputTokens: final.EvalCount,
		Model:        p.model,
	}, nil
}

// StreamComplete streams a reply from an OpenAI-compatible chat completions API.
//
// The wire format is server-sent events: each `data:` line is a JSON chunk, and
// the stream ends with `data: [DONE]`. Token counts arrive only when the caller
// asked for them with `stream_options.include_usage`, so they are requested and
// treated as optional — a provider that ignores the option still streams text.
func (p *OpenAICompatibleProvider) StreamComplete(ctx context.Context, req CompletionRequest, onDelta DeltaFunc) (*CompletionResponse, error) {
	msgs := make([]Message, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)

	body := map[string]any{
		"model":    p.model,
		"messages": msgs,
		"stream":   true,
		// Ask for a usage block in the final chunk. Providers that do not support
		// it ignore the field rather than rejecting the request.
		"stream_options": map[string]any{"include_usage": true},
	}
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
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	client := &http.Client{Timeout: p.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("the provider returned HTTP %d", resp.StatusCode)
	}

	var (
		out       strings.Builder
		inTokens  int
		outTokens int
		model     = p.model
		scanner   = bufio.NewScanner(resp.Body)
	)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			inTokens = chunk.Usage.PromptTokens
			outTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			out.WriteString(choice.Delta.Content)
			if err := onDelta(choice.Delta.Content); err != nil {
				return nil, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &CompletionResponse{
		Content:      out.String(),
		InputTokens:  inTokens,
		OutputTokens: outTokens,
		Model:        model,
	}, nil
}

// ErrStreamUnsupported is returned when a provider cannot stream and the caller
// requires incremental output. The service normally falls back to a single call,
// so this surfaces only when the fallback is explicitly declined.
var ErrStreamUnsupported = errors.New("the configured provider cannot stream")

// Stream runs a completion and delivers the reply incrementally.
//
// It enforces the same rate cap and records the same ai_tasks row as Complete, so
// a streamed call is accounted for exactly like a buffered one. A provider that
// does not implement StreamProvider still works: its whole reply is emitted as a
// single delta after the call returns, so a client written against the streaming
// contract does not have to know which kind of provider it is talking to.
func (s *Service) Stream(ctx context.Context, p principalLike, req CompletionRequest, onDelta DeltaFunc) (*CompletionResponse, error) {
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

	var (
		resp *CompletionResponse
		err  error
	)
	if sp, ok := s.provider.(StreamProvider); ok {
		resp, err = sp.StreamComplete(callCtx, req, onDelta)
	} else {
		resp, err = s.provider.Complete(callCtx, req)
		if err == nil && resp != nil && resp.Content != "" {
			if deltaErr := onDelta(resp.Content); deltaErr != nil {
				err = deltaErr
			}
		}
	}

	latency := time.Since(started).Milliseconds()
	if err != nil {
		// A client that stopped reading is not a provider failure and must not be
		// recorded as one: the task is marked cancelled so the log distinguishes a
		// disconnected reader from an outage.
		status := "failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			status = "cancelled"
		}
		s.finishTask(ctx, taskID, status, 0, 0, latency, err)
		return nil, fmt.Errorf("model call failed: %w", classifyProviderError(callCtx, err))
	}
	s.finishTask(ctx, taskID, "succeeded", resp.InputTokens, resp.OutputTokens, latency, nil)
	return resp, nil
}
