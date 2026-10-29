package ai

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Confidence is a server-computed assessment of how far an insight is grounded in
// the CRM facts the model was actually given.
//
// It is deliberately not the model's self-reported confidence. A model's own
// "confidence: 0.9" is a stylistic token, not a probability: it is produced by the
// same next-token process as the answer, is not calibrated against outcomes, and
// tends to be high whether or not the answer is correct. Reporting it as a
// confidence would dress an unmeasured number as a measured one. So the number
// here is computed by the server from signals it can verify: whether the reply was
// structured, whether the concrete claims in it appear in the context, and whether
// the context itself was complete.
//
// The score is an evidence check, not a truth check. A high score means the answer
// stays inside what the record supports; it does not mean the answer is right. The
// basis is returned so a reader can see which signals produced the score rather
// than being asked to trust a bare number.
type Confidence struct {
	// Level is the band: "high", "medium" or "low".
	Level string `json:"level"`
	// Score is the grounding fraction in [0,1]. It is a heuristic weight, not a
	// probability, and is rounded to two decimals.
	Score float64 `json:"score"`
	// Basis lists, in plain language, the signals that produced the score. It is
	// never empty: a score without a reason is exactly the unmeasured number this
	// type exists to replace.
	Basis []string `json:"basis"`
}

const (
	confidenceHigh   = "high"
	confidenceMedium = "medium"
	confidenceLow    = "low"
)

// claimPattern matches the concrete, checkable claims in a model's prose: numbers,
// money, percentages and ISO dates. Names are deliberately excluded — a person's
// name can legitimately be paraphrased ("the CEO") and matching names by string
// would flag correct answers as ungrounded. A figure, by contrast, is either in
// the record or it is not, which is what makes it a useful signal.
var claimPattern = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?%?`)

// computeConfidence scores how well the model's output is anchored in the facts the
// server provided. It is a pure function so it can be tested without a provider.
func computeConfidence(analysis string, recommendations []string, facts []Fact, disclosures []Disclosure) Confidence {
	basis := make([]string, 0, 4)
	score := 1.0

	// A reply the parser could not structure is the strongest negative signal: the
	// server cannot tell the analysis from the recommendations, so nothing about it
	// can be checked.
	if hasDisclosure(disclosures, "unstructured") {
		return Confidence{
			Level: confidenceLow,
			Score: 0.25,
			Basis: []string{
				"the reply was not structured, so its parts could not be checked against the record",
			},
		}
	}

	if strings.TrimSpace(analysis) == "" && len(recommendations) == 0 {
		return Confidence{
			Level: confidenceLow,
			Score: 0.0,
			Basis: []string{"the model returned no analysis and no recommendations"},
		}
	}

	// Grounding: every concrete claim in the output should appear in the context the
	// server provided. A claim that does not is the failure mode this guards — a
	// figure the record never contained, presented as if it had.
	claims := extractClaims(analysis, recommendations)
	if len(claims) == 0 {
		basis = append(basis, "the answer makes no checkable numeric claims")
	} else {
		corpus := factCorpus(facts)
		supported := 0
		var unsupported []string
		for _, c := range claims {
			if strings.Contains(corpus, normalizeClaim(c)) {
				supported++
				continue
			}
			unsupported = append(unsupported, c)
		}
		ratio := float64(supported) / float64(len(claims))
		// Even a fully unsupported answer keeps some weight for the prose around the
		// figures; the figure check adjusts the score rather than zeroing it.
		score *= 0.4 + 0.6*ratio
		if len(unsupported) > 0 {
			sort.Strings(unsupported)
			basis = append(basis, fmt.Sprintf(
				"%d of %d figures in the answer do not appear in the record (e.g. %s)",
				len(unsupported), len(claims), strings.Join(firstN(unsupported, 3), ", ")))
		} else {
			basis = append(basis, fmt.Sprintf(
				"every figure in the answer (%d) appears in the record", len(claims)))
		}
	}

	// An incomplete context lowers the ceiling: the model cannot be grounded in data
	// it was not shown. Masking, truncation and omission are recorded as disclosures
	// by the assembler, so they are visible here without re-reading the record.
	incomplete := incompleteContextCount(disclosures)
	if incomplete > 0 {
		// Each limitation costs a little; the effect is multiplicative so several
		// together matter more than one alone, but none of them alone is fatal.
		for i := 0; i < incomplete; i++ {
			score *= 0.9
		}
		basis = append(basis, fmt.Sprintf(
			"the record was incomplete for this request (%d limitation(s): masked, truncated or omitted data)",
			incomplete))
	}

	score = round2(clamp01(score))
	return Confidence{Level: bandFor(score), Score: score, Basis: basis}
}

// extractClaims pulls the concrete numeric claims from the model's output, keeping
// the order and de-duplicating so a repeated figure is counted once.
func extractClaims(analysis string, recommendations []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for _, m := range claimPattern.FindAllString(s, -1) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	add(analysis)
	for _, r := range recommendations {
		add(r)
	}
	return out
}

// factCorpus is every fact value (and label) joined and normalized, so a claim can
// be looked up with a substring test.
func factCorpus(facts []Fact) string {
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(normalizeClaim(f.Value))
		b.WriteByte(' ')
		b.WriteString(normalizeClaim(f.Label))
		b.WriteByte('\n')
	}
	return b.String()
}

// normalizeClaim strips the separators a model may add or drop when restating a
// figure, so "$1,200" and "1200" compare equal and "1 200" does not falsely differ.
func normalizeClaim(s string) string {
	r := strings.NewReplacer(",", "", "$", "", " ", "", "€", "", "£", "")
	return r.Replace(strings.ToLower(s))
}

func hasDisclosure(disclosures []Disclosure, kind string) bool {
	for _, d := range disclosures {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

// incompleteContextCount counts the disclosures that mean the model saw less than
// the whole record. It is counted separately from the grounding check because the
// two are different failures: an unsupported figure is the model's, an incomplete
// context is the server's.
func incompleteContextCount(disclosures []Disclosure) int {
	n := 0
	for _, d := range disclosures {
		switch d.Kind {
		case "masked", "truncated", "omitted":
			n++
		}
	}
	return n
}

func bandFor(score float64) string {
	switch {
	case score >= 0.75:
		return confidenceHigh
	case score >= 0.45:
		return confidenceMedium
	default:
		return confidenceLow
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
