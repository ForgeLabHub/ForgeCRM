//go:build integration

package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/ratelimit"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// TestAssistReportsAnUnreachableProviderAsUnavailable drives Assist end to end
// against a provider address nothing is listening on. The point is the error the
// caller receives: a bare transport failure would surface as a generic 500, which
// tells a user the assistant is broken and stops any retry. The classified error
// must instead be retryable, so the panel can offer "try again" and mean it.
func TestAssistReportsAnUnreachableProviderAsUnavailable(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "ai-unreachable")
	account := insertAccount(t, db, org, "Unreachable Co")
	userID := insertUser(t, db, org, "asker@example.com")

	// Bind a port and close it, so the address is well-formed and refused rather
	// than merely unroutable. A refused connection is a net.Error, the same class
	// a real provider outage produces.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	deadAddr := "http://" + l.Addr().String()
	_ = l.Close()

	cfg := &config.AIConfig{
		Enabled: true, Provider: "openai_compatible", BaseURL: deadAddr,
		Model: "test-model", Timeout: 2 * time.Second, MaxRequestsMin: 1000,
	}
	limiter, err := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.Policy{
		Name: "ai", Limit: cfg.MaxRequestsMin, Window: time.Minute, KeyPrefix: "rl:ai",
	})
	if err != nil {
		t.Fatalf("build the limiter: %v", err)
	}
	svc := ai.NewService(cfg, db, audit.New(db),
		crm.NewService(db, audit.New(db), outbox.NewStore(db)), slog.Default(), limiter)

	p := rbac.NewPrincipal(userID, "asker@example.com", "Asker", org,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAIRead, rbac.PermAccountsRead})

	_, err = svc.Assist(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount, EntityID: account, Kind: ai.KindSummary,
	})
	if err == nil {
		t.Fatal("an unreachable provider did not produce an error")
	}
	if !errors.Is(err, ai.ErrProviderUnavailable) {
		t.Fatalf("error = %v, want it to classify as ErrProviderUnavailable", err)
	}

	// The task row must record the failure, so an operator can see the outage
	// rather than only the user noticing it.
	var status string
	var errText *string
	if err := db.Pool().QueryRow(ctx,
		`SELECT status, error FROM ai_tasks WHERE organization_id = $1 ORDER BY created_at DESC LIMIT 1`,
		org).Scan(&status, &errText); err != nil {
		t.Fatalf("read the AI task: %v", err)
	}
	if status != "failed" {
		t.Errorf("task status = %q, want failed", status)
	}
	if errText == nil || *errText == "" {
		t.Errorf("the task recorded no error text")
	}
}
