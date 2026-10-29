// Command smoke exercises the extracted ratelimit package with the in-memory
// store, which is the path that needs no external service.
//
// It proves the module runs on its own: a limiter is built, a small budget is
// consumed and the refusal is observed. The sequence is deterministic because the
// clock is passed in.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/geff1778/forgecrm/internal/ratelimit"
)

func main() {
	store := ratelimit.NewMemoryStore()
	defer store.Close()

	limiter, err := ratelimit.New(store, ratelimit.Policy{
		Name:      "smoke",
		Limit:     3,
		Window:    time.Minute,
		FailMode:  ratelimit.FailClosed,
		KeyPrefix: "smoke",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "build limiter: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	allowed := 0
	for i := 0; i < 5; i++ {
		if limiter.Allow(ctx, "subject", now).Allowed {
			allowed++
		}
	}
	if allowed != 3 {
		fmt.Fprintf(os.Stderr, "allowed %d requests, want 3\n", allowed)
		os.Exit(1)
	}
	fmt.Printf("smoke: allowed=%d limit=%d\n", allowed, limiter.Policy().Limit)
}
