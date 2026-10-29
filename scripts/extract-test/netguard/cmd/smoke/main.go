// Command smoke exercises the extracted netguard package: the guard must refuse a
// loopback destination and accept an ordinary public one.
//
// It proves the module runs on its own with no service and no network: the
// refusal is decided from the address alone.
package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/geff1778/forgecrm/internal/netguard"
)

func main() {
	ctx := context.Background()

	if _, err := netguard.ValidateOutboundURL(ctx, "http://127.0.0.1/admin"); err == nil {
		fmt.Fprintln(os.Stderr, "loopback destination was accepted")
		os.Exit(1)
	}
	if _, err := netguard.ValidateOutboundURL(ctx, "http://169.254.169.254/latest/meta-data/"); err == nil {
		fmt.Fprintln(os.Stderr, "link-local metadata destination was accepted")
		os.Exit(1)
	}
	if _, err := netguard.ValidateOutboundURL(ctx, "file:///etc/passwd"); err == nil {
		fmt.Fprintln(os.Stderr, "a non-http scheme was accepted")
		os.Exit(1)
	}
	if _, err := netguard.ValidateOutboundURL(ctx, "https://93.184.216.34/"); err != nil {
		fmt.Fprintf(os.Stderr, "a public literal address was refused: %v\n", err)
		os.Exit(1)
	}
	if !netguard.IsBlockedIP(net.ParseIP("10.0.0.5")) {
		fmt.Fprintln(os.Stderr, "a private address was not blocked")
		os.Exit(1)
	}
	fmt.Println("smoke: loopback blocked=1 metadata blocked=1 public allowed=1")
}
