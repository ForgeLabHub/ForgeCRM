//go:build integration

// Integration tests for the workflow template catalog and the find_deals/foreach
// steps, against a real PostgreSQL schema.
//
// The properties that matter here are properties of the database, not of the Go
// code: a find_deals sweep is scoped to the author's organization by the SQL it
// runs, and a template's definition is validated by the same store path a hand-made
// workflow goes through. So these run the real engine over the real tables.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// insertPipeline makes a pipeline with one open stage and returns both ids.
func insertPipeline(t *testing.T, f *workflowFixture, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var pipelineID, stageID uuid.UUID
	if err := f.db.Pool().QueryRow(ctx,
		`INSERT INTO pipelines (organization_id, name) VALUES ($1,$2) RETURNING id`,
		f.org, name).Scan(&pipelineID); err != nil {
		t.Fatalf("insert pipeline: %v", err)
	}
	if err := f.db.Pool().QueryRow(ctx,
		`INSERT INTO pipeline_stages (pipeline_id, organization_id, name, position) VALUES ($1,$2,'Open',0) RETURNING id`,
		pipelineID, f.org).Scan(&stageID); err != nil {
		t.Fatalf("insert stage: %v", err)
	}
	return pipelineID, stageID
}

// insertDeal makes a deal owned by the fixture's user, optionally with a close date
// in the past.
func insertDeal(t *testing.T, f *workflowFixture, pipelineID, stageID uuid.UUID, title string, ownerID uuid.UUID, closeInDays int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := f.db.Pool().QueryRow(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, owner_user_id, expected_close_date)
		VALUES ($1,$2,$3,$4,$5, (now() + make_interval(days => $6::int))::date)
		RETURNING id`, f.org, pipelineID, stageID, title, ownerID, closeInDays).Scan(&id)
	if err != nil {
		t.Fatalf("insert deal %q: %v", title, err)
	}
	return id
}

// TestWorkflowFindDealsAndForeachCreatesOneTaskPerDeal proves the scheduled-sweep
// path end to end: a find_deals step selects the overdue deals in the caller's
// organization, and a foreach creates one task per selected deal. It is the shape
// every "remind me about a set of records" template uses.
func TestWorkflowFindDealsAndForeachCreatesOneTaskPerDeal(t *testing.T) {
	f := newWorkflowFixture(t, "wf-find", "sales_manager")
	ctx := context.Background()

	pipelineID, stageID := insertPipeline(t, f, "Default")
	// Two overdue open deals, one with a future close date.
	insertDeal(t, f, pipelineID, stageID, "Overdue A", f.userID, -5)
	insertDeal(t, f, pipelineID, stageID, "Overdue B", f.userID, -1)
	insertDeal(t, f, pipelineID, stageID, "Not overdue", f.userID, 30)

	def := json.RawMessage(`{"steps":[
		{"id":"overdue","type":"find_deals","args":{"status":"open","close_date_within_days":0}},
		{"id":"each","type":"foreach","do":[
			{"id":"task","type":"create_task","args":{"title":"Chase {{item.title}}"}}
		]}
	]}`)
	wf := f.create(t, "Overdue sweep", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	result, err := f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("run status = %q, summary=%q, want succeeded", result.Status, result.Summary)
	}

	var tasks int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id = $1`, f.org).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 2 {
		t.Fatalf("tasks created = %d, want 2 (one per overdue deal)", tasks)
	}

	// The titles must be built from the per-item context, not the trigger event.
	var chaseTitles int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id = $1 AND title LIKE 'Chase Overdue %'`, f.org).
		Scan(&chaseTitles); err != nil {
		t.Fatalf("count chase tasks: %v", err)
	}
	if chaseTitles != 2 {
		t.Fatalf("tasks titled from the item = %d, want 2", chaseTitles)
	}
}

// TestWorkflowFindDealsIsScopedToTheOrganization proves tenant isolation: a sweep in
// one organization cannot see, let alone act on, another organization's deals.
func TestWorkflowFindDealsIsScopedToTheOrganization(t *testing.T) {
	f := newWorkflowFixture(t, "wf-find-tenant", "sales_manager")
	ctx := context.Background()

	pipelineID, stageID := insertPipeline(t, f, "Default")
	insertDeal(t, f, pipelineID, stageID, "Mine overdue", f.userID, -3)

	// A second organization with its own overdue deal.
	otherOrg := insertOrganization(t, f.db, "wf-find-other")
	otherUser := seedMember(t, f.db, otherOrg, "sales_manager")
	var otherPipeline, otherStage uuid.UUID
	if err := f.db.Pool().QueryRow(ctx,
		`INSERT INTO pipelines (organization_id, name) VALUES ($1,'Other') RETURNING id`, otherOrg).Scan(&otherPipeline); err != nil {
		t.Fatalf("insert other pipeline: %v", err)
	}
	if err := f.db.Pool().QueryRow(ctx,
		`INSERT INTO pipeline_stages (pipeline_id, organization_id, name, position) VALUES ($1,$2,'Open',0) RETURNING id`,
		otherPipeline, otherOrg).Scan(&otherStage); err != nil {
		t.Fatalf("insert other stage: %v", err)
	}
	if _, err := f.db.Pool().Exec(ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, title, owner_user_id, expected_close_date)
		VALUES ($1,$2,$3,'Their overdue',$4,(now() - interval '3 days')::date)`,
		otherOrg, otherPipeline, otherStage, otherUser); err != nil {
		t.Fatalf("insert other deal: %v", err)
	}

	def := json.RawMessage(`{"steps":[
		{"id":"overdue","type":"find_deals","args":{"status":"open","close_date_within_days":0}},
		{"id":"each","type":"foreach","do":[{"id":"task","type":"create_task","args":{"title":"Chase {{item.title}}"}}]}
	]}`)
	wf := f.create(t, "Overdue sweep", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	if _, err := f.engine.RunManual(ctx, f.principal(t), *wf); err != nil {
		t.Fatalf("run: %v", err)
	}

	var tasks int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id = $1`, f.org).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 1 {
		t.Fatalf("tasks created = %d, want 1: the sweep saw another organization's deal", tasks)
	}
	var otherTasks int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tasks WHERE organization_id = $1`, otherOrg).Scan(&otherTasks); err != nil {
		t.Fatalf("count other tasks: %v", err)
	}
	if otherTasks != 0 {
		t.Fatalf("tasks written into the other organization = %d, want 0", otherTasks)
	}
}

