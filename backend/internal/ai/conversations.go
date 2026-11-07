package ai

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/google/uuid"
)

// Conversation is private to one user within an organization.
type Conversation struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	UserID         uuid.UUID  `json:"user_id"`
	Title          string     `json:"title"`
	Pinned         bool       `json:"pinned"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ConversationMessage is user-visible history, separate from technical logs.
type ConversationMessage struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	UserID         uuid.UUID `json:"user_id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

const conversationColumns = `id, organization_id, user_id, title, pinned, archived_at, created_at, updated_at`
const messageColumns = `id, organization_id, user_id, conversation_id, role, content, created_at`

// conversationAccess enforces permission even for callers outside the HTTP layer.
func conversationAccess(p *rbac.Principal) error {
	if p == nil || p.UserID == uuid.Nil || p.OrganizationID == uuid.Nil || !p.Has(rbac.PermAIToolsExecute) {
		return rbac.ErrForbidden
	}
	return nil
}

func conversationTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > 200 {
		return "", httpx.ValidationError(map[string]string{"title": "must contain 1 to 200 characters"})
	}
	return title, nil
}

func scanConversation(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.OrganizationID, &c.UserID, &c.Title, &c.Pinned, &c.ArchivedAt, &c.CreatedAt, &c.UpdatedAt)
	if database.IsNoRows(err) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read conversation: %w", err)
	}
	return &c, nil
}

// CreateConversation creates an empty private conversation without invoking a model.
func (s *Service) CreateConversation(ctx context.Context, p *rbac.Principal, title string) (*Conversation, error) {
	if err := conversationAccess(p); err != nil {
		return nil, err
	}
	title, err := conversationTitle(title)
	if err != nil {
		return nil, err
	}
	return scanConversation(s.db.Q(ctx).QueryRow(ctx, `INSERT INTO ai_conversations (organization_id,user_id,title) VALUES ($1,$2,$3) RETURNING `+conversationColumns, p.OrganizationID, p.UserID, title))
}

// GetConversation does not disclose whether another user's ID exists.
func (s *Service) GetConversation(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Conversation, error) {
	if err := conversationAccess(p); err != nil {
		return nil, err
	}
	return scanConversation(s.db.Q(ctx).QueryRow(ctx, `SELECT `+conversationColumns+` FROM ai_conversations WHERE id=$1 AND organization_id=$2 AND user_id=$3`, id, p.OrganizationID, p.UserID))
}

// ListConversations returns a bounded page of the owner's active or archived conversations.
func (s *Service) ListConversations(ctx context.Context, p *rbac.Principal, archived bool, limit, offset int) ([]Conversation, error) {
	if err := conversationAccess(p); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, httpx.ValidationError(map[string]string{"pagination": "invalid limit or offset"})
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+conversationColumns+` FROM ai_conversations WHERE organization_id=$1 AND user_id=$2 AND (archived_at IS NOT NULL)=$3 ORDER BY pinned DESC, updated_at DESC, id DESC LIMIT $4 OFFSET $5`, p.OrganizationID, p.UserID, archived, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	items := []Conversation{}
	for rows.Next() {
		item, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// UpdateConversation changes only metadata, never messages or CRM data.
func (s *Service) UpdateConversation(ctx context.Context, p *rbac.Principal, id uuid.UUID, title string, pinned, archived bool) (*Conversation, error) {
	if err := conversationAccess(p); err != nil {
		return nil, err
	}
	title, err := conversationTitle(title)
	if err != nil {
		return nil, err
	}
	return scanConversation(s.db.Q(ctx).QueryRow(ctx, `UPDATE ai_conversations SET title=$4, pinned=$5, archived_at=CASE WHEN $6 THEN COALESCE(archived_at, now()) ELSE NULL END, updated_at=now() WHERE id=$1 AND organization_id=$2 AND user_id=$3 RETURNING `+conversationColumns, id, p.OrganizationID, p.UserID, title, pinned, archived))
}

// DeleteConversation removes the private history and its messages atomically.
func (s *Service) DeleteConversation(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	if err := conversationAccess(p); err != nil {
		return err
	}
	result, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM ai_conversations WHERE id=$1 AND organization_id=$2 AND user_id=$3`, id, p.OrganizationID, p.UserID)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if result.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// AppendConversationMessage is for trusted server code, not model-controlled role
// selection. It stores a message only while the caller owns an active conversation.
func (s *Service) AppendConversationMessage(ctx context.Context, p *rbac.Principal, id uuid.UUID, role, content string) (*ConversationMessage, error) {
	if err := conversationAccess(p); err != nil {
		return nil, err
	}
	if (role != "user" && role != "assistant") || !utf8.ValidString(content) || strings.TrimSpace(content) == "" || utf8.RuneCountInString(content) > 16000 {
		return nil, httpx.ValidationError(map[string]string{"message": "invalid role or content"})
	}
	var out ConversationMessage
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// The update locks the parent through insertion, serializing archive/delete.
		result, err := s.db.Q(ctx).Exec(ctx, `UPDATE ai_conversations SET updated_at=clock_timestamp() WHERE id=$1 AND organization_id=$2 AND user_id=$3 AND archived_at IS NULL`, id, p.OrganizationID, p.UserID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		err = s.db.Q(ctx).QueryRow(ctx, `INSERT INTO ai_messages (organization_id,user_id,conversation_id,role,content,created_at) VALUES ($1,$2,$3,$4,$5,clock_timestamp()) RETURNING `+messageColumns, p.OrganizationID, p.UserID, id, role, content).Scan(&out.ID, &out.OrganizationID, &out.UserID, &out.ConversationID, &out.Role, &out.Content, &out.CreatedAt)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("append conversation message: %w", err)
	}
	return &out, nil
}

// ListConversationMessages returns a bounded chronological page for the owner.
func (s *Service) ListConversationMessages(ctx context.Context, p *rbac.Principal, id uuid.UUID, limit, offset int) ([]ConversationMessage, error) {
	if err := conversationAccess(p); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, httpx.ValidationError(map[string]string{"pagination": "invalid limit or offset"})
	}
	if _, err := s.GetConversation(ctx, p, id); err != nil {
		return nil, err
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+messageColumns+` FROM ai_messages WHERE organization_id=$1 AND user_id=$2 AND conversation_id=$3 ORDER BY created_at, id LIMIT $4 OFFSET $5`, p.OrganizationID, p.UserID, id, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	defer rows.Close()
	items := []ConversationMessage{}
	for rows.Next() {
		var m ConversationMessage
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
