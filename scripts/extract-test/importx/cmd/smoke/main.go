// Command smoke exercises the extracted importx package: the field catalog is the
// set of columns an import writes, and an unknown entity yields nothing rather
// than a guessed list.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/importx"
)

func main() {
	entities := []string{"account", "contact", "lead", "deal", "task", "ticket", "activity"}
	for _, e := range entities {
		if len(importx.FieldList(e)) == 0 {
			fail("entity %q exposes no fields", e)
		}
	}
	if got := importx.FieldList("no_such_entity"); got != nil {
		fail("an unknown entity returned %d fields instead of none", len(got))
	}
	fmt.Printf("field catalog ok: %d entities\n", len(entities))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
