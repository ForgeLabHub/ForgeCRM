package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

// telegramIncoming accepts only private, human messages and private button taps as
// commands. Other well-formed Telegram updates are acknowledged but never
// interpreted.
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
	CallbackQuery *struct {
		ID   string `json:"id"`
		From *struct {
			ID       int64  `json:"id"`
			IsBot    bool   `json:"is_bot"`
			Username string `json:"username"`
		} `json:"from"`
		Message *struct {
			Chat *struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
		} `json:"message"`
		Data string `json:"data"`
	} `json:"callback_query"`
}

// telegramReply is the response recorded in the receipt and delivered after the
// transaction commits. It carries localized text and an optional inline keyboard.
type telegramReply struct {
	ChatID        int64
	Text          string
	Buttons       [][]notify.InlineButton
	CallbackID    string
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

func (t *telegramTrackedTasks) ListMyOverdueTasks(ctx context.Context, p *rbac.Principal, now time.Time, limit int) ([]crm.Task, error) {
	items, err := t.telegramTaskOperations.ListMyOverdueTasks(ctx, p, now, limit)
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
	if events != 1 || !telegramEventValid(update) {
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

// telegramEventValid accepts a message or a callback query from a private chat and
// rejects anything malformed. A callback's data is bounded to Telegram's own limit
// so a crafted payload cannot inflate the encrypted reply.
func telegramEventValid(update telegramIncoming) bool {
	if m := update.Message; m != nil {
		return m.MessageID > 0 && m.Date > 0 && m.Chat != nil && m.Chat.ID != 0 &&
			telegramChatTypeValid(m.Chat.Type) && (m.From == nil || m.From.ID > 0)
	}
	if cq := update.CallbackQuery; cq != nil {
		return cq.ID != "" && len(cq.Data) <= 64 && cq.From != nil && cq.From.ID > 0 &&
			cq.Message != nil && cq.Message.Chat != nil && cq.Message.Chat.ID != 0 &&
			telegramChatTypeValid(cq.Message.Chat.Type)
	}
	return false
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
		reply, err := s.telegramEvent(ctx, update)
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

// telegramEvent turns a validated update into a reply, or nil when the update must
// be acknowledged without a response. Only private human senders reach here.
func (s *Server) telegramEvent(ctx context.Context, update telegramIncoming) (*telegramReply, error) {
	if cq := update.CallbackQuery; cq != nil {
		return s.telegramCallback(ctx, cq.ID, cq.From.ID, cq.From.Username, cq.Message.Chat.ID, cq.Message.Chat.Type, cq.Data)
	}
	m := update.Message
	if m == nil || m.From == nil || m.Chat == nil || m.MessageID <= 0 || m.From.ID <= 0 || m.From.IsBot || m.Chat.Type != "private" || m.Chat.ID != m.From.ID || strings.TrimSpace(m.Text) == "" {
		return nil, nil
	}
	return s.telegramCommand(ctx, m.From.ID, m.From.Username, m.Text)
}

// telegramCommand handles a text command. The reply is localized by the sender's
// stored language, which is read before any handler runs so an error message is
// already in the right language.
func (s *Server) telegramCommand(ctx context.Context, chatID int64, username, text string) (*telegramReply, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil, nil
	}
	loc, err := s.telegramLocale(ctx, chatID)
	if err != nil {
		return nil, err
	}
	cmd := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
	reply := func(text string, rows [][]notify.InlineButton) *telegramReply {
		return &telegramReply{ChatID: chatID, Text: text, Buttons: rows}
	}
	system := func(key telegramtext.Key) *telegramReply {
		return reply(telegramtext.T(loc, key), nil)
	}
	switch cmd {
	case "/start":
		settings, err := s.deps.Telegram.GetUserSettings(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if settings.OnboardedAt == nil {
			if err := s.deps.Telegram.MarkOnboarded(ctx, chatID); err != nil {
				return nil, err
			}
			return reply(telegramtext.T(loc, telegramtext.KeyWelcome), languageRows()), nil
		}
		return reply(telegramtext.T(loc, telegramtext.KeyMenuTitle), menuRows(loc)), nil
	case "/help":
		return reply(telegramtext.T(loc, telegramtext.KeyHelp), [][]notify.InlineButton{{{Text: telegramtext.T(loc, telegramtext.BtnMenu), Data: "menu:home"}}}), nil
	case "/menu":
		return reply(telegramtext.T(loc, telegramtext.KeyMenuTitle), menuRows(loc)), nil
	case "/language":
		if len(parts) == 2 {
			tag := strings.ToLower(parts[1])
			if !telegramtext.IsSupported(tag) {
				return reply(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
			}
			next := telegramtext.Normalize(tag)
			if err := s.deps.Telegram.SetUserLocale(ctx, chatID, next); err != nil {
				return nil, err
			}
			if err := s.deps.Telegram.MarkOnboarded(ctx, chatID); err != nil {
				return nil, err
			}
			return reply(telegramtext.T(next, telegramtext.KeyLanguageSet), menuRows(next)), nil
		}
		return reply(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
	case "/link":
		if len(parts) != 2 {
			return system(telegramtext.KeyLinkUsage), nil
		}
		_, err := s.deps.Telegram.Link(ctx, notify.LinkRequest{Code: parts[1], TelegramUserID: chatID, Username: username})
		if err != nil {
			var apiErr *httpx.APIError
			if errors.As(err, &apiErr) && (apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusConflict) {
				return system(telegramtext.KeyLinkFailed), nil
			}
			return nil, err
		}
		principal, err := s.deps.Telegram.ResolveUser(ctx, chatID)
		if err != nil {
			return nil, err
		}
		return &telegramReply{ChatID: chatID, Text: telegramtext.T(loc, telegramtext.KeyLinkSuccess), Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/profile":
		principal, ok, err := s.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		body := fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyProfile), principal.Email, principal.OrganizationName, string(loc))
		return &telegramReply{ChatID: chatID, Text: body, Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/notifications":
		principal, ok, err := s.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		items, err := s.deps.CRM.ListMyNotifications(ctx, principal, true, 5)
		if err != nil {
			return s.telegramTaskError(err, chatID, principal, loc)
		}
		if len(items) == 0 {
			return &telegramReply{ChatID: chatID, Text: telegramtext.T(loc, telegramtext.KeyNoNotifications), Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
		}
		var b strings.Builder
		b.WriteString(telegramtext.T(loc, telegramtext.KeyNotificationsHdr))
		for _, n := range items {
			b.WriteString("\n• " + safeTitle(n.Title))
		}
		return &telegramReply{ChatID: chatID, Text: b.String(), Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/tasks", "/today", "/overdue", "/task", "/done", "/due":
		principal, ok, err := s.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		tracked := &telegramTrackedTasks{telegramTaskOperations: s.deps.CRM}
		answer, rows, err := handleTelegramTaskCommand(ctx, tracked, principal, text, time.Now().UTC(), loc)
		if err != nil {
			return s.telegramTaskError(err, chatID, principal, loc)
		}
		return &telegramReply{ChatID: chatID, Text: answer, Buttons: rows, OrgID: &principal.OrganizationID, UserID: &principal.UserID, NeedsTaskRead: true, TaskSnapshots: tracked.snapshots}, nil
	default:
		return system(telegramtext.KeyUnknownCommand), nil
	}
}

// telegramCallback handles a button tap. Data is a short token the bot itself
// produced, so it is matched exactly rather than parsed loosely.
func (s *Server) telegramCallback(ctx context.Context, callbackID string, fromID int64, username string, chatID int64, chatType, data string) (*telegramReply, error) {
	if chatType != "private" || chatID != fromID {
		// A tap in a group chat is acknowledged but never acted on, because the
		// identity of the tapper and the destination would differ.
		return &telegramReply{ChatID: chatID, CallbackID: callbackID}, nil
	}
	loc, err := s.telegramLocale(ctx, fromID)
	if err != nil {
		return nil, err
	}
	reply := func(text string, rows [][]notify.InlineButton) *telegramReply {
		return &telegramReply{ChatID: chatID, Text: text, Buttons: rows, CallbackID: callbackID}
	}
	switch {
	case strings.HasPrefix(data, "lang:"):
		tag := strings.TrimPrefix(data, "lang:")
		if !telegramtext.IsSupported(tag) {
			return reply(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
		}
		next := telegramtext.Normalize(tag)
		if err := s.deps.Telegram.SetUserLocale(ctx, fromID, next); err != nil {
			return nil, err
		}
		if err := s.deps.Telegram.MarkOnboarded(ctx, fromID); err != nil {
			return nil, err
		}
		return reply(telegramtext.T(next, telegramtext.KeyLanguageSet), menuRows(next)), nil
	case data == "menu:home":
		return reply(telegramtext.T(loc, telegramtext.KeyMenuTitle), menuRows(loc)), nil
	case data == "menu:help":
		return reply(telegramtext.T(loc, telegramtext.KeyHelp), [][]notify.InlineButton{{{Text: telegramtext.T(loc, telegramtext.BtnMenu), Data: "menu:home"}}}), nil
	case data == "menu:language":
		return reply(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
	case data == "menu:tasks", data == "menu:overdue", data == "menu:profile", data == "menu:notifications":
		return s.telegramCommand(ctx, fromID, username, "/"+strings.TrimPrefix(data, "menu:"))
	case strings.HasPrefix(data, "task:"), strings.HasPrefix(data, "done:"):
		return s.telegramCommand(ctx, fromID, username, "/"+strings.Replace(data, ":", " ", 1))
	default:
		return reply(telegramtext.T(loc, telegramtext.KeyUnknownCommand), menuRows(loc)), nil
	}
}

// telegramLocale reads the stored language, defaulting to English.
func (s *Server) telegramLocale(ctx context.Context, chatID int64) (telegramtext.Locale, error) {
	settings, err := s.deps.Telegram.GetUserSettings(ctx, chatID)
	if err != nil {
		return telegramtext.LocaleEnglish, err
	}
	return settings.Locale, nil
}

// resolveLinked resolves the caller's principal, reporting ok=false when the
// account is not linked (which is answered with instructions, not an error).
func (s *Server) resolveLinked(ctx context.Context, chatID int64) (*rbac.Principal, bool, error) {
	principal, err := s.deps.Telegram.ResolveUser(ctx, chatID)
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
			return nil, false, nil
		}
		return nil, false, err
	}
	return principal, true, nil
}

// telegramTaskError maps a CRM error to a localized reply. The reply keeps the
// linked identity so it travels through the linked consent gate rather than the
// anonymous system path. An unexpected error is returned so the webhook answers
// non-2xx and Telegram retries; a recognized one is explained without leaking
// internals.
func (s *Server) telegramTaskError(err error, chatID int64, principal *rbac.Principal, loc telegramtext.Locale) (*telegramReply, error) {
	linked := func(key telegramtext.Key) *telegramReply {
		return &telegramReply{
			ChatID: chatID, Text: telegramtext.T(loc, key),
			OrgID: &principal.OrganizationID, UserID: &principal.UserID,
		}
	}
	switch {
	case errors.Is(err, rbac.ErrForbidden):
		return linked(telegramtext.KeyNoPermission), nil
	case errors.Is(err, crm.ErrNotFound):
		return linked(telegramtext.KeyTaskNotFound), nil
	case errors.Is(err, crm.ErrConflict):
		return linked(telegramtext.KeyTaskConflict), nil
	default:
		return nil, err
	}
}

func menuRows(loc telegramtext.Locale) [][]notify.InlineButton {
	return [][]notify.InlineButton{
		{{Text: telegramtext.T(loc, telegramtext.BtnTasks), Data: "menu:tasks"}, {Text: telegramtext.T(loc, telegramtext.BtnOverdue), Data: "menu:overdue"}},
		{{Text: telegramtext.T(loc, telegramtext.BtnProfile), Data: "menu:profile"}, {Text: telegramtext.T(loc, telegramtext.BtnLanguage), Data: "menu:language"}},
		{{Text: telegramtext.T(loc, telegramtext.BtnHelp), Data: "menu:help"}},
	}
}

// languageRows renders the language picker. The labels are written in their own
// language, so a speaker can find theirs without reading the current one.
func languageRows() [][]notify.InlineButton {
	return [][]notify.InlineButton{
		{{Text: "🇷🇺 Русский", Data: "lang:ru"}, {Text: "🇬🇧 English", Data: "lang:en"}},
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
		// A button tap must always be acknowledged, even if the message that follows
		// is refused or empty, so the client stops showing a spinner.
		if reply.CallbackID != "" {
			if err := s.deps.Telegram.AnswerCallback(ctx, reply.CallbackID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(reply.Text) == "" {
			return s.finishTelegramReply(ctx, updateID, "accepted")
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
			err = s.deps.Telegram.ReplyLinked(ctx, reply.ChatID, *reply.OrgID, *reply.UserID, reply.Text, reply.Buttons)
		} else {
			err = s.deps.Telegram.ReplySystem(ctx, reply.ChatID, reply.Text, reply.Buttons)
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
