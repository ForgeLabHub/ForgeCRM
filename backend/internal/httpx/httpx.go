// Package httpx provides HTTP plumbing shared by every handler: a uniform error
// envelope, JSON helpers, pagination, correlation IDs, rate limiting and
// security headers.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/logging"
)

// ErrorCode is a stable, machine-readable error identifier.
type ErrorCode string

const (
	CodeBadRequest      ErrorCode = "bad_request"
	CodeValidation      ErrorCode = "validation_failed"
	CodeUnauthorized    ErrorCode = "unauthorized"
	CodeForbidden       ErrorCode = "forbidden"
	CodeNotFound        ErrorCode = "not_found"
	CodeConflict        ErrorCode = "conflict"
	CodeRateLimited     ErrorCode = "rate_limited"
	CodePayloadTooLarge ErrorCode = "payload_too_large"
	CodeUnsupported     ErrorCode = "unsupported"
	CodeInternal        ErrorCode = "internal_error"
	CodeDisabled        ErrorCode = "disabled"
	CodeMFARequired     ErrorCode = "mfa_required"
)

// APIError is the single error shape returned by the API.
type APIError struct {
	Status  int               `json:"-"`
	Code    ErrorCode         `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

func (e *APIError) Error() string { return string(e.Code) + ": " + e.Message }

// NewError builds an APIError.
func NewError(status int, code ErrorCode, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

// ValidationError builds a 422 with per-field details.
func ValidationError(details map[string]string) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    CodeValidation,
		Message: "validation failed",
		Details: details,
	}
}

// Common errors used across handlers.
var (
	ErrNotFound   = NewError(http.StatusNotFound, CodeNotFound, "resource not found")
	ErrForbidden  = NewError(http.StatusForbidden, CodeForbidden, "permission denied")
	ErrUnauth     = NewError(http.StatusUnauthorized, CodeUnauthorized, "authentication required")
	ErrConflict   = NewError(http.StatusConflict, CodeConflict, "resource conflict")
	ErrInternal   = NewError(http.StatusInternalServerError, CodeInternal, "internal error")
	ErrDisabled   = NewError(http.StatusServiceUnavailable, CodeDisabled, "subsystem is disabled")
	ErrBadRequest = NewError(http.StatusBadRequest, CodeBadRequest, "malformed request")
)

// errorEnvelope is the single JSON shape returned for every failed request.
//
// It carries RFC 9457 Problem Details members at the top level (type, title,
// status, detail, instance) so a generic client or gateway can consume an error
// without knowing this API, and repeats the machine-readable code and the
// field-level errors under "error" so existing callers keep working. Both are
// derived from one APIError, so they cannot disagree.
type errorEnvelope struct {
	// Type is a URN identifying the problem class. It is deliberately not an
	// https URL: we do not operate a dereferenceable error catalogue, and a link
	// that 404s is worse than an identifier that never claimed to resolve. The
	// value to branch on is error.code.
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	// Instance is the request path where the problem occurred. The query string
	// is omitted on purpose: it can carry tokens and personal data that must not
	// be echoed back into logs or client-side error reporting.
	Instance string `json:"instance,omitempty"`

	// Retryable states whether resending the identical request could succeed. It
	// is what a client needs to choose between backing off and fixing the
	// request, and it is not derivable from the status code: a 500 may be a
	// transient fault or a deterministic defect, so we report the safer answer.
	Retryable     bool   `json:"retryable"`
	RequestID     string `json:"request_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`

	// Error is the original envelope, retained for compatibility.
	Error *APIError `json:"error"`
}

// errorTitles maps a code to the short summary RFC 9457 calls the problem title.
// An unlisted code falls back to the HTTP status phrase, which is always correct
// if not always specific.
var errorTitles = map[ErrorCode]string{
	CodeBadRequest:      "Malformed request",
	CodeValidation:      "Validation failed",
	CodeUnauthorized:    "Authentication required",
	CodeForbidden:       "Permission denied",
	CodeNotFound:        "Resource not found",
	CodeConflict:        "Conflict",
	CodeRateLimited:     "Too many requests",
	CodePayloadTooLarge: "Payload too large",
	CodeUnsupported:     "Unsupported operation",
	CodeInternal:        "Internal error",
	CodeDisabled:        "Subsystem disabled",
	CodeMFARequired:     "Second factor required",
}

