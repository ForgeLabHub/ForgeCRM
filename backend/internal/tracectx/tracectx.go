// Package tracectx propagates a W3C Trace Context through the process.
//
// ForgeCRM already carries a request id, which answers "which single request was
// this". It cannot answer the question an operator asks when something is slow or
// failing across a boundary: "which work, across services and background jobs,
// belongs to the same operation". That is what a trace does, and it is a different
// identifier: one trace spans many requests, and a retried delivery or a job that
// resumes after a restart shares the trace of the request that caused it.
//
// The format is the W3C Trace Context recommendation (the `traceparent` header),
// so a value produced here is understood by any standard tracing system and does
// not tie the platform to one vendor. This package only carries the identifier; it
// does not export spans, which keeps the dependency surface at the standard
// library. A deployment that wants to record spans can read the id from the
// context or the response header and send it wherever it likes.
package tracectx

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Header is the W3C Trace Context header name.
const Header = "traceparent"

// LogField is the structured-log field a trace id is recorded under.
const LogField = "trace_id"

// Trace is a parsed trace context.
type Trace struct {
	// TraceID is 32 lowercase hex characters and is stable for the whole operation.
	TraceID string
	// SpanID is 16 lowercase hex characters for this hop.
	SpanID string
	// Sampled reports the sampled flag of the incoming context.
	Sampled bool
}

// validTraceID is 32 hex characters, and must not be all zeroes (the spec's
// invalid value).
var validTraceID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// validSpanID is 16 hex characters, and must not be all zeroes.
var validSpanID = regexp.MustCompile(`^[0-9a-f]{16}$`)

type ctxKey struct{}

// FromContext returns the trace on the context, if any.
func FromContext(ctx context.Context) (Trace, bool) {
	t, ok := ctx.Value(ctxKey{}).(Trace)
	return t, ok
}

// TraceID returns the trace id on the context, or "" when unset.
func TraceID(ctx context.Context) string {
	t, _ := FromContext(ctx)
	return t.TraceID
}

// WithTrace stores a trace on the context.
func WithTrace(ctx context.Context, t Trace) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// NewTrace mints a fresh trace with a new trace id and span id.
func NewTrace() Trace {
	return Trace{TraceID: newTraceID(), SpanID: newSpanID()}
}

// newTraceID returns 32 lowercase hex characters, never all zeroes.
func newTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on a supported platform; if it somehow does,
		// a zero trace id is invalid and would be dropped downstream, so fall
		// back to a non-zero constant rather than emit an invalid value.
		copy(b[:], "forgecrm-trace!")
	}
	if isZero(b[:]) {
		b[15] = 1
	}
	return hex.EncodeToString(b[:])
}

// newSpanID returns 16 lowercase hex characters, never all zeroes.
func newSpanID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		copy(b[:], "spancrm!")
	}
	if isZero(b[:]) {
		b[7] = 1
	}
	return hex.EncodeToString(b[:])
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// Parse reads a `traceparent` header value.
//
// The grammar is `version "-" trace-id "-" parent-id "-" flags`. Version `00` is
// the current one and is the only one whose length is fixed; a future version may
// append fields, so a longer value with a version other than `00` is accepted for
// its leading four fields and the rest is ignored. An unparseable value yields no
// trace, and the caller starts a new one — an invalid header must not stop the
// request, and it must not be trusted either.
func Parse(header string) (Trace, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return Trace{}, false
	}
	parts := strings.Split(header, "-")
	if len(parts) < 4 {
		return Trace{}, false
	}
	version, traceID, spanID, flags := parts[0], parts[1], parts[2], parts[3]

	// Version is 2 hex characters. `ff` is reserved and invalid.
	if len(version) != 2 || !isHex(version) || version == "ff" {
		return Trace{}, false
	}
	// Version 00 has exactly four fields; extra fields mean the sender is
	// speaking a version this parser does not fully understand.
	if version == "00" && len(parts) != 4 {
		return Trace{}, false
	}
	if !validTraceID.MatchString(traceID) || traceID == strings.Repeat("0", 32) {
		return Trace{}, false
	}
	if !validSpanID.MatchString(spanID) || spanID == strings.Repeat("0", 16) {
		return Trace{}, false
	}
	if len(flags) != 2 || !isHex(flags) {
		return Trace{}, false
	}
	// Only the low bit of the first flags byte is the sampled flag in version 00.
	sampled, _ := hex.DecodeString(flags)
	return Trace{TraceID: traceID, SpanID: spanID, Sampled: len(sampled) == 1 && sampled[0]&0x01 == 1}, true
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Format renders the trace as a `traceparent` value.
func (t Trace) Format() string {
	flags := "00"
	if t.Sampled {
		flags = "01"
	}
	return "00-" + t.TraceID + "-" + t.SpanID + "-" + flags
}

// StartSpan returns a child trace: the same trace id, a new span id, for the next
// hop. Use it when making an outbound call so the callee continues the trace.
func (t Trace) StartSpan() Trace {
	return Trace{TraceID: t.TraceID, SpanID: newSpanID(), Sampled: t.Sampled}
}

// EnsureFromHeader returns a trace derived from the header, or a fresh one when
// the header is absent or invalid. It always returns a usable trace, so a caller
// never has to branch.
func EnsureFromHeader(header string) Trace {
	if t, ok := Parse(header); ok {
		return t
	}
	return NewTrace()
}

// FromSeed derives a trace whose trace id is a stable function of the seed, with a
// fresh span id for this hop.
//
// A background job has no inbound request to inherit a trace from, but it usually
// has a durable identifier — a delivery id, an event id — that is the same every
// time the work is retried. Seeding from it means the first attempt and every
// retry share one trace id, so an operator can follow the whole history of that
// unit of work, while each attempt gets its own span id and is distinguishable
// within the trace. The seed is hashed rather than used directly, so a short or
// non-hex identifier still yields a valid 32-hex trace id.
func FromSeed(seed string) Trace {
	sum := sha256.Sum256([]byte(seed))
	return Trace{TraceID: hex.EncodeToString(sum[:16]), SpanID: newSpanID()}
}
