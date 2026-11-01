package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseCronRejectsMalformedExpressions(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"too few fields", "0 9 * *"},
		{"too many fields", "0 9 * * * *"},
		{"macro not supported", "@daily"},
		{"minute out of range", "60 9 * * *"},
		{"hour out of range", "0 24 * * *"},
		{"day out of range", "0 9 32 * *"},
		{"month out of range", "0 9 * 13 *"},
		{"weekday out of range", "0 9 * * 7"},
		{"zero step", "*/0 9 * * *"},
		{"reversed range", "30-10 9 * * *"},
		{"empty list element", "1,,2 9 * * *"},
		{"garbage", "abc 9 * * *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseCron(tc.expr); err == nil {
				t.Fatalf("ParseCron(%q) accepted a malformed expression", tc.expr)
			}
		})
	}
}

func TestCronNextFindsTheExpectedInstant(t *testing.T) {
	// 09:30 on weekdays, in UTC.
	spec, err := ParseCron("30 9 * * 1-5")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	// Friday 2026-10-30 is a weekday; the next 09:30 after Thursday 08:00 is Thursday.
	after := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC) // Thursday
	got, ok := spec.Next(after, time.UTC)
	if !ok {
		t.Fatal("Next found no instant")
	}
	want := time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Next = %s, want %s", got, want)
	}

	// After Thursday 10:00 the next weekday 09:30 is Friday, not Saturday.
	after = time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	got, _ = spec.Next(after, time.UTC)
	want = time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Next across a day boundary = %s, want %s", got, want)
	}

	// From Friday afternoon, the next weekday 09:30 skips the weekend to Monday.
	after = time.Date(2026, 10, 30, 17, 0, 0, 0, time.UTC)
	got, _ = spec.Next(after, time.UTC)
	want = time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Next across a weekend = %s, want %s", got, want)
	}
}

func TestCronNextHonoursTheTimezone(t *testing.T) {
	// 09:00 daily in New York. In late October that is EDT (UTC-4), so 09:00 local is
	// 13:00 UTC. A naive UTC evaluation would fire at 09:00 UTC.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	spec, err := ParseCron("0 9 * * *")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	after := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	got, ok := spec.Next(after, loc)
	if !ok {
		t.Fatal("Next found no instant")
	}
	if got.Hour() != 9 {
		t.Fatalf("Next hour in local time = %d, want 9", got.Hour())
	}
	if got.UTC().Hour() != 13 {
		t.Fatalf("Next hour in UTC = %d, want 13 (EDT)", got.UTC().Hour())
	}
}

