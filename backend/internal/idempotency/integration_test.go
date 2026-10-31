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
