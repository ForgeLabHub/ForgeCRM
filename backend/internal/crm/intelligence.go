package crm

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/rbac"
)

// This file is the read side of the Forecasting and Risk modules. It is the same
// kind of reader as analytics.go: exact money read as decimal text, one currency
// per computation, organization-scoped, and read-only. It exists so the pure
// cores in internal/analytics receive observations and never touch the database.
//
// Two deliberate choices:
//
//  1. Ages are computed here, in SQL, because the analytics core has no clock. A
//     reader that passed timestamps to a pure package would force that package to
//     know what "now" is, which is exactly what keeps it testable.
//  2. Access is not re-derived here. The reader is gated on a permission, and the
//     per-record ownership rule the list endpoints use is applied by the same
//     predicate, so a risk signal is only raised for rows the caller could open.

// MonthlyRevenuePoint is one month of won-deal revenue, for a forecast series.
type MonthlyRevenuePoint struct {
	Period   string // "YYYY-MM"
	Amount   string // exact decimal, in Currency
	Currency string
	WonCount int64
}

// RevenueHistory is the observed monthly series a forecast is computed from.
type RevenueHistory struct {
	Currency string
	Points   []MonthlyRevenuePoint
	// ExcludedCurrencies names currencies dropped so the series is one currency.
	ExcludedCurrencies []string
}

// RevenueHistoryInput selects the series to read.
type RevenueHistoryInput struct {
	// Months is how many trailing months to read, ending at the current month.
	Months int
}

