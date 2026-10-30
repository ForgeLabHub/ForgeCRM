package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/httpx"
)

// These tests exercise the server-sent-event transport and the error mapping. The
// service call behind them is covered by the integration suite; here the concern is
// that the bytes on the wire are a valid event stream and that a failure travels as
// an event with the code the buffered route would have used.

// TestSSEWriterEmitsValidFrames checks the frame shape: each event is a name line,
// a data line and a blank line, and the payload is JSON on a single line so it
// cannot be split by an embedded newline.
func TestSSEWriterEmitsValidFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	s := newSSEWriter(rec)

	if err := s.event("delta", map[string]any{"text": "line one\nline two"}); err != nil {
		t.Fatalf("event: %v", err)
	}
	if err := s.event("done", map[string]any{"model": "m"}); err != nil {
		t.Fatalf("event: %v", err)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "event: delta\ndata: ") {
		t.Errorf("the first frame is malformed: %q", body)
	}
	// Every data line must be a single line. A raw newline inside the JSON would be
	// read by the client as a second line of the same event and break the parse.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			var v any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &v); err != nil {
				t.Errorf("a data line is not valid JSON: %q (%v)", line, err)
			}
		}
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("the stream does not end with a blank line: %q", body)
	}
}

// TestSSEWriterReportsAFailureAsAnEvent checks that an error after the stream is
// open is delivered as an event rather than an HTTP status, which is impossible
// once the status line is committed.
func TestSSEWriterReportsAFailureAsAnEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	s := newSSEWriter(rec)
	s.fail(ai.ErrRateLimited)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("no error event was written: %q", body)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (the status line is committed before the error)", rec.Code)
	}
	// The code must match what the buffered route would return, so a client handles
	// both transports the same way.
	if !strings.Contains(body, string(httpx.CodeRateLimited)) {
		t.Errorf("the error event does not carry the rate-limited code: %q", body)
	}
	if !strings.Contains(body, `"retryable":true`) {
		t.Errorf("a rate limit was not reported as retryable: %q", body)
	}
}

// TestStreamErrorFieldsMapsDomainErrors pins the mapping from a domain error to the
// code, message and retryability a client sees.
func TestStreamErrorFieldsMapsDomainErrors(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{"disabled", ai.ErrDisabled, string(httpx.CodeDisabled), false},
		{"rate limited", ai.ErrRateLimited, string(httpx.CodeRateLimited), true},
		{"provider down", ai.ErrProviderUnavailable, string(httpx.CodeUnavailable), true},
		{"validation", httpx.ValidationError(map[string]string{"kind": "bad"}), string(httpx.CodeValidation), false},
		{"unknown", errors.New("boom"), string(httpx.CodeInternal), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, message, retryable := streamErrorFields(tc.err)
			if code != tc.code {
				t.Errorf("code = %q, want %q", code, tc.code)
			}
			if message == "" {
				t.Error("the error event carries no message")
			}
			if retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", retryable, tc.retryable)
			}
		})
	}
}
