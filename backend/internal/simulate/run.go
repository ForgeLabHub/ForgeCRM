package simulate

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
	"github.com/geff1778/forgecrm/internal/rbac"
)

// This file holds the compute path: turning stored parameters and a freshly read
// baseline into a stored run.

// RunInput selects what to compute.
type RunInput struct {
	// PipelineID restricts the funnel baseline; nil uses the default pipeline.
	PipelineID *uuid.UUID
	From, To   *time.Time
}

// PreviewInput is an unsaved parameter set to compute without persisting.
type PreviewInput struct {
	Kind       analytics.Kind
	Params     json.RawMessage
	PipelineID *uuid.UUID
	From, To   *time.Time
}

// previewResult is what a preview returns: the comparison plus the baseline it
// was computed against, so the caller can see both without a stored run.
type PreviewResult struct {
	Comparison *analytics.Comparison `json:"comparison"`
	Baseline   *analytics.Baseline   `json:"baseline"`
	Warnings   []string              `json:"warnings,omitempty"`
}

// Preview computes a scenario without persisting it.
func (s *Service) Preview(ctx context.Context, p *rbac.Principal, in PreviewInput) (*PreviewResult, error) {
	if err := p.Require(rbac.PermSimulationRun); err != nil {
		return nil, err
	}
	if !in.Kind.Valid() {
		return nil, fmt.Errorf("%w: unknown scenario kind %q", ErrValidation, in.Kind)
	}
	params, err := parseParams(in.Kind, in.Params)
	if err != nil {
		return nil, err
	}
	baseline, warnings, err := s.readBaseline(ctx, p, RunInput{PipelineID: in.PipelineID, From: in.From, To: in.To})
	if err != nil {
		return nil, err
	}
	cmp, err := compute(in.Kind, baseline, params)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{Comparison: cmp, Baseline: baseline, Warnings: warnings}, nil
}

// RunScenario computes a saved scenario and persists the run.
func (s *Service) RunScenario(ctx context.Context, p *rbac.Principal, scenarioID uuid.UUID, in RunInput) (*Run, error) {
	if err := p.Require(rbac.PermSimulationRun); err != nil {
		return nil, err
	}
	sc, err := s.getScenario(ctx, p.OrganizationID, scenarioID)
	if err != nil {
		return nil, err
	}
	params, err := parseParams(sc.Kind, sc.Params)
	if err != nil {
		return nil, err
	}
	baseline, _, err := s.readBaseline(ctx, p, in)
	if err != nil {
		return nil, err
	}
	cmp, err := compute(sc.Kind, baseline, params)
	if err != nil {
		return nil, err
	}

	resultJSON, err := json.Marshal(cmp)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}
	baselineJSON, err := json.Marshal(baseline)
	if err != nil {
		return nil, fmt.Errorf("encode baseline: %w", err)
	}

	run := &Run{
		ID:               uuid.New(),
		OrganizationID:   p.OrganizationID,
		ScenarioID:       &sc.ID,
		Kind:             sc.Kind,
		AlgorithmVersion: cmp.AlgorithmVersion,
		Params:           sc.Params,
		Baseline:         baselineJSON,
		Result:           resultJSON,
		CreatedBy:        &p.UserID,
	}
	const q = `
		INSERT INTO simulation_runs
			(id, organization_id, scenario_id, created_by, kind, algorithm_version, params, baseline, result)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING created_at`
	if err := s.db.Q(ctx).QueryRow(ctx, q, run.ID, run.OrganizationID, run.ScenarioID,
		run.CreatedBy, run.Kind, run.AlgorithmVersion, run.Params, run.Baseline, run.Result).
		Scan(&run.CreatedAt); err != nil {
		return nil, fmt.Errorf("save run: %w", err)
	}

	if err := s.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID,
		ActorUserID:    &p.UserID,
		Action:         "simulation.run.created",
		EntityType:     "simulation_run",
		EntityID:       run.ID.String(),
		Metadata:       map[string]any{"scenario_id": sc.ID.String(), "kind": sc.Kind},
	}); err != nil {
		return nil, err
	}
	return run, nil
}

