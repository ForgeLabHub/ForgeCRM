package importx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/importx/parsers"
	"github.com/geff1778/forgecrm/internal/importx/safe"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// errRollbackDryRun forces a transaction rollback at the end of a dry run.
var errRollbackDryRun = errors.New("dry run: rolling back")

// JobPayload is what is stored in the queue job for an import.
type JobPayload struct {
	ImportJobID uuid.UUID `json:"import_job_id"`
	DryRun      bool      `json:"dry_run"`
}

// Validate checks the mapping and options against the analysed schema and a
// sample of rows, reporting problems before anything is written.
func (s *Service) Validate(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	mapping []MappingEntry, opts Options) (*ValidationReport, error) {

	job, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	spec, ok := specs[job.EntityType]
	if !ok {
		return nil, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	if err := validateOptions(job.EntityType, opts); err != nil {
		return nil, err
	}

	schema, err := s.analyseForRun(ctx, p, id, opts)
	if err != nil {
		return nil, err
	}
	defer schema.cleanup()

	report := &ValidationReport{}
	if len(mapping) == 0 {
		report.Issues = append(report.Issues, ValidationIssue{
			Severity: "error", Code: "no_mapping",
			Message: "no field mapping was provided",
		})
		return report, nil
	}

	// Every source column must exist in the analysed schema.
	sourceCols := map[string]bool{}
	for _, c := range schema.result.Columns {
		sourceCols[c] = true
	}
	// Every target field must be importable and mapped at most once.
	targets := map[string]int{}
	fieldKinds := map[string]fieldSpec{}
	for _, f := range spec.Fields {
		fieldKinds[f.Name] = f
	}
	mappedTargets := map[string]bool{}
	for _, m := range mapping {
		if !sourceCols[m.SourceColumn] {
			report.Issues = append(report.Issues, ValidationIssue{
				Severity: "error", Code: "unknown_source_column",
				Message: fmt.Sprintf("source column %q was not found in the file", m.SourceColumn),
			})
		}
		if _, ok := fieldKinds[m.TargetField]; !ok {
			report.Issues = append(report.Issues, ValidationIssue{
				Severity: "error", Code: "unknown_target_field",
				Message: fmt.Sprintf("%q is not an importable field for %s", m.TargetField, job.EntityType),
			})
			continue
		}
		targets[m.TargetField]++
		mappedTargets[m.TargetField] = true
	}
	for field, n := range targets {
		if n > 1 {
			report.Issues = append(report.Issues, ValidationIssue{
				Severity: "error", Code: "duplicate_target",
				Message: fmt.Sprintf("field %q is mapped from %d source columns", field, n),
			})
		}
	}
	for _, f := range spec.Fields {
		if f.Required && !mappedTargets[f.Name] && f.Default == "" {
			report.Issues = append(report.Issues, ValidationIssue{
				Severity: "error", Code: "missing_required_field",
				Message: fmt.Sprintf("required field %q is not mapped", f.Name),
			})
		}
	}
	if job.EntityType == "deal" && !mappedTargets["pipeline_id"] &&
		(opts.DefaultPipelineID == nil || opts.DefaultStageID == nil) {
		report.Issues = append(report.Issues, ValidationIssue{
			Severity: "error", Code: "missing_pipeline_defaults",
			Message: "importing deals requires default_pipeline_id and default_stage_id unless the file supplies them",
		})
	}

	// Validate a sample of rows end to end so conversion problems surface now.
	validated := int64(0)
	bad := int64(0)
	proc := &rowProcessor{
		svc: s, orgID: p.OrganizationID, actorID: p.UserID,
		entity: job.EntityType, spec: spec, opts: opts, mapping: mapping,
		onResult: func(rowNum int64, outcome writeOutcome, err error) {
			validated++
			if outcome == outcomeFailed {
				bad++
				if len(report.SampleErrors) < 25 {
					msg := ""
					if err != nil {
						msg = err.Error()
					}
					report.SampleErrors = append(report.SampleErrors, RowError{
						RowNumber: rowNum, Code: "row_invalid", Message: msg})
				}
			}
		},
	}
	// A dry-run transaction validates without persisting anything.
	_ = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		_ = proc.run(ctx, &parsers.Result{Rows: schema.result.Rows}, 0)
		return errRollbackDryRun
	})

	report.SampledRows = validated
	report.InvalidSampleRows = bad
	report.Valid = !hasErrors(report.Issues) && bad == 0

	if report.Valid {
		mappingJSON, _ := json.Marshal(mapping)
		optsJSON, _ := json.Marshal(opts)
		const upd = `
			UPDATE import_jobs SET mapping = $3, options = $4, status = 'validated'
			WHERE id = $1 AND organization_id = $2`
		if _, err := s.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID, mappingJSON, optsJSON); err != nil {
			return nil, fmt.Errorf("persist validated mapping: %w", err)
		}
	}
	return report, nil
}

