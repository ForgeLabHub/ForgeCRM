// Command smoke exercises the extracted workflow package: the template catalog
// validates, every template converts to a create input, and a malformed cron is
// refused.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/workflow"
)

func main() {
	// The catalog validates against the platform's event vocabulary, so the
	// vocabulary is registered first — the same call the engine makes at startup.
	workflow.RegisterKnownEvents(outbox.EventTypes())
	if err := workflow.ValidateCatalog(); err != nil {
		fail("the template catalog is invalid: %v", err)
	}
	ts := workflow.Templates()
	if len(ts) == 0 {
		fail("the catalog is empty")
	}
	// Every template must convert to a create input, so a picker can offer it.
	for i := range ts {
		if _, err := workflow.InputFromTemplate(&ts[i], ts[i].Name); err != nil {
			fail("template %q did not convert to a create input: %v", ts[i].ID, err)
		}
	}
	fmt.Printf("catalog validates: %d templates\n", len(ts))

	if _, err := workflow.ParseCron("not a cron"); err == nil {
		fail("a malformed cron expression was accepted")
	}
	if _, err := workflow.ParseCron("0 9 * * 1"); err != nil {
		fail("a valid cron expression was refused: %v", err)
	}
	fmt.Println("cron parsing ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
