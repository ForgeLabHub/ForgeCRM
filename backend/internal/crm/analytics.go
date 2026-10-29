package crm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/rbac"
)

// This file provides the read side of the intelligence platform: the observed
// baseline a scenario is computed against.
//
// Two properties matter and are why this is a separate, small reader rather than
// reuse of the report methods:
//
//  1. Exact money. The report structs scan numeric amounts into float64. A
//     simulation must not, so every amount here is read as its exact decimal
//     string (`::text` in SQL) and parsed into an exact value by the caller.
//  2. No mixed currencies. The reports sum nominal values across currencies and
//     warn. A scenario would silently combine dollars and euros, so this reader
//     picks one currency and reports the ones it excluded.
//
// The reader only reads. It has no write path and cannot be made to write.

// StageBaseline is one pipeline stage's observed open deals.
type StageBaseline struct {
	StageID     uuid.UUID
	StageName   string
	Position    int
	Probability int // 0..100
	OpenCount   int64
	OpenValue   string // exact decimal, in Currency
}

// BaselineData is the observed CRM data a scenario is computed against.
type BaselineData struct {
	Currency string
	Stages   []StageBaseline

	WonCount     int64
	LostCount    int64
	OpenCount    int64
	WonValue     string
	LostValue    string
	OpenValue    string
	AvgDealValue string

	ActiveCustomers  int64
	ChurnedCustomers int64

	PeriodDays int
	// Warnings describe what the reader could not include: an excluded currency,
	// a missing pipeline. They are surfaced to the user so a scenario is never
	// silently computed on less data than the user believes.
	Warnings []string
}

// BaselineInput selects the data to read.
type BaselineInput struct {
	// PipelineID selects the funnel. When nil, the organization's default
	// pipeline is used.
	PipelineID *uuid.UUID
	From, To   *time.Time
}

// Baseline reads the observed data for a scenario, scoped to the caller's
// organization and gated on the aggregate-reporting permission.
//
// It enforces reports.read rather than deals.read: the values are organization
// aggregates over every deal, not the caller's own records, so the permission
// that guards the existing reports is the correct one.
func (s *Service) Baseline(ctx context.Context, p *rbac.Principal, in BaselineInput) (*BaselineData, error) {
	if err := p.Require(rbac.PermReportsRead); err != nil {
		return nil, err
	}
	out := &BaselineData{}

	// Choose the currency the scenario is computed in: the one with the most
	// non-archived deals. Every aggregate below is filtered to it so the result is
	// never a sum of unlike currencies.
	currency, others, err := s.primaryCurrency(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	out.Currency = currency
	for _, c := range others {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("deals in %s were excluded; the scenario is computed in %s", c, currency))
	}

	if err := s.readPeriod(ctx, p.OrganizationID, currency, in, out); err != nil {
		return nil, err
	}
	if err := s.readCustomers(ctx, p.OrganizationID, in, out); err != nil {
		return nil, err
	}
	if err := s.readFunnel(ctx, p.OrganizationID, currency, in, out); err != nil {
		return nil, err
	}

	if in.From != nil && in.To != nil {
		days := int(in.To.Sub(*in.From).Hours() / 24)
		if days > 0 {
			out.PeriodDays = days
		}
	}
	return out, nil
}

// primaryCurrency returns the currency with the most non-archived deals, and the
// other currencies present.
func (s *Service) primaryCurrency(ctx context.Context, orgID uuid.UUID) (string, []string, error) {
	const q = `
		SELECT currency, count(*) AS n
		FROM deals
		WHERE organization_id = $1 AND archived_at IS NULL
		GROUP BY currency
		ORDER BY n DESC, currency ASC`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID)
	if err != nil {
		return "", nil, fmt.Errorf("read deal currencies: %w", err)
	}
	defer rows.Close()

	var primary string
	var others []string
	for rows.Next() {
		var cur string
		var n int64
		if err := rows.Scan(&cur, &n); err != nil {
			return "", nil, err
		}
		if primary == "" {
			primary = cur
		} else {
			others = append(others, cur)
		}
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if primary == "" {
		primary = "USD"
	}
	return primary, others, nil
}