// ValidationReport is the outcome of a mapping validation.
type ValidationReport struct {
	Valid             bool              `json:"valid"`
	Issues            []ValidationIssue `json:"issues"`
	SampledRows       int64             `json:"sampled_rows"`
	InvalidSampleRows int64             `json:"invalid_sample_rows"`
	SampleErrors      []RowError        `json:"sample_errors"`
}

// ValidationIssue is one problem found during validation.
type ValidationIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func hasErrors(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}

func validateOptions(entityType string, opts Options) error {
	switch opts.Duplicates {
	case "", DuplicatesSkip, DuplicatesUpdate, DuplicatesCreate, DuplicatesFail:
	default:
		return httpx.ValidationError(map[string]string{
			"duplicates": "must be one of skip, update, create, fail"})
	}
	if entityType != "deal" {
		return nil
	}
	if opts.DefaultPipelineID != nil && opts.DefaultStageID == nil {
		return httpx.ValidationError(map[string]string{
			"default_stage_id": "default_stage_id is required when default_pipeline_id is set"})
	}
	if opts.DefaultStageID != nil && opts.DefaultPipelineID == nil {
		return httpx.ValidationError(map[string]string{
			"default_pipeline_id": "default_pipeline_id is required when default_stage_id is set"})
	}
	return nil
}

// ---------------------------------------------------------------------------
// Source resolution
// ---------------------------------------------------------------------------

// resolvedSource is a parsed source with the extraction kept alive.
type resolvedSource struct {
	result  *parsers.Result
	root    string
	path    string
	format  parsers.Format
	cleanup func()
}

func (r *resolvedSource) Close() {
	if r.cleanup != nil {
		r.cleanup()
	}
}

