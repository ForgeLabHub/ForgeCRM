// Command smoke exercises the tabular-parser public API in a clean module.
//
// It parses a CSV and a JSON export and confirms the column set, the inferred
// types and the row-level error reporting all work outside the monolith. If the
// package needed anything from ForgeCRM, this program would not compile or run.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geff1778/forgecrm/internal/importx/parsers"
)

func main() {
	dir, err := os.MkdirTemp("", "parsers-smoke-")
	if err != nil {
		fail("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// A CSV with a duplicate header, a blank header and a short row.
	csv := filepath.Join(dir, "contacts.csv")
	if err := os.WriteFile(csv, []byte("name,email,,email\nAda,a@x.test,note,b@x.test\nBob,b@x.test\n"), 0o600); err != nil {
		fail("write csv: %v", err)
	}
	if got := parsers.Detect("contacts.csv", nil); got != parsers.FormatCSV {
		fail("Detect = %q, want csv", got)
	}
	res, err := parsers.Parse(csv, parsers.FormatCSV, parsers.Options{})
	if err != nil {
		fail("parse csv: %v", err)
	}
	if len(res.Columns) != 4 || res.Columns[2] != "column_3" || res.Columns[3] != "email_2" {
		fail("columns not normalised: %v", res.Columns)
	}
	if len(res.Errors) != 1 || res.Errors[0].Code != "column_count_mismatch" {
		fail("row error not reported: %+v", res.Errors)
	}
	fmt.Printf("csv columns=%v rows=%d errors=%d\n", res.Columns, len(res.Rows), len(res.Errors))

	// A JSON export whose records are nested inside an envelope.
	jsonPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(jsonPath, []byte(`{"items":[{"name":"Ada","address":{"city":"London"}}]}`), 0o600); err != nil {
		fail("write json: %v", err)
	}
	jres, err := parsers.Parse(jsonPath, parsers.FormatJSON, parsers.Options{})
	if err != nil {
		fail("parse json: %v", err)
	}
	if len(jres.Rows) != 1 || jres.Rows[0]["address.city"] != "London" {
		fail("json not flattened: %+v", jres.Rows)
	}
	fmt.Printf("json rows=%d nested=%v\n", len(jres.Rows), jres.Rows[0]["address.city"])

	// The shared value helpers behave as the mapper expects.
	if v, ok := parsers.ParseNumber("$1,234.50"); !ok || v != 1234.5 {
		fail("ParseNumber = %v, %v", v, ok)
	}
	fmt.Printf("number=%v\n", 1234.5)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
