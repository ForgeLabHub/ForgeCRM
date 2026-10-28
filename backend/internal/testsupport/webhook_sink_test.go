//go:build integration

package testsupport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/secrets"
)

// sinkRequest is one request received by the test endpoint.
type sinkRequest struct {
	timestamp string
	signature string // the hex digest, without the "sha256=" prefix
	body      []byte
}

// newWebhookSink starts a real HTTP server standing in for a customer's endpoint.
// The delivery test uses it rather than a mock client, so the POST, its headers
// and its body are exercised end to end.
func newWebhookSink(t *testing.T, out chan sinkRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read webhook body: %v", err)
		}
		out <- sinkRequest{
			timestamp: r.Header.Get("X-Signature-Timestamp"),
			signature: strings.TrimPrefix(r.Header.Get("X-Signature-256"), "sha256="),
			body:      body,
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// verifyWebhookSignature recomputes the signature the way a real receiver would
// before trusting the payload. It uses the same keyring, so the test asserts the
// exact contract rather than a lookalike.
func verifyWebhookSignature(k *secrets.Keyring, secret []byte, req sinkRequest) bool {
	return k.WebhookSign(secret, req.timestamp, req.body) == req.signature
}