func TestConditionOperators(t *testing.T) {
	env := NewEvalContext(map[string]any{"event_type": "crm.deal.created"}, json.RawMessage(`{
		"amount": 50000,
		"status": "open",
		"tags": ["enterprise", "priority"],
		"owner": {"name": "Jane", "active": true}
	}`))

	cases := []struct {
		name string
		cond Condition
		want bool
	}{
		{"eq string", Condition{Field: "event_type", Op: "eq", Value: "crm.deal.created"}, true},
		{"eq number across types", Condition{Field: "payload.amount", Op: "eq", Value: float64(50000)}, true},
		{"gt", Condition{Field: "payload.amount", Op: "gt", Value: float64(10000)}, true},
		{"gt false", Condition{Field: "payload.amount", Op: "gt", Value: float64(100000)}, false},
		{"nested path", Condition{Field: "payload.owner.name", Op: "eq", Value: "Jane"}, true},
		{"bool leaf", Condition{Field: "payload.owner.active", Op: "eq", Value: true}, true},
		{"contains in array", Condition{Field: "payload.tags", Op: "contains", Value: "enterprise"}, true},
		{"contains absent from array", Condition{Field: "payload.tags", Op: "contains", Value: "smb"}, false},
		{"in list", Condition{Field: "payload.status", Op: "in", Value: []any{"won", "open"}}, true},
		{"exists on a present field", Condition{Field: "payload.amount", Op: "exists"}, true},
		{"exists on an absent field", Condition{Field: "payload.missing", Op: "exists"}, false},
		{"starts_with", Condition{Field: "event_type", Op: "starts_with", Value: "crm."}, true},
		{"and", Condition{Op: "and", All: []Condition{
			{Field: "payload.amount", Op: "gt", Value: float64(1000)},
			{Field: "payload.status", Op: "eq", Value: "open"},
		}}, true},
		{"and short-circuits false", Condition{Op: "and", All: []Condition{
			{Field: "payload.amount", Op: "gt", Value: float64(999999)},
			{Field: "payload.status", Op: "eq", Value: "open"},
		}}, false},
		{"or", Condition{Op: "or", Any: []Condition{
			{Field: "payload.status", Op: "eq", Value: "won"},
			{Field: "payload.amount", Op: "gt", Value: float64(1000)},
		}}, true},
		{"not", Condition{Op: "not", Not: &Condition{Field: "payload.status", Op: "eq", Value: "won"}}, true},
		{"unknown field is not equal", Condition{Field: "payload.missing", Op: "eq", Value: "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := tc.cond
			if got := cond.Evaluate(env); got != tc.want {
				t.Fatalf("Evaluate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInterpolateReplacesPathsAndLeavesUnknownsEmpty(t *testing.T) {
	env := NewEvalContext(map[string]any{"event_type": "crm.deal.created"}, json.RawMessage(`{
		"title": "Big deal",
		"amount": 2500.50,
		"count": 3
	}`))

	cases := []struct{ in, want string }{
		{"Deal {{payload.title}} created", "Deal Big deal created"},
		{"amount={{payload.amount}}", "amount=2500.5"},
		{"whole number={{payload.count}}", "whole number=3"},
		{"{{payload.missing}}", ""},
		{"no placeholders", "no placeholders"},
		{"{{ payload.title }}", "Big deal"},
	}
	for _, tc := range cases {
		if got := interpolate(tc.in, env); got != tc.want {
			t.Fatalf("interpolate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDefinitionValidation(t *testing.T) {
	valid := `{"steps":[{"id":"a","type":"create_task","args":{"title":"Hi"}}]}`
	if _, err := ParseDefinition(json.RawMessage(valid)); err != nil {
		t.Fatalf("a valid definition was rejected: %v", err)
	}

	bad := []struct {
		name string
		doc  string
	}{
		{"no steps", `{"steps":[]}`},
		{"missing id", `{"steps":[{"type":"create_task","args":{"title":"x"}}]}`},
		{"duplicate id", `{"steps":[{"id":"a","type":"create_task","args":{"title":"x"}},{"id":"a","type":"create_task","args":{"title":"y"}}]}`},
		{"unknown type", `{"steps":[{"id":"a","type":"launch_missiles","args":{}}]}`},
		{"no type", `{"steps":[{"id":"a","args":{}}]}`},
		{"action without args", `{"steps":[{"id":"a","type":"create_task"}]}`},
		{"condition without a condition", `{"steps":[{"id":"a","type":"condition"}]}`},
		{"branch without paths", `{"steps":[{"id":"a","type":"branch","condition":{"field":"x","op":"eq","value":1}}]}`},
		{"unknown operator", `{"steps":[{"id":"a","type":"condition","condition":{"field":"x","op":"matches","value":"y"}}]}`},
		{"delay without a duration", `{"steps":[{"id":"a","type":"delay","args":{}}]}`},
		{"delay over the cap", `{"steps":[{"id":"a","type":"delay","args":{"hours":10000}}]}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDefinition(json.RawMessage(tc.doc)); err == nil {
				t.Fatalf("ParseDefinition accepted an invalid document: %s", tc.doc)
			}
		})
	}
}

func TestDelayInsideABranchIsRejected(t *testing.T) {
	// A delay pauses the whole run, so its position inside a conditional path would
	// be ambiguous on resume. It must be top-level.
	doc := `{"steps":[{"id":"b","type":"branch","condition":{"field":"payload.amount","op":"gt","value":1},
		"then":[{"id":"d","type":"delay","args":{"minutes":5}}]}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err == nil {
		t.Fatal("a delay inside a branch was accepted")
	}
}

func TestBranchDepthIsBounded(t *testing.T) {
	// Six levels of nesting exceed the limit of five.
	inner := `{"id":"leaf","type":"create_task","args":{"title":"x"}}`
	for i := 0; i < 6; i++ {
		inner = `{"id":"b` + string(rune('0'+i)) + `","type":"branch","condition":{"field":"x","op":"eq","value":1},"then":[` + inner + `]}`
	}
	doc := `{"steps":[` + inner + `]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err == nil {
		t.Fatal("a branch nested past the limit was accepted")
	}
}

// TestEveryStepTypeHasADeclaredPermission guards the privilege-escalation boundary.
//
// The engine calls crm.Service directly, so it cannot rely on the HTTP middleware or
// the MCP tool registry to enforce the coarse "may you do this at all" permission.
// It enforces that itself through requiredPermission, keyed by step type. A step
// type that reaches crm.Service or delivers a notification but is missing from that
// map would run for an author whose role forbids the action — a user holding only
// workflows.manage could create tasks or update deals their own role denies.
//
// The two sets must therefore agree in both directions: every step that does work
// (ActionSteps) and every step the engine runs against a permission must declare one.
func TestEveryStepTypeHasADeclaredPermission(t *testing.T) {
	for step := range ActionSteps {
		if _, ok := requiredPermission[step]; !ok {
			t.Errorf("action step %q has no entry in requiredPermission: an author "+
				"without the matching permission could run it through a workflow", step)
		}
	}
	for step := range requiredPermission {
		if !ActionSteps[step] && step != StepFindDeals {
			t.Errorf("requiredPermission names %q, which is neither an action step nor "+
				"a read step the engine runs; the entry is stale or misplaced", step)
		}
	}
	// The read step is checked in its own path (findDeals), but it must still be a
	// step the engine knows, or the permission is enforced for a type nothing runs.
	if !knownStepTypes[StepFindDeals] {
		t.Errorf("requiredPermission guards %q, which is not a known step type", StepFindDeals)
	}
}
