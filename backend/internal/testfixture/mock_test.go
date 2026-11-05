package testfixture

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestHTTPTransportRecordsAndResponds proves the transport captures what a client
// sent and returns the scripted status, which is the whole contract a delivery
// test relies on.
func TestHTTPTransportRecordsAndResponds(t *testing.T) {
	tr := &HTTPTransport{Respond: func(*http.Request, []byte) (int, string) {
		return http.StatusTooManyRequests, `{"ok":false}`
	}}
	client := &http.Client{Transport: tr}

	resp, err := client.Post("https://api.telegram.org/botsendMessage", "application/json",
		strings.NewReader(`{"chat_id":42,"text":"hello"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	if tr.Count() != 1 {
		t.Fatalf("recorded %d requests, want 1", tr.Count())
	}
	if !tr.BodyContains(`"chat_id":42`, `"text":"hello"`) {
		t.Errorf("recorded body %q does not contain the sent fields", tr.LastBody())
	}
}

// TestHTTPTransportIsConcurrencySafe proves the recorder can be shared by
// concurrent deliveries, which the worker does. Run with -race, a missing lock
// fails here.
func TestHTTPTransportIsConcurrencySafe(t *testing.T) {
	tr := &HTTPTransport{}
	client := &http.Client{Transport: tr}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get("https://example.test/ping")
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	if tr.Count() != 50 {
		t.Fatalf("recorded %d requests, want 50", tr.Count())
	}
}