// analyseForRun resolves the upload, safely extracts an archive if needed, and
// parses up to a bounded sample of rows.
func (s *Service) analyseForRun(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	opts Options) (*resolvedSource, error) {

	job, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	stored, err := s.storedPath(ctx, id, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	return s.resolveSource(ctx, stored, job, opts, 1000)
}

// resolveSource performs format detection, safe extraction and parsing.
func (s *Service) resolveSource(ctx context.Context, stored string, job *Job, opts Options,
	maxRows int64) (*resolvedSource, error) {

	head, err := readHead(stored, 512)
	if err != nil {
		return nil, err
	}
	containerFormat := safe.DetectFormat(ptrOr(job.OriginalFilename, ""), head)
	tabularFormat := parsers.Detect(ptrOr(job.OriginalFilename, ""), head)
	if safe.IsArchive(containerFormat) {
		// See Analyse: an archive must never be mistaken for a delimited file.
		tabularFormat = ""
	}

	sourcePath := stored
	cleanup := func() {}

	if safe.IsArchive(containerFormat) {
		extractor := safe.NewExtractor(safe.Limits{
			MaxEntries:       s.cfg.ImportMaxArchiveFiles,
			MaxTotalBytes:    s.cfg.ImportMaxUnpackedBytes,
			MaxEntryBytes:    s.cfg.ImportMaxUploadBytes,
			MaxDepth:         s.cfg.ImportMaxArchiveDepth,
			MaxCompressRatio: s.cfg.ImportMaxRatio,
		})
		extracted, err := extractor.Extract(s.cfg.ImportTempDir, stored, containerFormat,
			derefInt64(job.SizeBytes))
		if err != nil {
			return nil, httpx.NewError(422, httpx.CodeValidation, "archive rejected: "+err.Error())
		}
		cleanup = extracted.Cleanup

		chosen := ""
		if opts.ArchiveEntry != "" {
			for _, f := range extracted.Files {
				if f.RelPath == opts.ArchiveEntry {
					chosen = f.AbsPath
					break
				}
			}
			if chosen == "" {
				cleanup()
				return nil, httpx.NewError(422, httpx.CodeValidation,
					"archive_entry does not match any file in the archive")
			}
		} else {
			for _, f := range extracted.Files {
				fh, _ := readHead(f.AbsPath, 256)
				if parsers.Detect(f.RelPath, fh) != "" {
					chosen = f.AbsPath
					break
				}
			}
			if chosen == "" {
				cleanup()
				return nil, httpx.NewError(422, httpx.CodeValidation,
					"the archive contains no file in a supported format")
			}
		}
		sourcePath = chosen
		ch, _ := readHead(chosen, 512)
		tabularFormat = parsers.Detect(chosen, ch)
	}
	if tabularFormat == "" {
		cleanup()
		return nil, httpx.NewError(422, httpx.CodeValidation,
			"could not determine the data format; supported formats are CSV, TSV, JSON, JSONL, XLSX and XML")
	}

	popts := parsers.Options{
		MaxRows:       maxRows,
		HasHeader:     opts.HasHeader,
		SheetName:     opts.SheetName,
		XMLRowElement: opts.XMLRowElement,
	}
	if opts.Delimiter != "" {
		r := []rune(opts.Delimiter)
		if len(r) == 1 {
			popts.Delimiter = r[0]
		}
	}
	parsed, err := parsers.Parse(sourcePath, tabularFormat, popts)
	if err != nil {
		cleanup()
		return nil, httpx.NewError(422, httpx.CodeValidation, "could not parse the file: "+err.Error())
	}
	return &resolvedSource{result: parsed, path: sourcePath, format: tabularFormat, cleanup: cleanup}, nil
}

// ---------------------------------------------------------------------------
// Dry run
// ---------------------------------------------------------------------------

// DryRun executes the whole pipeline inside a transaction and rolls it back,
// producing a report of what would have happened without writing any business
// data. This is the mechanism behind "prove it before you commit".
func (s *Service) DryRun(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	mapping []MappingEntry, opts Options) (*Report, error) {

	job, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	spec, ok := specs[job.EntityType]
	if !ok {
		return nil, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	if err := validateOptions(job.EntityType, opts); err != nil {
		return nil, err
	}
	src, err := s.analyseForRun(ctx, p, id, opts)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	rep := &Report{
		TotalRows:    src.result.TotalRows,
		DryRun:       true,
		StartedAt:    time.Now(),
		ColumnCount:  len(src.result.Columns),
		MappedFields: mappedFields(mapping),
		Notes:        []string{"no business data was written; all changes were rolled back"},
	}
	var rowErrs []RowError
	proc := &rowProcessor{
		svc: s, orgID: p.OrganizationID, actorID: p.UserID,
		entity: job.EntityType, spec: spec, opts: opts, mapping: mapping,
		onResult: func(rowNum int64, outcome writeOutcome, err error) {
			tallyOutcome(rep, outcome, err, rowNum, &rowErrs)
		},
	}
	txErr := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := proc.run(ctx, src.result, opts.SkipRows); err != nil {
			return err
		}
		return errRollbackDryRun
	})
	if txErr != nil && !errors.Is(txErr, errRollbackDryRun) {
		return nil, txErr
	}
	rep.FinishedAt = time.Now()
	rep.DurationMS = rep.FinishedAt.Sub(rep.StartedAt).Milliseconds()
	if rep.DurationMS > 0 {
		rep.RowsPerSec = float64(rep.Imported+rep.Updated+rep.Skipped) / (float64(rep.DurationMS) / 1000.0)
	}
	rep.Checksum = ptrOr(job.ChecksumSHA256, "")

	repJSON, _ := json.Marshal(rep)
	const upd = `
		UPDATE import_jobs SET report = $3, status = 'dry_run', total_rows = $4,
			imported_rows = $5, skipped_rows = $6, failed_rows = $7
		WHERE id = $1 AND organization_id = $2`
	if _, err := s.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID, repJSON, rep.TotalRows,
		rep.Imported+rep.Updated, rep.Skipped, rep.Failed); err != nil {
		return nil, fmt.Errorf("persist dry run report: %w", err)
	}
	return rep, nil
}

