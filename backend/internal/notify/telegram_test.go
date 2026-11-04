package notify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, errors.New("request failed for " + r.URL.String())
}

func TestTelegramSendDoesNotExposeTokenOnTransportFailure(t *testing.T) {
	token := "secret-token-for-test"
	svc := NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: token}, nil, nil, nil, nil, nil)
	svc.client = &http.Client{Transport: failingTransport{}}
	err := svc.sendMessage(context.Background(), 123, "test")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("bot token leaked in error: %v", err)
	}
}

func TestTelegramSendDoesNotExposeTokenOnInvalidURL(t *testing.T) {
	token := "secret\ntoken"
	svc := NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: token}, nil, nil, nil, nil, nil)
	err := svc.sendMessage(context.Background(), 123, "test")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("bot token leaked in error: %v", err)
	}
}

// The mock transport runs locally; no real Telegram token or network is used.
type responseTransport struct {
	status int
	body   string
}

func (rt responseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: rt.status, Body: io.NopCloser(strings.NewReader(rt.body)), Header: make(http.Header)}, nil
}

func TestTelegramSendChecksAPIResult(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"accepted", 200, `{"ok":true}`, false},
		{"api rejects", 200, `{"ok":false,"description":"sensitive"}`, true},
		{"malformed", 200, `not-json`, true},
		{"rate limited", 429, `{"ok":false}`, true},
		{"server error", 500, `{"ok":false}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewTelegramService(&config.TelegramConfig{Enabled: true, BotToken: "test-only"}, nil, nil, nil, nil, nil)
			svc.client = &http.Client{Transport: responseTransport{status: tc.status, body: tc.body}}
			err := svc.sendMessage(context.Background(), 123, "private message")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "test-only")) {
				t.Fatal("sensitive data exposed in error")
			}
		})
	}
}
