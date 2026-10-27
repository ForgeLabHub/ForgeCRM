// Package importx implements the Data Import & Migration Center.
//
// The pipeline is: upload → detect format → safe extract → parse → infer schema
// → map → validate → dry-run → execute (batched, checkpointed) → report.
//
// Untrusted-input handling lives in importx/safe; this package is responsible for
// the business pipeline and for making every step inspectable before it runs.
package importx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/importx/parsers"
	"github.com/geff1778/forgecrm/internal/importx/safe"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Entity types an import can target.
var supportedEntities = map[string]bool{
	"account": true, "contact": true, "lead": true, "deal": true,
	"task": true, "ticket": true, "activity": true,
}

// Duplicate handling strategies.
const (
	DuplicatesSkip   = "skip"
	DuplicatesUpdate = "update"
	DuplicatesCreate = "create"
	DuplicatesFail   = "fail"
)

// Service drives imports.
type Service struct {
	db      *database.DB
	cfg     *config.Config
	audit   *audit.Logger
	outbox  *outbox.Store
	crm     *crm.Service
	queue   *jobs.Queue
	storage string
}

// NewService builds the import service. storage is a private directory that is
// never served over HTTP.
func NewService(db *database.DB, cfg *config.Config, auditLog *audit.Logger,
	ob *outbox.Store, crmSvc *crm.Service, queue *jobs.Queue, storage string) (*Service, error) {
	if storage == "" {
		return nil, errors.New("import storage directory must be configured")
	}
	if err := os.MkdirAll(storage, 0o700); err != nil {
		return nil, fmt.Errorf("create import storage: %w", err)
	}
	return &Service{db: db, cfg: cfg, audit: auditLog, outbox: ob, crm: crmSvc,
		queue: queue, storage: storage}, nil
}

// ---------------------------------------------------------------------------
// Job records
// ---------------------------------------------------------------------------

