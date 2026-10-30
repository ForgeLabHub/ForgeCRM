// Command smoke exercises the extracted simulate package: the scenario catalog is
// non-empty and every entry names the parameters it accepts.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/simulate"
)

func main() {
	cat := simulate.Catalog()
	if len(cat) == 0 {
		fail("the scenario catalog is empty")
	}
	seen := map[string]bool{}
	for _, e := range cat {
		if e.Name == "" {
			fail("a catalog entry has no name")
		}
		if seen[string(e.Kind)] {
			fail("scenario kind %q appears twice", e.Kind)
		}
		seen[string(e.Kind)] = true
		for _, p := range e.Parameters {
			if p.Key == "" {
				fail("scenario %q has a parameter with no key", e.Kind)
			}
		}
	}
	fmt.Printf("scenario catalog ok: %d kinds\n", len(cat))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
