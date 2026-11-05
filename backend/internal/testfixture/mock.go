package testfixture

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// RecordedRequest is one request an outbound client made.
type RecordedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

// HTTPTransport is a scriptable http.RoundTripper that records every request and
// replies with a caller-supplied response.
//
// It stands in for a third-party HTTP API — Telegram's Bot API is the one this
// repository talks to — so a test can assert what was sent and force a status
// code without reaching the network. Tests used to each define their own
// RoundTripper; this one records uniformly so a test asserts on a stable shape.
//
// It is safe for concurrent use, because the worker and the API can both deliver
// through it at once.
type HTTPTransport struct {
	mu       sync.Mutex
	requests []RecordedRequest

	// Respond, when set, returns the status and body for a request. When nil, every
	// request succeeds with 200 and an empty body.
	Respond func(r *http.Request, body []byte) (status int, response string)
}

// RoundTrip implements http.RoundTripper.
func (t *HTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		_ = r.Body.Close()
	}

	t.mu.Lock()
	t.requests = append(t.requests, RecordedRequest{
		Method: r.Method,
		URL:    r.URL.String(),
		Header: r.Header.Clone(),
		Body:   body,
	})
	t.mu.Unlock()

	status := http.StatusOK
	response := ""
	if t.Respond != nil {
		status, response = t.Respond(r, body)
	}

	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(response)),
		Request:    r,
	}, nil
}

// Requests returns a copy of every request recorded so far.
func (t *HTTPTransport) Requests() []RecordedRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]RecordedRequest, len(t.requests))
	copy(out, t.requests)
	return out
}

// Count returns how many requests reached the transport.
func (t *HTTPTransport) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

// LastBody returns the body of the most recent request, or nil when none was made.
func (t *HTTPTransport) LastBody() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.requests) == 0 {
		return nil
	}
	return t.requests[len(t.requests)-1].Body
}

// RequestedPaths returns the path of every recorded request, in order.
func (t *HTTPTransport) RequestedPaths() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.requests))
	for _, r := range t.requests {
		out = append(out, r.URL)
	}
	return out
}

// BodyContains reports whether the most recent request body contains every
// needle. It reads the way an assertion should: the test names what it expects to
// have been sent.
func (t *HTTPTransport) BodyContains(needles ...string) bool {
	last := t.LastBody()
	for _, n := range needles {
		if !bytes.Contains(last, []byte(n)) {
			return false
		}
	}
	return true
}
