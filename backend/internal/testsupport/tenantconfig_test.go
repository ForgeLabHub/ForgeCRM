//go:build integration

// Integration tests for the organization configuration's consumers.
//
// The configuration package has its own tests for storing and reading a value.
// What those cannot show is that anything acts on the value: a setting the
// platform stores and no code reads is a control that does nothing, which is the
// exact failure this feature exists to avoid. These tests therefore write a
// setting through the real service and then create a real deal, task or workflow,
// and assert on what the domain actually persisted.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/tenantconfig"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// tenantDefaultsFixture wires the configuration service into the domain services
// the same way the wiring package does, so the test exercises the real attachment
// rather than a hand-built one.
type tenantDefaultsFixture struct {
	db     *database.DB
	crm    *crm.Service
	config *tenantconfig.Service
	rbac   *rbac.Service
	org    uuid.UUID
	userID uuid.UUID
}

func newTenantDefaultsFixture(t *testing.T, slug, roleKey string) *tenantDefaultsFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	auditLog := audit.New(db)
	configSvc := tenantconfig.NewService(db, auditLog)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	crmSvc.SetOrgDefaults(configSvc)

	org := insertOrganization(t, db, slug)
	userID := seedMember(t, db, org, roleKey)

	return &tenantDefaultsFixture{
		db:     db,
		crm:    crmSvc,
		config: configSvc,
		rbac:   rbac.NewService(db),
		org:    org,
		userID: userID,
	}
}

func (f *tenantDefaultsFixture) principal(t *testing.T) *rbac.Principal {
	t.Helper()
	perms, err := f.rbac.EffectivePermissions(context.Background(), f.userID, f.org)
	if err != nil {
		t.Fatalf("resolve effective permissions: %v", err)
	}
	return rbac.NewPrincipal(f.userID, "author@example.test", "Author", f.org, perms)
}

func (f *tenantDefaultsFixture) seedPipeline(t *testing.T, name string) (uuid.UUID, uuid.UUID) {
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

// TestTenantDefaultCurrencyReachesANewDeal is the vertical slice for the currency:
// a setting written through the configuration service is the currency a deal is
// actually stored with.
func TestTenantDefaultCurrencyReachesANewDeal(t *testing.T) {
	f := newTenantDefaultsFixture(t, "defaults-currency", "sales_manager")
	ctx := context.Background()

	if _, err := f.config.Set(ctx, f.principal(t), crm.SettingDefaultCurrency, "EUR"); err != nil {
		t.Fatalf("set the default currency: %v", err)
	}

	pipelineID, stageID := f.seedPipeline(t, "Sales")

	deal, err := f.crm.CreateDeal(ctx, f.principal(t), crm.DealInput{
		Title:      "A deal with no currency named",
		PipelineID: pipelineID,
		StageID:    stageID,
		Amount:     100,
	})
	if err != nil {
		t.Fatalf("create deal: %v", err)
	}
	if deal.Currency != "EUR" {
		t.Fatalf("deal currency = %q, want EUR (the organization default)", deal.Currency)
	}

	// A caller that named a currency keeps it, so the default is a default and not an
	// override.
	named, err := f.crm.CreateDeal(ctx, f.principal(t), crm.DealInput{
		Title:      "A deal in a named currency",
		PipelineID: pipelineID,
		StageID:    stageID,
		Amount:     100,
		Currency:   "JPY",
	})
	if err != nil {
		t.Fatalf("create deal with a named currency: %v", err)
	}
	if named.Currency != "JPY" {
		t.Fatalf("deal currency = %q, want JPY (the caller's value)", named.Currency)
	}
}

// TestTenantDefaultPriorityReachesANewTask is the same slice for the task priority.
func TestTenantDefaultPriorityReachesANewTask(t *testing.T) {
	f := newTenantDefaultsFixture(t, "defaults-priority", "sales_manager")
	ctx := context.Background()

	if _, err := f.config.Set(ctx, f.principal(t), crm.SettingDefaultTaskPriority, "high"); err != nil {
		t.Fatalf("set the default priority: %v", err)
	}

	task, err := f.crm.CreateTask(ctx, f.principal(t), crm.TaskInput{Title: "A task with no priority named"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Priority != "high" {
		t.Fatalf("task priority = %q, want high (the organization default)", task.Priority)
	}

	named, err := f.crm.CreateTask(ctx, f.principal(t), crm.TaskInput{Title: "A task with a named priority", Priority: "low"})
	if err != nil {
		t.Fatalf("create task with a named priority: %v", err)
	}
	if named.Priority != "low" {
		t.Fatalf("task priority = %q, want low (the caller's value)", named.Priority)
	}
}

// TestTenantDefaultTimezoneReachesAScheduledWorkflow is the slice for the workflow
// timezone: the stored row carries the zone the run was actually scheduled in, so a
// schedule is not silently interpreted in UTC when the tenant chose otherwise.
func TestTenantDefaultTimezoneReachesAScheduledWorkflow(t *testing.T) {
	f := newTenantDefaultsFixture(t, "defaults-timezone", "sales_manager")
	ctx := context.Background()

	if _, err := f.config.Set(ctx, f.principal(t), workflow.SettingDefaultTimezone, "Europe/Berlin"); err != nil {
		t.Fatalf("set the default timezone: %v", err)
	}

	engine := workflow.NewEngine(workflow.Config{
		DB:       f.db,
		CRM:      f.crm,
		RBAC:     f.rbac,
		Audit:    audit.New(f.db),
		Outbox:   outbox.NewStore(f.db),
		Log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Defaults: f.config,
	})

	cron := "0 3 * * *"
	wf, err := engine.Store().Create(ctx, f.principal(t), workflow.CreateInput{
		Name:         "Nightly sweep",
		Definition:   json.RawMessage(`{"steps":[{"id":"t1","type":"create_task","args":{"title":"Nightly sweep"}}]}`),
		TriggerType:  workflow.TriggerSchedule,
		ScheduleCron: &cron,
	})
	if err != nil {
		t.Fatalf("create scheduled workflow: %v", err)
	}
	if wf.ScheduleTimezone == nil || *wf.ScheduleTimezone != "Europe/Berlin" {
		got := "<nil>"
		if wf.ScheduleTimezone != nil {
			got = *wf.ScheduleTimezone
		}
		t.Fatalf("schedule timezone = %s, want Europe/Berlin (the organization default)", got)
	}
}
