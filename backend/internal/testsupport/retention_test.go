//go:build integration

package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/retention"
)

// These tests run the retention runner against the real schema. The properties
// under test — that a window deletes exactly the rows past it, that a protected
// table is untouched unless allowed, that a dry run removes nothing, and that a
// second run is a no-op — are all statements about SQL, so a mock would prove
// nothing.

// retentionOrg creates an organization to hang swept rows off.
func retentionOrg(t *testing.T, db *database.DB, slug string) string {
	t.Helper()
	id := insertOrganization(t, db, slug)
	return id.String()
}

// insertOldJob inserts a job with a controllable finish time and status.
func insertOldJob(t *testing.T, db *database.DB, orgID, status string, finishedAt time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
        INSERT INTO jobs (organization_id, kind, status, finished_at)
        VALUES ($1, 'import.execute', $2, $3)`, orgID, status, finishedAt)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
}

func countJobs(t *testing.T, db *database.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

// TestRetentionDeletesOnlyRowsPastTheWindow is the central correctness check: a
// job finished long ago is removed, a job finished recently and a job still
// running are kept.
func TestRetentionDeletesOnlyRowsPastTheWindow(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := retentionOrg(t, db, "retention-window")
	now := time.Now()

	insertOldJob(t, db, org, "succeeded", now.Add(-40*24*time.Hour)) // past 30d
	insertOldJob(t, db, org, "succeeded", now.Add(-1*time.Hour))     // inside 30d
	insertOldJob(t, db, org, "failed", now.Add(-40*24*time.Hour))    // failed is never swept
	insertOldJob(t, db, org, "running", now.Add(-40*24*time.Hour))   // running is never swept

	runner := retention.New(db, audit.New(db), nil, retention.Options{
		Enabled: true,
		Delete:  true,
	})
	results, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("retention run: %v", err)
	}

	var deleted int64
	for _, res := range results {
		if res.Key == "jobs_finished" {
			deleted = res.Deleted
		}
	}
	if deleted != 1 {
		t.Fatalf("jobs_finished deleted %d rows, want 1", deleted)
	}
	if got := countJobs(t, db); got != 3 {
		t.Fatalf("jobs table holds %d rows after the sweep, want 3", got)
	}
}

// TestRetentionDryRunRemovesNothing asserts that the default posture previews:
// the candidate count is reported and the table is unchanged.
func TestRetentionDryRunRemovesNothing(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := retentionOrg(t, db, "retention-dry")
	insertOldJob(t, db, org, "succeeded", time.Now().Add(-40*24*time.Hour))

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true})
	if !runner.DryRun() {
		t.Fatal("runner with Delete unset is not a dry run")
	}

	results, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	var candidates int64
	for _, res := range results {
		if res.Key == "jobs_finished" {
			candidates = res.Candidates
			if res.Deleted != 0 {
				t.Errorf("dry run deleted %d rows", res.Deleted)
			}
		}
	}
	if candidates != 1 {
		t.Fatalf("dry run reported %d candidates, want 1", candidates)
	}
	if got := countJobs(t, db); got != 1 {
		t.Fatalf("dry run changed the jobs table: %d rows", got)
	}
}

// TestRetentionIsIdempotent runs the sweep twice. The second run must delete
// nothing and must not fail, which is what makes it safe on a timer.
func TestRetentionIsIdempotent(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := retentionOrg(t, db, "retention-idem")
	for i := 0; i < 3; i++ {
		insertOldJob(t, db, org, "succeeded", time.Now().Add(-40*24*time.Hour))
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	first, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if n := deletedFor(first, "jobs_finished"); n != 3 {
		t.Fatalf("first run deleted %d, want 3", n)
	}

	second, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := deletedFor(second, "jobs_finished"); n != 0 {
		t.Fatalf("second run deleted %d rows, want 0 (not idempotent)", n)
	}
	if got := countJobs(t, db); got != 0 {
		t.Fatalf("jobs table holds %d rows, want 0", got)
	}
}

// TestRetentionLeavesProtectedTablesAlone asserts the safety switch: security
// evidence is not swept unless the deployment explicitly allows it. security_events
// is used rather than audit_log because audit_log has its own append-only trigger
// and is covered by TestRetentionRefusesAppendOnlyAuditLog.
func TestRetentionLeavesProtectedTablesAlone(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-protected")
	if _, err := db.Pool().Exec(ctx, `
        INSERT INTO security_events (organization_id, event_type, severity, message, created_at)
		VALUES ($1, 'test.event', 'info', 'test', now() - interval '500 days')`, org); err != nil {
		t.Fatalf("insert security event: %v", err)
	}

	// Not allowed: the protected policy must report itself skipped.
	runner := retention.New(db, audit.New(db), nil, retention.Options{
		Enabled: true, Delete: true, AllowAudit: false,
		Windows: map[string]time.Duration{"security_events": 400 * 24 * time.Hour},
	})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	res := resultFor(results, "security_events")
	if res.Skipped == "" {
		t.Fatal("the security-events policy ran without the allow-audit switch")
	}
	if n := countSecurityEvents(t, db); n != 1 {
		t.Fatalf("security_events holds %d rows, want the row to be preserved", n)
	}

	// Allowed: with the switch and a window, the row is removed.
	allowed := retention.New(db, audit.New(db), nil, retention.Options{
		Enabled: true, Delete: true, AllowAudit: true,
		Windows: map[string]time.Duration{"security_events": 400 * 24 * time.Hour},
	})
	if _, err := allowed.Run(ctx); err != nil {
		t.Fatalf("allowed run: %v", err)
	}
	if n := countSecurityEvents(t, db); n != 0 {
		t.Fatalf("security_events holds %d rows after an allowed sweep, want 0", n)
	}
}

// TestRetentionRefusesAppendOnlyAuditLog asserts that the audit trail is refused
// even with the allow-audit switch, because a database trigger blocks DELETE. The
// policy must report why rather than surfacing a delete error.
func TestRetentionRefusesAppendOnlyAuditLog(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-append-only")
	if _, err := db.Pool().Exec(ctx, `
        INSERT INTO audit_log (organization_id, actor_type, action, entity_type, created_at)
        VALUES ($1, 'system', 'test.action', 'account', now() - interval '400 days')`, org); err != nil {
		t.Fatalf("insert audit row: %v", err)
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{
		Enabled: true, Delete: true, AllowAudit: true,
		Windows: map[string]time.Duration{"audit_log": 30 * 24 * time.Hour},
	})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	res := resultFor(results, "audit_log")
	if res.Skipped == "" {
		t.Fatal("the append-only audit policy was not reported as skipped")
	}
	if res.Error != "" {
		t.Fatalf("the audit policy produced an error instead of a skip: %s", res.Error)
	}
	if n := countAudit(t, db); n != 1 {
		t.Fatalf("audit_log holds %d rows, want the row preserved", n)
	}
}

// TestRetentionRefusesAWindowBelowTheMinimum asserts that a too-short window is
// skipped rather than honoured. A silently shortened window deletes more than the
// operator asked for, which is the failure this guard exists to prevent.
func TestRetentionRefusesAWindowBelowTheMinimum(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := retentionOrg(t, db, "retention-min")
	insertOldJob(t, db, org, "succeeded", time.Now().Add(-2*24*time.Hour))

	runner := retention.New(db, audit.New(db), nil, retention.Options{
		Enabled: true, Delete: true,
		// jobs_finished has a one-day minimum; one hour is below it.
		Windows: map[string]time.Duration{"jobs_finished": time.Hour},
	})
	results, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res := resultFor(results, "jobs_finished"); res.Skipped == "" {
		t.Fatal("a window below the minimum was honoured")
	}
	if got := countJobs(t, db); got != 1 {
		t.Fatalf("jobs table holds %d rows, want the row preserved", got)
	}
}

// TestRetentionZeroWindowIsRefused asserts that an unset window on a policy that
// needs one is read as "not configured", never as "delete everything".
func TestRetentionZeroWindowIsRefused(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := retentionOrg(t, db, "retention-zero")
	insertOldJob(t, db, org, "succeeded", time.Now().Add(-400*24*time.Hour))

	runner := retention.New(db, audit.New(db), nil, retention.Options{
		Enabled: true, Delete: true,
		Windows: map[string]time.Duration{"jobs_finished": 0},
	})
	results, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res := resultFor(results, "jobs_finished"); res.Skipped == "" {
		t.Fatal("a zero window was treated as delete-everything")
	}
	if got := countJobs(t, db); got != 1 {
		t.Fatalf("jobs table holds %d rows, want the row preserved", got)
	}
}

// TestRetentionBatchesLargeDeletes inserts more rows than one batch and checks the
// loop removes them all across several statements.
func TestRetentionBatchesLargeDeletes(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-batch")

	// The jobs policy batches 5000 rows per statement, so more than that
	// forces the delete loop to run more than once.
	const rows = 5200
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO jobs (organization_id, kind, status, finished_at)
		SELECT $1, 'import.execute', 'succeeded', now() - interval '40 days'
		FROM generate_series(1, $2)`, org, rows); err != nil {
		t.Fatalf("insert jobs: %v", err)
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	res := resultFor(results, "jobs_finished")
	if res.Deleted != rows {
		t.Fatalf("deleted %d rows, want %d", res.Deleted, rows)
	}
	if res.Batches < 2 {
		t.Fatalf("deleted in %d batches, want at least 2 (batching did not happen)", res.Batches)
	}
	if got := countJobs(t, db); got != 0 {
		t.Fatalf("jobs table holds %d rows, want 0", got)
	}
}

