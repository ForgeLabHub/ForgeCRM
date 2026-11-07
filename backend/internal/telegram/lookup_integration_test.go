//go:build integration

package telegram

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/telegramtext"
	"github.com/geff1778/forgecrm/internal/testdb"
)

// TestLookupCommandsPostgres proves the bot's CRM lookup commands read through the
// real service and honor its object-level rules: the linked user sees their own
// organization's data and never another organization's, and a user without a
// read permission is refused rather than shown a filtered list.
func TestLookupCommandsPostgres(t *testing.T) {
	ctx := context.Background()
	db := testdb.Connect(t)
	raw := testdb.URL(t)
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(raw, slog.Default()); err != nil {
		t.Fatal(err)
	}

	must := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.Pool().QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}

	org := must(`INSERT INTO organizations(name,slug) VALUES('Acme','tg-lookup') RETURNING id`)
	otherOrg := must(`INSERT INTO organizations(name,slug) VALUES('Other','tg-lookup-other') RETURNING id`)
	user := must(`INSERT INTO users(email,display_name) VALUES('lookup@example.test','Lookup') RETURNING id`)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO memberships(user_id,organization_id) VALUES($1,$2)`, user, org); err != nil {
		t.Fatal(err)
	}

	pipe := must(`INSERT INTO pipelines(organization_id,name,is_default,currency) VALUES($1,'Sales',true,'USD') RETURNING id`, org)
	stage := must(`INSERT INTO pipeline_stages(pipeline_id,organization_id,name,position,probability) VALUES($1,$2,'Qualified',1,50) RETURNING id`, pipe, org)
	must(`INSERT INTO deals(organization_id,pipeline_id,stage_id,title,amount,currency,owner_user_id) VALUES($1,$2,$3,'Acme Renewal',1000,'USD',$4) RETURNING id`, org, pipe, stage, user)

	// A deal in another organization with the same searchable word must never appear.
	otherPipe := must(`INSERT INTO pipelines(organization_id,name,is_default,currency) VALUES($1,'Other Sales',true,'USD') RETURNING id`, otherOrg)
	otherStage := must(`INSERT INTO pipeline_stages(pipeline_id,organization_id,name,position,probability) VALUES($1,$2,'Qualified',1,50) RETURNING id`, otherPipe, otherOrg)
	must(`INSERT INTO deals(organization_id,pipeline_id,stage_id,title,amount,currency) VALUES($1,$2,$3,'Acme Renewal',9999,'USD') RETURNING id`, otherOrg, otherPipe, otherStage)

	svc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	reader := rbac.NewPrincipal(user, "lookup@example.test", "Lookup", org,
		[]rbac.Permission{rbac.PermDealsRead, rbac.PermPipelinesRead, rbac.PermAccountsRead})
	blind := rbac.NewPrincipal(user, "", "", org, nil)

	// Search finds the deal in the caller's organization only.
	out, err := handleLookupCommand(ctx, svc, reader, "/search Acme", telegramtext.LocaleEnglish)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "Acme Renewal") || strings.Contains(out, "9999") {
		t.Fatalf("search crossed an organization boundary: %q", out)
	}

	// The pipeline report sums only the caller's organization.
	out, err = handleLookupCommand(ctx, svc, reader, "/pipeline", telegramtext.LocaleEnglish)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if !strings.Contains(out, "USD 1000.00") || strings.Contains(out, "9999") {
		t.Fatalf("pipeline report crossed an organization boundary: %q", out)
	}

	// A principal without the read permission is refused, not shown a filtered list.
	out, err = handleLookupCommand(ctx, svc, blind, "/deals", telegramtext.LocaleEnglish)
	if err != nil {
		t.Fatalf("deals as blind user: %v", err)
	}
	if out != telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyNoPermission) {
		t.Fatalf("expected a permission refusal, got %q", out)
	}
}
