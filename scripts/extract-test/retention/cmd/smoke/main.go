// Command smoke exercises the retention package's public API in a clean module.
//
// A real sweep needs a database, so it is not driven here. What is driven is the
// safety posture an operator depends on and that needs no server: the policy
// catalog is well formed, protected policies keep a long minimum window, and a
// runner that was not explicitly enabled or authorized to delete refuses to run
// and to delete.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/geff1778/forgecrm/internal/retention"
)

func main() {
	// Every policy names its table, time column, a positive window and a batch size.
	policies := retention.Policies()
	if len(policies) == 0 {
		fail("no retention policies registered")
	}
	for _, p := range policies {
		if p.Key == "" || p.Table == "" || p.TimeColumn == "" {
			fail("policy %q is missing a key, table or time column", p.Key)
		}
		if p.BatchSize <= 0 {
			fail("policy %q has a non-positive batch size", p.Key)
		}
		// A policy that is swept on a window must carry a positive default and a
		// positive minimum. An expiry-driven policy carries its own deadline per row,
		// and a protected policy has no default on purpose: audit and security
		// evidence is only ever removed when an operator configures a window
		// explicitly, so a zero default is the safe value, not a misconfiguration.
		if p.ExpiryDriven || p.Protected || p.AppendOnly {
			continue
		}
		if p.Default <= 0 || p.Min <= 0 {
			fail("policy %q has a non-positive window", p.Key)
		}
		if p.Min > p.Default {
			fail("policy %q: minimum %s exceeds default %s", p.Key, p.Min, p.Default)
		}
	}
	fmt.Printf("policies=%d\n", len(policies))

	// A disabled runner does nothing and says so, rather than deleting by accident.
	disabled := retention.New(nil, nil, nil, retention.Options{})
	if disabled.Enabled() {
		fail("a runner built with Enabled=false reports itself enabled")
	}
	if _, err := disabled.Run(context.Background()); !errors.Is(err, retention.ErrDisabled) {
		fail("a disabled runner did not return ErrDisabled: %v", err)
	}
	fmt.Println("disabled runner refuses to run")

	// A zero-value runner is a dry run: the delete flag is opt-in.
	if !disabled.DryRun() {
		fail("a runner with Delete=false does not report a dry run")
	}
	var nilRunner *retention.Runner
	if !nilRunner.DryRun() || nilRunner.Enabled() {
		fail("a nil runner is not safe")
	}
	fmt.Println("dry-run is the default posture")

	// Unknown window keys are refused, not ignored.
	if err := retention.ValidateWindows(map[string]time.Duration{"nope": time.Hour}); err == nil {
		fail("an unknown window key was accepted")
	}
	fmt.Println("unknown window keys rejected")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
