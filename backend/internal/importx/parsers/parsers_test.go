package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// writeFile writes content to a temp file with the given name and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// --- Detect -----------------------------------------------------------------

// TestDetectUsesExtensionThenContent pins the precedence rule: a known extension
// decides, and only an unknown one falls back to sniffing the leading bytes.
// A mis-detected format would route a file to the wrong parser, so the two paths
// are asserted separately.
func TestDetectUsesExtensionThenContent(t *testing.T) {
	cases := []struct {
		name string
		file string
		head string
		want Format
	}{
		{"csv extension", "data.csv", "", FormatCSV},
		{"tsv extension", "data.tsv", "", FormatTSV},
		{"tab extension", "data.tab", "", FormatTSV},
		{"jsonl extension", "data.jsonl", "", FormatJSONL},
		{"ndjson extension", "data.ndjson", "", FormatJSONL},
		{"json extension", "data.json", "", FormatJSON},
		{"xlsx extension", "data.xlsx", "", FormatXLSX},
		{"xlsm extension", "data.xlsm", "", FormatXLSX},
		{"xml extension", "data.xml", "", FormatXML},
		{"uppercase extension", "DATA.CSV", "", FormatCSV},
		{"extensionless json array", "export", "[{\"a\":1}]", FormatJSON},
		{"extensionless json object", "export", "{\"a\":1}", FormatJSONL},
		{"extensionless xml", "export", "<?xml version=\"1.0\"?>", FormatXML},
		{"extensionless tsv", "export", "a\tb\n1\t2\n", FormatTSV},
		{"extensionless csv", "export", "a,b\n1,2\n", FormatCSV},
		{"unknown", "export", "just words", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.file, []byte(tc.head))
			if got != tc.want {
				t.Fatalf("Detect(%q, %q) = %q, want %q", tc.file, tc.head, got, tc.want)
			}
		})
	}
}

// --- CSV --------------------------------------------------------------------

// TestParseCSVNormalisesHeader pins the header rules the mapping step relies on:
// blank names get a positional fallback, duplicates are made unique, and a UTF-8
// BOM is stripped. If any of these changed, a mapping saved against one export
// would silently target a different column on the next.
func TestParseCSVNormalisesHeader(t *testing.T) {
	// The leading BOM must be removed, the blank header replaced, and the two
	// "email" columns disambiguated.
	path := writeFile(t, "contacts.csv", "\ufeffname,email,,email\nAda,a@x.test,note,b@x.test\n")

	res, err := Parse(path, FormatCSV, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"name", "email", "column_3", "email_2"}
	if len(res.Columns) != len(want) {
		t.Fatalf("columns = %v, want %v", res.Columns, want)
	}
	for i := range want {
		if res.Columns[i] != want[i] {
			t.Fatalf("column %d = %q, want %q (full: %v)", i, res.Columns[i], want[i], res.Columns)
		}
	}
	if res.Rows[0]["email"] != "a@x.test" || res.Rows[0]["email_2"] != "b@x.test" {
		t.Fatalf("row = %v", res.Rows[0])
	}
}

// TestParseCSVRecordsMismatchedRowsAndKeepsGoing pins that one bad row does not
// discard the file: the row is recorded as an error and the rest still import.
func TestParseCSVRecordsMismatchedRowsAndKeepsGoing(t *testing.T) {
	path := writeFile(t, "data.csv", "a,b,c\n1,2,3\n4,5\n6,7,8,9\n")

	res, err := Parse(path, FormatCSV, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("errors = %+v, want 2 column-count mismatches", res.Errors)
	}
	for _, e := range res.Errors {
		if e.Code != "column_count_mismatch" {
			t.Fatalf("error code = %q, want column_count_mismatch", e.Code)
		}
	}
	// The short row is padded and the long row is truncated, so both survive.
	if len(res.Rows) != 3 {
		t.Fatalf("rows = %d, want 3 (a bad row is still importable)", len(res.Rows))
	}
	if res.Rows[1]["c"] != "" {
		t.Fatalf("short row was not padded: %v", res.Rows[1])
	}
}

