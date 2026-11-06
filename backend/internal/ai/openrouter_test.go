package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/config"
)

// OpenRouter is an OpenAI-compatible gateway, so it reuses OpenAICompatibleProvider
// rather than a second client. These tests pin the three things that differ from a
// plain OpenAI deployment: the provider is labelled "openrouter", the attribution
// headers are sent when configured, and the request still carries the bearer key.
// They exercise the real provider against a real HTTP server so a change to header
// wiring or the request path is caught here rather than at a customer's first call.

// openRouterTestServer records the headers and path of the first request and
// answers with a minimal non-streaming completion.
func openRouterTestServer(t *testing.T) (*httptest.Server, *http.Header, *string) {
	t.Helper()
	gotHeaders := &http.Header{}
	gotPath := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotHeaders = r.Header.Clone()
		*gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "google/gemma-2-9b-it:free",
			"choices": []any{map[string]any{"message": map[string]any{"content": "Привет!"}}},
			"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, gotHeaders, gotPath
}

func TestOpenRouterProviderSendsAttributionHeadersAndKey(t *testing.T) {
	srv, headers, path := openRouterTestServer(t)

	svc := NewService(&config.AIConfig{
		Enabled: true, Provider: "openrouter", BaseURL: srv.URL,
		APIKey: "test-key-not-real", Model: "google/gemma-2-9b-it:free",
		Timeout: 5 * time.Second, MaxRequestsMin: 1000,
		Referer: "https://forgecrm.example", Title: "ForgeCRM",
	}, nil, nil, nil, nil, nil)

	if !svc.Enabled() {
		t.Fatal("an openrouter provider should be enabled")
	}
	if got := svc.ProviderName(); got != "openrouter" {
		t.Fatalf("ProviderName() = %q, want openrouter", got)
	}

	resp, err := svc.provider.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "Привет"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "Привет!" {
		t.Fatalf("content = %q, want the model reply", resp.Content)
	}
	if *path != "/chat/completions" {
		t.Fatalf("request path = %q, want /chat/completions", *path)
	}
	if got := headers.Get("Authorization"); got != "Bearer test-key-not-real" {
		t.Fatalf("Authorization = %q, want the bearer key", got)
	}
	if got := headers.Get("HTTP-Referer"); got != "https://forgecrm.example" {
		t.Fatalf("HTTP-Referer = %q, want the configured referer", got)
	}
	if got := headers.Get("X-Title"); got != "ForgeCRM" {
		t.Fatalf("X-Title = %q, want the configured title", got)
	}
}

func TestOpenRouterProviderOmitsAttributionHeadersWhenUnset(t *testing.T) {
	srv, headers, _ := openRouterTestServer(t)

	svc := NewService(&config.AIConfig{
		Enabled: true, Provider: "openrouter", BaseURL: srv.URL,
		APIKey: "test-key-not-real", Model: "google/gemma-2-9b-it:free",
		Timeout: 5 * time.Second, MaxRequestsMin: 1000,
	}, nil, nil, nil, nil, nil)

	if _, err := svc.provider.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "Привет"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// An unset attribution header must be absent, not empty: sending an empty
	// HTTP-Referer would make OpenRouter record a blank application.
	if _, ok := (*headers)["Http-Referer"]; ok {
		t.Error("HTTP-Referer was sent even though it was not configured")
	}
	if _, ok := (*headers)["X-Title"]; ok {
		t.Error("X-Title was sent even though it was not configured")
	}
}
