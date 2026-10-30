// Command smoke exercises the rbac package's public API in a clean module.
//
// It confirms the catalog, the principal's checks and the permission set the
// client is handed all work outside the monolith. The DB-backed Service is not
// driven here — that needs a database — but the pure half of the package, which
// is what every handler calls on every request, is.
package main

import (
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/rbac"
)

func main() {
	// The catalog is non-empty and every entry is known to Known.
	catalog := rbac.All()
	if len(catalog) == 0 {
		fail("empty permission catalog")
	}
	for _, p := range catalog {
		if !rbac.Known(p) {
			fail("catalog entry %q is not Known", p)
		}
	}
	fmt.Printf("catalog=%d\n", len(catalog))

	// A principal holds exactly what it was granted.
	p := rbac.NewPrincipal(uuid.New(), "ada@example.test", "Ada", uuid.New(),
		[]rbac.Permission{rbac.PermContactsRead, rbac.PermDealsRead})
	if !p.Has(rbac.PermContactsRead) || p.Has(rbac.PermDealsUpdate) {
		fail("Has is wrong: read=%v update=%v", p.Has(rbac.PermContactsRead), p.Has(rbac.PermDealsUpdate))
	}
	if err := p.Require(rbac.PermDealsUpdate); err == nil {
		fail("Require allowed a permission the principal does not hold")
	}
	fmt.Printf("principal perms=%d\n", len(p.Permissions()))

	// A platform owner holds everything, and reports the whole catalog so the
	// client is not told the server will refuse what it would actually allow.
	owner := rbac.NewPrincipal(uuid.New(), "root@example.test", "Root", uuid.New(), nil)
	owner.IsPlatformOwner = true
	if !owner.Has(rbac.PermDealsApproveDiscount) {
		fail("platform owner does not hold an arbitrary permission")
	}
	if got := len(owner.Permissions()); got != len(catalog) {
		fail("platform owner reports %d permissions, want the whole catalog of %d", got, len(catalog))
	}
	fmt.Printf("owner perms=%d\n", len(owner.Permissions()))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
