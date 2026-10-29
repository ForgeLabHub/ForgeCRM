// Package retention prunes records past a configured retention window.
//
// The design rules this package exists to hold:
//
//  1. Nothing is deleted without a policy. Every table that is swept is named in
//     the registry below, with the column the window is measured from and the
//     reason the table needs a window at all. A table absent from the registry is
//     never touched.
//
//  2. The policy is off by default. A deployment that sets nothing keeps every
//     row; retention is opt-in, so an upgrade cannot quietly start deleting data.
//
//  3. It previews before it deletes. Dry-run is the default even once retention is
//     enabled, and it reports the same candidate count a real run would remove, so
//     an operator can see the effect before allowing it.
//
//  4. Audit and security records are protected. They are swept only when a
//     deployment explicitly allows it, because deleting the record of who did what
//     is exactly the action an attacker with database access would take.
//
//  5. It is bounded, idempotent and safe to run twice. Each policy deletes in
//     batches with a LIMIT, so a first run over a huge backlog cannot hold one long
//     transaction or bloat the WAL. A re-run deletes only what the previous run's
//     cutoff still matches, and two concurrent runs are harmless because the delete
//     is a set operation on the window, not a cursor.
//
//  6. It reports what it did. Every run returns per-policy counts of candidates
//     and rows removed, and a real run that removed anything writes one audit
//     entry, so a deletion is attributable after the fact.
package retention

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
)

// ErrDisabled is returned when a run is attempted on a disabled runner.
var ErrDisabled = errors.New("retention: not enabled")

// Policy describes one retention window.
//
// Table, TimeColumn and StatusPredicate are code constants, never user input, so
// they are the only identifiers interpolated into SQL. Everything a caller
// supplies — the cutoff and the batch size — is a bound parameter.
type Policy struct {
	// Key is the stable name used in configuration, logs and metrics. It is not
	// the table name, so a policy can be re-pointed at a different table without
	// changing the operator's configuration key.
	Key string
	// Table is the physical table to sweep.
	Table string
	// TimeColumn is the timestamp the window is measured from. It must be an
	// indexed or indexable column; the index is named in the comment beside each
	// policy.
	TimeColumn string
	// StatusPredicate restricts the sweep to rows that are finished. It is empty
	// when every row is eligible. It is code, not configuration, because a status
	// list is a fact about the schema's CHECK constraint.
	StatusPredicate string
	// Default is the window applied when the operator configures nothing for this
	// key.
	Default time.Duration
	// Min is the shortest window an operator may configure. A value below it is
	// refused rather than clamped, because a silently shortened window deletes more
	// than the operator asked for.
	Min time.Duration
	// BatchSize is the number of rows removed per statement.
	BatchSize int
	// ExpiryDriven marks a table whose rows carry their own expiry. The cutoff is
	// then "now", and the window configuration is ignored: the row decides when it
	// is eligible, not the sweep.
	ExpiryDriven bool
	// Protected marks a table that holds audit or security evidence. It is swept
	// only when the deployment explicitly allows it.
	Protected bool
	// AppendOnly marks a table the database itself refuses to delete from. It is a
	// stronger statement than Protected: no configuration can make a DELETE work, so
	// the policy is always reported as skipped with the reason. Retaining such a
	// table has to be done by partition drop or archival, which is a deliberate
	// operator action rather than a sweep.
	AppendOnly bool
	// Reason states why the table needs a window, so a reader can tell a considered
	// policy from an arbitrary one.
	Reason string
}