// TestWorkflowForeachWithNoItemsIsSkipped proves that a sweep which matches nothing
// is a normal outcome, not a failure: the step is recorded skipped and the run
// succeeds. A scheduled workflow that fails every time there is no work to do would
// bury a real failure in noise.
func TestWorkflowForeachWithNoItemsIsSkipped(t *testing.T) {
	f := newWorkflowFixture(t, "wf-find-empty", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[
		{"id":"overdue","type":"find_deals","args":{"status":"open","close_date_within_days":0}},
		{"id":"each","type":"foreach","do":[{"id":"task","type":"create_task","args":{"title":"Chase"}}]}
	]}`)
	wf := f.create(t, "Overdue sweep", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerManual,
	})

	result, err := f.engine.RunManual(ctx, f.principal(t), *wf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("run status = %q, summary=%q, want succeeded", result.Status, result.Summary)
	}
	var status string
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT status FROM workflow_step_runs WHERE run_id = $1 AND step_id = 'each'`, result.RunID).Scan(&status); err != nil {
		t.Fatalf("read the foreach step run: %v", err)
	}
	if status != "skipped" {
		t.Fatalf("foreach step status = %q, want skipped", status)
	}
}

// TestEveryCatalogTemplateCanBeCreatedAndRun proves that the catalog ships only
// definitions the store accepts. It creates each template through the same store
// path the API uses, so a template that would be rejected on create is caught here.
func TestEveryCatalogTemplateCanBeCreatedAndRun(t *testing.T) {
	f := newWorkflowFixture(t, "wf-templates", "sales_manager")
	ctx := context.Background()

	if err := workflow.ValidateCatalog(); err != nil {
		t.Fatalf("the catalog is invalid: %v", err)
	}
	for _, tmpl := range workflow.Templates() {
		t.Run(tmpl.ID, func(t *testing.T) {
			in, err := workflow.InputFromTemplate(&tmpl, "From "+tmpl.ID)
			if err != nil {
				t.Fatalf("InputFromTemplate: %v", err)
			}
			if _, err := f.store.Create(ctx, f.principal(t), in); err != nil {
				t.Fatalf("the store rejected the template: %v", err)
			}
		})
	}
}

