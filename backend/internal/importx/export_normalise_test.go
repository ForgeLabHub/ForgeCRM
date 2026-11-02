package importx

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A PostgreSQL uuid column reaches the export callback as [16]byte, because that
// is what pgx scans it into. Rendering it with fmt.Sprint produced a bracketed
// byte list in the CSV and JSONL bodies, which an importer cannot read back.
func TestNormaliseForExportRendersUUIDsAsCanonicalStrings(t *testing.T) {
	id := uuid.MustParse("421a825e-aed2-4ba0-95ed-0fe339720030")
	want := "421a825e-aed2-4ba0-95ed-0fe339720030"

	if got := normaliseForExport([16]byte(id)); got != want {
		t.Fatalf("[16]byte uuid: got %v, want %q", got, want)
	}
	if got := normaliseForExport(id); got != want {
		t.Fatalf("uuid.UUID: got %v, want %q", got, want)
	}
}

func TestNormaliseForExportKeepsOtherTypesReadable(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := normaliseForExport(ts); got != "2026-01-02T03:04:05Z" {
		t.Fatalf("time: got %v", got)
	}
	if got := normaliseForExport([]byte("raw")); got != "raw" {
		t.Fatalf("[]byte: got %v", got)
	}
	if got := normaliseForExport(nil); got != nil {
		t.Fatalf("nil: got %v", got)
	}
}