// policies is the registry. A table that is not here is never swept.
var policies = []Policy{
	{
		Key: "webhook_deliveries_delivered", Table: "webhook_deliveries",
		TimeColumn: "delivered_at", StatusPredicate: "status = 'delivered'",
		Default: 90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 5000,
		Reason: "a successfully delivered webhook is a receipt, not evidence; its " +
			"payload is already on the subscriber's side",
	},
	{
		Key: "webhook_deliveries_dead", Table: "webhook_deliveries",
		TimeColumn: "next_attempt_at", StatusPredicate: "status = 'dead'",
		Default: 90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 5000,
		Reason: "a dead delivery is terminal and is kept only long enough for an " +
			"operator to inspect and replay it",
	},
	{
		Key: "jobs_finished", Table: "jobs", TimeColumn: "finished_at",
		StatusPredicate: "status IN ('succeeded','cancelled')",
		Default:         30 * 24 * time.Hour, Min: 24 * time.Hour, BatchSize: 5000,
		Reason: "a finished job's payload and result are operational history; a " +
			"failed job is kept because it may still be retried or investigated",
	},
	{
		Key: "ai_tasks", Table: "ai_tasks", TimeColumn: "finished_at",
		Default: 90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 5000,
		Reason: "the AI call log is a cost and abuse record with a bounded use; the " +
			"answers themselves live in ai_insights, which expires on its own",
	},
	{
		Key: "ai_insights", Table: "ai_insights", TimeColumn: "expires_at",
		Default: 0, Min: 0, BatchSize: 5000, ExpiryDriven: true,
		Reason: "a cached insight past its own expiry is stale by definition and is " +
			"replaced on the next request, so the row's expiry is the whole policy",
	},
	{
		Key: "app_logs", Table: "app_logs", TimeColumn: "created_at",
		Default: 14 * 24 * time.Hour, Min: 24 * time.Hour, BatchSize: 10000,
		Reason: "application logs are diagnostic and high-volume; they are also " +
			"mirrored to stdout, so the table is a convenience, not the record",
	},
	{
		Key: "integration_runs", Table: "integration_runs", TimeColumn: "finished_at",
		Default: 90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 5000,
		Reason: "a finished integration run is a sync receipt",
	},
	{
		Key: "import_jobs", Table: "import_jobs", TimeColumn: "finished_at",
		StatusPredicate: "status IN ('succeeded','failed','cancelled')",
		Default:         180 * 24 * time.Hour, Min: 30 * 24 * time.Hour, BatchSize: 1000,
		Reason: "an import job carries a report of what was loaded; it is kept " +
			"longer than operational noise because it explains where data came from",
	},
	{
		Key: "export_jobs", Table: "export_jobs", TimeColumn: "finished_at",
		StatusPredicate: "status IN ('succeeded','failed','cancelled')",
		Default:         30 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 1000,
		Reason: "an export is a copy of data that left the system; the row is kept " +
			"only as a record that the export happened",
	},
	{
		Key: "notifications", Table: "notifications", TimeColumn: "created_at",
		StatusPredicate: "read_at IS NOT NULL",
		Default:         90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 5000,
		Reason: "a read notification has been delivered to its user; an unread one " +
			"is still pending and is never swept",
	},
	{
		Key: "outbox_events", Table: "outbox_events", TimeColumn: "occurred_at",
		StatusPredicate: "published_at IS NOT NULL",
		Default:         30 * 24 * time.Hour, Min: 24 * time.Hour, BatchSize: 10000,
		Reason: "a published outbox event has been handed to every subscriber; an " +
			"unpublished one is still pending and is never swept",
	},
	{
		Key: "idempotency_keys", Table: "idempotency_keys", TimeColumn: "created_at",
		Default: 7 * 24 * time.Hour, Min: 24 * time.Hour, BatchSize: 10000,
		Reason: "an idempotency key only needs to outlive the client's retry window",
	},
	{
		Key: "sessions_expired", Table: "sessions", TimeColumn: "expires_at",
		Default: 30 * 24 * time.Hour, Min: 24 * time.Hour, BatchSize: 5000,
		Reason: "an expired session is no longer accepted; it is kept briefly so an " +
			"operator can answer 'was this session active at time T'",
	},
	{
		Key: "workflow_runs", Table: "workflow_runs", TimeColumn: "finished_at",
		StatusPredicate: "status IN ('succeeded','failed','skipped')",
		Default:         90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 1000,
		Reason: "a finished run is the automation's execution history; a running or " +
			"paused run is still in flight and is never swept, and its step records " +
			"and event claims cascade when the run is removed",
	},
	{
		Key: "agent_runs", Table: "agent_runs", TimeColumn: "finished_at",
		StatusPredicate: "status IN ('completed','failed','cancelled')",
		Default:         90 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 1000,
		Reason: "the human-in-the-loop run log records what an agent proposed and what " +
			"a human approved; the approval decisions are also in the append-only audit " +
			"log, so this is the run detail and not the only record of the decision",
	},
	{
		Key: "events_inbox", Table: "events_inbox", TimeColumn: "received_at",
		StatusPredicate: "status IN ('processed','ignored')",
		Default:         30 * 24 * time.Hour, Min: 24 * time.Hour, BatchSize: 5000,
		Reason: "a processed inbound event is a delivery receipt, kept only long enough " +
			"to answer a duplicate-delivery question; the window must exceed the " +
			"sender's retry horizon, and a failed event is kept because it may still " +
			"be reprocessed",
	},

	// --- Intelligence history: stored analysis, not CRM data -----------------
	//
	// These tables hold the platform's own saved computations. Each row is
	// reproducible from its frozen parameters and algorithm version, so removing an
	// old one loses no fact about the CRM. They are given a longer default than the
	// operational logs because a saved scenario or projection is meant to be
	// revisited.
	{
		Key: "simulation_runs", Table: "simulation_runs", TimeColumn: "created_at",
		Default: 180 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 1000,
		Reason: "a stored simulation run is a saved what-if with its frozen baseline, " +
			"reproducible from its parameters; a run a user still wants is their own " +
			"record, so the default window is generous rather than tight",
	},
	{
		Key: "forecast_runs", Table: "forecast_runs", TimeColumn: "created_at",
		Default: 180 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 1000,
		Reason: "a stored forecast run keeps a projection and the history it was " +
			"computed from; it can be recomputed from the parameters and the " +
			"algorithm version, so an old run is disposable",
	},
	{
		Key: "risk_signals", Table: "risk_signals", TimeColumn: "status_changed_at",
		StatusPredicate: "status IN ('resolved','dismissed')",
		Default:         180 * 24 * time.Hour, Min: 7 * 24 * time.Hour, BatchSize: 1000,
		Reason: "a resolved or dismissed signal is a closed finding; a live signal " +
			"(open or acknowledged) is a current condition and is never swept. The " +
			"status_changed_at index restricted to terminal rows exists for this " +
			"sweep, and a condition that still holds is re-detected by the next scan",
	},

	// --- Protected: audit and security evidence -----------------------------
	//
	// These are listed so their window is visible and configurable, but they run
	// only when the deployment sets the allow-audit switch. A retention sweep that
	// quietly removed the audit trail would be indistinguishable from an attacker
	// covering their tracks.
	{
		Key: "audit_log", Table: "audit_log", TimeColumn: "created_at",
		Default: 0, Min: 365 * 24 * time.Hour, BatchSize: 5000,
		Protected: true, AppendOnly: true,
		Reason: "the append-only action trail; a database trigger refuses DELETE, " +
			"so it cannot be swept at all and is listed only to say so",
	},
	{
		Key: "security_events", Table: "security_events", TimeColumn: "created_at",
		Default: 0, Min: 365 * 24 * time.Hour, BatchSize: 5000, Protected: true,
		Reason: "the authentication and security log; protected because it is the " +
			"evidence of who signed in",
	},
	{
		Key: "login_attempts", Table: "login_attempts", TimeColumn: "created_at",
		Default: 0, Min: 90 * 24 * time.Hour, BatchSize: 10000, Protected: true,
		Reason: "per-attempt lockout counters; they age out of usefulness quickly " +
			"but are protected because they are a security signal",
	},
}

