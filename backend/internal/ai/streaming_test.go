package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
)

// The streaming providers speak two different wire formats, so each is exercised
// against a real HTTP server that emits the bytes a provider would. A mock of the
// Provider interface would not catch a mistake in the frame parsing, which is the
// part most likely to be wrong.

// TestOpenAIStreamParsesSSE drives the OpenAI-compatible streaming path against a
// server that sends the exact chunk shape a real provider sends, including the
// usage block that arrives only in the last chunk.
func TestOpenAIStreamParsesSSE(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		frames := []string{
			`data: {"model":"test-model","choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"model":"test-model","choices":[{"delta":{"content":", world"}}]}`,
			`data: {"model":"test-model","choices":[{"delta":{}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
			`data: [DONE]`,
		}
		for _, f := range frames {
			fmt.Fprintf(w, "%s\n\n", f)
		}
	}))
	defer srv.Close()

	p := &OpenAICompatibleProvider{baseURL: srv.URL, model: "test-model"}
	var deltas []string
	resp, err := p.StreamComplete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	if got, want := strings.Join(deltas, ""), "Hello, world"; got != want {
		t.Errorf("deltas joined = %q, want %q", got, want)
	}
	if resp.Content != "Hello, world" {
		t.Errorf("content = %q, want %q", resp.Content, "Hello, world")
	}
	if resp.InputTokens != 11 || resp.OutputTokens != 3 {
		t.Errorf("usage = %d/%d, want 11/3", resp.InputTokens, resp.OutputTokens)
	}
	if resp.Model != "test-model" {
		t.Errorf("model = %q, want test-model", resp.Model)
	}
	// The request must actually ask for a stream and for usage in the final chunk;
	// a provider that ignores the option still works, but one that honours it needs
	// both flags set or the usage block never arrives.
	if gotBody["stream"] != true {
		t.Errorf("the request did not ask to stream: %v", gotBody["stream"])
	}
	opts, _ := gotBody["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Errorf("the request did not ask for usage in the stream: %v", gotBody["stream_options"])
	}
}

// TestOpenAIStreamStopsOnDeltaError checks that an error from the consumer stops
// the read rather than being ignored. A disconnected client must not keep the
// provider call alive.
func TestOpenAIStreamStopsOnDeltaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := &OpenAICompatibleProvider{baseURL: srv.URL, model: "m"}
	calls := 0
	_, err := p.StreamComplete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(string) error {
		calls++
		return context.Canceled
	})
	if err == nil {
		t.Fatal("a consumer error was swallowed")
	}
	if calls != 1 {
		t.Errorf("the consumer was called %d times after returning an error, want 1", calls)
	}
}

// TestOllamaStreamParsesNDJSON drives the Ollama path, which streams newline
// delimited JSON rather than SSE.
func TestOllamaStreamParsesNDJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"message":{"content":"Hel"},"done":false}`+"\n")
		fmt.Fprint(w, `{"message":{"content":"lo"},"done":false}`+"\n")
		fmt.Fprint(w, `{"message":{"content":""},"done":true,"prompt_eval_count":7,"eval_count":2}`+"\n")
	}))
	defer srv.Close()

	p := &OllamaProvider{baseURL: srv.URL, model: "llama"}
	var deltas []string
	resp, err := p.StreamComplete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	if got := strings.Join(deltas, ""); got != "Hello" {
		t.Errorf("deltas joined = %q, want Hello", got)
	}
	if resp.Content != "Hello" || resp.InputTokens != 7 || resp.OutputTokens != 2 {
		t.Errorf("response = %+v, want content Hello and usage 7/2", resp)
	}
}

// TestStreamFallsBackForANonStreamingProvider is the compatibility guarantee: a
// provider that implements only the base interface must still work through Stream,
// with its whole reply delivered as one delta. This is what lets a new provider be
// added without a streaming implementation.
func TestStreamFallsBackForANonStreamingProvider(t *testing.T) {
	svc := NewService(&config.AIConfig{Enabled: true, Provider: "openai_compatible", Model: "m"},
		nil, nil, nil, nil, nil)
	svc.provider = &bufferedOnlyProvider{}

	var deltas []string
	resp, err := svc.Stream(context.Background(), nil, CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(deltas) != 1 || deltas[0] != "buffered answer" {
		t.Errorf("deltas = %v, want one delta with the whole reply", deltas)
	}
	if resp.Content != "buffered answer" {
		t.Errorf("content = %q", resp.Content)
	}
}

// TestStreamRefusesWhenDisabled checks the disabled path does not call a provider.
func TestStreamRefusesWhenDisabled(t *testing.T) {
	svc := NewService(&config.AIConfig{}, nil, nil, nil, nil, nil)
	_, err := svc.Stream(context.Background(), nil, CompletionRequest{}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("a disabled service streamed anyway: %v", err)
	}
}

// bufferedOnlyProvider implements Provider but not StreamProvider, so it exercises
// the fallback.
type bufferedOnlyProvider struct{}

func (bufferedOnlyProvider) Name() string  { return "buffered_only" }
func (bufferedOnlyProvider) Model() string { return "m" }
func (bufferedOnlyProvider) Complete(context.Context, CompletionRequest) (*CompletionResponse, error) {
	return &CompletionResponse{Content: "buffered answer", Model: "m"}, nil
}
func (bufferedOnlyProvider) Healthy(context.Context) error { return nil }

// TestProviderImplementationsSatisfyStreamProvider asserts the two real providers
// implement the streaming contract. Without this, a refactor that drops a method
// would silently fall back to buffering for every request.
func TestProviderImplementationsSatisfyStreamProvider(t *testing.T) {
	var (
		_ StreamProvider = (*OllamaProvider)(nil)
		_ StreamProvider = (*OpenAICompatibleProvider)(nil)
	)
}
