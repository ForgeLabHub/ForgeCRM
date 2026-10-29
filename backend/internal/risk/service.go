// Package risk implements the Risk half of the Forecasting & Risk Lab: scanning
// the organization's CRM for explainable risk conditions and keeping the signals
// an operator works through.
//
// The package is an application service in the same sense as simulate and
// forecast: it reads observations through crm.Service with the caller's
// principal, hands them to the pure detectors in internal/analytics, and persists
// the signals. It never writes CRM data. The only writes are to the risk tables
// the organization owns.
//
// A scan reconciles rather than appends. For each condition that fired it upserts
// one live signal, so a repeated scan does not duplicate a signal, and an
// operator's acknowledgement survives the next scan. Conditions that no longer
// hold are closed by the scan and marked as such, so the list reflects the
// current state of the CRM rather than an ever-growing log.
package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Service is the Risk application service.
type Service struct {
	db    *database.DB
	crm   *crm.Service
	audit *audit.Logger
	now   func() time.Time
}

// NewService builds the risk service.
func NewService(db *database.DB, crmSvc *crm.Service, auditLog *audit.Logger) *Service {
	return &Service{db: db, crm: crmSvc, audit: auditLog, now: time.Now}
}

// Signal is a persisted risk signal.
type Signal struct {
	ID               uuid.UUID                `json:"id"`
	OrganizationID   uuid.UUID                `json:"organization_id"`
	DetectedAt       time.Time                `json:"detected_at"`
	Type             analytics.RiskSignalType `json:"type"`
	Severity         analytics.RiskSeverity   `json:"severity"`
	Rule             string                   `json:"rule"`
	AlgorithmVersion string                   `json:"algorithm_version"`
	EntityType       string                   `json:"entity_type"`
	EntityID         string                   `json:"entity_id"`
	EntityLabel      string                   `json:"entity_label"`
	Rationale        string                   `json:"rationale"`
	Evidence         json.RawMessage          `json:"evidence"`
	Status           string                   `json:"status"`
	StatusChangedAt  *time.Time               `json:"status_changed_at,omitempty"`
	ResolvedByScan   bool                     `json:"resolved_by_scan"`
}

// Thresholds is the organization's risk threshold configuration.
type Thresholds struct {
	StalledDealDays         int `json:"stalled_deal_days"`
	OverdueTaskDays         int `json:"overdue_task_days"`
	AccountNoContactDays    int `json:"account_no_contact_days"`
	AmountSpikeRatio        int `json:"amount_spike_ratio"`
	ConversionDropRatio     int `json:"conversion_drop_ratio"`
	StageConcentrationRatio int `json:"stage_concentration_ratio"`
}

// toAnalytics converts the stored thresholds into the pure core's form.
func (t Thresholds) toAnalytics() analytics.RiskThresholds {
	return analytics.RiskThresholds{
		StalledDealDays:         t.StalledDealDays,
		OverdueTaskDays:         t.OverdueTaskDays,
		AccountNoContactDays:    t.AccountNoContactDays,
		AmountSpikeRatio:        analytics.NewRatioFromBasisPoints(int64(t.AmountSpikeRatio)),
		ConversionDropRatio:     analytics.NewRatioFromBasisPoints(int64(t.ConversionDropRatio)),
		StageConcentrationRatio: analytics.NewRatioFromBasisPoints(int64(t.StageConcentrationRatio)),
	}
}

// fromAnalyticsDefaults builds the threshold row from the core's defaults, so the
// database default and the code default cannot drift.
func fromAnalyticsDefaults() Thresholds {
	d := analytics.DefaultRiskThresholds()
	return Thresholds{
		StalledDealDays:         d.StalledDealDays,
		OverdueTaskDays:         d.OverdueTaskDays,
		AccountNoContactDays:    d.AccountNoContactDays,
		AmountSpikeRatio:        int(d.AmountSpikeRatio.BasisPoints),
		ConversionDropRatio:     int(d.ConversionDropRatio.BasisPoints),
		StageConcentrationRatio: int(d.StageConcentrationRatio.BasisPoints),
	}
}

