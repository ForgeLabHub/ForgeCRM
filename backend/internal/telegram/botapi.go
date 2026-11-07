package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultAPIBase is the public Telegram Bot API root. It is a variable so a test
// can point the client at a local mock without changing production behaviour.
var DefaultAPIBase = "https://api.telegram.org"

// BotAPI is a minimal, token-safe client for the Telegram Bot API.
//
// It owns the token and the base URL, and it never returns or logs the request
// URL (which carries the token) or the raw response body. Callers receive typed
// results or a bounded error, so a token can never leak through a wrapped error.
type BotAPI struct {
	token  string
	base   string
	client *http.Client
}

// NewBotAPI builds a client. A blank base falls back to DefaultAPIBase.
func NewBotAPI(token, base string, client *http.Client) *BotAPI {
	if base == "" {
		base = DefaultAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &BotAPI{token: token, base: strings.TrimRight(base, "/"), client: client}
}

// apiResponse is the common envelope of every Bot API reply.
type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
	Result      T      `json:"result"`
}

// APIError carries the HTTP status and Telegram error code without the token.
type APIError struct {
	Method      string
	StatusCode  int
	Code        int
	Description string
}

func (e *APIError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("telegram %s failed: HTTP %d, code %d: %s", e.Method, e.StatusCode, e.Code, e.Description)
	}
	return fmt.Sprintf("telegram %s failed: HTTP %d", e.Method, e.StatusCode)
}

// Retryable reports whether the caller should back off and retry. A 429 or a 5xx
// is transient; a 4xx is a permanent request error.
func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// RetryAfter returns the delay Telegram asked for on a 429, if any. The
// description carries "retry after N"; a 0 means the caller should use its own
// backoff.
func (e *APIError) RetryAfter() time.Duration {
	if e.StatusCode != http.StatusTooManyRequests {
		return 0
	}
	return parseRetryAfter(e.Description)
}

// call posts one method with a JSON body and decodes the typed result. The token
// never appears in a returned error.
func call[T any](ctx context.Context, b *BotAPI, method string, payload any) (T, error) {
	var zero T
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return zero, errors.New("telegram request could not be encoded")
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := fmt.Sprintf("%s/bot%s/%s", b.base, b.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return zero, errors.New("telegram request is invalid")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		// net/http errors can contain the URL, including the token.
		return zero, errors.New("telegram request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return zero, errors.New("telegram response could not be read")
	}
	var decoded apiResponse[T]
	if err := json.Unmarshal(raw, &decoded); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return zero, &APIError{Method: method, StatusCode: resp.StatusCode}
		}
		return zero, errors.New("telegram response was not valid JSON")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !decoded.OK {
		return zero, &APIError{Method: method, StatusCode: resp.StatusCode, Code: decoded.ErrorCode, Description: decoded.Description}
	}
	return decoded.Result, nil
}

// GetMe returns the bot's identity. It is the startup probe: a bad token fails here.
type BotUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

func (b *BotAPI) GetMe(ctx context.Context) (BotUser, error) {
	return call[BotUser](ctx, b, "getMe", nil)
}

// Update is one raw update from getUpdates. Its fields are decoded lazily by the
// dispatcher, which validates the envelope.
type rawUpdate = json.RawMessage

// GetUpdates long-polls for updates. A zero timeout returns immediately; a
// positive timeout holds the connection open until an update arrives, which is
// what makes long polling efficient. AllowedUpdates is restricted to the two
// update types the bot acts on, so Telegram does not buffer the rest.
type getUpdatesRequest struct {
	Offset         *int64   `json:"offset,omitempty"`
	Timeout        int      `json:"timeout,omitempty"`
	AllowedUpdates []string `json:"allowed_updates"`
}

func (b *BotAPI) GetUpdates(ctx context.Context, offset *int64, timeout time.Duration) ([]json.RawMessage, error) {
	req := getUpdatesRequest{
		Offset:         offset,
		Timeout:        int(timeout / time.Second),
		AllowedUpdates: []string{"message", "callback_query"},
	}
	return call[[]json.RawMessage](ctx, b, "getUpdates", req)
}

// DeleteWebhook removes any previously registered webhook. A bot cannot use
// getUpdates while a webhook is set, so the poller clears it at startup. It is
// idempotent: "webhook is not set" is treated as success.
func (b *BotAPI) DeleteWebhook(ctx context.Context, dropPending bool) error {
	_, err := call[bool](ctx, b, "deleteWebhook", map[string]any{"drop_pending_updates": dropPending})
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Description != "" && strings.Contains(strings.ToLower(apiErr.Description), "not set") {
			return nil
		}
		return err
	}
	return nil
}

// SetWebhook registers a public webhook URL. It exists so an operator can switch
// the bot to webhook mode from the same binary, but the service defaults to
// polling because that needs no inbound exposure.
func (b *BotAPI) SetWebhook(ctx context.Context, url, secret string, allowedUpdates []string) error {
	payload := map[string]any{"url": url, "allowed_updates": allowedUpdates}
	if secret != "" {
		payload["secret_token"] = secret
	}
	_, err := call[bool](ctx, b, "setWebhook", payload)
	return err
}

// BotCommand is one entry of the command menu Telegram shows.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommands publishes the command menu. It is best effort: a failure must not
// stop the service from running.
func (b *BotAPI) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	_, err := call[bool](ctx, b, "setMyCommands", map[string]any{"commands": commands})
	return err
}

// SendMessage posts a text message and returns its message id.
func (b *BotAPI) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	result, err := call[struct {
		MessageID int64 `json:"message_id"`
	}](ctx, b, "sendMessage", map[string]any{"chat_id": chatID, "text": text})
	if err != nil {
		return 0, err
	}
	return result.MessageID, nil
}

// sendChatAction shows a typing indicator, so a slow command does not look frozen.
func (b *BotAPI) SendChatAction(ctx context.Context, chatID int64, action string) error {
	_, err := call[bool](ctx, b, "sendChatAction", map[string]any{"chat_id": chatID, "action": action})
	return err
}

// parseRetryAfter reads a "retry after N" hint from a 429 description.
func parseRetryAfter(description string) time.Duration {
	fields := strings.Fields(strings.ToLower(description))
	for i, f := range fields {
		if f == "after" && i+1 < len(fields) {
			if n, err := strconv.Atoi(fields[i+1]); err == nil && n > 0 {
				return time.Duration(n) * time.Second
			}
		}
	}
	return 0
}
