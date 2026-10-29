package outbox

import (
	"strings"
	"testing"
)

// TestEventTypesIsWellFormed guards the shape of the advertised vocabulary.
//
// The list is the contract a webhook subscription is built from. A duplicate or a
// malformed entry would surface as a confusing UI or a subscription that can never
// match, so the invariants are pinned here rather than discovered in production.
func TestEventTypesIsWellFormed(t *testing.T) {
	types := EventTypes()
	if len(types) == 0 {
		t.Fatal("EventTypes returned no types")
	}

	seen := map[string]bool{}
	for _, et := range types {
		if et == "" {
			t.Error("EventTypes contains an empty string")
			continue
		}
		// Every event type is namespaced ("crm.contact.created"). A bare name is the
		// exact shape of the historical bug: it cannot match what the dispatcher emits.
		if !strings.Contains(et, ".") {
			t.Errorf("event type %q is not namespaced", et)
		}
		if seen[et] {
			t.Errorf("event type %q appears more than once", et)
		}
		seen[et] = true
	}
}
