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

// TestRetentionSweepsOnlyInactiveMcpCredentials pins the MCP credential policy.
//
// The sweep must remove only rows that can never authenticate again — a revoked
// credential or one whose expiry is well past — and must leave a live credential
// and a not-yet-expired one alone. A policy that ignored status would delete the
// live row, silently disconnecting a working client.
func TestRetentionSweepsOnlyInactiveMcpCredentials(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := uuid.MustParse(retentionOrg(t, db, "retention-mcp-credentials"))
	userID := insertUser(t, db, org, "mcp-retention@example.com")

	old := time.Now().Add(-365 * 24 * time.Hour)
	insert := func(name string, expiresAt, revokedAt *time.Time) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, `
            INSERT INTO mcp_credentials
                (organization_id, owner_user_id, name, token_prefix, token_hash, permissions, expires_at, revoked_at, created_at)
            VALUES ($1, $2, $3, 'fcmcp_test', $4, ARRAY['accounts.read'], $5, $6, $7)`,
			org, userID, name, []byte(uuid.NewString()), expiresAt, revokedAt, old); err != nil {
			t.Fatalf("insert credential %s: %v", name, err)
		}
	}
	revoked := old
	insert("revoked", nil, &revoked)
	expired := old
	insert("expired", &expired, nil)
	insert("live", nil, nil)
	future := time.Now().Add(24 * time.Hour)
	insert("live-but-expires-soon", &future, nil)

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	if _, err := runner.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	var remaining int64
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM mcp_credentials`).Scan(&remaining); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("mcp_credentials holds %d rows, want the live credential and the unexpired one", remaining)
	}
}

// TestRetentionSweepsRunHistoryAndInboundEvents covers the three policies added
// for tables that grow without bound: workflow runs, agent runs and the inbound
// event inbox.
//
// Each one is swept by terminal status only. The test inserts an old terminal row
// and an old in-flight row of the same age, so a policy that ignored status would
// delete both and the assertion that the in-flight row survives would fail. That
// is the property that matters: a paused workflow run can still resume and a
// received event has not been handled yet, so neither may be swept however old.
func TestRetentionSweepsRunHistoryAndInboundEvents(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-run-history")
	userID := insertUser(t, db, uuid.MustParse(org), "run-history@example.com")

	// A workflow to hang the runs off. created_by is NOT NULL, so it needs a user.
	var workflowID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO workflows (organization_id, name, definition, trigger_type, created_by)
		VALUES ($1, 'retention fixture', '{}'::jsonb, 'manual', $2) RETURNING id`,
		org, userID).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}

	old := time.Now().Add(-200 * 24 * time.Hour)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO workflow_runs (workflow_id, organization_id, trigger_type, status, finished_at)
		VALUES ($1, $2, 'manual', 'succeeded', $3),
		       ($1, $2, 'manual', 'paused',    NULL)`,
		workflowID, org, old); err != nil {
		t.Fatalf("insert workflow runs: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO agent_runs (organization_id, user_id, goal, status, finished_at)
		VALUES ($1, $2, 'old finished run', 'completed', $3),
		       ($1, $2, 'old live run',     'running',   NULL)`,
		org, userID, old); err != nil {
		t.Fatalf("insert agent runs: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO events_inbox (organization_id, event_id, event_type, source, payload, status, received_at)
		VALUES ($1, 'evt-old-processed', 'crm.deal.created', 'test', '{}'::jsonb, 'processed', $2),
		       ($1, 'evt-old-received',  'crm.deal.created', 'test', '{}'::jsonb, 'received',  $2)`,
		org, old); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, key := range []string{"workflow_runs", "agent_runs", "events_inbox"} {
		res := resultFor(results, key)
		if res.Error != "" {
			t.Fatalf("%s policy failed: %s", key, res.Error)
		}
		if res.Deleted != 1 {
			t.Errorf("%s deleted %d rows, want exactly the one terminal row", key, res.Deleted)
		}
	}

	count := func(table string) int64 {
		t.Helper()
		var n int64
		if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	if n := count("workflow_runs"); n != 1 {
		t.Errorf("workflow_runs holds %d rows, want the paused run preserved", n)
	}
	if n := count("agent_runs"); n != 1 {
		t.Errorf("agent_runs holds %d rows, want the running run preserved", n)
	}
	if n := count("events_inbox"); n != 1 {
		t.Errorf("events_inbox holds %d rows, want the received event preserved", n)
	}

	var status string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM workflow_runs`).Scan(&status); err != nil {
		t.Fatalf("read kept run: %v", err)
	}
	if status != "paused" {
		t.Errorf("kept workflow run is %q, want paused", status)
	}
}

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

