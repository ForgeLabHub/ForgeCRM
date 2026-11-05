//go:build integration

package testsupport

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// tenantRefFixture is a two-organization setup used to prove that a mutation
// never stores a reference to a record in another tenant.
type tenantRefFixture struct {
	db         *database.DB
	svc        *crm.Service
	orgA, orgB uuid.UUID
	userA      uuid.UUID
	userB      uuid.UUID
	acctB      uuid.UUID
	contactB   uuid.UUID
	dealB      uuid.UUID
	stageB     uuid.UUID
	teamB      uuid.UUID
	pA         *rbac.Principal
}

func newTenantRefFixture(t *testing.T) *tenantRefFixture {
	t.Helper()
	raw := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(strings.ToLower(parsed.Path), "_test") {
		t.Fatal("refusing to reset non-test database")
	}
	ctx := context.Background()
	db, err := database.Connect(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(raw, slog.Default()); err != nil {
		t.Fatal(err)
	}

	f := &tenantRefFixture{db: db, svc: crm.NewService(db, audit.New(db), outbox.NewStore(db))}
	must := func(q string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.Pool().QueryRow(ctx, q, args...).Scan(&id); err != nil {
			t.Fatalf("fixture query %q: %v", q, err)
		}
		return id
	}
	f.orgA = must(`INSERT INTO organizations(name,slug) VALUES('Org A','tref-org-a') RETURNING id`)
	f.orgB = must(`INSERT INTO organizations(name,slug) VALUES('Org B','tref-org-b') RETURNING id`)
	f.userA = must(`INSERT INTO users(email,display_name) VALUES('a@tref.test','Alice A') RETURNING id`)
	f.userB = must(`INSERT INTO users(email,display_name) VALUES('b@tref.test','Bob B') RETURNING id`)
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO memberships(user_id,organization_id) VALUES($1,$2),($3,$4)`,
		f.userA, f.orgA, f.userB, f.orgB); err != nil {
		t.Fatal(err)
	}
	// Org B's entities, which Org A must never be able to reference.
	f.teamB = must(`INSERT INTO teams(organization_id,name) VALUES($1,'Team B') RETURNING id`, f.orgB)
	f.acctB = must(`INSERT INTO accounts(organization_id,name,owner_user_id) VALUES($1,'Foreign Account',$2) RETURNING id`, f.orgB, f.userB)
	f.contactB = must(`INSERT INTO contacts(organization_id,first_name,last_name,owner_user_id) VALUES($1,'Foreign','Contact',$2) RETURNING id`, f.orgB, f.userB)
	pipeB := must(`INSERT INTO pipelines(organization_id,name,is_default) VALUES($1,'Sales B',true) RETURNING id`, f.orgB)
	stageB := must(`INSERT INTO pipeline_stages(pipeline_id,organization_id,name,position) VALUES($1,$2,'Qualified B',1) RETURNING id`, pipeB, f.orgB)
	f.stageB = stageB
	f.dealB = must(`INSERT INTO deals(organization_id,pipeline_id,stage_id,title,amount,currency,owner_user_id) VALUES($1,$2,$3,'Foreign Deal',100,'USD',$4) RETURNING id`, f.orgB, pipeB, stageB, f.userB)

	f.pA = rbac.NewPrincipal(f.userA, "a@tref.test", "Alice A", f.orgA, []rbac.Permission{
		rbac.PermTasksCreate, rbac.PermTasksRead, rbac.PermTasksUpdate, rbac.PermTasksAssign,
		rbac.PermImportsCreate, rbac.PermImportsExecute, rbac.PermImportsRead,
	})
	return f
}

// TestImportRefusesCrossTenantReferences is the async-mutation regression guard.
//
// The import worker writes CRM rows directly rather than through crm.Service, so
// it must apply the same tenant check the API does. A foreign key only proves the
// referenced row exists somewhere, not that it belongs to the importing
// organization, so without an explicit check an import in Org A can store a
// reference to an Org B account.
func TestImportRefusesCrossTenantReferences(t *testing.T) {
	f := newTenantRefFixture(t)
	ctx := context.Background()

	svc, err := importx.NewService(f.db, &config.Config{
		ImportTempDir:        t.TempDir(),
		ImportBatchSize:      500,
		ImportMaxUploadBytes: 256 << 20,
	}, audit.New(f.db), outbox.NewStore(f.db), f.svc, jobs.NewQueue(f.db, slog.Default()), t.TempDir())
	if err != nil {
		t.Fatalf("build import service: %v", err)
	}

	csv := "first_name,last_name,email,account_id\nEve,Importer,eve@tref.test," + f.acctB.String() + "\n"
	job, err := svc.Create(ctx, f.pA, importx.CreateRequest{
		Name: "cross tenant contacts", EntityType: "contact", SourceType: "file",
		OriginalFilename: "contacts.csv", Content: strings.NewReader(csv),
	})
	if err != nil {
		t.Fatalf("create import: %v", err)
	}
	header := true
	opts := importx.Options{Duplicates: importx.DuplicatesSkip, HasHeader: &header}
	if _, err := svc.Analyze(ctx, f.pA, job.ID, opts); err != nil {
		t.Fatalf("analyze import: %v", err)
	}
	mapping := []importx.MappingEntry{
		{SourceColumn: "first_name", TargetField: "first_name"},
		{SourceColumn: "last_name", TargetField: "last_name"},
		{SourceColumn: "email", TargetField: "email"},
		{SourceColumn: "account_id", TargetField: "account_id"},
	}
	queued, err := svc.Execute(ctx, f.pA, job.ID, mapping, opts)
	if err != nil {
		t.Fatalf("execute import: %v", err)
	}
	if err := svc.RunJob(ctx, jobs.NewQueue(f.db, slog.Default()), queued); err != nil {
		t.Fatalf("run import: %v", err)
	}

	var foreignRefs int
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM contacts WHERE organization_id=$1 AND account_id=$2`,
		f.orgA, f.acctB).Scan(&foreignRefs); err != nil {
		t.Fatal(err)
	}
	if foreignRefs != 0 {
		t.Fatalf("import stored %d contact(s) in Org A referencing an Org B account; "+
			"a cross-tenant reference must be rejected", foreignRefs)
	}
}

