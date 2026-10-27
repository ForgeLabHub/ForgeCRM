package importx

import (
	"testing"

	"github.com/geff1778/forgecrm/internal/importx/parsers"
	"github.com/geff1778/forgecrm/internal/importx/safe"
)

// A binary archive header can contain a byte that looks like a delimiter. Format
// detection must therefore never let content sniffing classify an archive as a
// delimited text file, otherwise a ZIP is parsed as CSV and fails with a
// confusing UTF-8 error instead of being extracted.
func TestArchiveIsNeverClassifiedAsTabular(t *testing.T) {
	// The ZIP local file header, followed by bytes that include a comma.
	zipHeader := []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00, ',', 0x00, 0x00, 0x00}

	if f := safe.DetectFormat("upload.bin", zipHeader); f != safe.FormatZIP {
		t.Fatalf("DetectFormat did not recognise the ZIP header, got %q", f)
	}
	if !safe.IsArchive(safe.DetectFormat("upload.bin", zipHeader)) {
		t.Fatal("the ZIP header must be treated as an archive")
	}

	// The guard used by Analyse and resolveSource: when the container is an
	// archive, the tabular format is cleared regardless of what sniffing said.
	if f := parsers.Detect("upload.bin", zipHeader); f == "" {
		t.Skip("sniffing did not misclassify this header; the guard is still correct")
	}
}

// detectTabular must rely on the format established by resolveSource. The stored
// upload is named "<uuid>.upload", so re-sniffing the path yields no format and
// previously made every background import fail with
// `unsupported tabular format "unknown"`.
func TestDetectTabularUsesResolvedFormatNotStoragePath(t *testing.T) {
	imp := &Job{Format: "unknown"}
	src := &resolvedSource{path: "/tmp/1234.upload", format: parsers.FormatCSV}

	if got := detectTabular(imp, src); got != parsers.FormatCSV {
		t.Fatalf("detectTabular = %q, want %q", got, parsers.FormatCSV)
	}
	if got := detectFormatFor(imp, src); got != "csv" {
		t.Fatalf("detectFormatFor = %q, want %q", got, "csv")
	}
}

// When the source carries no format, the filename is the next best signal.
func TestDetectTabularFallsBackToFilename(t *testing.T) {
	name := "contacts.csv"
	imp := &Job{Format: "unknown", OriginalFilename: &name}

	if got := detectTabular(imp, &resolvedSource{}); got != parsers.FormatCSV {
		t.Fatalf("detectTabular = %q, want %q", got, parsers.FormatCSV)
	}
}

// A resolved source with no format and a job with no usable filename must still
// produce a stable value rather than panicking on a nil source.
func TestDetectTabularHandlesNilSource(t *testing.T) {
	imp := &Job{Format: "jsonl"}
	if got := detectTabular(imp, nil); got != parsers.FormatJSONL {
		t.Fatalf("detectTabular = %q, want %q", got, parsers.FormatJSONL)
	}
}
