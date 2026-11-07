package ai

import (
	"strings"
	"testing"
)

func TestTaskSummaryDoesNotRetainPromptContent(t *testing.T) {
	secret := "client@example.test confidential CRM text"
	for _, req := range []CompletionRequest{
		{Messages: []Message{{Role: "user", Content: secret}}},
		{System: secret, Messages: []Message{{Role: "user", Content: strings.Repeat("x", 250) + secret}}},
		{Messages: []Message{{Role: "user", Content: secret}, {Role: "assistant", Content: secret}}},
	} {
		got := summarisePrompt(req)
		if got != "[content omitted]" || strings.Contains(got, secret) {
			t.Fatalf("task summary must not contain request data: %q", got)
		}
	}
	if got := summarisePrompt(CompletionRequest{}); got != "" {
		t.Fatalf("empty request summary = %q", got)
	}
}
