// Package views implements saved list views: named, reusable filter/sort/column
// selections for the CRM list screens.
//
// A view is a document, not a new query language. The stored `filters` payload is
// exactly the set of list-query parameters the CRM list endpoints already accept,
// so applying a view means handing those parameters back to the same endpoint the
// screen would have called by hand. That is deliberate: it keeps one definition of
// what a filter means, and a view cannot name a filter no list endpoint honours.
//
// Visibility is one predicate — a caller sees a view when they own it or it is
// shared — and it is applied in the query rather than after the fetch, so a
// private view of another user is never loaded at all.
package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// EntityType is a CRM list screen a view can be saved for.
type EntityType string

// The supported entity types. The set is closed: a view is only useful if a list
// endpoint exists to apply it to, and the database CHECK constraint lists the same
// set, so an unsupported type is refused at both layers.
const (
	EntityAccounts EntityType = "accounts"
	EntityContacts EntityType = "contacts"
	EntityLeads    EntityType = "leads"
	EntityDeals    EntityType = "deals"
	EntityTasks    EntityType = "tasks"
	EntityTickets  EntityType = "tickets"
)

// supportedEntities is the closed set, used to validate a type before it reaches
// the database.
var supportedEntities = map[EntityType]struct{}{
	EntityAccounts: {}, EntityContacts: {}, EntityLeads: {},
	EntityDeals: {}, EntityTasks: {}, EntityTickets: {},
}

// Valid reports whether t is a supported entity type.
func (t EntityType) Valid() bool {
	_, ok := supportedEntities[t]
	return ok
}

// ErrNotFound is returned when a view does not exist in the caller's scope.
var ErrNotFound = errors.New("saved view not found")

// ErrForbidden is returned when a caller may see a view but not modify it (a
// shared view owned by someone else).
var ErrForbidden = errors.New("saved view belongs to another user")

// ValidationError describes why a view document was refused. It carries the
// offending field so the API can render a 422 with per-field detail, the same
// shape the CRM handlers use.
type ValidationError struct {
	Fields map[string]string
}

// Error implements error.
func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for k, v := range e.Fields {
		parts = append(parts, k+": "+v)
	}
	sort.Strings(parts)
	return "invalid saved view: " + strings.Join(parts, "; ")
}

// FilterSet is the stored filter document. Every field is optional; an empty
// FilterSet is a valid view that simply shows everything.
//
// The field names mirror the query parameters the list endpoints parse, so
// applying a view is mechanical and the two cannot drift into different meanings.
type FilterSet struct {
	Search          string   `json:"q,omitempty"`
	Status          string   `json:"status,omitempty"`
	OwnerUserID     string   `json:"owner_user_id,omitempty"`
	TeamID          string   `json:"team_id,omitempty"`
	AccountID       string   `json:"account_id,omitempty"`
	PipelineID      string   `json:"pipeline_id,omitempty"`
	StageID         string   `json:"stage_id,omitempty"`
	AssigneeID      string   `json:"assignee_id,omitempty"`
	From            string   `json:"from,omitempty"`
	To              string   `json:"to,omitempty"`
	IncludeArchived bool     `json:"include_archived,omitempty"`
	Sort            string   `json:"sort,omitempty"`
	Columns         []string `json:"columns,omitempty"`
}

// View is a saved list view.
type View struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	OwnerUserID    uuid.UUID `json:"owner_user_id"`
	EntityType     string    `json:"entity_type"`
	Name           string    `json:"name"`
	Shared         bool      `json:"shared"`
	Pinned         bool      `json:"pinned"`
	Filters        FilterSet `json:"filters"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	// IsMine is derived at read time so the client can decide whether to offer edit
	// and delete without a second request. It is not stored.
	IsMine bool `json:"is_mine"`
}

// Input is the create/update body.
type Input struct {
	EntityType string    `json:"entity_type"`
	Name       string    `json:"name"`
	Shared     bool      `json:"shared"`
	Pinned     bool      `json:"pinned"`
	Filters    FilterSet `json:"filters"`
}

// Service provides saved-view operations.
type Service struct {
	db    *database.DB
	audit *audit.Logger
}

// NewService builds the saved-views service.
func NewService(db *database.DB, auditLog *audit.Logger) *Service {
	return &Service{db: db, audit: auditLog}
}

const viewColumns = `id, organization_id, owner_user_id, entity_type, name, shared, pinned,
        filters, created_at, updated_at`

// validate checks an input and returns a per-field explanation when it is
// unusable. It is the same check the API performs before writing, kept here so any
// future caller cannot bypass it.
func validate(in Input) error {
	fields := map[string]string{}
	if !EntityType(in.EntityType).Valid() {
		fields["entity_type"] = "must be one of accounts, contacts, leads, deals, tasks, tickets"
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		fields["name"] = "is required"
	} else if len(name) > 100 {
		fields["name"] = "must be at most 100 characters"
	}
	// A view is a filter, so a stored identifier that is not a UUID would be a
	// filter the list endpoint silently drops. Refusing it on write is what keeps
	// "the view shows nothing" from being a mystery.
	for field, value := range map[string]string{
		"filters.owner_user_id": in.Filters.OwnerUserID,
		"filters.team_id":       in.Filters.TeamID,
		"filters.account_id":    in.Filters.AccountID,
		"filters.pipeline_id":   in.Filters.PipelineID,
		"filters.stage_id":      in.Filters.StageID,
		"filters.assignee_id":   in.Filters.AssigneeID,
	} {
		if value != "" {
			if _, err := uuid.Parse(value); err != nil {
				fields[field] = "must be a UUID"
			}
		}
	}
	if in.Filters.From != "" {
		if _, err := time.Parse(time.RFC3339, in.Filters.From); err != nil {
			fields["filters.from"] = "must be an RFC 3339 timestamp"
		}
	}
	if in.Filters.To != "" {
		if _, err := time.Parse(time.RFC3339, in.Filters.To); err != nil {
			fields["filters.to"] = "must be an RFC 3339 timestamp"
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// Create stores a new view owned by the caller.
func (s *Service) Create(ctx context.Context, p *rbac.Principal, in Input) (*View, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(in.Filters)
	if err != nil {
		return nil, fmt.Errorf("marshal view filters: %w", err)
	}
	var v View
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
                INSERT INTO saved_views (organization_id, owner_user_id, entity_type, name, shared, pinned, filters)
                VALUES ($1, $2, $3, $4, $5, $6, $7)
                RETURNING ` + viewColumns
		row := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, p.UserID, in.EntityType,
			strings.TrimSpace(in.Name), in.Shared, in.Pinned, payload)
		scanned, err := scanView(row)
		if err != nil {
			return err
		}
		v = *scanned
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionViewCreated),
			EntityType:     "saved_view",
			EntityID:       v.ID.String(),
			After:          map[string]any{"name": v.Name, "entity_type": v.EntityType, "shared": v.Shared},
		})
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, &ValidationError{Fields: map[string]string{
				"name": "you already have a view with this name for this screen",
			}}
		}
		return nil, fmt.Errorf("create saved view: %w", err)
	}
	v.IsMine = true
	return &v, nil
}

