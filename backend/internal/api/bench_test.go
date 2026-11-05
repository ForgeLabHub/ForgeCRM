//go:build integration

// Performance benchmarks for the request paths that dominate a deployment.
//
// They are integration-tagged for the same reason the correctness tests are: a
// benchmark that ran against a fake database would measure the fake. The numbers
// below are only meaningful against a real PostgreSQL with the real migrations
// applied — the trigram indexes from 0030, the partial claim index on jobs, the
// organization-scoped composite indexes — because what is being measured is
// whether the planner chooses them.
//
// They are not a gate. Nothing fails when a number moves; the point is to have a
// recorded, reproducible baseline and a way to re-measure it before and after a
// change. The recording lives in docs/performance/baseline.md, and `make test-bench`
// produces it.
//
// Run:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_bench?sslmode=disable \
//	  go test -tags=integration -run '^$' -bench . -benchmem ./internal/api/
//
// The dataset is deliberately large enough that a sequential scan and an index
// scan are distinguishable: the default is 50k contacts, the same order as the
// 200k at which migration 0030 documents its measured improvement.
// FORGECRM_BENCH_CONTACTS scales it, up or down.
//
// Seeding is destructive — it resets the schema — so the benchmark refuses to run
// unless the target database name marks it as disposable (bench, test, scratch or
// ci). That guard is the difference between a measurement and an accident.
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/testdb"
)

// benchScale is the row count seeded into each entity table.
type benchScale struct {
	Accounts int
	Contacts int
	Leads    int
	Deals    int
	Tickets  int
	Tasks    int
}

// defaultBenchScale is the documented baseline scale: 50k contacts, the same
// order of magnitude as the 200k at which migration 0030 measured its indexes,
// so the search numbers here and the index rationale there describe comparable
// data.
//
// The baseline is kept to 50k contacts so that `make test-bench` finishes in a
// couple of minutes on a developer machine; the search plan is chosen from
// statistics, so the index-or-sequential-scan decision at 50k is the same one
// made at 200k.
// Set FORGECRM_BENCH_CONTACTS=200000 to reproduce the migration's own scale.
func defaultBenchScale() benchScale {
	s := benchScale{Accounts: 5_000, Contacts: 50_000, Leads: 20_000, Deals: 20_000, Tickets: 20_000, Tasks: 20_000}
	if v := os.Getenv("FORGECRM_BENCH_CONTACTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.Contacts = n
			// Keep the related tables proportional so a scaled-down run still
			// exercises the joins rather than only the largest table.
			s.Accounts = n / 10
			s.Leads, s.Deals, s.Tickets, s.Tasks = n/4, n/4, n/4, n/4
		}
	}
	return s
}

// benchFixtures is what a benchmark needs from the seeded database.
type benchFixtures struct {
	orgID     uuid.UUID
	actor     *actor
	principal *rbac.Principal
	contactID uuid.UUID
	// selectiveTerm matches exactly one contact. It is the contact's full email
	// address rather than a bare row number: a numeric term shares trigrams with
	// every other numeric email in the table, so the index would return a large
	// candidate set and the measurement would describe the data, not the query.
	selectiveTerm string
	// broadTerm matches nearly every contact, to time the case where the result
	// set must be capped rather than narrowed.
	broadTerm string
}

// newBenchHarness is newHarness with a guard against pointing a destructive seed
// at a database that is not disposable.
//
// newHarness resets the public schema, which is correct for a test database and
// catastrophic for a development or production one. The correctness tests accept
// that risk because FORGECRM_TEST_DATABASE_URL is understood to name a scratch
// database; a benchmark that seeds tens of thousands of rows is easier to run by
// habit, so it checks the name rather than trusting the habit.
func newBenchHarness(b *testing.B) *harness {
	b.Helper()

	url := testdb.URL(b)
	if !disposableDatabase(url) {
		b.Fatalf("refusing to seed %q: the benchmark resets the schema, so the database name "+
			"must mark it disposable (bench, test, scratch or ci)", url)
	}
	return newHarnessWithConfig(b, benchConfig(b))
}

