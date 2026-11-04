package notify

import (
	"context"
	"errors"
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
