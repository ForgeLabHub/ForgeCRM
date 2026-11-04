package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

// TelegramService implements the optional Telegram bot.
//
// The bot is disabled unless a token is configured, and even then every operation
// is authorised against a linked CRM user. A Telegram account that is not linked,
// or whose link has been revoked, can do nothing.
type TelegramService struct {
	cfg     *config.TelegramConfig
	db      *database.DB
	log     *slog.Logger
	audit   *audit.Logger
	keyring *secrets.Keyring
	client  *http.Client
	gate    ConsentGate
}

// NewTelegramService builds the Telegram service.
//
// The gate is taken here for the same reason as the email transport's: building a
// transport is the same act as deciding who it must ask before it messages a
// person, so no caller can reach Telegram without the policy having answered.
func NewTelegramService(cfg *config.TelegramConfig, db *database.DB, log *slog.Logger,
	auditLog *audit.Logger, keyring *secrets.Keyring, gate ConsentGate) *TelegramService {
	return &TelegramService{
		cfg: cfg, db: db, log: log, audit: auditLog, keyring: keyring, gate: gate,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the bot is configured and switched on.
//
// It tolerates a nil receiver so that an unconfigured bot reports "off" rather
// than panicking. This is the path taken when no token is set, which must leave the
// rest of the system working.
func (s *TelegramService) Enabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Enabled && s.cfg.BotToken != ""
}

// WebhookEnabled is deliberately opt-in and refuses a weak or malformed secret.
// TLS is terminated by the deployment proxy; this does not establish HTTPS.
func (s *TelegramService) WebhookEnabled() bool {
	if !s.Enabled() || !s.cfg.WebhookEnabled || len(s.cfg.WebhookSecret) < 32 || len(s.cfg.WebhookSecret) > 256 {
		return false
	}
	for _, c := range s.cfg.WebhookSecret {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ErrTelegramDisabled is returned when the bot is not configured.
var ErrTelegramDisabled = errors.New("the Telegram bot is not configured")

// LinkCodeTTL is how long a link code is valid.
const LinkCodeTTL = 15 * time.Minute

// lockTelegramAccount serializes linking and revocation for one CRM account in
// one organization, even when multiple codes are redeemed concurrently.
func (s *TelegramService) lockTelegramAccount(ctx context.Context, orgID, userID uuid.UUID) error {
	_, err := s.db.Q(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('telegram-link:' || $1::text || ':' || $2::text, 0))`, orgID, userID)
	return err
}

// CreateLinkCode issues a high-entropy, single-use challenge. Only its hash is
// stored. The active membership is checked again in the redemption transaction.
func (s *TelegramService) CreateLinkCode(ctx context.Context, p *rbac.Principal) (string, error) {
	if !s.Enabled() {
		return "", ErrTelegramDisabled
	}
	if p == nil || p.UserID == uuid.Nil || p.OrganizationID == uuid.Nil {
		return "", httpx.ErrForbidden
	}
	code, err := secrets.RandomToken(32)
	if err != nil {
		return "", err
	}
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockTelegramAccount(ctx, p.OrganizationID, p.UserID); err != nil {
			return fmt.Errorf("lock telegram account: %w", err)
		}
		const q = `INSERT INTO telegram_link_codes (organization_id, user_id, code_hash, expires_at)
			SELECT m.organization_id, m.user_id, $3, now() + make_interval(secs => $4::int)
			FROM memberships m JOIN users u ON u.id = m.user_id
			JOIN organizations o ON o.id = m.organization_id
			WHERE m.organization_id = $1 AND m.user_id = $2
			  AND m.status = 'active' AND u.status = 'active' AND o.status = 'active'`
		tag, err := s.db.Q(ctx).Exec(ctx, q, p.OrganizationID, p.UserID, secrets.HashToken(code), int(LinkCodeTTL.Seconds()))
		if err != nil {
			return fmt.Errorf("create telegram link code: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrForbidden
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// LinkRequest is a link attempt from the bot.
type LinkRequest struct {
	Code           string
	TelegramUserID int64
	Username       string
}

// Link atomically consumes a code and associates it with a private Telegram user.
// A webhook must independently establish that the sender and private chat match.
func (s *TelegramService) Link(ctx context.Context, req LinkRequest) (uuid.UUID, error) {
	if !s.Enabled() {
		return uuid.Nil, ErrTelegramDisabled
	}
	invalid := httpx.NewError(400, httpx.CodeBadRequest, "the link code is not valid or has expired")
	if req.TelegramUserID <= 0 || len(req.Code) != 43 || len(req.Username) > 64 {
		return uuid.Nil, invalid
	}
	hash := secrets.HashToken(req.Code)
	var userID, orgID uuid.UUID
	// This lookup only identifies the account lock. The code is rechecked and
	// consumed under that lock in the transaction; a stale lookup cannot succeed.
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT user_id, organization_id FROM telegram_link_codes WHERE code_hash = $1`, hash).Scan(&userID, &orgID)
	if database.IsNoRows(err) {
		return uuid.Nil, invalid
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lookup telegram link code: %w", err)
	}
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockTelegramAccount(ctx, orgID, userID); err != nil {
			return fmt.Errorf("lock telegram account: %w", err)
		}
		// Lock membership, user and organization until commit so disabling any of
		// them cannot race a successful link. All conditions are checked in SQL.
		var active bool
		const membership = `SELECT true FROM memberships m
			JOIN users u ON u.id=m.user_id JOIN organizations o ON o.id=m.organization_id
			WHERE m.user_id=$1 AND m.organization_id=$2 AND m.status='active'
			  AND u.status='active' AND o.status='active' FOR SHARE OF m, u, o`
		if err := s.db.Q(ctx).QueryRow(ctx, membership, userID, orgID).Scan(&active); err != nil {
			if database.IsNoRows(err) {
				return invalid
			}
			return fmt.Errorf("check telegram membership: %w", err)
		}
		tag, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM telegram_link_codes
			WHERE code_hash=$1 AND user_id=$2 AND organization_id=$3 AND expires_at > now()`, hash, userID, orgID)
		if err != nil {
			return fmt.Errorf("consume telegram link code: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return invalid
		}
		var existing int64
		err = s.db.Q(ctx).QueryRow(ctx, `SELECT telegram_user_id FROM telegram_links
			WHERE organization_id=$1 AND user_id=$2 AND linked_at IS NOT NULL AND revoked_at IS NULL LIMIT 1`, orgID, userID).Scan(&existing)
		if err != nil && !database.IsNoRows(err) {
			return fmt.Errorf("lookup existing telegram link: %w", err)
		}
		if err == nil && existing != req.TelegramUserID {
			return httpx.NewError(409, httpx.CodeConflict, "unlink the existing Telegram account first")
		}
		// The unique Telegram ID prevents concurrent claims by different accounts.
		// Never steal another account's active link, even across organizations.
		var id uuid.UUID
		const link = `INSERT INTO telegram_links (organization_id,user_id,telegram_user_id,telegram_username,linked_at)
			VALUES ($1,$2,$3,NULLIF($4,''),now())
			ON CONFLICT (telegram_user_id) DO UPDATE SET
			  organization_id=EXCLUDED.organization_id, user_id=EXCLUDED.user_id,
			  telegram_username=EXCLUDED.telegram_username, linked_at=now(), revoked_at=NULL,
			  link_code_hash=NULL, link_code_expires_at=NULL
			WHERE telegram_links.revoked_at IS NOT NULL OR
			  (telegram_links.user_id=EXCLUDED.user_id AND telegram_links.organization_id=EXCLUDED.organization_id)
			RETURNING id`
		if err := s.db.Q(ctx).QueryRow(ctx, link, orgID, userID, req.TelegramUserID, req.Username).Scan(&id); err != nil {
			if database.IsNoRows(err) {
				return httpx.NewError(409, httpx.CodeConflict, "Telegram account is already linked")
			}
			return fmt.Errorf("link telegram account: %w", err)
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &orgID, ActorUserID: &userID,
			Action: string(audit.ActionTelegramLinked), EntityType: "telegram_link", EntityID: id.String(),
		})
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// ResolveUser returns the CRM user linked to a Telegram account, or an error.
func (s *TelegramService) ResolveUser(ctx context.Context, telegramUserID int64) (*rbac.Principal, error) {
	if !s.Enabled() {
		return nil, ErrTelegramDisabled
	}
	var (
		userID uuid.UUID
		orgID  uuid.UUID
		status string
		email  string
		name   string
	)
	q := `
		SELECT l.user_id, l.organization_id, u.status, u.email, u.display_name
		FROM telegram_links l
		JOIN users u ON u.id = l.user_id
		JOIN memberships m ON m.user_id = l.user_id AND m.organization_id = l.organization_id AND m.status = 'active'
		JOIN organizations o ON o.id = l.organization_id AND o.status = 'active'
		WHERE l.telegram_user_id = $1 AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL`
	if _, ok := database.TxFromContext(ctx); ok {
		q += " FOR SHARE OF l, m, u, o"
	}
	if err := s.db.Q(ctx).QueryRow(ctx, q, telegramUserID).Scan(&userID, &orgID, &status, &email, &name); err != nil {
		if database.IsNoRows(err) {
			return nil, httpx.NewError(403, httpx.CodeForbidden, "this Telegram account is not linked to a ForgeCRM user")
		}
		return nil, fmt.Errorf("resolve telegram account: %w", err)
	}
	// A blocked or suspended user loses bot access immediately.
	if status != "active" {
		return nil, httpx.NewError(403, httpx.CodeForbidden, "this ForgeCRM account is not active")
	}
	// Permissions are resolved from the database at the time of the request, so a
	// role change takes effect on the next message.
	perms, err := rbac.NewService(s.db).EffectivePermissions(ctx, userID, orgID)
	if err != nil {
		return nil, err
	}
	p := rbac.NewPrincipal(userID, email, name, orgID, perms)
	return p, nil
}

// Unlink revokes a Telegram link.
func (s *TelegramService) Unlink(ctx context.Context, p *rbac.Principal) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockTelegramAccount(ctx, p.OrganizationID, p.UserID); err != nil {
			return fmt.Errorf("lock telegram account: %w", err)
		}
		codes, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM telegram_link_codes WHERE organization_id=$1 AND user_id=$2`, p.OrganizationID, p.UserID)
		if err != nil {
			return fmt.Errorf("revoke telegram link codes: %w", err)
		}
		const q = `
			UPDATE telegram_links SET revoked_at = now()
			WHERE user_id = $1 AND organization_id = $2 AND revoked_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, p.UserID, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("unlink telegram: %w", err)
		}
		if tag.RowsAffected() == 0 && codes.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionTelegramUnlinked), EntityType: "telegram_link",
		})
	})
}

// Update is an incoming Telegram update.
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		From      *struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Chat *struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// UserSettings is the assistant's per-Telegram-account preferences.
type UserSettings struct {
	Locale      telegramtext.Locale
	OnboardedAt *time.Time
}

// GetUserSettings loads the stored preferences for a Telegram account, creating
// nothing. An unknown account reports the English default and "not onboarded", so
// a first contact is answered without a write.
func (s *TelegramService) GetUserSettings(ctx context.Context, telegramUserID int64) (UserSettings, error) {
	settings := UserSettings{Locale: telegramtext.LocaleEnglish}
	if !s.Enabled() || telegramUserID <= 0 {
		return settings, nil
	}
	var locale string
	var onboarded *time.Time
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT locale, onboarded_at FROM telegram_user_settings WHERE telegram_user_id=$1`, telegramUserID).Scan(&locale, &onboarded)
	if database.IsNoRows(err) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("load telegram settings: %w", err)
	}
	settings.Locale = telegramtext.Normalize(locale)
	settings.OnboardedAt = onboarded
	return settings, nil
}

