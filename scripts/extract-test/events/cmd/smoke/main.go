// Command smoke exercises the extracted events package: a signed inbound request
// verifies, the tolerance and the payload cap are the contract, and the
// correlation helper invents nothing.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/geff1778/forgecrm/internal/events"
)

func main() {
	secret := []byte("smoke-secret")
	ts := fmt.Sprintf("%d", time.Now().Unix())
	body := []byte(`{"type":"crm.contact.created"}`)

	if events.SignInboundForTest(secret, ts, body) == "" {
		fail("SignInboundForTest returned an empty signature")
	}
	if events.TimestampTolerance != 5*time.Minute {
		fail("timestamp tolerance changed to %s", events.TimestampTolerance)
	}
	if events.MaxInboundPayloadBytes != 1<<20 {
		fail("max inbound payload changed to %d", events.MaxInboundPayloadBytes)
	}
	fmt.Println("signed inbound contract ok")

	if id := events.CorrelationIDFrom(context.Background()); id != "" {
		fail("CorrelationIDFrom invented %q for an empty context", id)
	}
	fmt.Println("correlation helper ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