// Job is an import job.
type Job struct {
	ID               uuid.UUID       `json:"id"`
	OrganizationID   uuid.UUID       `json:"organization_id"`
	Name             string          `json:"name"`
	SourceType       string          `json:"source_type"`
	EntityType       string          `json:"entity_type"`
	Format           string          `json:"format"`
	Status           string          `json:"status"`
	OriginalFilename *string         `json:"original_filename,omitempty"`
	ChecksumSHA256   *string         `json:"checksum_sha256,omitempty"`
	SizeBytes        *int64          `json:"size_bytes,omitempty"`
	Config           json.RawMessage `json:"config"`
	Mapping          json.RawMessage `json:"mapping"`
	Options          json.RawMessage `json:"options"`
	SchemaInfo       json.RawMessage `json:"schema_info"`
	Preview          json.RawMessage `json:"preview"`
	Report           json.RawMessage `json:"report"`
	TotalRows        int64           `json:"total_rows"`
	ImportedRows     int64           `json:"imported_rows"`
	SkippedRows      int64           `json:"skipped_rows"`
	FailedRows       int64           `json:"failed_rows"`
	JobID            *uuid.UUID      `json:"job_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
	Error            *string         `json:"error,omitempty"`
}

// MappingEntry maps one source column to a target field.
type MappingEntry struct {
	SourceColumn string `json:"source_column"`
	TargetField  string `json:"target_field"`
	// Transform is an optional conversion hint: trim, upper, lower, number, date, bool.
	Transform string `json:"transform,omitempty"`
	// Required marks a field that must be present and non-empty.
	Required bool `json:"required,omitempty"`
}

// Options controls import behaviour.
type Options struct {
	Duplicates        string            `json:"duplicates"`
	DuplicateKey      []string          `json:"duplicate_key,omitempty"`
	ExternalSource    string            `json:"external_source,omitempty"`
	Delimiter         string            `json:"delimiter,omitempty"`
	HasHeader         *bool             `json:"has_header,omitempty"`
	SheetName         string            `json:"sheet_name,omitempty"`
	XMLRowElement     string            `json:"xml_row_element,omitempty"`
	DefaultOwnerID    *uuid.UUID        `json:"default_owner_id,omitempty"`
	DefaultPipelineID *uuid.UUID        `json:"default_pipeline_id,omitempty"`
	DefaultStageID    *uuid.UUID        `json:"default_stage_id,omitempty"`
	SkipRows          int64             `json:"skip_rows,omitempty"`
	MaxRows           int64             `json:"max_rows,omitempty"`
	ArchiveEntry      string            `json:"archive_entry,omitempty"`
	Extra             map[string]string `json:"extra,omitempty"`
}

// ---------------------------------------------------------------------------
// Create and upload
// ---------------------------------------------------------------------------

// CreateRequest starts an import job.
type CreateRequest struct {
	Name             string
	EntityType       string
	SourceType       string
	OriginalFilename string
	SizeBytes        int64
	Content          io.Reader
}

// Create stores the uploaded file privately and records the job.
//
// The file is written with a random name under the private storage root; the
// caller-supplied filename is recorded as metadata only and is never used to
// build a path.
func (s *Service) Create(ctx context.Context, p *rbac.Principal, req CreateRequest) (*Job, error) {
	if !supportedEntities[req.EntityType] {
		return nil, httpx.ValidationError(map[string]string{
			"entity_type": "unsupported entity type; expected one of account, contact, lead, deal, task, ticket, activity"})
	}
	if req.SourceType == "" {
		req.SourceType = "file"
	}
	if req.SourceType != "file" {
		return nil, httpx.NewError(422, httpx.CodeValidation,
			"this cycle supports file uploads; database and REST sources are created through the migrations API")
	}
	if req.SizeBytes > s.cfg.ImportMaxUploadBytes {
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
			"upload exceeds the maximum of %d bytes", s.cfg.ImportMaxUploadBytes))
	}

	id := uuid.New()
	storedPath := filepath.Join(s.storage, id.String()+".upload")
	f, err := os.OpenFile(storedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create stored upload: %w", err)
	}

	hasher := sha256.New()
	limited := io.LimitReader(req.Content, s.cfg.ImportMaxUploadBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), limited)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(storedPath)
		return nil, fmt.Errorf("store upload: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(storedPath)
		return nil, fmt.Errorf("close upload: %w", closeErr)
	}
	if written > s.cfg.ImportMaxUploadBytes {
		os.Remove(storedPath)
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
			"upload exceeds the maximum of %d bytes", s.cfg.ImportMaxUploadBytes))
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	name := req.Name
	if strings.TrimSpace(name) == "" {
		name = req.OriginalFilename
	}
	if strings.TrimSpace(name) == "" {
		name = "Import " + time.Now().Format("2006-01-02 15:04")
	}

	var created *Job
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO import_jobs (id, organization_id, name, source_type, entity_type, format,
				status, original_filename, stored_path, checksum_sha256, size_bytes, created_by)
			VALUES ($1,$2,$3,$4,$5,'unknown','draft',$6,$7,$8,$9,$10)
			RETURNING ` + jobColumns
		j, err := scanJob(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID, name,
			req.SourceType, req.EntityType, nullIfEmpty(req.OriginalFilename), storedPath,
			checksum, written, p.UserID))
		if err != nil {
			return err
		}
		created = j
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionImportCreated), EntityType: "import_job", EntityID: j.ID.String(),
			Metadata: map[string]any{
				"filename": req.OriginalFilename, "checksum_sha256": checksum,
				"size_bytes": written, "entity_type": req.EntityType,
			},
		})
	})
	if err != nil {
		os.Remove(storedPath)
		return nil, err
	}
	return created, nil
}

const jobColumns = `
	id, organization_id, name, source_type, entity_type, format, status,
	original_filename, checksum_sha256, size_bytes, config, mapping, options,
	schema_info, preview, report, total_rows, imported_rows, skipped_rows, failed_rows,
	job_id, created_at, started_at, finished_at, error`

type rowScanner interface{ Scan(dest ...any) error }

func scanJob(row rowScanner) (*Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.OrganizationID, &j.Name, &j.SourceType, &j.EntityType, &j.Format,
		&j.Status, &j.OriginalFilename, &j.ChecksumSHA256, &j.SizeBytes, &j.Config, &j.Mapping,
		&j.Options, &j.SchemaInfo, &j.Preview, &j.Report, &j.TotalRows, &j.ImportedRows,
		&j.SkippedRows, &j.FailedRows, &j.JobID, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.Error)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// Get loads an import job scoped to the caller's organization.
