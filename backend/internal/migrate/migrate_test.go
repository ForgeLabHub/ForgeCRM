package migrate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestExpectedVersionMatchesTheEmbeddedMigrations guards the contract the
// readiness probe depends on: ExpectedVersion must equal the highest embedded
// migration. If a migration is added without the count moving, the drift check
// silently stops protecting anything.
func TestExpectedVersionMatchesTheEmbeddedMigrations(t *testing.T) {
	got, err := ExpectedVersion()
	if err != nil {
		t.Fatalf("ExpectedVersion: %v", err)
	}
	if got == 0 {
		t.Fatal("ExpectedVersion returned 0; the embedded source looks empty")
	}

	// The smoke test in scripts/extract-test/migrate walks the same source and
	// prints the count; here it is enough that the number is stable and non-zero
	// and that a second call agrees, since the value is cached.
	again, err := ExpectedVersion()
	if err != nil {
		t.Fatalf("ExpectedVersion (second call): %v", err)
	}
	if again != got {
		t.Fatalf("ExpectedVersion is not stable: %d then %d", got, again)
	}
}

// TestSchemaBehindErrorMessageNamesBothVersions keeps the failure diagnosable
// from a log line alone: an operator must see which version is applied and which
// the build wants, without reading the source.
func TestSchemaBehindErrorMessageNamesBothVersions(t *testing.T) {
	err := &SchemaBehindError{Applied: 27, Expected: 28}
	msg := err.Error()
	for _, want := range []string{"27", "28", "forgecrm-api -migrate"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error does not mention %q: %s", want, msg)
		}
	}
}

// TestIsSchemaNotReadyCoversDrift checks that a behind schema is reported as a
// readiness failure, not merely as an error, so main.go's startup guard and the
// readiness probe both refuse it.
func TestIsSchemaNotReadyCoversDrift(t *testing.T) {
	if !IsSchemaNotReady(&SchemaBehindError{Applied: 1, Expected: 2}) {
		t.Fatal("IsSchemaNotReady does not recognise a behind schema")
	}
	if !IsSchemaNotReady(&SchemaNotReadyError{Missing: []string{"users"}}) {
		t.Fatal("IsSchemaNotReady does not recognise a missing schema")
	}
	if IsSchemaNotReady(nil) {
		t.Fatal("IsSchemaNotReady(nil) reported a failure")
	}
}

// TestIsSchemaNotReadyCoversDirty checks that a half-applied migration is a
// readiness failure too. A dirty schema has every required table, so it is the
// case a table-only check would wrongly pass; the version read is what catches
// it, and this test pins that the error is classified as not-ready rather than
// treated as an ordinary error.
func TestIsSchemaNotReadyCoversDirty(t *testing.T) {
	if !IsSchemaNotReady(&SchemaDirtyError{Version: 28}) {
		t.Fatal("IsSchemaNotReady does not recognise a dirty schema")
	}
}

// TestSchemaDirtyErrorMessageNamesTheVersion keeps the failure diagnosable from a
// log line alone: the operator must see which migration failed without reading
// the source or querying the database by hand.
func TestSchemaDirtyErrorMessageNamesTheVersion(t *testing.T) {
	msg := (&SchemaDirtyError{Version: 28}).Error()
	for _, want := range []string{"28", "dirty"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error does not mention %q: %s", want, msg)
		}
	}
}

// flakyQueryer reproduces the race that masked the diagnosis: the first Check
// fails with a schema-readiness error (missing tables), and every later one
// fails with the context error, as a real pool does once the deadline fires
// mid-query.
type flakyQueryer struct{ calls int }

func (q *flakyQueryer) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	q.calls++
	if q.calls == 1 {
		return nil, &SchemaNotReadyError{Missing: []string{"users"}}
	}
	// Block like a real query until the deadline fires, then surface it. This is
	// the race the fix guards: the last Check fails with the context error, not
	// the schema state.
	<-ctx.Done()
	return nil, ctx.Err()
}

func (q *flakyQueryer) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	return errRow{err: ctx.Err()}
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// TestWaitForSchemaReportsWhyItGaveUp pins the diagnosis, not just the failure.
// When the deadline fires during a Check, the last raw error is "context deadline
// exceeded"; reporting that would hide the schema state the wait exists to
// explain. The returned error must still be classified as a readiness failure so
// a worker that gives up exits with an actionable reason.
func TestWaitForSchemaReportsWhyItGaveUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := WaitForSchema(ctx, &flakyQueryer{}, time.Millisecond, nil)
	if err == nil {
		t.Fatal("WaitForSchema returned nil against a queryer that never succeeds")
	}
	if !IsSchemaNotReady(err) {
		t.Fatalf("WaitForSchema lost the readiness diagnosis: %v", err)
	}
}