// ListRuns returns a scenario's run history, newest first.
func (s *Service) ListRuns(ctx context.Context, p *rbac.Principal, scenarioID uuid.UUID) ([]Run, error) {
	if err := p.Require(rbac.PermSimulationRead); err != nil {
		return nil, err
	}
	// Confirm the scenario is in this organization before returning its runs, so a
	// cross-org id cannot be used to enumerate another tenant's runs.
	if _, err := s.getScenario(ctx, p.OrganizationID, scenarioID); err != nil {
		return nil, err
	}
	const q = `
		SELECT id, organization_id, scenario_id, kind, algorithm_version, params, baseline,
		       result, created_by, created_at
		FROM simulation_runs
		WHERE organization_id = $1 AND scenario_id = $2
		ORDER BY created_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, scenarioID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.ScenarioID, &r.Kind, &r.AlgorithmVersion,
			&r.Params, &r.Baseline, &r.Result, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// readBaseline reads the CRM baseline and converts it into the analytics form.
// The warnings the reader produced (excluded currencies, missing pipeline) are
// returned so the caller can surface them.
func (s *Service) readBaseline(ctx context.Context, p *rbac.Principal, in RunInput) (*analytics.Baseline, []string, error) {
	data, err := s.crm.Baseline(ctx, p, crm.BaselineInput{
		PipelineID: in.PipelineID, From: in.From, To: in.To,
	})
	if err != nil {
		if errors.Is(err, rbac.ErrForbidden) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("read CRM baseline: %w", err)
	}
	b, err := baselineToAnalytics(data)
	if err != nil {
		return nil, nil, err
	}
	return b, data.Warnings, nil
}

// baselineToAnalytics converts the CRM reader's decimal strings into exact
// analytics values. A parse failure is returned rather than defaulted, because a
// silent zero would make a scenario quietly wrong.
func baselineToAnalytics(d *crm.BaselineData) (*analytics.Baseline, error) {
	out := &analytics.Baseline{
		Currency:         d.Currency,
		WonCount:         d.WonCount,
		LostCount:        d.LostCount,
		OpenCount:        d.OpenCount,
		ActiveCustomers:  d.ActiveCustomers,
		ChurnedCustomers: d.ChurnedCustomers,
		PeriodDays:       d.PeriodDays,
	}
	var err error
	if out.WonValue, err = analytics.ParseMoney(d.WonValue, d.Currency); err != nil {
		return nil, fmt.Errorf("parse won value %q: %w", d.WonValue, err)
	}
	if out.LostValue, err = analytics.ParseMoney(d.LostValue, d.Currency); err != nil {
		return nil, fmt.Errorf("parse lost value %q: %w", d.LostValue, err)
	}
	if out.OpenValue, err = analytics.ParseMoney(d.OpenValue, d.Currency); err != nil {
		return nil, fmt.Errorf("parse open value %q: %w", d.OpenValue, err)
	}
	if out.AvgDealValue, err = analytics.ParseMoney(d.AvgDealValue, d.Currency); err != nil {
		return nil, fmt.Errorf("parse average deal value %q: %w", d.AvgDealValue, err)
	}
	for _, st := range d.Stages {
		val, err := analytics.ParseMoney(st.OpenValue, d.Currency)
		if err != nil {
			return nil, fmt.Errorf("parse stage %q value %q: %w", st.StageName, st.OpenValue, err)
		}
		prob := analytics.NewRatioFromBasisPoints(int64(st.Probability) * 100) // percent -> factor
		out.Stages = append(out.Stages, analytics.StageSnapshot{
			StageID:     st.StageID.String(),
			StageName:   st.StageName,
			Position:    st.Position,
			OpenCount:   st.OpenCount,
			OpenValue:   val,
			Probability: prob,
		})
	}
	return out, nil
}

// compute dispatches to the pure core for the scenario kind.
func compute(kind analytics.Kind, b *analytics.Baseline, params any) (*analytics.Comparison, error) {
	switch kind {
	case analytics.KindFunnel:
		p, ok := params.(analytics.FunnelParams)
		if !ok {
			return nil, fmt.Errorf("%w: wrong parameter type for a funnel scenario", ErrValidation)
		}
		return analytics.RunFunnel(*b, p)
	case analytics.KindRevenue:
		p, ok := params.(analytics.RevenueParams)
		if !ok {
			return nil, fmt.Errorf("%w: wrong parameter type for a revenue scenario", ErrValidation)
		}
		return analytics.RunRevenue(*b, p)
	case analytics.KindRetention:
		p, ok := params.(analytics.RetentionParams)
		if !ok {
			return nil, fmt.Errorf("%w: wrong parameter type for a retention scenario", ErrValidation)
		}
		return analytics.RunRetention(*b, p)
	default:
		return nil, fmt.Errorf("%w: unknown scenario kind %q", ErrValidation, kind)
	}
}
