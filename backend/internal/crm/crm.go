// Package crm implements the CRM domain: accounts, contacts, leads, pipelines,
// deals, tasks, activities, tickets, tags, comments, custom fields, search and
// reports.
//
// Every method takes an explicit organization scope and builds it into the SQL
// predicate. Handlers never receive a row that was not fetched through a scoped
// query, which is what makes cross-tenant access a bug rather than a possibility.
package crm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Service provides CRM operations.
type Service struct {
	db     *database.DB
	audit  *audit.Logger
	outbox *outbox.Store
}

// NewService builds the CRM service.
func NewService(db *database.DB, auditLog *audit.Logger, ob *outbox.Store) *Service {
	return &Service{db: db, audit: auditLog, outbox: ob}
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

// Account is a client organisation.
type Account struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	LegalName      *string    `json:"legal_name,omitempty"`
	Industry       *string    `json:"industry,omitempty"`
	Website        *string    `json:"website,omitempty"`
	Phone          *string    `json:"phone,omitempty"`
	Email          *string    `json:"email,omitempty"`
	TaxID          *string    `json:"tax_id,omitempty"`
	AddressLine1   *string    `json:"address_line1,omitempty"`
	City           *string    `json:"city,omitempty"`
	Region         *string    `json:"region,omitempty"`
	PostalCode     *string    `json:"postal_code,omitempty"`
	Country        *string    `json:"country,omitempty"`
	AnnualRevenue  *float64   `json:"annual_revenue,omitempty"`
	EmployeeCount  *int       `json:"employee_count,omitempty"`
	OwnerUserID    *uuid.UUID `json:"owner_user_id,omitempty"`
	TeamID         *uuid.UUID `json:"team_id,omitempty"`
	ExternalSource *string    `json:"external_source,omitempty"`
	ExternalID     *string    `json:"external_id,omitempty"`
	CustomFields   JSONMap    `json:"custom_fields"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

// JSONMap is a jsonb column.
type JSONMap map[string]any

// Value implements driver.Valuer.
func (m JSONMap) Value() (any, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

// Scan implements sql.Scanner for jsonb columns.
func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = JSONMap{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONMap", src)
	}
	if len(b) == 0 {
		*m = JSONMap{}
		return nil
	}
	return json.Unmarshal(b, m)
}

const accountColumns = `
	id, organization_id, name, legal_name, industry, website, phone, email, tax_id,
	address_line1, city, region, postal_code, country, annual_revenue, employee_count,
	owner_user_id, team_id, external_source, external_id, custom_fields,
	created_at, updated_at, archived_at`

func scanAccount(row rowScanner) (*Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.OrganizationID, &a.Name, &a.LegalName, &a.Industry, &a.Website,
		&a.Phone, &a.Email, &a.TaxID, &a.AddressLine1, &a.City, &a.Region, &a.PostalCode,
		&a.Country, &a.AnnualRevenue, &a.EmployeeCount, &a.OwnerUserID, &a.TeamID,
		&a.ExternalSource, &a.ExternalID, &a.CustomFields, &a.CreatedAt, &a.UpdatedAt, &a.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AccountInput is the writable projection of an account.
type AccountInput struct {
	Name           string     `json:"name"`
	LegalName      *string    `json:"legal_name"`
	Industry       *string    `json:"industry"`
	Website        *string    `json:"website"`
	Phone          *string    `json:"phone"`
	Email          *string    `json:"email"`
	TaxID          *string    `json:"tax_id"`
	AddressLine1   *string    `json:"address_line1"`
	City           *string    `json:"city"`
	Region         *string    `json:"region"`
	PostalCode     *string    `json:"postal_code"`
	Country        *string    `json:"country"`
	AnnualRevenue  *float64   `json:"annual_revenue"`
	EmployeeCount  *int       `json:"employee_count"`
	OwnerUserID    *uuid.UUID `json:"owner_user_id"`
	TeamID         *uuid.UUID `json:"team_id"`
	ExternalSource *string    `json:"external_source"`
	ExternalID     *string    `json:"external_id"`
	CustomFields   JSONMap    `json:"custom_fields"`
}

// Validate checks the input.
func (in *AccountInput) Validate() error {
	details := map[string]string{}
	if strings.TrimSpace(in.Name) == "" {
		details["name"] = "name is required"
	} else if len(in.Name) > 300 {
		details["name"] = "name must be at most 300 characters"
	}
	if in.Email != nil && *in.Email != "" && !looksLikeEmail(*in.Email) {
		details["email"] = "email is not valid"
	}
	if in.EmployeeCount != nil && *in.EmployeeCount < 0 {
		details["employee_count"] = "must not be negative"
	}
	if in.AnnualRevenue != nil && *in.AnnualRevenue < 0 {
		details["annual_revenue"] = "must not be negative"
	}
	if (in.ExternalSource == nil) != (in.ExternalID == nil) {
		details["external_id"] = "external_source and external_id must be provided together"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

// ListAccounts returns a filtered, paginated page of accounts.
func (s *Service) ListAccounts(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Account, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}

	if q.IncludeArchived {
		// no archive predicate
	} else {
		where = append(where, "archived_at IS NULL")
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR COALESCE(email,'') ILIKE $%d)", len(args), len(args)))
	}
	if q.OwnerID != nil {
		args = append(args, *q.OwnerID)
		where = append(where, fmt.Sprintf("owner_user_id = $%d", len(args)))
	}
	if q.TeamID != nil {
		args = append(args, *q.TeamID)
		where = append(where, fmt.Sprintf("team_id = $%d", len(args)))
	}
	// Record-level rule: a caller who cannot read all accounts only sees the ones
	// they own or that belong to their teams.
	if !p.Has(rbac.PermAccountsArchive) && len(p.TeamIDs) > 0 {
		args = append(args, p.UserID, p.TeamIDs)
		where = append(where, fmt.Sprintf("(owner_user_id = $%d OR team_id = ANY($%d))", len(args)-1, len(args)))
	}

	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM accounts WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count accounts: %w", err)
	}

	order := q.Sort.OrderClause("created_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	listSQL := "SELECT " + accountColumns + " FROM accounts WHERE " + whereSQL +
		fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args))

	rows, err := s.db.Q(ctx).Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

// GetAccount loads one account, scoped to the caller's organization.
func (s *Service) GetAccount(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Account, error) {
	const q = `SELECT ` + accountColumns + ` FROM accounts WHERE id = $1 AND organization_id = $2`
	a, err := scanAccount(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load account: %w", err)
	}
	return a, nil
}

// CreateAccount inserts an account and emits an event in the same transaction.
func (s *Service) CreateAccount(ctx context.Context, p *rbac.Principal, in AccountInput) (*Account, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Account
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO accounts (organization_id, name, legal_name, industry, website, phone,
				email, tax_id, address_line1, city, region, postal_code, country, annual_revenue,
				employee_count, owner_user_id, team_id, external_source, external_id, custom_fields, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
			RETURNING ` + accountColumns
		a, err := scanAccount(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.Name, in.LegalName,
			in.Industry, in.Website, in.Phone, in.Email, in.TaxID, in.AddressLine1, in.City,
			in.Region, in.PostalCode, in.Country, in.AnnualRevenue, in.EmployeeCount,
			in.OwnerUserID, in.TeamID, in.ExternalSource, in.ExternalID,
			orEmptyJSON(in.CustomFields), p.UserID))
		if err != nil {
			return wrapConstraint(err, "account")
		}
		created = a
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventAccountCreated, map[string]any{
			"account_id": a.ID, "name": a.Name,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "accounts.create", EntityType: "account", EntityID: a.ID.String(),
			After: map[string]any{"name": a.Name},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateAccount applies a partial update.
func (s *Service) UpdateAccount(ctx context.Context, p *rbac.Principal, id uuid.UUID, in AccountInput) (*Account, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var updated *Account
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		before, err := s.GetAccount(ctx, p, id)
		if err != nil {
			return err
		}
		const q = `
			UPDATE accounts SET name=$3, legal_name=$4, industry=$5, website=$6, phone=$7,
				email=$8, tax_id=$9, address_line1=$10, city=$11, region=$12, postal_code=$13,
				country=$14, annual_revenue=$15, employee_count=$16, owner_user_id=$17,
				team_id=$18, external_source=$19, external_id=$20, custom_fields=$21
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + accountColumns
		a, err := scanAccount(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.Name,
			in.LegalName, in.Industry, in.Website, in.Phone, in.Email, in.TaxID, in.AddressLine1,
			in.City, in.Region, in.PostalCode, in.Country, in.AnnualRevenue, in.EmployeeCount,
			in.OwnerUserID, in.TeamID, in.ExternalSource, in.ExternalID, orEmptyJSON(in.CustomFields)))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return wrapConstraint(err, "account")
		}
		updated = a
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventAccountUpdated, map[string]any{
			"account_id": a.ID, "name": a.Name,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "accounts.update", EntityType: "account", EntityID: id.String(),
			Before: map[string]any{"name": before.Name}, After: map[string]any{"name": a.Name},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ArchiveAccount archives an account without deleting its history.
func (s *Service) ArchiveAccount(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `UPDATE accounts SET archived_at = now() WHERE id = $1 AND organization_id = $2 AND archived_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("archive account: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventAccountArchived, map[string]any{
			"account_id": id,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "accounts.archive", EntityType: "account", EntityID: id.String(),
		})
	})
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// ListQuery is the common filter/pagination/sort request.
type ListQuery struct {
	Pagination      httpx.Pagination
	Sort            httpx.SortSpec
	Search          string
	OwnerID         *uuid.UUID
	TeamID          *uuid.UUID
	Status          string
	IncludeArchived bool
	AccountID       *uuid.UUID
	DealID          *uuid.UUID
	ContactID       *uuid.UUID
	TicketID        *uuid.UUID
	AssigneeID      *uuid.UUID
	PipelineID      *uuid.UUID
	From            *time.Time
	To              *time.Time
	Extra           map[string]string
}

// ErrNotFound is returned when a record does not exist in the caller's scope.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on a uniqueness violation.
var ErrConflict = errors.New("conflict")

type rowScanner interface {
	Scan(dest ...any) error
}

func orEmptyJSON(m JSONMap) JSONMap {
	if m == nil {
		return JSONMap{}
	}
	return m
}

func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	domain := s[at+1:]
	return strings.Contains(domain, ".") && !strings.ContainsAny(s, " \t\r\n")
}

// wrapConstraint converts a unique or foreign-key violation into a domain error
// with a message safe to return to the client.
func wrapConstraint(err error, entity string) error {
	switch {
	case database.IsUniqueViolation(err):
		return fmt.Errorf("%w: a %s with the same unique values already exists", ErrConflict, entity)
	case database.IsForeignKeyViolation(err):
		return httpx.NewError(422, httpx.CodeValidation, "referenced record does not exist or belongs to another organization")
	case database.IsCheckViolation(err):
		return httpx.NewError(422, httpx.CodeValidation, "a field violates a database constraint")
	default:
		return fmt.Errorf("write %s: %w", entity, err)
	}
}