func (s *Service) Get(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Job, error) {
	const q = `SELECT ` + jobColumns + ` FROM import_jobs WHERE id = $1 AND organization_id = $2`
	j, err := scanJob(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, crm.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// List returns import jobs for the organization.
func (s *Service) List(ctx context.Context, p *rbac.Principal, limit, offset int) ([]Job, int64, error) {
	var total int64
	const countQ = `SELECT count(*) FROM import_jobs WHERE organization_id = $1`
	if err := s.db.Q(ctx).QueryRow(ctx, countQ, p.OrganizationID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count import jobs: %w", err)
	}
	const q = `SELECT ` + jobColumns + `
		FROM import_jobs WHERE organization_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list import jobs: %w", err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *j)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------------------
// Analysis
// ---------------------------------------------------------------------------

// AnalysisResult describes what was found in the upload.
type AnalysisResult struct {
	Format       string               `json:"format"`
	Archive      bool                 `json:"archive"`
	Files        []safe.ExtractedFile `json:"files,omitempty"`
	SelectedFile string               `json:"selected_file,omitempty"`
	Columns      []string             `json:"columns"`
	Types        map[string]string    `json:"types"`
	Preview      []parsers.Row        `json:"preview"`
	Errors       []parsers.RowError   `json:"errors"`
	TotalRows    int64                `json:"total_rows"`
	Truncated    bool                 `json:"truncated"`
}

// Analyze detects the format, safely extracts archives and parses a preview.
func (s *Service) Analyze(ctx context.Context, p *rbac.Principal, id uuid.UUID, opts Options) (*AnalysisResult, error) {
	job, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	stored, err := s.storedPath(ctx, id, p.OrganizationID)
	if err != nil {
		return nil, err
	}

	head, err := readHead(stored, 512)
	if err != nil {
		return nil, err
	}
	containerFormat := safe.DetectFormat(ptrOr(job.OriginalFilename, ""), head)
	tabularFormat := parsers.Detect(ptrOr(job.OriginalFilename, ""), head)
	if safe.IsArchive(containerFormat) {
		// A binary archive header can coincidentally contain a delimiter, so
		// content sniffing must never classify an archive as a data file.
		tabularFormat = ""
	}

	res := &AnalysisResult{Types: map[string]string{}}

	sourcePath := stored
	cleanup := func() {}

	if safe.IsArchive(containerFormat) && tabularFormat == "" {
		res.Archive = true
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
			// A rejected archive is a normal, reportable outcome.
			return nil, httpx.NewError(422, httpx.CodeValidation, "archive rejected: "+err.Error())
		}
		cleanup = extracted.Cleanup
		res.Files = extracted.Files
		res.Format = string(containerFormat)

		// Pick the entry to analyse: the requested one, or the first parseable file.
		chosen := ""
		if opts.ArchiveEntry != "" {
			for _, f := range extracted.Files {
				if f.RelPath == opts.ArchiveEntry {
					chosen = f.AbsPath
					res.SelectedFile = f.RelPath
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
					res.SelectedFile = f.RelPath
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
		tabularFormat = parsers.Detect(res.SelectedFile, ch)
	} else if safe.IsArchive(containerFormat) {
		// A compressed single file such as contacts.csv.gz.
		res.Archive = true
		extractor := safe.NewExtractor(safe.Limits{
			MaxEntries: s.cfg.ImportMaxArchiveFiles, MaxTotalBytes: s.cfg.ImportMaxUnpackedBytes,
			MaxEntryBytes: s.cfg.ImportMaxUploadBytes, MaxDepth: s.cfg.ImportMaxArchiveDepth,
			MaxCompressRatio: s.cfg.ImportMaxRatio,
		})
		extracted, err := extractor.Extract(s.cfg.ImportTempDir, stored, containerFormat,
			derefInt64(job.SizeBytes))
		if err != nil {
			return nil, httpx.NewError(422, httpx.CodeValidation, "archive rejected: "+err.Error())
		}
		cleanup = extracted.Cleanup
		res.Files = extracted.Files
		res.Format = string(containerFormat)

		// A single-file container such as contacts.csv.gz, or a tar/gzip whose
		// entries the run will select from. Pick the first parseable entry.
		if opts.ArchiveEntry != "" {
			for _, f := range extracted.Files {
				if f.RelPath == opts.ArchiveEntry {
					sourcePath = f.AbsPath
					res.SelectedFile = f.RelPath
					break
				}
			}
			if res.SelectedFile == "" {
				cleanup()
				return nil, httpx.NewError(422, httpx.CodeValidation,
					"archive_entry does not match any file in the archive")
			}
		} else {
			for _, f := range extracted.Files {
				fh, _ := readHead(f.AbsPath, 256)
				if parsers.Detect(f.RelPath, fh) != "" {
					sourcePath = f.AbsPath
					res.SelectedFile = f.RelPath
					break
				}
			}
			if res.SelectedFile == "" {
				cleanup()
				return nil, httpx.NewError(422, httpx.CodeValidation,
					"the archive contains no file in a supported format")
			}
		}
		ch, _ := readHead(sourcePath, 512)
		tabularFormat = parsers.Detect(res.SelectedFile, ch)
	}
	defer cleanup()

	if tabularFormat == "" {
		return nil, httpx.NewError(422, httpx.CodeValidation,
			"could not determine the data format; supported formats are CSV, TSV, JSON, JSONL, XLSX and XML")
	}
	if res.Format == "" {
		res.Format = string(tabularFormat)
	}

	popts := parsers.Options{
		MaxRows:       100,
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
		return nil, httpx.NewError(422, httpx.CodeValidation, "could not parse the file: "+err.Error())
	}

	res.Columns = parsed.Columns
	res.Types = parsed.Types
	res.Preview = parsed.Rows
	res.Errors = parsed.Errors
	res.TotalRows = parsed.TotalRows
	res.Truncated = parsed.Truncated

	// Persist the analysis so the wizard can resume and the operator can audit it.
	schemaInfo, _ := json.Marshal(map[string]any{
		"format": res.Format, "columns": res.Columns, "types": res.Types,
		"selected_file": res.SelectedFile, "archive": res.Archive,
		"files": len(res.Files), "total_rows": res.TotalRows,
	})
	previewJSON, _ := json.Marshal(res.Preview)
	const upd = `
		UPDATE import_jobs SET format = $3, schema_info = $4, preview = $5, status = 'analyzed'
		WHERE id = $1 AND organization_id = $2`
	if _, err := s.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID, string(tabularFormat),
		schemaInfo, previewJSON); err != nil {
		return nil, fmt.Errorf("persist analysis: %w", err)
	}
	return res, nil
}

// ReplaceFile streams a file into an existing import job.
//
// The file is written to a fresh random path and the previous upload (if any) is
// removed afterwards, so a failed upload cannot leave a job pointing at a
// half-written file.
func (s *Service) ReplaceFile(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	filename string, content io.Reader) (*Job, error) {

	job, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if job.Status == "running" || job.Status == "queued" {
		return nil, httpx.NewError(409, httpx.CodeConflict, "this import is currently running")
	}

	previous := filepath.Join(s.storage, id.String()+".upload")
	newPath := filepath.Join(s.storage, id.String()+".upload.tmp")

	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create stored upload: %w", err)
	}
	hasher := sha256.New()
	limited := io.LimitReader(content, s.cfg.ImportMaxUploadBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), limited)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(newPath)
		return nil, fmt.Errorf("store upload: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(newPath)
		return nil, fmt.Errorf("close upload: %w", closeErr)
	}
	if written > s.cfg.ImportMaxUploadBytes {
		os.Remove(newPath)
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, fmt.Sprintf(
			"upload exceeds the maximum of %d bytes", s.cfg.ImportMaxUploadBytes))
	}
	if written == 0 {
		os.Remove(newPath)
		return nil, httpx.ValidationError(map[string]string{"file": "the uploaded file is empty"})
	}

	// Swap the file in only after the new one is complete on disk.
	if err := os.Rename(newPath, previous); err != nil {
		os.Remove(newPath)
		return nil, fmt.Errorf("store upload: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	// Detect the format at upload time so that a run does not depend on the
	// operator having opened the analysis step first.
	head, _ := readHead(previous, 512)
	format := string(parsers.Detect(filename, head))

	var updated *Job
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE import_jobs
			SET original_filename = $3, checksum_sha256 = $4, size_bytes = $5,
			    status = 'draft', format = $6, schema_info = '{}'::jsonb,
			    preview = '[]'::jsonb, report = '{}'::jsonb, error = NULL,
			    total_rows = 0, imported_rows = 0, skipped_rows = 0, failed_rows = 0
			WHERE id = $1 AND organization_id = $2
			RETURNING ` + jobColumns
		j, err := scanJob(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID,
			nullIfEmpty(filename), checksum, written, format))
		if err != nil {
			return err
		}
		updated = j
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: "imports.file_uploaded", EntityType: "import_job", EntityID: id.String(),
			Metadata: map[string]any{
				"filename": filename, "size_bytes": written, "checksum_sha256": checksum,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// storedPath returns the private on-disk location of the upload.
func (s *Service) storedPath(ctx context.Context, id, orgID uuid.UUID) (string, error) {
	const q = `SELECT stored_path FROM import_jobs WHERE id = $1 AND organization_id = $2`
	var path *string
	if err := s.db.Q(ctx).QueryRow(ctx, q, id, orgID).Scan(&path); err != nil {
		return "", crm.ErrNotFound
	}
	if path == nil || *path == "" {
		return "", httpx.NewError(409, httpx.CodeConflict, "this import job has no uploaded file")
	}
	return *path, nil
}

func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read file head: %w", err)
	}
	return buf[:read], nil
}

func ptrOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
