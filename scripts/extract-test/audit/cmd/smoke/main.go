// Command smoke exercises the audit package's public API in a clean module.
//
// The write paths need a database, so they are not driven here; what is driven is
// the part every caller depends on and that needs no server — the action
// vocabulary and the validation that runs before any write.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
)

func main() {
	// The validation runs before the database is touched, so a Logger with no
	// database still rejects an unattributable entry. That ordering is the guard:
	// it keeps a row with no action out of an append-only trail.
	l := audit.New(nil)
	if err := l.Record(context.Background(), audit.Entry{EntityType: "contact"}); err == nil {
		fail("an audit entry with no action was accepted")
	}
	if err := l.RecordSecurity(context.Background(), audit.SecurityEvent{Severity: "warn"}); err == nil {
		fail("a security event with no type was accepted")
	}
	fmt.Println("validation runs before any write")

	// The action vocabulary is namespaced, which is what a filter or an alert is
	// written against.
	for _, a := range []audit.Action{audit.ActionLogin, audit.ActionAIInsightGenerated, audit.ActionRetentionSweep} {
		if !strings.Contains(string(a), ".") {
			fail("action %q is not namespaced", a)
		}
	}
	fmt.Printf("sample actions: %s, %s\n", audit.ActionLogin, audit.ActionAIInsightGenerated)

	// UUIDPtr maps the zero UUID to nil, so an entry for a system actor carries a
	// NULL actor rather than the all-zero UUID.
	if audit.UUIDPtr(uuid.Nil) != nil {
		fail("UUIDPtr(uuid.Nil) should be nil")
	}
	fmt.Println("helpers ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
