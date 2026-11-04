package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// telegramIncoming accepts only private, human messages as commands. Other
// well-formed Telegram updates are acknowledged but never interpreted.
type telegramIncoming struct {
	UpdateID *int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		Date      int64 `json:"date"`
		From      *struct {
			ID       int64  `json:"id"`
			IsBot    bool   `json:"is_bot"`
			Username string `json:"username"`
		} `json:"from"`
		Chat *struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

type telegramReply struct {
	ChatID        int64
	System        string
	Text          string
	OrgID         *uuid.UUID
	UserID        *uuid.UUID
	NeedsTaskRead bool
	TaskSnapshots []telegramTaskSnapshot
}

type telegramTaskSnapshot struct {
	ID        uuid.UUID
	UpdatedAt time.Time
}

// Capture exactly the tasks whose details were placed in a reply so delivery
// can revalidate them under row locks, including on a later Telegram retry.
type telegramTrackedTasks struct {
	telegramTaskOperations
	snapshots []telegramTaskSnapshot
}

func (t *telegramTrackedTasks) ListMyOpenTasks(ctx context.Context, p *rbac.Principal, from, before *time.Time, limit int) ([]crm.Task, error) {
	items, err := t.telegramTaskOperations.ListMyOpenTasks(ctx, p, from, before, limit)
	for _, item := range items {
		t.snapshots = append(t.snapshots, telegramTaskSnapshot{ID: item.ID, UpdatedAt: item.UpdatedAt})
	}
	return items, err
}
func (t *telegramTrackedTasks) GetMyTask(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*crm.Task, error) {
	item, err := t.telegramTaskOperations.GetMyTask(ctx, p, id)
	if err == nil && item != nil {
		t.snapshots = append(t.snapshots, telegramTaskSnapshot{ID: item.ID, UpdatedAt: item.UpdatedAt})
	}
	return item, err
}

func (s *Server) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if s.deps.Telegram == nil || !s.deps.Telegram.WebhookEnabled() || s.deps.Keyring == nil || s.deps.DB == nil || s.deps.CRM == nil {
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
	var raw map[string]json.RawMessage
	var update telegramIncoming
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil || len(raw) < 2 || json.Unmarshal(body, &update) != nil || update.UpdateID == nil || *update.UpdateID < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// A Telegram update contains exactly one event object in addition to its ID.
	// Unknown event types are accepted for forward compatibility but not executed.
	events := 0
	for key, value := range raw {
		if key == "update_id" {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(value, &obj) != nil || obj == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		events++
	}
	if events != 1 || (update.Message != nil && (update.Message.MessageID <= 0 || update.Message.Date <= 0 || update.Message.Chat == nil || update.Message.Chat.ID == 0 || !telegramChatTypeValid(update.Message.Chat.Type) || (update.Message.From != nil && update.Message.From.ID <= 0))) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// No request body, identifier, code, token or upstream response is logged.
	if err := s.processTelegramUpdate(r.Context(), update); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if err := s.deliverTelegramReply(r.Context(), *update.UpdateID); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func telegramChatTypeValid(kind string) bool {
	switch kind {
	case "private", "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

func (s *Server) processTelegramUpdate(ctx context.Context, update telegramIncoming) error {
	return s.deps.DB.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		tag, err := s.deps.DB.Q(ctx).Exec(ctx, `INSERT INTO telegram_update_receipts(update_id) VALUES($1) ON CONFLICT DO NOTHING`, *update.UpdateID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		m := update.Message
		// Groups, channels, anonymous senders and messages without text can never
		// link an account or invoke a CRM operation.
		if m == nil || m.From == nil || m.Chat == nil || m.MessageID <= 0 || m.From.ID <= 0 || m.From.IsBot || m.Chat.Type != "private" || m.Chat.ID != m.From.ID || strings.TrimSpace(m.Text) == "" {
			return nil
		}
		reply, err := s.telegramCommand(ctx, m.From.ID, m.From.Username, m.Text)
		if err != nil {
			return err
		}
		if reply == nil {
			return nil
		}
		// Encrypt even task titles, never the incoming message or the binding code.
		plain, err := json.Marshal(reply)
		if err != nil {
			return err
		}
		sealed, err := s.deps.Keyring.Encrypt(plain)
		if err != nil {
			return err
		}
		_, err = s.deps.DB.Q(ctx).Exec(ctx, `UPDATE telegram_update_receipts SET reply_ciphertext=$2 WHERE update_id=$1`, *update.UpdateID, sealed)
		return err
	})
}

func (s *Server) telegramCommand(ctx context.Context, chatID int64, username, text string) (*telegramReply, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil, nil
	}
	cmd := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
	system := func(kind string) *telegramReply { return &telegramReply{ChatID: chatID, System: kind} }
	switch cmd {
	case "/start", "/help":
		return system("help"), nil
	case "/link":
		if len(parts) != 2 {
			return system("link_usage"), nil
		}
		_, err := s.deps.Telegram.Link(ctx, notify.LinkRequest{Code: parts[1], TelegramUserID: chatID, Username: username})
		if err != nil {
			var apiErr *httpx.APIError
			if errors.As(err, &apiErr) && (apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusConflict) {
				return system("link_failed"), nil
			}
			return nil, err
		}
		principal, err := s.deps.Telegram.ResolveUser(ctx, chatID)
		if err != nil {
			return nil, err
		}
		return &telegramReply{ChatID: chatID, Text: "Telegram is linked to your ForgeCRM account.", OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/tasks", "/today", "/task", "/done", "/due":
		principal, err := s.deps.Telegram.ResolveUser(ctx, chatID)
		if err != nil {
			var apiErr *httpx.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
				return system("not_linked"), nil
			}
			return nil, err
		}
		tracked := &telegramTrackedTasks{telegramTaskOperations: s.deps.CRM}
		answer, err := handleTelegramTaskCommand(ctx, tracked, principal, text, time.Now())
		needsRead := err == nil
		if err != nil {
			switch {
			case errors.Is(err, rbac.ErrForbidden):
				answer = "You do not have permission for this action."
			case errors.Is(err, crm.ErrNotFound):
				answer = "Task not found."
			case errors.Is(err, crm.ErrConflict):
				answer = "This task cannot be changed."
			default:
				return nil, err
			}
		}
		return &telegramReply{ChatID: chatID, Text: answer, OrgID: &principal.OrganizationID, UserID: &principal.UserID, NeedsTaskRead: needsRead, TaskSnapshots: tracked.snapshots}, nil
	default:
		return system("unknown"), nil
	}
}

// deliverTelegramReply serializes duplicates with a receipt row lock. It clears
// the encrypted response once accepted. A network failure returns non-2xx so
// Telegram can retry, without executing the CRM action again. As with any
// external API, a crash after acceptance but before commit can duplicate a reply.
func (s *Server) deliverTelegramReply(ctx context.Context, updateID int64) error {
	return s.deps.DB.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		var sealed []byte
		err := s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT reply_ciphertext FROM telegram_update_receipts WHERE update_id=$1 FOR UPDATE`, updateID).Scan(&sealed)
		if err != nil {
			return err
		}
		if len(sealed) == 0 {
			return nil
		}
		plain, err := s.deps.Keyring.Decrypt(sealed)
		if err != nil {
			return err
		}
		var reply telegramReply
		if err := json.Unmarshal(plain, &reply); err != nil {
			return err
		}
		if reply.OrgID != nil && reply.UserID != nil {
			if reply.NeedsTaskRead {
				p, err := s.deps.Telegram.ResolveUser(ctx, reply.ChatID)
				if err != nil {
					var apiErr *httpx.APIError
					if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
						err = notify.ErrTelegramRecipientUnavailable
					}
					if !errors.Is(err, notify.ErrTelegramRecipientUnavailable) {
						return err
					}
				} else if p.UserID != *reply.UserID || p.OrganizationID != *reply.OrgID || !p.Has(rbac.PermTasksRead) {
					err = notify.ErrTelegramRecipientUnavailable
				}
				if err != nil {
					return s.finishTelegramReply(ctx, updateID, "unavailable")
				}
			}
			for _, snapshot := range reply.TaskSnapshots {
				var id uuid.UUID
				err := s.deps.DB.Q(ctx).QueryRow(ctx, `SELECT id FROM tasks WHERE id=$1 AND organization_id=$2 AND assignee_user_id=$3 AND archived_at IS NULL AND updated_at=$4 FOR SHARE`, snapshot.ID, *reply.OrgID, *reply.UserID, snapshot.UpdatedAt).Scan(&id)
				if database.IsNoRows(err) {
					return s.finishTelegramReply(ctx, updateID, "unavailable")
				}
				if err != nil {
					return err
				}
			}
			err = s.deps.Telegram.ReplyLinked(ctx, reply.ChatID, *reply.OrgID, *reply.UserID, reply.Text)
		} else {
			err = s.deps.Telegram.ReplySystem(ctx, reply.ChatID, reply.System)
		}
		outcome := "accepted"
		if errors.Is(err, notify.ErrConsentBlocked) {
			outcome = "blocked"
		} else if errors.Is(err, notify.ErrTelegramRecipientUnavailable) {
			outcome = "unavailable"
		} else if err != nil {
			return err
		}
		return s.finishTelegramReply(ctx, updateID, outcome)
	})
}

func (s *Server) finishTelegramReply(ctx context.Context, updateID int64, outcome string) error {
	_, err := s.deps.DB.Q(ctx).Exec(ctx, `UPDATE telegram_update_receipts SET reply_ciphertext=NULL, finished_at=now(), outcome=$2 WHERE update_id=$1`, updateID, outcome)
	return err
}
