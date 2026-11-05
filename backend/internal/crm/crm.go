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
	// defaults is the organization's own configuration, used to resolve a default the
	// caller left unset. It is an interface declared here rather than a dependency on
	// the configuration service, so the arrow points from the configuration to the
	// domain and not the other way: the domain stays ignorant of how a default is
	// stored or versioned, and a test can supply a fixed one. A nil port means the
	// model-level defaults apply, which is what the extraction harness and a
	// single-tenant test want.
	defaults OrgDefaults
}

// OrgDefaults answers the organization's effective configuration.
//
// It is the narrow question the domain needs — "what is this organization's value
// for this key" — and not the whole configuration surface, so the domain cannot
// change a setting by accident and a second implementation stays trivial.
type OrgDefaults interface {
	EffectiveString(ctx context.Context, orgID uuid.UUID, key string) string
}

// SetOrgDefaults attaches the organization configuration. It is a setter rather
// than a constructor parameter because the configuration service is built after
// the domain it configures, and because every existing caller — including the test
// harness — must keep working with the model defaults when it is not set.
func (s *Service) SetOrgDefaults(d OrgDefaults) { s.defaults = d }

// Setting keys the domain reads. They are spelled here rather than imported from
// the configuration package so the domain holds no dependency on it; the two
// spellings are pinned together by a test.
const (
	SettingDefaultCurrency     = "crm.default_currency"
	SettingDefaultTaskPriority = "tasks.default_priority"
)

// resolveCurrency returns the currency to use for a deal: the caller's value when
// it named one, otherwise the organization's default, otherwise the model default.
func (s *Service) resolveCurrency(ctx context.Context, orgID uuid.UUID, given string) string {
	if strings.TrimSpace(given) != "" {
		return given
	}
	if s.defaults != nil {
		if v := s.defaults.EffectiveString(ctx, orgID, SettingDefaultCurrency); v != "" {
			return v
		}
	}
	return "USD"
}

