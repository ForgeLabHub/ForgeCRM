package tracectx

import (
	"context"
	"strings"
	"testing"
)

func TestParseAcceptsAValidHeader(t *testing.T) {
	h := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tr, ok := Parse(h)
	if !ok {
		t.Fatal("a well-formed traceparent must parse")
	}
	if tr.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id: got %q", tr.TraceID)
	}
	if tr.SpanID != "00f067aa0ba902b7" {
		t.Errorf("span id: got %q", tr.SpanID)
	}
	if !tr.Sampled {
		t.Error("flag 01 must be sampled")
	}
	if tr.Format() != h {
		t.Errorf("round trip: got %q, want %q", tr.Format(), h)
	}
}

func TestParseRejectsMalformedHeaders(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"too few fields":   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
		"short trace id":   "00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01",
		"non-hex trace":    "00-4bf92f3577b34da6a3ce929d0e0e473z-00f067aa0ba902b7-01",
		"zero trace id":    "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"zero span id":     "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"reserved version": "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"extra field v00":  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
		"bad flags":        "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-zz",
	}
	for name, header := range cases {
		if _, ok := Parse(header); ok {
			t.Errorf("%s: %q must not parse", name, header)
		}
	}
}

func TestParseAcceptsAFutureVersionWithExtraFields(t *testing.T) {
	// A later version may append fields; the leading four are still readable.
	h := "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-what-the-future-adds"
	tr, ok := Parse(h)
	if !ok {
		t.Fatal("a future version with extra fields must still parse its prefix")
	}
	if tr.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id: got %q", tr.TraceID)
	}
}

func TestParseIsNotSampledWhenFlagIsZero(t *testing.T) {
	tr, ok := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	if !ok {
		t.Fatal("a zero flag is still a valid header")
	}
	if tr.Sampled {
		t.Error("flag 00 must not be sampled")
	}
}

func TestNewTraceIsWellFormedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tr := NewTrace()
		if !validTraceID.MatchString(tr.TraceID) {
			t.Fatalf("minted trace id is not 32 hex chars: %q", tr.TraceID)
		}
		if !validSpanID.MatchString(tr.SpanID) {
			t.Fatalf("minted span id is not 16 hex chars: %q", tr.SpanID)
		}
		if seen[tr.TraceID] {
			t.Fatalf("trace id repeated: %q", tr.TraceID)
		}
		seen[tr.TraceID] = true
	}
}

func TestStartSpanKeepsTheTraceAndChangesTheSpan(t *testing.T) {
	tr := NewTrace()
	child := tr.StartSpan()
	if child.TraceID != tr.TraceID {
		t.Error("a child span must keep the trace id")
	}
	if child.SpanID == tr.SpanID {
		t.Error("a child span must have a new span id")
	}
}

func TestEnsureFromHeaderFallsBackToANewTrace(t *testing.T) {
	// An absent or invalid header must never stop the caller: it gets a fresh trace.
	for _, header := range []string{"", "garbage", "00-zz-zz-01"} {
		tr := EnsureFromHeader(header)
		if tr.TraceID == "" || tr.SpanID == "" {
			t.Errorf("header %q: expected a fresh usable trace", header)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Error("a bare context carries no trace")
	}
	if TraceID(context.Background()) != "" {
		t.Error("a bare context has an empty trace id")
	}
	tr := NewTrace()
	ctx := WithTrace(context.Background(), tr)
	got, ok := FromContext(ctx)
	if !ok || got.TraceID != tr.TraceID {
		t.Errorf("round trip: got %+v ok=%v", got, ok)
	}
	if TraceID(ctx) != tr.TraceID {
		t.Errorf("TraceID: got %q", TraceID(ctx))
	}
}

func TestFormatUsesLowercaseHex(t *testing.T) {
	tr := Trace{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16)}
	if tr.Format() != "00-"+strings.Repeat("a", 32)+"-"+strings.Repeat("b", 16)+"-00" {
		t.Errorf("unexpected format: %q", tr.Format())
	}
}

func TestFromSeedIsStableAcrossRetries(t *testing.T) {
	// The same seed must yield the same trace id, so a retry joins the first
	// attempt's trace, while the span id differs per attempt.
	first := FromSeed("delivery-abc")
	second := FromSeed("delivery-abc")
	if first.TraceID != second.TraceID {
		t.Errorf("same seed produced different trace ids: %q vs %q", first.TraceID, second.TraceID)
	}
	if first.SpanID == second.SpanID {
		t.Error("each attempt must have its own span id")
	}
	if !validTraceID.MatchString(first.TraceID) {
		t.Errorf("seeded trace id is not valid hex: %q", first.TraceID)
	}
}

func TestFromSeedDiffersBySeed(t *testing.T) {
	a := FromSeed("delivery-a")
	b := FromSeed("delivery-b")
	if a.TraceID == b.TraceID {
		t.Error("different seeds must produce different trace ids")
	}
}

func TestFromSeedAcceptsNonHexInput(t *testing.T) {
	// A UUID, a slug or any opaque id must still produce a valid trace id.
	for _, seed := range []string{"", "not-hex!", "9b2f0c1e-1a2b-4c3d-8e9f-000000000000"} {
		tr := FromSeed(seed)
		if !validTraceID.MatchString(tr.TraceID) || tr.TraceID == strings.Repeat("0", 32) {
			t.Errorf("seed %q produced an invalid trace id %q", seed, tr.TraceID)
		}
	}
}
