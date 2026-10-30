// Command smoke exercises the outbox package's public API in a clean module.
//
// The Store needs a database, so the write and claim paths are not driven here.
// What is driven is the part a webhook subscription is built from and the part
// that needs no server: the event vocabulary and the event-to-envelope mapping.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/outbox"
)

func main() {
	// The vocabulary is namespaced ("crm.contact.created"). A bare name is the
	// exact shape of the historical bug: a subscription created against it is
	// accepted, enabled, and then matches nothing the dispatcher emits.
	types := outbox.EventTypes()
	if len(types) == 0 {
		fail("EventTypes returned no types")
	}
	for _, et := range types {
		if et == "" || !strings.Contains(et, ".") {
			fail("event type %q is not namespaced", et)
		}
	}
	fmt.Printf("event types=%d first=%s\n", len(types), types[0])

	// ToEnvelope is the canonical external representation, so its field mapping is
	// the contract a consumer deserializes.
	org := uuid.New()
	ev := outbox.Event{
		ID:            uuid.New(),
		OrganizationID: &org,
		Type:          "crm.deal.won",
		SchemaVersion: 1,
		Source:        "crm",
		Payload:       json.RawMessage(`{"deal_id":"x"}`),
		OccurredAt:    time.Now().UTC(),
	}
	env := ev.ToEnvelope()
	if env.EventID != ev.ID || env.EventType != ev.Type || env.TenantID == nil || *env.TenantID != org {
		fail("ToEnvelope dropped or changed a field: %+v", env)
	}
	fmt.Printf("envelope event_type=%s\n", env.EventType)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
