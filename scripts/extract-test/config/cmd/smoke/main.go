// Command smoke exercises the extracted config package: the loader applies its
// defaults and refuses the combinations that would otherwise fail silently.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/config"
)

func main() {
	// A database URL is mandatory; without one the loader refuses rather than
	// starting against an empty DSN.
	os.Unsetenv("FORGECRM_DATABASE_URL")
	if _, err := config.Load(); err == nil {
		fail("the loader accepted an empty database URL")
	}
	os.Setenv("FORGECRM_DATABASE_URL", "postgres://user:pass@127.0.0.1:5432/forgecrm?sslmode=disable")

	cfg, err := config.Load()
	if err != nil {
		fail("load with a database URL: %v", err)
	}
	if cfg.RateLimitStore != "memory" {
		fail("rate-limit store defaulted to %q, want memory", cfg.RateLimitStore)
	}
	// Retention is the safety-critical default: off, and a dry run when on.
	if cfg.Retention.Enabled {
		fail("retention is enabled by default; it must be opt-in")
	}
	if !cfg.Retention.DryRun {
		fail("retention is not a dry run by default")
	}
	if cfg.Retention.AllowAudit {
		fail("the audit trail is swept by default")
	}
	fmt.Println("config defaults and validation ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
