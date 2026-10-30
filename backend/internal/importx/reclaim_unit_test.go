package importx

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestReclaimOnAMissingStorageDirIsNotAnError covers the fresh deployment: the
// private storage directory has never been created, so there is nothing to
// reclaim. Returning an error there would fail the scheduled job on a healthy
// system. It is a unit test because the early return happens before any database
// access, so no database is needed to prove it.
func TestReclaimOnAMissingStorageDirIsNotAnError(t *testing.T) {
	svc := &Service{storage: filepath.Join(t.TempDir(), "never-created")}
	report, err := svc.ReclaimStoredFiles(context.Background(), time.Hour, false)
	if err != nil {
		t.Fatalf("reclaim on a missing storage directory returned an error: %v", err)
	}
	if report.Scanned != 0 || report.Removed != 0 {
		t.Errorf("reported scanned=%d removed=%d for a missing directory, want 0 and 0",
			report.Scanned, report.Removed)
	}
}

// TestReclaimWithNoStorageConfiguredIsANoOp proves the pass is inert when the
// service was built without a storage directory, so it cannot be pointed at the
// filesystem root by a zero-value configuration.
func TestReclaimWithNoStorageConfiguredIsANoOp(t *testing.T) {
	svc := &Service{}
	report, err := svc.ReclaimStoredFiles(context.Background(), time.Hour, false)
	if err != nil {
		t.Fatalf("reclaim with no storage configured returned an error: %v", err)
	}
	if report.Scanned != 0 || report.Removed != 0 {
		t.Errorf("reported scanned=%d removed=%d with no storage, want 0 and 0",
			report.Scanned, report.Removed)
	}
}
