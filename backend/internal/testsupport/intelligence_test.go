//go:build integration

// Integration tests for the Forecasting & Risk Lab. They exercise the real
// schema on a real PostgreSQL server: the forecast and risk tables, the
// permission seeds, the tenant scoping, the scan reconciliation and the
// threshold round trip against CRM rows written by hand.
//
// They are excluded from the default build because they need a database; run
// them with
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/forecast"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/risk"
)

// intelFixture bundles both services under test with the organization and the
// principal that own the data.
type intelFixture struct {
	db       *database.DB
	forecast *forecast.Service
	risk     *risk.Service
	rbac     *rbac.Service
	org      uuid.UUID
	user     uuid.UUID
	role     string
}

func newIntelFixture(t *testing.T, orgSlug, roleKey string) *intelFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	auditLog := audit.New(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	rbacSvc := rbac.NewService(db)

	org := insertOrganization(t, db, orgSlug)
	user := seedMember(t, db, org, roleKey)

	return &intelFixture{
		db:       db,
		forecast: forecast.NewService(db, crmSvc, auditLog),
		risk:     risk.NewService(db, crmSvc, auditLog),
		rbac:     rbacSvc,
		org:      org,
		user:     user,
		role:     roleKey,
	}
}

func (f *intelFixture) principal(t *testing.T) *rbac.Principal {
	t.Helper()
	perms, err := f.rbac.EffectivePermissions(context.Background(), f.user, f.org)
	if err != nil {
		t.Fatalf("resolve effective permissions: %v", err)
	}
	return rbac.NewPrincipal(f.user, "intel@example.test", "Analyst", f.org, perms)
}

// insertWonDeal creates a won deal closed in the given month offset (0 = this
// month), so a forecast series can be built with known values.
func insertWonDeal(t *testing.T, db *database.DB, orgID, pipelineID, stageID uuid.UUID, title, amount string, monthsAgo int) uuid.UUID {
	t.Helper()
	closedAt := time.Now().UTC().AddDate(0, -monthsAgo, 0)
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(), `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status, closed_at)
		VALUES ($1, $2, $3, $4, $5::numeric, 'USD', 'won', $6) RETURNING id`,
		orgID, pipelineID, stageID, title, amount, closedAt).Scan(&id); err != nil {
		t.Fatalf("insert won deal %q: %v", title, err)
	}
	return id
}

// TestForecastRunLifecycle proves the forecast path: a run is computed from the
// organization's revenue history, stored with its inputs, listed and fetched, and
// an insufficient series is refused rather than fabricated.
func TestForecastRunLifecycle(t *testing.T) {
	f := newIntelFixture(t, "forecast-lifecycle", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	if !p.Has(rbac.PermForecastRun) {
		t.Fatal("the analyst role should hold forecast.run after migration 0020")
	}

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Qualified", 20}, {"Won", 100}})
	// Twelve months of steady revenue so the series is long enough to score.
	for m := 11; m >= 0; m-- {
		insertWonDeal(t, f.db, f.org, pipelineID, stages[1], "Deal", "1000.00", m)
	}

	run, err := f.forecast.RunForecast(ctx, p, forecast.RunInput{
		Method: analytics.MethodMean, Horizon: 3,
	})
	if err != nil {
		t.Fatalf("run forecast: %v", err)
	}
	if run.ID == uuid.Nil || run.OrganizationID != f.org {
		t.Fatalf("run id/org not set: %+v", run)
	}
	if run.AlgorithmVersion != analytics.ForecastAlgorithmVersion {
		t.Fatalf("algorithm version = %q, want %q", run.AlgorithmVersion, analytics.ForecastAlgorithmVersion)
	}

	// The stored result must be a real forecast with three projected points, not an
	// empty document.
	var result analytics.Forecast
	if err := json.Unmarshal(run.Result, &result); err != nil {
		t.Fatalf("decode stored result: %v", err)
	}
	if len(result.Points) != 3 {
		t.Fatalf("projected points = %d, want 3", len(result.Points))
	}
	if result.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", result.Currency)
	}

	// The history is echoed back so the run is self-contained.
	var history []analytics.PeriodPoint
	if err := json.Unmarshal(run.History, &history); err != nil {
		t.Fatalf("decode stored history: %v", err)
	}
	if len(history) != 12 {
		t.Fatalf("history points = %d, want 12", len(history))
	}

	got, err := f.forecast.GetRun(ctx, p, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.ID != run.ID {
		t.Fatalf("get returned %s, want %s", got.ID, run.ID)
	}

	list, err := f.forecast.ListRuns(ctx, p)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("run list = %d, want 1", len(list))
	}
}

