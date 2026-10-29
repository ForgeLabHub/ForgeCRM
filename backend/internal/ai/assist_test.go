package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// These tests exercise the contextual-assistance logic that does not need a
// database: permission mapping, the facts/inference split, personal-data masking
// and the handling of a model that does not return the requested JSON. The parts
// that read records are covered by the integration suite, because a mock of the
// CRM service would not exercise the permission checks that matter.

func principalWith(perms ...rbac.Permission) *rbac.Principal {
	p := rbac.NewPrincipal(uuid.New(), "user@example.com", "Test User", uuid.New(), perms)
	return p
}

// TestEntityPermissionsCoverEveryType guards the property that makes the
// per-entity check meaningful: a type the service knows about must have a
// permission, or the check is a no-op for it.
func TestEntityPermissionsCoverEveryType(t *testing.T) {
	for _, et := range []EntityType{EntityAccount, EntityContact, EntityDeal, EntityTicket, EntityTask} {
		if _, ok := entityReadPermission[et]; !ok {
			t.Errorf("entity type %q has no read permission; a request for it would be served without a check", et)
		}
	}
}

// TestAssistRefusesWithoutTheEntityPermission is the core authorization test.
//
// Holding ai.tools.execute is enough to reach the endpoint, but not to read a
// record. This asserts the second check exists: a caller with the AI permission
// and no CRM permission is refused before any read.
func TestAssistRefusesWithoutTheEntityPermission(t *testing.T) {
	svc := NewService(&config.AIConfig{Enabled: true, Provider: "openai_compatible", Model: "m"},
		nil, nil, nil, nil)
	p := principalWith(rbac.PermAIToolsExecute) // no deals.read

	_, err := svc.Assist(context.Background(), p, AssistRequest{
		EntityType: EntityDeal,
		EntityID:   uuid.New(),
		Kind:       KindSummary,
	})
	if err == nil {
		t.Fatal("a caller without deals.read obtained a deal insight")
	}
	if !strings.Contains(err.Error(), "deals.read") {
		t.Fatalf("the refusal does not name the missing permission: %v", err)
	}
}

// TestAssistRejectsAnUnknownEntityType checks that a type outside the map is
// refused rather than falling through to a default.
func TestAssistRejectsAnUnknownEntityType(t *testing.T) {
	svc := NewService(&config.AIConfig{}, nil, nil, nil, nil)
	_, err := svc.Assist(context.Background(), principalWith(), AssistRequest{
		EntityType: EntityType("invoice"),
		EntityID:   uuid.New(),
		Kind:       KindSummary,
	})
	if err == nil {
		t.Fatal("an unsupported entity type was accepted")
	}
}

// TestAssistValidatesKindAndQuestion checks the request-level validation, which is
// what stops a caller reaching the provider with a request that cannot be honoured.
func TestAssistValidatesKindAndQuestion(t *testing.T) {
	svc := NewService(&config.AIConfig{}, nil, nil, nil, nil)
	p := principalWith(rbac.PermDealsRead)

	t.Run("unknown kind", func(t *testing.T) {
		_, err := svc.Assist(context.Background(), p, AssistRequest{
			EntityType: EntityDeal, EntityID: uuid.New(), Kind: Kind("translate"),
		})
		if err == nil {
			t.Fatal("an unsupported kind was accepted")
		}
	})

	t.Run("ask without a question", func(t *testing.T) {
		_, err := svc.Assist(context.Background(), p, AssistRequest{
			EntityType: EntityDeal, EntityID: uuid.New(), Kind: KindAsk,
		})
		if err == nil {
			t.Fatal("an ask with no question was accepted")
		}
	})

	t.Run("over-long question", func(t *testing.T) {
		_, err := svc.Assist(context.Background(), p, AssistRequest{
			EntityType: EntityDeal, EntityID: uuid.New(), Kind: KindAsk,
			Question: strings.Repeat("x", askQuestionMaxLen+1),
		})
		if err == nil {
			t.Fatal("an over-long question was accepted")
		}
	})
}

