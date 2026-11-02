// The audit hash chain is checked against the real schema and the real
// append-only trigger. Whether a chain is intact is a property of the rows and
// their order in PostgreSQL, not of Go code a mock could stand in for: the
// tamper cases have to actually alter a stored row, and the concurrency case has
// to actually run several writers against one table.
//
// It lives here rather than in internal/audit because it needs the migration
// runner, and internal/audit must stay extractable — the extraction proof for
// every module that depends on audit would fail if the package imported the
// monolith's migrate package, even from a test file.
//
// Run with:
//
// FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/testsupport/...
package testsupport

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
)

// seedAuditOrg inserts an organization and returns its id. Named to avoid
// colliding with the seed helpers other testsupport files define.
func seedAuditOrg(t *testing.T, db *database.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO organizations (name, slug) VALUES ($1::text, $2::text) RETURNING id`, name, name).Scan(&id)
	if err != nil {
		t.Fatalf("seed organization: %v", err)
	}
	return id
}

func recordAudit(t *testing.T, l *audit.Logger, org uuid.UUID, action string) {
	t.Helper()
	if err := l.Record(context.Background(), audit.Entry{
		OrganizationID: &org,
		ActorType:      "user",
		Action:         action,
		EntityType:     "contact",
		EntityID:       uuid.NewString(),
		After:          map[string]any{"action": action},
		Metadata:       map[string]any{"n": action},
		IP:             "203.0.113.7",
	}); err != nil {
		t.Fatalf("record %s: %v", action, err)
	}
}

// A chain built by ordinary Record calls verifies clean.
func TestChainIsIntactAfterOrdinaryWrites(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	org := seedAuditOrg(t, db, "acme")

	for i := 0; i < 5; i++ {
		recordAudit(t, l, org, fmt.Sprintf("contacts.created.%d", i))
	}

	res, err := l.Verify(context.Background(), &org)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Intact {
		t.Fatalf("chain reported broken: %+v", res)
	}
	if res.Checked != 5 {
		t.Fatalf("checked %d entries, want 5", res.Checked)
	}
	if res.Unchained != 0 {
		t.Fatalf("unchained = %d, want 0 for rows written after the chain existed", res.Unchained)
	}
}

// Editing a stored entry is detected, and the first break is named.
//
// The append-only trigger is disabled for the edit because that is exactly the
// threat model: an actor with direct write access bypasses the API and the
// trigger, and the chain is what makes the edit visible.
func TestVerifyDetectsAnAlteredEntry(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	org := seedAuditOrg(t, db, "acme")
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		recordAudit(t, l, org, fmt.Sprintf("contacts.created.%d", i))
	}

	// Find the id of the third entry and rewrite its action.
	var targetID int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT id FROM audit_log WHERE organization_id = $1 ORDER BY id ASC OFFSET 2 LIMIT 1`,
		org).Scan(&targetID); err != nil {
		t.Fatalf("locate target entry: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `ALTER TABLE audit_log DISABLE TRIGGER audit_log_no_update`); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE audit_log SET action = 'contacts.deleted' WHERE id = $1`, targetID); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `ALTER TABLE audit_log ENABLE TRIGGER audit_log_no_update`); err != nil {
		t.Fatalf("re-enable trigger: %v", err)
	}

	res, err := l.Verify(ctx, &org)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Intact {
		t.Fatal("an altered entry was reported as intact")
	}
	if res.FirstBreakID != targetID {
		t.Fatalf("first break = %d, want %d", res.FirstBreakID, targetID)
	}
}

