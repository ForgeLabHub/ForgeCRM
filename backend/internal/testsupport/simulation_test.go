//go:build integration

// Integration tests for the Simulation Lab. They exercise the real schema on a
// real PostgreSQL server: the scenario tables, the permission seeds, the tenant
// scoping and the compute path against CRM rows written by hand.
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

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/analytics"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrations"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/simulate"
)

// simFixture bundles the service under test with the organization and the
// principal that own the data.
type simFixture struct {
	db   *database.DB
	svc  *simulate.Service
	rbac *rbac.Service
	org  uuid.UUID
	user uuid.UUID
	role string
}

func newSimFixture(t *testing.T, orgSlug, roleKey string) *simFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	auditLog := audit.New(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	rbacSvc := rbac.NewService(db)
	svc := simulate.NewService(db, crmSvc, auditLog)

	org := insertOrganization(t, db, orgSlug)
	user := seedMember(t, db, org, roleKey)

	return &simFixture{db: db, svc: svc, rbac: rbacSvc, org: org, user: user, role: roleKey}
}

// principal resolves the user's effective permissions from the database, exactly
// as the API does for a request.
func (f *simFixture) principal(t *testing.T) *rbac.Principal {
	t.Helper()
	perms, err := f.rbac.EffectivePermissions(context.Background(), f.user, f.org)
	if err != nil {
		t.Fatalf("resolve effective permissions: %v", err)
	}
	return rbac.NewPrincipal(f.user, "analyst@example.test", "Analyst", f.org, perms)
}

// insertPipeline creates a pipeline with the given stages and marks it default.
// It returns the pipeline id and the stage ids in order.
func insertSimPipeline(t *testing.T, db *database.DB, orgID uuid.UUID, name string, stages []struct {
	Name        string
	Probability int
}) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var pipelineID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO pipelines (organization_id, name, is_default, currency)
		VALUES ($1, $2, true, 'USD') RETURNING id`, orgID, name).Scan(&pipelineID); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	ids := make([]uuid.UUID, 0, len(stages))
	for i, s := range stages {
		var id uuid.UUID
		if err := db.Pool().QueryRow(ctx, `
			INSERT INTO pipeline_stages (pipeline_id, organization_id, name, position, probability)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			pipelineID, orgID, s.Name, i, s.Probability).Scan(&id); err != nil {
			t.Fatalf("insert stage %q: %v", s.Name, err)
		}
		ids = append(ids, id)
	}
	return pipelineID, ids
}

// insertDeal creates a deal in the given stage. The amount is a decimal string so
// the test can use exact values the reader must reproduce.
func insertSimDeal(t *testing.T, db *database.DB, orgID, pipelineID, stageID uuid.UUID, title, amount, status string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(), `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, currency, status)
		VALUES ($1, $2, $3, $4, $5::numeric, 'USD', $6) RETURNING id`,
		orgID, pipelineID, stageID, title, amount, status).Scan(&id); err != nil {
		t.Fatalf("insert deal %q: %v", title, err)
	}
	return id
}

