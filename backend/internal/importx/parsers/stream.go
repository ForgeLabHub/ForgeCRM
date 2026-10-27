package parsers

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Batch is one chunk of rows handed to a streaming consumer.
type Batch struct {
	Rows []Row
	// StartRow is the 1-based data-row number of the first row in this batch.
	StartRow int64
}

// StreamCallback consumes a batch. Returning an error stops the stream; the
// special sentinel ErrStopStream stops it without reporting a failure.
type StreamCallback func(Batch) error

// ErrStopStream signals a deliberate early stop, for example a cancellation.
var ErrStopStream = errors.New("stop stream")

// StreamOptions controls streaming.
type StreamOptions struct {
	Options
	BatchSize int
	// SkipRows discards the first N data rows, which is how a resumed import
	// continues from a checkpoint.
	SkipRows int64
}

// Stream parses a source in batches.
//
// CSV, TSV and JSONL are streamed row by row, so an import of millions of rows
// uses a bounded amount of memory. JSON, XLSX and XML are parsed in full and then
// chunked: those formats need the whole document to be well-formed before any
// record is trustworthy, so streaming them would trade correctness for memory.
func Stream(path string, format Format, opts StreamOptions, cb StreamCallback) (*Result, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 500
	}
	switch format {
	case FormatCSV:
		return streamDelimited(path, opts, ',', cb)
	case FormatTSV:
		return streamDelimited(path, opts, '\t', cb)
	case FormatJSONL:
		return streamJSONL(path, opts, cb)
	case FormatJSON, FormatXLSX, FormatXML:
		return streamViaFullParse(path, format, opts, cb)
	default:
		return nil, fmt.Errorf("unsupported tabular format %q", format)
	}
}

func streamViaFullParse(path string, format Format, opts StreamOptions, cb StreamCallback) (*Result, error) {
	res, err := Parse(path, format, opts.Options)
	if err != nil {
		return nil, err
	}
	rows := res.Rows
	if opts.SkipRows > 0 {
		if opts.SkipRows >= int64(len(rows)) {
			rows = nil
		} else {
			rows = rows[opts.SkipRows:]
		}
	}
	start := opts.SkipRows + 1
	for i := 0; i < len(rows); i += opts.BatchSize {
		end := i + opts.BatchSize
		if end > len(rows) {
			end = len(rows)
		}
		b := Batch{Rows: rows[i:end], StartRow: start + int64(i)}
		if err := cb(b); err != nil {
			if errors.Is(err, ErrStopStream) {
				res.Truncated = true
				return res, nil
			}
			return res, err
		}
	}
	res.Rows = nil
	return res, nil
}

func streamDelimited(path string, opts StreamOptions, defDelim rune, cb StreamCallback) (*Result, error) {
	if err := checkEncoding(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 64*1024))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	r.Comma = defDelim
	if opts.Delimiter != 0 {
		r.Comma = opts.Delimiter
	}

	res := &Result{Types: map[string]string{}}
	var (
		header    []string
		batch     []Row
		rowNum    int64
		dataRow   int64
		processed int64
	)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		b := Batch{Rows: batch, StartRow: dataRow - int64(len(batch)) + 1}
		batch = nil
		return cb(b)
	}

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				res.Errors = append(res.Errors, RowError{
					Row: int64(pe.Line), Code: "parse_error", Message: pe.Err.Error()})
				continue
			}
			return nil, fmt.Errorf("read delimited file: %w", err)
		}
		rowNum++
		if header == nil {
			if !opts.hasHeader() {
				header = synthColumns(len(rec))
			} else {
				header = normaliseHeader(rec)
				continue
			}
		}
		dataRow++
		res.TotalRows = dataRow
		if dataRow <= opts.SkipRows {
			continue
		}
		if len(rec) > MaxColumnCount {
			res.Errors = append(res.Errors, RowError{
				Row: rowNum, Code: "too_many_columns",
				Message: fmt.Sprintf("row has %d columns, the maximum is %d", len(rec), MaxColumnCount)})
			continue
		}
		if len(rec) != len(header) {
			res.Errors = append(res.Errors, RowError{
				Row: rowNum, Code: "column_count_mismatch",
				Message: fmt.Sprintf("row has %d columns but the header has %d", len(rec), len(header))})
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
		batch = append(batch, row)
		processed++
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				if errors.Is(err, ErrStopStream) {
					res.Columns = header
					res.Truncated = true
					return res, nil
				}
				return res, err
			}
		}
		if opts.MaxRows > 0 && processed >= opts.MaxRows {
			res.Truncated = true
			break
		}
	}
	if err := flush(); err != nil {
		if errors.Is(err, ErrStopStream) {
			res.Columns = header
			res.Truncated = true
			return res, nil
		}
		return res, err
	}
	res.Columns = header
	inferTypesFromSample(res, header)
	return res, nil
}

func streamJSONL(path string, opts StreamOptions, cb StreamCallback) (*Result, error) {
	if err := checkEncoding(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open jsonl file: %w", err)
	}
	defer f.Close()

	res := &Result{Types: map[string]string{}}
	columnSet := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var (
		batch     []Row
		line      int64
		dataRow   int64
		processed int64
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		b := Batch{Rows: batch, StartRow: dataRow - int64(len(batch)) + 1}
		batch = nil
		return cb(b)
	}

	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			res.Errors = append(res.Errors, RowError{Row: line, Code: "invalid_json", Message: err.Error()})
			continue
		}
		dataRow++
		res.TotalRows = dataRow
		if dataRow <= opts.SkipRows {
			continue
		}
		row := flattenObject(obj)
		for k := range row {
			columnSet[k] = true
		}
		batch = append(batch, row)
		processed++
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				if errors.Is(err, ErrStopStream) {
					res.Columns = sortedKeys(columnSet)
					res.Truncated = true
					return res, nil
				}
				return res, err
			}
		}
		if opts.MaxRows > 0 && processed >= opts.MaxRows {
			res.Truncated = true
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read jsonl: %w", err)
	}
	if err := flush(); err != nil {
		if errors.Is(err, ErrStopStream) {
			res.Columns = sortedKeys(columnSet)
			res.Truncated = true
			return res, nil
		}
		return res, err
	}
	res.Columns = sortedKeys(columnSet)
	inferTypesFromSample(res, res.Columns)
	return res, nil
}

// inferTypesFromSample infers types without holding every row in memory: it uses
// the rows still buffered plus the column list.
func inferTypesFromSample(res *Result, columns []string) {
	if res.Types == nil {
		res.Types = map[string]string{}
	}
	for _, col := range columns {
		if _, ok := res.Types[col]; !ok {
			res.Types[col] = "text"
		}
	}
}