// SetUserLocale persists the chosen language for a Telegram account. It is
// idempotent and safe to call before the account is linked.
func (s *TelegramService) SetUserLocale(ctx context.Context, telegramUserID int64, locale telegramtext.Locale) error {
	if !s.Enabled() || telegramUserID <= 0 {
		return ErrTelegramDisabled
	}
	const q = `INSERT INTO telegram_user_settings (telegram_user_id, locale) VALUES ($1, $2)
		ON CONFLICT (telegram_user_id) DO UPDATE SET locale=EXCLUDED.locale, updated_at=now()`
	if _, err := s.db.Q(ctx).Exec(ctx, q, telegramUserID, string(locale)); err != nil {
		return fmt.Errorf("store telegram locale: %w", err)
	}
	return nil
}

// MarkOnboarded records that the introduction has been shown, so /start does not
// repeat it. It does not overwrite a chosen locale.
func (s *TelegramService) MarkOnboarded(ctx context.Context, telegramUserID int64) error {
	if !s.Enabled() || telegramUserID <= 0 {
		return ErrTelegramDisabled
	}
	const q = `INSERT INTO telegram_user_settings (telegram_user_id, onboarded_at) VALUES ($1, now())
		ON CONFLICT (telegram_user_id) DO UPDATE SET onboarded_at=COALESCE(telegram_user_settings.onboarded_at, now()), updated_at=now()`
	if _, err := s.db.Q(ctx).Exec(ctx, q, telegramUserID); err != nil {
		return fmt.Errorf("store telegram onboarding: %w", err)
	}
	return nil
}

