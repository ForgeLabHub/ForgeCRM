// Package logging configures structured logging with correlation IDs and
// field-level redaction of secrets.
//
// Redaction is applied centrally so that no individual call site can leak a
// password, token or key by forgetting to omit it. Keys whose names look
// sensitive are replaced before the record is written.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey string

const (
	correlationKey ctxKey = "correlation_id"
	requestKey     ctxKey = "request_id"
	actorKey       ctxKey = "actor_id"
	orgKey         ctxKey = "organization_id"
)

// WithCorrelationID stores a correlation ID on the context.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

// CorrelationID returns the correlation ID, or "" when unset.
func CorrelationID(ctx context.Context) string {
	v, _ := ctx.Value(correlationKey).(string)
	return v
}

// WithActor stores the acting user ID on the context.
func WithActor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, actorKey, id)
}

// Actor returns the acting user ID, or "" when unset.
func Actor(ctx context.Context) string {
	v, _ := ctx.Value(actorKey).(string)
	return v
}

// WithOrganization stores the active organization ID on the context.
func WithOrganization(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, orgKey, id)
}

// Organization returns the active organization ID, or "" when unset.
func Organization(ctx context.Context) string {
	v, _ := ctx.Value(orgKey).(string)
	return v
}

var sensitiveFragments = []string{
	"password", "passwd", "secret", "token", "apikey", "api_key", "authorization",
	"credential", "private_key", "privatekey", "cookie", "session_id", "sessionid",
	"signature", "csrf", "mfa_secret", "totp",
}

// IsSensitiveKey reports whether a log field name should be redacted.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, frag := range sensitiveFragments {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}

// Redact replaces the value of any sensitive field with a placeholder.
func Redact(key, value string) string {
	if value == "" {
		return value
	}
	if IsSensitiveKey(key) {
		return "[REDACTED]"
	}
	return value
}

type redactHandler struct{ inner slog.Handler }

func (h redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		redacted = append(redacted, redactAttr(a))
	}
	return redactHandler{inner: h.inner.WithAttrs(redacted)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	if a.Value.Kind() == slog.KindGroup {
		g := a.Value.Group()
		out := make([]slog.Attr, 0, len(g))
		for _, sub := range g {
			out = append(out, redactAttr(sub))
		}
		return slog.Group(a.Key, attrsToAny(out)...)
	}
	return a
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a)
	}
	return out
}

// New builds a logger from level and format strings.
func New(level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}

	var base slog.Handler
	if strings.EqualFold(format, "text") {
		base = slog.NewTextHandler(os.Stdout, opts)
	} else {
		base = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(redactHandler{inner: base})
}