// Policies returns a copy of the registry.
func Policies() []Policy {
	out := make([]Policy, len(policies))
	copy(out, policies)
	return out
}

// ValidateWindows refuses a window override whose key is not in the registry.
//
// A mistyped key would otherwise be accepted and silently do nothing: the operator
// believes they shortened a window, the default is used instead, and the mistake
// surfaces only when data they meant to prune is still there — or when data they
// meant to keep is gone. This is checked at startup, where the operator can still
// see the error.
func ValidateWindows(windows map[string]time.Duration) error {
	if len(windows) == 0 {
		return nil
	}
	known := make(map[string]bool, len(policies))
	for _, p := range policies {
		known[p.Key] = true
	}
	for key := range windows {
		if !known[key] {
			return fmt.Errorf("unknown retention policy %q; known keys are %s",
				key, strings.Join(knownKeys(), ", "))
		}
	}
	return nil
}

// knownKeys returns the registry's keys in declaration order.
func knownKeys() []string {
	keys := make([]string, len(policies))
	for i, p := range policies {
		keys[i] = p.Key
	}
	return keys
}

// Result is the outcome for one policy in one run.
type Result struct {
	Key       string        `json:"key"`
	Table     string        `json:"table"`
	Cutoff    time.Time     `json:"cutoff"`
	Window    time.Duration `json:"window"`
	BatchSize int           `json:"batch_size"`
	// Candidates is how many rows matched the window when the policy was measured.
	// In a dry run it is the number that would be removed; in a real run it is the
	// number seen before the deletes, which may differ slightly from Deleted if
	// another writer inserted or removed rows meanwhile.
	Candidates int64 `json:"candidates"`
	// Deleted is the number of rows actually removed. It is always zero in a dry
	// run.
	Deleted int64 `json:"deleted"`
	// Batches is the number of DELETE statements issued.
	Batches int `json:"batches"`
	// Skipped is set when a policy was not run, with the reason.
	Skipped string `json:"skipped,omitempty"`
	// Error is set when the policy failed. A failure in one policy does not stop
	// the others.
	Error string `json:"error,omitempty"`
	// DurationMS is how long the policy took.
	DurationMS int64 `json:"duration_ms"`
}

