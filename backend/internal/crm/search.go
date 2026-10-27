package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

// Tag is a coloured label.
type Tag struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

// ListTags returns the organization's tags.
func (s *Service) ListTags(ctx context.Context, p *rbac.Principal) ([]Tag, error) {
	const q = `SELECT id, name, color, created_at FROM tags WHERE organization_id = $1 ORDER BY name`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTag inserts a tag.
func (s *Service) CreateTag(ctx context.Context, p *rbac.Principal, name, color string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, httpx.ValidationError(map[string]string{"name": "name is required"})
	}
	if color == "" {
		color = "#64748b"
	}
	var t Tag
	const q = `
		INSERT INTO tags (organization_id, name, color) VALUES ($1,$2,$3)
		RETURNING id, name, color, created_at`
	if err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, name, color).Scan(
		&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
		return nil, wrapConstraint(err, "tag")
	}
	return &t, nil
}

// validTaggableTypes is the allowlist of entities that can carry tags.
var validTaggableTypes = map[string]bool{
	"account": true, "contact": true, "lead": true, "deal": true, "ticket": true, "task": true,
}

// TagEntity attaches a tag to a record.
func (s *Service) TagEntity(ctx context.Context, p *rbac.Principal, tagID uuid.UUID, entityType string, entityID uuid.UUID) error {
	if !validTaggableTypes[entityType] {
		return httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// Verify the tag belongs to the caller's organization.
		var exists int
		const tagQ = `SELECT 1 FROM tags WHERE id = $1 AND organization_id = $2`
		if err := s.db.Q(ctx).QueryRow(ctx, tagQ, tagID, p.OrganizationID).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("check tag scope: %w", err)
		}
		const q = `
			INSERT INTO taggables (tag_id, entity_type, entity_id) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING`
		if _, err := s.db.Q(ctx).Exec(ctx, q, tagID, entityType, entityID); err != nil {
			return fmt.Errorf("tag entity: %w", err)
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "tags.attach", EntityType: entityType, EntityID: entityID.String(),
			Metadata: map[string]any{"tag_id": tagID},
		})
	})
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