// List returns every view the caller may use for one entity type: their own, plus
// any shared with the organization. Pinned views come first.
func (s *Service) List(ctx context.Context, p *rbac.Principal, entityType EntityType) ([]View, error) {
	if !entityType.Valid() {
		return nil, &ValidationError{Fields: map[string]string{"entity_type": "unsupported"}}
	}
	const q = `
                SELECT ` + viewColumns + `
                FROM saved_views
                WHERE organization_id = $1 AND entity_type = $2 AND archived_at IS NULL
                  AND (owner_user_id = $3 OR shared = true)
                ORDER BY pinned DESC, lower(name) ASC`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, entityType, p.UserID)
	if err != nil {
		return nil, fmt.Errorf("list saved views: %w", err)
	}
	defer rows.Close()

	out := make([]View, 0)
	for rows.Next() {
		v, err := scanView(rows)
		if err != nil {
			return nil, err
		}
		v.IsMine = v.OwnerUserID == p.UserID
		out = append(out, *v)
	}
	return out, rows.Err()
}

// Get loads one view the caller may use.
func (s *Service) Get(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*View, error) {
	const q = `
                SELECT ` + viewColumns + `
                FROM saved_views
                WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL
                  AND (owner_user_id = $3 OR shared = true)`
	v, err := scanView(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, p.UserID))
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load saved view: %w", err)
	}
	v.IsMine = v.OwnerUserID == p.UserID
	return v, nil
}

// Update changes a view. Only the owner may update it: a shared view is visible to
// the organization but not editable by it, so an operator cannot rewrite a
// colleague's view under them.
func (s *Service) Update(ctx context.Context, p *rbac.Principal, id uuid.UUID, in Input) (*View, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(in.Filters)
	if err != nil {
		return nil, fmt.Errorf("marshal view filters: %w", err)
	}
	var v View
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
                UPDATE saved_views
                SET entity_type = $3, name = $4, shared = $5, pinned = $6, filters = $7
                WHERE id = $1 AND organization_id = $2 AND owner_user_id = $8 AND archived_at IS NULL
                RETURNING ` + viewColumns
		scanned, err := scanView(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.EntityType,
			strings.TrimSpace(in.Name), in.Shared, in.Pinned, payload, p.UserID))
		if err != nil {
			if database.IsNoRows(err) {
				return s.missReason(ctx, p, id)
			}
			return err
		}
		v = *scanned
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionViewUpdated),
			EntityType:     "saved_view",
			EntityID:       v.ID.String(),
			After:          map[string]any{"name": v.Name, "shared": v.Shared},
		})
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, &ValidationError{Fields: map[string]string{
				"name": "you already have a view with this name for this screen",
			}}
		}
		return nil, err
	}
	v.IsMine = true
	return &v, nil
}

// missReason distinguishes "no such view" from "not yours", so the API can answer
// 404 and 403 respectively. The distinction matters: a shared view exists and the
// caller may read it, so telling them it does not exist would be a lie.
func (s *Service) missReason(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	var owner uuid.UUID
	const q = `SELECT owner_user_id FROM saved_views
               WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL`
	err := s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID).Scan(&owner)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("look up saved view owner: %w", err)
	}
	if owner == p.UserID {
		return ErrNotFound
	}
	return ErrForbidden
}

// Delete archives a view. Only the owner may delete it, on the same reasoning as
// Update. It is an archive rather than a hard delete so that a view a colleague
// was relying on can be restored, and so the audit trail keeps pointing at a row.
func (s *Service) Delete(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
                UPDATE saved_views SET archived_at = now()
                WHERE id = $1 AND organization_id = $2 AND owner_user_id = $3 AND archived_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID, p.UserID)
		if err != nil {
			return fmt.Errorf("archive saved view: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return s.missReason(ctx, p, id)
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionViewDeleted),
			EntityType:     "saved_view",
			EntityID:       id.String(),
		})
	})
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanView(row rowScanner) (*View, error) {
	var (
		v       View
		payload []byte
	)
	if err := row.Scan(&v.ID, &v.OrganizationID, &v.OwnerUserID, &v.EntityType, &v.Name,
		&v.Shared, &v.Pinned, &payload, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &v.Filters); err != nil {
			return nil, fmt.Errorf("decode view filters: %w", err)
		}
	}
	return &v, nil
}