// VerifyWebhookSecret checks the secret token Telegram sends in the header.
//
// Telegram supports a secret token on webhook registration; comparing it with a
// constant-time check prevents an unauthenticated caller from injecting updates.
func (s *TelegramService) VerifyWebhookSecret(headerValue string) bool {
	if !s.Enabled() {
		return false
	}
	if s.cfg.WebhookSecret == "" {
		return false
	}
	return secrets.ConstantTimeEqual([]byte(s.cfg.WebhookSecret), []byte(headerValue))
}

// AllowedCommands is the closed set of bot commands.
var AllowedCommands = map[string]string{
	"/start":  "link your ForgeCRM account",
	"/link":   "link your ForgeCRM account with a code",
	"/tasks":  "list your open tasks",
	"/unlink": "remove the link between Telegram and ForgeCRM",
	"/help":   "show this help",
}

// HandleCommand processes a bot command for a linked user and returns the reply.
//
// The task list is filtered by the same permission and ownership rules as the
// REST API, so the bot cannot become a way to read another user's work.
func (s *TelegramService) HandleCommand(ctx context.Context, telegramUserID int64, text string) (string, error) {
	if !s.Enabled() {
		return "", ErrTelegramDisabled
	}
	text = strings.TrimSpace(text)
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil
	}
	command := strings.ToLower(parts[0])
	// Telegram commands may carry a bot suffix, e.g. /tasks@ForgeCRMBot.
	if i := strings.IndexByte(command, '@'); i > 0 {
		command = command[:i]
	}

	if command == "/start" || command == "/help" {
		var b strings.Builder
		b.WriteString("ForgeCRM bot\n\n")
		for cmd, desc := range AllowedCommands {
			fmt.Fprintf(&b, "%s - %s\n", cmd, desc)
		}
		return b.String(), nil
	}

	if command == "/link" {
		if len(parts) < 2 {
			return "Usage: /link <code>\nGenerate a code in ForgeCRM under Settings, then send it here.", nil
		}
		userID, err := s.Link(ctx, LinkRequest{Code: parts[1], TelegramUserID: telegramUserID})
		if err != nil {
			var apiErr *httpx.APIError
			if errors.As(err, &apiErr) && (apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusConflict) {
				return "Could not link this account. Check the code or unlink the existing account first.", nil
			}
			return "", err
		}
		return "Linked successfully to ForgeCRM user " + userID.String()[:8] + ".\nUse /tasks to see your open tasks.", nil
	}

	// Every other command requires a linked, active, permissioned user.
	principal, err := s.ResolveUser(ctx, telegramUserID)
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
			return "This Telegram account is not linked. Send /link <code> first.", nil
		}
		return "", err
	}

	switch command {
	case "/tasks":
		if !principal.Has(rbac.PermTasksRead) {
			return "You do not have permission to view tasks.", nil
		}
		return s.listOwnTasks(ctx, principal)
	case "/unlink":
		if err := s.Unlink(ctx, principal); err != nil {
			return "", err
		}
		return "This Telegram account is no longer linked to ForgeCRM.", nil
	default:
		return "Unknown command. Send /help for the list of available commands.", nil
	}
}

