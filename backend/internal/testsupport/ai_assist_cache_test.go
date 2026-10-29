//go:build integration

package testsupport

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// newFakeProvider starts an OpenAI-compatible endpoint that answers every
// completion with a distinct, numbered analysis, and reports how many calls it
// received.
//
// The contextual path is driven through the real provider client so the cache is
// exercised on the real code path: a stub Provider would let a cache bug hide
// behind a hand-written service method that never runs.
func newFakeProvider(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		n := atomic.AddInt64(&calls, 1)
		body, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"analysis":"answer-` + itoa(n) + `","recommendations":[]}`}},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestAssistCacheIsKeyedByTheQuestion is the regression for a cache read that
// ignored the context hash.
//
// ai_insights is keyed by (organization, entity, kind, context_hash), and its
// unique constraint includes context_hash precisely because two requests of the
// same kind can carry different input — two different questions, most obviously.
// The read filtered on everything except the hash, so the second question
// returned the first question's cached answer: the user asked one thing and was
// shown another, with the panel reporting it as a fresh answer for their input.
func TestAssistCacheIsKeyedByTheQuestion(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "ai-cache")
	account := insertAccount(t, db, org, "Cached Co")
	// created_by is a real foreign key, so the asker must be a real user.
	userID := insertUser(t, db, org, "asker@example.com")

	srv, calls := newFakeProvider(t)
	cfg := &config.AIConfig{
		Enabled: true, Provider: "openai_compatible", BaseURL: srv.URL,
		Model: "test-model", Timeout: 5 * time.Second, MaxRequestsMin: 1000,
	}
	svc := ai.NewService(cfg, db, audit.New(db), crm.NewService(db, audit.New(db), outbox.NewStore(db)), slog.Default())

	p := rbac.NewPrincipal(userID, "asker@example.com", "Asker", org,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAIRead, rbac.PermAccountsRead})

	first, err := svc.Assist(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: account, Kind: ai.KindAsk, Question: "What is the renewal risk?",
	})
	if err != nil {
		t.Fatalf("first ask: %v", err)
	}
	if first.Analysis != "answer-1" {
		t.Fatalf("first answer = %q, want answer-1", first.Analysis)
	}

	second, err := svc.Assist(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: account, Kind: ai.KindAsk, Question: "Who is the economic buyer?",
	})
	if err != nil {
		t.Fatalf("second ask: %v", err)
	}
	if second.Cached {
		t.Errorf("a second, different question was served from cache")
	}
	if second.Analysis != "answer-2" {
		t.Errorf("second answer = %q, want answer-2; the first question's answer was reused", second.Analysis)
	}
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Errorf("the provider was called %d times, want 2 (one per distinct question)", got)
	}

	// The same question asked again must hit the cache and not call the provider.
	again, err := svc.Assist(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: account, Kind: ai.KindAsk, Question: "What is the renewal risk?",
	})
	if err != nil {
		t.Fatalf("repeat ask: %v", err)
	}
	if !again.Cached {
		t.Errorf("repeating the same question did not use the cache")
	}
	if again.Analysis != "answer-1" {
		t.Errorf("repeated answer = %q, want answer-1", again.Analysis)
	}
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Errorf("the provider was called %d times, want 2; the repeat should not reach it", got)
	}
}
