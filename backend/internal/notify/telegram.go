package notify

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

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
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

// ErrTelegramDisabled is returned when the bot is not configured.
var ErrTelegramDisabled = errors.New("the Telegram bot is not configured")

// LinkCodeTTL is how long a link code is valid.
const LinkCodeTTL = 15 * time.Minute

// CreateLinkCode issues a one-time code a user sends to the bot to link accounts.
func (s *TelegramService) CreateLinkCode(ctx context.Context, p *rbac.Principal) (string, error) {
	if !s.Enabled() {
		return "", ErrTelegramDisabled
	}
	code, err := secrets.RandomNumericCode(8)
	if err != nil {
		return "", err
	}
	const q = `
		INSERT INTO telegram_links (organization_id, user_id, telegram_user_id, link_code_hash, link_code_expires_at)
		VALUES ($1, $2, 0, $3, $4)`
	if _, err := s.db.Q(ctx).Exec(ctx, q, p.OrganizationID, p.UserID,
		secrets.HashToken(code), time.Now().Add(LinkCodeTTL)); err != nil {
		return "", fmt.Errorf("create telegram link code: %w", err)
	}
	return code, nil
}

// LinkRequest is a link attempt from the bot.
type LinkRequest struct {
	Code           string
	TelegramUserID int64
	Username       string
}

// Link associates a Telegram account with a CRM user.
func (s *TelegramService) Link(ctx context.Context, req LinkRequest) (uuid.UUID, error) {
	if !s.Enabled() {
		return uuid.Nil, ErrTelegramDisabled
	}
	var (
		id     uuid.UUID
		userID uuid.UUID
		orgID  uuid.UUID
		expiry time.Time
	)
	const sel = `
		SELECT id, user_id, organization_id, link_code_expires_at
		FROM telegram_links
		WHERE link_code_hash = $1 AND linked_at IS NULL AND revoked_at IS NULL
		ORDER BY created_at DESC LIMIT 1`
	if err := s.db.Q(ctx).QueryRow(ctx, sel, secrets.HashToken(req.Code)).Scan(&id, &userID, &orgID, &expiry); err != nil {
		return uuid.Nil, httpx.NewError(400, httpx.CodeBadRequest, "the link code is not valid")
	}
	if time.Now().After(expiry) {
		return uuid.Nil, httpx.NewError(400, httpx.CodeBadRequest, "the link code has expired")
	}
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// Remove any previous link for this Telegram account so one chat maps to
		// exactly one CRM user.
		const clear = `DELETE FROM telegram_links WHERE telegram_user_id = $1 AND id <> $2`
		if _, err := s.db.Q(ctx).Exec(ctx, clear, req.TelegramUserID, id); err != nil {
			return err
		}
		const upd = `
			UPDATE telegram_links
			SET telegram_user_id=$2, telegram_username=NULLIF($3,''), linked_at=now(), link_code_hash=NULL
			WHERE id=$1`
		if _, err := s.db.Q(ctx).Exec(ctx, upd, id, req.TelegramUserID, req.Username); err != nil {
			return err
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
	const q = `
		SELECT l.user_id, l.organization_id, u.status, u.email, u.display_name
		FROM telegram_links l
		JOIN users u ON u.id = l.user_id
		WHERE l.telegram_user_id = $1 AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL`
	if err := s.db.Q(ctx).QueryRow(ctx, q, telegramUserID).Scan(&userID, &orgID, &status, &email, &name); err != nil {
		return nil, httpx.NewError(403, httpx.CodeForbidden, "this Telegram account is not linked to a ForgeCRM user")
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
		const q = `
			UPDATE telegram_links SET revoked_at = now()
			WHERE user_id = $1 AND organization_id = $2 AND revoked_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, p.UserID, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("unlink telegram: %w", err)
		}
		if tag.RowsAffected() == 0 {
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
			return "Could not link this account: " + err.Error(), nil
		}
		return "Linked successfully to ForgeCRM user " + userID.String()[:8] + ".\nUse /tasks to see your open tasks.", nil
	}

	// Every other command requires a linked, active, permissioned user.
	principal, err := s.ResolveUser(ctx, telegramUserID)
	if err != nil {
		return "This Telegram account is not linked. Send /link <code> first.", nil
	}

	switch command {
	case "/tasks":
		if !principal.Has(rbac.PermTasksRead) {
			return "You do not have permission to view tasks.", nil
		}
		return s.listOwnTasks(ctx, principal)
	case "/unlink":
		if err := s.Unlink(ctx, principal); err != nil {
			return "Could not unlink: " + err.Error(), nil
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

// sendMessage posts a message to a chat. It is unexported on purpose: every
// message this transport sends to a person must pass the consent gate, and the only
// gated path is NotifyUser. Leaving a public SendMessage would be an ungated door
// beside a gated one, which is exactly the shape of bypass the gate exists to end.
func (s *TelegramService) sendMessage(ctx context.Context, chatID int64, text string) error {
	if !s.Enabled() {
		return ErrTelegramDisabled
	}
	payload, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", s.cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram API returned HTTP %d", resp.StatusCode)
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
		JOIN users u ON u.id = l.user_id
		WHERE l.user_id = $1 AND l.linked_at IS NOT NULL AND l.revoked_at IS NULL LIMIT 1`
	var chatID int64
	var email string
	if err := s.db.Q(ctx).QueryRow(ctx, q, userID).Scan(&chatID, &email); err != nil {
		return nil
	}
	// The gate is asked before the bot is contacted, so a message consent forbids is
	// never sent, rather than being sent and then explained away.
	if err := enforce(ctx, s.gate, &orgID, &userID, email, string(PurposeTransactional), ChannelTelegram); err != nil {
		return err
	}
	return s.sendMessage(ctx, chatID, text)
}
