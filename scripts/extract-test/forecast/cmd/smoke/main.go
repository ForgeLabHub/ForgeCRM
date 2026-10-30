// Command smoke exercises the extracted forecast package: the method catalog is
// non-empty and every windowed method states the window the UI should offer.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/forecast"
)

func main() {
	cat := forecast.Catalog()
	if len(cat) == 0 {
		fail("the forecast method catalog is empty")
	}
	for _, e := range cat {
		if e.Name == "" {
			fail("a method has no name")
		}
		if e.Windowed && e.DefaultWindow <= 0 {
			fail("windowed method %q has no default window", e.Method)
		}
		if !e.Windowed && e.DefaultWindow != 0 {
			fail("method %q is not windowed but carries a window", e.Method)
		}
	}
	fmt.Printf("forecast method catalog ok: %d methods\n", len(cat))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
