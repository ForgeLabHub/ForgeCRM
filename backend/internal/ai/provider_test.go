package ai

import (
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
)

// The provider name is quoted back to an operator by the admin status page and
// written into the AI audit log, so it has to be the configured kind rather than
// a generic label that hides which endpoint is in use.

func TestGroqProviderIsBuiltAndNamedHonestly(t *testing.T) {
	svc := NewService(&config.AIConfig{
		Enabled:  true,
		Provider: config.AIProviderGroq,
		BaseURL:  "https://api.groq.com/openai/v1",
		Model:    "openai/gpt-oss-20b",
	}, nil, nil, nil, nil, nil)

	if !svc.Enabled() {
		t.Fatal("a groq provider did not enable the AI layer")
	}
	if got := svc.ProviderName(); got != config.AIProviderGroq {
		t.Fatalf("ProviderName() = %q, want %q", got, config.AIProviderGroq)
	}
	if got := svc.ModelName(); got != "openai/gpt-oss-20b" {
		t.Fatalf("ModelName() = %q, want the configured model", got)
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

// An unknown provider builds nothing, which is exactly why config rejects it at
// startup; this pins the service-side behaviour the validation protects against.
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