// TestRetentionSweepsIntelligenceHistory covers the three policies added for the
// tables the Forecasting & Risk Lab and the Simulation Lab write. Each grows with
// use and none was pruned before.
//
// The status-bearing policy is the one that can go wrong: risk_signals keeps a
// live signal (open or acknowledged) as a current condition, so only a terminal
// signal may be swept. The test inserts an old terminal signal and an old live one
// of the same age, so a policy that ignored status would delete both and the
// assertion that the live signal survives would fail. The two run tables have no
// status — every stored run is finished when written — so they are swept purely by
// age, and a fresh run of each must be kept.
func TestRetentionSweepsIntelligenceHistory(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-intelligence")
	old := time.Now().Add(-400 * 24 * time.Hour)

	if _, err := db.Pool().Exec(ctx, `
INSERT INTO simulation_runs (organization_id, kind, algorithm_version, created_at)
VALUES ($1, 'funnel', 'simulate-v1', $2),
       ($1, 'funnel', 'simulate-v1', now())`, org, old); err != nil {
		t.Fatalf("insert simulation runs: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, `
INSERT INTO forecast_runs (organization_id, method, algorithm_version, horizon, created_at)
VALUES ($1, 'mean', 'forecast-v1', 3, $2),
       ($1, 'mean', 'forecast-v1', 3, now())`, org, old); err != nil {
		t.Fatalf("insert forecast runs: %v", err)
	}

	// rule/entity columns are NOT NULL, and the live-signal unique index needs the
	// two live rows to differ in rule so both may exist.
	if _, err := db.Pool().Exec(ctx, `
INSERT INTO risk_signals
    (organization_id, type, severity, rule, algorithm_version, entity_type, entity_id,
     rationale, status, status_changed_at)
VALUES
    ($1, 'stalled_deal', 'warn', 'stalled_deal', 'risk-v1', 'deal', 'd-old-resolved',
     'resolved long ago', 'resolved', $2),
    ($1, 'stalled_deal', 'warn', 'stalled_deal', 'risk-v1', 'deal', 'd-old-open',
     'still open', 'open', $2),
    ($1, 'overdue_task', 'warn', 'overdue_task', 'risk-v1', 'task', 't-old-ack',
     'acknowledged long ago', 'acknowledged', $2)`,
		org, old); err != nil {
		t.Fatalf("insert risk signals: %v", err)
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, key := range []string{"simulation_runs", "forecast_runs", "risk_signals"} {
		res := resultFor(results, key)
		if res.Error != "" {
			t.Fatalf("%s policy failed: %s", key, res.Error)
		}
		if res.Deleted != 1 {
			t.Errorf("%s deleted %d rows, want exactly the one eligible row", key, res.Deleted)
		}
	}

	count := func(table string) int64 {
		t.Helper()
		var n int64
		if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	if n := count("simulation_runs"); n != 1 {
		t.Errorf("simulation_runs holds %d rows, want the fresh run preserved", n)
	}
	if n := count("forecast_runs"); n != 1 {
		t.Errorf("forecast_runs holds %d rows, want the fresh run preserved", n)
	}
	if n := count("risk_signals"); n != 2 {
		t.Errorf("risk_signals holds %d rows, want the live signals preserved", n)
	}

	// The kept signals must be exactly the live ones, so a policy that swept by age
	// alone (ignoring status) is caught even if the count happened to match.
	rows, err := db.Pool().Query(ctx, `SELECT status FROM risk_signals ORDER BY status`)
	if err != nil {
		t.Fatalf("read kept signals: %v", err)
	}
	defer rows.Close()
	var kept []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan status: %v", err)
		}
		kept = append(kept, s)
	}
	if len(kept) != 2 || kept[0] != "acknowledged" || kept[1] != "open" {
		t.Errorf("kept signal statuses = %v, want [acknowledged open]", kept)
	}
}

// TestRetentionSweepsTransientAuthAndDeliveryRows covers the three policies added
// for rows that accumulate by one per user action: an expired OIDC sign-in state,
// an expired password-reset token and a settled email log entry.
//
// Each policy is swept on the row's own terminal condition, so an in-flight row of
// the same age must survive: a login state that has not yet expired (or that was
// already consumed within its window), a reset token that is still redeemable and a
// queued email that has not been sent. A policy that swept by age alone would take
// the live row too, and the assertions below would fail.
func TestRetentionSweepsTransientAuthAndDeliveryRows(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := retentionOrg(t, db, "retention-transient")
	orgID := uuid.MustParse(org)
	userID := insertUser(t, db, orgID, "transient@example.com")

	now := time.Now()

	// A provider to hang the login states off.
	var providerID uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO oidc_providers (key, display_name, issuer, client_id)
		VALUES ('retention', 'Retention Test', 'https://issuer.example', 'client')
		RETURNING id`).Scan(&providerID); err != nil {
		t.Fatalf("insert oidc provider: %v", err)
	}

	// One expired state and one still within its window. The sweep is expiry-driven,
	// so the unexpired one must survive.
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO oidc_login_states (state, provider_id, nonce, code_verifier, expires_at)
		VALUES ('expired-state', $1, 'n1', 'v1', $2),
		       ('live-state',    $1, 'n2', 'v2', $3)`,
		providerID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert login states: %v", err)
	}

	// One reset token expired well beyond the 7-day window and one still valid. The
	// window is measured from expires_at (the same convention as sessions_expired), so
	// a token that lapsed an hour ago is still inside the window and must survive. The
	// token hash is a bytea with a UNIQUE constraint, so the two fixtures need
	// distinct bytes.
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, '\x01'::bytea, $2),
	       ($1, '\x02'::bytea, $3)`,
		userID, now.Add(-10*24*time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert reset tokens: %v", err)
	}

	// One settled email (sent) and one still queued, both older than the window. Only
	// the settled one may be swept.
	old := now.Add(-200 * 24 * time.Hour)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO email_log (organization_id, to_address, subject, status, created_at)
		VALUES ($1, 'sent@example.com',    'settled', 'sent',    $2),
	       ($1, 'queued@example.com', 'pending', 'queued', $2)`,
		orgID, old); err != nil {
		t.Fatalf("insert email log: %v", err)
	}

	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	results, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, key := range []string{
		"oidc_login_states_expired",
		"password_reset_tokens_expired",
		"email_log_settled",
	} {
		res := resultFor(results, key)
		if res.Error != "" {
			t.Fatalf("%s policy failed: %s", key, res.Error)
		}
		if res.Deleted != 1 {
			t.Errorf("%s deleted %d rows, want exactly the one eligible row", key, res.Deleted)
		}
	}

	count := func(table string) int64 {
		t.Helper()
		var n int64
		if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	if n := count("oidc_login_states"); n != 1 {
		t.Errorf("oidc_login_states holds %d rows, want the unexpired state preserved", n)
	}
	if n := count("password_reset_tokens"); n != 1 {
		t.Errorf("password_reset_tokens holds %d rows, want the still-valid token preserved", n)
	}
	if n := count("email_log"); n != 1 {
		t.Errorf("email_log holds %d rows, want the queued email preserved", n)
	}

	// The kept rows must be exactly the live ones, so a sweep that ignored the
	// terminal condition cannot pass by matching a count.
	var state string
	if err := db.Pool().QueryRow(ctx, `SELECT state FROM oidc_login_states`).Scan(&state); err != nil {
		t.Fatalf("read kept state: %v", err)
	}
	if state != "live-state" {
		t.Errorf("kept login state is %q, want live-state", state)
	}
	var status string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM email_log`).Scan(&status); err != nil {
		t.Fatalf("read kept email: %v", err)
	}
	if status != "queued" {
		t.Errorf("kept email status is %q, want queued", status)
	}
}

