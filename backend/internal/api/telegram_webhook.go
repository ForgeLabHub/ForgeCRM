package api

import (
	"io"
	"net/http"

	"github.com/geff1778/forgecrm/internal/telegram"
)

// handleTelegramWebhook is the inbound webhook entry point. It authenticates the
// caller with Telegram's secret header, validates the update envelope, then hands
// the update to the shared dispatcher, which is the same code path the standalone
// long-poll service uses. Keeping one dispatcher means the webhook and the poller
// cannot drift apart in authorization, consent or idempotency behaviour.
func (s *Server) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	dispatcher := telegram.NewDispatcher(s.deps.DB, s.deps.Telegram, s.deps.CRM, s.deps.Keyring, s.deps.Log)
	if s.deps.Telegram == nil || !s.deps.Telegram.WebhookEnabled() || dispatcher == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if !s.deps.Telegram.VerifyWebhookSecret(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	update, ok := telegram.ParseIncoming(body)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// No request body, identifier, code, token or upstream response is logged.
	if err := dispatcher.ProcessUpdate(r.Context(), update); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if err := dispatcher.DeliverReply(r.Context(), *update.UpdateID); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