// TestParseCSVHonoursMaxRowsAndTruncation pins that the limit stops reading and
// says so, so the wizard can warn that a file was cut short.
func TestParseCSVHonoursMaxRowsAndTruncation(t *testing.T) {
	path := writeFile(t, "data.csv", "a\n1\n2\n3\n4\n")

	res, err := Parse(path, FormatCSV, Options{MaxRows: 2})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 || !res.Truncated {
		t.Fatalf("rows = %d, truncated = %v; want 2 and true", len(res.Rows), res.Truncated)
	}
}

// TestParseCSVWithoutHeaderSynthesisesColumns pins the headerless path, where the
// column names must be positional rather than the first data row.
func TestParseCSVWithoutHeaderSynthesisesColumns(t *testing.T) {
	path := writeFile(t, "data.csv", "Ada,a@x.test\nBob,b@x.test\n")
	no := false

	res, err := Parse(path, FormatCSV, Options{HasHeader: &no})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "column_1" || res.Columns[1] != "column_2" {
		t.Fatalf("columns = %v, want column_1/column_2", res.Columns)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (the first row is data, not a header)", len(res.Rows))
	}
}

// TestParseCSVDelimiterOverride pins that a caller can force the separator, which
// is what a semicolon-separated European export needs.
func TestParseCSVDelimiterOverride(t *testing.T) {
	path := writeFile(t, "data.csv", "a;b\n1;2\n")

	res, err := Parse(path, FormatCSV, Options{Delimiter: ';'})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "a" || res.Columns[1] != "b" {
		t.Fatalf("columns = %v, want [a b]", res.Columns)
	}
}

// --- Type inference ---------------------------------------------------------

// TestParseInfersColumnTypes pins the inferred type per column, which the wizard
// shows and the mapper uses to pre-select fields.
func TestParseInfersColumnTypes(t *testing.T) {
	// The amount is quoted because it holds a thousands separator, which exercises
	// the tolerant number parser rather than the CSV reader.
	path := writeFile(t, "typed.csv",
		"n,amount,flag,when,who,blank\n"+
			"1,\"$1,234.50\",yes,2024-01-02,ada@example.test,\n"+
			"2,10,no,2024-03-04,bob@example.test,\n")

	res, err := Parse(path, FormatCSV, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"n": "integer", "amount": "number", "flag": "boolean",
		"when": "date", "who": "email", "blank": "empty",
	}
	for col, wantType := range want {
		if got := res.Types[col]; got != wantType {
			t.Fatalf("type of %q = %q, want %q (all: %v)", col, got, wantType, res.Types)
		}
	}
}

// --- JSON / JSONL -----------------------------------------------------------

// TestParseJSONUnwrapsEnvelopeAndFlattens pins the two conveniences the API
// exports rely on: an object wrapper is unwrapped, and a nested object becomes a
// dotted column.
func TestParseJSONUnwrapsEnvelopeAndFlattens(t *testing.T) {
	path := writeFile(t, "data.json",
		`{"data":[{"name":"Ada","address":{"city":"London"}},{"name":"Bob","address":{"city":"Paris"}}]}`)

	res, err := Parse(path, FormatJSON, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if res.Rows[0]["address.city"] != "London" {
		t.Fatalf("nested object not flattened: %v", res.Rows[0])
	}
}

// TestParseJSONRejectsObjectWithoutArray pins the failure mode: an object with no
// record array is refused with a message that names the accepted keys, rather
// than silently producing zero rows.
func TestParseJSONRejectsObjectWithoutArray(t *testing.T) {
	path := writeFile(t, "data.json", `{"meta":{"count":0}}`)
	if _, err := Parse(path, FormatJSON, Options{}); err == nil {
		t.Fatal("an object with no record array was accepted")
	}
}

// TestParseJSONLRecordsBadLinesAndContinues pins the line-oriented recovery: a
// malformed line is recorded against its line number and the rest still parse.
func TestParseJSONLRecordsBadLinesAndContinues(t *testing.T) {
	path := writeFile(t, "data.jsonl", "{\"a\":1}\nnot json\n{\"a\":2}\n")

	res, err := Parse(path, FormatJSONL, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(res.Rows))
	}
	if len(res.Errors) != 1 || res.Errors[0].Code != "invalid_json" {
		t.Fatalf("errors = %+v, want one invalid_json", res.Errors)
	}
	if res.Errors[0].Row != 2 {
		t.Fatalf("error line = %d, want 2", res.Errors[0].Row)
	}
}

