package idempotency

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The buffering writer stands between a handler and the client. If it is not
// transparent the handler's response is corrupted, so the shape of what reaches the
// client is checked directly: status, headers and body, buffered and streamed.

func TestReplayWriterBuffersAWholeResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &replayWriter{w: rec}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusCreated)
	if _, err := rw.Write([]byte(`{"id":"1"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Nothing reaches the client until finish: that is the point of buffering, so a
	// response can be stored before it is sent.
	if rec.Body.Len() != 0 {
		t.Fatalf("the body reached the client before finish: %q", rec.Body.String())
	}
	rw.finish()

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if got := rec.Body.String(); got != `{"id":"1"}` {
		t.Fatalf("body = %q, want the handler's body", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q, want application/json", ct)
	}
}

// A handler that never writes must still produce a 200, not a zero status, so the
// access log and the stored response agree with the wire.
func TestReplayWriterDefaultsToOKWhenNothingIsWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &replayWriter{w: rec}
	rw.finish()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// A flush promotes the response to streaming and forwards it, so a server-sent
// event frame is not held back.
func TestReplayWriterFlushStreamsAndForwards(t *testing.T) {
	rec := &flushRecorder{ResponseWriter: httptest.NewRecorder()}
	rw := &replayWriter{w: rec}

	rw.Header().Set("Content-Type", "text/event-stream")
	if _, err := rw.Write([]byte("event: delta\ndata: {}\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	rw.Flush()

	if !rw.streaming {
		t.Fatal("a flush did not promote the response to streaming")
	}
	if rec.flushes != 1 {
		t.Fatalf("the flush reached the wrapped writer %d times, want 1", rec.flushes)
	}
	if !strings.Contains(rec.ResponseWriter.(*httptest.ResponseRecorder).Body.String(), "event: delta") {
		t.Fatal("the buffered frame was not written through on promotion")
	}
	// A later write must pass straight through, not accumulate.
	if _, err := rw.Write([]byte("event: done\ndata: {}\n\n")); err != nil {
		t.Fatalf("write after flush: %v", err)
	}
	if rw.buf.Len() != 0 {
		t.Fatal("a write after promotion was buffered")
	}
}

// A response larger than the cap is promoted rather than held, so a large export
// cannot be buffered in memory.
func TestReplayWriterPromotesPastTheCap(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &replayWriter{w: rec}

	// Fill the buffer exactly to the cap: still buffered, because the limit is a
	// bound on what is held, not a trigger at the boundary.
	if _, err := rw.Write(bytes.Repeat([]byte("x"), maxBuffer)); err != nil {
		t.Fatalf("write to the cap: %v", err)
	}
	if rw.streaming {
		t.Fatal("a write to the cap promoted the response")
	}
	// One byte past the cap promotes and passes everything through.
	if _, err := rw.Write([]byte("y")); err != nil {
		t.Fatalf("write past the cap: %v", err)
	}
	if !rw.streaming {
		t.Fatal("a write past the cap did not promote the response")
	}
	if rw.buf.Len() != 0 {
		t.Fatalf("bytes remained buffered after promotion: %d", rw.buf.Len())
	}
	if got := rec.Body.Len(); got != maxBuffer+1 {
		t.Fatalf("the client received %d bytes, want %d", got, maxBuffer+1)
	}
}

// A request without a key, and a non-mutating request, both bypass the store
// entirely, so the feature costs nothing when unused. The store here has no
// database, which would panic if it were reached — so a pass-through that returns
// the handler's own response proves the claim was never attempted.
func TestMiddlewarePassesThroughWithoutAKeyOrForARead(t *testing.T) {
	identity := func(_ context.Context) (string, string, bool) { return "org", "user", true }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Handler", "ran")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("handler body"))
	})
	h := Middleware(&Store{}, identity)(next)

	cases := []struct {
		name   string
		method string
		key    string
	}{
		{"no key on a mutation", http.MethodPost, ""},
		{"a key on a read", http.MethodGet, "abc"},
		{"no key on a read", http.MethodGet, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/customers", nil)
			if tc.key != "" {
				req.Header.Set(Header, tc.key)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Header().Get("X-Handler") != "ran" {
				t.Fatal("the handler did not run; the request was intercepted")
			}
			if rec.Code != http.StatusTeapot {
				t.Fatalf("status = %d, want 418 from the handler", rec.Code)
			}
			if rec.Body.String() != "handler body" {
				t.Fatalf("body = %q, want the handler's body", rec.Body.String())
			}
		})
	}
}

// An over-long key is rejected before any database work, so a caller cannot make
// the server store an unbounded value.
func TestMiddlewareRejectsAnOverlongKey(t *testing.T) {
	identity := func(_ context.Context) (string, string, bool) { return "org", "user", true }
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the handler ran despite an invalid key")
	})
	h := Middleware(&Store{}, identity)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", nil)
	req.Header.Set(Header, strings.Repeat("k", maxKeyLen+1))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// The scope separates callers and operations, so two users, or one user on two
// paths, never share a key.
func TestScopeKeySeparatesCallersAndOperations(t *testing.T) {
	a := ScopeKey("org1", "user1", http.MethodPost, "/api/v1/customers")
	if a == ScopeKey("org2", "user1", http.MethodPost, "/api/v1/customers") {
		t.Error("two organizations share a scope")
	}
	if a == ScopeKey("org1", "user2", http.MethodPost, "/api/v1/customers") {
		t.Error("two users share a scope")
	}
	if a == ScopeKey("org1", "user1", http.MethodPost, "/api/v1/deals") {
		t.Error("two paths share a scope")
	}
	if a == ScopeKey("org1", "user1", http.MethodDelete, "/api/v1/customers") {
		t.Error("two methods share a scope")
	}
}

type flushRecorder struct {
	http.ResponseWriter
	flushes int
}

func (f *flushRecorder) Flush() { f.flushes++ }

// A response the handler marked `Cache-Control: no-store` must not be written to the
// idempotency table. The case that matters is a one-time secret — a second-factor
// seed, a freshly minted API token — which the handler promises to reveal exactly
// once. Persisting it would keep the secret readable and replay it to a later
// request that reuses the key.
func TestReplayWriterRefusesToStoreANoStoreResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &replayWriter{w: rec}
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(http.StatusOK)
	if _, err := rw.Write([]byte(`{"secret":"s3cr3t"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	rw.finish()

	if rw.storable() {
		t.Fatal("a no-store response was reported as storable")
	}
}

// An ordinary response is storable, so the guard does not silently disable the
// feature it protects.
func TestReplayWriterStoresAnOrdinaryResponse(t *testing.T) {
	rw := &replayWriter{w: httptest.NewRecorder()}
	rw.Header().Set("Cache-Control", "no-cache")
	if !rw.storable() {
		t.Fatal("a response with only no-cache was reported as not storable")
	}
	rw2 := &replayWriter{w: httptest.NewRecorder()}
	if !rw2.storable() {
		t.Fatal("a response with no Cache-Control was reported as not storable")
	}
}

// The directive is parsed, not substring-matched: `no-store` inside a list is
// recognised, and a lookalike directive is not.
func TestHasNoStoreParsesTheDirectiveList(t *testing.T) {
	cases := map[string]bool{
		"no-store":                   true,
		"no-store, no-cache":         true,
		"private, no-store":          true,
		"no-cache":                   false,
		"no-store-and-more":          false,
		"":                           false,
		"max-age=0, must-revalidate": false,
		"NO-STORE":                   true,
	}
	for header, want := range cases {
		if got := hasNoStore(header); got != want {
			t.Errorf("hasNoStore(%q) = %v, want %v", header, got, want)
		}
	}
}

// recordingClaimer is a Claimer that records the calls the middleware makes, so a
// test can assert on the release without a database.
type recordingClaimer struct {
	released []string
	complete int
}

func (r *recordingClaimer) Claim(context.Context, string, string) (Outcome, *storedResponse, error) {
	return Claimed, nil, nil
}

func (r *recordingClaimer) Complete(context.Context, string, string, int, string, []byte) error {
	r.complete++
	return nil
}

func (r *recordingClaimer) Release(_ context.Context, _, key string) error {
	r.released = append(r.released, key)
	return nil
}

// A handler that panics must not leave its claim held. The panic is recovered by an
// outer middleware that knows nothing about the key, so the release has to happen as
// the stack unwinds here. Without it the key stays claimed until the lease expires,
// and every retry in that window is refused with 409.
func TestMiddlewareReleasesTheClaimWhenTheHandlerPanics(t *testing.T) {
	store := &recordingClaimer{}
	identity := func(_ context.Context) (string, string, bool) { return "org", "user", true }
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})
	h := Middleware(store, identity)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", nil)
	req.Header.Set(Header, "panic-key")
	rec := httptest.NewRecorder()

	// The middleware re-panics so the outer recover still sees it; catch it here.
	func() {
		defer func() {
			if rec := recover(); rec == nil {
				t.Fatal("the panic did not propagate; an outer recover would not see it")
			}
		}()
		h.ServeHTTP(rec, req)
	}()

	if len(store.released) != 1 || store.released[0] != "panic-key" {
		t.Fatalf("released = %v, want the claim to be released on panic", store.released)
	}
	if store.complete != 0 {
		t.Fatalf("the response was stored %d times, want 0 for a panicked handler", store.complete)
	}
}
