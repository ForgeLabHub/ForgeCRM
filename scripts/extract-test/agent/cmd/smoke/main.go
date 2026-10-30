// Command smoke exercises the agent package's public API in a clean module.
//
// The queue needs a database, so a proposal is not driven here. What is driven is
// the part the whole approval flow rests on and that needs no server: the
// arguments hash is stable across map key order, the risk tiers classify
// correctly, and a service refuses to start without a signing secret.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/agent"
)

func main() {
	// The operator's page renders the arguments, hashes what it rendered and
	// submits that hash; the server hashes what it stored. If map iteration order
	// leaked into the hash, the same arguments would hash differently on each call
	// and every approval would be refused as "the action changed".
	a := map[string]any{"title": "Follow up", "priority": "high", "account_id": "abc"}
	b := map[string]any{"account_id": "abc", "priority": "high", "title": "Follow up"}
	ha, err := agent.ArgumentsHash(a)
	if err != nil {
		fail("hash a: %v", err)
	}
	hb, err := agent.ArgumentsHash(b)
	if err != nil {
		fail("hash b: %v", err)
	}
	if ha != hb {
		fail("the arguments hash depends on key order: %s != %s", ha, hb)
	}
	fmt.Println("arguments hash is stable across key order")

	// A tier that mutates state, or is irreversible, must be gated behind a human
	// decision; a read-only tier must not be.
	if !agent.TierConfirm.RequiresApproval() || !agent.TierDestructive.RequiresApproval() {
		fail("a mutating or destructive tier does not require approval")
	}
	if agent.TierSafe.RequiresApproval() {
		fail("the safe tier requires approval")
	}
	if !agent.TierSafe.Valid() || !agent.TierConfirm.Valid() || !agent.TierDestructive.Valid() {
		fail("a known tier is reported invalid")
	}
	if agent.RiskTier("nonsense").Valid() {
		fail("an unknown tier is reported valid")
	}
	fmt.Println("risk tiers classify correctly")

	// An empty signing secret would make every approval signature forgeable, so the
	// service refuses to be built with one.
	if _, err := agent.NewService(nil, nil, nil, agent.Config{}); err == nil {
		fail("a service was built with an empty platform secret")
	}
	if _, err := agent.NewService(nil, nil, nil, agent.Config{PlatformSecret: "x"}); err != nil {
		fail("a service with a secret failed to build: %v", err)
	}
	fmt.Println("empty signing secret refused")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