// TestForecastRefusesShortHistory proves a series too short to score is reported
// as an error rather than a fabricated number. The reader pads every month in the
// requested window, so the reachable short-series case is a window shorter than
// the minimum history.
func TestForecastRefusesShortHistory(t *testing.T) {
	f := newIntelFixture(t, "forecast-short", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Won", 100}})
	insertWonDeal(t, f.db, f.org, pipelineID, stages[0], "Deal", "500.00", 0)

	// Two months is below MinHistoryPeriods, so the forecast is refused.
	_, err := f.forecast.RunForecast(ctx, p, forecast.RunInput{
		Method: analytics.MethodTrend, Horizon: 3, Months: 2,
	})
	if !errors.Is(err, analytics.ErrForecastInsufficientData) {
		t.Fatalf("err = %v, want ErrForecastInsufficientData", err)
	}
}

// TestForecastTenantIsolation proves a run stored for one organization is not
// visible to a principal in another.
func TestForecastTenantIsolation(t *testing.T) {
	f := newIntelFixture(t, "forecast-iso-a", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Won", 100}})
	for m := 5; m >= 0; m-- {
		insertWonDeal(t, f.db, f.org, pipelineID, stages[0], "Deal", "100.00", m)
	}
	run, err := f.forecast.RunForecast(ctx, p, forecast.RunInput{
		Method: analytics.MethodMean, Horizon: 2,
	})
	if err != nil {
		t.Fatalf("run forecast: %v", err)
	}

	// A second organization with its own user cannot see the first org's run.
	otherOrg := insertOrganization(t, f.db, "forecast-iso-b")
	otherUser := seedMember(t, f.db, otherOrg, "analyst")
	perms, err := f.rbac.EffectivePermissions(ctx, otherUser, otherOrg)
	if err != nil {
		t.Fatalf("resolve other permissions: %v", err)
	}
	otherP := rbac.NewPrincipal(otherUser, "other@example.test", "Other", otherOrg, perms)

	if _, err := f.forecast.GetRun(ctx, otherP, run.ID); !errors.Is(err, forecast.ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v, want ErrNotFound", err)
	}
	otherList, err := f.forecast.ListRuns(ctx, otherP)
	if err != nil {
		t.Fatalf("other list: %v", err)
	}
	if len(otherList) != 0 {
		t.Fatalf("other org saw %d runs, want 0", len(otherList))
	}
}

// TestRiskScanReconciles proves the scan is idempotent: a second scan with no CRM
// change opens nothing, an operator's acknowledgement survives a re-scan, and a
// condition that no longer holds is closed by the scan.
func TestRiskScanReconciles(t *testing.T) {
	f := newIntelFixture(t, "risk-reconcile", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	if !p.Has(rbac.PermRiskRead) {
		t.Fatal("the analyst role should hold risk.read after migration 0020")
	}

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Qualified", 20}, {"Won", 100}})
	// A deal created well over the stalled threshold with no activity: it should be
	// flagged as stalled.
	var dealID uuid.UUID
	if err := f.db.Pool().QueryRow(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status, created_at)
		VALUES ($1, $2, $3, 'Stuck deal', 5000.00, 'USD', 'open', now() - interval '90 days')
		RETURNING id`, f.org, pipelineID, stages[0]).Scan(&dealID); err != nil {
		t.Fatalf("insert stalled deal: %v", err)
	}

	first, err := f.risk.Scan(ctx, p)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Opened == 0 {
		t.Fatalf("first scan opened nothing; detected=%d", first.Detected)
	}
	var stalled *risk.Signal
	for i := range first.Signals {
		if first.Signals[i].Type == analytics.RiskStalledDeal {
			stalled = &first.Signals[i]
			break
		}
	}
	if stalled == nil {
		t.Fatalf("no stalled_deal signal among %d", len(first.Signals))
	}

	// Acknowledge it, then re-scan. The acknowledgement must survive.
	if _, err := f.risk.SetStatus(ctx, p, stalled.ID, "acknowledged"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	second, err := f.risk.Scan(ctx, p)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Opened != 0 {
		t.Fatalf("second scan opened %d, want 0 (idempotent)", second.Opened)
	}
	var afterAck *risk.Signal
	for i := range second.Signals {
		if second.Signals[i].ID == stalled.ID {
			afterAck = &second.Signals[i]
			break
		}
	}
	if afterAck == nil || afterAck.Status != "acknowledged" {
		t.Fatalf("acknowledgement not preserved: %+v", afterAck)
	}

	// Make the condition stop holding: add a fresh activity, then re-scan. The
	// signal must be resolved by the scan.
	if _, err := f.db.Pool().Exec(ctx, `
		INSERT INTO activities (organization_id, type, subject, occurred_at, deal_id)
		VALUES ($1, 'note', 'recent touch', now(), $2)`, f.org, dealID); err != nil {
		t.Fatalf("insert activity: %v", err)
	}
	third, err := f.risk.Scan(ctx, p)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if third.Resolved == 0 {
		t.Fatalf("third scan resolved nothing; the stalled condition no longer holds")
	}
	got, err := f.risk.GetSignal(ctx, p, stalled.ID)
	if err != nil {
		t.Fatalf("get resolved signal: %v", err)
	}
	if got.Status != "resolved" || !got.ResolvedByScan {
		t.Fatalf("signal after resolution = %+v, want resolved by scan", got)
	}
}

// TestRiskThresholdsRoundTrip proves the organization's thresholds are stored and
// read back, and that an invalid set is refused.
func TestRiskThresholdsRoundTrip(t *testing.T) {
	f := newIntelFixture(t, "risk-thresholds", "data_steward")
	p := f.principal(t)
	ctx := context.Background()

	if !p.Has(rbac.PermRiskManage) {
		t.Fatal("the data_steward role should hold risk.manage after migration 0020")
	}

	// Defaults are returned before anything is stored.
	def, err := f.risk.Thresholds(ctx, p)
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if def.StalledDealDays != 30 {
		t.Fatalf("default stalled days = %d, want 30", def.StalledDealDays)
	}

	set := risk.Thresholds{StalledDealDays: 45, OverdueTaskDays: 10, AccountNoContactDays: 90,
		AmountSpikeRatio: 40000, ConversionDropRatio: 4000, StageConcentrationRatio: 8000}
	got, err := f.risk.SetThresholds(ctx, p, set)
	if err != nil {
		t.Fatalf("set thresholds: %v", err)
	}
	if got.StalledDealDays != 45 {
		t.Fatalf("stored stalled days = %d, want 45", got.StalledDealDays)
	}
	read, err := f.risk.Thresholds(ctx, p)
	if err != nil {
		t.Fatalf("re-read thresholds: %v", err)
	}
	if read != got {
		t.Fatalf("round trip = %+v, want %+v", read, got)
	}

	// A ratio above 100% is not a valid drop fraction.
	if _, err := f.risk.SetThresholds(ctx, p, risk.Thresholds{ConversionDropRatio: 20000}); !errors.Is(err, risk.ErrValidation) {
		t.Fatalf("invalid thresholds err = %v, want ErrValidation", err)
	}
}

// TestRiskScanRequiresPermission proves a role without risk.read is refused.
func TestRiskScanRequiresPermission(t *testing.T) {
	f := newIntelFixture(t, "risk-perm", "support_agent")
	p := f.principal(t)
	ctx := context.Background()

	if p.Has(rbac.PermRiskRead) {
		t.Skip("support_agent unexpectedly holds risk.read; pick a different role")
	}
	if _, err := f.risk.Scan(ctx, p); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("scan without risk.read err = %v, want ErrForbidden", err)
	}
}

// TestRiskTenantIsolation proves a signal for one organization is not visible to
// a principal in another.
func TestRiskTenantIsolation(t *testing.T) {
	f := newIntelFixture(t, "risk-iso-a", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Qualified", 20}})
	if _, err := f.db.Pool().Exec(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status, created_at)
		VALUES ($1, $2, $3, 'Stuck deal', 5000.00, 'USD', 'open', now() - interval '90 days')`,
		f.org, pipelineID, stages[0]); err != nil {
		t.Fatalf("insert stalled deal: %v", err)
	}
	res, err := f.risk.Scan(ctx, p)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(res.Signals) == 0 {
		t.Fatal("no signals to test isolation with")
	}

	otherOrg := insertOrganization(t, f.db, "risk-iso-b")
	otherUser := seedMember(t, f.db, otherOrg, "analyst")
	perms, err := f.rbac.EffectivePermissions(ctx, otherUser, otherOrg)
	if err != nil {
		t.Fatalf("resolve other permissions: %v", err)
	}
	otherP := rbac.NewPrincipal(otherUser, "other@example.test", "Other", otherOrg, perms)

	if _, err := f.risk.GetSignal(ctx, otherP, res.Signals[0].ID); !errors.Is(err, risk.ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v, want ErrNotFound", err)
	}
	list, err := f.risk.ListSignals(ctx, otherP, risk.ListFilter{})
	if err != nil {
		t.Fatalf("other list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("other org saw %d signals, want 0", len(list))
	}
}

// TestRiskStatusTransitionValidation proves only the permitted operator
// transitions are accepted.
func TestRiskStatusTransitionValidation(t *testing.T) {
	f := newIntelFixture(t, "risk-status", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Qualified", 20}})
	if _, err := f.db.Pool().Exec(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status, created_at)
		VALUES ($1, $2, $3, 'Stuck deal', 5000.00, 'USD', 'open', now() - interval '90 days')`,
		f.org, pipelineID, stages[0]); err != nil {
		t.Fatalf("insert stalled deal: %v", err)
	}
	res, err := f.risk.Scan(ctx, p)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(res.Signals) == 0 {
		t.Fatal("no signals")
	}
	id := res.Signals[0].ID

	if _, err := f.risk.SetStatus(ctx, p, id, "bogus"); !errors.Is(err, risk.ErrBadStatus) {
		t.Fatalf("bogus status err = %v, want ErrBadStatus", err)
	}
	if _, err := f.risk.SetStatus(ctx, p, id, "resolved"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// TestRiskScanDoesNotDuplicateAcrossRescan is the regression guard for the
// partial unique index: two scans in a row must leave exactly one live signal per
// rule and entity, not two.
func TestRiskScanDoesNotDuplicateAcrossRescan(t *testing.T) {
	f := newIntelFixture(t, "risk-nodup", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Qualified", 20}})
	if _, err := f.db.Pool().Exec(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status, created_at)
		VALUES ($1, $2, $3, 'Stuck deal', 5000.00, 'USD', 'open', now() - interval '90 days')`,
		f.org, pipelineID, stages[0]); err != nil {
		t.Fatalf("insert stalled deal: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := f.risk.Scan(ctx, p); err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
	}
	var live int
	if err := f.db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM risk_signals
		WHERE organization_id = $1 AND rule = 'stalled_deal' AND status IN ('open','acknowledged')`,
		f.org).Scan(&live); err != nil {
		t.Fatalf("count live signals: %v", err)
	}
	if live != 1 {
		t.Fatalf("live stalled signals = %d, want 1", live)
	}
}

// TestRiskScanExcludesArchivedDeals proves an archived deal is never flagged.
func TestRiskScanExcludesArchivedDeals(t *testing.T) {
	f := newIntelFixture(t, "risk-archived", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Qualified", 20}})
	if _, err := f.db.Pool().Exec(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status, created_at, archived_at)
		VALUES ($1, $2, $3, 'Archived deal', 5000.00, 'USD', 'open', now() - interval '90 days', now())`,
		f.org, pipelineID, stages[0]); err != nil {
		t.Fatalf("insert archived deal: %v", err)
	}
	res, err := f.risk.Scan(ctx, p)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, sig := range res.Signals {
		if strings.Contains(sig.EntityLabel, "Archived deal") {
			t.Fatalf("archived deal was flagged: %+v", sig)
		}
	}
}
