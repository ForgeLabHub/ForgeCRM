package views

import (
	"errors"
	"strings"
	"testing"
)

// TestEntityTypeIsClosed pins the supported set. A view is only useful if a list
// endpoint exists to apply it to, so an unsupported type must be refused here
// rather than stored and then silently ignored by every screen.
func TestEntityTypeIsClosed(t *testing.T) {
	for _, ok := range []EntityType{EntityAccounts, EntityContacts, EntityLeads, EntityDeals, EntityTasks, EntityTickets} {
		if !ok.Valid() {
			t.Errorf("EntityType(%q).Valid() = false, want true", ok)
		}
	}
	for _, bad := range []EntityType{"", "Account", "organizations", "users"} {
		if bad.Valid() {
			t.Errorf("EntityType(%q).Valid() = true, want false", bad)
		}
	}
}

func TestValidateAcceptsAWellFormedView(t *testing.T) {
	err := validate(Input{
		EntityType: string(EntityDeals),
		Name:       "Hot leads",
		Filters:    FilterSet{Status: "open"},
	})
	if err != nil {
		t.Fatalf("validate returned %v, want nil", err)
	}
}

// TestValidateRefusesABadFilterIdentifier is the guard that keeps "the view shows
// nothing" from being a mystery: a stored identifier that is not a UUID is a
// filter the list endpoint would silently drop, so it is refused on write.
func TestValidateRefusesABadFilterIdentifier(t *testing.T) {
	err := validate(Input{
		EntityType: string(EntityDeals),
		Name:       "Broken",
		Filters:    FilterSet{OwnerUserID: "not-a-uuid"},
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("validate returned %v, want a ValidationError", err)
	}
	if _, ok := ve.Fields["filters.owner_user_id"]; !ok {
		t.Errorf("error does not name the offending field: %v", ve.Fields)
	}
}

func TestValidateRefusesBadTimestamps(t *testing.T) {
	err := validate(Input{
		EntityType: string(EntityTasks),
		Name:       "Recent",
		Filters:    FilterSet{From: "yesterday"},
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("validate returned %v, want a ValidationError", err)
	}
	if _, ok := ve.Fields["filters.from"]; !ok {
		t.Errorf("error does not name the offending field: %v", ve.Fields)
	}
}

func TestValidateRefusesAnEmptyName(t *testing.T) {
	err := validate(Input{EntityType: string(EntityDeals), Name: "   "})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("validate returned %v, want a ValidationError", err)
	}
	if _, ok := ve.Fields["name"]; !ok {
		t.Errorf("error does not name the name field: %v", ve.Fields)
	}
}

func TestValidateRefusesAnOverlongName(t *testing.T) {
	err := validate(Input{EntityType: string(EntityDeals), Name: strings.Repeat("x", 101)})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("validate returned %v, want a ValidationError", err)
	}
	if _, ok := ve.Fields["name"]; !ok {
		t.Errorf("error does not name the name field: %v", ve.Fields)
	}
}

// TestValidationErrorListsFieldsInOrder keeps the message stable, so a client
// snapshot or a log line does not churn on Go's map iteration order.
func TestValidationErrorListsFieldsInOrder(t *testing.T) {
	e := &ValidationError{Fields: map[string]string{"b": "second", "a": "first"}}
	if got := e.Error(); got != "invalid saved view: a: first; b: second" {
		t.Errorf("Error() = %q, want the fields sorted", got)
	}
}
