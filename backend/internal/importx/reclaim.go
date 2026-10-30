package importx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReclaimReport describes what a stored-file reconciliation found and removed.
type ReclaimReport struct {
	// Scanned is the number of files in the private storage directory that the
	// sweep owns, whether or not they were eligible. Scanned equals Referenced +
	// Orphans + TooYoung.
	Scanned int `json:"scanned"`
	// Referenced is how many eligible files an import_jobs row still points at.
	Referenced int `json:"referenced"`
	// Orphans is how many eligible files had no row pointing at them.
	Orphans int `json:"orphans"`
	// TooYoung is how many files were left because they are younger than the age
	// bound and may belong to an upload in flight.
	TooYoung int `json:"too_young"`
	// Removed is how many orphaned files were deleted. It is zero in a dry run.
	Removed int `json:"removed"`
	// Bytes is the total size of the files removed.
	Bytes int64 `json:"bytes"`
	// DryRun reports whether the pass measured without removing.
	DryRun bool `json:"dry_run"`
	// Errors names files that could not be removed, so a persistent failure is
	// visible rather than presenting as a disk that keeps filling.
	Errors []string `json:"errors,omitempty"`
}

// ReclaimStoredFiles removes uploaded files no import_jobs row references.
//
// The retention engine sweeps import_jobs rows, but it operates on rows and knows
// nothing about the private files those rows describe. When a row is removed its
// stored file becomes unreferenced, and nothing else reclaims it, so the storage
// directory grows by the size of every upload ever made. This pass is the
// file-side counterpart: it is what makes the import_jobs window actually
// reclaim the disk the import consumed.
//
// A file is removed only when it is older than minAge. That bound is what keeps
// the pass from deleting the file of an import that is being created right now:
// the file is written before its row is inserted, so a file younger than the
// bound may belong to a row that does not exist yet.
//
// It is idempotent and safe to run twice: a file already removed is simply gone,
// and a file still referenced is left alone. It never fails on a single file it
// cannot remove; the failure is recorded in the report so the run continues.
func (s *Service) ReclaimStoredFiles(ctx context.Context, minAge time.Duration, dryRun bool) (ReclaimReport, error) {
	report := ReclaimReport{DryRun: dryRun}
	if s.storage == "" {
		return report, nil
	}

	entries, err := os.ReadDir(s.storage)
	if err != nil {
		// A missing directory means there is nothing to reclaim, which is the state
		// a fresh deployment is in and is not an error.
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, fmt.Errorf("read import storage: %w", err)
	}

	cutoff := time.Now().Add(-minAge)

	// Collect the files this pass owns: the upload itself and the temporary file a
	// replacement writes before it renames. Anything else in the directory is not
	// ours and is left untouched.
	type candidate struct {
		path string
		size int64
	}
	var candidates []candidate
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".upload") && !strings.HasSuffix(name, ".upload.tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		report.Scanned++
		if info.ModTime().After(cutoff) {
			// Too young to be sure it is not an in-flight upload; leave it for the
			// next pass.
			report.TooYoung++
			continue
		}
		candidates = append(candidates, candidate{path: filepath.Join(s.storage, name), size: info.Size()})
	}
	if len(candidates) == 0 {
		return report, nil
	}

	// Ask the database which of the candidates a row still references. The lookup
	// is batched so one pass over a large storage directory issues a bounded number
	// of statements, and it names only the paths this pass is considering, so it
	// never reads the whole table.
	referenced := map[string]bool{}
	const batchSize = 500
	for start := 0; start < len(candidates); start += batchSize {
		end := start + batchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		paths := make([]string, 0, end-start)
		for _, c := range candidates[start:end] {
			paths = append(paths, c.path)
		}
		rows, err := s.db.Q(ctx).Query(ctx,
			`SELECT stored_path FROM import_jobs WHERE stored_path = ANY($1)`, paths)
		if err != nil {
			return report, fmt.Errorf("look up referenced import files: %w", err)
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return report, fmt.Errorf("scan referenced import file: %w", err)
			}
			referenced[p] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return report, fmt.Errorf("read referenced import files: %w", err)
		}
		rows.Close()
	}

	for _, c := range candidates {
		if referenced[c.path] {
			report.Referenced++
			continue
		}
		report.Orphans++
		if dryRun {
			continue
		}
		if err := os.Remove(c.path); err != nil {
			if os.IsNotExist(err) {
				// A concurrent pass already removed it; the outcome is the one we
				// wanted, so it is not a failure.
				report.Removed++
				continue
			}
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", filepath.Base(c.path), err))
			continue
		}
		report.Removed++
		report.Bytes += c.size
	}
	return report, nil
}
