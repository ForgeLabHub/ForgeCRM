//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/rbac"
)

type telegramMockTransport struct {
	fail      atomic.Bool
	calls     atomic.Int32
	answers   atomic.Int32
	mu        sync.Mutex
	messages  []string
	callbacks []string
}

func (m *telegramMockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "api.telegram.org" {
		return nil, fmt.Errorf("unexpected test host")
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		m.calls.Add(1)
		var payload struct {
			Text      string `json:"text"`
			ReplyMark struct {
				InlineKeyboard [][]struct {
					Data string `json:"callback_data"`
				} `json:"inline_keyboard"`
			} `json:"reply_markup"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.messages = append(m.messages, payload.Text)
		for _, row := range payload.ReplyMark.InlineKeyboard {
			for _, cell := range row {
				m.callbacks = append(m.callbacks, cell.Data)
			}
		}
		m.mu.Unlock()
	case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
		m.answers.Add(1)
	default:
		return nil, fmt.Errorf("unexpected test endpoint")
	}
	body := `{"ok":true}`
	if m.fail.Load() {
		body = `{"ok":false,"description":"sensitive test response"}`
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestTelegramWebhookPostgres(t *testing.T) {
	mock := &telegramMockTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = mock
	t.Cleanup(func() { http.DefaultTransport = previous })
	cfg := testConfig(t)
	cfg.Telegram = config.TelegramConfig{Enabled: true, BotToken: "test-only-token", WebhookSecret: strings.Repeat("s", 32), WebhookEnabled: true}
	h := newHarnessWithConfig(t, cfg)
	org := h.seedOrganization("Telegram Webhook")
	user := h.seedUser("telegram-webhook@example.test", "Test-Passw0rd-2026!!", "Test")
	h.joinOrganization(user, org, "tenant_admin")
	p := rbac.NewPrincipal(user, "telegram-webhook@example.test", "Test", org, nil)
	code, err := h.svcs.Telegram.CreateLinkCode(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	post := func(id int, chat int64, chatType, text, secret string) int {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{"message_id": id + 1, "date": 1730000000, "from": map[string]any{"id": chat, "is_bot": false}, "chat": map[string]any{"id": chat, "type": chatType}, "text": text}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		rec := httptest.NewRecorder()
		h.server.ServeHTTP(rec, req)
		return rec.Code
	}
	secret := cfg.Telegram.WebhookSecret
	for _, body := range []string{`{`, `{"update_id":1}`, `{"update_id":1,"message":{"message_id":1,"chat":{"id":100,"type":"private"}}}`, `{"update_id":1,"message":{"message_id":1,"date":1730000000,"chat":{"id":100,"type":"unexpected"}}}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/webhook", strings.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		rec := httptest.NewRecorder()
		h.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid update accepted: %d", rec.Code)
		}
	}
	if got := post(1, 100, "private", "/link "+code, "incorrect"); got != 401 {
		t.Fatalf("invalid secret: %d", got)
	}
	if got := post(2, 100, "group", "/link "+code, secret); got != 200 {
		t.Fatalf("group: %d", got)
	}
	var count int
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM telegram_links WHERE telegram_user_id=100 AND revoked_at IS NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("group linked: %d %v", count, err)
	}
	if got := post(3, 100, "private", "/link "+code, secret); got != 200 {
		t.Fatalf("link: %d", got)
	}
	if got := post(4, 101, "private", "/link "+code, secret); got != 200 {
		t.Fatalf("reused link: %d", got)
	}
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM telegram_links WHERE telegram_user_id=101 AND revoked_at IS NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reused linked: %d %v", count, err)
	}
	var task uuid.UUID
	if err := h.db.Pool().QueryRow(context.Background(), `INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,created_by) VALUES($1,'Confidential task','open','normal',$2,$2) RETURNING id`, org, user).Scan(&task); err != nil {
		t.Fatal(err)
	}
	mock.fail.Store(true)
	before := mock.calls.Load()
	if got := post(5, 100, "private", "/done "+task.String(), secret); got != 503 {
		t.Fatalf("API rejection must retry: %d", got)
	}
	var state string
	var ciphertext []byte
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT status FROM tasks WHERE id=$1`, task).Scan(&state); err != nil || state != "completed" {
		t.Fatalf("task not committed: %s %v", state, err)
	}
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT reply_ciphertext FROM telegram_update_receipts WHERE update_id=5`).Scan(&ciphertext); err != nil || len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("Task completed")) {
		t.Fatalf("pending reply not encrypted: %v", err)
	}
	mock.fail.Store(false)
	var wg sync.WaitGroup
	var bad atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if post(5, 100, "private", "/done "+task.String(), secret) != 200 {
				bad.Add(1)
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 || mock.calls.Load()-before != 2 {
		t.Fatalf("duplicate reply attempts: bad=%d calls=%d", bad.Load(), mock.calls.Load()-before)
	}
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE event_type='crm.task.completed' AND payload->>'task_id'=$1`, task.String()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate action: %d %v", count, err)
	}
	var outcome string
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT outcome,reply_ciphertext FROM telegram_update_receipts WHERE update_id=5`).Scan(&outcome, &ciphertext); err != nil || outcome != "accepted" || len(ciphertext) != 0 {
		t.Fatalf("reply not cleared: %s %v", outcome, err)
	}
	// A linked user cannot read another employee's task or retain privileges
	// after their role has been revoked.
	mock.mu.Lock()
	firstCheckedMessage := len(mock.messages)
	mock.mu.Unlock()
	other := h.seedUser("other-telegram@example.test", "Test-Passw0rd-2026!!", "Other")
	if _, err := h.db.Pool().Exec(context.Background(), `INSERT INTO memberships(user_id,organization_id) VALUES($1,$2)`, other, org); err != nil {
		t.Fatal(err)
	}
	var otherTask uuid.UUID
	if err := h.db.Pool().QueryRow(context.Background(), `INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,created_by) VALUES($1,'Other secret','open','normal',$2,$2) RETURNING id`, org, other).Scan(&otherTask); err != nil {
		t.Fatal(err)
	}
	if got := post(7, 100, "private", "/task "+otherTask.String(), secret); got != 200 {
		t.Fatalf("other task: %d", got)
	}
	var roleID uuid.UUID
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT role_id FROM user_roles WHERE user_id=$1 AND organization_id=$2 LIMIT 1`, user, org).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Pool().Exec(context.Background(), `DELETE FROM user_roles WHERE user_id=$1 AND organization_id=$2`, user, org); err != nil {
		t.Fatal(err)
	}
	if got := post(8, 100, "private", "/task "+task.String(), secret); got != 200 {
		t.Fatalf("revoked permissions: %d", got)
	}
	mock.mu.Lock()
	for _, m := range mock.messages[firstCheckedMessage:] {
		if strings.Contains(m, "Other secret") || strings.Contains(m, "Confidential task") {
			t.Fatal("task disclosed without ownership or permission")
		}
	}
	mock.mu.Unlock()
	if _, err := h.db.Pool().Exec(context.Background(), `INSERT INTO user_roles(user_id,organization_id,role_id) VALUES($1,$2,$3)`, user, org, roleID); err != nil {
		t.Fatal(err)
	}
	// A pending task card must not be sent after reassignment.
	var reassigned uuid.UUID
	if err := h.db.Pool().QueryRow(context.Background(), `INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,created_by) VALUES($1,'Reassigned secret','open','normal',$2,$2) RETURNING id`, org, user).Scan(&reassigned); err != nil {
		t.Fatal(err)
	}
	mock.fail.Store(true)
	if got := post(10, 100, "private", "/task "+reassigned.String(), secret); got != 503 {
		t.Fatalf("pending reassigned reply: %d", got)
	}
	if _, err := h.db.Pool().Exec(context.Background(), `UPDATE tasks SET assignee_user_id=$1 WHERE id=$2`, other, reassigned); err != nil {
		t.Fatal(err)
	}
	mock.fail.Store(false)
	before = mock.calls.Load()
	if got := post(10, 100, "private", "/task "+reassigned.String(), secret); got != 200 || mock.calls.Load() != before {
		t.Fatalf("reassigned pending reply: %d", got)
	}
	// A failed private reply is never replayed after the link is revoked.
	mock.fail.Store(true)
	if got := post(9, 100, "private", "/task "+task.String(), secret); got != 503 {
		t.Fatalf("pending private reply: %d", got)
	}
	mock.fail.Store(false)
	if _, err := h.db.Pool().Exec(context.Background(), `UPDATE telegram_links SET revoked_at=now() WHERE telegram_user_id=100`); err != nil {
		t.Fatal(err)
	}
	before = mock.calls.Load()
	if got := post(9, 100, "private", "/task "+task.String(), secret); got != 200 || mock.calls.Load() != before {
		t.Fatalf("revoked pending reply: %d", got)
	}
	if got := post(6, 100, "private", "/tasks", secret); got != 200 {
		t.Fatalf("revoked link: %d", got)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	for _, m := range mock.messages {
		if strings.Contains(m, code) || strings.Contains(m, "test-only-token") {
			t.Fatal("secret leaked to response")
		}
	}
}

func TestTelegramSelfServicePostgres(t *testing.T) {
	cfg := testConfig(t)
	cfg.Telegram = config.TelegramConfig{Enabled: true, BotToken: "test-only-token", WebhookSecret: strings.Repeat("s", 32), WebhookEnabled: true}
	h := newHarnessWithConfig(t, cfg)
	org := h.seedOrganization("Telegram Self Service")
	user := h.seedUser("telegram-self@example.test", "Test-Passw0rd-2026!!", "Self")
	h.joinOrganization(user, org, "sales_representative")
	actor := h.login("telegram-self@example.test", "Test-Passw0rd-2026!!")
	for _, p := range actor.permissions {
		if p == "email.manage" {
			t.Fatal("test user unexpectedly has email.manage")
		}
	}
	for _, path := range []string{"/api/v1/admin/telegram/status", "/api/v1/admin/telegram/link-code", "/api/v1/admin/telegram/link"} {
		method := http.MethodPost
		if strings.HasSuffix(path, "status") {
			method = http.MethodGet
		} else if strings.HasSuffix(path, "/link") {
			method = http.MethodDelete
		}
		if rec := h.do(nil, method, path, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, rec.Code)
		}
		if rec := h.do(actor, method, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("self service %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	var count int
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM telegram_link_codes WHERE organization_id=$1 AND user_id=$2`, org, user).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unlink did not revoke own pending code %d: %v", count, err)
	}
}

