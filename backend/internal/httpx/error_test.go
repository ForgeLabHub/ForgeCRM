package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeEnvelope renders err through WriteError and decodes the result, so the
// assertions below are made against the bytes a client actually receives rather
// than against the struct we happen to have built.
func decodeEnvelope(t *testing.T, r *http.Request, err error) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	WriteError(rec, r, err)

	var body map[string]any
	if decErr := json.Unmarshal(rec.Body.Bytes(), &body); decErr != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", decErr, rec.Body.String())
	}
	return rec.Code, body
}

// requestWithPath builds a request carrying a correlation ID, set through the
// same context key the middleware uses so the test exercises the real lookup.
func requestWithPath(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := context.WithValue(r.Context(), requestCtxKey{}, "req-abc-123")
	return r.WithContext(ctx)
}

func TestWriteErrorCarriesRFC9457Members(t *testing.T) {
	status, body := decodeEnvelope(t, requestWithPath("/api/v1/contacts/42"), ErrNotFound)

	if status != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", status, http.StatusNotFound)
	}
	for _, member := range []string{"type", "title", "status", "detail"} {
		if v, ok := body[member]; !ok || v == "" {
			t.Errorf("RFC 9457 member %q is missing or empty: %#v", member, body[member])
		}
	}
	if got := body["type"]; got != "urn:forgecrm:error:not_found" {
		t.Errorf("type: got %v, want urn:forgecrm:error:not_found", got)
	}
	if got := body["title"]; got != "Resource not found" {
		t.Errorf("title: got %v", got)
	}
	if got := body["instance"]; got != "/api/v1/contacts/42" {
		t.Errorf("instance: got %v, want the request path", got)
	}
	// status must be a number, not a string: RFC 9457 defines it as a number.
	if _, ok := body["status"].(float64); !ok {
		t.Errorf("status should be a JSON number, got %T", body["status"])
	}
}

// The original envelope must survive, because the frontend and every existing
// caller read error.code and error.details from it.
func TestWriteErrorKeepsLegacyEnvelope(t *testing.T) {
	_, body := decodeEnvelope(t, requestWithPath("/api/v1/contacts"), ValidationError(map[string]string{
		"name": "is required",
	}))

	nested, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error envelope missing: %#v", body)
	}
	if nested["code"] != "validation_failed" {
		t.Errorf("error.code: got %v", nested["code"])
	}
	if nested["message"] != "validation failed" {
		t.Errorf("error.message: got %v", nested["message"])
	}
	details, ok := nested["details"].(map[string]any)
	if !ok || details["name"] != "is required" {
		t.Errorf("error.details: got %#v", nested["details"])
	}
}

// The top-level members and the nested envelope are two views of one value, so
// they must not drift apart.
func TestWriteErrorTopLevelAgreesWithNestedEnvelope(t *testing.T) {
	for _, err := range []*APIError{
		ErrNotFound, ErrForbidden, ErrUnauth, ErrConflict, ErrInternal,
		ErrDisabled, ErrBadRequest,
		NewError(http.StatusTooManyRequests, CodeRateLimited, "slow down"),
	} {
		t.Run(string(err.Code), func(t *testing.T) {
			status, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), err)
			nested := body["error"].(map[string]any)

			if int(body["status"].(float64)) != status {
				t.Errorf("top-level status %v disagrees with HTTP status %d", body["status"], status)
			}
			if int(body["status"].(float64)) != err.Status {
				t.Errorf("top-level status %v disagrees with the APIError status %d", body["status"], err.Status)
			}
			if body["detail"] != nested["message"] {
				t.Errorf("detail %v disagrees with error.message %v", body["detail"], nested["message"])
			}
			if body["type"] != "urn:forgecrm:error:"+nested["code"].(string) {
				t.Errorf("type %v disagrees with error.code %v", body["type"], nested["code"])
			}
		})
	}
}

func TestWriteErrorReportsRetryability(t *testing.T) {
	// Retryable means "resending this request unchanged could work". The point
	// of the test is that the answer is not simply "status >= 500": a 500 from a
	// defect is not worth retrying, while a 503 is.
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"429 is retryable", NewError(http.StatusTooManyRequests, CodeRateLimited, "slow down"), true},
		{"503 is retryable", ErrDisabled, true},
		{"502 is retryable", NewError(http.StatusBadGateway, CodeInternal, "bad gateway"), true},
		{"504 is retryable", NewError(http.StatusGatewayTimeout, CodeInternal, "timeout"), true},
		{"500 is not retryable", ErrInternal, false},
		{"400 is not retryable", ErrBadRequest, false},
		{"403 is not retryable", ErrForbidden, false},
		{"404 is not retryable", ErrNotFound, false},
		{"422 is not retryable", ValidationError(map[string]string{"a": "b"}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), tc.err)
			if got, _ := body["retryable"].(bool); got != tc.want {
				t.Errorf("retryable: got %v, want %v", got, tc.want)
			}
		})
	}
}