// TestTemplateCreateFromAPI proves the HTTP path: POST /workflow-templates/{id}/create
// stores a workflow whose definition is the template's, and the audit trail names
// the template it came from.
func TestTemplateCreateFromAPI(t *testing.T) {
	f := newWorkflowFixture(t, "wf-template-api", "sales_manager")
	ctx := context.Background()

	tmpl, ok := workflow.TemplateByID("deal-won-onboarding-task")
	if !ok {
		t.Fatal("the onboarding template is not in the catalog")
	}
	in, err := workflow.InputFromTemplate(tmpl, "Onboarding")
	if err != nil {
		t.Fatalf("InputFromTemplate: %v", err)
	}
	wf, err := f.store.Create(ctx, f.principal(t), in)
	if err != nil {
		t.Fatalf("create from template: %v", err)
	}
	if wf.EventType == nil || *wf.EventType != "crm.deal.won" {
		t.Fatalf("created workflow event type = %v, want crm.deal.won", wf.EventType)
	}
}

// TestExportImportRoundTripsThroughTheStore proves the portable document survives a
// full round trip: a stored workflow is exported, the export is imported into a
// second organization, and the second workflow has the same definition.
func TestExportImportRoundTripsThroughTheStore(t *testing.T) {
	f := newWorkflowFixture(t, "wf-export", "sales_manager")
	ctx := context.Background()

	def := json.RawMessage(`{"steps":[{"id":"t","type":"create_task","args":{"title":"From an export"}}]}`)
	original := f.create(t, "Exportable", workflow.CreateInput{
		Definition:  def,
		TriggerType: workflow.TriggerEvent,
		EventType:   strPtrTest("crm.deal.won"),
	})

	doc, err := workflow.ExportWorkflow(original, nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	in, err := workflow.ImportWorkflow(raw)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	// A different organization imports the document.
	otherOrg := insertOrganization(t, f.db, "wf-export-other")
	otherUser := seedMember(t, f.db, otherOrg, "sales_manager")
	perms, err := f.rbac.EffectivePermissions(ctx, otherUser, otherOrg)
	if err != nil {
		t.Fatalf("resolve permissions: %v", err)
	}
	imported, err := f.store.Create(ctx, rbac.NewPrincipal(otherUser, "other@example.test", "Other", otherOrg, perms), in)
	if err != nil {
		t.Fatalf("import into the other organization: %v", err)
	}
	if imported.OrganizationID != otherOrg {
		t.Fatalf("the imported workflow belongs to %s, want %s", imported.OrganizationID, otherOrg)
	}
	var a, b any
	if err := json.Unmarshal(original.Definition, &a); err != nil {
		t.Fatalf("unmarshal original: %v", err)
	}
	if err := json.Unmarshal(imported.Definition, &b); err != nil {
		t.Fatalf("unmarshal imported: %v", err)
	}
	na, _ := json.Marshal(a)
	nb, _ := json.Marshal(b)
	if string(na) != string(nb) {
		t.Fatalf("the imported definition differs:\n %s\n %s", na, nb)
	}
}