// TestDisabledProviderIsReported checks that a disabled AI subsystem is reported
// as such rather than as a permission problem or an empty answer.
func TestDisabledProviderIsReported(t *testing.T) {
	svc := NewService(&config.AIConfig{Enabled: false}, nil, nil, nil, nil)
	if svc.Enabled() {
		t.Fatal("a service built from a disabled configuration reports itself enabled")
	}
}

// TestMaskEmailKeepsTheDomainAndHidesTheLocalPart pins the masking rule. The
// domain is the organisation signal a model can usefully reason about; the local
// part identifies a person.
func TestMaskEmailKeepsTheDomainAndHidesTheLocalPart(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "a***@example.com",
		"a@b.co":            "a***@b.co",
		"not-an-address":    "[redacted]",
		"@nolocal.com":      "[redacted]",
		"trailing@":         "[redacted]",
	}
	for in, want := range cases {
		v := in
		if got := maskEmail(&v); got != want {
			t.Errorf("maskEmail(%q) = %q, want %q", in, got, want)
		}
	}
	if got := maskEmail(nil); got != "" {
		t.Errorf("maskEmail(nil) = %q, want the empty string", got)
	}
}

// TestMaskPhoneKeepsOnlyTheLastTwoDigits checks that a masked number is not usable
// as a number.
func TestMaskPhoneKeepsOnlyTheLastTwoDigits(t *testing.T) {
	v := "+1 (555) 010-1234"
	got := maskPhone(&v)
	if got != "***34" {
		t.Errorf("maskPhone(%q) = %q, want %q", v, got, "***34")
	}
	short := "1"
	if got := maskPhone(&short); got != "[redacted]" {
		t.Errorf("maskPhone(%q) = %q, want %q", short, got, "[redacted]")
	}
}

// TestParseModelOutputSplitsAnalysisAndRecommendations checks the normal path: a
// well-formed reply becomes distinct fields, so the client can label the model's
// inference apart from CRM facts.
func TestParseModelOutputSplitsAnalysisAndRecommendations(t *testing.T) {
	content := `{"analysis":"The deal has stalled.","recommendations":["Call the buyer","Send a quote"]}`
	out, disclosures := parseModelOutput(content)
	if len(disclosures) != 0 {
		t.Fatalf("a well-formed reply produced disclosures: %+v", disclosures)
	}
	if out.Analysis != "The deal has stalled." {
		t.Errorf("analysis = %q", out.Analysis)
	}
	if len(out.Recommendations) != 2 {
		t.Fatalf("recommendations = %v, want two", out.Recommendations)
	}
}

// TestParseModelOutputFencesAndBlankEntries covers a fenced reply, which models
// produce often, and blank recommendation entries, which must not become empty
// bullets.
func TestParseModelOutputFencesAndBlankEntries(t *testing.T) {
	content := "```json\n{\"analysis\":\"ok\",\"recommendations\":[\"do a\",\"  \",\"do b\"]}\n```"
	out, _ := parseModelOutput(content)
	if out.Analysis != "ok" {
		t.Errorf("analysis = %q", out.Analysis)
	}
	if len(out.Recommendations) != 2 {
		t.Fatalf("recommendations = %v, want the two non-blank entries", out.Recommendations)
	}
}

// TestParseModelOutputFallsBackToFreeText checks the important failure mode: a
// model that answers in prose. The text is kept as the analysis and a disclosure
// records that the structured fields were unavailable, so the client can tell the
// difference between a parsed answer and raw text.
func TestParseModelOutputFallsBackToFreeText(t *testing.T) {
	out, disclosures := parseModelOutput("I could not find much here.")
	if out.Analysis != "I could not find much here." {
		t.Errorf("analysis = %q", out.Analysis)
	}
	if len(out.Recommendations) != 0 {
		t.Errorf("a prose reply produced recommendations: %v", out.Recommendations)
	}
	if len(disclosures) != 1 || disclosures[0].Kind != "unstructured" {
		t.Fatalf("a prose reply did not record the unstructured disclosure: %+v", disclosures)
	}
}