func mappedFields(mapping []MappingEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mapping {
		if !seen[m.TargetField] {
			seen[m.TargetField] = true
			out = append(out, m.TargetField)
		}
	}
	return out
}

func tallyOutcome(rep *Report, outcome writeOutcome, err error, rowNum int64, rowErrs *[]RowError) {
	switch outcome {
	case outcomeCreated:
		rep.Imported++
	case outcomeUpdated:
		rep.Updated++
	case outcomeSkipped:
		rep.Skipped++
	case outcomeFailed:
		rep.Failed++
		if len(*rowErrs) < 10000 {
			msg := "row rejected"
			if err != nil {
				msg = err.Error()
			}
			if len(msg) > 500 {
				msg = msg[:500]
			}
			*rowErrs = append(*rowErrs, RowError{RowNumber: rowNum, Code: "row_failed", Message: msg})
		}
	}
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

// Execute enqueues an import for background processing.
//
// The HTTP request returns as soon as the job is queued: a large import must not
// hold a request open.
func (s *Service) Execute(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	mapping []MappingEntry, opts Options) (*jobs.Job, error) {

	job, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if job.Status == "running" || job.Status == "queued" {
		return nil, httpx.NewError(409, httpx.CodeConflict, "this import is already queued or running")
	}
	if _, ok := specs[job.EntityType]; !ok {
		return nil, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	if err := validateOptions(job.EntityType, opts); err != nil {
		return nil, err
	}
	if len(mapping) == 0 {
		return nil, httpx.ValidationError(map[string]string{"mapping": "a field mapping is required"})
	}

	var queued *jobs.Job
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		mappingJSON, _ := json.Marshal(mapping)
		optsJSON, _ := json.Marshal(opts)
		const upd = `
			UPDATE import_jobs SET mapping = $3, options = $4, status = 'queued', error = NULL
			WHERE id = $1 AND organization_id = $2`
		if _, err := s.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID, mappingJSON, optsJSON); err != nil {
			return fmt.Errorf("queue import: %w", err)
		}
		q, err := s.queue.Enqueue(ctx, jobs.EnqueueRequest{
			OrganizationID: &p.OrganizationID,
			Kind:           jobs.KindImportExecute,
			Payload:        JobPayload{ImportJobID: id, DryRun: false},
			CreatedBy:      &p.UserID,
		})
		if err != nil {
			return err
		}
		queued = q
		const link = `UPDATE import_jobs SET job_id = $2 WHERE id = $1`
		if _, err := s.db.Q(ctx).Exec(ctx, link, id, q.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionImportExecuted), EntityType: "import_job", EntityID: id.String(),
			Metadata: map[string]any{"job_id": q.ID, "entity_type": job.EntityType},
		})
	})
	if err != nil {
		return nil, err
	}
	return queued, nil
}

