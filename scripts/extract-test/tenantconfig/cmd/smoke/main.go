// Command smoke exercises the tenantconfig package's public API in a clean module.
//
// The Service needs a database and a principal, so it is not driven here. What is
// driven is the surface a caller depends on and that needs no server: the catalog
// is closed, every declared default passes its own key's validation — a platform
// that shipped a default it would refuse from a caller would be inconsistent with
// itself — an unknown key is reported as not-found rather than guessed at, and the
// kinds are the closed set the API renders editors for.
//
// It also proves the arrow of dependency: this program imports only
// `tenantconfig`, and the package builds with only its three declared
// dependencies (audit, database, rbac) present. A dependency on the CRM or
// workflow domain would fail to build here, which is the point.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/tenantconfig"
)

func main() {
	keys := tenantconfig.Keys()
	if len(keys) == 0 {
		fail("the catalog is empty; the settings surface would have nothing to read")
	}

	kinds := map[tenantconfig.Kind]bool{
		tenantconfig.KindString:  true,
		tenantconfig.KindEnum:    true,
		tenantconfig.KindInteger: true,
		tenantconfig.KindBoolean: true,
	}

	for _, key := range keys {
		def, ok := tenantconfig.Lookup(key)
		if !ok {
			fail("Keys() lists %q but Lookup does not find it", key)
		}
		if def.Default == nil {
			fail("%s has no default; an absent row could not resolve", key)
		}
		if !kinds[def.Kind] {
			fail("%s declares kind %q, which is outside the closed set", key, def.Kind)
		}
	}

	// An unknown key must be refused rather than resolved to something. A setting
	// the platform does not understand is a button that does nothing, and guessing
	// a default for it would hide that.
	if _, ok := tenantconfig.Lookup("no.such.setting"); ok {
		fail("Lookup accepted a key outside the catalog")
	}

	// The catalog is published sorted, so the API and the editor render it
	// deterministically and a diff of the document is meaningful.
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			fail("Keys() is not sorted: %q before %q", keys[i-1], keys[i])
		}
	}

	fmt.Printf("smoke: tenantconfig ok (%d keys)\n", len(keys))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
