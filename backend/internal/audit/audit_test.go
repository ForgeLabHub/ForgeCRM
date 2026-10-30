package audit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The action vocabulary is the contract a filter, an alert or a report is built
// from, so its shape is pinned here rather than discovered in production.
//
// Every action is namespaced ("auth.login", "workflows.created"). A bare name
// would be a value no filter written against the namespace would match, which is
// the same class of bug as the webhook event vocabulary: accepted at the write,
// invisible at the read.
func TestActionVocabularyIsNamespacedAndUnique(t *testing.T) {
	actions := []Action{
		ActionLogin, ActionLoginFailed, ActionLogout, ActionSessionRevoked,
		ActionMFAEnabled, ActionMFADisabled, ActionPasswordReset, ActionPasswordChanged,
		ActionUserInvited, ActionUserBlocked, ActionUserUnblocked, ActionUserUpdated,
		ActionRoleCreated, ActionRoleUpdated, ActionRoleGranted, ActionRoleRevoked,
		ActionOrgCreated, ActionOrgUpdated,
		ActionImportCreated, ActionImportExecuted, ActionImportCancelled, ActionExportExecuted,
		ActionMigrationCreated,
		ActionIntegrationCreated, ActionIntegrationUpdated, ActionIntegrationRevoked,
		ActionWebhookCreated, ActionWebhookUpdated, ActionWebhookReplayed, ActionWebhookDeadLettered,
		ActionDealDiscountApproved, ActionJobCancelled, ActionJobRetried, ActionSettingsUpdated,
		ActionAIToolInvoked, ActionEmailConfigured, ActionTelegramLinked, ActionTelegramUnlinked,
		ActionRetentionSweep,
		ActionDocumentUploaded, ActionDocumentArchived,
		ActionAgentRunStarted, ActionAgentRunCompleted, ActionAgentRunFailed,
		ActionAgentActionProposed, ActionAgentActionApproved, ActionAgentActionRejected,
		ActionAgentActionExpired, ActionAgentActionExecuted, ActionAgentActionFailed,
		ActionWorkflowCreated, ActionWorkflowUpdated, ActionWorkflowEnabled, ActionWorkflowArchived,
		ActionWorkflowRunOK, ActionWorkflowRunFail,
		ActionAIInsightGenerated,
	}

	seen := map[Action]bool{}
	for _, a := range actions {
		if a == "" {
			t.Error("an action constant is empty")
			continue
		}
		if !strings.Contains(string(a), ".") {
			t.Errorf("action %q is not namespaced", a)
		}
		if seen[a] {
			t.Errorf("action %q is declared twice", a)
		}
		seen[a] = true
	}
}

// Record rejects an entry with no action or no entity type before it touches the
// database. That ordering is what lets the guard be tested with no server, and it
// is the guard that keeps an anonymous, unattributable row out of the trail.
func TestRecordRequiresActionAndEntityType(t *testing.T) {
	l := &Logger{} // no database: the validation must run first
	if err := l.Record(context.Background(), Entry{EntityType: "contact"}); err == nil {
		t.Error("an entry with no action was accepted")
	}
	if err := l.Record(context.Background(), Entry{Action: "contacts.created"}); err == nil {
		t.Error("an entry with no entity type was accepted")
	}
}

// RecordSecurity rejects an event with no type before it touches the database.
func TestRecordSecurityRequiresType(t *testing.T) {
	l := &Logger{}
	if err := l.RecordSecurity(context.Background(), SecurityEvent{Severity: "warn"}); err == nil {
		t.Error("a security event with no type was accepted")
	}
}

func TestMarshalJSONOrNil(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string // "" means nil
	}{
		{"nil", nil, ""},
		{"typed nil map", map[string]any(nil), ""},
		{"json null", json.RawMessage("null"), ""},
		{"map", map[string]any{"a": 1}, `{"a":1}`},
		{"string", "x", `"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := marshalJSONOrNil(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("want nil, got %q", got)
				}
				return
			}
			if string(got) != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}

	// An unmarshalable value is an error, not a silent null: the caller must not
	// commit an audited write whose before/after state was dropped.
	if _, err := marshalJSONOrNil(make(chan int)); err == nil {
		t.Error("an unmarshalable value was accepted")
	}
}

// nonNilMap keeps the column a JSON object rather than SQL NULL, so a reader
// never has to handle both an absent key and a null document.
func TestNonNilMap(t *testing.T) {
	if got := nonNilMap(nil); got == nil || len(got) != 0 {
		t.Fatalf("nonNilMap(nil) = %v, want an empty map", got)
	}
	in := map[string]any{"k": "v"}
	if got := nonNilMap(in); len(got) != 1 || got["k"] != "v" {
		t.Fatalf("nonNilMap did not preserve the map: %v", got)
	}
}

func TestNullIfEmpty(t *testing.T) {
	if got := nullIfEmpty(""); got != nil {
		t.Errorf("nullIfEmpty(\"\") = %v, want nil", got)
	}
	if got := nullIfEmpty("x"); got != "x" {
		t.Errorf("nullIfEmpty(\"x\") = %v, want \"x\"", got)
	}
}

func TestUUIDPtrAndStrPtr(t *testing.T) {
	if UUIDPtr(uuid.Nil) != nil {
		t.Error("UUIDPtr(uuid.Nil) should be nil")
	}
	id := uuid.New()
	if p := UUIDPtr(id); p == nil || *p != id {
		t.Error("UUIDPtr did not round-trip a real id")
	}
	if p := StrPtr("x"); p == nil || *p != "x" {
		t.Error("StrPtr did not round-trip")
	}
}