// TestRetentionSweepIndexesExist pins the indexes the sweep depends on. A sweep
// filters on the time column alone (plus a status predicate where one exists) and
// no organization, so it needs an index that leads with that column. The per-org
// list indexes lead with organization_id and cannot serve it, so each sweep table
// gets a dedicated one — migrations 0015, 0017 and 0021. This asserts they are
// present after migrations run, so a future migration that drops or renames one is
// caught rather than silently turning the sweep into a table scan.
func TestRetentionSweepIndexesExist(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	want := []string{
		// 0015: the operational tables.
		"webhook_deliveries_delivered_at_idx",
		"jobs_finished_at_idx",
		"ai_tasks_finished_at_idx",
		"integration_runs_finished_at_idx",
		// 0017: the run history.
		"workflow_runs_finished_at_idx",
		"agent_runs_finished_at_idx",
		"events_inbox_processed_idx",
		// 0020: the terminal risk signals.
		"risk_signals_terminal_idx",
		// 0021: the intelligence run history.
		"simulation_runs_created_at_idx",
		"forecast_runs_created_at_idx",
		// 0024: the transient auth and delivery rows.
		"password_reset_tokens_expires_idx",
		"email_log_settled_idx",
		// 0025: the MCP client credentials.
		"mcp_credentials_created_at_idx",
	}
	for _, name := range want {
		var exists bool
		if err := db.Pool().QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`, name).Scan(&exists); err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		if !exists {
			t.Errorf("retention sweep index %s is missing; the sweep for its table would scan the whole table", name)
		}
	}
}