// Sentinel errors mapped to HTTP statuses by the API layer.
var (
	// ErrNotFound is returned when a signal does not exist in the caller's organization.
	ErrNotFound = errors.New("risk signal not found")
	// ErrValidation is returned for a request that is well-formed but invalid.
	ErrValidation = errors.New("invalid risk request")
	// ErrBadStatus is returned for a status transition that is not permitted.
	ErrBadStatus = errors.New("invalid signal status")
)

// ScanResult summarises one scan.
type ScanResult struct {
	Detected int `json:"detected"`
	Opened   int `json:"opened"`
	Updated  int `json:"updated"`
	Resolved int `json:"resolved"`
	// Signals is the full live list after the scan, so a caller needs no second read.
	Signals []Signal `json:"signals"`
}

// Thresholds returns the organization's thresholds, or the defaults when it has
// never set any. Reading is gated on risk.read; a caller who may see signals may
// see the thresholds that produced them.
func (s *Service) Thresholds(ctx context.Context, p *rbac.Principal) (Thresholds, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return Thresholds{}, err
	}
	const q = `
		SELECT stalled_deal_days, overdue_task_days, account_no_contact_days,
		       amount_spike_ratio, conversion_drop_ratio, stage_concentration_ratio
		FROM risk_thresholds WHERE organization_id = $1`
	var t Thresholds
	err := s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID).Scan(
		&t.StalledDealDays, &t.OverdueTaskDays, &t.AccountNoContactDays,
		&t.AmountSpikeRatio, &t.ConversionDropRatio, &t.StageConcentrationRatio)
	if err != nil {
		if database.IsNoRows(err) {
			return fromAnalyticsDefaults(), nil
		}
		return Thresholds{}, fmt.Errorf("read risk thresholds: %w", err)
	}
	return t, nil
}

// SetThresholds stores the organization's thresholds after validating them
// against the same rules the detector applies.
func (s *Service) SetThresholds(ctx context.Context, p *rbac.Principal, t Thresholds) (Thresholds, error) {
	if err := p.Require(rbac.PermRiskManage); err != nil {
		return Thresholds{}, err
	}
	if err := t.toAnalytics().Validate(); err != nil {
		return Thresholds{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	// A zero for a ratio or a day count is not a valid stored value (the columns
	// have CHECKs), and it means "unset" to the caller. Treat it as "leave the
	// default" so a partial form does not fail, matching the detector's behaviour.
	def := fromAnalyticsDefaults()
	if t.StalledDealDays <= 0 {
		t.StalledDealDays = def.StalledDealDays
	}
	if t.OverdueTaskDays <= 0 {
		t.OverdueTaskDays = def.OverdueTaskDays
	}
	if t.AccountNoContactDays <= 0 {
		t.AccountNoContactDays = def.AccountNoContactDays
	}
	if t.AmountSpikeRatio <= 0 {
		t.AmountSpikeRatio = def.AmountSpikeRatio
	}
	if t.ConversionDropRatio <= 0 {
		t.ConversionDropRatio = def.ConversionDropRatio
	}
	if t.StageConcentrationRatio <= 0 {
		t.StageConcentrationRatio = def.StageConcentrationRatio
	}
	// Validate against the same bounds the columns enforce, so an out-of-range value
	// is refused with a clear reason rather than surfacing a raw constraint error.
	if err := t.validateBounds(); err != nil {
		return Thresholds{}, err
	}

	const q = `
		INSERT INTO risk_thresholds
			(organization_id, stalled_deal_days, overdue_task_days, account_no_contact_days,
			 amount_spike_ratio, conversion_drop_ratio, stage_concentration_ratio, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (organization_id) DO UPDATE SET
			stalled_deal_days = EXCLUDED.stalled_deal_days,
			overdue_task_days = EXCLUDED.overdue_task_days,
			account_no_contact_days = EXCLUDED.account_no_contact_days,
			amount_spike_ratio = EXCLUDED.amount_spike_ratio,
			conversion_drop_ratio = EXCLUDED.conversion_drop_ratio,
			stage_concentration_ratio = EXCLUDED.stage_concentration_ratio,
			updated_by = EXCLUDED.updated_by`
	if _, err := s.db.Q(ctx).Exec(ctx, q, p.OrganizationID, t.StalledDealDays, t.OverdueTaskDays,
		t.AccountNoContactDays, t.AmountSpikeRatio, t.ConversionDropRatio,
		t.StageConcentrationRatio, p.UserID); err != nil {
		return Thresholds{}, fmt.Errorf("save risk thresholds: %w", err)
	}

	if err := s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         "risk.thresholds.updated",
		EntityType:     "risk_thresholds",
		EntityID:       p.OrganizationID.String(),
		After:          t,
	}); err != nil {
		return Thresholds{}, err
	}
	return t, nil
}

