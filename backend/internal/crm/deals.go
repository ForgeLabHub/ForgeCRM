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

// Pipeline is a sales process.
type Pipeline struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	Name           string          `json:"name"`
	IsDefault      bool            `json:"is_default"`
	Currency       string          `json:"currency"`
	Stages         []PipelineStage `json:"stages,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	ArchivedAt     *time.Time      `json:"archived_at,omitempty"`
}

// PipelineStage is one step of a pipeline.
type PipelineStage struct {
	ID          uuid.UUID `json:"id"`
	PipelineID  uuid.UUID `json:"pipeline_id"`
	Name        string    `json:"name"`
	Position    int       `json:"position"`
	Probability int       `json:"probability"`
	IsWon       bool      `json:"is_won"`
	IsLost      bool      `json:"is_lost"`
}

// ListPipelines returns pipelines with their stages.
func (s *Service) ListPipelines(ctx context.Context, p *rbac.Principal) ([]Pipeline, error) {
	const q = `
		SELECT id, organization_id, name, is_default, currency, created_at, archived_at
		FROM pipelines
		WHERE organization_id = $1 AND archived_at IS NULL
		ORDER BY is_default DESC, name`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer rows.Close()

	var out []Pipeline
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var pl Pipeline
		if err := rows.Scan(&pl.ID, &pl.OrganizationID, &pl.Name, &pl.IsDefault, &pl.Currency,
			&pl.CreatedAt, &pl.ArchivedAt); err != nil {
			return nil, err
		}
		out = append(out, pl)
		index[pl.ID] = len(out) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const stageQ = `
		SELECT id, pipeline_id, name, position, probability, is_won, is_lost
		FROM pipeline_stages WHERE organization_id = $1 ORDER BY pipeline_id, position`
	stageRows, err := s.db.Q(ctx).Query(ctx, stageQ, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list stages: %w", err)
	}
	defer stageRows.Close()
	for stageRows.Next() {
		var st PipelineStage
		if err := stageRows.Scan(&st.ID, &st.PipelineID, &st.Name, &st.Position, &st.Probability,
			&st.IsWon, &st.IsLost); err != nil {
			return nil, err
		}
		if i, ok := index[st.PipelineID]; ok {
			out[i].Stages = append(out[i].Stages, st)
		}
	}
	return out, stageRows.Err()
}

// CreatePipeline inserts a pipeline and its stages.
func (s *Service) CreatePipeline(ctx context.Context, p *rbac.Principal, name, currency string, stages []PipelineStage) (*Pipeline, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, httpx.ValidationError(map[string]string{"name": "name is required"})
	}
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return nil, httpx.ValidationError(map[string]string{"currency": "must be a 3-letter code"})
	}
	var created *Pipeline
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO pipelines (organization_id, name, currency)
			VALUES ($1,$2,$3)
			RETURNING id, organization_id, name, is_default, currency, created_at, archived_at`
		var pl Pipeline
		if err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, name, currency).Scan(
			&pl.ID, &pl.OrganizationID, &pl.Name, &pl.IsDefault, &pl.Currency, &pl.CreatedAt, &pl.ArchivedAt); err != nil {
			return wrapConstraint(err, "pipeline")
		}
		for i := range stages {
			st := &stages[i]
			st.Name = strings.TrimSpace(st.Name)
			if st.Name == "" {
				return httpx.ValidationError(map[string]string{"stages": "stage names must not be empty"})
			}
			if st.Probability < 0 || st.Probability > 100 {
				return httpx.ValidationError(map[string]string{"stages": "probability must be between 0 and 100"})
			}
			if st.IsWon && st.IsLost {
				return httpx.ValidationError(map[string]string{"stages": "a stage cannot be both won and lost"})
			}
			const insStage = `
				INSERT INTO pipeline_stages (pipeline_id, organization_id, name, position, probability, is_won, is_lost)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
				RETURNING id`
			if err := s.db.Q(ctx).QueryRow(ctx, insStage, pl.ID, p.OrganizationID, st.Name,
				i, st.Probability, st.IsWon, st.IsLost).Scan(&st.ID); err != nil {
				return wrapConstraint(err, "pipeline stage")
			}
			st.PipelineID = pl.ID
			pl.Stages = append(pl.Stages, *st)
		}
		created = &pl
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "pipelines.create", EntityType: "pipeline", EntityID: pl.ID.String(),
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ---------------------------------------------------------------------------
// Deals
// ---------------------------------------------------------------------------

