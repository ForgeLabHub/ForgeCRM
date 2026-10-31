// Package idempotency makes a mutating HTTP request safe to retry.
//
// The API documents that a creating operation accepts an `Idempotency-Key` header
// and that a repeat with the same key returns the original result instead of
// performing the operation twice. Nothing enforced that: the `idempotency_keys`
// table existed and the retention engine swept it, but no request ever wrote to it,
// so a client that retried a create after a dropped connection created a second
// row. A table that is never written is the same failure the consent model had — a
// record that exists and is never consulted.
//
// The mechanism is a claim. The first request to present a key inserts a row and
// proceeds; a concurrent request for the same key finds the row without a stored
// response and is refused with 409 rather than allowed to run in parallel. When the
// first request finishes successfully its response is stored, and every later
// request with the same key is answered from that stored response without reaching
// the handler. A failed request releases the key, so a client can correct the
// request and retry.
//
// A claim is a lease, not a permanent lock. A request that dies without finishing —
// the process is killed, the host loses power, a handler panics past the release
// path — would otherwise leave a row with no stored response, and every retry with
// that key would be refused with 409 until the retention sweep removed it seven days
// later. So a claim whose row is older than the lease may be taken over: the new
// request resets the claim and proceeds. The lease is longer than the server's
// write timeout, so a takeover cannot collide with a handler that is still running.
// A panic is handled immediately rather than by waiting for the lease — the
// middleware releases the claim as the panic unwinds — and the lease remains the
// backstop for the cases where no Go code runs at all.
//
// The key is scoped to the caller and the operation — organization, user, method
// and path — so one tenant's key cannot collide with another's, and a key reused
// against a different resource is a different key. The scope is built from values
// the server resolved, never from a header the client chose.
package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// Header is the request header a client sets to make a request replayable.
const Header = "Idempotency-Key"

// ReplayHeader marks a response served from a stored result rather than executed
// again. It is informational: a client can assert that its retry was deduplicated.
const ReplayHeader = "Idempotent-Replay"

// maxKeyLen bounds the header value. A longer key is a mistake or an attempt to
// fill the table, and it is rejected before any query runs rather than after the
// database has stored it.
const maxKeyLen = 255

// maxBuffer bounds how much of a response is held in memory to be stored.
//
// A response larger than this is not buffered: the request is promoted to a
// passthrough and its result is not recorded, because holding an unbounded body —
// an export, a large report — in memory to write it into an idempotency row would
// turn the retry guard into a memory amplifier. A client that retries such a
// request re-runs it, which is the behaviour it had before the header existed.
const maxBuffer = 1 << 20

// ClaimLease bounds how long a claim may be held without a result before another
// request may take it over.
//
// It must exceed the longest a handler can legitimately run, or a takeover would
// start a second execution while the first is still writing. The API's write
// timeout is 120s (cmd/forgecrm-api), so the lease is set above it; a request that
// has not answered within its own write timeout is not going to. The value is a
// package variable so a test can shorten it without waiting.
var ClaimLease = 5 * time.Minute

// Store is the persistence for idempotency claims.
type Store struct {
	db *database.DB
}

// NewStore builds a store over the connection pool.
func NewStore(db *database.DB) *Store { return &Store{db: db} }

// storedResponse is the persisted form of a completed response.
//
// The body is carried as a byte slice so it survives a round trip through JSON
// losslessly whether or not it is JSON itself; encoding/json renders []byte as
// base64, which is what makes that true for binary bodies as well.
type storedResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
}

// Outcome is what a claim attempt produced.
type Outcome int

const (
	// Claimed means the caller owns the key and must run the handler.
	Claimed Outcome = iota
	// Replayed means a completed response exists and must be returned as-is.
	Replayed
	// InFlight means another request holds the key and has not finished.
	InFlight
)

