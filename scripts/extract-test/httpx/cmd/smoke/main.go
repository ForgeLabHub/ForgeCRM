// Command smoke exercises the httpx public API in a clean module.
//
// It stands in for a real caller of the package: it writes and reads a JSON
// response, drives a rate-limited handler through the limiter, resolves a client
// address, and renders a sort clause from an allowlist. If any of that needed the
// ForgeCRM monolith, this program would not compile or run here.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

func main() {
	// A successful response round-trips through the same writer the API uses.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/items?limit=2&offset=1&sort=-name", nil)
	page := httpx.ParsePagination(req)
	sort := httpx.ParseSort(req, map[string]string{"name": "accounts.name"}, httpx.SortSpec{})
	httpx.WriteJSON(rec, httpx.NewListResponse([]string{"a", "b"}, 2, page))

	var decoded struct {
		Items []string `json:"items"`
		Total int64    `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		fail("decode response: %v", err)
	}
	if len(decoded.Items) != 2 || decoded.Total != 2 {
		fail("list response did not round-trip: %+v", decoded)
	}
	if page.Limit != 2 || page.Offset != 1 {
		fail("pagination not parsed: %+v", page)
	}
	if sort.Column != "accounts.name" || !sort.Desc {
		fail("sort not parsed: %+v", sort)
	}
	if clause := sort.OrderClause("id"); clause != "accounts.name DESC" {
		fail("order clause = %q", clause)
	}
	fmt.Printf("json roundtrip=1 limit=%d offset=%d order=%q\n",
		page.Limit, page.Offset, sort.OrderClause("id"))

	// A rate-limited handler: the first request is allowed and carries headers,
	// the second, over the budget, is refused with 429.
	limiter, err := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.Policy{
		Name: "smoke", Limit: 1, Window: time.Minute, FailMode: ratelimit.FailOpen,
	})
	if err != nil {
		fail("build limiter: %v", err)
	}
	handler := httpx.RateLimitMiddleware(httpx.RateLimitOptions{
		Limiter: limiter,
		Subject: func(*http.Request) string { return "smoke-subject" },
		Log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, map[string]string{"ok": "true"})
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusOK {
		fail("first request status = %d, want 200", first.Code)
	}
	if second.Code != http.StatusTooManyRequests {
		fail("second request status = %d, want 429", second.Code)
	}
	if first.Header().Get("RateLimit-Limit") == "" {
		fail("allowed response carries no RateLimit-Limit header")
	}
	fmt.Printf("ratelimit allowed=%d refused=%d limit_header=%q\n",
		first.Code, second.Code, first.Header().Get("RateLimit-Limit"))

	// Client address resolution: the peer is used when nothing is trusted.
	peer := httptest.NewRequest(http.MethodGet, "/", nil)
	peer.RemoteAddr = "203.0.113.7:5555"
	if got := httpx.ClientIP(peer); got != "203.0.113.7" {
		fail("ClientIP = %q, want the peer address", got)
	}
	fmt.Printf("clientip=%s\n", httpx.ClientIP(peer))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