// Deal is a sales opportunity.
type Deal struct {
	ID                 uuid.UUID  `json:"id"`
	OrganizationID     uuid.UUID  `json:"organization_id"`
	PipelineID         uuid.UUID  `json:"pipeline_id"`
	StageID            uuid.UUID  `json:"stage_id"`
	AccountID          *uuid.UUID `json:"account_id,omitempty"`
	PrimaryContactID   *uuid.UUID `json:"primary_contact_id,omitempty"`
	Title              string     `json:"title"`
	Description        *string    `json:"description,omitempty"`
	Amount             float64    `json:"amount"`
	Currency           string     `json:"currency"`
	DiscountPercent    float64    `json:"discount_percent"`
	DiscountApprovedBy *uuid.UUID `json:"discount_approved_by,omitempty"`
	DiscountApprovedAt *time.Time `json:"discount_approved_at,omitempty"`
	Status             string     `json:"status"`
	ExpectedCloseDate  *string    `json:"expected_close_date,omitempty"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
	LostReason         *string    `json:"lost_reason,omitempty"`
	OwnerUserID        *uuid.UUID `json:"owner_user_id,omitempty"`
	TeamID             *uuid.UUID `json:"team_id,omitempty"`
	ExternalSource     *string    `json:"external_source,omitempty"`
	ExternalID         *string    `json:"external_id,omitempty"`
	CustomFields       JSONMap    `json:"custom_fields"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
}

// DealInput is the writable projection of a deal.
type DealInput struct {
	PipelineID        uuid.UUID  `json:"pipeline_id"`
	StageID           uuid.UUID  `json:"stage_id"`
	AccountID         *uuid.UUID `json:"account_id"`
	PrimaryContactID  *uuid.UUID `json:"primary_contact_id"`
	Title             string     `json:"title"`
	Description       *string    `json:"description"`
	Amount            float64    `json:"amount"`
	Currency          string     `json:"currency"`
	DiscountPercent   float64    `json:"discount_percent"`
	ExpectedCloseDate *string    `json:"expected_close_date"`
	OwnerUserID       *uuid.UUID `json:"owner_user_id"`
	TeamID            *uuid.UUID `json:"team_id"`
	ExternalSource    *string    `json:"external_source"`
	ExternalID        *string    `json:"external_id"`
	CustomFields      JSONMap    `json:"custom_fields"`
}