// --- XLSX -------------------------------------------------------------------

// TestParseXLSXSkipsBlankRowsAndSelectsSheet pins the spreadsheet behaviour a CSV
// has no equivalent for: trailing blank rows are dropped and a named sheet can be
// selected.
func TestParseXLSXSkipsBlankRowsAndSelectsSheet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.xlsx")
	f := excelize.NewFile()
	if err := f.SetSheetName("Sheet1", "Contacts"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	rows := [][]any{
		{"name", "email"},
		{"Ada", "ada@example.test"},
		{"", ""}, // wholly blank, must be skipped
		{"Bob", "bob@example.test"},
	}
	for r, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			if err := f.SetCellValue("Contacts", cell, v); err != nil {
				t.Fatalf("set %s: %v", cell, err)
			}
		}
	}
	if _, err := f.NewSheet("Other"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	sheets, err := ListSheets(path)
	if err != nil {
		t.Fatalf("ListSheets: %v", err)
	}
	if len(sheets) != 2 {
		t.Fatalf("sheets = %+v, want 2", sheets)
	}

	res, err := Parse(path, FormatXLSX, Options{SheetName: "Contacts"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (the blank row is skipped)", len(res.Rows))
	}
	if res.Rows[0]["name"] != "Ada" {
		t.Fatalf("row = %v", res.Rows[0])
	}
}

// TestParseXLSXUnknownSheetIsAnError pins that selecting a missing sheet fails
// loudly rather than falling back to the first one.
func TestParseXLSXUnknownSheetIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.xlsx")
	f := excelize.NewFile()
	if err := f.SetCellValue("Sheet1", "A1", "x"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := Parse(path, FormatXLSX, Options{SheetName: "Nope"}); err == nil {
		t.Fatal("an unknown sheet name was accepted")
	}
}

// --- XML --------------------------------------------------------------------

// TestParseXMLInfersRecordElementAndAttributes pins the record inference and the
// attribute-as-field rule, which is what makes an XML export mappable. It also
// pins that a record's nested elements stay on the same row as its attributes:
// the bug this test was written against emitted {@id:1} and {name:Ada} as two
// separate records.
func TestParseXMLInfersRecordElementAndAttributes(t *testing.T) {
	// The record element is named explicitly, and the root's first child is named
	// identically so the inference path is covered too.
	path := writeFile(t, "data.xml",
		`<contacts><contact id="1"><name>Ada</name></contact><contact id="2"><name>Bob</name></contact></contacts>`)

	res, err := Parse(path, FormatXML, Options{XMLRowElement: "contact"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (one per record): %+v", len(res.Rows), res.Rows)
	}
	if res.Rows[0]["name"] != "Ada" || res.Rows[0]["@id"] != "1" {
		t.Fatalf("row = %v, want name=Ada and @id=1 on the same record", res.Rows[0])
	}
	if res.Rows[1]["name"] != "Bob" || res.Rows[1]["@id"] != "2" {
		t.Fatalf("row = %v", res.Rows[1])
	}
}

// TestParseXMLInfersRecordElement pins the inference path: with no named element,
// the root's first child is the record, so a plain export parses without the
// caller knowing the schema.
func TestParseXMLInfersRecordElement(t *testing.T) {
	path := writeFile(t, "data.xml",
		`<rows><row><name>Ada</name></row><row><name>Bob</name></row></rows>`)
	res, err := Parse(path, FormatXML, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 2 || res.Rows[0]["name"] != "Ada" {
		t.Fatalf("rows = %+v, want two records with name", res.Rows)
	}
}

// TestParseXMLFlattensNestedElements pins that a nested group becomes a dotted
// field, matching the JSON parser, so an address survives the import.
func TestParseXMLFlattensNestedElements(t *testing.T) {
	path := writeFile(t, "data.xml",
		`<contacts><contact><name>Ada</name><address city="London"><zip>E1</zip></address></contact></contacts>`)
	res, err := Parse(path, FormatXML, Options{XMLRowElement: "contact"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(res.Rows))
	}
	row := res.Rows[0]
	if row["address.zip"] != "E1" {
		t.Fatalf("nested element not flattened: %v", row)
	}
	if row["@address.city"] != "London" {
		t.Fatalf("nested attribute not kept: %v", row)
	}
}

// --- Encoding ---------------------------------------------------------------

// TestParseRejectsNonUTF8 pins the guard that turns a mis-encoded upload into a
// clear message instead of a confusing parse failure deeper in.
func TestParseRejectsNonUTF8(t *testing.T) {
	// A UTF-16 little-endian BOM is the common spreadsheet export artefact.
	utf16 := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(utf16, []byte{0xFF, 0xFE, 'a', 0, ',', 0, 'b', 0}, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Parse(utf16, FormatCSV, Options{}); err == nil {
		t.Fatal("a UTF-16 file was accepted")
	}

	invalid := filepath.Join(t.TempDir(), "bad.csv")
	if err := os.WriteFile(invalid, []byte{'a', ',', 0xff, 0xfe, 0xfd}, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Parse(invalid, FormatCSV, Options{}); err == nil {
		t.Fatal("an invalid-UTF-8 file was accepted")
	}
}

// TestParseEmptyJSONIsAnEmptyResult pins that an empty file is a valid, empty
// import rather than an error, which is what a user who exported nothing sees.
func TestParseEmptyJSONIsAnEmptyResult(t *testing.T) {
	path := writeFile(t, "empty.json", "   ")
	res, err := Parse(path, FormatJSON, Options{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Rows) != 0 || len(res.Columns) != 0 {
		t.Fatalf("empty json produced %+v", res)
	}
}

// TestParseUnsupportedFormatIsAnError pins that an unknown format is refused
// rather than silently treated as CSV.
func TestParseUnsupportedFormatIsAnError(t *testing.T) {
	path := writeFile(t, "data.bin", "x")
	if _, err := Parse(path, Format("nope"), Options{}); err == nil {
		t.Fatal("an unsupported format was accepted")
	}
}

// --- Value helpers ----------------------------------------------------------

// TestValueHelpers pins the tolerant parsers the mapper and validator share, so
// a change in what counts as a number or a date is visible here.
func TestValueHelpers(t *testing.T) {
	if v, ok := ParseNumber("$1,234.50"); !ok || v != 1234.5 {
		t.Fatalf("ParseNumber($1,234.50) = %v, %v", v, ok)
	}
	if _, ok := ParseNumber("not a number"); ok {
		t.Fatal("ParseNumber accepted a non-number")
	}
	if _, ok := ParseNumber(""); ok {
		t.Fatal("ParseNumber accepted an empty string")
	}
	if v, ok := ParseBool("ON"); !ok || !v {
		t.Fatalf("ParseBool(ON) = %v, %v", v, ok)
	}
	if v, ok := ParseBool("off"); !ok || v {
		t.Fatalf("ParseBool(off) = %v, %v", v, ok)
	}
	if _, ok := ParseBool("maybe"); ok {
		t.Fatal("ParseBool accepted an unknown token")
	}
	if _, ok := ParseDate("2024-01-02"); !ok {
		t.Fatal("ParseDate rejected an ISO date")
	}
	if _, ok := ParseDate("02 Jan 2006"); !ok {
		t.Fatal("ParseDate rejected a long-form date")
	}
	if _, ok := ParseDate(""); ok {
		t.Fatal("ParseDate accepted an empty string")
	}
}