// Scan reads the CRM, runs the detectors, and reconciles the stored signals. It
// is idempotent: a second scan with no CRM change opens nothing and resolves
// nothing.
func (s *Service) Scan(ctx context.Context, p *rbac.Principal) (*ScanResult, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return nil, err
	}
	thresholds, err := s.Thresholds(ctx, p)
	if err != nil {
		return nil, err
	}
	obs, err := s.crm.ReadRiskObservations(ctx, p)
	if err != nil {
		if errors.Is(err, rbac.ErrForbidden) {
			return nil, err
		}
		return nil, fmt.Errorf("read risk observations: %w", err)
	}

	detected := detect(obs, thresholds.toAnalytics())

	result := &ScanResult{Detected: len(detected)}
	err = s.db.WithTx(ctx, func(txctx context.Context, _ database.Tx) error {
		// Live signals before the scan, keyed by rule+entity, so we can tell an
		// insert from an update and know which live signals are no longer matched.
		live, err := s.liveSignals(txctx, p.OrganizationID)
		if err != nil {
			return err
		}
		detectedKeys := map[string]bool{}
		for _, sig := range detected {
			key := signalKey(sig.Rule, sig.EntityType, sig.EntityID)
			detectedKeys[key] = true
			evidence, err := json.Marshal(sig.Evidence)
			if err != nil {
				return fmt.Errorf("encode evidence: %w", err)
			}
			opened, err := s.upsertSignal(txctx, p.OrganizationID, sig, evidence)
			if err != nil {
				return err
			}
			if opened {
				result.Opened++
			} else {
				result.Updated++
			}
		}
		// Close the live signals whose condition no longer holds.
		for key, id := range live {
			if detectedKeys[key] {
				continue
			}
			if err := s.resolveByScan(txctx, p.OrganizationID, id); err != nil {
				return err
			}
			result.Resolved++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// One audit entry per scan that changed anything, so a scan is attributable
	// without writing an entry per signal.
	if result.Opened > 0 || result.Updated > 0 || result.Resolved > 0 {
		if err := s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         "risk.scan.completed",
			EntityType:     "risk_signal",
			Metadata: map[string]any{
				"detected": result.Detected, "opened": result.Opened,
				"updated": result.Updated, "resolved": result.Resolved,
			},
		}); err != nil {
			return nil, err
		}
	}

	list, err := s.ListSignals(ctx, p, ListFilter{})
	if err != nil {
		return nil, err
	}
	result.Signals = list
	return result, nil
}

// detect runs every detector and returns the union, most severe first.
func detect(obs *crm.RiskObservationSet, th analytics.RiskThresholds) []analytics.RiskSignal {
	deals := make([]analytics.DealObservation, 0, len(obs.AllOpenDeals))
	for _, d := range obs.AllOpenDeals {
		deals = append(deals, analytics.DealObservation{
			ID: d.ID.String(), Title: d.Title, AmountMinor: d.AmountMinor, Currency: d.Currency,
			StageID: d.StageID.String(), StageName: d.StageName,
			DaysSinceActivity: d.DaysSinceActivity, CloseDatePassed: d.CloseDatePassed,
		})
	}
	tasks := make([]analytics.TaskObservation, 0, len(obs.OverdueTasks))
	for _, t := range obs.OverdueTasks {
		tasks = append(tasks, analytics.TaskObservation{
			ID: t.ID.String(), Title: t.Title, DaysOverdue: t.DaysOverdue, Assignee: t.Assignee,
		})
	}
	accounts := make([]analytics.AccountObservation, 0, len(obs.AccountsWithoutContact))
	for _, a := range obs.AccountsWithoutContact {
		accounts = append(accounts, analytics.AccountObservation{
			ID: a.ID.String(), Name: a.Name, ContactCount: a.ContactCount, AgeDays: a.AgeDays,
		})
	}
	periods := make([]analytics.ConversionPeriodObservation, 0, len(obs.Conversion))
	for _, c := range obs.Conversion {
		periods = append(periods, analytics.ConversionPeriodObservation{
			Period: c.Period, Won: c.Won, Lost: c.Lost,
		})
	}

	var out []analytics.RiskSignal
	out = append(out, analytics.DetectStalledDeals(deals, th)...)
	out = append(out, analytics.DetectOverdueTasks(tasks, th)...)
	out = append(out, analytics.DetectAccountsWithoutContact(accounts, th)...)
	out = append(out, analytics.DetectAmountSpikes(deals, th)...)
	out = append(out, analytics.DetectConversionDrops(periods, th, 3)...)
	out = append(out, analytics.DetectStageConcentration(deals, th)...)
	analytics.SortSignals(out)
	return out
}