// Options configures a Runner.
type Options struct {
	Enabled bool
	// Delete authorizes removal. It is phrased as an opt-in rather than an
	// opt-out: a zero-value Options measures and removes nothing, so a caller who
	// forgets to set it previews instead of pruning.
	Delete bool
	// AllowAudit permits the protected policies to run. When false they are
	// reported as skipped rather than removed.
	AllowAudit bool
	// Windows overrides the default window for a policy key.
	Windows map[string]time.Duration
}

// Runner executes retention policies.
type Runner struct {
	db         *database.DB
	audit      *audit.Logger
	log        *slog.Logger
	windows    map[string]time.Duration
	deleteRows bool
	enabled    bool
	allowAudit bool
	// now is indirected so a test can place the cutoff deterministically.
	now func() time.Time
}

// New builds a runner. A nil logger is replaced with a discarding one so the
// subsystem stays optional.
func New(db *database.DB, auditLog *audit.Logger, log *slog.Logger, opts Options) *Runner {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{
		db:         db,
		audit:      auditLog,
		log:        log,
		windows:    opts.Windows,
		deleteRows: opts.Delete,
		enabled:    opts.Enabled,
		allowAudit: opts.AllowAudit,
		now:        time.Now,
	}
}

// Enabled reports whether the runner will do anything. A disabled runner is a
// valid runner: retention is opt-in and the rest of the platform works without it.
func (r *Runner) Enabled() bool { return r != nil && r.enabled }

// DryRun reports whether the runner only measures.
func (r *Runner) DryRun() bool { return r == nil || !r.deleteRows }

// window returns the effective window for a policy.
func (r *Runner) window(p Policy) time.Duration {
	if r.windows != nil {
		if w, ok := r.windows[p.Key]; ok {
			return w
		}
	}
	return p.Default
}

// Run executes every policy and returns one result per policy, in registry order.
//
// It does not stop at the first failure: a table that cannot be swept should not
// prevent the others from being swept, and each failure is reported so an operator
// can see which one needs attention. It returns an error only when the whole run
// could not start; per-policy failures are in the results.
func (r *Runner) Run(ctx context.Context) ([]Result, error) {
	if r == nil || !r.enabled {
		return nil, ErrDisabled
	}
	if r.db == nil {
		return nil, errors.New("retention: no database configured")
	}

	results := make([]Result, 0, len(policies))
	for _, p := range policies {
		results = append(results, r.runPolicy(ctx, p))
	}

	// One audit entry for the run as a whole, written only when something was
	// actually removed. A dry run changes nothing, so recording it would fill the
	// trail with no-ops.
	if r.deleteRows {
		if deleted := totalDeleted(results); deleted > 0 {
			r.record(ctx, results, deleted)
		}
	}
	return results, nil
}