// TestCRMMutationsRefuseCrossTenantReferences proves the same invariant for the
// synchronous CRM paths: a task, ticket or deal must not accept an assignee,
// contact or ticket that belongs to another organization. These paths validate
// account_id today but not the other reference fields.
func TestCRMMutationsRefuseCrossTenantReferences(t *testing.T) {
	f := newTenantRefFixture(t)
	ctx := context.Background()

	if _, err := f.svc.CreateTask(ctx, f.pA, crm.TaskInput{
		Title: "cross tenant assignee", Status: "open", Priority: "normal",
		AssigneeUserID: &f.userB,
	}); err == nil {
		t.Fatal("CreateTask accepted an assignee from another organization")
	}

	if _, err := f.svc.CreateTask(ctx, f.pA, crm.TaskInput{
		Title: "cross tenant deal ref", Status: "open", Priority: "normal", DealID: &f.dealB,
	}); err == nil {
		t.Fatal("CreateTask accepted a deal from another organization")
	}

	if _, err := f.svc.CreateTask(ctx, f.pA, crm.TaskInput{
		Title: "cross tenant contact ref", Status: "open", Priority: "normal", ContactID: &f.contactB,
	}); err == nil {
		t.Fatal("CreateTask accepted a contact from another organization")
	}
}

// TestRiskViewDoesNotLeakAcrossTenants proves the defense-in-depth JOIN scoping:
// even if a cross-tenant reference already exists (written before the write-path
// check, or by a future bug), a read that joins through it must not surface the
// foreign row. The deals and pipeline_stages joins carry the organization in the
// ON clause so a foreign stage name cannot leak into a risk view.
func TestRiskViewDoesNotLeakAcrossTenants(t *testing.T) {
	f := newTenantRefFixture(t)
	ctx := context.Background()

	// Force a cross-tenant reference directly, bypassing the service, to model
	// pre-existing bad data.
	pipeA := mustUUID(t, f.db, `INSERT INTO pipelines(organization_id,name,is_default) VALUES($1,'Sales A',true) RETURNING id`, f.orgA)
	stageA := mustUUID(t, f.db, `INSERT INTO pipeline_stages(pipeline_id,organization_id,name,position) VALUES($1,$2,'Qualified A',1) RETURNING id`, pipeA, f.orgA)
	dealA := mustUUID(t, f.db, `INSERT INTO deals(organization_id,pipeline_id,stage_id,title,amount,currency,owner_user_id) VALUES($1,$2,$3,'Deal A',100,'USD',$4) RETURNING id`, f.orgA, pipeA, stageA, f.userA)
	// Point Org A's deal at Org B's stage, the reference the write check now forbids.
	if _, err := f.db.Pool().Exec(ctx, `UPDATE deals SET stage_id=$1 WHERE id=$2`, f.stageB, dealA); err != nil {
		t.Fatal(err)
	}

	pA := rbac.NewPrincipal(f.userA, "a@tref.test", "Alice A", f.orgA, []rbac.Permission{
		rbac.PermRiskRead, rbac.PermDealsRead, rbac.PermDealsArchive,
	})
	set, err := f.svc.ReadRiskObservations(ctx, pA)
	if err != nil {
		t.Fatalf("read risk observations: %v", err)
	}
	for _, d := range set.AllOpenDeals {
		if d.StageName == "Qualified B" {
			t.Fatal("risk view leaked a pipeline stage name from another organization")
		}
	}
}

func mustUUID(t *testing.T, db *database.DB, q string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(), q, args...).Scan(&id); err != nil {
		t.Fatalf("fixture query %q: %v", q, err)
	}
	return id
}
