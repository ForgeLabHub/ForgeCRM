//go:build integration

// Integration test for the worker's storage reconcile handler. It needs a real
// PostgreSQL server and the in-memory object store, because the property under test
// is that the periodic pass repairs a disagreement between the documents table and
// the store — a claim about the two together, not about either alone.
//
//	FORGECRM_TEST_DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable \
//	  go test -tags=integration ./cmd/forgecrm-worker/...
package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/retention"
	"github.com/geff1778/forgecrm/internal/storage"
)

// reconcileRegistry builds just enough of a registry to drive storageReconcile.
func reconcileRegistry(t *testing.T, db *database.DB, svc *storage.Service, runner *retention.Runner) *handlerRegistry {
	t.Helper()
	return &handlerRegistry{
		db:        db,
		log:       slog.New(slog.NewTextHandler(discard{}, nil)),
		storage:   svc,
		retention: runner,
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// TestWorkerStorageReconcileRepairsADisagreement is the guard for the gap this
// handler closes: the storage reconcile service method existed and was tested, but
// no production path ever called it, so an orphan object and a row whose object was
// gone both stayed wrong forever. The test drives the worker's own handler, not the
// service method, so it fails if the wiring is removed.
func TestWorkerStorageReconcileRepairsADisagreement(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrg(t, db, "storage-reconcile-worker")
	user := insertUser(t, db, org)

	store := storage.NewMemory()
	svc := storage.New(db, store, audit.New(db), nil, storage.Config{
		Enabled: true, MaxObjectBytes: 8 << 20, PresignTTL: time.Minute,
	})
	p := rbac.NewPrincipal(user, "u@example.com", "U", org, []rbac.Permission{rbac.PermDocumentsRead, rbac.PermDocumentsCreate, rbac.PermDocumentsArchive})

	// A live row whose object is deleted behind it.
	broken, err := svc.Upload(ctx, p, storage.UploadInput{Filename: "gone.txt", ContentType: "text/plain", Size: 2}, strings.NewReader("go"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var key string
	if err := db.Pool().QueryRow(ctx, `SELECT storage_path FROM documents WHERE id = $1`, broken.ID).Scan(&key); err != nil {
		t.Fatalf("read storage path: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	// An orphan object, aged past the grace period.
	store.PutObject("org/"+org.String()+"/documents/orphan.bin", []byte("junk"), "application/octet-stream", time.Now().Add(-48*time.Hour))

	// A runner that deletes is the "opted in" posture, so the handler runs for real.
	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: true})
	reg := reconcileRegistry(t, db, svc, runner)

	if err := reg.storageReconcile(ctx); err != nil {
		t.Fatalf("storageReconcile: %v", err)
	}

	var archived bool
	if err := db.Pool().QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM documents WHERE id = $1`, broken.ID).Scan(&archived); err != nil {
		t.Fatalf("read archived flag: %v", err)
	}
	if !archived {
		t.Fatal("the worker did not archive a document whose object was missing")
	}
	if store.Len() != 0 {
		t.Fatalf("the worker left %d objects, want 0", store.Len())
	}
}

// TestWorkerStorageReconcileIsADryRunWhenRetentionIs checks the safety property: a
// deployment that has not opted into deletion must not have an orphan object removed
// by the maintenance pass, so the handler inherits the retention engine's dry-run
// posture rather than deciding for itself.
func TestWorkerStorageReconcileIsADryRunWhenRetentionIs(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	ctx := context.Background()
	org := insertOrg(t, db, "storage-reconcile-dryrun")

	store := storage.NewMemory()
	svc := storage.New(db, store, audit.New(db), nil, storage.Config{Enabled: true, MaxObjectBytes: 8 << 20, PresignTTL: time.Minute})
	store.PutObject("org/"+org.String()+"/documents/orphan.bin", []byte("junk"), "application/octet-stream", time.Now().Add(-48*time.Hour))

	// Retention enabled but in dry-run: nothing should be deleted.
	runner := retention.New(db, audit.New(db), nil, retention.Options{Enabled: true, Delete: false})
	reg := reconcileRegistry(t, db, svc, runner)

	if err := reg.storageReconcile(ctx); err != nil {
		t.Fatalf("storageReconcile: %v", err)
	}
	if store.Len() != 1 {
		t.Fatalf("a dry-run pass removed an object: %d left, want 1", store.Len())
	}
}

// TestWorkerStorageReconcileIsANoOpWhenStorageIsOff checks that a deployment with no
// object storage runs the periodic job harmlessly rather than failing it every hour.
func TestWorkerStorageReconcileIsANoOpWhenStorageIsOff(t *testing.T) {
	svc := storage.New(nil, nil, nil, nil, storage.Config{Enabled: false})
	reg := &handlerRegistry{log: slog.New(slog.NewTextHandler(discard{}, nil)), storage: svc}
	if err := reg.storageReconcile(context.Background()); err != nil {
		t.Fatalf("a disabled store should be a no-op, got %v", err)
	}
}

func insertOrg(t *testing.T, db *database.DB, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&id); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	return id
}

func insertUser(t *testing.T, db *database.DB, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO users (email, display_name, status) VALUES ($1, 'U', 'active') RETURNING id`,
		"u-"+orgID.String()+"@example.com").Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO memberships (user_id, organization_id, status) VALUES ($1, $2, 'active')`, id, orgID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	return id
}
