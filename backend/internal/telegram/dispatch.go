// Package telegram runs the ForgeCRM Telegram bot.
//
// The bot is a first-class service, not a hook inside the HTTP API: it polls the
// Telegram Bot API directly, so it needs no public webhook and no inbound port.
// The dispatcher here is transport-neutral — it turns one validated Telegram
// update into a reply — so the same authorization, consent and idempotency rules
// apply whether an update arrives by long poll (this service) or by webhook (the
// API). There is one implementation of that path, and both entry points call it.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

// Dispatcher turns one validated Telegram update into a reply and delivers it.
//
// Every field is an interface the caller already builds for the rest of the
// system, so the dispatcher cannot reach anything the API could not. It holds no
// token of its own: sending is delegated to the TelegramService, which is the one
// place that owns the consent gate.
type Dispatcher struct {
	DB       *database.DB
	Telegram *notify.TelegramService
	CRM      *crm.Service
	Keyring  *secrets.Keyring
	Log      *slog.Logger
}

// NewDispatcher builds a dispatcher. It returns nil when the transport is
// unavailable, so a caller can treat "Telegram is off" as a nil check rather than
// threading a second enabled flag.
func NewDispatcher(db *database.DB, tg *notify.TelegramService, crmSvc *crm.Service, keyring *secrets.Keyring, log *slog.Logger) *Dispatcher {
	if db == nil || tg == nil || !tg.Enabled() || keyring == nil || crmSvc == nil {
		return nil
	}
	return &Dispatcher{DB: db, Telegram: tg, CRM: crmSvc, Keyring: keyring, Log: log}
}

