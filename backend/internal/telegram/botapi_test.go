package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockBot is a minimal stand-in for the Telegram Bot API. It records the methods
// it was called with and can be made to fail, so the client's error handling and
// the poll loop's offset advance can be tested without a real bot.
type mockBot struct {
	mu         sync.Mutex
	methods    []string
	updates    []json.RawMessage
	polls      atomic.Int32
	lastOffset atomic.Int64
	failNext   atomic.Bool
	server     *httptest.Server
}

func newMockBot(t *testing.T) *mockBot {
	m := &mockBot{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		m.mu.Lock()
		m.methods = append(m.methods, method)
		m.mu.Unlock()
		if m.failNext.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
			return
		}
		switch method {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":42,"is_bot":true,"username":"forgecrm_test_bot"}}`))
		case "getUpdates":
			m.polls.Add(1)
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var req struct {
				Offset *int64 `json:"offset"`
			}
			_ = json.Unmarshal(body, &req)
			if req.Offset != nil {
				m.lastOffset.Store(*req.Offset)
			}
			m.mu.Lock()
			batch := m.updates
			m.updates = nil
			m.mu.Unlock()
			if batch == nil {
				batch = []json.RawMessage{}
			}
			out, _ := json.Marshal(map[string]any{"ok": true, "result": batch})
			_, _ = w.Write(out)
		case "deleteWebhook", "setMyCommands", "sendMessage", "answerCallbackQuery", "sendChatAction":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"description":"unknown method"}`))
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockBot) base() string { return m.server.URL }

func (m *mockBot) called(method string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.methods {
		if c == method {
			return true
		}
	}
	return false
}

func TestBotAPIGetMe(t *testing.T) {
	m := newMockBot(t)
	api := NewBotAPI("test-token", m.base(), m.server.Client())
	me, err := api.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if me.ID != 42 || me.Username != "forgecrm_test_bot" {
		t.Fatalf("unexpected identity: %+v", me)
	}
}

func TestBotAPIErrorHidesToken(t *testing.T) {
	m := newMockBot(t)
	m.failNext.Store(true)
	api := NewBotAPI("super-secret-token", m.base(), m.server.Client())
	_, err := api.GetMe(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("token leaked in error: %v", err)
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected an APIError with 401, got %v", err)
	}
}

func TestBotAPIGetUpdatesAndAllowedUpdates(t *testing.T) {
	m := newMockBot(t)
	m.updates = []json.RawMessage{json.RawMessage(`{"update_id":5,"message":{"message_id":1,"date":1,"from":{"id":9},"chat":{"id":9,"type":"private"},"text":"/start"}}`)}
	api := NewBotAPI("t", m.base(), m.server.Client())
	updates, err := api.GetUpdates(context.Background(), nil, 0)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
}

func TestParseIncomingValidatesEnvelope(t *testing.T) {
	good := `{"update_id":1,"message":{"message_id":1,"date":1,"from":{"id":9},"chat":{"id":9,"type":"private"},"text":"/start"}}`
	if _, ok := ParseIncoming([]byte(good)); !ok {
		t.Fatal("expected a valid update")
	}
	// Two event objects in one update is not a Telegram update.
	bad := `{"update_id":1,"message":{"message_id":1,"date":1,"from":{"id":9},"chat":{"id":9,"type":"private"},"text":"x"},"callback_query":{"id":"a","from":{"id":9},"message":{"chat":{"id":9,"type":"private"}},"data":"x"}}`
	if _, ok := ParseIncoming([]byte(bad)); ok {
		t.Fatal("expected a two-event update to be rejected")
	}
	if _, ok := ParseIncoming([]byte(`{"update_id":1}`)); ok {
		t.Fatal("expected a bare update_id to be rejected")
	}
	if _, ok := ParseIncoming([]byte(`not json`)); ok {
		t.Fatal("expected invalid JSON to be rejected")
	}
}

func TestRetryAfterParsing(t *testing.T) {
	e := &APIError{Method: "sendMessage", StatusCode: http.StatusTooManyRequests, Description: "Too Many Requests: retry after 7"}
	if e.RetryAfter() != 7*time.Second {
		t.Fatalf("expected 7s, got %v", e.RetryAfter())
	}
	if !e.Retryable() {
		t.Fatal("429 must be retryable")
	}
	perm := &APIError{StatusCode: http.StatusBadRequest}
	if perm.Retryable() {
		t.Fatal("400 must not be retryable")
	}
}

// asAPIError is errors.As specialised to avoid importing errors in every test.
func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

var _ = io.Discard

// discardLogger returns a logger that drops everything, for tests that do not
// assert on log output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeProcessor records the updates the poll loop hands it.
type fakeProcessor struct {
	mu        sync.Mutex
	processed []int64
	delivered []int64
}

func (f *fakeProcessor) ProcessUpdate(_ context.Context, update Incoming) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if update.UpdateID != nil {
		f.processed = append(f.processed, *update.UpdateID)
	}
	return nil
}

func (f *fakeProcessor) DeliverReply(_ context.Context, updateID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivered = append(f.delivered, updateID)
	return nil
}

func (f *fakeProcessor) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.processed)
}

func TestServicePollLoopAdvancesOffset(t *testing.T) {
	m := newMockBot(t)
	m.updates = []json.RawMessage{
		json.RawMessage(`{"update_id":10,"message":{"message_id":1,"date":1,"from":{"id":9},"chat":{"id":9,"type":"private"},"text":"/start"}}`),
		json.RawMessage(`{"update_id":11,"message":{"message_id":2,"date":1,"from":{"id":9},"chat":{"id":9,"type":"private"},"text":"/help"}}`),
	}
	api := NewBotAPI("t", m.base(), m.server.Client())
	proc := &fakeProcessor{}
	// A telegram service is only constructible with an enabled transport, so the
	// poll loop is exercised directly rather than through NewService.
	svc := &Service{bot: api, dispatcher: proc, log: discardLogger(), opts: Options{PollTimeout: time.Second, PollInterval: 5 * time.Millisecond}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.pollLoop(ctx); close(done) }()

	deadline := time.After(3 * time.Second)
	for proc.count() < 2 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("poll loop did not process both updates (got %d)", proc.count())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if got := m.lastOffset.Load(); got != 11 {
		t.Fatalf("expected the offset to advance to 11, got %d", got)
	}
	if len(proc.delivered) != 2 {
		t.Fatalf("expected 2 replies delivered, got %d", len(proc.delivered))
	}
	if !m.called("deleteWebhook") && !m.called("getUpdates") {
		t.Fatal("expected the poll loop to call the Bot API")
	}
}

func TestServicePollLoopStopsOnPermanentError(t *testing.T) {
	m := newMockBot(t)
	m.failNext.Store(true)
	api := NewBotAPI("t", m.base(), m.server.Client())
	svc := &Service{bot: api, dispatcher: &fakeProcessor{}, log: discardLogger(), opts: Options{PollTimeout: time.Second, PollInterval: 5 * time.Millisecond}}
	done := make(chan struct{})
	go func() { svc.pollLoop(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("poll loop did not stop on a permanent error")
	}
}