// TestRetentionIsTenantAgnostic documents that retention is a platform operation,
// not a tenant one: it sweeps every organization's expired operational rows. This
// is the intended behaviour — retention is a storage policy — and the test makes
// it explicit so a future change to scope it per tenant is a deliberate decision.
func TestRetentionIsTenantAgnostic(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	orgA := retentionOrg(t, db, "retention-org-a")
	orgB := retentionOrg(t, db, "retention-org-b")
	insertOldJob(t, db, orgA, "succeeded", time.Now().Add(-40*24*time.Hour))
	insertOldJob(t, db, orgB, "succeeded", time.Now().Add(-40*24*time.Hour))

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := countJobs(t, db); got != 0 {
		t.Fatalf("jobs table holds %d rows, want both organizations swept", got)
	}
}

// TestRetentionRecordsAnAuditEntry asserts the run is attributable: a sweep that
// removed rows writes exactly one audit entry naming the total.
func TestRetentionRecordsAnAuditEntry(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-audit")
	insertOldJob(t, db, org, "succeeded", time.Now().Add(-40*24*time.Hour))

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	if _, err := runner.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	var n int64
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1`, string(audit.ActionRetentionSweep)).Scan(&n); err != nil {
		t.Fatalf("count retention audit entries: %v", err)
	}
	if n != 1 {
		t.Fatalf("retention wrote %d audit entries, want 1", n)
	}

	// A dry run changes nothing, so it must not add a second entry.
	dry := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true})
	if _, err := dry.Run(ctx); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1`, string(audit.ActionRetentionSweep)).Scan(&n); err != nil {
		t.Fatalf("recount: %v", err)
	}
	if n != 1 {
		t.Fatalf("a dry run wrote an audit entry: %d entries", n)
	}
}

