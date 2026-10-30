// Command smoke exercises the crm package's public API in a clean module.
//
// The service needs a database, so a create is not driven here. What is driven is
// the validation boundary an attacker's payload hits first — before any database
// access — and the JSONMap column codec.
package main

import (
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
)

func main() {
	// A deal with no pipeline is refused on the field the client sent, not on a
	// generic error: the API returns details keyed by field.
	bad := &crm.DealInput{Title: "Renewal", StageID: uuid.New()}
	if err := bad.Validate(); err == nil {
		fail("a deal with no pipeline was accepted")
	}
	good := &crm.DealInput{Title: "Renewal", PipelineID: uuid.New(), StageID: uuid.New()}
	if err := good.Validate(); err != nil {
		fail("a valid deal was rejected: %v", err)
	}
	if good.Currency != "USD" {
		fail("currency did not default to USD, got %q", good.Currency)
	}
	fmt.Println("deal validation and currency default ok")

	// The contact rule is "a name or an email"; neither is refused.
	if err := (&crm.ContactInput{}).Validate(); err == nil {
		fail("a contact with no name and no email was accepted")
	}
	email := "ada@example.com"
	if err := (&crm.ContactInput{Email: &email}).Validate(); err != nil {
		fail("an email-only contact was rejected: %v", err)
	}
	fmt.Println("contact validation ok")

	// The bulk-archive shape rule caps the request and refuses a nil UUID.
	ids := []uuid.UUID{uuid.Nil}
	if err := (&crm.BulkArchiveInput{IDs: ids}).Validate(); err == nil {
		fail("a nil UUID was accepted in a bulk archive")
	}
	if err := (&crm.BulkArchiveInput{IDs: []uuid.UUID{uuid.New()}}).Validate(); err != nil {
		fail("a valid bulk archive was rejected: %v", err)
	}
	fmt.Println("bulk archive shape ok")

	// A nil JSONMap persists as an empty object, not SQL NULL.
	v, err := crm.JSONMap(nil).Value()
	if err != nil || v != "{}" {
		fail("nil JSONMap Value() = %v (%v), want {}", v, err)
	}
	var m crm.JSONMap
	if err := m.Scan([]byte(`{"k":1}`)); err != nil || m["k"] != float64(1) {
		fail("JSONMap scan failed: %v %v", m, err)
	}
	fmt.Println("JSONMap codec ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
