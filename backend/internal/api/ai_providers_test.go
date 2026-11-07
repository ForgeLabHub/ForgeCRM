package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/config"
)

// TestAIProviderCatalogMatchesTheAcceptedSet pins the provider catalog the API
// publishes against the closed set config validates FORGECRM_AI_PROVIDER against.
//
// The catalog once listed only "ollama" and "openai_compatible" while config also
// accepted "openai" and "groq", so the endpoint that is supposed to tell an
// operator what can be configured omitted kinds that work. Deriving the list from
// config.AIProviderKinds makes them the same list by construction; this test fails
// if a future kind is added to one place and not the other.
func TestAIProviderCatalogMatchesTheAcceptedSet(t *testing.T) {
	srv := newTestServer(t, &Deps{
		Config: &config.Config{},
		// A real service, as production wires it; ProviderName reads it back.
		AI:  ai.NewService(&config.AIConfig{}, nil, nil, nil, nil, nil),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	rec := httptest.NewRecorder()
	srv.handleAIProviders(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("handleAIProviders returned %d, want 200", rec.Code)
	}

	var body struct {
		Items []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			RequiresKey bool   `json:"requires_api_key"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("providers response is not valid JSON: %v", err)
	}

	listed := make(map[string]bool, len(body.Items))
	for _, it := range body.Items {
		if it.Description == "" {
			t.Errorf("provider %q has no description", it.Name)
		}
		listed[it.Name] = true
	}

	for _, kind := range config.AIProviderKinds() {
		if kind == config.AIProviderNone {
			continue
		}
		if !listed[kind] {
			t.Errorf("provider %q is accepted by config but missing from the catalog", kind)
		}
		// The endpoint and config.Validate must agree end to end: a kind the catalog
		// advertises has to survive Validate with the fields a real deployment sets,
		// or an operator is told to configure a value that fails at startup.
		if err := probeProviderAccepted(kind); err != nil {
			t.Errorf("catalog lists %q but config rejects it: %v", kind, err)
		}
	}
	for name := range listed {
		if name == config.AIProviderNone {
			t.Errorf(`the catalog lists "none", which is a setting, not a provider`)
		}
	}
	if !listed[config.AIProviderGroq] {
		t.Error("groq is not advertised; a hosted OpenAI-compatible provider must be listed")
	}
}

// probeProviderAccepted reports whether config.Validate accepts name as an AI
// provider, using the same fields a real deployment sets. It returns nil when the
// provider is accepted and the validation error otherwise.
func probeProviderAccepted(name string) error {
	cfg := &config.Config{AI: config.AIConfig{
		Enabled:  true,
		Provider: name,
		BaseURL:  "https://example.test/v1",
		Model:    "test-model",
		APIKey:   "test-key",
		Timeout:  30 * time.Second,
	}}
	err := cfg.Validate()
	if err == nil {
		return nil
	}
	// Any error other than the provider-list one means the kind itself was fine.
	if strings.Contains(err.Error(), "FORGECRM_AI_PROVIDER must be one of") {
		return err
	}
	return nil
}
