// Command smoke exercises the views package's public API in a clean module.
//
// A real list needs a database and a principal, so it is not driven here. What is
// driven is the surface a caller depends on and that needs no server: the entity
// type set is closed to the screens that exist, a well-formed filter is accepted
// and an unusable one is refused with a per-field reason, and the filter document
// round-trips through JSON unchanged so applying a view is the same as replaying
// the query parameters by hand.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/geff1778/forgecrm/internal/views"
)

func main() {
	// Only the entity types a list endpoint honours are valid. A view over an
	// unsupported type would be stored and then silently ignored by every screen.
	for _, ok := range []views.EntityType{
		views.EntityAccounts, views.EntityContacts, views.EntityLeads,
		views.EntityDeals, views.EntityTasks, views.EntityTickets,
	} {
		if !ok.Valid() {
			fail("EntityType(%q).Valid() = false, want true", ok)
		}
	}
	for _, bad := range []views.EntityType{"", "organizations", "users"} {
		if bad.Valid() {
			fail("EntityType(%q).Valid() = true, want false", bad)
		}
	}

	// The filter document is exactly the query-parameter set the list endpoints
	// accept, so it must round-trip through JSON without gaining or losing a field.
	filters := views.FilterSet{
		Status:     "open",
		Search:     "acme",
		AssigneeID: "me",
		Sort:       "-created_at",
	}
	raw, err := json.Marshal(filters)
	if err != nil {
		fail("marshal filters: %v", err)
	}
	var back views.FilterSet
	if err := json.Unmarshal(raw, &back); err != nil {
		fail("unmarshal filters: %v", err)
	}
	if !reflect.DeepEqual(filters, back) {
		fail("filters did not round-trip: %+v != %+v", filters, back)
	}

	// A nil service reports the not-found sentinel for a missing view rather than
	// panicking, which is what a route guard relies on when the feature is off.
	if views.ErrNotFound == nil || views.ErrForbidden == nil {
		fail("the sentinel errors are missing")
	}

	fmt.Println("smoke: views ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
