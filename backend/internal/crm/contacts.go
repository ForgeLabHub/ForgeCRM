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
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Contact is a person associated with an account.
type Contact struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	AccountID      *uuid.UUID `json:"account_id,omitempty"`
	FirstName      *string    `json:"first_name,omitempty"`
	LastName       *string    `json:"last_name,omitempty"`
	Email          *string    `json:"email,omitempty"`
	Phone          *string    `json:"phone,omitempty"`
	Mobile         *string    `json:"mobile,omitempty"`
	JobTitle       *string    `json:"job_title,omitempty"`
	Department     *string    `json:"department,omitempty"`
	IsPrimary      bool       `json:"is_primary"`
	OwnerUserID    *uuid.UUID `json:"owner_user_id,omitempty"`
	TeamID         *uuid.UUID `json:"team_id,omitempty"`
	ExternalSource *string    `json:"external_source,omitempty"`
	ExternalID     *string    `json:"external_id,omitempty"`
	CustomFields   JSONMap    `json:"custom_fields"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
}

// ContactInput is the writable projection of a contact.
type ContactInput struct {
	AccountID      *uuid.UUID `json:"account_id"`
	FirstName      *string    `json:"first_name"`
	LastName       *string    `json:"last_name"`
	Email          *string    `json:"email"`
	Phone          *string    `json:"phone"`
	Mobile         *string    `json:"mobile"`
	JobTitle       *string    `json:"job_title"`
	Department     *string    `json:"department"`
	IsPrimary      bool       `json:"is_primary"`
	OwnerUserID    *uuid.UUID `json:"owner_user_id"`
	TeamID         *uuid.UUID `json:"team_id"`
	ExternalSource *string    `json:"external_source"`
	ExternalID     *string    `json:"external_id"`
	CustomFields   JSONMap    `json:"custom_fields"`
}

// Validate checks the input against the domain rules.
func (in *ContactInput) Validate() error {
	details := map[string]string{}
	hasName := (in.FirstName != nil && strings.TrimSpace(*in.FirstName) != "") ||
		(in.LastName != nil && strings.TrimSpace(*in.LastName) != "")
	if !hasName && (in.Email == nil || *in.Email == "") {
		details["name"] = "at least a name or an email is required"
	}
	if in.Email != nil && *in.Email != "" && !looksLikeEmail(*in.Email) {
		details["email"] = "email is not valid"
	}
	if (in.ExternalSource == nil) != (in.ExternalID == nil) {
		details["external_id"] = "external_source and external_id must be provided together"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

const contactColumns = `
	id, organization_id, account_id, first_name, last_name, email, phone, mobile,
	job_title, department, is_primary, owner_user_id, team_id, external_source,
	external_id, custom_fields, created_at, updated_at, archived_at`

func scanContact(row rowScanner) (*Contact, error) {
	var c Contact
	err := row.Scan(&c.ID, &c.OrganizationID, &c.AccountID, &c.FirstName, &c.LastName, &c.Email,
		&c.Phone, &c.Mobile, &c.JobTitle, &c.Department, &c.IsPrimary, &c.OwnerUserID,
		&c.TeamID, &c.ExternalSource, &c.ExternalID, &c.CustomFields, &c.CreatedAt,
		&c.UpdatedAt, &c.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListContacts returns a page of contacts.
func (s *Service) ListContacts(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Contact, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if !q.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf(
			"(COALESCE(first_name,'') ILIKE $%d OR COALESCE(last_name,'') ILIKE $%d OR COALESCE(email,'') ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	if q.AccountID != nil {
		args = append(args, *q.AccountID)
		where = append(where, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if q.OwnerID != nil {
		args = append(args, *q.OwnerID)
		where = append(where, fmt.Sprintf("owner_user_id = $%d", len(args)))
	}
	if q.DealID != nil {
		// Contacts reachable through a deal's account, plus the deal's primary contact.
		args = append(args, *q.DealID)
		where = append(where, fmt.Sprintf(
			"(account_id = (SELECT account_id FROM deals WHERE id = $%d AND organization_id = $1)"+
				" OR id = (SELECT primary_contact_id FROM deals WHERE id = $%d AND organization_id = $1))",
			len(args), len(args)))
	}
	if !p.Has(rbac.PermContactsArchive) && len(p.TeamIDs) > 0 {
		args = append(args, p.UserID, p.TeamIDs)
		where = append(where, fmt.Sprintf("(owner_user_id = $%d OR team_id = ANY($%d))", len(args)-1, len(args)))
	}

	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM contacts WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count contacts: %w", err)
	}
	order := q.Sort.OrderClause("created_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	rows, err := s.db.Q(ctx).Query(ctx,
		"SELECT "+contactColumns+" FROM contacts WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// GetContact loads one contact in scope.
func (s *Service) GetContact(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Contact, error) {
	const q = `SELECT ` + contactColumns + ` FROM contacts WHERE id = $1 AND organization_id = $2`
	c, err := scanContact(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load contact: %w", err)
	}
	return c, nil
}

// CreateContact inserts a contact.
func (s *Service) CreateContact(ctx context.Context, p *rbac.Principal, in ContactInput) (*Contact, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Contact
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.assertAccountInOrg(ctx, p.OrganizationID, in.AccountID); err != nil {
			return err
		}
		const q = `
			INSERT INTO contacts (organization_id, account_id, first_name, last_name, email,
				phone, mobile, job_title, department, is_primary, owner_user_id, team_id,
				external_source, external_id, custom_fields, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			RETURNING ` + contactColumns
		c, err := scanContact(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.AccountID,
			in.FirstName, in.LastName, in.Email, in.Phone, in.Mobile, in.JobTitle, in.Department,
			in.IsPrimary, in.OwnerUserID, in.TeamID, in.ExternalSource, in.ExternalID,
			orEmptyJSON(in.CustomFields), p.UserID))
		if err != nil {
			return wrapConstraint(err, "contact")
		}
		created = c
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventContactCreated, map[string]any{
			"contact_id": c.ID, "account_id": c.AccountID,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "contacts.create", EntityType: "contact", EntityID: c.ID.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateContact applies a partial update.
func (s *Service) UpdateContact(ctx context.Context, p *rbac.Principal, id uuid.UUID, in ContactInput) (*Contact, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var updated *Contact
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.assertAccountInOrg(ctx, p.OrganizationID, in.AccountID); err != nil {
			return err
		}
		const q = `
			UPDATE contacts SET account_id=$3, first_name=$4, last_name=$5, email=$6, phone=$7,
				mobile=$8, job_title=$9, department=$10, is_primary=$11, owner_user_id=$12,
				team_id=$13, external_source=$14, external_id=$15, custom_fields=$16
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + contactColumns
		c, err := scanContact(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.AccountID,
			in.FirstName, in.LastName, in.Email, in.Phone, in.Mobile, in.JobTitle, in.Department,
			in.IsPrimary, in.OwnerUserID, in.TeamID, in.ExternalSource, in.ExternalID,
			orEmptyJSON(in.CustomFields)))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return wrapConstraint(err, "contact")
		}
		updated = c
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventContactUpdated, map[string]any{
			"contact_id": c.ID,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "contacts.update", EntityType: "contact", EntityID: id.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ArchiveContact archives a contact.
func (s *Service) ArchiveContact(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `UPDATE contacts SET archived_at = now() WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("archive contact: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventContactArchived, map[string]any{"contact_id": id}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "contacts.archive", EntityType: "contact", EntityID: id.String(),
		})
	})
}

// assertAccountInOrg rejects a reference to an account from another organization.
// This closes the "guess an ID" path: a foreign key alone would accept the row,
// because the FK checks existence, not tenancy.
func (s *Service) assertAccountInOrg(ctx context.Context, orgID uuid.UUID, accountID *uuid.UUID) error {
	if accountID == nil {
		return nil
	}
	const q = `SELECT 1 FROM accounts WHERE id = $1 AND organization_id = $2`
	var one int
	err := s.db.Q(ctx).QueryRow(ctx, q, *accountID, orgID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NewError(422, httpx.CodeValidation, "account_id does not belong to this organization")
	}
	if err != nil {
		return fmt.Errorf("check account scope: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Leads
// ---------------------------------------------------------------------------

// Lead is an unqualified prospect.
type Lead struct {
	ID                 uuid.UUID  `json:"id"`
	OrganizationID     uuid.UUID  `json:"organization_id"`
	Title              string     `json:"title"`
	FirstName          *string    `json:"first_name,omitempty"`
	LastName           *string    `json:"last_name,omitempty"`
	Email              *string    `json:"email,omitempty"`
	Phone              *string    `json:"phone,omitempty"`
	Company            *string    `json:"company,omitempty"`
	Source             *string    `json:"source,omitempty"`
	Status             string     `json:"status"`
	Score              *int       `json:"score,omitempty"`
	Notes              *string    `json:"notes,omitempty"`
	OwnerUserID        *uuid.UUID `json:"owner_user_id,omitempty"`
	TeamID             *uuid.UUID `json:"team_id,omitempty"`
	ConvertedAccountID *uuid.UUID `json:"converted_account_id,omitempty"`
	ConvertedContactID *uuid.UUID `json:"converted_contact_id,omitempty"`
	ConvertedAt        *time.Time `json:"converted_at,omitempty"`
	ExternalSource     *string    `json:"external_source,omitempty"`
	ExternalID         *string    `json:"external_id,omitempty"`
	CustomFields       JSONMap    `json:"custom_fields"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
}

// LeadInput is the writable projection of a lead.
type LeadInput struct {
	Title          string     `json:"title"`
	FirstName      *string    `json:"first_name"`
	LastName       *string    `json:"last_name"`
	Email          *string    `json:"email"`
	Phone          *string    `json:"phone"`
	Company        *string    `json:"company"`
	Source         *string    `json:"source"`
	Status         string     `json:"status"`
	Score          *int       `json:"score"`
	Notes          *string    `json:"notes"`
	OwnerUserID    *uuid.UUID `json:"owner_user_id"`
	TeamID         *uuid.UUID `json:"team_id"`
	ExternalSource *string    `json:"external_source"`
	ExternalID     *string    `json:"external_id"`
	CustomFields   JSONMap    `json:"custom_fields"`
}

// Valid lead statuses.
var leadStatuses = map[string]bool{
	"new": true, "contacted": true, "qualified": true, "unqualified": true, "converted": true,
}

// Validate checks the input.
func (in *LeadInput) Validate() error {
	details := map[string]string{}
	if in.Status == "" {
		in.Status = "new"
	}
	if !leadStatuses[in.Status] {
		details["status"] = "must be one of new, contacted, qualified, unqualified, converted"
	}
	if in.Email != nil && *in.Email != "" && !looksLikeEmail(*in.Email) {
		details["email"] = "email is not valid"
	}
	if in.Score != nil && (*in.Score < 0 || *in.Score > 100) {
		details["score"] = "must be between 0 and 100"
	}
	if (in.ExternalSource == nil) != (in.ExternalID == nil) {
		details["external_id"] = "external_source and external_id must be provided together"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

const leadColumns = `
	id, organization_id, title, first_name, last_name, email, phone, company, source,
	status, score, notes, owner_user_id, team_id, converted_account_id, converted_contact_id,
	converted_at, external_source, external_id, custom_fields, created_at, updated_at, archived_at`

func scanLead(row rowScanner) (*Lead, error) {
	var l Lead
	err := row.Scan(&l.ID, &l.OrganizationID, &l.Title, &l.FirstName, &l.LastName, &l.Email,
		&l.Phone, &l.Company, &l.Source, &l.Status, &l.Score, &l.Notes, &l.OwnerUserID,
		&l.TeamID, &l.ConvertedAccountID, &l.ConvertedContactID, &l.ConvertedAt,
		&l.ExternalSource, &l.ExternalID, &l.CustomFields, &l.CreatedAt, &l.UpdatedAt, &l.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ListLeads returns a page of leads.
func (s *Service) ListLeads(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Lead, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if !q.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf(
			"(title ILIKE $%d OR COALESCE(company,'') ILIKE $%d OR COALESCE(email,'') ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	if q.OwnerID != nil {
		args = append(args, *q.OwnerID)
		where = append(where, fmt.Sprintf("owner_user_id = $%d", len(args)))
	}
	if !p.Has(rbac.PermLeadsArchive) && len(p.TeamIDs) > 0 {
		args = append(args, p.UserID, p.TeamIDs)
		where = append(where, fmt.Sprintf("(owner_user_id = $%d OR team_id = ANY($%d))", len(args)-1, len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM leads WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count leads: %w", err)
	}
	order := q.Sort.OrderClause("created_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	rows, err := s.db.Q(ctx).Query(ctx,
		"SELECT "+leadColumns+" FROM leads WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list leads: %w", err)
	}
	defer rows.Close()
	var out []Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *l)
	}
	return out, total, rows.Err()
}

// GetLead loads one lead in scope.
func (s *Service) GetLead(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Lead, error) {
	const q = `SELECT ` + leadColumns + ` FROM leads WHERE id = $1 AND organization_id = $2`
	l, err := scanLead(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load lead: %w", err)
	}
	return l, nil
}

// CreateLead inserts a lead.
func (s *Service) CreateLead(ctx context.Context, p *rbac.Principal, in LeadInput) (*Lead, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Lead
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO leads (organization_id, title, first_name, last_name, email, phone,
				company, source, status, score, notes, owner_user_id, team_id,
				external_source, external_id, custom_fields, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			RETURNING ` + leadColumns
		l, err := scanLead(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.Title, in.FirstName,
			in.LastName, in.Email, in.Phone, in.Company, in.Source, in.Status, in.Score, in.Notes,
			in.OwnerUserID, in.TeamID, in.ExternalSource, in.ExternalID,
			orEmptyJSON(in.CustomFields), p.UserID))
		if err != nil {
			return wrapConstraint(err, "lead")
		}
		created = l
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventLeadCreated, map[string]any{"lead_id": l.ID}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "leads.create", EntityType: "lead", EntityID: l.ID.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateLead applies a partial update.
func (s *Service) UpdateLead(ctx context.Context, p *rbac.Principal, id uuid.UUID, in LeadInput) (*Lead, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var updated *Lead
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE leads SET title=$3, first_name=$4, last_name=$5, email=$6, phone=$7,
				company=$8, source=$9, status=$10, score=$11, notes=$12, owner_user_id=$13,
				team_id=$14, external_source=$15, external_id=$16, custom_fields=$17
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + leadColumns
		l, err := scanLead(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.Title,
			in.FirstName, in.LastName, in.Email, in.Phone, in.Company, in.Source, in.Status,
			in.Score, in.Notes, in.OwnerUserID, in.TeamID, in.ExternalSource, in.ExternalID,
			orEmptyJSON(in.CustomFields)))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return wrapConstraint(err, "lead")
		}
		updated = l
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventLeadUpdated, map[string]any{"lead_id": l.ID}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "leads.update", EntityType: "lead", EntityID: id.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ConvertLead creates an account and a contact from a qualified lead. The whole
// operation is one transaction: a partially converted lead would be worse than a
// failed conversion.
func (s *Service) ConvertLead(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Lead, *Account, *Contact, error) {
	var (
		lead    *Lead
		account *Account
		contact *Contact
	)
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		existing, err := s.GetLead(ctx, p, id)
		if err != nil {
			return err
		}
		if existing.Status == "converted" {
			return httpx.NewError(409, httpx.CodeConflict, "lead has already been converted")
		}
		lead = existing

		accountName := "Unnamed company"
		if existing.Company != nil && strings.TrimSpace(*existing.Company) != "" {
			accountName = *existing.Company
		} else if existing.LastName != nil && *existing.LastName != "" {
			accountName = *existing.LastName
		}
		const insAccount = `
			INSERT INTO accounts (organization_id, name, email, phone, owner_user_id, team_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			RETURNING ` + accountColumns
		account, err = scanAccount(s.db.Q(ctx).QueryRow(ctx, insAccount, p.OrganizationID,
			accountName, existing.Email, existing.Phone, existing.OwnerUserID, existing.TeamID, p.UserID))
		if err != nil {
			return wrapConstraint(err, "account")
		}

		const insContact = `
			INSERT INTO contacts (organization_id, account_id, first_name, last_name, email, phone,
				is_primary, owner_user_id, team_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,true,$7,$8,$9)
			RETURNING ` + contactColumns
		contact, err = scanContact(s.db.Q(ctx).QueryRow(ctx, insContact, p.OrganizationID,
			account.ID, existing.FirstName, existing.LastName, existing.Email, existing.Phone,
			existing.OwnerUserID, existing.TeamID, p.UserID))
		if err != nil {
			return wrapConstraint(err, "contact")
		}

		const upd = `
			UPDATE leads SET status='converted', converted_account_id=$3,
				converted_contact_id=$4, converted_at=now()
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + leadColumns
		converted, err := scanLead(s.db.Q(ctx).QueryRow(ctx, upd, id, p.OrganizationID, account.ID, contact.ID))
		if err != nil {
			return fmt.Errorf("mark lead converted: %w", err)
		}
		lead = converted

		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventLeadConverted, map[string]any{
			"lead_id": lead.ID, "account_id": account.ID, "contact_id": contact.ID,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "leads.convert", EntityType: "lead", EntityID: id.String(),
			After: map[string]any{"account_id": account.ID, "contact_id": contact.ID},
		})
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return lead, account, contact, nil
}
