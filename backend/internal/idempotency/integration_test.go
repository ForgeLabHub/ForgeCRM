//go:build integration

// The idempotency store is checked against the real schema. The claim is an
// INSERT ... ON CONFLICT, and its correctness — that a second claim sees the first
// row rather than inserting a second — is a property of the database, not of Go
// code a mock could stand in for.
package idempotency

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
)

func connectOrSkip(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping integration test")
	}
	db, err := database.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func freshSchema(t *testing.T, db *database.DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := migrate.Up(os.Getenv("FORGECRM_TEST_DATABASE_URL"), slog.Default()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// The first claim takes the key; a second sees it as in flight because no response
// has been stored yet.
func TestClaimTakesThenReportsInFlight(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	outcome, stored, err := store.Claim(ctx, "scope", "key-1")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if outcome != Claimed || stored != nil {
		t.Fatalf("first claim = %v/%v, want Claimed/nil", outcome, stored)
	}

	outcome, stored, err = store.Claim(ctx, "scope", "key-1")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if outcome != InFlight || stored != nil {
		t.Fatalf("second claim = %v/%v, want InFlight/nil", outcome, stored)
	}
}

// Once a response is stored, a later claim returns it verbatim, so the handler does
// not run again.
func TestClaimReplaysAStoredResponse(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	if _, _, err := store.Claim(ctx, "scope", "key-2"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, "scope", "key-2", 201, "application/json", []byte(`{"id":"abc"}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}

	outcome, stored, err := store.Claim(ctx, "scope", "key-2")
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if outcome != Replayed {
		t.Fatalf("outcome = %v, want Replayed", outcome)
	}
	if stored == nil || stored.Status != 201 || stored.ContentType != "application/json" {
		t.Fatalf("stored = %+v, want status 201 and the content type", stored)
	}
	if string(stored.Body) != `{"id":"abc"}` {
		t.Fatalf("stored body = %q", stored.Body)
	}
}

// A binary body must survive the JSON round trip, which is why it is stored as
// bytes rather than a string.
func TestStoredResponsePreservesBinaryBodies(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	body := []byte{0x00, 0x01, 0xff, 0xfe, 0x7f}
	if _, _, err := store.Claim(ctx, "scope", "key-bin"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, "scope", "key-bin", 200, "application/octet-stream", body); err != nil {
		t.Fatalf("complete: %v", err)
	}
	_, stored, err := store.Claim(ctx, "scope", "key-bin")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if string(stored.Body) != string(body) {
		t.Fatalf("binary body changed: %v", stored.Body)
	}
}

// Releasing a key lets it be claimed again, which is what a client needs after a
// failed request.
func TestReleaseAllowsReclaim(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	if _, _, err := store.Claim(ctx, "scope", "key-3"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Release(ctx, "scope", "key-3"); err != nil {
		t.Fatalf("release: %v", err)
	}
	outcome, _, err := store.Claim(ctx, "scope", "key-3")
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if outcome != Claimed {
		t.Fatalf("outcome after release = %v, want Claimed", outcome)
	}
}

// Two concurrent claims for one key must not both win. Exactly one is Claimed; the
// other is InFlight. This is the race the whole feature exists to close.
func TestConcurrentClaimsYieldExactlyOneOwner(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	const racers = 8
	var wg sync.WaitGroup
	results := make([]Outcome, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outcome, _, err := store.Claim(ctx, "scope", "key-race")
			results[i], errs[i] = outcome, err
		}(i)
	}
	close(start)
	wg.Wait()

	claimed := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if results[i] == Claimed {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("%d racers claimed the key, want exactly 1 (results: %v)", claimed, results)
	}
}

// A claim for a key that does not exist must not report an error: it takes the key.
// This guards the "no rows" path that a lost INSERT returns.
func TestClaimOfAFreshKeyIsNotAnError(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)

	outcome, _, err := store.Claim(context.Background(), "scope", "never-seen")
	if err != nil {
		t.Fatalf("claim of a fresh key returned an error: %v", err)
	}
	if outcome != Claimed {
		t.Fatalf("outcome = %v, want Claimed", outcome)
	}
}

// An abandoned claim must not hold the key forever. A request that died without
// storing a response — the process was killed, the host lost power — leaves a row
// with response NULL. Once that claim has outlived its lease, a retry takes it over
// and runs, instead of being refused with 409 until retention sweeps the row.
func TestAnAbandonedClaimIsTakenOverAfterTheLease(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	// Abandon a claim by taking the key and never completing it.
	if outcome, _, err := store.Claim(ctx, "scope", "key-abandoned"); err != nil || outcome != Claimed {
		t.Fatalf("first claim = %v/%v, want Claimed/nil", outcome, err)
	}
	// Within the lease it is still held: the owner may simply be slow.
	if outcome, _, err := store.Claim(ctx, "scope", "key-abandoned"); err != nil || outcome != InFlight {
		t.Fatalf("claim within the lease = %v/%v, want InFlight/nil", outcome, err)
	}

	// Age the claim past the lease by writing created_at directly: the lease is
	// measured against the database clock, and a test must not sleep for minutes.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE idempotency_keys SET created_at = now() - make_interval(secs => $1) WHERE scope = $2 AND key = $3`,
		(ClaimLease + time.Minute).Seconds(), "scope", "key-abandoned"); err != nil {
		t.Fatalf("age the claim: %v", err)
	}

	outcome, stored, err := store.Claim(ctx, "scope", "key-abandoned")
	if err != nil {
		t.Fatalf("claim after the lease: %v", err)
	}
	if outcome != Claimed || stored != nil {
		t.Fatalf("claim after the lease = %v/%v, want Claimed/nil (the retry must run)", outcome, stored)
	}
	// The takeover refreshed the claim, so an immediate second attempt is refused
	// again: the new owner is running.
	if outcome, _, err := store.Claim(ctx, "scope", "key-abandoned"); err != nil || outcome != InFlight {
		t.Fatalf("claim after takeover = %v/%v, want InFlight/nil", outcome, err)
	}
}

// A claim that has been completed must never be taken over, however old it is: the
// stored response is the whole point of the key, and replaying it is what makes a
// retry safe.
func TestACompletedClaimIsReplayedNotTakenOver(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	if _, _, err := store.Claim(ctx, "scope", "key-old"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, "scope", "key-old", 201, "application/json", []byte(`{"id":"kept"}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// Age the row well past the lease. A stored response has no lease.
	if _, err := db.Pool().Exec(ctx,
		`UPDATE idempotency_keys SET created_at = now() - interval '30 days' WHERE scope = $1 AND key = $2`,
		"scope", "key-old"); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	outcome, stored, err := store.Claim(ctx, "scope", "key-old")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if outcome != Replayed || stored == nil || string(stored.Body) != `{"id":"kept"}` {
		t.Fatalf("outcome = %v stored = %+v, want Replayed with the stored body", outcome, stored)
	}
}

// Releasing a key must not erase a response another request already stored: a late
// failure from an execution whose claim was taken over must not delete the result
// the newer owner committed.
func TestReleaseDoesNotEraseAStoredResponse(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	if _, _, err := store.Claim(ctx, "scope", "key-stored"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, "scope", "key-stored", 200, "application/json", []byte(`{"id":"kept"}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := store.Release(ctx, "scope", "key-stored"); err != nil {
		t.Fatalf("release: %v", err)
	}

	outcome, stored, err := store.Claim(ctx, "scope", "key-stored")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if outcome != Replayed || stored == nil {
		t.Fatalf("outcome = %v stored = %+v, want the stored response to survive the release", outcome, stored)
	}
}

// A completed response must not be overwritten by a second execution that lost its
// claim to a takeover. The first result wins, so a client never sees two different
// answers for one key.
func TestCompleteDoesNotOverwriteAnExistingResponse(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	store := NewStore(db)
	ctx := context.Background()

	if _, _, err := store.Claim(ctx, "scope", "key-winner"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, "scope", "key-winner", 200, "application/json", []byte(`{"winner":"first"}`)); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	// The late owner of the same key tries to store its own result.
	if err := store.Complete(ctx, "scope", "key-winner", 200, "application/json", []byte(`{"winner":"second"}`)); err != nil {
		t.Fatalf("second complete: %v", err)
	}

	_, stored, err := store.Claim(ctx, "scope", "key-winner")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if stored == nil || string(stored.Body) != `{"winner":"first"}` {
		t.Fatalf("stored body = %q, want the first result to be kept", stored.Body)
	}
}
