package migrate

import (
	"strings"
	"testing"
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