// errorTypeURN builds the problem type identifier for a code.
func errorTypeURN(code ErrorCode) string { return "urn:forgecrm:error:" + string(code) }

// retryableStatus reports whether a request that failed with this status could
// succeed if sent again unchanged.
//
// 429 and 503 mean "not now" and 502/504 mean an upstream hop failed, so all
// four are worth a bounded retry. 500 is excluded on purpose: it usually means
// the request hit a defect, and retrying a defect is how a small incident
// becomes a large one.
func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// WriteError renders err as JSON. Unknown errors become a generic 500 so that
// internal details never reach the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	// errors.As reports true for a typed-nil *APIError while leaving apiErr
	// nil, so the nil check is required: without it the field access below
	// panics inside the error path, turning a handler mistake into a crash.
	if !errors.As(err, &apiErr) || apiErr == nil {
		apiErr = ErrInternal
	}
	if apiErr.Status == 0 {
		apiErr.Status = http.StatusInternalServerError
	}

	title, ok := errorTitles[apiErr.Code]
	if !ok {
		// http.StatusText returns "" for an unrecognised status, so keep the
		// title non-empty for clients that require the member.
		title = http.StatusText(apiErr.Status)
		if title == "" {
			title = "Error"
		}
	}

	env := errorEnvelope{
		Type:      errorTypeURN(apiErr.Code),
		Title:     title,
		Status:    apiErr.Status,
		Detail:    apiErr.Message,
		Retryable: retryableStatus(apiErr.Status),
		Error:     apiErr,
	}
	if r != nil {
		env.RequestID = RequestID(r.Context())
		env.CorrelationID = env.RequestID
		if r.URL != nil {
			env.Instance = r.URL.Path
		}
	}
	WriteJSONStatus(w, apiErr.Status, env)
}

// WriteJSON writes a 200 JSON response.
func WriteJSON(w http.ResponseWriter, v any) { WriteJSONStatus(w, http.StatusOK, v) }

// WriteJSONStatus writes v with an explicit status code.
func WriteJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

// DecodeJSON reads a JSON body with a size limit and unknown-field rejection.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return NewError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
				fmt.Sprintf("request body exceeds %d bytes", maxBytes))
		}
		if errors.Is(err, io.EOF) {
			return NewError(http.StatusBadRequest, CodeBadRequest, "request body is empty")
		}
		return NewError(http.StatusBadRequest, CodeBadRequest, "invalid JSON body: "+err.Error())
	}
	// Reject trailing content so a caller cannot smuggle a second document.
	if dec.More() {
		return NewError(http.StatusBadRequest, CodeBadRequest, "unexpected trailing data in request body")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Request context
// ---------------------------------------------------------------------------

type requestCtxKey struct{}

// RequestID returns the request's correlation identifier.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestCtxKey{}).(string)
	return v
}

// RequestIDMiddleware assigns a request/correlation ID and echoes it back.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if id == "" || len(id) > 128 {
			id = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), requestCtxKey{}, id)
		ctx = logging.WithCorrelationID(ctx, id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---------------------------------------------------------------------------
// Logging and recovery
// ---------------------------------------------------------------------------

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// AccessLogMiddleware logs one structured line per request without body content.
func AccessLogMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestID(r.Context()),
			}
			if uid := logging.Actor(r.Context()); uid != "" {
				attrs = append(attrs, "actor_id", uid)
			}
			switch {
			case rec.status >= 500:
				log.Error("request", attrs...)
			case rec.status >= 400:
				log.Warn("request", attrs...)
			default:
				log.Info("request", attrs...)
			}
		})
	}
}

// RecoverMiddleware converts a panic into a 500 without leaking the stack.
func RecoverMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					log.Error("panic recovered",
						"path", r.URL.Path,
						"request_id", RequestID(r.Context()),
						"panic", fmt.Sprint(rec))
					WriteError(w, r, ErrInternal)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Security headers
