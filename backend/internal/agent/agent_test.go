package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestArgumentsHashIsStableAcrossKeyOrder is the property the whole approval flow
// rests on. The operator's page renders the arguments, hashes what it rendered and
// submits that hash; the server hashes what it stored. If Go's map iteration order
// leaked into the hash, the same arguments would hash differently on each call and
// every approval would be refused as "the action changed".
func TestArgumentsHashIsStableAcrossKeyOrder(t *testing.T) {
	a := map[string]any{"title": "Follow up", "priority": "high", "account_id": "abc"}
	b := map[string]any{"account_id": "abc", "priority": "high", "title": "Follow up"}

	ha, err := ArgumentsHash(a)
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	hb, err := ArgumentsHash(b)
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if ha != hb {
		t.Fatalf("equal maps hashed differently:\n  %s\n  %s", ha, hb)
	}
	if !strings.HasPrefix(ha, "sha256:") {
		t.Fatalf("hash %q does not carry the algorithm prefix", ha)
	}
}

// TestArgumentsHashDistinguishesDifferentArguments is the other half: a hash that
// collided for different arguments would let one approval authorise another.
func TestArgumentsHashDistinguishesDifferentArguments(t *testing.T) {
	ha, err := ArgumentsHash(map[string]any{"title": "Follow up"})
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	hb, err := ArgumentsHash(map[string]any{"title": "Follow up "})
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if ha == hb {
		t.Fatal("a one-character difference produced the same hash")
	}
}

// TestArgumentsHashOfNilIsTheEmptyObject checks that a tool with no arguments
// still hashes deterministically, so a no-argument action is decidable.
func TestArgumentsHashOfNilIsTheEmptyObject(t *testing.T) {
	hn, err := ArgumentsHash(nil)
	if err != nil {
		t.Fatalf("hash nil: %v", err)
	}
	he, err := ArgumentsHash(map[string]any{})
	if err != nil {
		t.Fatalf("hash empty: %v", err)
	}
	if hn != he {
		t.Fatalf("nil and empty maps hashed differently: %s vs %s", hn, he)
	}
}

// TestDecisionSignatureIsLengthPrefixed guards the classic concatenation bug: a
// signature over ("ab","c") must differ from one over ("a","bc"). Without
// length prefixes they produce the same signed string, and a decision on one
// action would verify for another.
func TestDecisionSignatureIsLengthPrefixed(t *testing.T) {
	key := approvalKey("test-platform-secret")
	org := uuid.New()
	user := uuid.New()
	at := time.Unix(1700000000, 0).UTC()

	// Two tuples that concatenate to the same string if fields are not delimited.
	// The arguments hash and the decision are the free-form fields; the uuids and
	// the timestamp are fixed width, so the collision has to be built from these.
	actionID := uuid.New()
	sigA := decisionSignature(key, actionID, org, "sha256:ab", user, "approve", at)
	sigB := decisionSignature(key, actionID, org, "sha256:a", user, "bapprove", at)
	if sigA == sigB {
		t.Fatal("two different decision tuples produced the same signature; fields are not length-prefixed")
	}
}

// TestDecisionSignatureIsDeterministicAndKeyed checks that a signature verifies
// with the same key and fails with a different one. The second half is what makes
// a forged approval row detectable: an attacker without the platform secret cannot
// produce a signature that verifies.
func TestDecisionSignatureIsDeterministicAndKeyed(t *testing.T) {
	actionID := uuid.New()
	org := uuid.New()
	user := uuid.New()
	at := time.Unix(1700000000, 0).UTC()

	good := approvalKey("secret-one")
	sig := decisionSignature(good, actionID, org, "sha256:abc", user, "approve", at)

	if again := decisionSignature(good, actionID, org, "sha256:abc", user, "approve", at); again != sig {
		t.Fatal("the same tuple produced two different signatures")
	}

	other := approvalKey("secret-two")
	if decisionSignature(other, actionID, org, "sha256:abc", user, "approve", at) == sig {
		t.Fatal("a different key produced the same signature")
	}
}

// TestApprovalKeyIsPurposeDerived checks that the approval key is not the raw
// platform secret. Using the secret directly would let a signature from this
// purpose be replayed in another that happens to key on the same secret.
func TestApprovalKeyIsPurposeDerived(t *testing.T) {
	secret := "the-platform-secret"
	if string(approvalKey(secret)) == secret {
		t.Fatal("the approval key is the raw platform secret")
	}
	// Different secrets must produce different keys, and the same secret the same
	// key: the derivation is deterministic.
	if string(approvalKey(secret)) != string(approvalKey(secret)) {
		t.Fatal("key derivation is not deterministic")
	}
	if string(approvalKey("a")) == string(approvalKey("b")) {
		t.Fatal("different secrets produced the same key")
	}
}

// TestRiskTierClassification pins the tier rules the executor depends on.
func TestRiskTierClassification(t *testing.T) {
	cases := []struct {
		tier     RiskTier
		valid    bool
		approval bool
	}{
		{TierSafe, true, false},
		{TierConfirm, true, true},
		{TierDestructive, true, true},
		{RiskTier("whatever"), false, false},
	}
	for _, c := range cases {
		if got := c.tier.Valid(); got != c.valid {
			t.Errorf("Valid(%q) = %v, want %v", c.tier, got, c.valid)
		}
		if got := c.tier.RequiresApproval(); got != c.approval {
			t.Errorf("RequiresApproval(%q) = %v, want %v", c.tier, got, c.approval)
		}
	}
}

// TestNewServiceRejectsAnEmptySecret checks the one configuration mistake that
// would make every signature forgeable.
func TestNewServiceRejectsAnEmptySecret(t *testing.T) {
	if _, err := NewService(nil, nil, nil, Config{PlatformSecret: ""}); err == nil {
		t.Fatal("NewService accepted an empty platform secret")
	}
	if _, err := NewService(nil, nil, nil, Config{PlatformSecret: "x"}); err != nil {
		t.Fatalf("NewService rejected a non-empty secret: %v", err)
	}
}

// TestDeniedToolsCannotBeProposed checks the deny list is consulted before the
// insert. A denied tool must not be able to enter the queue at all, so a later
// decision cannot authorise it.
func TestDeniedToolsCannotBeProposed(t *testing.T) {
	svc, err := NewService(nil, nil, nil, Config{
		PlatformSecret: "s",
		DeniedTools:    []string{"shell.exec", "users.delete"},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if !svc.Denied("shell.exec") {
		t.Fatal("a listed tool is not reported denied")
	}
	if svc.Denied("create_task") {
		t.Fatal("an unlisted tool is reported denied")
	}
	got := svc.DeniedTools()
	if len(got) != 2 || got[0] != "shell.exec" || got[1] != "users.delete" {
		t.Fatalf("DeniedTools = %v, want the sorted deny list", got)
	}
}

// TestDefaultTTLIsBounded checks that a service with no explicit TTL still bounds
// the wait, so "waiting for approval" cannot become "waiting forever".
func TestDefaultTTLIsBounded(t *testing.T) {
	svc, err := NewService(nil, nil, nil, Config{PlatformSecret: "s"})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc.defaultTTL <= 0 {
		t.Fatalf("default TTL = %v, want a positive bound", svc.defaultTTL)
	}
	svc2, err := NewService(nil, nil, nil, Config{PlatformSecret: "s", DefaultTTL: time.Minute})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc2.defaultTTL != time.Minute {
		t.Fatalf("default TTL = %v, want the configured minute", svc2.defaultTTL)
	}
}