// Comment is a note attached to any entity.
type Comment struct {
	ID           uuid.UUID  `json:"id"`
	EntityType   string     `json:"entity_type"`
	EntityID     uuid.UUID  `json:"entity_id"`
	Body         string     `json:"body"`
	AuthorUserID *uuid.UUID `json:"author_user_id,omitempty"`
	AuthorName   *string    `json:"author_name,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// ListComments returns comments on an entity.
func (s *Service) ListComments(ctx context.Context, p *rbac.Principal, entityType string, entityID uuid.UUID) ([]Comment, error) {
	if !validTaggableTypes[entityType] {
		return nil, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	const q = `
		SELECT c.id, c.entity_type, c.entity_id, c.body, c.author_user_id, u.display_name, c.created_at
		FROM comments c
		LEFT JOIN users u ON u.id = c.author_user_id
		WHERE c.organization_id = $1 AND c.entity_type = $2 AND c.entity_id = $3 AND c.archived_at IS NULL
		ORDER BY c.created_at ASC`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.EntityType, &c.EntityID, &c.Body, &c.AuthorUserID,
			&c.AuthorName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateComment adds a comment.
func (s *Service) CreateComment(ctx context.Context, p *rbac.Principal, entityType string, entityID uuid.UUID, body string) (*Comment, error) {
	if !validTaggableTypes[entityType] {
		return nil, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, httpx.ValidationError(map[string]string{"body": "comment must not be empty"})
	}
	var c Comment
	const q = `
		INSERT INTO comments (organization_id, entity_type, entity_id, body, author_user_id)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, entity_type, entity_id, body, author_user_id, created_at`
	if err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, entityType, entityID, body, p.UserID).
		Scan(&c.ID, &c.EntityType, &c.EntityID, &c.Body, &c.AuthorUserID, &c.CreatedAt); err != nil {
		return nil, wrapConstraint(err, "comment")
	}
	return &c, nil
}

// ---------------------------------------------------------------------------
// Custom fields
// ---------------------------------------------------------------------------

// CustomFieldDefinition describes a tenant-defined field.
type CustomFieldDefinition struct {
	ID         uuid.UUID `json:"id"`
	EntityType string    `json:"entity_type"`
	Key        string    `json:"key"`
	Label      string    `json:"label"`
	FieldType  string    `json:"field_type"`
	Options    JSONMap   `json:"options"`
	Required   bool      `json:"required"`
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
}

var validCustomFieldTypes = map[string]bool{
	"text": true, "number": true, "boolean": true, "date": true, "select": true, "multiselect": true,
}

// ListCustomFields returns definitions for an entity type.
func (s *Service) ListCustomFields(ctx context.Context, p *rbac.Principal, entityType string) ([]CustomFieldDefinition, error) {
	if !validTaggableTypes[entityType] {
		return nil, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	const q = `
		SELECT id, entity_type, key, label, field_type, options, required, position, created_at
		FROM custom_field_definitions
		WHERE organization_id = $1 AND entity_type = $2 AND archived_at IS NULL
		ORDER BY position, key`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, entityType)
	if err != nil {
		return nil, fmt.Errorf("list custom fields: %w", err)
	}
	defer rows.Close()
	var out []CustomFieldDefinition
	for rows.Next() {
		var d CustomFieldDefinition
		if err := rows.Scan(&d.ID, &d.EntityType, &d.Key, &d.Label, &d.FieldType,
			&d.Options, &d.Required, &d.Position, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateCustomField defines a new field.
func (s *Service) CreateCustomField(ctx context.Context, p *rbac.Principal, d CustomFieldDefinition) (*CustomFieldDefinition, error) {
	details := map[string]string{}
	if !validTaggableTypes[d.EntityType] {
		details["entity_type"] = "unsupported entity type"
	}
	if !validCustomFieldTypes[d.FieldType] {
		details["field_type"] = "must be one of text, number, boolean, date, select, multiselect"
	}
	key := strings.ToLower(strings.TrimSpace(d.Key))
	if key == "" || !isValidFieldKey(key) {
		details["key"] = "must start with a letter and contain only lowercase letters, digits and underscores"
	}
	if strings.TrimSpace(d.Label) == "" {
		details["label"] = "label is required"
	}
	if len(details) > 0 {
		return nil, httpx.ValidationError(details)
	}
	var out CustomFieldDefinition
	const q = `
		INSERT INTO custom_field_definitions
			(organization_id, entity_type, key, label, field_type, options, required, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, entity_type, key, label, field_type, options, required, position, created_at`
	if err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, d.EntityType, key, d.Label,
		d.FieldType, orEmptyJSON(d.Options), d.Required, d.Position).Scan(
		&out.ID, &out.EntityType, &out.Key, &out.Label, &out.FieldType, &out.Options,
		&out.Required, &out.Position, &out.CreatedAt); err != nil {
		return nil, wrapConstraint(err, "custom field")
	}
	return &out, nil
}

func isValidFieldKey(k string) bool {
	if k == "" {
		return false
	}
	if k[0] < 'a' || k[0] > 'z' {
		return false
	}
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

// Notification is an in-app message for a user.
type Notification struct {
	ID         uuid.UUID  `json:"id"`
	Type       string     `json:"type"`
	Title      string     `json:"title"`
	Body       *string    `json:"body,omitempty"`
	EntityType *string    `json:"entity_type,omitempty"`
	EntityID   *uuid.UUID `json:"entity_id,omitempty"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListNotifications returns a user's notifications.
func (s *Service) ListNotifications(ctx context.Context, p *rbac.Principal, unreadOnly bool, limit, offset int) ([]Notification, int64, error) {
	where := "WHERE organization_id = $1 AND user_id = $2"
	args := []any{p.OrganizationID, p.UserID}
	if unreadOnly {
		where += " AND read_at IS NULL"
	}
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM notifications "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}
	args = append(args, limit, offset)
	q := `
		SELECT id, type, title, body, entity_type, entity_id, read_at, created_at
		FROM notifications ` + where + `
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`
	rows, err := s.db.Q(ctx).Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.EntityType, &n.EntityID,
			&n.ReadAt, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

// MarkNotificationRead marks one notification as read.
func (s *Service) MarkNotificationRead(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	const q = `UPDATE notifications SET read_at = now() WHERE id = $1 AND user_id = $2 AND organization_id = $3`
	tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.UserID, p.OrganizationID)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateNotification inserts a notification for a user.
func (s *Service) CreateNotification(ctx context.Context, orgID, userID uuid.UUID, typ, title string, body *string, entityType *string, entityID *uuid.UUID) error {
	const q = `
		INSERT INTO notifications (organization_id, user_id, type, title, body, entity_type, entity_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`
	if _, err := s.db.Q(ctx).Exec(ctx, q, orgID, userID, typ, title, body, entityType, entityID); err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// SearchResult is one hit in a cross-entity search.
type SearchResult struct {
	EntityType string    `json:"entity_type"`
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Subtitle   string    `json:"subtitle,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Search queries accounts, contacts, leads, deals and tickets in one call.
//
// Only entity types the caller can read are searched, so the result set cannot
// disclose the existence of records the caller has no access to.
func (s *Service) Search(ctx context.Context, p *rbac.Principal, term string, limit int) ([]SearchResult, error) {
	term = strings.TrimSpace(term)
	if len(term) < 2 {
		return nil, httpx.ValidationError(map[string]string{"q": "search term must be at least 2 characters"})
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	like := "%" + term + "%"

	type source struct {
		permission rbac.Permission
		entityType string
		sql        string
	}
	sources := []source{
		{rbac.PermAccountsRead, "account", `
			SELECT id, name, COALESCE(email::text,''), updated_at FROM accounts
			WHERE organization_id = $1 AND archived_at IS NULL AND (name ILIKE $2 OR COALESCE(email::text,'') ILIKE $2)
			ORDER BY updated_at DESC LIMIT $3`},
		{rbac.PermContactsRead, "contact", `
			SELECT id, COALESCE(NULLIF(TRIM(COALESCE(first_name,'') || ' ' || COALESCE(last_name,'')), ''), COALESCE(email::text,'')),
			       COALESCE(email::text,''), updated_at FROM contacts
			WHERE organization_id = $1 AND archived_at IS NULL
			  AND (COALESCE(first_name,'') ILIKE $2 OR COALESCE(last_name,'') ILIKE $2 OR COALESCE(email::text,'') ILIKE $2)
			ORDER BY updated_at DESC LIMIT $3`},
		{rbac.PermLeadsRead, "lead", `
			SELECT id, COALESCE(NULLIF(title,''), COALESCE(company,'')), COALESCE(email::text,''), updated_at FROM leads
			WHERE organization_id = $1 AND archived_at IS NULL
			  AND (title ILIKE $2 OR COALESCE(company,'') ILIKE $2 OR COALESCE(email::text,'') ILIKE $2)
			ORDER BY updated_at DESC LIMIT $3`},
		{rbac.PermDealsRead, "deal", `
			SELECT id, title, COALESCE(currency,'') || ' ' || amount::text, updated_at FROM deals
			WHERE organization_id = $1 AND archived_at IS NULL AND title ILIKE $2
			ORDER BY updated_at DESC LIMIT $3`},
		{rbac.PermTicketsRead, "ticket", `
			SELECT id, subject, COALESCE(status,''), updated_at FROM tickets
			WHERE organization_id = $1 AND archived_at IS NULL AND subject ILIKE $2
			ORDER BY updated_at DESC LIMIT $3`},
	}

	var out []SearchResult
	for _, src := range sources {
		if !p.Has(src.permission) {
			continue
		}
		rows, err := s.db.Q(ctx).Query(ctx, src.sql, p.OrganizationID, like, limit)
		if err != nil {
			return nil, fmt.Errorf("search %s: %w", src.entityType, err)
		}
		for rows.Next() {
			var r SearchResult
			r.EntityType = src.entityType
			if err := rows.Scan(&r.ID, &r.Title, &r.Subtitle, &r.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Reports
// ---------------------------------------------------------------------------

// PipelineReport summarises a pipeline's value by stage.
type PipelineReport struct {
	PipelineID   uuid.UUID           `json:"pipeline_id"`
	PipelineName string              `json:"pipeline_name"`
	Currency     string              `json:"currency"`
	Stages       []PipelineStageStat `json:"stages"`
	TotalOpen    float64             `json:"total_open"`
	TotalWon     float64             `json:"total_won"`
	TotalLost    float64             `json:"total_lost"`
}

// PipelineStageStat is one row of a pipeline report.
type PipelineStageStat struct {
	StageID        uuid.UUID `json:"stage_id"`
	StageName      string    `json:"stage_name"`
	DealCount      int64     `json:"deal_count"`
	TotalAmount    float64   `json:"total_amount"`
	WeightedAmount float64   `json:"weighted_amount"`
}

// PipelineReportFor builds a pipeline report for one pipeline.
func (s *Service) PipelineReportFor(ctx context.Context, p *rbac.Principal, pipelineID uuid.UUID) (*PipelineReport, error) {
	report := &PipelineReport{PipelineID: pipelineID}
	const head = `SELECT name, currency FROM pipelines WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL`
	if err := s.db.Q(ctx).QueryRow(ctx, head, pipelineID, p.OrganizationID).
		Scan(&report.PipelineName, &report.Currency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load pipeline: %w", err)
	}

	const q = `
		SELECT s.id, s.name, count(d.id),
		       COALESCE(sum(d.amount), 0),
		       COALESCE(sum(d.amount * s.probability / 100.0), 0),
		       s.is_won, s.is_lost
		FROM pipeline_stages s
		LEFT JOIN deals d ON d.stage_id = s.id AND d.archived_at IS NULL AND d.status = 'open'
		WHERE s.pipeline_id = $1 AND s.organization_id = $2
		GROUP BY s.id, s.name, s.position, s.is_won, s.is_lost
		ORDER BY s.position`
	rows, err := s.db.Q(ctx).Query(ctx, q, pipelineID, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("pipeline report: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st PipelineStageStat
		var isWon, isLost bool
		if err := rows.Scan(&st.StageID, &st.StageName, &st.DealCount, &st.TotalAmount,
			&st.WeightedAmount, &isWon, &isLost); err != nil {
			return nil, err
		}
		report.Stages = append(report.Stages, st)
		report.TotalOpen += st.TotalAmount
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const totals = `
		SELECT d.status, COALESCE(sum(d.amount),0)
		FROM deals d
		WHERE d.organization_id = $1 AND d.pipeline_id = $2 AND d.archived_at IS NULL
		  AND d.status IN ('won','lost')
		GROUP BY d.status`
	rows2, err := s.db.Q(ctx).Query(ctx, totals, p.OrganizationID, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("pipeline totals: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var status string
		var amount float64
		if err := rows2.Scan(&status, &amount); err != nil {
			return nil, err
		}
		switch status {
		case "won":
			report.TotalWon = amount
		case "lost":
			report.TotalLost = amount
		}
	}
	return report, rows2.Err()
}

// SalesSummaryReport is an aggregate view of deals over a period.
type SalesSummaryReport struct {
	From          *time.Time `json:"from,omitempty"`
	To            *time.Time `json:"to,omitempty"`
	DealsCreated  int64      `json:"deals_created"`
	DealsWon      int64      `json:"deals_won"`
	DealsLost     int64      `json:"deals_lost"`
	WonValue      float64    `json:"won_value"`
	LostValue     float64    `json:"lost_value"`
	OpenValue     float64    `json:"open_value"`
	OpenCount     int64      `json:"open_count"`
	AvgDealAmount float64    `json:"avg_deal_amount"`
	WinRate       float64    `json:"win_rate"`
}

// SalesSummary builds the sales summary report.
func (s *Service) SalesSummary(ctx context.Context, p *rbac.Principal, from, to *time.Time) (*SalesSummaryReport, error) {
	rep := &SalesSummaryReport{From: from, To: to}
	const q = `
		SELECT
			COALESCE(count(*) FILTER (WHERE created_at >= COALESCE($2, 'epoch'::timestamptz)
			                                AND created_at <= COALESCE($3, now())), 0),
			COALESCE(count(*) FILTER (WHERE status = 'won'), 0),
			COALESCE(count(*) FILTER (WHERE status = 'lost'), 0),
			COALESCE(count(*) FILTER (WHERE status = 'open'), 0),
			COALESCE(sum(amount) FILTER (WHERE status = 'won'), 0),
			COALESCE(sum(amount) FILTER (WHERE status = 'lost'), 0),
			COALESCE(sum(amount) FILTER (WHERE status = 'open'), 0),
			COALESCE(avg(amount), 0)
		FROM deals
		WHERE organization_id = $1 AND archived_at IS NULL`
	if err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, from, to).Scan(
		&rep.DealsCreated, &rep.DealsWon, &rep.DealsLost, &rep.OpenCount,
		&rep.WonValue, &rep.LostValue, &rep.OpenValue, &rep.AvgDealAmount); err != nil {
		return nil, fmt.Errorf("sales summary: %w", err)
	}
	closed := rep.DealsWon + rep.DealsLost
	if closed > 0 {
		rep.WinRate = float64(rep.DealsWon) / float64(closed) * 100
	}
	return rep, nil
}

// SupportSummaryReport aggregates ticket metrics.
type SupportSummaryReport struct {
	Total              int64            `json:"total"`
	Open               int64            `json:"open"`
	Resolved           int64            `json:"resolved"`
	SLABreached        int64            `json:"sla_breached"`
	AvgResolutionHours float64          `json:"avg_resolution_hours"`
	ByPriority         map[string]int64 `json:"by_priority"`
}

// SupportSummary builds the support summary report.
func (s *Service) SupportSummary(ctx context.Context, p *rbac.Principal) (*SupportSummaryReport, error) {
	rep := &SupportSummaryReport{ByPriority: map[string]int64{}}
	const q = `
		SELECT
			count(*),
			count(*) FILTER (WHERE status IN ('new','open','pending')),
			count(*) FILTER (WHERE status IN ('resolved','closed')),
			count(*) FILTER (WHERE sla_breached),
			COALESCE(avg(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 3600.0)
			         FILTER (WHERE resolved_at IS NOT NULL), 0)
		FROM tickets WHERE organization_id = $1 AND archived_at IS NULL`
	if err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID).Scan(
		&rep.Total, &rep.Open, &rep.Resolved, &rep.SLABreached, &rep.AvgResolutionHours); err != nil {
		return nil, fmt.Errorf("support summary: %w", err)
	}
	const byPriority = `
		SELECT priority, count(*) FROM tickets
		WHERE organization_id = $1 AND archived_at IS NULL GROUP BY priority`
	rows, err := s.db.Q(ctx).Query(ctx, byPriority, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("support by priority: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var prio string
		var n int64
		if err := rows.Scan(&prio, &n); err != nil {
			return nil, err
		}
		rep.ByPriority[prio] = n
	}
	return rep, rows.Err()
}