// readPeriod fills the won/lost/open aggregates over the selected period.
func (s *Service) readPeriod(ctx context.Context, orgID uuid.UUID, currency string, in BaselineInput, out *BaselineData) error {
	const q = `
		SELECT
			COALESCE(count(*) FILTER (WHERE status = 'won'), 0),
			COALESCE(count(*) FILTER (WHERE status = 'lost'), 0),
			COALESCE(count(*) FILTER (WHERE status = 'open'), 0),
			COALESCE(sum(amount) FILTER (WHERE status = 'won'), 0)::text,
			COALESCE(sum(amount) FILTER (WHERE status = 'lost'), 0)::text,
			COALESCE(sum(amount) FILTER (WHERE status = 'open'), 0)::text,
			-- avg() over numeric(18,2) returns a higher scale (for example
			-- 500.0000000000000000), which the exact money parser refuses rather
			-- than truncating. Round to the money scale where the value is produced,
			-- so the reader hands the parser a value it can hold exactly.
			COALESCE(round(avg(amount), 2), 0)::text
		FROM deals
		WHERE organization_id = $1 AND archived_at IS NULL AND currency = $2
		  AND ($3::timestamptz IS NULL OR created_at >= $3)
		  AND ($4::timestamptz IS NULL OR created_at <= $4)`
	if err := s.db.Q(ctx).QueryRow(ctx, q, orgID, currency, in.From, in.To).Scan(
		&out.WonCount, &out.LostCount, &out.OpenCount,
		&out.WonValue, &out.LostValue, &out.OpenValue, &out.AvgDealValue); err != nil {
		return fmt.Errorf("read period aggregates: %w", err)
	}
	return nil
}

// readCustomers fills the customer counts the retention model uses.
//
// The definition is deliberately simple and stated: an active customer is an
// account with at least one non-archived deal; a churned customer is an account
// whose every non-archived deal is lost. This is a parameterisation, not a
// learned churn model, and the documentation says so.
func (s *Service) readCustomers(ctx context.Context, orgID uuid.UUID, in BaselineInput, out *BaselineData) error {
	const q = `
		WITH per_account AS (
			SELECT account_id,
			       count(*) FILTER (WHERE status IN ('open','won')) AS live,
			       count(*) FILTER (WHERE status = 'lost') AS lost
			FROM deals
			WHERE organization_id = $1 AND archived_at IS NULL AND account_id IS NOT NULL
			  AND ($2::timestamptz IS NULL OR created_at >= $2)
			  AND ($3::timestamptz IS NULL OR created_at <= $3)
			GROUP BY account_id
		)
		SELECT
			COALESCE(count(*), 0),
			COALESCE(count(*) FILTER (WHERE live = 0 AND lost > 0), 0)
		FROM per_account`
	if err := s.db.Q(ctx).QueryRow(ctx, q, orgID, in.From, in.To).Scan(
		&out.ActiveCustomers, &out.ChurnedCustomers); err != nil {
		return fmt.Errorf("read customer counts: %w", err)
	}
	return nil
}

// readFunnel fills the per-stage open-deal funnel for the selected pipeline.
func (s *Service) readFunnel(ctx context.Context, orgID uuid.UUID, currency string, in BaselineInput, out *BaselineData) error {
	pipelineID := in.PipelineID
	if pipelineID == nil {
		id, err := s.defaultPipelineID(ctx, orgID)
		if err != nil {
			return err
		}
		if id == nil {
			out.Warnings = append(out.Warnings,
				"no pipeline is configured, so the funnel has no stages")
			return nil
		}
		pipelineID = id
	}

	const q = `
		SELECT s.id, s.name, s.position, s.probability,
		       count(d.id),
		       COALESCE(sum(d.amount), 0)::text
		FROM pipeline_stages s
		LEFT JOIN deals d ON d.stage_id = s.id AND d.archived_at IS NULL
		     AND d.status = 'open' AND d.currency = $3
		WHERE s.pipeline_id = $1 AND s.organization_id = $2
		GROUP BY s.id, s.name, s.position, s.probability
		ORDER BY s.position`
	rows, err := s.db.Q(ctx).Query(ctx, q, *pipelineID, orgID, currency)
	if err != nil {
		return fmt.Errorf("read funnel: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st StageBaseline
		if err := rows.Scan(&st.StageID, &st.StageName, &st.Position, &st.Probability,
			&st.OpenCount, &st.OpenValue); err != nil {
			return err
		}
		out.Stages = append(out.Stages, st)
	}
	return rows.Err()
}

// defaultPipelineID returns the organization's default pipeline, or nil when
// none exists.
func (s *Service) defaultPipelineID(ctx context.Context, orgID uuid.UUID) (*uuid.UUID, error) {
	const q = `
		SELECT id FROM pipelines
		WHERE organization_id = $1 AND archived_at IS NULL
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1`
	var id uuid.UUID
	err := s.db.Q(ctx).QueryRow(ctx, q, orgID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find default pipeline: %w", err)
	}
	return &id, nil
}