// liveSignals returns the id of each live signal, keyed by rule+entity.
func (s *Service) liveSignals(ctx context.Context, orgID uuid.UUID) (map[string]uuid.UUID, error) {
	const q = `
		SELECT id, rule, entity_type, entity_id
		FROM risk_signals
		WHERE organization_id = $1 AND status IN ('open', 'acknowledged')`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("read live signals: %w", err)
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var rule, entityType, entityID string
		if err := rows.Scan(&id, &rule, &entityType, &entityID); err != nil {
			return nil, err
		}
		out[signalKey(rule, entityType, entityID)] = id
	}
	return out, rows.Err()
}

// upsertSignal inserts a signal or refreshes the live one for the same
// rule+entity. It reports whether a new signal was opened (true) or an existing
// one was updated (false). An operator's status is preserved on update.
func (s *Service) upsertSignal(ctx context.Context, orgID uuid.UUID, sig analytics.RiskSignal, evidence []byte) (bool, error) {
	const q = `
		INSERT INTO risk_signals
			(organization_id, type, severity, rule, algorithm_version, entity_type, entity_id,
			 entity_label, rationale, evidence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (organization_id, rule, entity_type, entity_id)
			WHERE status IN ('open', 'acknowledged')
		DO UPDATE SET
			severity = EXCLUDED.severity,
			rationale = EXCLUDED.rationale,
			evidence = EXCLUDED.evidence,
			entity_label = EXCLUDED.entity_label,
			detected_at = now()
		RETURNING (xmax = 0) AS inserted`
	var inserted bool
	if err := s.db.Q(ctx).QueryRow(ctx, q, orgID, sig.Type, sig.Severity, sig.Rule,
		sig.AlgorithmVersion, sig.EntityType, sig.EntityID, sig.EntityLabel, sig.Rationale, evidence).
		Scan(&inserted); err != nil {
		return false, fmt.Errorf("upsert risk signal: %w", err)
	}
	return inserted, nil
}

// resolveByScan closes a signal whose condition no longer holds.
func (s *Service) resolveByScan(ctx context.Context, orgID, id uuid.UUID) error {
	const q = `
		UPDATE risk_signals
		SET status = 'resolved', resolved_by_scan = true, status_changed_at = now()
		WHERE id = $1 AND organization_id = $2 AND status IN ('open', 'acknowledged')`
	if _, err := s.db.Q(ctx).Exec(ctx, q, id, orgID); err != nil {
		return fmt.Errorf("resolve stale signal: %w", err)
	}
	return nil
}

// ListFilter narrows the signal list.
type ListFilter struct {
	// Status, when set, restricts to one status.
	Status string
	// Severity, when set, restricts to one severity.
	Severity string
	// Type, when set, restricts to one signal type.
	Type string
	// IncludeTerminal includes resolved and dismissed signals, which are hidden by
	// default so the list shows what still needs attention.
	IncludeTerminal bool
}

// ListSignals returns the organization's signals, most severe first. Terminal
// signals are hidden unless the filter asks for them.
func (s *Service) ListSignals(ctx context.Context, p *rbac.Principal, f ListFilter) ([]Signal, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return nil, err
	}
	where := []string{"organization_id = $1"}
	args := []any{p.OrganizationID}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	} else if !f.IncludeTerminal {
		where = append(where, "status IN ('open', 'acknowledged')")
	}
	if f.Severity != "" {
		args = append(args, f.Severity)
		where = append(where, fmt.Sprintf("severity = $%d", len(args)))
	}
	if f.Type != "" {
		args = append(args, f.Type)
		where = append(where, fmt.Sprintf("type = $%d", len(args)))
	}
	q := `
		SELECT id, organization_id, detected_at, type, severity, rule, algorithm_version,
		       entity_type, entity_id, entity_label, rationale, evidence, status,
		       status_changed_at, resolved_by_scan
		FROM risk_signals
		WHERE ` + joinAnd(where) + `
		ORDER BY CASE severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3
		                       WHEN 'warn' THEN 2 WHEN 'info' THEN 1 ELSE 0 END DESC,
		         detected_at DESC
		LIMIT 500`
	rows, err := s.db.Q(ctx).Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list risk signals: %w", err)
	}
	defer rows.Close()
	var out []Signal
	for rows.Next() {
		sig, err := scanSignal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sig)
	}
	return out, rows.Err()
}