// TestSimulationScenarioLifecycle proves the CRUD path: a scenario is saved, listed,
// fetched, and archived, and an archived scenario disappears from the list.
func TestSimulationScenarioLifecycle(t *testing.T) {
	f := newSimFixture(t, "sim-lifecycle", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	if !p.Has(rbac.PermSimulationManage) {
		t.Fatal("the analyst role should hold simulation.manage after migration 0019")
	}

	sc, err := f.svc.CreateScenario(ctx, p, simulate.CreateScenarioInput{
		Name:   "Q3 best case",
		Kind:   analytics.KindRevenue,
		Params: json.RawMessage(`{"win_rate": 10}`),
	})
	if err != nil {
		t.Fatalf("create scenario: %v", err)
	}
	if sc.ID == uuid.Nil || sc.OrganizationID != f.org {
		t.Fatalf("scenario id/org not set: %+v", sc)
	}
	if sc.AlgorithmVersion != analytics.AlgorithmVersion {
		t.Fatalf("algorithm version = %q, want %q", sc.AlgorithmVersion, analytics.AlgorithmVersion)
	}

	// The name is unique per organization among live scenarios.
	if _, err := f.svc.CreateScenario(ctx, p, simulate.CreateScenarioInput{
		Name: "Q3 best case", Kind: analytics.KindRevenue,
	}); !errors.Is(err, simulate.ErrNameTaken) {
		t.Fatalf("duplicate name error = %v, want ErrNameTaken", err)
	}

	list, err := f.svc.ListScenarios(ctx, p)
	if err != nil {
		t.Fatalf("list scenarios: %v", err)
	}
	if len(list) != 1 || list[0].ID != sc.ID {
		t.Fatalf("list = %+v, want the one scenario", list)
	}

	if err := f.svc.ArchiveScenario(ctx, p, sc.ID); err != nil {
		t.Fatalf("archive scenario: %v", err)
	}
	list, err = f.svc.ListScenarios(ctx, p)
	if err != nil {
		t.Fatalf("list after archive: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("archived scenario still listed: %+v", list)
	}
	if _, err := f.svc.GetScenario(ctx, p, sc.ID); !errors.Is(err, simulate.ErrNotFound) {
		t.Fatalf("get archived scenario error = %v, want ErrNotFound", err)
	}
}

// TestSimulationScenarioValidation checks that a scenario whose parameters cannot
// be parsed is refused on save, so a stored scenario never fails at run time.
func TestSimulationScenarioValidation(t *testing.T) {
	f := newSimFixture(t, "sim-validation", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	cases := []struct {
		name string
		in   simulate.CreateScenarioInput
	}{
		{"empty name", simulate.CreateScenarioInput{Name: "  ", Kind: analytics.KindRevenue}},
		{"unknown kind", simulate.CreateScenarioInput{Name: "x", Kind: analytics.Kind("forecast")}},
		{"unknown parameter", simulate.CreateScenarioInput{
			Name: "x", Kind: analytics.KindRevenue, Params: json.RawMessage(`{"nope": 1}`)}},
		{"non-numeric parameter", simulate.CreateScenarioInput{
			Name: "x", Kind: analytics.KindRevenue, Params: json.RawMessage(`{"win_rate": "abc"}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.svc.CreateScenario(ctx, p, tc.in); !errors.Is(err, simulate.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}

// TestSimulationRunAgainstRealData is the end-to-end check: deals are written into
// a pipeline, a funnel scenario is saved and run, and the stored result is compared
// against a hand-computed expectation. This is the test that proves the reader and
// the arithmetic agree with the database.
func TestSimulationRunAgainstRealData(t *testing.T) {
	f := newSimFixture(t, "sim-run", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{
		{"Qualified", 20},
		{"Proposal", 50},
	})

	// Two open deals in Qualified at 1000 each, one open deal in Proposal at 2000.
	// Expected won value baseline = 2×1000×0.20 + 1×2000×0.50 = 400 + 1000 = 1400.
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "Q1", "1000.00", "open")
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "Q2", "1000.00", "open")
	insertSimDeal(t, f.db, f.org, pipelineID, stages[1], "P1", "2000.00", "open")

	sc, err := f.svc.CreateScenario(ctx, p, simulate.CreateScenarioInput{
		Name: "Boost proposal", Kind: analytics.KindFunnel,
		// Scale the Proposal stage probability by +50% (50% -> 75%).
		Params: json.RawMessage(`{"stage_conversions": {"` + stages[1].String() + `": 50}}`),
	})
	if err != nil {
		t.Fatalf("create scenario: %v", err)
	}

	run, err := f.svc.RunScenario(ctx, p, sc.ID, simulate.RunInput{})
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if run.AlgorithmVersion != analytics.AlgorithmVersion {
		t.Fatalf("run algorithm version = %q", run.AlgorithmVersion)
	}

	var cmp analytics.Comparison
	if err := json.Unmarshal(run.Result, &cmp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	got := metricValue(t, cmp, "expected_won_value")
	// Baseline 1400.00 -> 140000 minor units. Variant: 2×1000×0.20 + 1×2000×0.75 = 400 + 1500 = 1900.00.
	if got.Baseline != 140_000 {
		t.Fatalf("baseline expected_won_value = %d, want 140000", got.Baseline)
	}
	if got.Variant != 190_000 {
		t.Fatalf("variant expected_won_value = %d, want 190000", got.Variant)
	}
	if got.Delta != 50_000 {
		t.Fatalf("delta = %d, want 50000", got.Delta)
	}

	// The frozen baseline is stored, so the run can be explained later.
	var frozen analytics.Baseline
	if err := json.Unmarshal(run.Baseline, &frozen); err != nil {
		t.Fatalf("decode frozen baseline: %v", err)
	}
	if len(frozen.Stages) != 2 {
		t.Fatalf("frozen baseline has %d stages, want 2", len(frozen.Stages))
	}

	runs, err := f.svc.ListRuns(ctx, p, sc.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("run history = %+v, want the one run", runs)
	}
}

// TestSimulationPreviewPersistsNothing checks that a preview returns a comparison
// and a baseline but writes no rows.
func TestSimulationPreviewPersistsNothing(t *testing.T) {
	f := newSimFixture(t, "sim-preview", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Won-ish", 100}})
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "D1", "500.00", "won")

	res, err := f.svc.Preview(ctx, p, simulate.PreviewInput{
		Kind:   analytics.KindRevenue,
		Params: json.RawMessage(`{"win_rate": 20}`),
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if res.Comparison == nil || res.Baseline == nil {
		t.Fatalf("preview returned nil comparison or baseline")
	}

	var runs int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM simulation_runs`).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runs != 0 {
		t.Fatalf("preview wrote %d runs, want 0", runs)
	}
}

// TestSimulationNeverWritesCRMRows proves the boundary the plan promises: the lab
// reads CRM data and writes only to its own tables. A run must not change any
// deal, task or activity, and it must not enqueue a job or emit an event.
func TestSimulationNeverWritesCRMRows(t *testing.T) {
	f := newSimFixture(t, "sim-readonly", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Stage", 30}})
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "D1", "1000.00", "open")
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "D2", "2000.00", "won")

	counts := func() (deals, tasks, activities, jobs, outbox int) {
		t.Helper()
		for _, q := range []struct {
			query string
			dst   *int
		}{
			{`SELECT count(*) FROM deals`, &deals},
			{`SELECT count(*) FROM tasks`, &tasks},
			{`SELECT count(*) FROM activities`, &activities},
			{`SELECT count(*) FROM jobs`, &jobs},
			{`SELECT count(*) FROM outbox_events`, &outbox},
		} {
			if err := f.db.Pool().QueryRow(ctx, q.query).Scan(q.dst); err != nil {
				t.Fatalf("count (%s): %v", q.query, err)
			}
		}
		return
	}

	beforeDeals, beforeTasks, beforeActivities, beforeJobs, beforeOutbox := counts()

	sc, err := f.svc.CreateScenario(ctx, p, simulate.CreateScenarioInput{
		Name: "Read only", Kind: analytics.KindRevenue,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.RunScenario(ctx, p, sc.ID, simulate.RunInput{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := f.svc.Preview(ctx, p, simulate.PreviewInput{Kind: analytics.KindRevenue}); err != nil {
		t.Fatalf("preview: %v", err)
	}

	afterDeals, afterTasks, afterActivities, afterJobs, afterOutbox := counts()
	if beforeDeals != afterDeals || beforeTasks != afterTasks || beforeActivities != afterActivities ||
		beforeJobs != afterJobs || beforeOutbox != afterOutbox {
		t.Fatalf("CRM or queue state changed across a run: deals %d->%d tasks %d->%d activities %d->%d jobs %d->%d outbox %d->%d",
			beforeDeals, afterDeals, beforeTasks, afterTasks, beforeActivities, afterActivities,
			beforeJobs, afterJobs, beforeOutbox, afterOutbox)
	}
}

// TestSimulationInsufficientDataIsReported checks that a scenario with too little
// CRM history returns ErrInsufficientData rather than a fabricated zero.
func TestSimulationInsufficientDataIsReported(t *testing.T) {
	f := newSimFixture(t, "sim-nodata", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	// A retention scenario with no customers at all.
	if _, err := f.svc.Preview(ctx, p, simulate.PreviewInput{
		Kind: analytics.KindRetention,
	}); !errors.Is(err, analytics.ErrInsufficientData) {
		t.Fatalf("preview error = %v, want ErrInsufficientData", err)
	}

	// A revenue scenario with a pipeline but no won or lost deals.
	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Open", 50}})
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "Open deal", "100.00", "open")
	if _, err := f.svc.Preview(ctx, p, simulate.PreviewInput{
		Kind: analytics.KindRevenue,
	}); !errors.Is(err, analytics.ErrInsufficientData) {
		t.Fatalf("revenue preview error = %v, want ErrInsufficientData", err)
	}
}

// TestSimulationTenantIsolation proves a scenario saved in one organization is
// invisible to another, and that a cross-organization id cannot be used to read
// or run it. This is the check that the org filter is on the query and not left to
// the caller.
func TestSimulationTenantIsolation(t *testing.T) {
	f := newSimFixture(t, "sim-tenant-a", "analyst")
	pA := f.principal(t)
	ctx := context.Background()

	scA, err := f.svc.CreateScenario(ctx, pA, simulate.CreateScenarioInput{
		Name: "A's scenario", Kind: analytics.KindRevenue,
	})
	if err != nil {
		t.Fatalf("create scenario in A: %v", err)
	}

	// A second organization, same role.
	orgB := insertOrganization(t, f.db, "sim-tenant-b")
	userB := seedMember(t, f.db, orgB, "analyst")
	permsB, err := f.rbac.EffectivePermissions(ctx, userB, orgB)
	if err != nil {
		t.Fatalf("resolve B permissions: %v", err)
	}
	pB := rbac.NewPrincipal(userB, "b@example.test", "B", orgB, permsB)

	listB, err := f.svc.ListScenarios(ctx, pB)
	if err != nil {
		t.Fatalf("list in B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("B sees %d scenarios from A", len(listB))
	}

	if _, err := f.svc.GetScenario(ctx, pB, scA.ID); !errors.Is(err, simulate.ErrNotFound) {
		t.Fatalf("cross-org get error = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.RunScenario(ctx, pB, scA.ID, simulate.RunInput{}); !errors.Is(err, simulate.ErrNotFound) {
		t.Fatalf("cross-org run error = %v, want ErrNotFound", err)
	}
	if err := f.svc.ArchiveScenario(ctx, pB, scA.ID); !errors.Is(err, simulate.ErrNotFound) {
		t.Fatalf("cross-org archive error = %v, want ErrNotFound", err)
	}
}

// TestSimulationPermissionIsEnforced proves the service checks the permission
// itself rather than trusting the route. A principal without simulation.manage is
// refused even though it reaches the service directly.
func TestSimulationPermissionIsEnforced(t *testing.T) {
	f := newSimFixture(t, "sim-perm", "sales_representative")
	p := f.principal(t)
	ctx := context.Background()

	// A sales representative holds no simulation permission in migration 0019.
	if p.Has(rbac.PermSimulationManage) {
		t.Fatal("sales_representative should not hold simulation.manage")
	}
	if _, err := f.svc.CreateScenario(ctx, p, simulate.CreateScenarioInput{
		Name: "nope", Kind: analytics.KindRevenue,
	}); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("create error = %v, want ErrForbidden", err)
	}
	if _, err := f.svc.ListScenarios(ctx, p); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("list error = %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Preview(ctx, p, simulate.PreviewInput{
		Kind: analytics.KindRevenue,
	}); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("preview error = %v, want ErrForbidden", err)
	}
}

// TestSimulationRunIsReproducible proves that two runs of the same scenario
// against the same data produce the same result, which is what the stored
// algorithm version and the frozen baseline promise.
func TestSimulationRunIsReproducible(t *testing.T) {
	f := newSimFixture(t, "sim-repro", "analyst")
	p := f.principal(t)
	ctx := context.Background()

	pipelineID, stages := insertSimPipeline(t, f.db, f.org, "Sales", []struct {
		Name        string
		Probability int
	}{{"Stage", 40}})
	insertSimDeal(t, f.db, f.org, pipelineID, stages[0], "D", "1234.56", "open")

	sc, err := f.svc.CreateScenario(ctx, p, simulate.CreateScenarioInput{
		Name: "Reproducible", Kind: analytics.KindFunnel,
		Params: json.RawMessage(`{"lead_volume": 10}`),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := f.svc.RunScenario(ctx, p, sc.ID, simulate.RunInput{})
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := f.svc.RunScenario(ctx, p, sc.ID, simulate.RunInput{})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if string(first.Result) != string(second.Result) {
		t.Fatalf("results differ:\n%s\n%s", first.Result, second.Result)
	}
	if first.ID == second.ID {
		t.Fatal("two runs share an id")
	}
}

// TestSimulationMigrationDownRemovesTheTables checks the down migration, so a
// rollback does not leave the schema in a state the up migration cannot rebuild.
func TestSimulationMigrationDownRemovesTheTables(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()

	// The tables and the seeded permissions exist after the up migration.
	var exists bool
	if err := db.Pool().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'simulation_scenarios')`).
		Scan(&exists); err != nil {
		t.Fatalf("check table: %v", err)
	}
	if !exists {
		t.Fatal("simulation_scenarios missing after migration")
	}

	body, err := migrations.FS.ReadFile("0019_simulation_lab.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, string(body)); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}

	if err := db.Pool().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'simulation_scenarios')`).
		Scan(&exists); err != nil {
		t.Fatalf("recheck table: %v", err)
	}
	if exists {
		t.Fatal("simulation_scenarios still present after the down migration")
	}

	// The seeded permissions are removed too, so the catalog and the seed stay in
	// step after a rollback.
	var perms int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM permissions WHERE key LIKE 'simulation.%'`).Scan(&perms); err != nil {
		t.Fatalf("count permissions: %v", err)
	}
	if perms != 0 {
		t.Fatalf("%d simulation permissions survived the rollback", perms)
	}
}

// metricValue returns the named metric, failing the test when it is absent.
func metricValue(t *testing.T, cmp analytics.Comparison, key string) analytics.Metric {
	t.Helper()
	for _, m := range cmp.Metrics {
		if m.Key == key {
			return m
		}
	}
	keys := make([]string, 0, len(cmp.Metrics))
	for _, m := range cmp.Metrics {
		keys = append(keys, m.Key)
	}
	t.Fatalf("metric %q not found; have %s", key, strings.Join(keys, ", "))
	return analytics.Metric{}
}