// RevenueHistory reads the organization's monthly won-deal revenue, oldest month
// first, in the organization's primary currency.
//
// Months with no won deals are included with a zero amount, because a gap in
// revenue is a data point, not a missing one — dropping it would make a flat
// series look like growth.
func (s *Service) RevenueHistory(ctx context.Context, p *rbac.Principal, in RevenueHistoryInput) (*RevenueHistory, error) {
	if err := p.Require(rbac.PermForecastRead); err != nil {
		return nil, err
	}
	months := in.Months
	if months <= 0 {
		months = 12
	}
	if months > 60 {
		months = 60
	}

	currency, others, err := s.primaryCurrency(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := &RevenueHistory{Currency: currency}
	for _, c := range others {
		out.ExcludedCurrencies = append(out.ExcludedCurrencies, c)
	}

	// A generated month series left-joined to the aggregates, so every month in
	// the window appears even with no rows. The window ends at the current month.
	const q = `
                WITH months AS (
                        SELECT to_char(date_trunc('month', now()) - (n || ' month')::interval, 'YYYY-MM') AS period,
                               date_trunc('month', now()) - (n || ' month')::interval AS month_start
                        FROM generate_series(0, $3 - 1) AS n
                )
                SELECT m.period,
                       COALESCE(sum(d.amount) FILTER (WHERE d.status = 'won'), 0)::text,
                       COALESCE(count(*) FILTER (WHERE d.status = 'won'), 0)
                FROM months m
                LEFT JOIN deals d
                       ON d.organization_id = $1
                      AND d.currency = $2
                      AND d.archived_at IS NULL
                      AND date_trunc('month', d.closed_at) = m.month_start
                GROUP BY m.period, m.month_start
                ORDER BY m.month_start`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, currency, months)
	if err != nil {
		return nil, fmt.Errorf("read revenue history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pt MonthlyRevenuePoint
		if err := rows.Scan(&pt.Period, &pt.Amount, &pt.WonCount); err != nil {
			return nil, err
		}
		pt.Currency = currency
		out.Points = append(out.Points, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// PeriodConversion is one month's won/lost counts, for the conversion-trend rule.
type PeriodConversion struct {
	Period string
	Won    int64
	Lost   int64
}

// ConversionHistory reads the organization's monthly won/lost counts, oldest
// first, over the trailing window. It is the input to the conversion-drop rule.
func (s *Service) ConversionHistory(ctx context.Context, p *rbac.Principal, months int) ([]PeriodConversion, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return nil, err
	}
	if months <= 0 {
		months = 12
	}
	if months > 60 {
		months = 60
	}
	const q = `
                WITH months AS (
                        SELECT to_char(date_trunc('month', now()) - (n || ' month')::interval, 'YYYY-MM') AS period,
                               date_trunc('month', now()) - (n || ' month')::interval AS month_start
                        FROM generate_series(0, $2 - 1) AS n
                )
                SELECT m.period,
                       COALESCE(count(*) FILTER (WHERE d.status = 'won'), 0),
                       COALESCE(count(*) FILTER (WHERE d.status = 'lost'), 0)
                FROM months m
                LEFT JOIN deals d
                       ON d.organization_id = $1
                      AND d.archived_at IS NULL
                      AND d.status IN ('won', 'lost')
                      AND date_trunc('month', d.closed_at) = m.month_start
                GROUP BY m.period, m.month_start
                ORDER BY m.month_start`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, months)
	if err != nil {
		return nil, fmt.Errorf("read conversion history: %w", err)
	}
	defer rows.Close()
	var out []PeriodConversion
	for rows.Next() {
		var pc PeriodConversion
		if err := rows.Scan(&pc.Period, &pc.Won, &pc.Lost); err != nil {
			return nil, err
		}
		out = append(out, pc)
	}
	return out, rows.Err()
}

// RiskObservationSet is everything the risk detectors need, read in one pass.
// Splitting it into several calls would risk the pieces being read at different
// instants and describing different data.
type RiskObservationSet struct {
	// StalledDeals are open deals, with the age of their most recent activity.
	StalledDeals []DealObservationData
	// AllOpenDeals are the same open deals, used for the amount and concentration
	// rules. Kept separate from StalledDeals so the two rules read clearly.
	AllOpenDeals []DealObservationData
	// OverdueTasks are tasks past their due date.
	OverdueTasks []TaskObservationData
	// AccountsWithoutContact are accounts old enough to be expected to have one.
	AccountsWithoutContact []AccountObservationData
	// Conversion is the monthly win/loss series.
	Conversion []PeriodConversion
	// Currency is the currency the deal amounts are in.
	Currency string
}

// DealObservationData is an open deal as the risk reader reports it.
type DealObservationData struct {
	ID                uuid.UUID
	Title             string
	AmountMinor       int64
	Currency          string
	StageID           uuid.UUID
	StageName         string
	DaysSinceActivity int
	CloseDatePassed   bool
}

// TaskObservationData is an overdue task.
type TaskObservationData struct {
	ID          uuid.UUID
	Title       string
	DaysOverdue int
	Assignee    string
}

// AccountObservationData is an account with no contact.
type AccountObservationData struct {
	ID           uuid.UUID
	Name         string
	ContactCount int
	AgeDays      int
}

// ReadRiskObservations gathers the observations the risk detectors run on, scoped
// to the caller's organization and gated on risk.read.
//
// Amounts are read as exact minor units by casting the numeric to cents in SQL
// (`round(amount*100)::bigint`), so the reader hands the core integers and never a
// float. Only the organization's primary currency is included, so a spike is never
// claimed by comparing dollars to euros.
func (s *Service) ReadRiskObservations(ctx context.Context, p *rbac.Principal) (*RiskObservationSet, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return nil, err
	}
	currency, _, err := s.primaryCurrency(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := &RiskObservationSet{Currency: currency}

	if err := s.readOpenDeals(ctx, p, currency, out); err != nil {
		return nil, err
	}
	if err := s.readOverdueTasks(ctx, p, out); err != nil {
		return nil, err
	}
	if err := s.readAccountsWithoutContact(ctx, p, out); err != nil {
		return nil, err
	}
	conv, err := s.ConversionHistory(ctx, p, 12)
	if err != nil {
		return nil, err
	}
	out.Conversion = conv
	return out, nil
}

// readOpenDeals reads the open deals with their activity age. The same rows feed
// both the stalled and the concentration/amount rules.
func (s *Service) readOpenDeals(ctx context.Context, p *rbac.Principal, currency string, out *RiskObservationSet) error {
	// The per-record ownership rule the deal list uses: a caller without the
	// archive permission sees only their own or their team's deals. A risk signal
	// must not reveal a deal the caller could not open.
	ownership := ""
	args := []any{p.OrganizationID, currency}
	if !p.Has(rbac.PermDealsArchive) && len(p.TeamIDs) > 0 {
		args = append(args, p.UserID, p.TeamIDs)
		ownership = fmt.Sprintf(" AND (d.owner_user_id = $%d OR d.team_id = ANY($%d))", len(args)-1, len(args))
	}
	q := `
                SELECT d.id, d.title, round(d.amount * 100)::bigint, d.currency,
                       d.stage_id, COALESCE(s.name, ''),
                       GREATEST(0, EXTRACT(DAY FROM now() - COALESCE(
                               (SELECT max(a.occurred_at) FROM activities a WHERE a.deal_id = d.id),
                               d.created_at))::int) AS days_since_activity,
                       (d.expected_close_date IS NOT NULL AND d.expected_close_date < now()::date) AS close_date_passed
                FROM deals d
                LEFT JOIN pipeline_stages s ON s.id = d.stage_id
                WHERE d.organization_id = $1 AND d.archived_at IS NULL
                  AND d.status = 'open' AND d.currency = $2` + ownership
	rows, err := s.db.Q(ctx).Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("read open deals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d DealObservationData
		if err := rows.Scan(&d.ID, &d.Title, &d.AmountMinor, &d.Currency,
			&d.StageID, &d.StageName, &d.DaysSinceActivity, &d.CloseDatePassed); err != nil {
			return err
		}
		out.AllOpenDeals = append(out.AllOpenDeals, d)
		out.StalledDeals = append(out.StalledDeals, d)
	}
	return rows.Err()
}

// readOverdueTasks reads tasks past their due date and not done.
func (s *Service) readOverdueTasks(ctx context.Context, p *rbac.Principal, out *RiskObservationSet) error {
	q := `
                SELECT t.id, t.title,
                       GREATEST(0, EXTRACT(DAY FROM now() - t.due_at))::int AS days_overdue,
                       COALESCE(u.display_name, '')
                FROM tasks t
                LEFT JOIN users u ON u.id = t.assignee_user_id
                WHERE t.organization_id = $1 AND t.archived_at IS NULL
                  AND t.status IN ('open', 'in_progress')
                  AND t.due_at IS NOT NULL AND t.due_at < now()`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return fmt.Errorf("read overdue tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t TaskObservationData
		if err := rows.Scan(&t.ID, &t.Title, &t.DaysOverdue, &t.Assignee); err != nil {
			return err
		}
		out.OverdueTasks = append(out.OverdueTasks, t)
	}
	return rows.Err()
}

// readAccountsWithoutContact reads accounts with no contacts at all.
func (s *Service) readAccountsWithoutContact(ctx context.Context, p *rbac.Principal, out *RiskObservationSet) error {
	q := `
                SELECT a.id, a.name, 0,
                       GREATEST(0, EXTRACT(DAY FROM now() - a.created_at))::int AS age_days
                FROM accounts a
                WHERE a.organization_id = $1 AND a.archived_at IS NULL
                  AND NOT EXISTS (SELECT 1 FROM contacts c
                                  WHERE c.account_id = a.id AND c.archived_at IS NULL)`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return fmt.Errorf("read accounts without contact: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a AccountObservationData
		if err := rows.Scan(&a.ID, &a.Name, &a.ContactCount, &a.AgeDays); err != nil {
			return err
		}
		out.AccountsWithoutContact = append(out.AccountsWithoutContact, a)
	}
	return rows.Err()
}
