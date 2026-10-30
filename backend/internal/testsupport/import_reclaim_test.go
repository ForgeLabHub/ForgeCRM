//go:build integration

package testsupport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/retention"
)

// The reclaim pass is a statement about the filesystem and the import_jobs table,
// so it is exercised against a real database and a real storage directory. A mock
// would prove nothing about either.

// insertStoredImportJob records a terminal import_jobs row pointing at storedPath.
func insertStoredImportJob(t *testing.T, db *database.DB, orgID, storedPath string) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
        INSERT INTO import_jobs (organization_id, source_type, entity_type, status, stored_path, finished_at)
        VALUES ($1, 'file', 'account', 'succeeded', $2, now())`, orgID, storedPath)
	if err != nil {
		t.Fatalf("insert import job: %v", err)
	}
}

// newImportServiceForReclaim builds the real import service over the test
// database and a private storage directory.
func newImportServiceForReclaim(t *testing.T, db *database.DB, storage string) *importx.Service {
	t.Helper()
	svc, err := importx.NewService(db, &config.Config{ImportTempDir: t.TempDir()},
		audit.New(db), nil, nil, nil, storage)
	if err != nil {
		t.Fatalf("build import service: %v", err)
	}
	return svc
}

// writeStoredFile writes a file with a given age and returns its path.
func writeStoredFile(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("age %s: %v", name, err)
	}
	return path
}

// TestReclaimRemovesOnlyUnreferencedStoredFiles is the central correctness check:
// an old file with no row is removed, an old file with a row is kept, a young file
// is kept even without a row because it may belong to an upload in flight, and a
// file the pass does not own is never touched.
func TestReclaimRemovesOnlyUnreferencedStoredFiles(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "reclaim").String()
	storage := t.TempDir()

	referenced := writeStoredFile(t, storage, "11111111-1111-1111-1111-111111111111.upload", 3*time.Hour)
	orphan := writeStoredFile(t, storage, "22222222-2222-2222-2222-222222222222.upload", 3*time.Hour)
	young := writeStoredFile(t, storage, "33333333-3333-3333-3333-333333333333.upload", time.Minute)
	notOurs := filepath.Join(storage, "notes.txt")
	if err := os.WriteFile(notOurs, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}

	insertStoredImportJob(t, db, org, referenced)

	svc := newImportServiceForReclaim(t, db, storage)
	report, err := svc.ReclaimStoredFiles(context.Background(), 2*time.Hour, false)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	if report.Scanned != 3 {
		t.Errorf("scanned %d files, want 3 (the .txt is not ours)", report.Scanned)
	}
	if report.Referenced != 1 {
		t.Errorf("referenced %d files, want 1", report.Referenced)
	}
	if report.Orphans != 1 {
		t.Errorf("found %d orphans, want 1 (the young file is not eligible)", report.Orphans)
	}
	if report.Removed != 1 {
		t.Errorf("removed %d files, want 1", report.Removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the orphan survived the reclaim")
	}
	if _, err := os.Stat(referenced); err != nil {
		t.Errorf("a referenced file was removed: %v", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("a young file was removed: %v", err)
	}
	if _, err := os.Stat(notOurs); err != nil {
		t.Errorf("a file the pass does not own was removed: %v", err)
	}
}

// TestReclaimDryRunRemovesNothing asserts the default posture: the pass measures
// and changes nothing on disk.
func TestReclaimDryRunRemovesNothing(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	storage := t.TempDir()
	orphan := writeStoredFile(t, storage, "44444444-4444-4444-4444-444444444444.upload", 3*time.Hour)

	svc := newImportServiceForReclaim(t, db, storage)
	report, err := svc.ReclaimStoredFiles(context.Background(), 2*time.Hour, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !report.DryRun {
		t.Error("report does not record the dry run")
	}
	if report.Orphans != 1 || report.Removed != 0 {
		t.Errorf("dry run reported orphans=%d removed=%d, want 1 and 0", report.Orphans, report.Removed)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("a dry run removed a file: %v", err)
	}
}

// TestReclaimIsIdempotent proves a second pass is a no-op: the file the first pass
// removed is gone, and the run does not fail on its absence.
func TestReclaimIsIdempotent(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	storage := t.TempDir()
	writeStoredFile(t, storage, "55555555-5555-5555-5555-555555555555.upload", 3*time.Hour)

	svc := newImportServiceForReclaim(t, db, storage)
	first, err := svc.ReclaimStoredFiles(context.Background(), 2*time.Hour, false)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if first.Removed != 1 {
		t.Fatalf("first pass removed %d, want 1", first.Removed)
	}
	second, err := svc.ReclaimStoredFiles(context.Background(), 2*time.Hour, false)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Removed != 0 {
		t.Errorf("second pass removed %d files, want 0", second.Removed)
	}
	if second.Scanned != 0 {
		t.Errorf("second pass scanned %d files, want 0", second.Scanned)
	}
}

// TestReclaimAfterTheRetentionSweepRemovesTheFile is the end-to-end property that
// motivates the pass: sweep the import_jobs row with the retention engine, then
// reclaim, and the file the row described is gone.
func TestReclaimAfterTheRetentionSweepRemovesTheFile(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "reclaim-after-sweep").String()
	storage := t.TempDir()
	path := writeStoredFile(t, storage, "66666666-6666-6666-6666-666666666666.upload", 3*time.Hour)
	insertStoredImportJob(t, db, org, path)

	svc := newImportServiceForReclaim(t, db, storage)

	// Before the row sweep the file is referenced and kept.
	before, err := svc.ReclaimStoredFiles(context.Background(), 2*time.Hour, false)
	if err != nil {
		t.Fatalf("reclaim before sweep: %v", err)
	}
	if before.Removed != 0 {
		t.Fatalf("a referenced file was removed before the row sweep")
	}

	// Age the import job past its window and sweep it with the real runner.
	if _, err := db.Pool().Exec(context.Background(),
		`UPDATE import_jobs SET finished_at = $1 WHERE stored_path = $2`,
		time.Now().Add(-400*24*time.Hour), path); err != nil {
		t.Fatalf("age the import job: %v", err)
	}
	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("retention run: %v", err)
	}

	var rows int64
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM import_jobs WHERE stored_path = $1`, path).Scan(&rows); err != nil {
		t.Fatalf("count import rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("the retention sweep left %d import rows, want 0", rows)
	}

	after, err := svc.ReclaimStoredFiles(context.Background(), 2*time.Hour, false)
	if err != nil {
		t.Fatalf("reclaim after sweep: %v", err)
	}
	if after.Removed != 1 {
		t.Fatalf("reclaim removed %d files after the row sweep, want 1", after.Removed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file the swept row described survived the reclaim")
	}
}