// TestParseModelOutputDropsAnEmptyDraft checks that a draft with no body is
// discarded rather than returned as an empty message a user might send.
func TestParseModelOutputDropsAnEmptyDraft(t *testing.T) {
	out, _ := parseModelOutput(`{"analysis":"a","draft":{"subject":"hi","body":"   "}}`)
	if out.Draft != nil {
		t.Fatalf("an empty draft was kept: %+v", out.Draft)
	}
}

// TestContextHashIsStableAndSensitiveToInput pins the cache key's two required
// properties: the same input hashes the same, and a changed fact hashes
// differently. Without the second, a stale answer would be served as current.
func TestContextHashIsStableAndSensitiveToInput(t *testing.T) {
	req := AssistRequest{EntityType: EntityDeal, EntityID: uuid.New(), Kind: KindSummary}
	base := &contextBundle{Facts: []Fact{{Label: "Deal", Value: "Acme renewal"}}}

	h1 := contextHash(req, base, "m1")
	h2 := contextHash(req, base, "m1")
	if h1 != h2 {
		t.Fatal("the same input produced two different hashes")
	}

	changed := &contextBundle{Facts: []Fact{{Label: "Deal", Value: "Acme renewal — won"}}}
	if contextHash(req, changed, "m1") == h1 {
		t.Fatal("a changed fact did not change the hash")
	}
	if contextHash(req, base, "m2") == h1 {
		t.Fatal("a changed model did not change the hash")
	}

	asked := req
	asked.Question = "what next?"
	if contextHash(asked, base, "m1") == h1 {
		t.Fatal("a changed question did not change the hash")
	}
}

// TestAssemblePromptKeepsFactsAndQuestionInContextBlocks checks the prompt's
// structure: the record data and the user's question are in labelled, delimited
// context blocks, not concatenated into the instruction region.
func TestAssemblePromptKeepsFactsAndQuestionInContextBlocks(t *testing.T) {
	svc := NewService(&config.AIConfig{}, nil, nil, nil, nil)
	b := &contextBundle{
		Facts:  []Fact{{Label: "Deal", Value: "Acme renewal"}},
		Blocks: []ContextBlock{{Label: "activities", Source: "crm.activities", Trust: TrustInternal, Content: "2026-01-01 [call] intro"}},
	}
	prompt := svc.buildSystemPrompt(KindAsk, b, "what is blocking this?", "the buyer")

	for _, want := range []string{"crm facts", "Deal: Acme renewal", "activities", "what is blocking this?", "the buyer"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the assembled prompt does not contain %q", want)
		}
	}
	// The data must be fenced, or a record could forge instructions.
	if !strings.Contains(prompt, contextFence) {
		t.Error("the prompt has no context fence")
	}
	// The response contract must be stated, so the model returns the shape the
	// parser expects.
	if !strings.Contains(prompt, "recommendations") {
		t.Error("the prompt does not state the response shape")
	}
}

// TestRenderFactsStatesWhenThereAreNone checks that an empty context is admitted
// rather than rendering as an empty block, which a model may read as "no data
// exists" or as an instruction to invent some.
func TestRenderFactsStatesWhenThereAreNone(t *testing.T) {
	if got := renderFacts(nil); !strings.Contains(got, "no facts") {
		t.Errorf("renderFacts(nil) = %q, want it to state there were no facts", got)
	}
}

// TestInsightJSONSeparatesFactsFromInference pins the response contract that keeps
// a model's guess from being read as a CRM value: the facts and the analysis are
// distinct JSON fields.
func TestInsightJSONSeparatesFactsFromInference(t *testing.T) {
	ins := Insight{
		Kind:            KindSummary,
		EntityType:      EntityDeal,
		EntityID:        uuid.NewString(),
		Facts:           []Fact{{Label: "Amount", Value: "1000.00 USD"}},
		Analysis:        "The deal looks healthy.",
		Recommendations: []string{"Follow up on Friday"},
	}
	encoded, err := json.Marshal(ins)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["facts"]; !ok {
		t.Error("the insight JSON has no facts field")
	}
	if _, ok := decoded["analysis"]; !ok {
		t.Error("the insight JSON has no analysis field")
	}
	if _, ok := decoded["recommendations"]; !ok {
		t.Error("the insight JSON has no recommendations field")
	}
}
