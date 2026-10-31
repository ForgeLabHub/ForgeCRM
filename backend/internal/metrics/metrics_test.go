package metrics

import (
	"math"
	"strings"
	"testing"
)

// TestRenderProducesExpositionFormat pins the exact bytes for a small set, so the
// format cannot drift silently. A scraper parses this text; a change in spacing or
// ordering is a change in the contract with every monitoring system.
func TestRenderProducesExpositionFormat(t *testing.T) {
	var b strings.Builder
	err := Render(&b, []Sample{
		{Name: "webhook_deliveries_dead_lettered_total",
			Help: "Transitions into the dead-letter state.", Kind: Counter, Value: 3},
		{Name: "uptime_seconds", Help: "Seconds since start.", Kind: Gauge, Value: 12},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	want := "# HELP uptime_seconds Seconds since start.\n" +
		"# TYPE uptime_seconds gauge\n" +
		"uptime_seconds 12\n" +
		"# HELP webhook_deliveries_dead_lettered_total Transitions into the dead-letter state.\n" +
		"# TYPE webhook_deliveries_dead_lettered_total counter\n" +
		"webhook_deliveries_dead_lettered_total 3\n"
	if got := b.String(); got != want {
		t.Errorf("unexpected exposition body\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestRenderGroupsSamplesUnderOneHeader checks that several samples of one metric
// share a single HELP and TYPE, which is what makes them one metric rather than
// several. A scraper rejects a body that declares the same metric twice.
func TestRenderGroupsSamplesUnderOneHeader(t *testing.T) {
	var b strings.Builder
	err := Render(&b, []Sample{
		{Name: "jobs", Help: "Jobs by status.", Kind: Gauge, Labels: map[string]string{"status": "failed"}, Value: 2},
		{Name: "jobs", Help: "Jobs by status.", Kind: Gauge, Labels: map[string]string{"status": "queued"}, Value: 5},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	got := b.String()
	if strings.Count(got, "# HELP jobs ") != 1 || strings.Count(got, "# TYPE jobs ") != 1 {
		t.Errorf("one metric declared more than once:\n%s", got)
	}
	// Sorted by label value, so the body is stable between scrapes.
	wantOrder := []string{`jobs{status="failed"} 2`, `jobs{status="queued"} 5`}
	if i, j := strings.Index(got, wantOrder[0]), strings.Index(got, wantOrder[1]); i < 0 || j < 0 || i > j {
		t.Errorf("samples are not in a stable order:\n%s", got)
	}
}

// TestRenderIsDeterministic checks that the same samples render identically
// regardless of input order. A scrape body that reorders between requests cannot
// be diffed and cannot be asserted on.
func TestRenderIsDeterministic(t *testing.T) {
	a := []Sample{
		{Name: "b_metric", Help: "b.", Kind: Gauge, Value: 1},
		{Name: "a_metric", Help: "a.", Kind: Counter, Value: 2},
	}
	b := []Sample{
		{Name: "a_metric", Help: "a.", Kind: Counter, Value: 2},
		{Name: "b_metric", Help: "b.", Kind: Gauge, Value: 1},
	}

	var first, second strings.Builder
	if err := Render(&first, a); err != nil {
		t.Fatalf("render a: %v", err)
	}
	if err := Render(&second, b); err != nil {
		t.Fatalf("render b: %v", err)
	}
	if first.String() != second.String() {
		t.Errorf("render depends on input order:\n%s\n---\n%s", first.String(), second.String())
	}
}

// TestRenderRejectsAMalformedBody checks that an invalid sample is an error rather
// than a body the scraper would silently drop. A dropped scrape looks like a
// missing metric, which is harder to diagnose than a failed request.
func TestRenderRejectsAMalformedBody(t *testing.T) {
	cases := map[string]Sample{
		"invalid name":        {Name: "has a space", Help: "x.", Kind: Gauge, Value: 1},
		"name starting digit": {Name: "1metric", Help: "x.", Kind: Gauge, Value: 1},
		"unknown type":        {Name: "metric", Help: "x.", Kind: "histogram", Value: 1},
		"invalid label":       {Name: "metric", Help: "x.", Kind: Gauge, Labels: map[string]string{"has-dash": "v"}, Value: 1},
	}
	for name, s := range cases {
		var b strings.Builder
		if err := Render(&b, []Sample{s}); err == nil {
			t.Errorf("%s: expected an error, got a body:\n%s", name, b.String())
		}
	}
}

// TestRenderRejectsConflictingDeclarations checks that one metric cannot be
// declared with two help texts or two types. A scraper would reject the body, so
// refusing it here names the bug at its source.
func TestRenderRejectsConflictingDeclarations(t *testing.T) {
	var b strings.Builder
	err := Render(&b, []Sample{
		{Name: "metric", Help: "first.", Kind: Gauge, Value: 1},
		{Name: "metric", Help: "second.", Kind: Gauge, Value: 2},
	})
	if err == nil {
		t.Fatal("expected an error for conflicting help texts")
	}

	err = Render(&b, []Sample{
		{Name: "metric", Help: "x.", Kind: Gauge, Value: 1},
		{Name: "metric", Help: "x.", Kind: Counter, Value: 2},
	})
	if err == nil {
		t.Fatal("expected an error for conflicting metric types")
	}
}

// TestRenderEscapesLabelValues checks that a value containing the characters the
// format reserves is escaped rather than emitted raw, which would produce a body
// the scraper cannot parse. A version string is the realistic case.
func TestRenderEscapesLabelValues(t *testing.T) {
	var b strings.Builder
	err := Render(&b, []Sample{
		{Name: "build_info", Help: "Build.", Kind: Gauge,
			Labels: map[string]string{"version": `1.0"beta` + "\n" + `\x`}, Value: 1},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	got := b.String()
	if strings.Contains(got, `1.0"beta`) {
		t.Errorf("an unescaped quote reached the body:\n%s", got)
	}
	if !strings.Contains(got, `\"`) || !strings.Contains(got, `\n`) || !strings.Contains(got, `\\`) {
		t.Errorf("expected escaped quote, newline and backslash:\n%s", got)
	}
}

// TestRenderFormatsSpecialValues checks the named forms for NaN and the
// infinities, which strconv would otherwise spell in a way the format does not
// accept.
func TestRenderFormatsSpecialValues(t *testing.T) {
	var b strings.Builder
	if err := Render(&b, []Sample{
		{Name: "a", Help: "a.", Kind: Gauge, Value: nan()},
		{Name: "b", Help: "b.", Kind: Gauge, Value: inf(1)},
		{Name: "c", Help: "c.", Kind: Gauge, Value: inf(-1)},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{"a NaN", "b +Inf", "c -Inf"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("expected %q in:\n%s", want, b.String())
		}
	}
}

func nan() float64      { return math.NaN() }
func inf(s int) float64 { return math.Inf(s) }
