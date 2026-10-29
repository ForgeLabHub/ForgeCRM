package rbac

import (
	"testing"

	"github.com/google/uuid"
)

// TestPlatformOwnerReportsEveryPermission pins the agreement between Has and
// Permissions for a platform owner.
//
// A platform owner holds every permission (Has returns true for anything). The
// set reported by Permissions is what /auth/me hands the client, and the client
// gates pages on it. When Permissions returned only the role-derived subset, the
// owner's own session advertised fewer permissions than the server enforced, so
// the interface hid pages the API would have served — for example /ai, whose
// route requires ai.read, a permission no seeded role necessarily carries.
func TestPlatformOwnerReportsEveryPermission(t *testing.T) {
	owner := &Principal{
		UserID:          uuid.New(),
		Email:           "owner@example.test",
		OrganizationID:  uuid.New(),
		IsPlatformOwner: true,
		permissions:     map[Permission]struct{}{PermContactsRead: {}},
	}

	reported := owner.Permissions()
	if len(reported) != len(All()) {
		t.Fatalf("platform owner reports %d permissions, want the whole catalog of %d",
			len(reported), len(All()))
	}

	for _, perm := range All() {
		if !owner.Has(perm) {
			t.Fatalf("Has(%q) = false for a platform owner", perm)
		}
	}

	// The specific regression: ai.read must be reported even though the owner's
	// role grants never included it.
	if !contains(reported, PermAIRead) {
		t.Fatalf("platform owner does not report %q; /ai would be hidden", PermAIRead)
	}
}

// TestNonOwnerReportsOnlyHeldPermissions confirms the owner branch is the only
// special case, so an ordinary principal still reports exactly what it holds.
func TestNonOwnerReportsOnlyHeldPermissions(t *testing.T) {
	p := NewPrincipal(uuid.New(), "rep@example.test", "Rep", uuid.New(),
		[]Permission{PermContactsRead, PermContactsCreate})

	reported := p.Permissions()
	if len(reported) != 2 {
		t.Fatalf("reported %d permissions, want 2", len(reported))
	}
	if !contains(reported, PermContactsRead) || !contains(reported, PermContactsCreate) {
		t.Fatalf("reported set %v does not match the granted permissions", reported)
	}
	if p.Has(PermAIRead) {
		t.Fatal("a non-owner must not hold ai.read it was never granted")
	}
}

// TestNilPrincipalPermissionsIsSafe guards the /auth/me path against a nil
// principal, which the middleware should prevent but which must not panic here.
func TestNilPrincipalPermissionsIsSafe(t *testing.T) {
	var p *Principal
	if got := p.Permissions(); got != nil {
		t.Fatalf("nil principal returned %v, want nil", got)
	}
	if p.Has(PermContactsRead) {
		t.Fatal("nil principal must not hold a permission")
	}
}

func contains(perms []Permission, want Permission) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}
