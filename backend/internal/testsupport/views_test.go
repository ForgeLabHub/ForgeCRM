//go:build integration

package testsupport

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/views"
)

// newViewsFixture builds a views service with two users in one organization, each
// holding the two saved-view permissions, plus a second organization for the
// isolation check.
type viewsFixture struct {
	db     *database.DB
	svc    *views.Service
	orgA   uuid.UUID
	orgB   uuid.UUID
	userA  uuid.UUID
	userB  uuid.UUID
	other  uuid.UUID
	prinA  *rbac.Principal
	prinB  *rbac.Principal
	prinBs *rbac.Principal
}

func newViewsFixture(t *testing.T) *viewsFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	orgA := insertOrganization(t, db, "views-a")
	orgB := insertOrganization(t, db, "views-b")
	userA := seedMember(t, db, orgA, "sales_manager")
	userB := seedMember(t, db, orgA, "sales_representative")
	other := seedMember(t, db, orgB, "sales_manager")

	perms := []rbac.Permission{rbac.PermViewsRead, rbac.PermViewsManage}
	return &viewsFixture{
		db:    db,
		svc:   views.NewService(db, audit.New(db)),
		orgA:  orgA,
		orgB:  orgB,
		userA: userA,
		userB: userB,
		other: other,
		prinA: rbac.NewPrincipal(userA, "a@example.test", "A", orgA, perms),
		prinB: rbac.NewPrincipal(userB, "b@example.test", "B", orgA, perms),
		// The same person-scoped permissions, but a different organization: the
		// principal is what the handler resolves, so this is the realistic shape of a
		// cross-tenant attempt.
		prinBs: rbac.NewPrincipal(other, "c@example.test", "C", orgB, perms),
	}
}

func TestSavedViewCreateAndListAreOwnerScoped(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.prinA, views.Input{
		EntityType: "deals", Name: "My open deals",
		Filters: views.FilterSet{Status: "open", Sort: "-amount"},
	})
	if err != nil {
		t.Fatalf("create view: %v", err)
	}
	if !created.IsMine {
		t.Error("a freshly created view should report IsMine")
	}

	// A private view is invisible to a colleague on the same screen.
	other, err := f.svc.List(ctx, f.prinB, views.EntityDeals)
	if err != nil {
		t.Fatalf("list as colleague: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("a colleague saw %d of another user's private views, want 0", len(other))
	}

	// The owner sees it.
	mine, err := f.svc.List(ctx, f.prinA, views.EntityDeals)
	if err != nil {
		t.Fatalf("list as owner: %v", err)
	}
	if len(mine) != 1 || mine[0].Name != "My open deals" {
		t.Fatalf("owner list = %+v, want the one view", mine)
	}
	if mine[0].Filters.Status != "open" || mine[0].Filters.Sort != "-amount" {
		t.Fatalf("the filter document did not round-trip: %+v", mine[0].Filters)
	}
}

func TestSavedViewSharedIsVisibleButNotEditable(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.prinA, views.Input{
		EntityType: "accounts", Name: "Team accounts", Shared: true,
	})
	if err != nil {
		t.Fatalf("create shared view: %v", err)
	}

	seen, err := f.svc.List(ctx, f.prinB, views.EntityAccounts)
	if err != nil {
		t.Fatalf("list shared view: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("colleague saw %d shared views, want 1", len(seen))
	}
	if seen[0].IsMine {
		t.Error("a shared view should not report IsMine for a non-owner")
	}

	// A colleague may read it but not rename it.
	if _, err := f.svc.Update(ctx, f.prinB, created.ID, views.Input{
		EntityType: "accounts", Name: "Hijacked",
	}); err != views.ErrForbidden {
		t.Fatalf("update by non-owner = %v, want ErrForbidden", err)
	}
	if err := f.svc.Delete(ctx, f.prinB, created.ID); err != views.ErrForbidden {
		t.Fatalf("delete by non-owner = %v, want ErrForbidden", err)
	}
}

func TestSavedViewCrossOrganizationIsInvisible(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.prinA, views.Input{
		EntityType: "leads", Name: "Cross-tenant probe", Shared: true,
	})
	if err != nil {
		t.Fatalf("create view: %v", err)
	}

	// Even a shared view is shared inside its organization, not across them.
	if _, err := f.svc.Get(ctx, f.prinBs, created.ID); err != views.ErrNotFound {
		t.Fatalf("get from another organization = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Update(ctx, f.prinBs, created.ID, views.Input{
		EntityType: "leads", Name: "Renamed", Shared: true,
	}); err != views.ErrNotFound {
		t.Fatalf("update from another organization = %v, want ErrNotFound", err)
	}
	list, err := f.svc.List(ctx, f.prinBs, views.EntityLeads)
	if err != nil {
		t.Fatalf("list from another organization: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("another organization saw %d views, want 0", len(list))
	}
}

