package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/geff1778/forgecrm/internal/database"
)

// ReconcileReport describes what a reconciliation pass found and did.
type ReconcileReport struct {
	// ScannedRows is the number of live document rows examined.
	ScannedRows int
	// MissingObjects are keys a live row points at that the store does not hold.
	MissingObjects []string
	// RepairedRows are rows marked archived because their object was missing. It is
	// populated only when the pass is not a dry run.
	RepairedRows int
	// ScannedObjects is the number of stored objects listed.
	ScannedObjects int
	// OrphanObjects are objects no row references.
	OrphanObjects []string
	// DeletedObjects are orphans that were removed. It is populated only when the
	// pass is not a dry run.
	DeletedObjects int
	// DryRun reports whether the pass changed anything.
	DryRun bool
}

// Reconcile finds and, unless it is a dry run, repairs the two ways the object
// store and the documents table can disagree.
//
// A row whose object is missing is a broken download. It is marked archived,
// which is the same state a successful archive leaves, so the API stops offering
// a file that cannot be served. A row is never deleted: the metadata is the record
// that the file existed, and the audit trail refers to it.
//
// An object no row references is an orphan left by an interrupted upload whose
// compensating delete failed. It is removed, but only once it is older than a
// grace period: a just-written object has no row yet for the brief window between
// the store returning and the insert committing, and deleting it would break a
// concurrent upload. The grace period makes the pass safe to run while the service
// is serving traffic.
func (s *Service) Reconcile(ctx context.Context, dryRun bool, grace time.Duration) (ReconcileReport, error) {
	if !s.Enabled() {
		return ReconcileReport{}, ErrNotConfigured
	}
	if grace <= 0 {
		grace = 24 * time.Hour
	}
	report := ReconcileReport{DryRun: dryRun}

	// Every organization is scanned: this is an operator tool run by a background
	// job, not a tenant request, so it deliberately crosses tenant boundaries. It
	// only ever sets the archived_at flag and removes unreferenced objects.
	rows, err := s.db.Q(ctx).Query(ctx,
		`SELECT storage_path FROM documents WHERE archived_at IS NULL`)
	if err != nil {
		return report, fmt.Errorf("list documents: %w", err)
	}

	var refs []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return report, fmt.Errorf("scan document: %w", err)
		}
		refs = append(refs, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}
	report.ScannedRows = len(refs)

	referenced := make(map[string]struct{}, len(refs))
	for _, key := range refs {
		referenced[key] = struct{}{}
		if _, err := s.store.Stat(ctx, key); err != nil {
			if !errors.Is(err, ErrObjectMissing) {
				return report, fmt.Errorf("stat %s: %w", key, err)
			}
			report.MissingObjects = append(report.MissingObjects, key)
			if !dryRun {
				if err := s.markArchivedByPath(ctx, key); err != nil {
					return report, err
				}
				report.RepairedRows++
			}
		}
	}

	objects, err := s.store.List(ctx, "org/")
	if err != nil {
		return report, fmt.Errorf("list objects: %w", err)
	}
	report.ScannedObjects = len(objects)

	cutoff := time.Now().Add(-grace)
	for _, obj := range objects {
		if _, ok := referenced[obj.Key]; ok {
			continue
		}
		if obj.LastModified.After(cutoff) {
			// Too new to judge: a concurrent upload may be between the store write and
			// the row insert.
			continue
		}
		report.OrphanObjects = append(report.OrphanObjects, obj.Key)
		if dryRun {
			continue
		}
		if err := s.store.Delete(ctx, obj.Key); err != nil {
			return report, fmt.Errorf("delete orphan %s: %w", obj.Key, err)
		}
		report.DeletedObjects++
	}
	return report, nil
}

func (s *Service) markArchivedByPath(ctx context.Context, key string) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		_, err := s.db.Q(ctx).Exec(ctx,
			`UPDATE documents SET archived_at = now() WHERE storage_path = $1 AND archived_at IS NULL`, key)
		if err != nil {
			return fmt.Errorf("archive an unreachable document: %w", err)
		}
		return nil
	})
}