// Claim attempts to take ownership of (scope, key).
//
// It returns Claimed with no response when the caller must execute the handler,
// Replayed with the stored response when a previous request already completed, and
// InFlight when another request holds the key. The insert is the claim, so two
// requests racing for the same key cannot both proceed: the loser sees the winner's
// row and is refused.
func (s *Store) Claim(ctx context.Context, scope, key string) (Outcome, *storedResponse, error) {
	const claim = `
		INSERT INTO idempotency_keys (scope, key, response, created_at)
		VALUES ($1, $2, NULL, now())
		ON CONFLICT (scope, key) DO NOTHING
		RETURNING response`

	var raw []byte
	err := s.db.Q(ctx).QueryRow(ctx, claim, scope, key).Scan(&raw)
	if err == nil {
		// The insert won: no conflicting row existed, so the caller owns the key.
		return Claimed, nil, nil
	}
	if !database.IsNoRows(err) {
		return 0, nil, fmt.Errorf("claim idempotency key: %w", err)
	}

	// The insert lost. Read the existing row to decide between a replay, a claim
	// that is still running, and a claim that has been abandoned.
	const read = `SELECT response, created_at < now() - make_interval(secs => $3) AS stale
		FROM idempotency_keys WHERE scope = $1 AND key = $2`
	var (
		existing []byte
		stale    bool
	)
	if err := s.db.Q(ctx).QueryRow(ctx, read, scope, key, ClaimLease.Seconds()).Scan(&existing, &stale); err != nil {
		if database.IsNoRows(err) {
			// The row was swept by retention between the insert and the read. Treating
			// it as in-flight is the safe answer: it refuses the request rather than
			// running the operation twice, and the client can retry.
			return InFlight, nil, nil
		}
		return 0, nil, fmt.Errorf("read idempotency key: %w", err)
	}
	if existing != nil {
		var resp storedResponse
		if err := json.Unmarshal(existing, &resp); err != nil {
			return 0, nil, fmt.Errorf("decode stored idempotent response: %w", err)
		}
		return Replayed, &resp, nil
	}

	// No stored response: a claim is in flight. It is only still in flight if it is
	// within its lease; a claim that has outlived the lease belongs to a request that
	// died without finishing, and holding it would refuse every retry with this key
	// until retention swept it. Take it over.
	if !stale {
		return InFlight, nil, nil
	}
	// The takeover re-checks staleness in the same statement that resets the claim,
	// so two requests racing for the same abandoned claim cannot both win: the
	// second finds the row already refreshed and affects no row. The comparison is
	// done in SQL against `now()`, so it uses one clock and a skewed application
	// clock cannot expire a claim early or hold one too long.
	const takeOver = `
		UPDATE idempotency_keys SET created_at = now()
		WHERE scope = $1 AND key = $2 AND response IS NULL
		  AND created_at < now() - make_interval(secs => $3)`
	tag, err := s.db.Q(ctx).Exec(ctx, takeOver, scope, key, ClaimLease.Seconds())
	if err != nil {
		return 0, nil, fmt.Errorf("take over idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Another request took the stale claim over, or completed it, between the read
		// and here. Refuse this one rather than run a third execution.
		return InFlight, nil, nil
	}
	return Claimed, nil, nil
}

// Complete stores the response for a key the caller claimed, so later requests are
// answered from it.
//
// The update only fills a claim that has no response yet, so it is the first
// completed execution that wins. A request whose claim was taken over after the
// lease expired cannot overwrite the response the newer owner stored: the row it
// would write is already answered, and RowsAffected reports 0.
func (s *Store) Complete(ctx context.Context, scope, key string, status int, contentType string, body []byte) error {
	payload, err := json.Marshal(storedResponse{Status: status, ContentType: contentType, Body: body})
	if err != nil {
		return fmt.Errorf("encode idempotent response: %w", err)
	}
	const q = `UPDATE idempotency_keys SET response = $3 WHERE scope = $1 AND key = $2 AND response IS NULL`
	if _, err := s.db.Q(ctx).Exec(ctx, q, scope, key, payload); err != nil {
		return fmt.Errorf("store idempotent response: %w", err)
	}
	return nil
}

// Release removes a claim so the key can be used again.
//
// It is called when the handler did not succeed. A failure means the operation did
// not take effect in a way worth remembering, so holding the key would turn a
// correctable request into one that can never be retried with the same key.
//
// The delete only touches a claim without a stored response, so releasing a failed
// attempt can never erase a response another request already stored for the key.
func (s *Store) Release(ctx context.Context, scope, key string) error {
	const q = `DELETE FROM idempotency_keys WHERE scope = $1 AND key = $2 AND response IS NULL`
	if _, err := s.db.Q(ctx).Exec(ctx, q, scope, key); err != nil {
		return fmt.Errorf("release idempotency key: %w", err)
	}
	return nil
}

// ScopeKey builds the storage scope for a request.
//
// The scope separates one caller's keys from another's and one operation from
// another, so a key is only ever compared against the same user performing the same
// request on the same path. Every component is a server-resolved value: the
// organization and user come from the session, and the method and path come from the
// routing table, not from a header the client can vary.
func ScopeKey(orgID, userID, method, path string) string {
	return "org:" + orgID + ":user:" + userID + ":" + method + ":" + path
}

// Identity resolves the caller a key is scoped to. It returns ok=false when no
// authenticated caller is in the context.
type Identity func(ctx context.Context) (orgID, userID string, ok bool)

// Claimer is the persistence the middleware needs. It is an interface so the
// middleware can be tested against a recording double — the lease and the race are
// properties of the store's SQL and are covered there, while the middleware's own
// behaviour (pass-through, replay, release on panic) does not need a database.
type Claimer interface {
	Claim(ctx context.Context, scope, key string) (Outcome, *storedResponse, error)
	Complete(ctx context.Context, scope, key string, status int, contentType string, body []byte) error
	Release(ctx context.Context, scope, key string) error
}

// Middleware applies idempotency to the mutating requests that carry a key.
//
// A request without the header is passed through untouched, so the feature is
// opt-in per request and costs nothing when unused. The identity used for the scope
// is read from the context, so the middleware must be mounted after the session
// middleware that establishes it.
func Middleware(store Claimer, identity Identity) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil || !isMutation(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			key := strings.TrimSpace(r.Header.Get(Header))
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > maxKeyLen {
				httpx.WriteError(w, r, httpx.NewError(http.StatusBadRequest, httpx.CodeValidation,
					"the Idempotency-Key header is longer than 255 characters"))
				return
			}
			orgID, userID, ok := identity(r.Context())
			if !ok {
				// No resolved caller: nothing to scope the key to. The session
				// middleware already refused such a request upstream, so this is a
				// wiring mistake rather than a client error; run the request rather than
				// invent a scope from a client-supplied value.
				next.ServeHTTP(w, r)
				return
			}

			scope := ScopeKey(orgID, userID, r.Method, r.URL.Path)
			outcome, stored, err := store.Claim(r.Context(), scope, key)
			if err != nil {
				httpx.WriteError(w, r, httpx.ErrInternal)
				return
			}
			switch outcome {
			case InFlight:
				w.Header().Set("Retry-After", "1")
				httpx.WriteError(w, r, httpx.NewError(http.StatusConflict, httpx.CodeConflict,
					"a request with this Idempotency-Key is already being processed"))
				return
			case Replayed:
				writeStored(w, stored)
				return
			}

			// The caller owns the key. Buffer the response so it can be stored, unless
			// the handler turns out to stream or the body grows past the cap, in which
			// case the buffer is abandoned and bytes flow straight through.
			rw := &replayWriter{w: w}
			// A handler that panics never reaches the release below, and the panic is
			// recovered by an outer middleware that knows nothing about the claim. Release
			// it here as the stack unwinds, so a panic does not leave the key held until
			// the lease expires; the lease stays as the backstop for a process that dies
			// without running any Go code at all.
			defer func() {
				if rec := recover(); rec != nil {
					_ = store.Release(r.Context(), scope, key)
					panic(rec)
				}
			}()
			next.ServeHTTP(rw, r)
			rw.finish()

			if rw.streaming || !rw.storable() {
				// A streamed response cannot be replayed, and a response the handler
				// marked non-storable must not be written down. The second case is a
				// secret: a one-time token or second-factor seed is minted in the
				// response body and would otherwise be persisted in plaintext in
				// `idempotency_keys.response`, outliving the single delivery the
				// handler promises. Release the claim so a later retry re-executes
				// instead of being answered from a response that was never kept.
				_ = store.Release(r.Context(), scope, key)
				return
			}

			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 400 {
				// The operation did not succeed, so the key is released for a corrected
				// retry rather than pinning the failure.
				_ = store.Release(r.Context(), scope, key)
				return
			}
			// A failure to record the response only means the next retry re-executes;
			// the response already sent is correct, so the client is not told about it.
			_ = store.Complete(r.Context(), scope, key, status, rw.contentType(), rw.buf.Bytes())
		})
	}
}

