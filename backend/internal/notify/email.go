// Package notify implements outbound notifications: SMTP email and the optional
// Telegram bot.
//
// Email distinguishes three things that are easy to conflate:
//
//	ValidateConfig  - the configuration is internally consistent;
//	TestConnection  - the server is reachable and accepts our credentials;
//	Send            - the server accepted a specific message for delivery.
//
// Only the last one justifies reporting that a message was sent.
package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
)

// EmailService sends transactional email.
type EmailService struct {
	cfg  *config.EmailConfig
	db   *database.DB
	log  *slog.Logger
	gate ConsentGate
}

// NewEmailService builds the email service. A disabled configuration yields a
// service whose methods report ErrDisabled rather than failing startup, so the
// platform runs fully without mail.
//
// The gate is taken here rather than set later so that building a transport is
// the same act as deciding who it must ask before it sends. A nil gate is
// permitted only for a transport that never sends — the extraction smoke test —
// and is documented on ConsentGate.
func NewEmailService(cfg *config.EmailConfig, db *database.DB, log *slog.Logger, gate ConsentGate) *EmailService {
	return &EmailService{cfg: cfg, db: db, log: log, gate: gate}
}

// ErrDisabled is returned when a notification transport is not configured.
var ErrDisabled = errors.New("email notifications are not configured")

// Enabled reports whether email is configured.
// Enabled reports whether the transport is configured and switched on.
//
// It tolerates a nil receiver so that a service that was never constructed reports
// "off" rather than panicking. A capability report must not be able to crash the
// diagnostics endpoint because one optional subsystem was left nil.
func (s *EmailService) Enabled() bool { return s != nil && s.cfg != nil && s.cfg.Enabled }

// ValidateConfig checks internal consistency.
func (s *EmailService) ValidateConfig() error {
	if s.cfg == nil {
		return ErrDisabled
	}
	if !s.cfg.Enabled {
		return ErrDisabled
	}
	if strings.TrimSpace(s.cfg.Host) == "" {
		return errors.New("SMTP host is required")
	}
	if s.cfg.Port <= 0 || s.cfg.Port > 65535 {
		return errors.New("SMTP port must be between 1 and 65535")
	}
	if strings.TrimSpace(s.cfg.From) == "" {
		return errors.New("a From address is required")
	}
	if (s.cfg.Username == "") != (s.cfg.Password == "") {
		return errors.New("SMTP username and password must be set together")
	}
	return nil
}

// TestConnection opens a connection and, when credentials are configured,
// authenticates. It does not send a message.
func (s *EmailService) TestConnection(ctx context.Context) error {
	if err := s.ValidateConfig(); err != nil {
		return err
	}
	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprint(s.cfg.Port))
	dialer := &net.Dialer{Timeout: s.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()

	if s.cfg.UseTLS && !s.cfg.StartTLS {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: s.cfg.Host,
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("TLS handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer client.Close()

	if s.cfg.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("STARTTLS: %w", err)
			}
		} else if s.cfg.UseTLS {
			return errors.New("the server does not offer STARTTLS but TLS is required")
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	return client.Quit()
}

// Message is one email to send.
//
// Purpose and ActorUserID exist so the transport can ask the consent policy the
// two questions it needs: what the message is about, and who is accountable for
// it. A caller that omits Purpose gets the strict default rather than an implicit
// pass, so the field is safe to leave empty and unsafe to fill carelessly.
type Message struct {
	To       []string
	Subject  string
	HTMLBody string
	TextBody string
	Template string
	OrgID    *uuid.UUID
	// Purpose is what the message is about, in the consent vocabulary. Empty means
	// the strict default (marketing).
	Purpose string
	// ActorUserID is the user the send is attributed to in the audit trail, when
	// there is one. A message with no user behind it — a password reset triggered by
	// the person themselves — leaves it nil and the policy records the actor as
	// "system".
	ActorUserID *uuid.UUID
}

// Send delivers a message and records the attempt in the send log.
//
// The success value reflects the SMTP server accepting the message. A local
// queue acceptance is not reported as delivery.
//
// Consent is checked here, inside the transport, before the send log is written
// and before any connection is opened. Every caller therefore inherits the check:
// the manual endpoint, the queued-send worker and any future sender cannot reach
// SMTP without the policy having answered for each recipient. A refusal returns
// ErrConsentBlocked and leaves no 'queued' row, so a blocked message never appears
// in the log as something that was attempted.
func (s *EmailService) Send(ctx context.Context, msg Message) error {
	if len(msg.To) == 0 {
		return errors.New("at least one recipient is required")
	}
	// Consent is asked before the transport is consulted, and this order is deliberate.
	// A message the policy forbids must be refused whether or not SMTP is configured:
	// answering "SMTP is not configured" would tell an operator the send would succeed
	// once mail is set up, which is the belief this gate exists to correct.
	//
	// Each recipient is its own subject. Checking only the first would let a caller put
	// an address with a grant first and an address without one second: the first would
	// answer for both. The first refusal stops the send before any recipient is reached.
	for _, to := range msg.To {
		if err := enforce(ctx, s.gate, msg.OrgID, msg.ActorUserID, to, msg.Purpose, ChannelEmail); err != nil {
			return err
		}
	}
	if !s.Enabled() {
		return ErrDisabled
	}
	entryIDs, err := s.logQueued(ctx, msg)
	if err != nil {
		return err
	}

	body := buildMIME(s.cfg.From, s.cfg.FromName, msg)
	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprint(s.cfg.Port))

	dialer := &net.Dialer{Timeout: s.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()

	if s.cfg.UseTLS && !s.cfg.StartTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			s.markFailed(ctx, entryIDs, err)
			return fmt.Errorf("TLS handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer client.Close()

	if s.cfg.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				s.markFailed(ctx, entryIDs, err)
				return fmt.Errorf("STARTTLS: %w", err)
			}
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			s.markFailed(ctx, entryIDs, err)
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("SMTP MAIL FROM rejected: %w", err)
	}
	for _, rcpt := range msg.To {
		if err := client.Rcpt(rcpt); err != nil {
			s.markFailed(ctx, entryIDs, err)
			return fmt.Errorf("SMTP RCPT TO %s rejected: %w", rcpt, err)
		}
	}
	wc, err := client.Data()
	if err != nil {
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("SMTP DATA rejected: %w", err)
	}
	if _, err := wc.Write(body); err != nil {
		wc.Close()
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("write message body: %w", err)
	}
	if err := wc.Close(); err != nil {
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("finish message body: %w", err)
	}
	if err := client.Quit(); err != nil {
		s.markFailed(ctx, entryIDs, err)
		return fmt.Errorf("SMTP QUIT: %w", err)
	}

	s.markSent(ctx, entryIDs)
	return nil
}