// Removing an entry from the middle is detected at the entry that follows it.
func TestVerifyDetectsARemovedEntry(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	org := seedAuditOrg(t, db, "acme")
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		recordAudit(t, l, org, fmt.Sprintf("contacts.created.%d", i))
	}

	var removedID, followerID int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT id FROM audit_log WHERE organization_id = $1 ORDER BY id ASC OFFSET 1 LIMIT 1`,
		org).Scan(&removedID); err != nil {
		t.Fatalf("locate removed entry: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`SELECT id FROM audit_log WHERE organization_id = $1 ORDER BY id ASC OFFSET 2 LIMIT 1`,
		org).Scan(&followerID); err != nil {
		t.Fatalf("locate follower: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `ALTER TABLE audit_log DISABLE TRIGGER audit_log_no_update`); err != nil {
		t.Fatalf("disable trigger: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM audit_log WHERE id = $1`, removedID); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `ALTER TABLE audit_log ENABLE TRIGGER audit_log_no_update`); err != nil {
		t.Fatalf("re-enable trigger: %v", err)
	}

	res, err := l.Verify(ctx, &org)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Intact {
		t.Fatal("a removed entry was reported as intact")
	}
	// The break shows at the first surviving entry whose prev_hash points at the
	// row that is now gone.
	if res.FirstBreakID != followerID {
		t.Fatalf("first break = %d, want the follower %d", res.FirstBreakID, followerID)
	}
}

// Chains are per organization, so one tenant's trail verifies on its own and
// interleaved writes to another tenant do not affect it.
func TestChainsAreIndependentPerOrganization(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	orgA := seedAuditOrg(t, db, "alpha")
	orgB := seedAuditOrg(t, db, "beta")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		recordAudit(t, l, orgA, fmt.Sprintf("contacts.created.a%d", i))
		recordAudit(t, l, orgB, fmt.Sprintf("contacts.created.b%d", i))
	}

	for _, org := range []uuid.UUID{orgA, orgB} {
		res, err := l.Verify(ctx, &org)
		if err != nil {
			t.Fatalf("verify %s: %v", org, err)
		}
		if !res.Intact || res.Checked != 3 {
			t.Fatalf("chain for %s = %+v, want intact with 3 entries", org, res)
		}
	}
}

// Concurrent writers against one organization must not corrupt the chain: the
// advisory lock serializes them, so every entry still links to its predecessor.
func TestConcurrentWritesKeepTheChainIntact(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	org := seedAuditOrg(t, db, "acme")
	ctx := context.Background()

	const writers, perWriter = 8, 6
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if err := l.Record(ctx, audit.Entry{
					OrganizationID: &org,
					ActorType:      "user",
					Action:         fmt.Sprintf("contacts.created.%d.%d", w, i),
					EntityType:     "contact",
					EntityID:       uuid.NewString(),
					IP:             "203.0.113.7",
				}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent record: %v", err)
	}

	res, err := l.Verify(ctx, &org)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Intact {
		t.Fatalf("the chain was corrupted by concurrent writers: %+v", res)
	}
	if res.Checked != writers*perWriter {
		t.Fatalf("checked %d entries, want %d", res.Checked, writers*perWriter)
	}
}

// A SkipChain write is stored unchained, is reported as unchained, and does not
// break the chain around it.
func TestSkipChainWritesAnUnchainedEntry(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	org := seedAuditOrg(t, db, "acme")
	ctx := context.Background()

	recordAudit(t, l, org, "contacts.created.0")
	if err := l.Record(ctx, audit.Entry{
		OrganizationID: &org, ActorType: "system", Action: "system.note",
		EntityType: "system", SkipChain: true,
	}); err != nil {
		t.Fatalf("skip-chain record: %v", err)
	}
	recordAudit(t, l, org, "contacts.created.1")

	res, err := l.Verify(ctx, &org)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Intact {
		t.Fatalf("an unchained entry broke the chain: %+v", res)
	}
	if res.Unchained != 1 {
		t.Fatalf("unchained = %d, want 1", res.Unchained)
	}
	if res.Checked != 3 {
		t.Fatalf("checked = %d, want 3 (the unchained row is still counted)", res.Checked)
	}
}

// A bare Record with no ambient transaction is still chained: the logger takes
// its own transaction rather than silently writing an unchained row.
func TestRecordWithoutTransactionIsStillChained(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	l := audit.New(db)
	org := seedAuditOrg(t, db, "acme")

	recordAudit(t, l, org, "contacts.created.0")
	recordAudit(t, l, org, "contacts.created.1")

	res, err := l.Verify(context.Background(), &org)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Checked != 2 || res.Unchained != 0 {
		t.Fatalf("bare Record left rows unchained: %+v", res)
	}
}
