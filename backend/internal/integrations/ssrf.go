package integrations

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/geff1778/forgecrm/internal/netguard"
)

// The SSRF guard lives in internal/netguard so the object store — and any future
// module — can use the same rules without importing the Integration Hub. These
// aliases keep the existing `integrations.X` names working for every caller
// (adapters.go, dial.go, hub.go, migratex, events, storage) and for the tests, so
// moving the code changed no call site outside this file.

// GuardedClient is an HTTP client that refuses SSRF destinations on every hop.
type GuardedClient = netguard.GuardedClient

// NewGuardedClient builds a client with the given timeout.
func NewGuardedClient(timeout time.Duration) *GuardedClient {
	return netguard.NewGuardedClient(timeout)
}

// IsBlockedIP reports whether an address must not be contacted.
func IsBlockedIP(ip net.IP) bool { return netguard.IsBlockedIP(ip) }

// ValidateOutboundURL checks scheme, host and every resolved address.
func ValidateOutboundURL(ctx context.Context, raw string) (*url.URL, error) {
	return netguard.ValidateOutboundURL(ctx, raw)
}