// GetSignal returns one signal, scoped to the organization.
func (s *Service) GetSignal(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Signal, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return nil, err
	}
	const q = `
		SELECT id, organization_id, detected_at, type, severity, rule, algorithm_version,
		       entity_type, entity_id, entity_label, rationale, evidence, status,
		       status_changed_at, resolved_by_scan
		FROM risk_signals WHERE id = $1 AND organization_id = $2`
	sig, err := scanSignal(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load risk signal: %w", err)
	}
	return sig, nil
}

// SetStatus moves a signal to a new status. Only the three operator transitions
// are permitted — acknowledged, dismissed, resolved — and the move is recorded.
func (s *Service) SetStatus(ctx context.Context, p *rbac.Principal, id uuid.UUID, status string) (*Signal, error) {
	if err := p.Require(rbac.PermRiskRead); err != nil {
		return nil, err
	}
	switch status {
	case "acknowledged", "dismissed", "resolved":
	default:
		return nil, fmt.Errorf("%w: %q", ErrBadStatus, status)
	}
	const q = `
		UPDATE risk_signals
		SET status = $3, status_changed_at = now(), status_changed_by = $4, resolved_by_scan = false
		WHERE id = $1 AND organization_id = $2
		RETURNING id, organization_id, detected_at, type, severity, rule, algorithm_version,
		          entity_type, entity_id, entity_label, rationale, evidence, status,
		          status_changed_at, resolved_by_scan`
	sig, err := scanSignal(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, status, p.UserID))
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update signal status: %w", err)
	}
	if err := s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         "risk.signal.status_changed",
		EntityType:     "risk_signal",
		EntityID:       id.String(),
		After:          map[string]any{"status": status},
	}); err != nil {
		return nil, err
	}
	return sig, nil
}

// validateBounds checks a fully-defaulted threshold set against the same bounds
// the columns enforce, so a range error is a 422 with a reason rather than a raw
// constraint violation.
func (t Thresholds) validateBounds() error {
	bounds := []struct {
		name string
		v    int
		lo   int
		hi   int
	}{
		{"stalled_deal_days", t.StalledDealDays, 1, 3650},
		{"overdue_task_days", t.OverdueTaskDays, 1, 3650},
		{"account_no_contact_days", t.AccountNoContactDays, 1, 3650},
		{"amount_spike_ratio", t.AmountSpikeRatio, 10000, 1000000},
		{"conversion_drop_ratio", t.ConversionDropRatio, 1, 10000},
		{"stage_concentration_ratio", t.StageConcentrationRatio, 1, 10000},
	}
	for _, b := range bounds {
		if b.v < b.lo || b.v > b.hi {
			return fmt.Errorf("%w: %s must be between %d and %d, got %d",
				ErrValidation, b.name, b.lo, b.hi, b.v)
		}
	}
	return nil
}

// signalKey is the identity of a live signal: the rule and the record it is about.
func signalKey(rule, entityType, entityID string) string {
	return rule + "\x00" + entityType + "\x00" + entityID
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// rowScanner is the subset of pgx.Row and pgx.Rows this file scans with.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSignal(row rowScanner) (*Signal, error) {
	var s Signal
	if err := row.Scan(&s.ID, &s.OrganizationID, &s.DetectedAt, &s.Type, &s.Severity, &s.Rule,
		&s.AlgorithmVersion, &s.EntityType, &s.EntityID, &s.EntityLabel, &s.Rationale,
		&s.Evidence, &s.Status, &s.StatusChangedAt, &s.ResolvedByScan); err != nil {
		return nil, err
	}
	return &s, nil
}