// TestRetentionSweepsExpiredAiInsights exercises the expiry-driven policy, whose
// cutoff is "now" rather than a configured window: the row's own expires_at is the
// whole policy.
func TestRetentionSweepsExpiredAiInsights(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrganization(t, db, "retention-insights")
	account := insertAccount(t, db, org, "Insight Co")

	insertInsight := func(expiresAt time.Time) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, `
            INSERT INTO ai_insights
                (organization_id, entity_type, entity_id, kind, context_hash,
                 provider, model, expires_at)
            VALUES ($1, 'account', $2, 'summary', $3, 'test', 'test', $4)`,
			org, account, "hash-"+uuid.NewString(), expiresAt); err != nil {
			t.Fatalf("insert insight: %v", err)
		}
	}
	insertInsight(time.Now().Add(-time.Hour))     // already expired
	insertInsight(time.Now().Add(24 * time.Hour)) // still fresh

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	if _, err := runner.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	var remaining int64
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM ai_insights`).Scan(&remaining); err != nil {
		t.Fatalf("count insights: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("ai_insights holds %d rows, want only the unexpired one", remaining)
	}
}

// --- helpers ---------------------------------------------------------------

func deletedFor(results []retention.Result, key string) int64 {
	for _, r := range results {
		if r.Key == key {
			return r.Deleted
		}
	}
	return -1
}

func resultFor(results []retention.Result, key string) retention.Result {
	for _, r := range results {
		if r.Key == key {
			return r
		}
	}
	return retention.Result{Key: key, Skipped: "policy not present in the registry"}
}

func countAudit(t *testing.T, db *database.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE entity_type = 'account'`).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

func countSecurityEvents(t *testing.T, db *database.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM security_events WHERE event_type = 'test.event'`).Scan(&n); err != nil {
		t.Fatalf("count security events: %v", err)
	}
	return n
}

// TestRetentionSweepsAKeylessTable is the regression guard for a table whose
// primary key is not a single column.
//
// idempotency_keys is keyed on (scope, key) and has no id column at all. The
// delete statement selected "id", so this policy failed with "column id does not
// exist" on every real run — silently, because the failure is per-policy and the
// handler only logs it. The window is well past the 24h minimum, so the policy is
// eligible, and a key inserted long ago must actually be removed while a fresh one
// is kept.
func TestRetentionSweepsAKeylessTable(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `
INSERT INTO idempotency_keys (scope, key, created_at)
VALUES ('test', 'old-key', now() - interval '30 days'),
       ('test', 'fresh-key', now())`); err != nil {
		t.Fatalf("insert idempotency keys: %v", err)
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	res := resultFor(results, "idempotency_keys")
	if res.Error != "" {
		t.Fatalf("idempotency_keys policy failed: %s", res.Error)
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted %d idempotency keys, want 1", res.Deleted)
	}

	var remaining int64
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM idempotency_keys`).Scan(&remaining); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("idempotency_keys holds %d rows, want the fresh key preserved", remaining)
	}
	var kept string
	if err := db.Pool().QueryRow(ctx, `SELECT key FROM idempotency_keys`).Scan(&kept); err != nil {
		t.Fatalf("read kept key: %v", err)
	}
	if kept != "fresh-key" {
		t.Fatalf("kept key is %q, want fresh-key", kept)
	}
}
