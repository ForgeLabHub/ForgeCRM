//go:build integration

package testsupport

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// objectAuthzFixture is a two-organization, two-user setup. bob is a read-only
// user of org A who belongs to a team; alice owns the records. carol owns
// records in org B. The list endpoints already hide alice's records from bob,
// so any single-record read or search that still returns them is an object-level
// authorization gap, not a permission the user is meant to have.
type objectAuthzFixture struct {
	db           *database.DB
	svc          *crm.Service
	orgA, orgB   uuid.UUID
	alice, bob   uuid.UUID
	aliceOnly    map[string]uuid.UUID // entity type -> record owned by alice in org A
	crossTenant  map[string]uuid.UUID // entity type -> record in org B
	readOnlyBob  *rbac.Principal
	managerAlice *rbac.Principal
}

func newObjectAuthzFixture(t *testing.T) *objectAuthzFixture {
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

	f := &objectAuthzFixture{db: db, svc: crm.NewService(db, audit.New(db), outbox.NewStore(db))}
	must := func(q string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.Pool().QueryRow(ctx, q, args...).Scan(&id); err != nil {
			t.Fatalf("fixture query %q: %v", q, err)
		}
		return id
	}
	f.orgA = must(`INSERT INTO organizations(name,slug) VALUES('Org A','obj-org-a') RETURNING id`)
	f.orgB = must(`INSERT INTO organizations(name,slug) VALUES('Org B','obj-org-b') RETURNING id`)
	f.alice = must(`INSERT INTO users(email,display_name) VALUES('alice@obj.test','Alice') RETURNING id`)
	f.bob = must(`INSERT INTO users(email,display_name) VALUES('bob@obj.test','Bob') RETURNING id`)
	carol := must(`INSERT INTO users(email,display_name) VALUES('carol@obj.test','Carol') RETURNING id`)
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO memberships(user_id,organization_id) VALUES($1,$2),($3,$2),($4,$5)`,
		f.alice, f.orgA, f.bob, carol, f.orgB); err != nil {
		t.Fatal(err)
	}
	// bob is in a team, so the list endpoints apply their record-level rule.
	team := must(`INSERT INTO teams(organization_id,name) VALUES($1,'Team A') RETURNING id`, f.orgA)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, f.bob); err != nil {
		t.Fatal(err)
	}

	pipeline := must(`INSERT INTO pipelines(organization_id,name,is_default) VALUES($1,'Sales',true) RETURNING id`, f.orgA)
	stage := must(`INSERT INTO pipeline_stages(pipeline_id,organization_id,name,position) VALUES($1,$2,'Qualified',1) RETURNING id`, pipeline, f.orgA)

	f.aliceOnly = map[string]uuid.UUID{
		"account": must(`INSERT INTO accounts(organization_id,name,owner_user_id) VALUES($1,'Zebra Account',$2) RETURNING id`, f.orgA, f.alice),
		"contact": must(`INSERT INTO contacts(organization_id,first_name,last_name,owner_user_id) VALUES($1,'Zebra','Contact',$2) RETURNING id`, f.orgA, f.alice),
		"lead":    must(`INSERT INTO leads(organization_id,title,owner_user_id) VALUES($1,'Zebra Lead',$2) RETURNING id`, f.orgA, f.alice),
		"deal":    must(`INSERT INTO deals(organization_id,pipeline_id,stage_id,title,amount,currency,owner_user_id) VALUES($1,$2,$3,'Zebra Deal',100,'USD',$4) RETURNING id`, f.orgA, pipeline, stage, f.alice),
		"task":    must(`INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,created_by) VALUES($1,'Zebra Task','open','normal',$2,$2) RETURNING id`, f.orgA, f.alice),
		"ticket":  must(`INSERT INTO tickets(organization_id,subject,status,priority,assignee_user_id) VALUES($1,'Zebra Ticket','new','normal',$2) RETURNING id`, f.orgA, f.alice),
	}
	f.crossTenant = map[string]uuid.UUID{
		"account": must(`INSERT INTO accounts(organization_id,name,owner_user_id) VALUES($1,'Other Account',$2) RETURNING id`, f.orgB, carol),
		"contact": must(`INSERT INTO contacts(organization_id,first_name,last_name,owner_user_id) VALUES($1,'Other','Contact',$2) RETURNING id`, f.orgB, carol),
		"lead":    must(`INSERT INTO leads(organization_id,title,owner_user_id) VALUES($1,'Other Lead',$2) RETURNING id`, f.orgB, carol),
		"task":    must(`INSERT INTO tasks(organization_id,title,status,priority,assignee_user_id,created_by) VALUES($1,'Other Task','open','normal',$2,$2) RETURNING id`, f.orgB, carol),
		"ticket":  must(`INSERT INTO tickets(organization_id,subject,status,priority,assignee_user_id) VALUES($1,'Other Ticket','new','normal',$2) RETURNING id`, f.orgB, carol),
	}

	// bob holds the read permissions but neither the assign nor the archive
	// permission, so the list endpoints restrict him to records he owns or that
	// belong to his team. He owns nothing in the fixture.
	f.readOnlyBob = rbac.NewPrincipal(f.bob, "bob@obj.test", "Bob", f.orgA, []rbac.Permission{
		rbac.PermAccountsRead, rbac.PermContactsRead, rbac.PermLeadsRead,
		rbac.PermDealsRead, rbac.PermTasksRead, rbac.PermTicketsRead,
	})
	f.readOnlyBob.TeamIDs = []uuid.UUID{team}

	// alice owns her records and holds the archive/assign permissions, so she is
	// the legitimate reader the fix must keep working.
	f.managerAlice = rbac.NewPrincipal(f.alice, "alice@obj.test", "Alice", f.orgA, []rbac.Permission{
		rbac.PermAccountsRead, rbac.PermAccountsArchive, rbac.PermContactsRead, rbac.PermContactsArchive,
		rbac.PermLeadsRead, rbac.PermLeadsArchive, rbac.PermDealsRead, rbac.PermDealsArchive,
		rbac.PermTasksRead, rbac.PermTasksAssign, rbac.PermTicketsRead, rbac.PermTicketsAssign,
	})
	return f
}

// get returns the single-record read for an entity type.
func (f *objectAuthzFixture) get(t *testing.T, p *rbac.Principal, entity string, id uuid.UUID) error {
	t.Helper()
	ctx := context.Background()
	var err error
	switch entity {
	case "account":
		_, err = f.svc.GetAccount(ctx, p, id)
	case "contact":
		_, err = f.svc.GetContact(ctx, p, id)
	case "lead":
		_, err = f.svc.GetLead(ctx, p, id)
	case "deal":
		_, err = f.svc.GetDeal(ctx, p, id)
	case "task":
		_, err = f.svc.GetTask(ctx, p, id)
	case "ticket":
		_, err = f.svc.GetTicket(ctx, p, id)
	default:
		t.Fatalf("unknown entity %q", entity)
	}
	return err
}

// TestObjectAuthorizationIsConsistentAcrossReadPaths is the Phase 2.5.4
// regression guard. For every CRM entity it checks that a user who cannot see a
// record in a list also cannot reach it by id, and that search does not leak it.
func TestObjectAuthorizationIsConsistentAcrossReadPaths(t *testing.T) {
	f := newObjectAuthzFixture(t)
	entities := []string{"account", "contact", "lead", "deal", "task", "ticket"}

	for _, entity := range entities {
		entity := entity
		t.Run("single-record read hides "+entity, func(t *testing.T) {
			err := f.get(t, f.readOnlyBob, entity, f.aliceOnly[entity])
			if !errors.Is(err, crm.ErrNotFound) {
				t.Fatalf("bob read a %s owned by alice: err = %v, want ErrNotFound", entity, err)
			}
		})
		t.Run("single-record read stays inside the organization for "+entity, func(t *testing.T) {
			id, ok := f.crossTenant[entity]
			if !ok {
				t.Skip("no cross-tenant fixture for this entity")
			}
			if err := f.get(t, f.readOnlyBob, entity, id); !errors.Is(err, crm.ErrNotFound) {
				t.Fatalf("bob read a %s from another organization: err = %v, want ErrNotFound", entity, err)
			}
		})
		t.Run("owner still reads own "+entity, func(t *testing.T) {
			if err := f.get(t, f.managerAlice, entity, f.aliceOnly[entity]); err != nil {
				t.Fatalf("alice could not read her own %s: %v", entity, err)
			}
		})
	}

	t.Run("search does not leak records hidden from the list", func(t *testing.T) {
		ctx := context.Background()
		results, err := f.svc.Search(ctx, f.readOnlyBob, "Zebra", 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			for entity, id := range f.aliceOnly {
				if r.ID == id {
					t.Fatalf("search leaked a %s (%s) that the list endpoints hide", entity, id)
				}
			}
		}
	})

	t.Run("list endpoints and single-record reads agree", func(t *testing.T) {
		ctx := context.Background()
		q := crm.ListQuery{Pagination: httpx.Pagination{Limit: 100}}
		deals, _, err := f.svc.ListDeals(ctx, f.readOnlyBob, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range deals {
			if d.ID == f.aliceOnly["deal"] {
				t.Fatal("list already hid the deal, so the single-record read must too")
			}
		}
	})
}

// TestObjectAuthorizationBlocksForeignMutations checks that the read gap cannot
// be turned into a write. bob holds the corresponding update permissions, so the
// only thing standing between him and alice's records is the object-level check.
func TestObjectAuthorizationBlocksForeignMutations(t *testing.T) {
	f := newObjectAuthzFixture(t)
	ctx := context.Background()
	bobWriter := rbac.NewPrincipal(f.bob, "bob@obj.test", "Bob", f.orgA, []rbac.Permission{
		rbac.PermDealsRead, rbac.PermDealsUpdate, rbac.PermTasksRead, rbac.PermTasksUpdate,
		rbac.PermContactsRead, rbac.PermContactsUpdate, rbac.PermLeadsRead, rbac.PermLeadsUpdate,
		rbac.PermTicketsRead, rbac.PermTicketsUpdate,
	})
	bobWriter.TeamIDs = f.readOnlyBob.TeamIDs

	deal, err := f.svc.GetDeal(ctx, f.managerAlice, f.aliceOnly["deal"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateDeal(ctx, bobWriter, deal.ID, crm.DealInput{
		PipelineID: deal.PipelineID, StageID: deal.StageID, Title: "hijacked",
		Amount: 1, Currency: "USD",
	}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("bob updated a deal owned by alice: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.UpdateTask(ctx, bobWriter, f.aliceOnly["task"], crm.TaskInput{
		Title: "hijacked", Status: "open", Priority: "normal",
	}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("bob updated a task assigned to alice: err = %v, want ErrNotFound", err)
	}
	contact, err := f.svc.GetContact(ctx, f.managerAlice, f.aliceOnly["contact"])
	if err != nil {
		t.Fatal(err)
	}
	hijack := "hijacked"
	if _, err := f.svc.UpdateContact(ctx, bobWriter, contact.ID, crm.ContactInput{
		FirstName: &hijack, LastName: &hijack,
	}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("bob updated a contact owned by alice: err = %v, want ErrNotFound", err)
	}
	lead, err := f.svc.GetLead(ctx, f.managerAlice, f.aliceOnly["lead"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateLead(ctx, bobWriter, lead.ID, crm.LeadInput{
		Title: "hijacked",
	}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("bob updated a lead owned by alice: err = %v, want ErrNotFound", err)
	}
	ticket, err := f.svc.GetTicket(ctx, f.managerAlice, f.aliceOnly["ticket"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateTicket(ctx, bobWriter, ticket.ID, crm.TicketInput{
		Subject: "hijacked", Status: "open", Priority: "normal",
	}); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("bob updated a ticket assigned to alice: err = %v, want ErrNotFound", err)
	}

	for _, check := range []struct {
		table, column string
		id            uuid.UUID
	}{
		{"deals", "title", f.aliceOnly["deal"]},
		{"tasks", "title", f.aliceOnly["task"]},
		{"contacts", "first_name", f.aliceOnly["contact"]},
		{"leads", "title", f.aliceOnly["lead"]},
		{"tickets", "subject", f.aliceOnly["ticket"]},
	} {
		var value string
		if err := f.db.Pool().QueryRow(ctx,
			`SELECT `+check.column+` FROM `+check.table+` WHERE id = $1`, check.id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value == "hijacked" {
			t.Fatalf("the %s row was actually modified despite the error", check.table)
		}
	}
}

// TestTeamlessCallerSeesOnlyOwnWorkItems pins the personal-work-item rule: a
// caller with no team and without the assign permission reads only the tasks and
// tickets assigned to them, never the whole organization.
func TestTeamlessCallerSeesOnlyOwnWorkItems(t *testing.T) {
	f := newObjectAuthzFixture(t)
	ctx := context.Background()
	teamless := rbac.NewPrincipal(f.bob, "bob@obj.test", "Bob", f.orgA, []rbac.Permission{
		rbac.PermTasksRead, rbac.PermTicketsRead,
	})

	if err := f.get(t, teamless, "task", f.aliceOnly["task"]); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("a teamless caller read a task assigned to alice: err = %v, want ErrNotFound", err)
	}
	if err := f.get(t, teamless, "ticket", f.aliceOnly["ticket"]); !errors.Is(err, crm.ErrNotFound) {
		t.Fatalf("a teamless caller read a ticket assigned to alice: err = %v, want ErrNotFound", err)
	}

	// Their own task stays reachable.
	own, err := f.svc.CreateTask(ctx, f.managerAlice, crm.TaskInput{
		Title: "Bob's task", Status: "open", Priority: "normal", AssigneeUserID: &f.bob,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GetTask(ctx, teamless, own.ID); err != nil {
		t.Fatalf("a teamless caller could not read their own task: %v", err)
	}
}