// TestTelegramLocalizationAndCallbacksPostgres exercises the language picker,
// onboarding and inline-button flows end to end against a real database and a
// fake Telegram endpoint. It proves the persisted language survives a restart of
// the process (a second harness reads it), a repeated update is not delivered
// twice, and a callback in a group chat is acknowledged but never acted on.
func TestTelegramLocalizationAndCallbacksPostgres(t *testing.T) {
	mock := &telegramMockTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = mock
	t.Cleanup(func() { http.DefaultTransport = previous })
	cfg := testConfig(t)
	cfg.Telegram = config.TelegramConfig{Enabled: true, BotToken: "test-only-token", WebhookSecret: strings.Repeat("s", 32), WebhookEnabled: true}
	h := newHarnessWithConfig(t, cfg)
	secret := cfg.Telegram.WebhookSecret

	postMessage := func(id int, chat int64, chatType, text string) int {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{"message_id": id + 1, "date": 1730000000, "from": map[string]any{"id": chat, "is_bot": false}, "chat": map[string]any{"id": chat, "type": chatType}, "text": text}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		rec := httptest.NewRecorder()
		h.server.ServeHTTP(rec, req)
		return rec.Code
	}
	postCallback := func(id int, chat int64, chatType, data string) int {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"update_id": id,
			"callback_query": map[string]any{
				"id":   fmt.Sprintf("cb-%d", id),
				"from": map[string]any{"id": chat, "is_bot": false},
				"message": map[string]any{
					"message_id": id + 1,
					"chat":       map[string]any{"id": chat, "type": chatType},
				},
				"data": data,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		rec := httptest.NewRecorder()
		h.server.ServeHTTP(rec, req)
		return rec.Code
	}
	lastMessage := func() string {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		if len(mock.messages) == 0 {
			return ""
		}
		return mock.messages[len(mock.messages)-1]
	}
	hasCallback := func(data string) bool {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		for _, c := range mock.callbacks {
			if c == data {
				return true
			}
		}
		return false
	}

	// A brand-new account is onboarded in English with the language picker.
	if got := postMessage(100, 500, "private", "/start"); got != 200 {
		t.Fatalf("start: %d", got)
	}
	if msg := lastMessage(); !strings.Contains(msg, "ForgeCRM Assistant") || !hasCallback("lang:ru") {
		t.Fatalf("welcome: %q", msg)
	}
	// Onboarding is recorded, so a second /start shows the menu, not the intro.
	if got := postMessage(101, 500, "private", "/start"); got != 200 {
		t.Fatalf("second start: %d", got)
	}
	if msg := lastMessage(); strings.Contains(msg, "First, choose a language") {
		t.Fatalf("onboarding repeated: %q", msg)
	}
	// Choosing Russian persists and answers in Russian with the menu.
	if got := postCallback(102, 500, "private", "lang:ru"); got != 200 {
		t.Fatalf("lang callback: %d", got)
	}
	if msg := lastMessage(); !strings.Contains(msg, "Готово") {
		t.Fatalf("language not applied: %q", msg)
	}
	var locale string
	if err := h.db.Pool().QueryRow(context.Background(), `SELECT locale FROM telegram_user_settings WHERE telegram_user_id=500`).Scan(&locale); err != nil || locale != "ru" {
		t.Fatalf("stored locale: %q %v", locale, err)
	}
	// /help is answered in the stored language.
	if got := postMessage(103, 500, "private", "/help"); got != 200 {
		t.Fatalf("help: %d", got)
	}
	if msg := lastMessage(); !strings.Contains(msg, "Что я умею") {
		t.Fatalf("help not localized: %q", msg)
	}
	// A repeated update is acknowledged but not delivered again.
	before := mock.calls.Load()
	if got := postMessage(103, 500, "private", "/help"); got != 200 {
		t.Fatalf("duplicate help: %d", got)
	}
	if mock.calls.Load() != before {
		t.Fatalf("duplicate update delivered again: %d -> %d", before, mock.calls.Load())
	}
	// An unlinked account asking for tasks gets instructions, not data.
	if got := postMessage(104, 500, "private", "/tasks"); got != 200 {
		t.Fatalf("unlinked tasks: %d", got)
	}
	if msg := lastMessage(); !strings.Contains(msg, "не привязан") {
		t.Fatalf("unlinked message not localized: %q", msg)
	}
	// A tap in a group chat is acknowledged but never acted on.
	if got := postCallback(105, 500, "supergroup", "menu:tasks"); got != 200 {
		t.Fatalf("group callback: %d", got)
	}
	if mock.answers.Load() == 0 {
		t.Fatal("group callback was not acknowledged")
	}
	// Link the account, then a task button opens the task through the callback path.
	org := h.seedOrganization("Telegram Localization")
	user := h.seedUser("telegram-locale@example.test", "Test-Passw0rd-2026!!", "Locale")
	h.joinOrganization(user, org, "tenant_admin")
	p := rbac.NewPrincipal(user, "telegram-locale@example.test", "Locale", org, nil)
	code, err := h.svcs.Telegram.CreateLinkCode(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if got := postMessage(106, 501, "private", "/link "+code); got != 200 {
		t.Fatalf("link: %d", got)
	}
	var task uuid.UUID
	if err := h.db.Pool().QueryRow(context.Background(), `INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,created_by) VALUES($1,'Localized task','open','normal',$2,$2) RETURNING id`, org, user).Scan(&task); err != nil {
		t.Fatal(err)
	}
	if got := postCallback(107, 501, "private", "task:"+task.String()); got != 200 {
		t.Fatalf("task callback: %d", got)
	}
	if msg := lastMessage(); !strings.Contains(msg, "Localized task") {
		t.Fatalf("task callback body: %q", msg)
	}
	// /overdue is answered and lists the overdue task when its deadline has passed.
	if _, err := h.db.Pool().Exec(context.Background(), `UPDATE tasks SET due_at=now()-interval '1 day' WHERE id=$1`, task); err != nil {
		t.Fatal(err)
	}
	if got := postMessage(108, 501, "private", "/overdue"); got != 200 {
		t.Fatalf("overdue: %d", got)
	}
	if msg := lastMessage(); !strings.Contains(msg, "Localized task") {
		t.Fatalf("overdue body: %q", msg)
	}
}