// runPolicy executes one policy.
func (r *Runner) runPolicy(ctx context.Context, p Policy) Result {
	res := Result{Key: p.Key, Table: p.Table, BatchSize: p.BatchSize}
	start := r.now()
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	if p.AppendOnly {
		res.Skipped = "append-only: the table refuses DELETE at the database level; " +
			"retain it by partition drop or archival instead"
		return res
	}
	if p.Protected && !r.allowAudit {
		res.Skipped = "protected: set the allow-audit switch to sweep audit or security evidence"
		return res
	}

	window := r.window(p)
	var cutoff time.Time
	switch {
	case p.ExpiryDriven:
		// The row's own expiry is the policy: everything already expired goes.
		cutoff = r.now()
	case window == 0:
		// A zero window on a policy that needs one means "not configured". It is
		// refused rather than read as "delete everything", which is the dangerous
		// interpretation an unset value must never take.
		res.Skipped = "no window configured"
		return res
	case window < p.Min:
		res.Skipped = fmt.Sprintf("configured window %s is below the minimum %s", window, p.Min)
		return res
	default:
		cutoff = r.now().Add(-window)
	}
	res.Cutoff = cutoff
	res.Window = window

	candidates, err := r.count(ctx, p, cutoff)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Candidates = candidates

	if !r.deleteRows {
		return res
	}

	deleted, batches, err := r.delete(ctx, p, cutoff)
	res.Deleted = deleted
	res.Batches = batches
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

// count measures how many rows match the window. It is what a dry run reports and
// what a real run reports before deleting, so the two are comparable.
func (r *Runner) count(ctx context.Context, p Policy, cutoff time.Time) (int64, error) {
	q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s < $1%s`,
		p.Table, p.TimeColumn, statusClause(p.StatusPredicate))
	var n int64
	if err := r.db.Pool().QueryRow(ctx, q, cutoff).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", p.Key, err)
	}
	return n, nil
}

// delete removes rows in batches until the window is empty.
//
// The subquery selects the physical row identifiers to remove, bounded by LIMIT,
// so one statement never touches more than BatchSize rows. It selects ctid rather
// than a named key because not every swept table has a single-column primary key
// — idempotency_keys is keyed on (scope, key) — and ctid exists on every table, so
// one statement serves every policy. The loop is what makes a first run over a
// large backlog safe: it commits between batches, so it holds no long lock and
// produces a bounded amount of WAL.
func (r *Runner) delete(ctx context.Context, p Policy, cutoff time.Time) (int64, int, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE ctid IN (
        SELECT ctid FROM %s WHERE %s < $1%s LIMIT $2)`,
		p.Table, p.Table, p.TimeColumn, statusClause(p.StatusPredicate))

	var (
		total   int64
		batches int
	)
	for {
		if err := ctx.Err(); err != nil {
			return total, batches, err
		}
		tag, err := r.db.Pool().Exec(ctx, q, cutoff, p.BatchSize)
		if err != nil {
			return total, batches, fmt.Errorf("delete from %s: %w", p.Key, err)
		}
		n := tag.RowsAffected()
		batches++
		total += n
		if n < int64(p.BatchSize) {
			// Fewer than a full batch means the window is now empty. No further
			// statement is needed: the previous one removed everything it saw, and a
			// row inserted into the same window meanwhile is a new row the next run
			// will handle.
			break
		}
	}
	return total, batches, nil
}

// record writes the audit entry for a run that removed rows.
func (r *Runner) record(ctx context.Context, results []Result, deleted int64) {
	if r.audit == nil {
		return
	}
	perPolicy := make(map[string]any, len(results))
	for _, res := range results {
		if res.Deleted > 0 {
			perPolicy[res.Key] = res.Deleted
		}
	}
	_ = r.audit.Record(ctx, audit.Entry{
		ActorType:  "system",
		Action:     string(audit.ActionRetentionSweep),
		EntityType: "retention",
		Metadata: map[string]any{
			"deleted":    deleted,
			"per_policy": perPolicy,
		},
	})
}

func statusClause(predicate string) string {
	if predicate == "" {
		return ""
	}
	return " AND " + predicate
}

func totalDeleted(results []Result) int64 {
	var n int64
	for _, res := range results {
		n += res.Deleted
	}
	return n
}