// ---------------------------------------------------------------------------

// SecurityHeadersMiddleware sets conservative browser hardening headers.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

// RateLimiter is a fixed-window per-key limiter. It is intentionally in-process:
// a single instance is the supported deployment shape for this cycle.
type RateLimiter struct {
	limit  int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	count int
	start time.Time
}

// NewRateLimiter creates a limiter allowing limit requests per window.
func NewRateLimiter(limit int, per time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: per, hits: make(map[string]*window)}
}

// Allow reports whether key may proceed, and how long to wait if not.
func (rl *RateLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	w, ok := rl.hits[key]
	if !ok || now.Sub(w.start) >= rl.window {
		rl.hits[key] = &window{count: 1, start: now}
		return true, 0
	}
	w.count++
	if w.count > rl.limit {
		return false, rl.window - now.Sub(w.start)
	}
	return true, 0
}

// Sweep removes expired windows to bound memory.
func (rl *RateLimiter) Sweep(now time.Time) {
	for k, w := range rl.hits {
		if now.Sub(w.start) >= rl.window {
			delete(rl.hits, k)
		}
	}
}

// RateLimitMiddleware applies the limiter keyed by client IP.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl == nil {
				next.ServeHTTP(w, r)
				return
			}
			ok, retry := rl.Allow(ClientIP(r), time.Now())
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
				WriteError(w, r, NewError(http.StatusTooManyRequests, CodeRateLimited,
					"too many requests"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP extracts the client address, honouring a single proxy hop.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if rip := r.Header.Get("X-Real-Ip"); rip != "" {
		return strings.TrimSpace(rip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

// Pagination holds validated list parameters.
type Pagination struct {
	Limit  int
	Offset int
}

// MaxPageSize caps collection responses so a client cannot request everything.
const MaxPageSize = 200

// ParsePagination reads limit/offset from the query string and applies defaults.
func ParsePagination(r *http.Request) Pagination {
	p := Pagination{Limit: 50, Offset: 0}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.Limit = n
		}
	}
	if p.Limit > MaxPageSize {
		p.Limit = MaxPageSize
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			p.Offset = n
		}
	}
	return p
}

// ListResponse is the standard shape for a paginated collection.
type ListResponse[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// NewListResponse builds a list response, guaranteeing items is never null.
func NewListResponse[T any](items []T, total int64, p Pagination) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	return ListResponse[T]{Items: items, Total: total, Limit: p.Limit, Offset: p.Offset}
}

// WriteJSONList writes a non-paginated collection.
//
// Small, bounded collections (pipelines, tags) do not need pagination, but they
// must still serialise as [] rather than null: a client that maps over the array
// would crash on null, and the shape would differ from every other collection.
func WriteJSONList[T any](w http.ResponseWriter, items []T) {
	if items == nil {
		items = []T{}
	}
	WriteJSON(w, map[string]any{"items": items})
}

// SortSpec is a validated ORDER BY request.
type SortSpec struct {
	Column string
	Desc   bool
}

// ParseSort validates the requested sort column against an allowlist. Only
// allowlisted columns reach the SQL layer, so the value can be interpolated
// safely.
func ParseSort(r *http.Request, allowed map[string]string, def SortSpec) SortSpec {
	col := r.URL.Query().Get("sort")
	if col == "" {
		return def
	}
	desc := false
	if strings.HasPrefix(col, "-") {
		desc = true
		col = col[1:]
	}
	mapped, ok := allowed[col]
	if !ok {
		return def
	}
	return SortSpec{Column: mapped, Desc: desc}
}

// OrderClause renders the sort as a SQL fragment. It is safe because Column is
// taken from an allowlist mapping, never from raw user input.
func (s SortSpec) OrderClause(fallback string) string {
	if s.Column == "" {
		return fallback
	}
	if s.Desc {
		return s.Column + " DESC"
	}
	return s.Column + " ASC"
}

// PathParam reads a chi URL parameter without importing chi in handlers.
func PathParam(r *http.Request, key string) string {
	return chiURLParam(r, key)
}
