package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geff1778/forgecrm/internal/tracectx"
)

// TestTraceMiddlewareContinuesAnIncomingTrace pins the cross-service contract:
// a caller that already has a trace keeps its trace id, and this hop is recorded
// as a new span within it.
func TestTraceMiddlewareContinuesAnIncomingTrace(t *testing.T) {
	const incoming = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	var seen string
	handler := TraceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = tracectx.TraceID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	r.Header.Set(tracectx.Header, incoming)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if seen != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id on the context: got %q, want the incoming one", seen)
	}
	out := rec.Header().Get(tracectx.Header)
	if out == "" {
		t.Fatal("the response must echo a traceparent")
	}
	parsed, ok := tracectx.Parse(out)
	if !ok {
		t.Fatalf("the echoed traceparent is not valid: %q", out)
	}
	if parsed.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("the echoed trace id changed: %q", parsed.TraceID)
	}
	if parsed.SpanID == "00f067aa0ba902b7" {
		t.Error("this hop must have its own span id, not the caller's")
	}
}

// TestTraceMiddlewareStartsATraceWhenNoneIsGiven covers the ordinary case: no
// header in, a usable trace out.
func TestTraceMiddlewareStartsATraceWhenNoneIsGiven(t *testing.T) {
	var seen string
	handler := TraceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = tracectx.TraceID(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if seen == "" {
		t.Error("a request with no header must still get a trace")
	}
	if _, ok := tracectx.Parse(rec.Header().Get(tracectx.Header)); !ok {
		t.Errorf("the response traceparent is not valid: %q", rec.Header().Get(tracectx.Header))
	}
}

// TestTraceMiddlewareDoesNotRejectAMalformedHeader documents that a broken
// traceparent is not a way to fail a request: it is replaced with a fresh trace.
func TestTraceMiddlewareDoesNotRejectAMalformedHeader(t *testing.T) {
	called := false
	handler := TraceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if tracectx.TraceID(r.Context()) == "" {
			t.Error("a fresh trace must be minted for a malformed header")
		}
	}))

	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.Header.Set(tracectx.Header, "not-a-trace")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if !called {
		t.Error("the request must still reach the handler")
	}
}

// TestOutboundTraceHeaderContinuesTheTrace checks the helper an outbound call
// uses: it carries the same trace id under a new span, and is empty when there is
// no trace to continue.
func TestOutboundTraceHeaderContinuesTheTrace(t *testing.T) {
	if got := OutboundTraceHeader(context.Background()); got != "" {
		t.Errorf("no trace on the context must yield an empty header, got %q", got)
	}

	tr := tracectx.NewTrace()
	ctx := tracectx.WithTrace(context.Background(), tr)
	got := OutboundTraceHeader(ctx)
	parsed, ok := tracectx.Parse(got)
	if !ok {
		t.Fatalf("the outbound header is not valid: %q", got)
	}
	if parsed.TraceID != tr.TraceID {
		t.Errorf("the outbound trace id changed: %q", parsed.TraceID)
	}
	if parsed.SpanID == tr.SpanID {
		t.Error("the outbound hop must use a new span id")
	}
}