// benchConfig is the default test config with the import limits the harness does
// not otherwise need.
//
// testConfig leaves ImportMaxUploadBytes at zero, which is right for tests that
// never upload and wrong for the one benchmark that does: a zero limit rejects
// every upload before the code under measurement runs.
func benchConfig(tb testing.TB) *config.Config {
	cfg := testConfig(tb)
	cfg.ImportMaxUploadBytes = 256 << 20
	cfg.ImportMaxUnpackedBytes = 2 << 30
	cfg.ImportMaxArchiveFiles = 5_000
	cfg.ImportMaxArchiveDepth = 8
	cfg.ImportMaxRatio = 200
	cfg.ImportBatchSize = 500
	return cfg
}

// disposableDatabase reports whether the database in a connection URL is named as
// a scratch database. The check is on the path segment, not a substring of the
// whole URL, so a host or password containing "test" does not pass it.
func disposableDatabase(url string) bool {
	name := url
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToLower(name)
	for _, marker := range []string{"bench", "test", "scratch", "ci"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// benchSeed creates one organization with a real role, signs in as it through the
// real login endpoint, and bulk-loads the dataset.
//
// The data is inserted with generate_series rather than row by row: a benchmark
// that spent minutes seeding would not be run, and the insertion path is not what
// is under measurement here.
func benchSeed(tb testing.TB, h *harness, scale benchScale) benchFixtures {
	tb.Helper()
	ctx := context.Background()
	pool := h.db.Pool()

	orgID := h.seedOrganization("Bench Org")
	userID := h.seedUser("bench@example.test", "bench-password-1234", "Bench User")
	h.joinOrganization(userID, orgID, "tenant_admin")
	a := h.login("bench@example.test", "bench-password-1234")

	// Accounts. The name and email carry the row number so a search term can
	// select a known number of rows.
	mustExec(tb, pool, ctx, `
		INSERT INTO accounts (organization_id, name, email, industry, created_at, updated_at)
		SELECT $1, 'Account ' || g, 'acct' || g || '@example.test', 'Software',
		       now() - (g || ' seconds')::interval, now() - (g || ' seconds')::interval
		FROM generate_series(1, $2) g`, orgID, scale.Accounts)

	// A bounded set of account ids, assigned to contacts by modulo, so the FK is
	// populated without a per-row lookup.
	accountIDs := queryStrings(tb, pool, ctx,
		`SELECT id::text FROM accounts WHERE organization_id = $1 ORDER BY id LIMIT 200`, orgID)
	if len(accountIDs) == 0 {
		tb.Fatal("no accounts were seeded")
	}

	mustExec(tb, pool, ctx, `
		INSERT INTO contacts (organization_id, account_id, first_name, last_name, email, job_title, created_at, updated_at)
		SELECT $1, accts.aid::uuid, 'First' || g, 'Last' || g, 'person' || g || '@example.test', 'Engineer',
		       now() - (g || ' seconds')::interval, now() - (g || ' seconds')::interval
		FROM generate_series(1, $2) g
		JOIN (SELECT aid, (ord - 1) AS rn FROM unnest($3::text[]) WITH ORDINALITY AS t(aid, ord)) accts
		  ON accts.rn = g % $4`, orgID, scale.Contacts, accountIDs, len(accountIDs))

	mustExec(tb, pool, ctx, `
		INSERT INTO leads (organization_id, title, company, email, status, created_at, updated_at)
		SELECT $1, 'Lead ' || g, 'Company ' || (g % 1000), 'lead' || g || '@example.test', 'new',
		       now() - (g || ' seconds')::interval, now() - (g || ' seconds')::interval
		FROM generate_series(1, $2) g`, orgID, scale.Leads)

	// Deals need a pipeline and a stage; one open stage is enough for the list and
	// search paths, neither of which reads the stage tree.
	var pipelineID, stageID uuid.UUID
	mustScan(tb, pool.QueryRow(ctx,
		`INSERT INTO pipelines (organization_id, name, is_default) VALUES ($1, 'Bench Pipeline', true) RETURNING id`,
		orgID), &pipelineID)
	mustScan(tb, pool.QueryRow(ctx, `
		INSERT INTO pipeline_stages (pipeline_id, organization_id, name, position, probability)
		VALUES ($1, $2, 'Qualify', 0, 20) RETURNING id`, pipelineID, orgID), &stageID)

	mustExec(tb, pool, ctx, `
		INSERT INTO deals (organization_id, pipeline_id, stage_id, account_id, title, amount, status, created_at, updated_at)
		SELECT $1, $2, $3, accts.aid::uuid, 'Deal ' || g, (g % 100000)::numeric, 'open',
		       now() - (g || ' seconds')::interval, now() - (g || ' seconds')::interval
		FROM generate_series(1, $4) g
		JOIN (SELECT aid, (ord - 1) AS rn FROM unnest($5::text[]) WITH ORDINALITY AS t(aid, ord)) accts
		  ON accts.rn = g % $6`,
		orgID, pipelineID, stageID, scale.Deals, accountIDs, len(accountIDs))

	mustExec(tb, pool, ctx, `
		INSERT INTO tickets (organization_id, subject, status, priority, created_at, updated_at)
		SELECT $1, 'Ticket ' || g, 'new', 'normal',
		       now() - (g || ' seconds')::interval, now() - (g || ' seconds')::interval
		FROM generate_series(1, $2) g`, orgID, scale.Tickets)

	mustExec(tb, pool, ctx, `
		INSERT INTO tasks (organization_id, title, status, priority, created_at, updated_at)
		SELECT $1, 'Task ' || g, 'open', 'normal',
		       now() - (g || ' seconds')::interval, now() - (g || ' seconds')::interval
		FROM generate_series(1, $2) g`, orgID, scale.Tasks)

	var contactID uuid.UUID
	mustScan(tb, pool.QueryRow(ctx,
		`SELECT id FROM contacts WHERE organization_id = $1 LIMIT 1`, orgID), &contactID)

	// ANALYZE so the planner has statistics; without it every plan is a guess and
	// the numbers would say more about the seed than about the queries.
	mustExec(tb, pool, ctx, `ANALYZE accounts; ANALYZE contacts; ANALYZE leads; ANALYZE deals; ANALYZE tickets; ANALYZE tasks;`)

	// The service-level calls take a principal directly. tenant_admin holds the
	// read and import permissions these paths need; the HTTP paths resolve their
	// own principal from the session.
	principal := rbac.NewPrincipal(userID, "bench@example.test", "Bench User", orgID, rbac.All())

	// The selective term is one contact's full email, which the email trigram index
	// resolves to a single row. The broad term matches a large fraction of the
	// table — every row number that starts with "1" — so the result set must be
	// capped rather than narrowed.
	selectiveTerm := fmt.Sprintf("person%d@example.test", scale.Contacts)
	broadTerm := "First1"

	return benchFixtures{
		orgID: orgID, actor: a, principal: principal, contactID: contactID,
		selectiveTerm: selectiveTerm, broadTerm: broadTerm,
	}
}

// BenchmarkReadPaths times the list, lookup, search and metrics reads.
//
// They share one seeded database and one signed-in session, because the cost of
// interest is the query and not the setup.
func BenchmarkReadPaths(b *testing.B) {
	h := newBenchHarness(b)
	fx := benchSeed(b, h, defaultBenchScale())

	// A term that matches exactly one contact: the trigram index earns its keep
	// here, and the plan is the one an autocomplete keystroke produces.
	b.Run("contacts_list_search_selective", func(b *testing.B) {
		b.ReportAllocs()
		path := "/api/v1/contacts/?q=" + url.QueryEscape(fx.selectiveTerm) + "&limit=50"
		for i := 0; i < b.N; i++ {
			benchOK(b, h.do(fx.actor, http.MethodGet, path, ""))
		}
	})

	// A term that matches a large fraction of the table: the planner must still use
	// the index and cap the result rather than fall back to a sequential scan.
	b.Run("contacts_list_search_broad", func(b *testing.B) {
		b.ReportAllocs()
		path := "/api/v1/contacts/?q=" + url.QueryEscape(fx.broadTerm) + "&limit=50"
		for i := 0; i < b.N; i++ {
			benchOK(b, h.do(fx.actor, http.MethodGet, path, ""))
		}
	})

	// The unfiltered first page: the common case, served by the organization and
	// created_at index.
	b.Run("contacts_list_first_page", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchOK(b, h.do(fx.actor, http.MethodGet, "/api/v1/contacts/?limit=50", ""))
		}
	})

	b.Run("get_contact", func(b *testing.B) {
		b.ReportAllocs()
		path := "/api/v1/contacts/" + fx.contactID.String()
		for i := 0; i < b.N; i++ {
			benchOK(b, h.do(fx.actor, http.MethodGet, path, ""))
		}
	})

	// The cross-entity search runs one query per readable entity type — six of
	// them — which is the most expensive read in the product.
	b.Run("global_search", func(b *testing.B) {
		b.ReportAllocs()
		path := "/api/v1/search?q=" + url.QueryEscape(fx.selectiveTerm) + "&limit=20"
		for i := 0; i < b.N; i++ {
			benchOK(b, h.do(fx.actor, http.MethodGet, path, ""))
		}
	})

	// The admin metrics endpoint issues a dozen unlabelled count(*) queries in one
	// request; it is the read most sensitive to table size.
	b.Run("admin_metrics", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchOK(b, h.do(fx.actor, http.MethodGet, "/api/v1/admin/metrics", ""))
		}
	})
}