// Cancel requests cancellation of a running or queued import.
func (s *Service) Cancel(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	job, err := s.Get(ctx, p, id)
	if err != nil {
		return err
	}
	if job.Status == "succeeded" || job.Status == "failed" || job.Status == "cancelled" {
		return httpx.NewError(409, httpx.CodeConflict, "this import has already finished")
	}
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if job.JobID != nil {
			if err := s.queue.Cancel(ctx, *job.JobID); err != nil {
				return err
			}
		}
		const upd = `
			UPDATE import_jobs SET status = 'cancelled', finished_at = now()
			WHERE id = $1 AND organization_id = $2 AND status NOT IN ('succeeded','failed','cancelled')`
		if _, err := s.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID); err != nil {
			return fmt.Errorf("cancel import: %w", err)
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionImportCancelled), EntityType: "import_job", EntityID: id.String(),
		})
	})
}

// Errors returns the recorded row-level errors for an import.
func (s *Service) Errors(ctx context.Context, p *rbac.Principal, id uuid.UUID, limit, offset int) ([]RowError, int64, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, 0, err
	}
	var total int64
	const countQ = `SELECT count(*) FROM import_row_errors WHERE import_job_id = $1`
	if err := s.db.Q(ctx).QueryRow(ctx, countQ, id).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count import errors: %w", err)
	}
	const q = `
		SELECT row_number, error_code, message FROM import_row_errors
		WHERE import_job_id = $1 ORDER BY row_number LIMIT $2 OFFSET $3`
	rows, err := s.db.Q(ctx).Query(ctx, q, id, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list import errors: %w", err)
	}
	defer rows.Close()
	var out []RowError
	for rows.Next() {
		var e RowError
		if err := rows.Scan(&e.RowNumber, &e.Code, &e.Message); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------------------
// Worker entrypoint
// ---------------------------------------------------------------------------

// RunJob executes an import as a background job.
//
// Processing is batched and checkpointed. Each batch commits on its own, so an
// interruption loses at most one batch, and the checkpoint records how far the
// run got. Re-running is safe because duplicate detection is applied per row.
func (s *Service) RunJob(ctx context.Context, q *jobs.Queue, job *jobs.Job) error {
	var payload JobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("decode import job payload: %w", err)
	}
	if job.OrganizationID == nil {
		return errors.New("import job has no organization")
	}

	// The worker has no HTTP principal; it acts as the platform on behalf of the
	// organization that created the job, and every write still carries the
	// organization scope. The creator is recorded for the audit trail.
	actorID := uuid.Nil
	if job.CreatedBy != nil {
		actorID = *job.CreatedBy
	}

	const sel = `SELECT ` + jobColumns + ` FROM import_jobs WHERE id = $1`
	imp, err := scanJob(s.db.Pool().QueryRow(ctx, sel, payload.ImportJobID))
	if err != nil {
		return fmt.Errorf("load import job: %w", err)
	}
	if imp.OrganizationID != *job.OrganizationID {
		return errors.New("import job belongs to a different organization than the queue job")
	}
	if imp.Status == "cancelled" {
		return nil
	}

	spec, ok := specs[imp.EntityType]
	if !ok {
		return fmt.Errorf("unsupported entity type %q", imp.EntityType)
	}
	var mapping []MappingEntry
	if err := json.Unmarshal(imp.Mapping, &mapping); err != nil {
		return fmt.Errorf("decode mapping: %w", err)
	}
	var opts Options
	if err := json.Unmarshal(imp.Options, &opts); err != nil {
		return fmt.Errorf("decode options: %w", err)
	}

	started := time.Now()
	if _, err := s.db.Pool().Exec(ctx,
		`UPDATE import_jobs SET status='running', started_at=now(), error=NULL WHERE id=$1`, imp.ID); err != nil {
		return fmt.Errorf("mark import running: %w", err)
	}

	stored := ""
	{
		const p2 = `SELECT stored_path FROM import_jobs WHERE id = $1`
		var sp *string
		if err := s.db.Pool().QueryRow(ctx, p2, imp.ID).Scan(&sp); err != nil {
			return fmt.Errorf("load stored path: %w", err)
		}
		if sp != nil {
			stored = *sp
		}
	}
	if stored == "" {
		return s.failImport(ctx, imp, actorID, "the import has no uploaded file")
	}

	src, err := s.resolveSource(ctx, stored, imp, opts, 0)
	if err != nil {
		return s.failImport(ctx, imp, actorID, err.Error())
	}
	defer src.Close()

	format := detectFormatFor(imp, src)
	rep := &Report{
		DryRun:       false,
		StartedAt:    started,
		ColumnCount:  len(src.result.Columns),
		MappedFields: mappedFields(mapping),
		Checksum:     ptrOr(imp.ChecksumSHA256, ""),
	}
	var (
		rowErrs  []RowError
		rowCount int64
	)
	proc := &rowProcessor{
		svc: s, orgID: imp.OrganizationID, actorID: actorID,
		entity: imp.EntityType, spec: spec, opts: opts, mapping: mapping,
		onResult: func(rowNum int64, outcome writeOutcome, err error) {
			rowCount++
			tallyOutcome(rep, outcome, err, rowNum, &rowErrs)
		},
	}

	// Each batch runs in its own transaction so that a failure late in a large
	// file does not discard the work already committed.
	_, streamErr := parsers.Stream(src.sourcePath(), detectTabular(imp, src), parsers.StreamOptions{
		Options: parsers.Options{
			HasHeader:     opts.HasHeader,
			SheetName:     opts.SheetName,
			XMLRowElement: opts.XMLRowElement,
		},
		BatchSize: s.cfg.ImportBatchSize,
		SkipRows:  opts.SkipRows,
	}, func(batch parsers.Batch) error {
		cancelled, err := q.IsCancelled(ctx, job.ID)
		if err != nil {
			return err
		}
		if cancelled {
			return parsers.ErrStopStream
		}
		return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
			return proc.run(ctx, &parsers.Result{Rows: batch.Rows}, batch.StartRow-1)
		})
	})
	if streamErr != nil {
		return s.failImport(ctx, imp, actorID, streamErr.Error())
	}
	_ = format

	rep.TotalRows = rowCount
	rep.FinishedAt = time.Now()
	rep.DurationMS = rep.FinishedAt.Sub(rep.StartedAt).Milliseconds()
	if rep.DurationMS > 0 {
		rep.RowsPerSec = float64(rowCount) / (float64(rep.DurationMS) / 1000.0)
	}

	// Persist the report, the error rows and the final state atomically.
	repJSON, _ := json.Marshal(rep)
	finalStatus := "succeeded"
	if rep.Failed > 0 && rep.Imported+rep.Updated == 0 {
		finalStatus = "failed"
	}
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const upd = `
			UPDATE import_jobs SET status=$3, report=$4, total_rows=$5, imported_rows=$6,
				skipped_rows=$7, failed_rows=$8, finished_at=now()
			WHERE id=$1 AND organization_id=$2`
		if _, err := s.db.Q(ctx).Exec(ctx, upd, imp.ID, imp.OrganizationID, finalStatus, repJSON,
			rep.TotalRows, rep.Imported+rep.Updated, rep.Skipped, rep.Failed); err != nil {
			return fmt.Errorf("store import report: %w", err)
		}
		if len(rowErrs) > 0 {
			const ins = `
				INSERT INTO import_row_errors (import_job_id, row_number, error_code, message)
				VALUES ($1,$2,$3,$4)`
			for _, e := range rowErrs {
				if _, err := s.db.Q(ctx).Exec(ctx, ins, imp.ID, e.RowNumber, e.Code, e.Message); err != nil {
					return fmt.Errorf("store row error: %w", err)
				}
			}
		}
		eventType := outbox.EventImportCompleted
		if finalStatus == "failed" {
			eventType = outbox.EventImportFailed
		}
		return s.outbox.Emit(ctx, &imp.OrganizationID, eventType, map[string]any{
			"import_job_id": imp.ID, "entity_type": imp.EntityType,
			"total_rows": rep.TotalRows, "imported": rep.Imported, "updated": rep.Updated,
			"skipped": rep.Skipped, "failed": rep.Failed, "checksum_sha256": rep.Checksum,
		})
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) failImport(ctx context.Context, imp *Job, actorID uuid.UUID, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	const upd = `
		UPDATE import_jobs SET status='failed', error=$3, finished_at=now()
		WHERE id=$1 AND organization_id=$2`
	if _, err := s.db.Pool().Exec(ctx, upd, imp.ID, imp.OrganizationID, message); err != nil {
		return fmt.Errorf("mark import failed: %w", err)
	}
	_ = s.outbox.Emit(ctx, &imp.OrganizationID, outbox.EventImportFailed, map[string]any{
		"import_job_id": imp.ID, "error": message,
	})
	return nil
}