// writeStored replays a completed response.
func writeStored(w http.ResponseWriter, resp *storedResponse) {
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	w.Header().Set(ReplayHeader, "true")
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(resp.Body)
}

// isMutation reports whether a method can change state. Only these are worth
// deduplicating; a repeated GET is already safe.
func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// replayWriter buffers a handler's response so it can be stored for replay.
//
// It stays transparent to the handler: the header map is the wrapped writer's, and
// Flush is forwarded. The first flush — or the first write past the buffer cap —
// promotes the response to streaming, at which point everything buffered so far is
// written through and later bytes are not held. That is what keeps a server-sent
// event stream and a large export from being buffered in memory.
type replayWriter struct {
	w           http.ResponseWriter
	status      int
	buf         bytes.Buffer
	streaming   bool
	wroteHeader bool
}

func (rw *replayWriter) Header() http.Header { return rw.w.Header() }

func (rw *replayWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.status = code
}

func (rw *replayWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	if rw.streaming {
		return rw.w.Write(b)
	}
	if rw.buf.Len()+len(b) > maxBuffer {
		// Past the cap: promote and pass everything through without keeping it.
		rw.promote()
		return rw.w.Write(b)
	}
	return rw.buf.Write(b)
}

// Flush promotes the response to streaming and forwards the flush.
func (rw *replayWriter) Flush() {
	if !rw.streaming {
		rw.promote()
	}
	if f, ok := rw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the wrapped writer so interface probes reach the original.
func (rw *replayWriter) Unwrap() http.ResponseWriter { return rw.w }

// promote commits the status line, flushes whatever was buffered and switches to
// passthrough. It is idempotent.
func (rw *replayWriter) promote() {
	if rw.streaming {
		return
	}
	rw.streaming = true
	rw.commit()
	if rw.buf.Len() > 0 {
		_, _ = rw.w.Write(rw.buf.Bytes())
		rw.buf.Reset()
	}
}

// commit writes the status line exactly once.
func (rw *replayWriter) commit() {
	if !rw.wroteHeader {
		rw.status = http.StatusOK
		rw.wroteHeader = true
	}
	rw.w.WriteHeader(rw.status)
}

// finish flushes a buffered, non-streaming response at the end of the handler.
func (rw *replayWriter) finish() {
	if rw.streaming {
		return
	}
	rw.commit()
	if rw.buf.Len() > 0 {
		_, _ = rw.w.Write(rw.buf.Bytes())
	}
}

// contentType reports the response content type for storage.
func (rw *replayWriter) contentType() string { return rw.w.Header().Get("Content-Type") }

// storable reports whether the response may be written to the idempotency table.
//
// A handler that returns a secret — a one-time token, a second-factor seed, a
// freshly minted API credential — marks the response `Cache-Control: no-store`,
// which is the same signal an HTTP cache reads. The middleware honours it for the
// same reason: a response that must not be kept anywhere must not be kept here
// either. Without this check the secret would be persisted in plaintext and
// replayed to a later request with the same key, turning a value the handler
// promises to reveal exactly once into a durable, readable one.
func (rw *replayWriter) storable() bool {
	return !hasNoStore(rw.w.Header().Get("Cache-Control"))
}

// hasNoStore reports whether a Cache-Control value forbids storage. It parses the
// directive list rather than matching a substring, so `no-store` is recognised
// whether it stands alone or shares the header with other directives, and
// `no-store-and-more` is not mistaken for it.
func hasNoStore(header string) bool {
	for _, directive := range strings.Split(header, ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(directive), "=")
		if strings.EqualFold(strings.TrimSpace(name), "no-store") {
			return true
		}
	}
	return false
}
