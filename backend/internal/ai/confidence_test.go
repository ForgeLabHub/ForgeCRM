package ai

import (
	"strings"
	"testing"
)

func TestConfidenceIsGroundedInProvidedFacts(t *testing.T) {
	facts := []Fact{
		{Label: "Amount", Value: "$12,000"},
		{Label: "Close date", Value: "2026-03-01"},
	}
	got := computeConfidence(
		"The deal is worth $12,000 and closes 2026-03-01.",
		[]string{"Confirm the close date with the buyer."},
		facts, nil)
	if got.Level != confidenceHigh {
		t.Fatalf("level = %q, want high (score %.2f, basis %v)", got.Level, got.Score, got.Basis)
	}
	if len(got.Basis) == 0 {
		t.Fatal("a confidence with no basis is the bare number this type exists to replace")
	}
}

func TestConfidenceFlagsFiguresTheRecordNeverContained(t *testing.T) {
	facts := []Fact{{Label: "Amount", Value: "$12,000"}}
	got := computeConfidence(
		"The deal is worth $48,000 and closes in 90 days.",
		nil, facts, nil)
	if got.Level == confidenceHigh {
		t.Fatalf("an invented figure must not score high: %+v", got)
	}
	// The basis must name the problem, not just lower a number.
	joined := ""
	for _, b := range got.Basis {
		joined += b + "\n"
	}
	if !strings.Contains(joined, "do not appear in the record") {
		t.Fatalf("basis does not explain the unsupported figures: %v", got.Basis)
	}
}

func TestConfidenceLowersWhenTheContextWasIncomplete(t *testing.T) {
	facts := []Fact{{Label: "Amount", Value: "$12,000"}}
	complete := computeConfidence("Worth $12,000.", nil, facts, nil)
	masked := computeConfidence("Worth $12,000.", nil, facts, []Disclosure{
		{Kind: "masked", Detail: "the email address was masked"},
		{Kind: "truncated", Detail: "only the most recent 20 activities were included"},
	})
	if masked.Score >= complete.Score {
		t.Fatalf("a masked/truncated context must score no higher: complete %.2f masked %.2f",
			complete.Score, masked.Score)
	}
}

func TestConfidenceIsLowForAnUnstructuredReply(t *testing.T) {
	got := computeConfidence("Sure! Here is a summary.", nil,
		[]Fact{{Label: "Name", Value: "Acme"}},
		[]Disclosure{{Kind: "unstructured", Detail: "not valid JSON"}})
	if got.Level != confidenceLow {
		t.Fatalf("level = %q, want low", got.Level)
	}
}

func TestConfidenceIsZeroWhenNothingWasReturned(t *testing.T) {
	got := computeConfidence("", nil, []Fact{{Label: "Name", Value: "Acme"}}, nil)
	if got.Score != 0 || got.Level != confidenceLow {
		t.Fatalf("an empty answer must score 0/low: %+v", got)
	}
}
