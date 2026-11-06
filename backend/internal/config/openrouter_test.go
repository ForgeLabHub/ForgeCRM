package config

import (
	"strings"
	"testing"
)

// These tests pin the OpenRouter wiring added on top of the generic
// OpenAI-compatible provider: the conventional OPENROUTER_/LLM_ names are accepted
// as aliases, the bare host is completed to the versioned API path OpenRouter
// actually serves, and a misconfiguration is a startup error rather than a first
// request that returns a 401 or a 404.

func TestOpenRouterAliasesAreRead(t *testing.T) {
	baseEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test-not-real")
	t.Setenv("LLM_MODEL", "google/gemma-2-9b-it:free")
	t.Setenv("LLM_BASE_URL", "https://openrouter.ai")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AI.Provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter inferred from OPENROUTER_API_KEY", cfg.AI.Provider)
	}
	if cfg.AI.APIKey != "sk-or-test-not-real" {
		t.Fatalf("api key = %q, want the OPENROUTER_API_KEY alias", cfg.AI.APIKey)
	}
	if cfg.AI.Model != "google/gemma-2-9b-it:free" {
		t.Fatalf("model = %q, want the LLM_MODEL alias", cfg.AI.Model)
	}
	// The bare host must be completed to the versioned path, or the request would
	// go to /chat/completions instead of /api/v1/chat/completions.
	if cfg.AI.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("base URL = %q, want the versioned OpenRouter path", cfg.AI.BaseURL)
	}
}

func TestForgeCRMNamesWinOverAliases(t *testing.T) {
	baseEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test-not-real")
	t.Setenv("LLM_MODEL", "alias-model")
	t.Setenv("LLM_BASE_URL", "https://openrouter.ai")
	t.Setenv("FORGECRM_AI_MODEL", "explicit-model")
	t.Setenv("FORGECRM_AI_BASE_URL", "https://openrouter.ai/api/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AI.Model != "explicit-model" {
		t.Fatalf("model = %q, want the FORGECRM_ name to win", cfg.AI.Model)
	}
}

func TestOpenRouterBaseURLWithAPathIsLeftAlone(t *testing.T) {
	// A caller who spelled out the path — or points at a proxy — must not have it
	// rewritten. The value flows through unchanged.
	cases := []string{
		"https://openrouter.ai/api/v1",
		"https://gateway.internal/openrouter/v1",
	}
	for _, in := range cases {
		if got := normalizeOpenRouterBaseURL(in); got != in {
			t.Errorf("normalizeOpenRouterBaseURL(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestOpenRouterRequiresAKeyWhenEnabled(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_PROVIDER", "openrouter")
	t.Setenv("FORGECRM_AI_BASE_URL", "https://openrouter.ai/api/v1")
	t.Setenv("FORGECRM_AI_MODEL", "google/gemma-2-9b-it:free")
	t.Setenv("FORGECRM_AI_API_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("an enabled OpenRouter with no key was accepted; it would fail on the first request")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("the error does not name the missing key: %v", err)
	}
}

func TestUnknownAIProviderIsRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_AI_PROVIDER", "opemrouter") // typo
	t.Setenv("FORGECRM_AI_ENABLED", "true")
	t.Setenv("FORGECRM_AI_BASE_URL", "https://example.com")
	t.Setenv("FORGECRM_AI_MODEL", "m")

	_, err := Load()
	if err == nil {
		t.Fatal("a misspelled provider was accepted; it would silently leave AI off")
	}
	if !strings.Contains(err.Error(), "FORGECRM_AI_PROVIDER") {
		t.Fatalf("the error does not name the setting: %v", err)
	}
}