// resolvePriority returns the priority to use for a task.
func (s *Service) resolvePriority(ctx context.Context, orgID uuid.UUID, given string) string {
	if strings.TrimSpace(given) != "" {
		return given
	}
	if s.defaults != nil {
		if v := s.defaults.EffectiveString(ctx, orgID, SettingDefaultTaskPriority); v != "" {
			return v
		}
	}
	return "normal"
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
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR COALESCE(email::text,'') ILIKE $%d)", len(args), len(args)))
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
	if frag, withArgs := objectOwnerPredicateFor(p, "account", args); frag != "" {
		where = append(where, frag)
		args = withArgs
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

// GetAccount loads one account in the caller's object scope: it returns only an
// account the caller could also see in a list.
func (s *Service) GetAccount(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Account, error) {
	where := "id = $1 AND organization_id = $2"
	args := []any{id, p.OrganizationID}
	if frag, withArgs := objectOwnerPredicateFor(p, "account", args); frag != "" {
		where += " AND " + frag
		args = withArgs
	}
	a, err := scanAccount(s.db.Q(ctx).QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE `+where, args...))
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
	// OpenTasksOnly and due bounds are used by callers which need an actionable
	// task list. DueBefore is exclusive, allowing adjacent day windows.
	OpenTasksOnly bool
	TaskDueFrom   *time.Time
	TaskDueBefore *time.Time
	PipelineID    *uuid.UUID
	From          *time.Time
	To            *time.Time
	// ExpectedCloseBefore selects deals whose expected close date is before an
	// instant. It is what a "deal is overdue" query needs, and it is expressed as a
	// bound rather than a boolean so the caller chooses the cutoff.
	ExpectedCloseBefore *time.Time
	// UpdatedBefore selects deals that have not changed since an instant.
	UpdatedBefore *time.Time
	// NoActivitySince selects deals with no recorded activity at or after an
	// instant. It is a separate field from UpdatedBefore because a deal can be edited
	// (a field corrected) without anyone having interacted with the customer, and the
	// "nothing is happening" question is about the latter.
	NoActivitySince *time.Time
	Extra           map[string]string
}

// ErrNotFound is returned when a record does not exist in the caller's scope.
var ErrNotFound = errors.New("not found")

// objectRule describes how one entity type is reached at the object level.
//
// reachPerm is the permission that lets a caller reach every record of the type:
// the archive permission for the shared CRM records, the assign permission for
// the personal work items. ownerColumn is the column holding the owning user.
// teamlessReadsAll preserves the behaviour each entity's list query already had:
// for shared CRM records a caller without a team is treated as an
// organization-wide reader, while for personal work items a caller without a
// team sees only their own.
type objectRule struct {
	reachPerm        rbac.Permission
	ownerColumn      string
	teamlessReadsAll bool
}

// objectRules is the single source of truth for the object-level authorization
// rule. Every read and write path for an entity — list, single-record read,
// search, export, the nested feeds that hang off a parent record, and the
// mutations — resolves its rule here, so the paths cannot drift apart again.
//
// A path that cannot name its entity type in this table has no object-level rule
// and must be handled explicitly, not left to a default.
var objectRules = map[string]objectRule{
	"account": {rbac.PermAccountsArchive, "owner_user_id", true},
	"contact": {rbac.PermContactsArchive, "owner_user_id", true},
	"lead":    {rbac.PermLeadsArchive, "owner_user_id", true},
	"deal":    {rbac.PermDealsArchive, "owner_user_id", true},
	"task":    {rbac.PermTasksAssign, "assignee_user_id", false},
	"ticket":  {rbac.PermTicketsAssign, "assignee_user_id", false},
}

// objectOwnerPredicateFor is the object-level authorization rule for an entity
// type, resolved through objectRules.
//
// A caller who cannot reach every record of the type only sees the records they
// own or that belong to one of their teams. The rule is applied identically by
// list queries, single-record reads, search, export and mutations, so a record a
// caller cannot see in a list cannot be fetched, exported or written through a
// guessed id either. It returns the SQL fragment and the arguments it binds, or
// an empty fragment when the caller may reach the whole organization.
//
// An unknown entity type returns an empty fragment: callers validate the entity
// type before they reach here, and this function must not invent a rule for a
// type it does not know.
func objectOwnerPredicateFor(p *rbac.Principal, entityType string, args []any) (string, []any) {
	ownerColumn, applies := objectScopeParts(p, entityType)
	if !applies {
		return "", args
	}
	args = append(args, p.UserID, p.TeamIDs)
	return fmt.Sprintf("(%s = $%d OR team_id = ANY($%d))", ownerColumn, len(args)-1, len(args)), args
}

// objectScopeParts reports whether the object-level rule applies to this caller
// for this entity type, and if so which column holds the owner. It is the one
// place the rule is decided; objectOwnerPredicateFor and ObjectScopeFilter both
// build their SQL from it.
func objectScopeParts(p *rbac.Principal, entityType string) (ownerColumn string, applies bool) {
	rule, ok := objectRules[entityType]
	if !ok {
		return "", false
	}
	if p.Has(rule.reachPerm) || (rule.teamlessReadsAll && len(p.TeamIDs) == 0) {
		return "", false
	}
	return rule.ownerColumn, true
}

// ObjectScopeFilter returns the object-level SQL filter for an entity type with
// placeholders numbered from baseArg+1, together with the arguments it binds. It
// is the same rule objectOwnerPredicateFor applies, exposed for callers outside
// this package — the export writer — that assemble their own query. baseArg must
// be the number of parameters the caller has already bound. An empty filter means
// the caller may reach every record of the type, and no arguments are returned.
//
// The activity feed is a nested view of several parent types, so it delegates to
// activityScopeFilter rather than a single owner column.
func ObjectScopeFilter(p *rbac.Principal, entityType string, baseArg int) (string, []any) {
	if entityType == "activity" {
		return activityScopeFilter(p, baseArg)
	}
	ownerColumn, applies := objectScopeParts(p, entityType)
	if !applies {
		return "", nil
	}
	return fmt.Sprintf("(%s = $%d OR team_id = ANY($%d))", ownerColumn, baseArg+1, baseArg+2),
		[]any{p.UserID, p.TeamIDs}
}

// activityParentTypes maps the parent columns an activity can hang from to the
// entity type whose object rule governs whether the activity may be seen.
var activityParentTypes = []struct {
	column     string
	entityType string
	table      string
}{
	{"account_id", "account", "accounts"},
	{"contact_id", "contact", "contacts"},
	{"deal_id", "deal", "deals"},
	{"ticket_id", "ticket", "tickets"},
}

// activityScopeFilter restricts the global activity feed to activities the caller
// can reach through at least one of their parent records, using the same
// per-entity object rules as everything else. Placeholders are numbered from
// baseArg+1.
//
// When the caller is unrestricted on every parent type, it returns an empty
// filter and no arguments, so a caller with full reach still sees every activity,
// including the ones with no parent at all. A caller who is restricted on at
// least one parent type sees only activities linked to a parent they can reach,
// which is what stops the global feed from disclosing other users' history.
func activityScopeFilter(p *rbac.Principal, baseArg int) (string, []any) {
	var clauses []string
	var args []any
	n := baseArg
	restricted := false
	for _, pt := range activityParentTypes {
		ownerColumn, applies := objectScopeParts(p, pt.entityType)
		if !applies {
			clauses = append(clauses, pt.column+" IS NOT NULL")
			continue
		}
		restricted = true
		n++
		userArg := n
		n++
		teamArg := n
		args = append(args, p.UserID, p.TeamIDs)
		clauses = append(clauses, fmt.Sprintf(
			"%s IS NOT NULL AND EXISTS (SELECT 1 FROM %s x WHERE x.id = activities.%s AND (x.%s = $%d OR x.team_id = ANY($%d)))",
			pt.column, pt.table, pt.column, ownerColumn, userArg, teamArg))
	}
	if !restricted {
		return "", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

// ensureReadableObject returns ErrNotFound unless the caller can read the named
// record. A nested resource — comments, activities, tags — calls it before it
// touches data that hangs off a parent record: the authorization decision belongs
// to the parent, and a parent the caller cannot read must not be addressable by
// id. It reuses the single-record reads, so a nested path can never be looser
// than the record it hangs from.
func (s *Service) ensureReadableObject(ctx context.Context, p *rbac.Principal, entityType string, id uuid.UUID) error {
	switch entityType {
	case "account":
		_, err := s.GetAccount(ctx, p, id)
		return err
	case "contact":
		_, err := s.GetContact(ctx, p, id)
		return err
	case "lead":
		_, err := s.GetLead(ctx, p, id)
		return err
	case "deal":
		_, err := s.GetDeal(ctx, p, id)
		return err
	case "task":
		_, err := s.GetTask(ctx, p, id)
		return err
	case "ticket":
		_, err := s.GetTicket(ctx, p, id)
		return err
	default:
		return httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
}

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