// Validate checks the input.
func (in *DealInput) Validate() error {
	details := map[string]string{}
	if strings.TrimSpace(in.Title) == "" {
		details["title"] = "title is required"
	}
	if in.PipelineID == uuid.Nil {
		details["pipeline_id"] = "pipeline_id is required"
	}
	if in.StageID == uuid.Nil {
		details["stage_id"] = "stage_id is required"
	}
	if in.Amount < 0 {
		details["amount"] = "must not be negative"
	}
	if in.DiscountPercent < 0 || in.DiscountPercent > 100 {
		details["discount_percent"] = "must be between 0 and 100"
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	if len(in.Currency) != 3 {
		details["currency"] = "must be a 3-letter code"
	}
	if in.ExpectedCloseDate != nil && *in.ExpectedCloseDate != "" {
		if _, err := time.Parse("2006-01-02", *in.ExpectedCloseDate); err != nil {
			details["expected_close_date"] = "must be an ISO date (YYYY-MM-DD)"
		}
	}
	if (in.ExternalSource == nil) != (in.ExternalID == nil) {
		details["external_id"] = "external_source and external_id must be provided together"
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

// dealColumns is the column list every deal read shares. `expected_close_date` is
// cast to text because it is a PostgreSQL `date` column: the driver delivers a date
// in binary format, which will not scan into the *string the DTO and the JSON
// contract use, so a deal that has a close date would fail to load at all. The cast
// yields the ISO date the API already promises.
const dealColumns = `
	id, organization_id, pipeline_id, stage_id, account_id, primary_contact_id, title,
	description, amount, currency, discount_percent, discount_approved_by, discount_approved_at,
	status, expected_close_date::text, closed_at, lost_reason, owner_user_id, team_id,
	external_source, external_id, custom_fields, created_at, updated_at, archived_at`

func scanDeal(row rowScanner) (*Deal, error) {
	var d Deal
	err := row.Scan(&d.ID, &d.OrganizationID, &d.PipelineID, &d.StageID, &d.AccountID,
		&d.PrimaryContactID, &d.Title, &d.Description, &d.Amount, &d.Currency,
		&d.DiscountPercent, &d.DiscountApprovedBy, &d.DiscountApprovedAt, &d.Status,
		&d.ExpectedCloseDate, &d.ClosedAt, &d.LostReason, &d.OwnerUserID, &d.TeamID,
		&d.ExternalSource, &d.ExternalID, &d.CustomFields, &d.CreatedAt, &d.UpdatedAt, &d.ArchivedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListDeals returns a page of deals.
func (s *Service) ListDeals(ctx context.Context, p *rbac.Principal, q ListQuery) ([]Deal, int64, error) {
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if !q.IncludeArchived {
		where = append(where, "archived_at IS NULL")
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if q.PipelineID != nil {
		args = append(args, *q.PipelineID)
		where = append(where, fmt.Sprintf("pipeline_id = $%d", len(args)))
	}
	if q.AccountID != nil {
		args = append(args, *q.AccountID)
		where = append(where, fmt.Sprintf("account_id = $%d", len(args)))
	}
	if q.OwnerID != nil {
		args = append(args, *q.OwnerID)
		where = append(where, fmt.Sprintf("owner_user_id = $%d", len(args)))
	}
	if q.ContactID != nil {
		args = append(args, *q.ContactID)
		where = append(where, fmt.Sprintf("primary_contact_id = $%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where = append(where, fmt.Sprintf("title ILIKE $%d", len(args)))
	}
	// The date filters a scheduled CRM workflow needs. Each is a bound rather than a
	// boolean so the caller owns the cutoff, and each is applied to the table's own
	// columns so the existing organization and ownership filters still decide which
	// rows are visible at all.
	if q.ExpectedCloseBefore != nil {
		args = append(args, *q.ExpectedCloseBefore)
		where = append(where, fmt.Sprintf("expected_close_date < $%d::date", len(args)))
	}
	if q.UpdatedBefore != nil {
		args = append(args, *q.UpdatedBefore)
		where = append(where, fmt.Sprintf("updated_at < $%d", len(args)))
	}
	if q.NoActivitySince != nil {
		args = append(args, *q.NoActivitySince)
		where = append(where, fmt.Sprintf(
			`NOT EXISTS (SELECT 1 FROM activities a WHERE a.deal_id = deals.id AND a.occurred_at >= $%d)`,
			len(args)))
	}
	if !p.Has(rbac.PermDealsArchive) && len(p.TeamIDs) > 0 {
		args = append(args, p.UserID, p.TeamIDs)
		where = append(where, fmt.Sprintf("(owner_user_id = $%d OR team_id = ANY($%d))", len(args)-1, len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM deals WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deals: %w", err)
	}
	order := q.Sort.OrderClause("created_at DESC")
	args = append(args, q.Pagination.Limit, q.Pagination.Offset)
	rows, err := s.db.Q(ctx).Query(ctx,
		"SELECT "+dealColumns+" FROM deals WHERE "+whereSQL+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list deals: %w", err)
	}
	defer rows.Close()
	var out []Deal
	for rows.Next() {
		d, err := scanDeal(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *d)
	}
	return out, total, rows.Err()
}

// GetDeal loads one deal in scope.
func (s *Service) GetDeal(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Deal, error) {
	const q = `SELECT ` + dealColumns + ` FROM deals WHERE id = $1 AND organization_id = $2`
	d, err := scanDeal(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load deal: %w", err)
	}
	return d, nil
}

// CreateDeal inserts a deal and records the initial stage in the history.
func (s *Service) CreateDeal(ctx context.Context, p *rbac.Principal, in DealInput) (*Deal, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var created *Deal
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.assertStageInOrg(ctx, p.OrganizationID, in.PipelineID, in.StageID); err != nil {
			return err
		}
		if err := s.assertAccountInOrg(ctx, p.OrganizationID, in.AccountID); err != nil {
			return err
		}
		const q = `
			INSERT INTO deals (organization_id, pipeline_id, stage_id, account_id, primary_contact_id,
				title, description, amount, currency, discount_percent, expected_close_date,
				owner_user_id, team_id, external_source, external_id, custom_fields, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			RETURNING ` + dealColumns
		d, err := scanDeal(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, in.PipelineID, in.StageID,
			in.AccountID, in.PrimaryContactID, in.Title, in.Description, in.Amount, in.Currency,
			in.DiscountPercent, in.ExpectedCloseDate, in.OwnerUserID, in.TeamID,
			in.ExternalSource, in.ExternalID, orEmptyJSON(in.CustomFields), p.UserID))
		if err != nil {
			return wrapConstraint(err, "deal")
		}
		created = d
		const hist = `
			INSERT INTO deal_stage_history (deal_id, organization_id, from_stage_id, to_stage_id, changed_by, note)
			VALUES ($1,$2,NULL,$3,$4,'created')`
		if _, err := s.db.Q(ctx).Exec(ctx, hist, d.ID, p.OrganizationID, in.StageID, p.UserID); err != nil {
			return fmt.Errorf("record initial stage: %w", err)
		}
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventDealCreated, map[string]any{
			"deal_id": d.ID, "amount": d.Amount, "stage_id": d.StageID,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "deals.create", EntityType: "deal", EntityID: d.ID.String(),
			After: map[string]any{"title": d.Title, "amount": d.Amount},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateDeal applies a partial update. Moving to a different stage is handled by
// MoveDealStage so that the change is always recorded in the history.
func (s *Service) UpdateDeal(ctx context.Context, p *rbac.Principal, id uuid.UUID, in DealInput) (*Deal, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	var updated *Deal
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		before, err := s.GetDeal(ctx, p, id)
		if err != nil {
			return err
		}
		if err := s.assertStageInOrg(ctx, p.OrganizationID, in.PipelineID, in.StageID); err != nil {
			return err
		}
		if err := s.assertAccountInOrg(ctx, p.OrganizationID, in.AccountID); err != nil {
			return err
		}
		if in.DiscountPercent != before.DiscountPercent && !p.Has(rbac.PermDealsApproveDiscount) {
			return httpx.NewError(403, httpx.CodeForbidden,
				"changing the discount requires the deals.approve_discount permission")
		}
		if in.DiscountPercent != before.DiscountPercent {
			// A discount change is an approval action; record who approved it.
			const approve = `UPDATE deals SET discount_approved_by = $2, discount_approved_at = now() WHERE id = $1`
			if _, err := s.db.Q(ctx).Exec(ctx, approve, id, p.UserID); err != nil {
				return fmt.Errorf("record discount approval: %w", err)
			}
		}
		const q = `
			UPDATE deals SET pipeline_id=$3, stage_id=$4, account_id=$5, primary_contact_id=$6,
				title=$7, description=$8, amount=$9, currency=$10, discount_percent=$11,
				expected_close_date=$12, owner_user_id=$13, team_id=$14, external_source=$15,
				external_id=$16, custom_fields=$17
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + dealColumns
		d, err := scanDeal(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, in.PipelineID,
			in.StageID, in.AccountID, in.PrimaryContactID, in.Title, in.Description, in.Amount,
			in.Currency, in.DiscountPercent, in.ExpectedCloseDate, in.OwnerUserID, in.TeamID,
			in.ExternalSource, in.ExternalID, orEmptyJSON(in.CustomFields)))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return wrapConstraint(err, "deal")
		}
		updated = d
		if err := s.outbox.Emit(ctx, &p.OrganizationID, outbox.EventDealUpdated, map[string]any{
			"deal_id": d.ID, "amount": d.Amount,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "deals.update", EntityType: "deal", EntityID: id.String(),
			Before: map[string]any{"amount": before.Amount, "discount_percent": before.DiscountPercent},
			After:  map[string]any{"amount": d.Amount, "discount_percent": d.DiscountPercent},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// MoveDealStage moves a deal and appends to the stage history.
func (s *Service) MoveDealStage(ctx context.Context, p *rbac.Principal, id, stageID uuid.UUID, note string) (*Deal, error) {
	var updated *Deal
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		before, err := s.GetDeal(ctx, p, id)
		if err != nil {
			return err
		}
		var isWon, isLost bool
		var pipelineID uuid.UUID
		const stageQ = `
			SELECT pipeline_id, is_won, is_lost FROM pipeline_stages
			WHERE id = $1 AND organization_id = $2`
		if err := s.db.Q(ctx).QueryRow(ctx, stageQ, stageID, p.OrganizationID).Scan(&pipelineID, &isWon, &isLost); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.NewError(422, httpx.CodeValidation, "stage_id does not belong to this organization")
			}
			return fmt.Errorf("load stage: %w", err)
		}

		status := "open"
		switch {
		case isWon:
			status = "won"
		case isLost:
			status = "lost"
		}

		const q = `
			UPDATE deals SET stage_id=$3, status=$4,
				closed_at = CASE WHEN $4 = 'open' THEN NULL ELSE now() END
			WHERE id=$1 AND organization_id=$2
			RETURNING ` + dealColumns
		d, err := scanDeal(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, stageID, status))
		if err != nil {
			return fmt.Errorf("move deal stage: %w", err)
		}
		updated = d

		const hist = `
			INSERT INTO deal_stage_history (deal_id, organization_id, from_stage_id, to_stage_id, changed_by, note)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,''))`
		if _, err := s.db.Q(ctx).Exec(ctx, hist, id, p.OrganizationID, before.StageID, stageID, p.UserID, note); err != nil {
			return fmt.Errorf("record stage history: %w", err)
		}

		eventType := outbox.EventDealStageChanged
		if isWon {
			eventType = outbox.EventDealWon
		} else if isLost {
			eventType = outbox.EventDealLost
		}
		if err := s.outbox.Emit(ctx, &p.OrganizationID, eventType, map[string]any{
			"deal_id": id, "from_stage_id": before.StageID, "to_stage_id": stageID, "status": status,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "deals.change_stage", EntityType: "deal", EntityID: id.String(),
			Before: map[string]any{"stage_id": before.StageID, "status": before.Status},
			After:  map[string]any{"stage_id": stageID, "status": status},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DealStageHistory returns the stage history for a deal.
func (s *Service) DealStageHistory(ctx context.Context, p *rbac.Principal, dealID uuid.UUID) ([]map[string]any, error) {
	if _, err := s.GetDeal(ctx, p, dealID); err != nil {
		return nil, err
	}
	const q = `
		SELECT h.id, h.from_stage_id, h.to_stage_id, h.changed_by, u.display_name, h.note, h.changed_at
		FROM deal_stage_history h
		LEFT JOIN users u ON u.id = h.changed_by
		WHERE h.deal_id = $1 AND h.organization_id = $2
		ORDER BY h.changed_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, dealID, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("deal stage history: %w", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id                            int64
			fromStage, toStage, changedBy *uuid.UUID
			displayName, note             *string
			changedAt                     time.Time
		)
		if err := rows.Scan(&id, &fromStage, &toStage, &changedBy, &displayName, &note, &changedAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "from_stage_id": fromStage, "to_stage_id": toStage,
			"changed_by": changedBy, "changed_by_name": displayName, "note": note, "changed_at": changedAt,
		})
	}
	return out, rows.Err()
}

// ArchiveDeal archives a deal.
func (s *Service) ArchiveDeal(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `UPDATE deals SET archived_at = now() WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL`
		tag, err := s.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("archive deal: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "deals.archive", EntityType: "deal", EntityID: id.String(),
		})
	})
}

// assertStageInOrg verifies that a stage belongs to the given pipeline and
// organization, so a caller cannot attach a deal to another tenant's stage.
func (s *Service) assertStageInOrg(ctx context.Context, orgID, pipelineID, stageID uuid.UUID) error {
	const q = `SELECT 1 FROM pipeline_stages WHERE id = $1 AND pipeline_id = $2 AND organization_id = $3`
	var one int
	err := s.db.Q(ctx).QueryRow(ctx, q, stageID, pipelineID, orgID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NewError(422, httpx.CodeValidation,
			"stage_id does not belong to the given pipeline in this organization")
	}
	if err != nil {
		return fmt.Errorf("check stage scope: %w", err)
	}
	return nil
}
