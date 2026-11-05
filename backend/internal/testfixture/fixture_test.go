//go:build integration

// These tests prove the fixture itself works against the real schema. A fixture
// that builds an organization no role can be granted in, or a principal with no
// permissions, would make every test that depends on it pass vacuously.
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./internal/testfixture/...
package testfixture

import (
	"context"
	"testing"
)

// TestTenantProvisionsAnOwnerWithPermissions is the fixture's own contract: the
// owner it returns must actually hold the permissions the API resolves for that
// role. If the role clone or the grant were wrong, the principal would be empty
// and every authorization test built on it would be meaningless.
func TestTenantProvisionsAnOwnerWithPermissions(t *testing.T) {
	db := DB(t)
	org := Tenant(t, db, "fixture-owner")

	owner := org.Owner()
	if owner == nil {
		t.Fatal("Tenant returned no owner principal")
	}
	if owner.OrganizationID != org.ID {
		t.Fatalf("owner organization = %s, want %s", owner.OrganizationID, org.ID)
	}
	if len(owner.Permissions()) == 0 {
		t.Fatal("the owner principal holds no permissions; the role grant did not resolve")
	}
	if !owner.IsOrgOwner {
		t.Error("the owner principal is not marked as the organization owner")
	}
}

// TestTenantViewerIsReadOnly proves the second principal is genuinely
// under-privileged, so a test can use it to assert a denial rather than assume
// one.
func TestTenantViewerIsReadOnly(t *testing.T) {
	db := DB(t)
	org := Tenant(t, db, "fixture-viewer")

	viewer := org.Viewer()
	if len(viewer.Permissions()) == 0 {
		t.Fatal("the viewer principal holds no permissions")
	}
	if viewer.IsOrgOwner {
		t.Fatal("the viewer principal is marked as an owner")
	}
	if len(viewer.Permissions()) >= len(org.Owner().Permissions()) {
		t.Errorf("the viewer holds %d permissions and the owner %d; the read-only role is not narrower",
			len(viewer.Permissions()), len(org.Owner().Permissions()))
	}
}

// TestTenantsAreIsolated proves two tenants built by the fixture share no rows,
// which is the property every isolation test relies on.
func TestTenantsAreIsolated(t *testing.T) {
	db := DB(t)
	a := Tenant(t, db, "fixture-a")
	b := Tenant(t, db, "fixture-b")

	if a.ID == b.ID {
		t.Fatal("two tenants share an organization id")
	}
	Account(t, db, a.ID, "Only in A")

	ctx := context.Background()
	var visibleToB int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE organization_id = $1`, b.ID).Scan(&visibleToB); err != nil {
		t.Fatalf("count accounts in B: %v", err)
	}
	if visibleToB != 0 {
		t.Fatalf("organization B sees %d accounts that belong to A", visibleToB)
	}
}

// TestConnectedGraphIsWired proves the graph helpers produce rows the schema
// accepts and that the deal actually points at the pipeline stage it was given.
func TestConnectedGraphIsWired(t *testing.T) {
	db := DB(t)
	org := Tenant(t, db, "fixture-graph")
	g := Connected(t, db, org)

	ctx := context.Background()
	var pipelineID, stageID string
	if err := db.Pool().QueryRow(ctx,
		`SELECT pipeline_id, stage_id FROM deals WHERE id = $1`, g.Deal).
		Scan(&pipelineID, &stageID); err != nil {
		t.Fatalf("read the seeded deal: %v", err)
	}
	if pipelineID != g.Pipeline.String() {
		t.Errorf("deal pipeline = %s, want %s", pipelineID, g.Pipeline)
	}
	if stageID != g.Stages[0].String() {
		t.Errorf("deal stage = %s, want the entry stage %s", stageID, g.Stages[0])
	}

	var contacts int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM contacts WHERE account_id = $1`, g.Account).Scan(&contacts); err != nil {
		t.Fatalf("count contacts: %v", err)
	}
	if contacts != 1 {
		t.Errorf("the account has %d contacts, want 1", contacts)
	}
}