// logQueued writes one row per recipient. The log is read by an operator asking
// "who did we email", and one row naming only the first of several recipients
// would answer that question wrongly. Each address is its own row so the record
// matches the set the message actually went to.
func (s *EmailService) logQueued(ctx context.Context, msg Message) ([]int64, error) {
	const q = `
		INSERT INTO email_log (organization_id, to_address, subject, template, status)
		VALUES ($1, $2, $3, $4, 'queued') RETURNING id`
	ids := make([]int64, 0, len(msg.To))
	for _, to := range msg.To {
		var id int64
		if err := s.db.Pool().QueryRow(ctx, q, msg.OrgID, to, msg.Subject, msg.Template).Scan(&id); err != nil {
			return nil, fmt.Errorf("log email: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *EmailService) markSent(ctx context.Context, ids []int64) {
	const q = `UPDATE email_log SET status='sent', sent_at=now(), attempts=attempts+1 WHERE id=$1`
	for _, id := range ids {
		if _, err := s.db.Pool().Exec(ctx, q, id); err != nil {
			s.log.Warn("could not mark email as sent", "email_log_id", id, "error", err)
		}
	}
}

func (s *EmailService) markFailed(ctx context.Context, ids []int64, cause error) {
	msg := cause.Error()
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	const q = `UPDATE email_log SET status='failed', error=$2, attempts=attempts+1 WHERE id=$1`
	for _, id := range ids {
		if _, err := s.db.Pool().Exec(ctx, q, id, msg); err != nil {
			s.log.Warn("could not mark email as failed", "email_log_id", id, "error", err)
		}
	}
}

// buildMIME produces a multipart message with both a plain-text and an HTML part.
func buildMIME(from, fromName string, msg Message) []byte {
	boundary := "forgecrm-" + uuid.NewString()
	var b strings.Builder

	displayFrom := from
	if fromName != "" {
		displayFrom = fmt.Sprintf("%s <%s>", fromName, from)
	}
	fmt.Fprintf(&b, "From: %s\r\n", displayFrom)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(msg.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", sanitiseHeader(msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@forgecrm>\r\n", uuid.NewString())
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	text := msg.TextBody
	if text == "" {
		text = stripHTML(msg.HTMLBody)
	}
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(text)
	b.WriteString("\r\n\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(msg.HTMLBody)
	b.WriteString("\r\n\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// sanitiseHeader removes CR and LF so a crafted subject cannot inject headers.
func sanitiseHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// Collapse the whitespace that tag removal leaves behind.
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}

// ListSendLog returns the recorded send attempts.
func (s *EmailService) ListSendLog(ctx context.Context, orgID *uuid.UUID, limit, offset int) ([]map[string]any, int64, error) {
	args := []any{}
	where := "WHERE 1 = 1"
	if orgID != nil {
		args = append(args, *orgID)
		where += " AND organization_id = $1"
	}
	var total int64
	if err := s.db.Pool().QueryRow(ctx, "SELECT count(*) FROM email_log "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count email log: %w", err)
	}
	args = append(args, limit, offset)
	q := `SELECT id, to_address, subject, template, status, attempts, error, created_at, sent_at
	      FROM email_log ` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		len(args)-1, len(args))
	rows, err := s.db.Pool().Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list email log: %w", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id        int64
			to        string
			subject   string
			template  string
			status    string
			attempts  int
			errText   *string
			createdAt time.Time
			sentAt    *time.Time
		)
		if err := rows.Scan(&id, &to, &subject, &template, &status, &attempts, &errText, &createdAt, &sentAt); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"id": id, "to": to, "subject": subject, "template": template, "status": status,
			"attempts": attempts, "error": errText, "created_at": createdAt, "sent_at": sentAt,
		})
	}
	return out, total, rows.Err()
}