func TestSavedViewNameIsUniquePerOwnerAndScreen(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Create(ctx, f.prinA, views.Input{
		EntityType: "tasks", Name: "Mine",
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// The same name on the same screen is refused with a validation error, not a
	// raw unique-violation.
	_, err := f.svc.Create(ctx, f.prinA, views.Input{EntityType: "tasks", Name: "mine"})
	var ve *views.ValidationError
	if !asValidation(err, &ve) {
		t.Fatalf("duplicate name error = %v, want a ValidationError", err)
	}
	if ve.Fields["name"] == "" {
		t.Errorf("the duplicate-name error should name the field: %+v", ve.Fields)
	}

	// A different owner may reuse the same name, and the same owner may reuse it on
	// a different screen.
	if _, err := f.svc.Create(ctx, f.prinB, views.Input{EntityType: "tasks", Name: "Mine"}); err != nil {
		t.Fatalf("a colleague should be able to use the same name: %v", err)
	}
	if _, err := f.svc.Create(ctx, f.prinA, views.Input{EntityType: "contacts", Name: "Mine"}); err != nil {
		t.Fatalf("the same name on another screen should be allowed: %v", err)
	}
}

func TestSavedViewValidationRefusesUnusableFilters(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		input views.Input
		field string
	}{
		{"unknown entity", views.Input{EntityType: "invoices", Name: "x"}, "entity_type"},
		{"empty name", views.Input{EntityType: "deals", Name: "   "}, "name"},
		{"non-uuid owner", views.Input{EntityType: "deals", Name: "x",
			Filters: views.FilterSet{OwnerUserID: "not-a-uuid"}}, "filters.owner_user_id"},
		{"bad date", views.Input{EntityType: "deals", Name: "x",
			Filters: views.FilterSet{From: "yesterday"}}, "filters.from"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.Create(ctx, f.prinA, tc.input)
			var ve *views.ValidationError
			if !asValidation(err, &ve) {
				t.Fatalf("error = %v, want a ValidationError", err)
			}
			if ve.Fields[tc.field] == "" {
				t.Errorf("expected an error on %q, got %+v", tc.field, ve.Fields)
			}
		})
	}
}

func TestSavedViewDeleteArchivesAndFreesTheName(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.prinA, views.Input{EntityType: "deals", Name: "Temporary"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.svc.Delete(ctx, f.prinA, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Gone from the list.
	list, err := f.svc.List(ctx, f.prinA, views.EntityDeals)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("archived view still listed: %+v", list)
	}
	// The name is free again, because the unique index excludes archived rows.
	if _, err := f.svc.Create(ctx, f.prinA, views.Input{EntityType: "deals", Name: "Temporary"}); err != nil {
		t.Fatalf("reuse the name after delete: %v", err)
	}
	// Deleting again is a 404, not a silent success.
	if err := f.svc.Delete(ctx, f.prinA, created.ID); err != views.ErrNotFound {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

func TestSavedViewUpdatePersistsFilters(t *testing.T) {
	f := newViewsFixture(t)
	ctx := context.Background()

	created, err := f.svc.Create(ctx, f.prinA, views.Input{EntityType: "deals", Name: "Pipeline"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pipeline := uuid.New().String()
	updated, err := f.svc.Update(ctx, f.prinA, created.ID, views.Input{
		EntityType: "deals", Name: "Pipeline v2", Pinned: true,
		Filters: views.FilterSet{PipelineID: pipeline, IncludeArchived: true},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Pipeline v2" || !updated.Pinned {
		t.Fatalf("update did not persist: %+v", updated)
	}
	reloaded, err := f.svc.Get(ctx, f.prinA, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Filters.PipelineID != pipeline || !reloaded.Filters.IncludeArchived {
		t.Fatalf("filters did not persist: %+v", reloaded.Filters)
	}
}

// asValidation reports whether err is a *views.ValidationError, filling target.
func asValidation(err error, target **views.ValidationError) bool {
	ve, ok := err.(*views.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