// BenchmarkWritePaths times the mutation path and the outbox write behind it.
func BenchmarkWritePaths(b *testing.B) {
	h := newBenchHarness(b)
	fx := benchSeed(b, h, benchScale{Accounts: 100, Contacts: 1_000, Leads: 100, Deals: 100, Tickets: 100, Tasks: 100})
	ctx := context.Background()

	// A contact create writes three rows in one transaction: the contact, the
	// outbox event and the audit entry. This is the atomic-write path every CRM
	// mutation shares.
	b.Run("create_contact", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			body := fmt.Sprintf(`{"first_name":"Bench","last_name":"Created","email":"created-%d@example.test"}`, i)
			rec := h.do(fx.actor, http.MethodPost, "/api/v1/contacts/", body)
			if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
				b.Fatalf("create contact: status %d, body %s", rec.Code, rec.Body.String())
			}
		}
	})

	// The outbox insert on its own, without the domain write, isolates the cost
	// the transactional outbox adds to every mutation.
	b.Run("outbox_emit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := h.svcs.Outbox.Emit(ctx, &fx.orgID, outbox.EventContactCreated,
				map[string]any{"contact_id": uuid.New()}); err != nil {
				b.Fatalf("emit: %v", err)
			}
		}
	})
}

// BenchmarkBackgroundWork times the worker-side paths: the outbox drain, the job
// queue, and a full CSV import.
func BenchmarkBackgroundWork(b *testing.B) {
	h := newBenchHarness(b)
	fx := benchSeed(b, h, benchScale{Accounts: 200, Contacts: 2_000, Leads: 500, Deals: 500, Tickets: 500, Tasks: 500})
	ctx := context.Background()

	// One drain cycle: claim a batch of pending events and mark each published.
	// This is what a worker does on every poll.
	b.Run("outbox_claim_and_publish", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 50; j++ {
				if err := h.svcs.Outbox.Emit(ctx, &fx.orgID, outbox.EventContactCreated,
					map[string]any{"n": j}); err != nil {
					b.Fatalf("emit: %v", err)
				}
			}
			events, err := h.svcs.Outbox.ClaimPending(ctx, "bench-worker", 50)
			if err != nil {
				b.Fatalf("claim: %v", err)
			}
			for _, e := range events {
				if err := h.svcs.Outbox.MarkPublished(ctx, e.ID); err != nil {
					b.Fatalf("publish: %v", err)
				}
			}
		}
	})

	// The full queue round trip: enqueue, claim with FOR UPDATE SKIP LOCKED,
	// complete. The claim is the contended path when several workers poll.
	b.Run("jobs_enqueue_claim_complete", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			enqueued, err := h.svcs.Jobs.Enqueue(ctx, jobs.EnqueueRequest{
				OrganizationID: &fx.orgID,
				Kind:           jobs.KindRetentionSweep,
				Payload:        map[string]any{"n": i},
			})
			if err != nil {
				b.Fatalf("enqueue: %v", err)
			}
			claimed, err := h.svcs.Jobs.Claim(ctx, "bench-worker")
			if err != nil {
				b.Fatalf("claim: %v", err)
			}
			if claimed == nil {
				b.Fatal("claim returned no job although one was just enqueued")
			}
			if err := h.svcs.Jobs.Complete(ctx, enqueued.ID, map[string]any{"ok": true}); err != nil {
				b.Fatalf("complete: %v", err)
			}
		}
	})

	// A full import: store the upload, analyse it, queue it and run it through the
	// worker path. This is the heaviest single operation in the product.
	b.Run("import_csv_execute", func(b *testing.B) {
		b.ReportAllocs()
		const rows = 5_000
		csv := benchContactsCSV(rows)
		mapping := []importx.MappingEntry{
			{SourceColumn: "first_name", TargetField: "first_name"},
			{SourceColumn: "last_name", TargetField: "last_name"},
			{SourceColumn: "email", TargetField: "email"},
		}
		header := true
		opts := importx.Options{Duplicates: importx.DuplicatesSkip, HasHeader: &header}

		for i := 0; i < b.N; i++ {
			job, err := h.svcs.Imports.Create(ctx, fx.principal, importx.CreateRequest{
				Name:             fmt.Sprintf("bench import %d", i),
				EntityType:       "contact",
				SourceType:       "file",
				OriginalFilename: "bench.csv",
				Content:          strings.NewReader(csv),
			})
			if err != nil {
				b.Fatalf("create import: %v", err)
			}
			if _, err := h.svcs.Imports.Analyze(ctx, fx.principal, job.ID, opts); err != nil {
				b.Fatalf("analyze import: %v", err)
			}
			queued, err := h.svcs.Imports.Execute(ctx, fx.principal, job.ID, mapping, opts)
			if err != nil {
				b.Fatalf("execute import: %v", err)
			}
			if err := h.svcs.Imports.RunJob(ctx, h.svcs.Jobs, queued); err != nil {
				b.Fatalf("run import: %v", err)
			}
		}
	})
}

// benchContactsCSV builds a header plus rows of contact data.
func benchContactsCSV(rows int) string {
	var sb strings.Builder
	sb.WriteString("first_name,last_name,email\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&sb, "Imported%d,Bench%d,imported-%d@example.test\n", i, i, i)
	}
	return sb.String()
}

// benchOK fails the benchmark when a request did not answer 200.
func benchOK(b *testing.B, rec *httptest.ResponseRecorder) {
	b.Helper()
	if rec.Code != http.StatusOK {
		b.Fatalf("unexpected status %d, body %s", rec.Code, rec.Body.String())
	}
}

// --- seeding helpers --------------------------------------------------------

func mustExec(tb testing.TB, pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) {
	tb.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		tb.Fatalf("seed statement: %v", err)
	}
}

func mustScan(tb testing.TB, row pgx.Row, dest ...any) {
	tb.Helper()
	if err := row.Scan(dest...); err != nil {
		tb.Fatalf("seed query: %v", err)
	}
}

func queryStrings(tb testing.TB, pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) []string {
	tb.Helper()
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		tb.Fatalf("seed query: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			tb.Fatalf("seed scan: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		tb.Fatalf("seed iterate: %v", err)
	}
	return out
}
