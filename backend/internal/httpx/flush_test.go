package httpx

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The access-log middleware wraps the response writer to count bytes and status.
// Wrapping hides the optional interfaces the original writer implements, and the
// one that matters is http.Flusher: a server-sent-events handler flushes each
// frame, and if the wrapper does not forward Flush the stream buffers until the
// response ends. A handler that asserts for the flusher — as the SSE routes do —
// silently loses it, so this pins the passthrough rather than trusting that no one
// ever wraps a streaming handler.
func TestAccessLogPreservesFlusher(t *testing.T) {
	var sawFlusher bool
	h := AccessLogMiddleware(slog.New(slog.NewTextHandler(discard{}, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, sawFlusher = w.(http.Flusher)
			w.WriteHeader(http.StatusOK)
		}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if !sawFlusher {
		t.Fatal("the writer behind the access log is not an http.Flusher; server-sent events would buffer")
	}
}

// Flush must reach the wrapped writer rather than being swallowed. A recording
// writer that implements http.Flusher counts the calls, so a Flush on the wrapper
// that never forwards is caught.
func TestStatusRecorderForwardsFlush(t *testing.T) {
	rec := &flushRecorder{ResponseWriter: httptest.NewRecorder()}
	s := &statusRecorder{ResponseWriter: rec}

	s.Flush()

	if rec.flushes != 1 {
		t.Fatalf("flush reached the wrapped writer %d times, want 1", rec.flushes)
	}
	// A flush is the first write to the stream, so the implicit status must be
	// recorded; otherwise the access log reports the response as status 0.
	if s.status != http.StatusOK {
		t.Fatalf("status after flush = %d, want 200", s.status)
	}
}

// Unwrap lets http.ResponseController and other interface probes reach the
// original writer instead of stopping at the wrapper.
func TestStatusRecorderUnwraps(t *testing.T) {
	inner := httptest.NewRecorder()
	s := &statusRecorder{ResponseWriter: inner}
	if s.Unwrap() != http.ResponseWriter(inner) {
		t.Fatal("Unwrap did not return the wrapped writer")
	}
}

type flushRecorder struct {
	http.ResponseWriter
	flushes int
}

func (f *flushRecorder) Flush() { f.flushes++ }

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