// An unknown error must never reach the client as itself: that is how SQL, file
// paths and internal hostnames escape into error responses.
func TestWriteErrorNeverLeaksInternals(t *testing.T) {
	secret := "pq: relation \"users\" does not exist at /srv/forgecrm/internal/db.go:42 host=10.0.3.7"
	_, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), errors.New(secret))

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	// Needles are specific strings, not bare words: "relation" would also match
	// the correlation_id member that the envelope is supposed to contain.
	for _, needle := range []string{"pq:", "db.go", "10.0.3.7", `relation "users"`, "/srv/forgecrm"} {
		if strings.Contains(string(raw), needle) {
			t.Errorf("response leaked internal detail %q: %s", needle, raw)
		}
	}
	if body["error"].(map[string]any)["code"] != "internal_error" {
		t.Errorf("an unknown error should be reported as internal_error, got %v", body["error"])
	}
	if body["detail"] != "internal error" {
		t.Errorf("detail should be the generic message, got %v", body["detail"])
	}
}

// A typed nil wrapped in an error interface is a real hazard: errors.As succeeds,
// leaving apiErr nil, and the next field access panics inside the error path.
func TestWriteErrorHandlesTypedNil(t *testing.T) {
	var nilAPIErr *APIError
	status, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), nilAPIErr)
	if status != http.StatusInternalServerError {
		t.Errorf("a typed-nil APIError should become 500, got %d", status)
	}
	if body["error"].(map[string]any)["code"] != "internal_error" {
		t.Errorf("expected internal_error, got %v", body["error"])
	}
}

func TestWriteErrorWithoutRequestContext(t *testing.T) {
	// Callers such as middleware may not have a request to hand; the envelope
	// must still be well-formed rather than panicking on a nil request.
	rec := httptest.NewRecorder()
	WriteError(rec, nil, ErrNotFound)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if body["title"] != "Resource not found" {
		t.Errorf("title: got %v", body["title"])
	}
	if _, present := body["request_id"]; present {
		t.Errorf("request_id should be omitted when there is no request context: %#v", body["request_id"])
	}
}

// The request identifier must appear under both names: request_id is what the
// existing frontend reads, correlation_id is what the log pipeline uses.
func TestWriteErrorCarriesCorrelationID(t *testing.T) {
	_, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), ErrNotFound)
	if body["request_id"] != "req-abc-123" {
		t.Errorf("request_id: got %v", body["request_id"])
	}
	if body["correlation_id"] != "req-abc-123" {
		t.Errorf("correlation_id: got %v", body["correlation_id"])
	}
}

// The query string can carry tokens and personal data, so it must not be echoed
// into instance, which clients send to error trackers.
func TestWriteErrorInstanceOmitsQueryString(t *testing.T) {
	_, body := decodeEnvelope(t, requestWithPath("/api/v1/search?token=super-secret-value"), ErrNotFound)
	if got := body["instance"]; got != "/api/v1/search" {
		t.Errorf("instance should drop the query string, got %v", got)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "super-secret-value") {
		t.Errorf("the query string leaked into the response: %s", raw)
	}
}

// Every code in the catalogue must produce a non-empty title, so that adding a
// code without a title entry degrades to the status phrase rather than to "".
func TestEveryErrorCodeHasATitle(t *testing.T) {
	for _, code := range []ErrorCode{
		CodeBadRequest, CodeValidation, CodeUnauthorized, CodeForbidden,
		CodeNotFound, CodeConflict, CodeRateLimited, CodePayloadTooLarge,
		CodeUnsupported, CodeInternal, CodeDisabled, CodeMFARequired,
	} {
		t.Run(string(code), func(t *testing.T) {
			_, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), NewError(400, code, "x"))
			if title, _ := body["title"].(string); title == "" {
				t.Errorf("code %q has no title", code)
			}
		})
	}

	// An unlisted code must fall back rather than produce an empty title.
	_, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), NewError(http.StatusTeapot, "made_up_code", "x"))
	if title, _ := body["title"].(string); title == "" {
		t.Errorf("an unlisted code should fall back to the status phrase, got %q", title)
	}
}

func TestWriteErrorDefaultsStatusWhenUnset(t *testing.T) {
	// A handler that constructs APIError by hand may leave Status zero. That must
	// not become an HTTP 0, which would panic the writer.
	status, body := decodeEnvelope(t, requestWithPath("/api/v1/x"), &APIError{Code: CodeInternal, Message: "boom"})
	if status != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", status)
	}
	if body["status"].(float64) != float64(http.StatusInternalServerError) {
		t.Errorf("body status: got %v", body["status"])
	}
}
