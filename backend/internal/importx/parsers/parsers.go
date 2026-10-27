// Package parsers converts tabular source files into a common row model.
//
// A parser never guesses silently: it reports the inferred column set, the
// detected types and any row-level problems, so the wizard can show the operator
// exactly what will be imported.
package parsers

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// Format identifies a tabular data format.
type Format string

// Supported tabular formats.
const (
	FormatCSV   Format = "csv"
	FormatTSV   Format = "tsv"
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
	FormatXLSX  Format = "xlsx"
	FormatXML   Format = "xml"
)

// Detect determines the tabular format from the filename and a content sample.
func Detect(filename string, head []byte) Format {
	lower := strings.ToLower(filepath.Base(filename))
	switch {
	case strings.HasSuffix(lower, ".csv"):
		return FormatCSV
	case strings.HasSuffix(lower, ".tsv"), strings.HasSuffix(lower, ".tab"):
		return FormatTSV
	case strings.HasSuffix(lower, ".jsonl"), strings.HasSuffix(lower, ".ndjson"):
		return FormatJSONL
	case strings.HasSuffix(lower, ".json"):
		return FormatJSON
	case strings.HasSuffix(lower, ".xlsx"), strings.HasSuffix(lower, ".xlsm"):
		return FormatXLSX
	case strings.HasSuffix(lower, ".xml"):
		return FormatXML
	}
	// Content sniffing for extensionless files.
	trimmed := strings.TrimSpace(string(head))
	if strings.HasPrefix(trimmed, "{") {
		return FormatJSONL
	}
	if strings.HasPrefix(trimmed, "[") {
		return FormatJSON
	}
	if strings.HasPrefix(trimmed, "<?xml") || strings.HasPrefix(trimmed, "<") {
		return FormatXML
	}
	if strings.Contains(trimmed, "\t") && strings.Contains(trimmed, "\n") {
		return FormatTSV
	}
	if strings.Contains(trimmed, ",") {
		return FormatCSV
	}
	return ""
}

// Sheet describes one importable sheet or file inside a source.
type Sheet struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Format  Format `json:"format"`
	Rows    int64  `json:"rows,omitempty"`
	Columns int    `json:"columns,omitempty"`
}

// Row is a parsed record: column name to value.
type Row map[string]any

