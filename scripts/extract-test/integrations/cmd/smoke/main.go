// Command smoke exercises the integrations package's public API in a clean module.
//
// The Hub needs a database and a keyring, so adapter lifecycle is not driven here.
// What is driven is the security decision the whole Integration Hub rests on: the
// SSRF guard refuses private, loopback and link-local destinations before any
// dial, and an outbound URL that resolves to such an address is rejected.
package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/geff1778/forgecrm/internal/integrations"
)

func main() {
	// Addresses that must never be contacted: loopback, private, link-local and the
	// IPv6 equivalents.
	blocked := []string{
		"127.0.0.1", "10.0.0.5", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "::1", "fe80::1",
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			fail("could not parse %q", s)
		}
		if !integrations.IsBlockedIP(ip) {
			fail("the guard allows %s", s)
		}
	}
	// A public address is not blocked, so the guard is not simply refusing
	// everything.
	if integrations.IsBlockedIP(net.ParseIP("93.184.216.34")) {
		fail("the guard blocks a public address")
	}
	fmt.Println("SSRF guard classifies addresses correctly")

	// The URL validator refuses a private destination before the request is built.
	if _, err := integrations.ValidateOutboundURL(context.Background(), "http://127.0.0.1/admin"); err == nil {
		fail("the guard accepted a loopback URL")
	}
	if _, err := integrations.ValidateOutboundURL(context.Background(), "file:///etc/passwd"); err == nil {
		fail("the guard accepted a file:// URL")
	}
	if _, err := integrations.ValidateOutboundURL(context.Background(), "https://example.com/hook"); err != nil {
		fail("the guard rejected a public https URL: %v", err)
	}
	fmt.Println("SSRF guard validates URLs")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