// Incoming is a Telegram update. Only private, human messages and private button
// taps are ever interpreted; every other well-formed update is acknowledged.
type Incoming struct {
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

// reply is the response recorded in the receipt and delivered after the
// transaction commits. It carries localized text and an optional inline keyboard.
type reply struct {
	ChatID        int64
	Text          string
	Buttons       [][]notify.InlineButton
	CallbackID    string
	OrgID         *uuid.UUID
	UserID        *uuid.UUID
	NeedsTaskRead bool
	TaskSnapshots []taskSnapshot
}

type taskSnapshot struct {
	ID        uuid.UUID
	UpdatedAt time.Time
}

// ParseIncoming validates the envelope of a raw update. It rejects a body that is
// not a single update object, so a crafted payload cannot smuggle a second event.
func ParseIncoming(body []byte) (Incoming, bool) {
	var raw map[string]json.RawMessage
	var update Incoming
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil || len(raw) < 2 {
		return Incoming{}, false
	}
	if json.Unmarshal(body, &update) != nil || update.UpdateID == nil || *update.UpdateID < 0 {
		return Incoming{}, false
	}
	events := 0
	for key, value := range raw {
		if key == "update_id" {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(value, &obj) != nil || obj == nil {
			return Incoming{}, false
		}
		events++
	}
	if events != 1 || !eventValid(update) {
		return Incoming{}, false
	}
	return update, true
}

// eventValid accepts a message or a callback query from a private chat and rejects
// anything malformed. A callback's data is bounded to Telegram's own limit so a
// crafted payload cannot inflate the encrypted reply.
func eventValid(update Incoming) bool {
	if m := update.Message; m != nil {
		return m.MessageID > 0 && m.Date > 0 && m.Chat != nil && m.Chat.ID != 0 &&
			chatTypeValid(m.Chat.Type) && (m.From == nil || m.From.ID > 0)
	}
	if cq := update.CallbackQuery; cq != nil {
		return cq.ID != "" && len(cq.Data) <= 64 && cq.From != nil && cq.From.ID > 0 &&
			cq.Message != nil && cq.Message.Chat != nil && cq.Message.Chat.ID != 0 &&
			chatTypeValid(cq.Message.Chat.Type)
	}
	return false
}

func chatTypeValid(kind string) bool {
	switch kind {
	case "private", "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

// ProcessUpdate records the update's receipt, runs the command in the same
// transaction and stores the encrypted reply. It is idempotent: a replayed update
// id is ignored, so a redelivery never repeats a CRM write.
func (d *Dispatcher) ProcessUpdate(ctx context.Context, update Incoming) error {
	return d.DB.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		tag, err := d.DB.Q(ctx).Exec(ctx, `INSERT INTO telegram_update_receipts(update_id) VALUES($1) ON CONFLICT DO NOTHING`, *update.UpdateID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		r, err := d.event(ctx, update)
		if err != nil {
			return err
		}
		if r == nil {
			return nil
		}
		// Encrypt even task titles, never the incoming message or the binding code.
		plain, err := json.Marshal(r)
		if err != nil {
			return err
		}
		sealed, err := d.Keyring.Encrypt(plain)
		if err != nil {
			return err
		}
		_, err = d.DB.Q(ctx).Exec(ctx, `UPDATE telegram_update_receipts SET reply_ciphertext=$2 WHERE update_id=$1`, *update.UpdateID, sealed)
		return err
	})
}

// event turns a validated update into a reply, or nil when the update must be
// acknowledged without a response. Only private human senders reach here.
func (d *Dispatcher) event(ctx context.Context, update Incoming) (*reply, error) {
	if cq := update.CallbackQuery; cq != nil {
		return d.callback(ctx, cq.ID, cq.From.ID, cq.From.Username, cq.Message.Chat.ID, cq.Message.Chat.Type, cq.Data)
	}
	m := update.Message
	if m == nil || m.From == nil || m.Chat == nil || m.MessageID <= 0 || m.From.ID <= 0 || m.From.IsBot || m.Chat.Type != "private" || m.Chat.ID != m.From.ID || strings.TrimSpace(m.Text) == "" {
		return nil, nil
	}
	return d.command(ctx, m.From.ID, m.From.Username, m.Text)
}

// command handles a text command. The reply is localized by the sender's stored
// language, which is read before any handler runs so an error message is already
// in the right language.
func (d *Dispatcher) command(ctx context.Context, chatID int64, username, text string) (*reply, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil, nil
	}
	loc, err := d.locale(ctx, chatID)
	if err != nil {
		return nil, err
	}
	cmd := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
	replyFn := func(text string, rows [][]notify.InlineButton) *reply {
		return &reply{ChatID: chatID, Text: text, Buttons: rows}
	}
	system := func(key telegramtext.Key) *reply {
		return replyFn(telegramtext.T(loc, key), nil)
	}
	switch cmd {
	case "/start":
		settings, err := d.Telegram.GetUserSettings(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if settings.OnboardedAt == nil {
			if err := d.Telegram.MarkOnboarded(ctx, chatID); err != nil {
				return nil, err
			}
			return replyFn(telegramtext.T(loc, telegramtext.KeyWelcome), languageRows()), nil
		}
		return replyFn(telegramtext.T(loc, telegramtext.KeyMenuTitle), menuRows(loc)), nil
	case "/help":
		return replyFn(telegramtext.T(loc, telegramtext.KeyHelp), [][]notify.InlineButton{{{Text: telegramtext.T(loc, telegramtext.BtnMenu), Data: "menu:home"}}}), nil
	case "/menu":
		return replyFn(telegramtext.T(loc, telegramtext.KeyMenuTitle), menuRows(loc)), nil
	case "/language":
		if len(parts) == 2 {
			tag := strings.ToLower(parts[1])
			if !telegramtext.IsSupported(tag) {
				return replyFn(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
			}
			next := telegramtext.Normalize(tag)
			if err := d.Telegram.SetUserLocale(ctx, chatID, next); err != nil {
				return nil, err
			}
			if err := d.Telegram.MarkOnboarded(ctx, chatID); err != nil {
				return nil, err
			}
			return replyFn(telegramtext.T(next, telegramtext.KeyLanguageSet), menuRows(next)), nil
		}
		return replyFn(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
	case "/link":
		if len(parts) != 2 {
			return system(telegramtext.KeyLinkUsage), nil
		}
		_, err := d.Telegram.Link(ctx, notify.LinkRequest{Code: parts[1], TelegramUserID: chatID, Username: username})
		if err != nil {
			var apiErr *httpx.APIError
			if errors.As(err, &apiErr) && (apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusConflict) {
				return system(telegramtext.KeyLinkFailed), nil
			}
			return nil, err
		}
		principal, err := d.Telegram.ResolveUser(ctx, chatID)
		if err != nil {
			return nil, err
		}
		return &reply{ChatID: chatID, Text: telegramtext.T(loc, telegramtext.KeyLinkSuccess), Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/profile":
		principal, ok, err := d.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		body := fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyProfile), principal.Email, principal.OrganizationName, string(loc))
		return &reply{ChatID: chatID, Text: body, Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/notifications":
		principal, ok, err := d.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		items, err := d.CRM.ListMyNotifications(ctx, principal, true, 5)
		if err != nil {
			return d.taskError(err, chatID, principal, loc)
		}
		if len(items) == 0 {
			return &reply{ChatID: chatID, Text: telegramtext.T(loc, telegramtext.KeyNoNotifications), Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
		}
		var b strings.Builder
		b.WriteString(telegramtext.T(loc, telegramtext.KeyNotificationsHdr))
		for _, n := range items {
			b.WriteString("\n• " + safeTitle(n.Title))
		}
		return &reply{ChatID: chatID, Text: b.String(), Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	case "/tasks", "/today", "/overdue", "/task", "/done", "/due":
		principal, ok, err := d.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		tracked := &trackedTasks{taskOperations: d.CRM}
		answer, rows, err := handleTaskCommand(ctx, tracked, principal, text, time.Now().UTC(), loc)
		if err != nil {
			return d.taskError(err, chatID, principal, loc)
		}
		return &reply{ChatID: chatID, Text: answer, Buttons: rows, OrgID: &principal.OrganizationID, UserID: &principal.UserID, NeedsTaskRead: true, TaskSnapshots: tracked.snapshots}, nil
	case "/search", "/deals", "/pipeline":
		principal, ok, err := d.resolveLinked(ctx, chatID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return system(telegramtext.KeyNotLinked), nil
		}
		answer, err := handleLookupCommand(ctx, d.CRM, principal, text, loc)
		if err != nil {
			return d.taskError(err, chatID, principal, loc)
		}
		return &reply{ChatID: chatID, Text: answer, Buttons: menuRows(loc), OrgID: &principal.OrganizationID, UserID: &principal.UserID}, nil
	default:
		return system(telegramtext.KeyUnknownCommand), nil
	}
}

// callback handles a button tap. Data is a short token the bot itself produced, so
// it is matched exactly rather than parsed loosely.
func (d *Dispatcher) callback(ctx context.Context, callbackID string, fromID int64, username string, chatID int64, chatType, data string) (*reply, error) {
	if chatType != "private" || chatID != fromID {
		// A tap in a group chat is acknowledged but never acted on, because the
		// identity of the tapper and the destination would differ.
		return &reply{ChatID: chatID, CallbackID: callbackID}, nil
	}
	loc, err := d.locale(ctx, fromID)
	if err != nil {
		return nil, err
	}
	replyFn := func(text string, rows [][]notify.InlineButton) *reply {
		return &reply{ChatID: chatID, Text: text, Buttons: rows, CallbackID: callbackID}
	}
	switch {
	case strings.HasPrefix(data, "lang:"):
		tag := strings.TrimPrefix(data, "lang:")
		if !telegramtext.IsSupported(tag) {
			return replyFn(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
		}
		next := telegramtext.Normalize(tag)
		if err := d.Telegram.SetUserLocale(ctx, fromID, next); err != nil {
			return nil, err
		}
		if err := d.Telegram.MarkOnboarded(ctx, fromID); err != nil {
			return nil, err
		}
		return replyFn(telegramtext.T(next, telegramtext.KeyLanguageSet), menuRows(next)), nil
	case data == "menu:home":
		return replyFn(telegramtext.T(loc, telegramtext.KeyMenuTitle), menuRows(loc)), nil
	case data == "menu:help":
		return replyFn(telegramtext.T(loc, telegramtext.KeyHelp), [][]notify.InlineButton{{{Text: telegramtext.T(loc, telegramtext.BtnMenu), Data: "menu:home"}}}), nil
	case data == "menu:language":
		return replyFn(telegramtext.T(loc, telegramtext.KeyChooseLanguage), languageRows()), nil
	case data == "menu:tasks", data == "menu:overdue", data == "menu:profile", data == "menu:notifications", data == "menu:deals", data == "menu:pipeline", data == "menu:search":
		return d.command(ctx, fromID, username, "/"+strings.TrimPrefix(data, "menu:"))
	case strings.HasPrefix(data, "task:"), strings.HasPrefix(data, "done:"):
		return d.command(ctx, fromID, username, "/"+strings.Replace(data, ":", " ", 1))
	default:
		return replyFn(telegramtext.T(loc, telegramtext.KeyUnknownCommand), menuRows(loc)), nil
	}
}

// locale reads the stored language, defaulting to English.
func (d *Dispatcher) locale(ctx context.Context, chatID int64) (telegramtext.Locale, error) {
	settings, err := d.Telegram.GetUserSettings(ctx, chatID)
	if err != nil {
		return telegramtext.LocaleEnglish, err
	}
	return settings.Locale, nil
}

// resolveLinked resolves the caller's principal, reporting ok=false when the
// account is not linked (which is answered with instructions, not an error).
func (d *Dispatcher) resolveLinked(ctx context.Context, chatID int64) (*rbac.Principal, bool, error) {
	principal, err := d.Telegram.ResolveUser(ctx, chatID)
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
			return nil, false, nil
		}
		return nil, false, err
	}
	return principal, true, nil
}

// taskError maps a CRM error to a localized reply. The reply keeps the linked
// identity so it travels through the linked consent gate rather than the anonymous
// system path. An unexpected error is returned so the caller answers non-2xx and
// Telegram retries; a recognized one is explained without leaking internals.
func (d *Dispatcher) taskError(err error, chatID int64, principal *rbac.Principal, loc telegramtext.Locale) (*reply, error) {
	linked := func(key telegramtext.Key) *reply {
		return &reply{
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
		{{Text: telegramtext.T(loc, telegramtext.BtnDeals), Data: "menu:deals"}, {Text: telegramtext.T(loc, telegramtext.BtnPipeline), Data: "menu:pipeline"}},
		{{Text: telegramtext.T(loc, telegramtext.BtnSearch), Data: "menu:search"}},
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

// DeliverReply serializes duplicates with a receipt row lock. It clears the
// encrypted response once accepted. A network failure returns an error so the
// caller can retry, without executing the CRM action again. As with any external
// API, a crash after acceptance but before commit can duplicate a reply.
func (d *Dispatcher) DeliverReply(ctx context.Context, updateID int64) error {
	return d.DB.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		var sealed []byte
		err := d.DB.Q(ctx).QueryRow(ctx, `SELECT reply_ciphertext FROM telegram_update_receipts WHERE update_id=$1 FOR UPDATE`, updateID).Scan(&sealed)
		if err != nil {
			return err
		}
		if len(sealed) == 0 {
			return nil
		}
		plain, err := d.Keyring.Decrypt(sealed)
		if err != nil {
			return err
		}
		var r reply
		if err := json.Unmarshal(plain, &r); err != nil {
			return err
		}
		// A button tap must always be acknowledged, even if the message that follows
		// is refused or empty, so the client stops showing a spinner.
		if r.CallbackID != "" {
			if err := d.Telegram.AnswerCallback(ctx, r.CallbackID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(r.Text) == "" {
			return d.finishReply(ctx, updateID, "accepted")
		}
		if r.OrgID != nil && r.UserID != nil {
			if r.NeedsTaskRead {
				p, err := d.Telegram.ResolveUser(ctx, r.ChatID)
				if err != nil {
					var apiErr *httpx.APIError
					if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
						err = notify.ErrTelegramRecipientUnavailable
					}
					if !errors.Is(err, notify.ErrTelegramRecipientUnavailable) {
						return err
					}
				} else if p.UserID != *r.UserID || p.OrganizationID != *r.OrgID || !p.Has(rbac.PermTasksRead) {
					err = notify.ErrTelegramRecipientUnavailable
				}
				if err != nil {
					return d.finishReply(ctx, updateID, "unavailable")
				}
			}
			for _, snapshot := range r.TaskSnapshots {
				var id uuid.UUID
				err := d.DB.Q(ctx).QueryRow(ctx, `SELECT id FROM tasks WHERE id=$1 AND organization_id=$2 AND assignee_user_id=$3 AND archived_at IS NULL AND updated_at=$4 FOR SHARE`, snapshot.ID, *r.OrgID, *r.UserID, snapshot.UpdatedAt).Scan(&id)
				if database.IsNoRows(err) {
					return d.finishReply(ctx, updateID, "unavailable")
				}
				if err != nil {
					return err
				}
			}
			err = d.Telegram.ReplyLinked(ctx, r.ChatID, *r.OrgID, *r.UserID, r.Text, r.Buttons)
		} else {
			err = d.Telegram.ReplySystem(ctx, r.ChatID, r.Text, r.Buttons)
		}
		outcome := "accepted"
		if errors.Is(err, notify.ErrConsentBlocked) {
			outcome = "blocked"
		} else if errors.Is(err, notify.ErrTelegramRecipientUnavailable) {
			outcome = "unavailable"
		} else if err != nil {
			return err
		}
		return d.finishReply(ctx, updateID, outcome)
	})
}

func (d *Dispatcher) finishReply(ctx context.Context, updateID int64, outcome string) error {
	_, err := d.DB.Q(ctx).Exec(ctx, `UPDATE telegram_update_receipts SET reply_ciphertext=NULL, finished_at=now(), outcome=$2 WHERE update_id=$1`, updateID, outcome)
	return err
}
