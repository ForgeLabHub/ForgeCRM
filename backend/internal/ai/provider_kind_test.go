package ai

import (
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
)

// An unknown provider builds nothing. config.Validate rejects such a value at
// startup, and this pins the service-side behaviour that validation protects
// against: without it the subsystem would read as enabled while every call
// reports the provider unavailable.
func TestUnknownProviderBuildsNothing(t *testing.T) {
	svc := NewService(&config.AIConfig{
		Enabled:  true,
		Provider: "grok",
		BaseURL:  "https://example.invalid/v1",
		Model:    "m",
	}, nil, nil, nil, nil, nil)

	if svc.Enabled() {
		t.Fatal("an unknown provider kind enabled the AI layer; config validation must reject it before this point")
	}
}

// A provider built directly, as a test might, has no configured kind, so it
// falls back to the generic name instead of reporting an empty string.
func TestDirectlyBuiltProviderFallsBackToTheGenericName(t *testing.T) {
	p := &OpenAICompatibleProvider{baseURL: "http://127.0.0.1:11434/v1", model: "m"}
	if got := p.Name(); got != "openai_compatible" {
		t.Fatalf("Name() = %q, want openai_compatible", got)
	}
}
