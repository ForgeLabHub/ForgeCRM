//go:build integration

package testsupport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// streamingProvider stands up a real HTTP server that emits the OpenAI streaming
// wire format, and returns a configured AI service pointed at it. The streaming
// path is exercised through the real provider code rather than a stub, because the
// frame parsing and the fallback are the parts that can be wrong.
func streamingProvider(t *testing.T, chunks []string) *ai.Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, c := range chunks {
			payload, _ := json.Marshal(map[string]any{
				"model":   "stream-model",
				"choices": []any{map[string]any{"delta": map[string]any{"content": c}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	return ai.NewService(&config.AIConfig{
		Enabled: true, Provider: "openai_compatible", BaseURL: srv.URL,
		Model: "stream-model", Timeout: 5 * time.Second, MaxRequestsMin: 1000,
	}, nil, nil, nil, nil, nil)
}

// TestAssistStreamDeliversDeltasAndTheFinalInsight is the end-to-end check of the
// streaming assistance path: the model's reply arrives incrementally, and the
// returned Insight carries the CRM facts separately from the model's analysis, so
// the interface can label each correctly.
func TestAssistStreamDeliversDeltasAndTheFinalInsight(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "ai-stream")
	account := insertAccount(t, db, org, "Streaming Co")

	crmSvc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	cfg := &config.AIConfig{Enabled: true, Provider: "openai_compatible", Model: "stream-model",
		Timeout: 5 * time.Second, MaxRequestsMin: 1000}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// A reply split across chunks, ending in a valid structured payload so the
		// parser produces an analysis and recommendations.
		full := `{"analysis":"Two open risks.","recommendations":["Call the customer"]}`
		mid := len(full) / 2
		for _, c := range []string{full[:mid], full[mid:]} {
			payload, _ := json.Marshal(map[string]any{
				"model":   "stream-model",
				"choices": []any{map[string]any{"delta": map[string]any{"content": c}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	cfg.BaseURL = srv.URL
	svc := ai.NewService(cfg, db, audit.New(db), crmSvc, nil, nil)

	p := rbac.NewPrincipal(uuid.New(), "owner@example.com", "Owner", org,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAIRead, rbac.PermAccountsRead})

	var deltas []string
	insight, err := svc.AssistStream(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: account, Kind: ai.KindRisks,
	}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("AssistStream: %v", err)
	}
	if len(deltas) < 2 {
		t.Fatalf("expected the reply in more than one delta, got %d: %v", len(deltas), deltas)
	}
	if got := strings.Join(deltas, ""); !strings.Contains(got, "Two open risks.") {
		t.Errorf("deltas joined = %q, want the model's analysis", got)
	}
	if insight.Analysis != "Two open risks." {
		t.Errorf("analysis = %q, want the parsed analysis", insight.Analysis)
	}
	if len(insight.Recommendations) != 1 || insight.Recommendations[0] != "Call the customer" {
		t.Errorf("recommendations = %v, want the parsed recommendation", insight.Recommendations)
	}
	if insight.Cached {
		t.Error("a fresh stream was reported as cached")
	}
	// The facts must come from the CRM, not the model: the account name is a fact
	// the server read.
	foundName := false
	for _, f := range insight.Facts {
		if f.Label == "Account" && f.Value == "Streaming Co" {
			foundName = true
		}
	}
	if !foundName {
		t.Errorf("the facts do not include the account name read from the CRM: %v", insight.Facts)
	}
}

// TestAssistStreamEnforcesTheEntityPermission is the authorization check for the
// streaming path. A caller holding only the AI permission must be refused before
// any provider call, exactly as on the buffered path.
func TestAssistStreamEnforcesTheEntityPermission(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "ai-stream-perm")
	account := insertAccount(t, db, org, "Guarded Co")

	providerCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	crmSvc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	svc := ai.NewService(&config.AIConfig{Enabled: true, Provider: "openai_compatible",
		BaseURL: srv.URL, Model: "m", Timeout: 5 * time.Second, MaxRequestsMin: 1000},
		db, audit.New(db), crmSvc, nil, nil)

	// The AI permission, but no accounts.read.
	p := rbac.NewPrincipal(uuid.New(), "ai-only@example.com", "AI Only", org,
		[]rbac.Permission{rbac.PermAIToolsExecute})

	_, err := svc.AssistStream(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: account, Kind: ai.KindSummary,
	}, func(string) error { return nil })
	if err == nil {
		t.Fatal("a caller without accounts.read obtained a streamed account insight")
	}
	if !strings.Contains(err.Error(), "accounts.read") {
		t.Fatalf("the refusal does not name the missing permission: %v", err)
	}
	if providerCalled {
		t.Fatal("the provider was called before the permission check")
	}
}

// TestAssistStreamRefusesAnotherOrganizationsRecord is the tenant-isolation check
// for the streaming path.
func TestAssistStreamRefusesAnotherOrganizationsRecord(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	orgA := insertOrganization(t, db, "ai-stream-org-a")
	orgB := insertOrganization(t, db, "ai-stream-org-b")
	accountA := insertAccount(t, db, orgA, "A's account")

	crmSvc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	svc := ai.NewService(&config.AIConfig{Enabled: true, Provider: "openai_compatible",
		Model: "m", Timeout: 5 * time.Second, MaxRequestsMin: 1000},
		db, audit.New(db), crmSvc, nil, nil)

	foreign := rbac.NewPrincipal(uuid.New(), "b@example.com", "B", orgB,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAIRead, rbac.PermAccountsRead})

	_, err := svc.AssistStream(ctx, foreign, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: accountA, Kind: ai.KindSummary,
	}, func(string) error { return nil })
	if err == nil {
		t.Fatal("a principal in organization B streamed an account in organization A")
	}
}