func (s *TelegramService) listOwnTasks(ctx context.Context, p *rbac.Principal) (string, error) {
	const q = `
		SELECT title, status, priority, due_at
		FROM tasks
		WHERE organization_id = $1 AND assignee_user_id = $2
		  AND status IN ('open','in_progress') AND archived_at IS NULL
		ORDER BY due_at ASC NULLS LAST LIMIT 10`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, p.UserID)
	if err != nil {
		return "", fmt.Errorf("list tasks for telegram: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString("Your open tasks:\n")
	n := 0
	for rows.Next() {
		var (
			title    string
			status   string
			priority string
			dueAt    *time.Time
		)
		if err := rows.Scan(&title, &status, &priority, &dueAt); err != nil {
			return "", err
		}
		n++
		due := "no due date"
		if dueAt != nil {
			due = "due " + dueAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&b, "%d. [%s] %s (%s)\n", n, priority, title, due)
	}
	if n == 0 {
		return "You have no open tasks.", nil
	}
	return b.String(), rows.Err()
}

// InlineButton is one button of an inline keyboard. Data is the callback payload
// Telegram echoes back; it must stay small and carry no secret.
type InlineButton struct {
	Text string
	Data string
}

// sendMessage posts a message to a chat. It is unexported on purpose: every
// message this transport sends to a person must pass the consent gate, and the only
// gated path is NotifyUser. Leaving a public SendMessage would be an ungated door
// beside a gated one, which is exactly the shape of bypass the gate exists to end.
func (s *TelegramService) sendMessage(ctx context.Context, chatID int64, text string) error {
	return s.sendMessageWithButtons(ctx, chatID, text, nil)
}

// sendMessageWithButtons posts a message with an optional inline keyboard. The
// keyboard rows are built by the caller from localized labels; the payloads are
// short opaque tokens the bot itself defined, so a tap cannot smuggle data.
func (s *TelegramService) sendMessageWithButtons(ctx context.Context, chatID int64, text string, rows [][]InlineButton) error {
	if !s.Enabled() {
		return ErrTelegramDisabled
	}
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if len(rows) > 0 {
		keyboard := make([][]map[string]string, 0, len(rows))
		for _, row := range rows {
			cells := make([]map[string]string, 0, len(row))
			for _, b := range row {
				cells = append(cells, map[string]string{"text": b.Text, "callback_data": b.Data})
			}
			keyboard = append(keyboard, cells)
		}
		payload["reply_markup"] = map[string]any{"inline_keyboard": keyboard}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.post(ctx, "sendMessage", encoded)
}

// answerCallbackQuery acknowledges a button tap so the client stops showing a
// spinner. It is best effort: a failure here must not fail the update.
func (s *TelegramService) answerCallbackQuery(ctx context.Context, callbackID string) error {
	if !s.Enabled() || callbackID == "" {
		return nil
	}
	encoded, err := json.Marshal(map[string]any{"callback_query_id": callbackID})
	if err != nil {
		return err
	}
	return s.post(ctx, "answerCallbackQuery", encoded)
}

// post sends one JSON body to a Telegram Bot API method and confirms acceptance.
// It never returns or logs the URL (which carries the token) or the response body.
func (s *TelegramService) post(ctx context.Context, method string, body []byte) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", s.cfg.BotToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		// URL parsing errors can contain the bot token as well.
		return errors.New("telegram request: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		// net/http errors may contain the URL, including the bot token. Never return
		// or log that error; the caller can safely report this generic failure.
		return errors.New("telegram request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API returned HTTP %d", resp.StatusCode)
	}
	// A successful HTTP status alone is not proof Telegram accepted the API request.
	// Do not return the response body: it may contain sensitive message data.
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil || !result.OK {
		return errors.New("telegram API did not confirm acceptance")
	}
	return nil
}

// NotifyUser sends a notification to a user's linked Telegram account, if any.
// The absence of a link is not an error.
//
// Consent is checked before the message is sent, against the linked user's own
// email address and on the telegram channel. The subject is the person, not the
// chat: a decision recorded for someone's address applies whether they are reached
// by mail or by bot, and a Telegram message is not exempt because the transport
// happens to be a bot the user linked themselves. The purpose is transactional: a
// notification addressed to a member of staff is about the work they are doing, not
// a marketing message, so it does not require a recorded grant — but an explicit
// denial or withdrawal still stops it, which is the case this gate is here for.
func (s *TelegramService) NotifyUser(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, text string) error {
	if !s.Enabled() {
		return nil
	}
	const q = `
		SELECT l.telegram_user_id, u.email
		FROM telegram_links l
		JOIN users u ON u.id = l.user_id AND u.status = 'active'
		JOIN memberships m ON m.user_id = l.user_id AND m.organization_id = l.organization_id AND m.status = 'active'
		JOIN organizations o ON o.id = l.organization_id AND o.status = 'active'
		WHERE l.user_id = $1 AND l.organization_id = $2
		  AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL LIMIT 1`
	var chatID int64
	var email string
	if err := s.db.Q(ctx).QueryRow(ctx, q, userID, orgID).Scan(&chatID, &email); err != nil {
		if database.IsNoRows(err) {
			return nil
		}
		return fmt.Errorf("resolve telegram recipient: %w", err)
	}
	// The gate is asked before the bot is contacted, so a message consent forbids is
	// never sent, rather than being sent and then explained away.
	if err := enforce(ctx, s.gate, &orgID, &userID, email, string(PurposeTransactional), ChannelTelegram); err != nil {
		return err
	}
	return s.sendMessage(ctx, chatID, text)
}

// ErrTelegramRecipientUnavailable indicates a pending response must be discarded:
// the original account was unlinked, disabled, or moved to another chat.
var ErrTelegramRecipientUnavailable = errors.New("telegram recipient is no longer available")

// ReplyLinked sends a response only if the exact original link is still active.
// It must run in the receipt transaction so the share lock lasts through the
// send and cannot race a revoke or reassignment. The regular consent gate applies.
func (s *TelegramService) ReplyLinked(ctx context.Context, chatID int64, orgID, userID uuid.UUID, text string, rows [][]InlineButton) error {
	if !s.Enabled() {
		return ErrTelegramDisabled
	}
	if _, ok := database.TxFromContext(ctx); !ok {
		return errors.New("telegram reply requires a transaction")
	}
	var email string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT u.email FROM telegram_links l
		JOIN users u ON u.id=l.user_id AND u.status='active'
		JOIN memberships m ON m.user_id=l.user_id AND m.organization_id=l.organization_id AND m.status='active'
		JOIN organizations o ON o.id=l.organization_id AND o.status='active'
		WHERE l.telegram_user_id=$1 AND l.organization_id=$2 AND l.user_id=$3
		AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL FOR SHARE OF l,u,m,o`, chatID, orgID, userID).Scan(&email)
	if database.IsNoRows(err) {
		return ErrTelegramRecipientUnavailable
	}
	if err != nil {
		return fmt.Errorf("resolve telegram reply recipient: %w", err)
	}
	if err := enforce(ctx, s.gate, &orgID, &userID, email, string(PurposeTransactional), ChannelTelegram); err != nil {
		return err
	}
	return s.sendMessageWithButtons(ctx, chatID, text, rows)
}

// ReplySystem answers a private, user-initiated interaction that may not belong to
// a linked account: onboarding, the language picker, /help and error notices. The
// text is localized by the caller. An existing (even revoked) link is checked
// against that person's consent; an unknown chat uses the account-level policy.
func (s *TelegramService) ReplySystem(ctx context.Context, chatID int64, text string, rows [][]InlineButton) error {
	if !s.Enabled() {
		return ErrTelegramDisabled
	}
	if _, ok := database.TxFromContext(ctx); !ok {
		return errors.New("telegram reply requires a transaction")
	}
	var org, user uuid.UUID
	var email string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT l.organization_id,l.user_id,u.email FROM telegram_links l
		JOIN users u ON u.id=l.user_id WHERE l.telegram_user_id=$1 FOR SHARE OF l,u`, chatID).Scan(&org, &user, &email)
	if err != nil && !database.IsNoRows(err) {
		return fmt.Errorf("resolve telegram system reply: %w", err)
	}
	if err == nil {
		if err := enforce(ctx, s.gate, &org, &user, email, string(PurposeTransactional), ChannelTelegram); err != nil {
			return err
		}
	} else if err := enforce(ctx, s.gate, nil, nil, "", string(PurposeTransactional), ChannelTelegram); err != nil {
		return err
	}
	return s.sendMessageWithButtons(ctx, chatID, text, rows)
}

