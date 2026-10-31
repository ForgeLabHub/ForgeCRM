package crm

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// stubDefaults is a fixed organization configuration, standing in for the real
// service so the resolver's precedence can be tested without a database.
type stubDefaults struct {
	values map[string]string
}

func (s stubDefaults) EffectiveString(_ context.Context, _ uuid.UUID, key string) string {
	return s.values[key]
}

// TestResolveCurrencyPrecedence pins the order: the caller's value wins, then the
// organization's default, then the model default. Getting this order wrong is how a
// tenant's chosen currency silently fails to apply.
func TestResolveCurrencyPrecedence(t *testing.T) {
	org := uuid.New()
	ctx := context.Background()

	plain := NewService(nil, nil, nil)

	// No port attached: the model default applies.
	if got := plain.resolveCurrency(ctx, org, ""); got != "USD" {
		t.Fatalf("with no configuration attached, resolveCurrency = %q, want USD", got)
	}
	// A caller that named a currency keeps it.
	if got := plain.resolveCurrency(ctx, org, "GBP"); got != "GBP" {
		t.Fatalf("a named currency was overridden: got %q, want GBP", got)
	}

	withDefaults := NewService(nil, nil, nil)
	withDefaults.SetOrgDefaults(stubDefaults{values: map[string]string{
		SettingDefaultCurrency: "EUR",
	}})

	if got := withDefaults.resolveCurrency(ctx, org, ""); got != "EUR" {
		t.Fatalf("an unset currency did not take the organization default: got %q, want EUR", got)
	}
	if got := withDefaults.resolveCurrency(ctx, org, "JPY"); got != "JPY" {
		t.Fatalf("a named currency was overridden by the organization default: got %q, want JPY", got)
	}
}

// TestResolvePriorityPrecedence is the same check for the task priority.
func TestResolvePriorityPrecedence(t *testing.T) {
	org := uuid.New()
	ctx := context.Background()

	plain := NewService(nil, nil, nil)
	if got := plain.resolvePriority(ctx, org, ""); got != "normal" {
		t.Fatalf("with no configuration attached, resolvePriority = %q, want normal", got)
	}
	if got := plain.resolvePriority(ctx, org, "urgent"); got != "urgent" {
		t.Fatalf("a named priority was overridden: got %q, want urgent", got)
	}

	withDefaults := NewService(nil, nil, nil)
	withDefaults.SetOrgDefaults(stubDefaults{values: map[string]string{
		SettingDefaultTaskPriority: "high",
	}})
	if got := withDefaults.resolvePriority(ctx, org, ""); got != "high" {
		t.Fatalf("an unset priority did not take the organization default: got %q, want high", got)
	}
	if got := withDefaults.resolvePriority(ctx, org, "low"); got != "low" {
		t.Fatalf("a named priority was overridden by the organization default: got %q, want low", got)
	}
}

// TestResolvedDefaultStillPassesValidation checks that a default the organization
// chose is subject to the same validation as a caller's value: a default that is not
// a permitted priority must be refused, not stored.
func TestResolvedDefaultStillPassesValidation(t *testing.T) {
	withDefaults := NewService(nil, nil, nil)
	withDefaults.SetOrgDefaults(stubDefaults{values: map[string]string{
		SettingDefaultTaskPriority: "not-a-priority",
	}})

	in := TaskInput{Title: "A task"}
	in.Priority = withDefaults.resolvePriority(context.Background(), uuid.New(), in.Priority)
	if err := in.Validate(); err == nil {
		t.Fatal("a default outside the permitted set was accepted by validation")
	}
}
