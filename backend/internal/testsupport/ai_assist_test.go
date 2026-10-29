//go:build integration

package testsupport

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Contextual AI reads through crm.Service with the caller's principal, so these
// tests assert what the service actually returns for a real principal against a
// real schema. They do not mock the CRM service: the property under test is that
// the entity permission is enforced on the real query path, and a mock would
// assert the mock's behaviour instead.

// TestAssistRefusesToReadAnotherOrganizationsAccount is the tenant-isolation
// check for the contextual path: a principal scoped to organization B must not
// obtain a summary of a record in organization A, even holding the read
// permission.
func TestAssistRefusesToReadAnotherOrganizationsAccount(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	orgA := insertOrganization(t, db, "ai-org-a")
	orgB := insertOrganization(t, db, "ai-org-b")

	accountA := insertAccount(t, db, orgA, "Acme in A")

	crmSvc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	svc := ai.NewService(nil, db, audit.New(db), crmSvc, nil)

	// A principal in B with the AI and accounts permissions. The organization
	// filter, not the permission, is what must refuse this.
	foreign := rbac.NewPrincipal(uuid.New(), "b@example.com", "B", orgB,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAIRead, rbac.PermAccountsRead})

	_, err := svc.Assemble(ctx, foreign, ai.AssistRequest{
		EntityType: ai.EntityAccount,
		EntityID:   accountA,
		Kind:       ai.KindSummary,
	})
	if err == nil {
		t.Fatal("a principal in organization B read an account in organization A")
	}

	// The owning organization can read it, which proves the refusal above was
	// scope and not a missing record.
	owner := rbac.NewPrincipal(uuid.New(), "a@example.com", "A", orgA,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAIRead, rbac.PermAccountsRead})
	bundle, err := svc.Assemble(ctx, owner, ai.AssistRequest{
		EntityType: ai.EntityAccount,
		EntityID:   accountA,
		Kind:       ai.KindSummary,
	})
	if err != nil {
		t.Fatalf("the owning organization could not assemble the account context: %v", err)
	}
	if bundle.Subject != "Acme in A" {
		t.Errorf("subject = %q, want %q", bundle.Subject, "Acme in A")
	}
}

// TestAssembleOmitsDataTheCallerCannotRead checks that a caller with accounts.read
// but no contacts.read gets an account summary with no contacts and an explicit
// disclosure, rather than a silent gap or a leak.
func TestAssembleOmitsDataTheCallerCannotRead(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "ai-scoped")
	accountID := insertAccount(t, db, org, "Scoped Co")

	// A contact in the account, which this principal must not see.
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO contacts (organization_id, account_id, first_name, last_name, email)
		 VALUES ($1, $2, 'Jane', 'Doe', 'jane@scoped.example')`, org, accountID); err != nil {
		t.Fatalf("insert a contact: %v", err)
	}

	crmSvc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	svc := ai.NewService(nil, db, audit.New(db), crmSvc, nil)

	// accounts.read only. No contacts.read, no activities.read, no tasks.read.
	p := rbac.NewPrincipal(uuid.New(), "p@example.com", "P", org,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermAccountsRead})

	bundle, err := svc.Assemble(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityAccount,
		EntityID:   accountID,
		Kind:       ai.KindSummary,
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	// No fact or block may name the contact.
	for _, f := range bundle.Facts {
		if strings.Contains(f.Value, "Jane") || strings.Contains(f.Value, "jane@scoped.example") {
			t.Errorf("a contact the caller cannot read appeared in the facts: %+v", f)
		}
	}
	for _, b := range bundle.Blocks {
		if strings.Contains(b.Content, "Jane") || strings.Contains(b.Content, "jane@scoped.example") {
			t.Errorf("a contact the caller cannot read appeared in a context block %q", b.Label)
		}
	}

	// And the omission must be disclosed, so the user knows the summary is partial.
	found := false
	for _, d := range bundle.Disclosures {
		if d.Kind == "omitted" && strings.Contains(d.Detail, "contacts.read") {
			found = true
		}
	}
	if !found {
		t.Errorf("the omitted contacts were not disclosed: %+v", bundle.Disclosures)
	}
}

// TestAssistMasksContactPersonalData checks that a contact's email and phone are
// masked in the assembled facts, so the model is not given a usable address.
func TestAssistMasksContactPersonalData(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "ai-mask")

	var contactID uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO contacts (organization_id, first_name, last_name, email, phone)
		 VALUES ($1, 'Jane', 'Doe', 'jane@scoped.example', '+15550101234')
		 RETURNING id`, org).Scan(&contactID); err != nil {
		t.Fatalf("insert a contact: %v", err)
	}

	crmSvc := crm.NewService(db, audit.New(db), outbox.NewStore(db))
	svc := ai.NewService(nil, db, audit.New(db), crmSvc, nil)

	p := rbac.NewPrincipal(uuid.New(), "p@example.com", "P", org,
		[]rbac.Permission{rbac.PermAIToolsExecute, rbac.PermContactsRead})

	bundle, err := svc.Assemble(ctx, p, ai.AssistRequest{
		EntityType: ai.EntityContact,
		EntityID:   contactID,
		Kind:       ai.KindSummary,
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	for _, f := range bundle.Facts {
		if strings.Contains(f.Value, "jane@scoped.example") {
			t.Errorf("the full email address reached the facts: %+v", f)
		}
		if strings.Contains(f.Value, "5550101234") {
			t.Errorf("the full phone number reached the facts: %+v", f)
		}
	}
	// The masked form should still be present, so the model can reason about the
	// organisation.
	joined := ""
	for _, f := range bundle.Facts {
		joined += f.Label + "=" + f.Value + ";"
	}
	if !strings.Contains(joined, "@scoped.example") {
		t.Errorf("the masked email did not keep the domain: %s", joined)
	}
}