// AnswerCallback acknowledges a button tap so the client stops showing a spinner.
// It is best effort and never fails the update.
func (s *TelegramService) AnswerCallback(ctx context.Context, callbackID string) error {
	return s.answerCallbackQuery(ctx, callbackID)
}

// TaskDeliveryResult distinguishes API acceptance from proof of delivery. An
// image error is sanitized and retained even when text fallback succeeds.
type TaskDeliveryResult struct {
	Status     string
	ImageError string
}

// NotifyTask sends a CRM-backed card using the same recipient and consent policy
// as other notifications. The caller must hold any domain locks needed to ensure
// the task is still assigned to the recipient while this method executes.
func (s *TelegramService) NotifyTask(ctx context.Context, orgID, userID uuid.UUID, card TaskCard) (TaskDeliveryResult, error) {
	var result TaskDeliveryResult
	if !s.Enabled() {
		return result, ErrTelegramDisabled
	}
	var chatID int64
	var email string
	const q = `SELECT l.telegram_user_id,u.email FROM telegram_links l
		JOIN users u ON u.id=l.user_id AND u.status='active'
		JOIN memberships m ON m.user_id=l.user_id AND m.organization_id=l.organization_id AND m.status='active'
		JOIN organizations o ON o.id=l.organization_id AND o.status='active'
		WHERE l.organization_id=$1 AND l.user_id=$2 AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL`
	query := q
	if _, ok := database.TxFromContext(ctx); ok {
		query += " FOR SHARE OF l,u,m,o"
	}
	err := s.db.Q(ctx).QueryRow(ctx, query, orgID, userID).Scan(&chatID, &email)
	if database.IsNoRows(err) {
		result.Status = "no_recipient"
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("resolve telegram recipient: %w", err)
	}
	if err := enforce(ctx, s.gate, &orgID, &userID, email, string(PurposeTransactional), ChannelTelegram); err != nil {
		if errors.Is(err, ErrConsentBlocked) {
			result.Status = "blocked"
			return result, nil
		}
		return result, err
	}
	image, text, err := RenderTaskCard(card)
	if err != nil && text == "" {
		return result, err
	}
	if err == nil {
		err = s.sendPhoto(ctx, chatID, text, image)
	}
	if err == nil {
		result.Status = "photo_accepted"
		return result, nil
	}
	// Error messages from both the renderer and transport are bounded and never
	// include the API URL, response body, message text or token.
	result.ImageError = err.Error()
	if err := s.sendMessage(ctx, chatID, text); err != nil {
		return result, err
	}
	result.Status = "text_accepted"
	return result, nil
}

func (s *TelegramService) sendPhoto(ctx context.Context, chatID int64, caption string, photo []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", fmt.Sprint(chatID)); err != nil {
		return errors.New("telegram photo request failed")
	}
	if err := writer.WriteField("caption", caption); err != nil {
		return errors.New("telegram photo request failed")
	}
	part, err := writer.CreateFormFile("photo", "task.png")
	if err != nil {
		return errors.New("telegram photo request failed")
	}
	if _, err := part.Write(photo); err != nil {
		return errors.New("telegram photo request failed")
	}
	if err := writer.Close(); err != nil {
		return errors.New("telegram photo request failed")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", s.cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return errors.New("telegram photo request invalid")
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New("telegram photo request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram photo API returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil || !result.OK {
		return errors.New("telegram photo API did not confirm acceptance")
	}
	return nil
}