// RowError records a problem with a specific row.
type RowError struct {
	Row     int64  `json:"row"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result is the outcome of parsing a source.
type Result struct {
	Columns []string   `json:"columns"`
	Rows    []Row      `json:"rows"`
	Errors  []RowError `json:"errors"`
	// Types holds the inferred type for each column.
	Types map[string]string `json:"types"`
	// TotalRows is the number of data rows read, including any not returned.
	TotalRows int64 `json:"total_rows"`
	// Truncated reports whether reading stopped at the requested limit.
	Truncated bool `json:"truncated"`
}

// Options controls parsing.
type Options struct {
	// MaxRows limits how many rows are returned. Zero means no limit.
	MaxRows int64
	// Delimiter overrides the CSV/TSV delimiter. Zero means auto.
	Delimiter rune
	// HasHeader indicates the first row contains column names. Default true.
	HasHeader *bool
	// SheetName selects an XLSX sheet. Empty means the first sheet.
	SheetName string
	// XMLRowElement names the element that represents one record. Empty means
	// the parser infers the repeated child element.
	XMLRowElement string
}

func (o Options) hasHeader() bool {
	if o.HasHeader == nil {
		return true
	}
	return *o.HasHeader
}

// MaxColumnCount bounds the column count so a malformed file cannot exhaust memory.
const MaxColumnCount = 512

// MaxColumnNameLength bounds a column name.
const MaxColumnNameLength = 200

// Parse reads a source file into the common row model.
func Parse(path string, format Format, opts Options) (*Result, error) {
	if err := checkEncoding(path); err != nil {
		return nil, err
	}
	switch format {
	case FormatCSV:
		return parseDelimited(path, opts, ',')
	case FormatTSV:
		return parseDelimited(path, opts, '\t')
	case FormatJSON:
		return parseJSON(path, opts)
	case FormatJSONL:
		return parseJSONL(path, opts)
	case FormatXLSX:
		return parseXLSX(path, opts)
	case FormatXML:
		return parseXML(path, opts)
	default:
		return nil, fmt.Errorf("unsupported tabular format %q", format)
	}
}

// checkEncoding rejects files that are not valid UTF-8, with a clear message
// rather than a confusing failure deeper in a parser. A UTF-8 BOM is tolerated.
func checkEncoding(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer f.Close()

	// Sample the first block; a binary or mis-encoded file is usually obvious
	// immediately.
	buf := make([]byte, 64*1024)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read source file: %w", err)
	}
	sample := buf[:n]
	// UTF-16 byte-order marks are a common export artefact.
	if len(sample) >= 2 {
		if (sample[0] == 0xFF && sample[1] == 0xFE) || (sample[0] == 0xFE && sample[1] == 0xFF) {
			return fmt.Errorf("file appears to be UTF-16 encoded; re-export it as UTF-8")
		}
	}
	// Strip a UTF-8 BOM before validation.
	if len(sample) >= 3 && sample[0] == 0xEF && sample[1] == 0xBB && sample[2] == 0xBF {
		sample = sample[3:]
	}
	if !utf8.Valid(sample) {
		return fmt.Errorf("file is not valid UTF-8; re-export it with UTF-8 encoding")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Delimited (CSV / TSV)
// ---------------------------------------------------------------------------

func parseDelimited(path string, opts Options, defDelim rune) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 64*1024))
	r.FieldsPerRecord = -1 // rows may have differing lengths; we validate below
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	delim := defDelim
	if opts.Delimiter != 0 {
		delim = opts.Delimiter
	}
	r.Comma = delim

	res := &Result{Types: map[string]string{}}

	var header []string
	rowNum := int64(0)
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A malformed record is recorded, and parsing continues, so one bad
			// line does not discard the whole file.
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				res.Errors = append(res.Errors, RowError{
					Row: int64(pe.Line), Code: "parse_error", Message: pe.Err.Error(),
				})
				continue
			}
			return nil, fmt.Errorf("read delimited file: %w", err)
		}
		rowNum++

		if header == nil {
			if !opts.hasHeader() {
				// Synthesise column names from the first row's width.
				header = synthColumns(len(rec))
			} else {
				header = normaliseHeader(rec)
				continue
			}
		}

		if len(rec) > MaxColumnCount {
			res.Errors = append(res.Errors, RowError{
				Row: rowNum, Code: "too_many_columns",
				Message: fmt.Sprintf("row has %d columns, the maximum is %d", len(rec), MaxColumnCount),
			})
			continue
		}
		if len(rec) != len(header) {
			res.Errors = append(res.Errors, RowError{
				Row: rowNum, Code: "column_count_mismatch",
				Message: fmt.Sprintf("row has %d columns but the header has %d", len(rec), len(header)),
			})
			// Still import what we can when the row is short.
			if len(rec) > len(header) {
				rec = rec[:len(header)]
			}
		}

		row := make(Row, len(header))
		for i, name := range header {
			if i < len(rec) {
				row[name] = strings.TrimSpace(rec[i])
			} else {
				row[name] = ""
			}
		}
		res.TotalRows++
		if opts.MaxRows > 0 && int64(len(res.Rows)) >= opts.MaxRows {
			res.Truncated = true
			break
		}
		res.Rows = append(res.Rows, row)
	}

	res.Columns = header
	inferTypes(res)
	return res, nil
}

func synthColumns(n int) []string {
	if n > MaxColumnCount {
		n = MaxColumnCount
	}
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("column_%d", i+1)
	}
	return out
}

// normaliseHeader makes column names unique and non-empty so that mapping is
// deterministic even for a messy export.
func normaliseHeader(rec []string) []string {
	if len(rec) > MaxColumnCount {
		rec = rec[:MaxColumnCount]
	}
	seen := map[string]int{}
	out := make([]string, len(rec))
	for i, raw := range rec {
		name := strings.TrimSpace(raw)
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "" {
			name = fmt.Sprintf("column_%d", i+1)
		}
		if len(name) > MaxColumnNameLength {
			name = name[:MaxColumnNameLength]
		}
		if n, dup := seen[name]; dup {
			seen[name] = n + 1
			name = fmt.Sprintf("%s_%d", name, n+1)
		} else {
			seen[name] = 1
		}
		out[i] = name
	}
	return out
}

// ---------------------------------------------------------------------------
// JSON and JSONL
// ---------------------------------------------------------------------------

func parseJSON(path string, opts Options) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read json file: %w", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return &Result{Columns: []string{}, Rows: []Row{}, Types: map[string]string{}}, nil
	}

	// An object with a single array property is a common export shape; unwrap it.
	var raw []map[string]any
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parse json array: %w", err)
		}
	} else {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, fmt.Errorf("parse json object: %w", err)
		}
		found := false
		for _, key := range []string{"data", "items", "records", "rows", "results"} {
			if v, ok := envelope[key]; ok && strings.HasPrefix(strings.TrimSpace(string(v)), "[") {
				if err := json.Unmarshal(v, &raw); err != nil {
					return nil, fmt.Errorf("parse json %s array: %w", key, err)
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("json object does not contain an array of records " +
				"(expected a top-level array or one of: data, items, records, rows, results)")
		}
	}

	res := &Result{Types: map[string]string{}}
	columnSet := map[string]bool{}
	for _, obj := range raw {
		res.TotalRows++
		if opts.MaxRows > 0 && int64(len(res.Rows)) >= opts.MaxRows {
			res.Truncated = true
			break
		}
		row := flattenObject(obj)
		for k := range row {
			columnSet[k] = true
		}
		res.Rows = append(res.Rows, row)
	}
	res.Columns = sortedKeys(columnSet)
	inferTypes(res)
	return res, nil
}

func parseJSONL(path string, opts Options) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open jsonl file: %w", err)
	}
	defer f.Close()

	res := &Result{Types: map[string]string{}}
	columnSet := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // allow long lines
	line := int64(0)
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			res.Errors = append(res.Errors, RowError{
				Row: line, Code: "invalid_json", Message: err.Error(),
			})
			continue
		}
		res.TotalRows++
		if opts.MaxRows > 0 && int64(len(res.Rows)) >= opts.MaxRows {
			res.Truncated = true
			break
		}
		row := flattenObject(obj)
		for k := range row {
			columnSet[k] = true
		}
		res.Rows = append(res.Rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read jsonl: %w", err)
	}
	res.Columns = sortedKeys(columnSet)
	inferTypes(res)
	return res, nil
}

// flattenObject converts nested objects to dotted keys and arrays to JSON
// strings, which keeps the row model flat enough to map to CRM fields.
func flattenObject(obj map[string]any) Row {
	out := Row{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch val := v.(type) {
		case map[string]any:
			for k, child := range val {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}
				walk(key, child)
			}
		case []any:
			b, _ := json.Marshal(val)
			out[prefix] = string(b)
		case nil:
			out[prefix] = nil
		default:
			out[prefix] = val
		}
	}
	for k, v := range obj {
		walk(k, v)
	}
	return out
}

// ---------------------------------------------------------------------------
// XLSX
// ---------------------------------------------------------------------------

func parseXLSX(path string, opts Options) (*Result, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("xlsx workbook contains no sheets")
	}
	sheet := sheets[0]
	if opts.SheetName != "" {
		found := false
		for _, s := range sheets {
			if s == opts.SheetName {
				sheet, found = s, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("sheet %q not found in workbook", opts.SheetName)
		}
	}

	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", sheet, err)
	}
	defer rows.Close()

	res := &Result{Types: map[string]string{}}
	var header []string
	rowNum := int64(0)
	for rows.Next() {
		rowNum++
		cells, err := rows.Columns()
		if err != nil {
			res.Errors = append(res.Errors, RowError{Row: rowNum, Code: "read_error", Message: err.Error()})
			continue
		}
		if header == nil {
			if !opts.hasHeader() {
				header = synthColumns(len(cells))
			} else {
				header = normaliseHeader(cells)
				continue
			}
		}
		if len(cells) > MaxColumnCount {
			res.Errors = append(res.Errors, RowError{
				Row: rowNum, Code: "too_many_columns",
				Message: fmt.Sprintf("row has %d columns, the maximum is %d", len(cells), MaxColumnCount),
			})
			continue
		}
		row := make(Row, len(header))
		for i, name := range header {
			if i < len(cells) {
				row[name] = strings.TrimSpace(cells[i])
			} else {
				row[name] = ""
			}
		}
		// Skip wholly empty rows, which spreadsheets are full of.
		if isBlankRow(row) {
			continue
		}
		res.TotalRows++
		if opts.MaxRows > 0 && int64(len(res.Rows)) >= opts.MaxRows {
			res.Truncated = true
			break
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Error(); err != nil {
		return nil, fmt.Errorf("iterate sheet: %w", err)
	}
	res.Columns = header
	inferTypes(res)
	return res, nil
}

// ListSheets returns the sheets in an XLSX workbook.
func ListSheets(path string) ([]Sheet, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()
	var out []Sheet
	for _, name := range f.GetSheetList() {
		rows, err := f.Rows(name)
		count := int64(0)
		if err == nil {
			for rows.Next() {
				count++
				if count > 1_000_000 {
					break
				}
			}
			rows.Close()
		}
		out = append(out, Sheet{Name: name, Path: path, Format: FormatXLSX, Rows: count})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// XML
// ---------------------------------------------------------------------------

func parseXML(path string, opts Options) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open xml file: %w", err)
	}
	defer f.Close()

	dec := xml.NewDecoder(f)
	dec.Strict = false
	dec.CharsetReader = charsetReader

	res := &Result{Types: map[string]string{}}
	columnSet := map[string]bool{}

	var (
		stack       []string // element name stack
		rowElement  string   // the element that delimits one record
		rowDepth    int
		current     Row
		recordNum   int64
		pendingText string
	)

	commit := func() {
		if current == nil || len(current) == 0 {
			current = nil
			return
		}
		recordNum++
		res.TotalRows++
		if opts.MaxRows > 0 && int64(len(res.Rows)) >= opts.MaxRows {
			res.Truncated = true
			current = nil
			return
		}
		for k := range current {
			columnSet[k] = true
		}
		res.Rows = append(res.Rows, current)
		current = nil
	}

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Attributes become fields named @attr on the record itself.
			if current != nil && len(stack) == rowDepth {
				for _, a := range t.Attr {
					current["@"+a.Name.Local] = a.Value
				}
			}
			stack = append(stack, t.Name.Local)
			if rowElement == "" {
				// The first element that has children becomes the record
				// element, unless the caller named one explicitly.
				rowElement = opts.XMLRowElement
				if rowElement == "" {
					rowElement = t.Name.Local
				}
				rowDepth = len(stack)
				current = Row{}
				continue
			}
			if t.Name.Local == rowElement && len(stack) > rowDepth {
				// A nested occurrence of the record element starts a new record.
				commit()
				rowDepth = len(stack)
				current = Row{}
			}
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text == "" || current == nil {
				continue
			}
			pendingText += text
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			name := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			if current != nil && len(stack) >= rowDepth && name != rowElement {
				if pendingText != "" {
					if existing, ok := current[name]; ok {
						current[name] = fmt.Sprintf("%v %s", existing, pendingText)
					} else {
						current[name] = pendingText
					}
				}
			}
			pendingText = ""

			if current != nil && name == rowElement && len(stack) < rowDepth {
				commit()
			} else if current != nil && name == rowElement && len(stack)+1 == rowDepth {
				commit()
			}
		}
		if opts.MaxRows > 0 && int64(len(res.Rows)) >= opts.MaxRows {
			res.Truncated = true
			break
		}
	}
	commit()

	res.Columns = sortedKeys(columnSet)
	inferTypes(res)
	return res, nil
}

// ---------------------------------------------------------------------------
// Type inference
// ---------------------------------------------------------------------------

// inferTypes determines a likely type per column from the sample rows.
func inferTypes(res *Result) {
	for _, col := range res.Columns {
		var (
			sawValue  bool
			allInt    = true
			allNumber = true
			allBool   = true
			allDate   = true
			allEmail  = true
		)
		for _, row := range res.Rows {
			v, ok := row[col]
			if !ok || v == nil {
				continue
			}
			s := strings.TrimSpace(fmt.Sprint(v))
			if s == "" {
				continue
			}
			sawValue = true
			if !isInteger(s) {
				allInt = false
			}
			if !isNumber(s) {
				allNumber = false
			}
			if !isBoolean(s) {
				allBool = false
			}
			if !isDate(s) {
				allDate = false
			}
			if !strings.Contains(s, "@") || strings.ContainsAny(s, " \t") {
				allEmail = false
			}
		}
		switch {
		case !sawValue:
			res.Types[col] = "empty"
		case allBool:
			res.Types[col] = "boolean"
		case allInt:
			res.Types[col] = "integer"
		case allNumber:
			res.Types[col] = "number"
		case allDate:
			res.Types[col] = "date"
		case allEmail:
			res.Types[col] = "email"
		default:
			res.Types[col] = "text"
		}
	}
}

func isBlankRow(row Row) bool {
	for _, v := range row {
		if v == nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(v)) != "" {
			return false
		}
	}
	return true
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	// Stable, deterministic order for the UI.
	sortStrings(out)
	return out
}
