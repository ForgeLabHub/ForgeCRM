// Command smoke exercises the database package's public API in a clean module.
//
// It confirms the pool-size guard works outside the monolith. No server is
// needed: the guard is enforced before a connection is attempted, which is the
// property that makes it testable everywhere.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/geff1778/forgecrm/internal/database"
)

func main() {
	// An over-large pool is refused at startup, before any connection is tried.
	_, err := database.ConnectWithPool(context.Background(),
		"postgres://user:pass@127.0.0.1:5432/db?sslmode=disable", database.MaxAllowedConns+1)
	if err == nil || !strings.Contains(err.Error(), "exceeds the maximum") {
		fail("over-large pool not refused: %v", err)
	}
	fmt.Printf("over-large pool refused: %v\n", err)

	// The default must itself be within the bound, or Connect would always fail.
	if database.DefaultMaxConns > database.MaxAllowedConns {
		fail("default %d exceeds allowed %d; Connect would always fail",
			database.DefaultMaxConns, database.MaxAllowedConns)
	}
	fmt.Printf("default=%d allowed=%d\n", database.DefaultMaxConns, database.MaxAllowedConns)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
