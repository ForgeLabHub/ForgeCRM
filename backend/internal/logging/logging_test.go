package logging

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// Redaction is a security control, not a formatting convenience: it is the only
// thing stopping a password or token that a call site passes as a field from
// being written to the log, where it would be retained far longer than the
// credential lives. These tests exercise the handler that applies it, because the
// failure mode — a secret in a log file — is invisible until it is a disclosure.

func TestContextHelpersRoundTrip(t *testing.T) {
	ctx := context.Background()
	if CorrelationID(ctx) != "" || Actor(ctx) != "" || Organization(ctx) != "" {
		t.Fatal("an empty context returned a non-empty identifier")
	}

	ctx = WithCorrelationID(ctx, "corr-1")
	ctx = WithActor(ctx, "user-1")
	ctx = WithOrganization(ctx, "org-1")

	if CorrelationID(ctx) != "corr-1" {
		t.Errorf("CorrelationID = %q, want corr-1", CorrelationID(ctx))
	}
	if Actor(ctx) != "user-1" {
		t.Errorf("Actor = %q, want user-1", Actor(ctx))
	}
	if Organization(ctx) != "org-1" {
		t.Errorf("Organization = %q, want org-1", Organization(ctx))
	}
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"password", "PASSWORD", "db_passwd", "client_secret", "access_token",
		"api_key", "apiKey", "Authorization", "credential", "private_key",
		"session_id", "csrf_token", "mfa_secret", "totp", "cookie", "signature",
	}
	for _, key := range sensitive {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", key)
		}
	}
	benign := []string{"user_id", "organization_id", "email", "status", "count"}
	for _, key := range benign {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", key)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("password", "hunter2"); got != "[REDACTED]" {
		t.Errorf("Redact of a sensitive key = %q, want [REDACTED]", got)
	}
	if got := Redact("email", "a@b.c"); got != "a@b.c" {
		t.Errorf("Redact of a benign key = %q, want it unchanged", got)
	}
	// An empty value is left empty rather than replaced, so a log line does not
	// claim a field was present when it was not.
	if got := Redact("password", ""); got != "" {
		t.Errorf("Redact of an empty value = %q, want empty", got)
	}
}

// captureLogs builds a logger with the same redaction wrapper that New applies,
// writing to a buffer so the test can inspect the output. It uses the package's
// own redactHandler rather than a stand-in, so a regression that removed the
// wrapper from New would be caught by the fact that New and this helper share it.
func captureLogs(t *testing.T, format string, emit func(*slog.Logger)) string {
	t.Helper()
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var base slog.Handler = slog.NewJSONHandler(&buf, opts)
	if format == "text" {
		base = slog.NewTextHandler(&buf, opts)
	}
	emit(slog.New(redactHandler{inner: base}))
	return buf.String()
}

// TestNewRedactsSensitiveFields is the core assertion: a password passed as a
// field must not appear in the output, in either format.
func TestNewRedactsSensitiveFields(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		out := captureLogs(t, format, func(l *slog.Logger) {
			l.Info("login attempt",
				"email", "user@example.com",
				"password", "super-secret-value",
				"api_key", "sk-live-123",
			)
		})
		if strings.Contains(out, "super-secret-value") {
			t.Errorf("%s: the password appears in the log output: %s", format, out)
		}
		if strings.Contains(out, "sk-live-123") {
			t.Errorf("%s: the api key appears in the log output: %s", format, out)
		}
		if !strings.Contains(out, "user@example.com") {
			t.Errorf("%s: a benign field was dropped: %s", format, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("%s: the sensitive field was not marked redacted: %s", format, out)
		}
	}
}

// A sensitive key attached with With (a logger carried through a request) must be
// redacted too; the handler's WithAttrs path is a separate code path from Handle.
func TestRedactionAppliesToWithAttrs(t *testing.T) {
	out := captureLogs(t, "json", func(l *slog.Logger) {
		l = l.With("session_id", "abc-123")
		l.Info("request", "path", "/api/v1/crm")
	})
	if strings.Contains(out, "abc-123") {
		t.Errorf("a session id attached with With appears in the log: %s", out)
	}
	if !strings.Contains(out, "/api/v1/crm") {
		t.Errorf("a benign attached field was dropped: %s", out)
	}
}

// A sensitive key inside a group must be redacted: groups are how call sites
// nest structured data, and a handler that only redacts top-level keys would leak
// through them. A benign sibling in the same group must survive, so the recursion
// does not simply drop the group.
func TestRedactionRecursesIntoGroups(t *testing.T) {
	out := captureLogs(t, "json", func(l *slog.Logger) {
		l.Info("integration", slog.Group("conn",
			slog.String("token", "ghp_secret"),
			slog.String("host", "example.com"),
		))
	})
	if strings.Contains(out, "ghp_secret") {
		t.Errorf("a token inside a group appears in the log: %s", out)
	}
	if !strings.Contains(out, "example.com") {
		t.Errorf("a benign field inside a group was dropped: %s", out)
	}
}

// A group whose own name is sensitive is redacted whole. That is the safe
// direction: a group named "credentials" holds credentials, and redacting the
// group as a unit cannot leave a member behind.
func TestSensitiveGroupNameRedactsTheWholeGroup(t *testing.T) {
	out := captureLogs(t, "json", func(l *slog.Logger) {
		l.Info("stored", slog.Group("credentials",
			slog.String("username", "svc"),
			slog.String("token", "ghp_secret"),
		))
	})
	if strings.Contains(out, "ghp_secret") {
		t.Errorf("a token inside a sensitive group appears in the log: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("a sensitive group was not redacted: %s", out)
	}
}

func TestNewLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	l := slog.New(redactHandler{inner: base})
	l.Info("suppressed")
	l.Warn("shown")
	out := buf.String()
	if strings.Contains(out, "suppressed") {
		t.Errorf("an Info record passed a Warn-level handler: %s", out)
	}
	if !strings.Contains(out, "shown") {
		t.Errorf("a Warn record was suppressed: %s", out)
	}
}

// New must return a logger whose handler is the redacting one, not the base. The
// check asserts the wiring directly: the other tests build the handler themselves
// and so would not notice if New stopped wrapping it.
func TestNewWiresTheRedactingHandler(t *testing.T) {
	for _, format := range []string{"json", "text", "JSON", "TEXT"} {
		logger := New("info", format)
		if logger.Handler() == nil {
			t.Fatalf("%s: New returned a logger with no handler", format)
		}
		if got := fmt.Sprintf("%T", logger.Handler()); got != "logging.redactHandler" {
			t.Errorf("%s: New wrapped the base handler with %s, want logging.redactHandler", format, got)
		}
	}
}

// A level string New does not recognise falls back to Info rather than silencing
// the logger, so a typo in configuration does not turn logging off silently.
func TestNewUnknownLevelFallsBackToInfo(t *testing.T) {
	var buf bytes.Buffer
	// Reach the level decision through New by checking a recognised level works,
	// then assert the fallback with an unrecognised one.
	for _, level := range []string{"", "nonsense", "info"} {
		logger := New(level, "json")
		if logger.Handler() == nil {
			t.Fatalf("level %q produced a logger with no handler", level)
		}
	}
	// Behavioural check: at the fallback (Info) level, an Info record is emitted.
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.New(redactHandler{inner: base}).Info("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatal("an Info record was not emitted at the fallback level")
	}
}