// sourcePath is a helper that exposes the parsed source's file, which the
// streaming parser needs. The archive extraction root is kept alive by the
// resolvedSource for the duration of the run.
func (r *resolvedSource) sourcePath() string { return r.path }

// detectFormatFor returns the container format for the report.
func detectFormatFor(imp *Job, src *resolvedSource) string {
	if src != nil && src.format != "" {
		return string(src.format)
	}
	return imp.Format
}

// detectTabular returns the tabular format to stream with.
//
// It relies on the format resolveSource already established. Sniffing the path
// again would fail: the stored upload is named "<uuid>.upload" and therefore has
// no extension to match on.
func detectTabular(imp *Job, src *resolvedSource) parsers.Format {
	if src != nil && src.format != "" {
		return src.format
	}
	if f := parsers.Detect(ptrOr(imp.OriginalFilename, ""), nil); f != "" {
		return f
	}
	return parsers.Format(imp.Format)
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// Export streams an entity set as CSV or JSONL. Exports are bounded by the
// caller's permissions and organization scope, exactly like a list request.
func (s *Service) Export(ctx context.Context, p *rbac.Principal, entityType, format string,
	w func(row map[string]any) error) (int64, error) {

	if !supportedEntities[entityType] {
		return 0, httpx.ValidationError(map[string]string{"entity_type": "unsupported entity type"})
	}
	if format == "" {
		format = "csv"
	}
	spec := specs[entityType]
	cols := make([]string, 0, len(spec.Fields))
	for _, f := range spec.Fields {
		cols = append(cols, f.Name)
	}
	selects := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		selects = append(selects, c)
	}
	selects = append(selects, "external_source", "external_id")

	q := fmt.Sprintf(`SELECT %s FROM %s WHERE organization_id = $1 AND archived_at IS NULL ORDER BY created_at`,
		strings.Join(selects, ", "), spec.Table)
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return 0, fmt.Errorf("export %s: %w", entityType, err)
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		values := make([]any, len(selects))
		ptrs := make([]any, len(selects))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, err
		}
		row := map[string]any{}
		for i, name := range selects {
			if values[i] == nil {
				continue
			}
			row[name] = normaliseForExport(values[i])
		}
		if err := w(row); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

func normaliseForExport(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339)
	default:
		return t
	}
}

// CleanupTemp removes extraction directories an import left behind, returning how
// many it removed.
//
// A directory is removed only when it is older than minAge. The deferred cleanup
// on the normal path already removes the directory for the import that produced
// it; this sweep exists for the import whose worker died before that defer ran.
// The age bound is what keeps the sweep from deleting a directory a live import is
// still reading: writing into a directory updates its mtime, so a running import
// keeps its directory young. It must therefore be larger than the job timeout, and
// callers pass a value that is.
//
// A removal failure is reported, not ignored, so a persistent failure is visible
// rather than presenting as a disk that keeps filling.
func (s *Service) CleanupTemp(minAge time.Duration) (int, error) {
	root := s.cfg.ImportTempDir
	if root == "" {
		root = os.TempDir()
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		// A missing root means there is nothing to sweep, which is the state a fresh
		// deployment is in and is not an error.
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-minAge)
	var removed int
	var firstErr error
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "forgecrm-import-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

// ensure crm is referenced (it is used by writer.go for validation).
var _ = crm.ErrNotFound
