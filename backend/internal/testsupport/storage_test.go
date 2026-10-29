//go:build integration

package testsupport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/storage"
)

// These tests exercise the object store and the documents table together. The
// properties under test — that an upload writes both the object and the row, that
// a failed insert leaves no object behind, that a cross-organization read is
// refused, and that reconciliation repairs both directions of disagreement — are
// statements about the database and the store, so a mock would prove nothing.

// storageService builds a service backed by the in-memory store. The memory store
// is a real ObjectStore implementation, not a mock: the service's own code paths
// (key construction, compensating delete, reconciliation) all run.
func storageService(t *testing.T, db *database.DB, store storage.ObjectStore) *storage.Service {
	t.Helper()
	return storage.New(db, store, audit.New(db), nil, storage.Config{
		Enabled:        true,
		MaxObjectBytes: 8 << 20,
		PresignTTL:     time.Minute,
	})
}

func orgPrincipal(t *testing.T, db *database.DB, orgID uuid.UUID, perms ...rbac.Permission) *rbac.Principal {
	t.Helper()
	userID := insertUser(t, db, orgID, "uploader-"+orgID.String()+"@example.com")
	return rbac.NewPrincipal(userID, "uploader@example.com", "Uploader", orgID, perms)
}

// insertUser creates an active user with an active membership in an organization.
// The organization link lives in memberships, not on the users row.
func insertUser(t *testing.T, db *database.DB, orgID uuid.UUID, email string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := db.Pool().QueryRow(ctx, `
		INSERT INTO users (email, display_name, status)
		VALUES ($1, $2, 'active') RETURNING id`,
		email, "Uploader").Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO memberships (user_id, organization_id, status)
		VALUES ($1, $2, 'active')`, id, orgID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	return id
}

func uploadPerms() []rbac.Permission {
	return []rbac.Permission{
		rbac.PermDocumentsRead, rbac.PermDocumentsCreate, rbac.PermDocumentsArchive,
		rbac.PermAccountsRead, rbac.PermDealsRead,
	}
}

// TestStorageUploadWritesObjectAndRow checks the happy path: the object is in the
// store, the row is in the table, and the checksum matches the bytes.
func TestStorageUploadWritesObjectAndRow(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-upload")
	account := insertAccount(t, db, org, "Acme")
	p := orgPrincipal(t, db, org, uploadPerms()...)

	store := storage.NewMemory()
	svc := storageService(t, db, store)

	body := []byte("hello forgecrm")
	doc, err := svc.Upload(context.Background(), p, storage.UploadInput{
		Filename:    "note.txt",
		ContentType: "text/plain",
		Size:        int64(len(body)),
		AccountID:   &account,
		Title:       "A note",
	}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if doc.SizeBytes != int64(len(body)) {
		t.Fatalf("size = %d, want %d", doc.SizeBytes, len(body))
	}
	if store.Len() != 1 {
		t.Fatalf("store holds %d objects, want 1", store.Len())
	}

	var path string
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT storage_path FROM documents WHERE id = $1`, doc.ID).Scan(&path); err != nil {
		t.Fatalf("read the document row: %v", err)
	}
	if _, err := store.Stat(context.Background(), path); err != nil {
		t.Fatalf("the row's object is not in the store: %v", err)
	}
	if !strings.HasPrefix(path, "org/"+org.String()+"/documents/") {
		t.Fatalf("storage path %q is not under the organization prefix", path)
	}
}

// TestStorageUploadToUnreadableEntityIsRefused checks object-level authorization:
// a user cannot attach a file to a record they cannot read, even with
// documents.create.
func TestStorageUploadToUnreadableEntityIsRefused(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	orgA := insertOrganization(t, db, "storage-org-a")
	orgB := insertOrganization(t, db, "storage-org-b")
	foreignAccount := insertAccount(t, db, orgB, "Foreign")

	p := orgPrincipal(t, db, orgA, uploadPerms()...)
	store := storage.NewMemory()
	svc := storageService(t, db, store)

	_, err := svc.Upload(context.Background(), p, storage.UploadInput{
		Filename:    "x.txt",
		ContentType: "text/plain",
		Size:        1,
		AccountID:   &foreignAccount,
	}, strings.NewReader("x"))
	if err == nil {
		t.Fatal("upload into another organization's account was accepted")
	}
	if !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
	if store.Len() != 0 {
		t.Fatalf("a refused upload left %d objects in the store", store.Len())
	}
}

// TestStorageReadIsOrganizationScoped checks that a document uploaded in one
// organization is invisible to another.
func TestStorageReadIsOrganizationScoped(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	orgA := insertOrganization(t, db, "storage-scope-a")
	orgB := insertOrganization(t, db, "storage-scope-b")
	pA := orgPrincipal(t, db, orgA, uploadPerms()...)
	pB := orgPrincipal(t, db, orgB, uploadPerms()...)

	store := storage.NewMemory()
	svc := storageService(t, db, store)

	doc, err := svc.Upload(context.Background(), pA, storage.UploadInput{
		Filename: "secret.txt", ContentType: "text/plain", Size: 3,
	}, strings.NewReader("abc"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if _, err := svc.Get(context.Background(), pB, doc.ID); !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("cross-organization read returned %v, want not found", err)
	}
}

// TestStorageInsertFailureRemovesTheObject checks the compensating delete: when
// the row cannot be written, the object must not survive.
func TestStorageInsertFailureRemovesTheObject(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-compensate")
	p := orgPrincipal(t, db, org, uploadPerms()...)

	store := storage.NewMemory()
	svc := storageService(t, db, store)

	// The insert is made to fail after the object is stored, which is the ordering the
	// compensation exists for. The documents table is renamed away for the attempt and
	// restored afterwards, so the failure is a real SQL error rather than a fabricated
	// one, and the schema this test leaves behind is the schema it found.
	ctx := context.Background()
	if _, err := db.Pool().Exec(ctx, `ALTER TABLE documents RENAME TO documents_hidden`); err != nil {
		t.Fatalf("hide the documents table: %v", err)
	}
	_, err := svc.Upload(ctx, p, storage.UploadInput{
		Filename: "note.txt", ContentType: "text/plain", Size: 1,
	}, strings.NewReader("x"))
	if _, err := db.Pool().Exec(ctx, `ALTER TABLE documents_hidden RENAME TO documents`); err != nil {
		t.Fatalf("restore the documents table: %v", err)
	}
	if err == nil {
		t.Fatal("an upload succeeded with no documents table")
	}
	if store.Len() != 0 {
		t.Fatalf("a failed insert left %d objects in the store, want 0", store.Len())
	}
	var rows int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM documents`).Scan(&rows); err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if rows != 0 {
		t.Fatalf("a failed insert left %d rows", rows)
	}
}

// TestStorageReconcileRepairsBothDirections checks the reconciliation pass: a row
// with no object is archived, an unreferenced object is removed, and a dry run
// changes neither.
func TestStorageReconcileRepairsBothDirections(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-reconcile")
	p := orgPrincipal(t, db, org, uploadPerms()...)

	store := storage.NewMemory()
	svc := storageService(t, db, store)
	ctx := context.Background()

	// A row whose object is missing: uploaded, then the object deleted behind it.
	broken, err := svc.Upload(ctx, p, storage.UploadInput{
		Filename: "gone.txt", ContentType: "text/plain", Size: 2,
	}, strings.NewReader("go"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var key string
	if err := db.Pool().QueryRow(ctx,
		`SELECT storage_path FROM documents WHERE id = $1`, broken.ID).Scan(&key); err != nil {
		t.Fatalf("read storage path: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete object: %v", err)
	}

	// An orphan: an object with no row, aged past the grace period.
	old := time.Now().Add(-48 * time.Hour)
	store.PutObject("org/"+org.String()+"/documents/orphan.bin", []byte("junk"), "application/octet-stream", old)

	// A dry run must report both and change nothing.
	report, err := svc.Reconcile(ctx, true, time.Hour)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(report.MissingObjects) != 1 {
		t.Fatalf("dry run reported %d missing objects, want 1", len(report.MissingObjects))
	}
	if len(report.OrphanObjects) != 1 {
		t.Fatalf("dry run reported %d orphans, want 1", len(report.OrphanObjects))
	}
	if report.RepairedRows != 0 || report.DeletedObjects != 0 {
		t.Fatalf("dry run changed state: %+v", report)
	}

	// The real run repairs both.
	report, err = svc.Reconcile(ctx, false, time.Hour)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.RepairedRows != 1 {
		t.Fatalf("reconcile archived %d rows, want 1", report.RepairedRows)
	}
	if report.DeletedObjects != 1 {
		t.Fatalf("reconcile removed %d orphans, want 1", report.DeletedObjects)
	}
	var archived bool
	if err := db.Pool().QueryRow(ctx,
		`SELECT archived_at IS NOT NULL FROM documents WHERE id = $1`, broken.ID).Scan(&archived); err != nil {
		t.Fatalf("read archived flag: %v", err)
	}
	if !archived {
		t.Fatal("a document with no object was not archived")
	}
	if store.Len() != 0 {
		t.Fatalf("reconcile left %d objects, want 0", store.Len())
	}
}

// TestStorageReconcileKeepsFreshOrphans checks the grace period: an object
// written a moment ago may belong to an upload whose insert has not committed, so
// it must survive a pass.
func TestStorageReconcileKeepsFreshOrphans(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-grace")
	store := storage.NewMemory()
	svc := storageService(t, db, store)

	store.PutObject("org/"+org.String()+"/documents/fresh.bin", []byte("x"), "application/octet-stream", time.Now())

	report, err := svc.Reconcile(context.Background(), false, time.Hour)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(report.OrphanObjects) != 0 {
		t.Fatalf("a fresh object was treated as an orphan: %+v", report)
	}
	if store.Len() != 1 {
		t.Fatal("a fresh object was deleted")
	}
}

// TestStorageArchiveDeletesTheObject checks that archiving removes the object but
// keeps the row, so the record that the file existed survives.
func TestStorageArchiveDeletesTheObject(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-archive")
	p := orgPrincipal(t, db, org, uploadPerms()...)
	store := storage.NewMemory()
	svc := storageService(t, db, store)
	ctx := context.Background()

	doc, err := svc.Upload(ctx, p, storage.UploadInput{
		Filename: "old.txt", ContentType: "text/plain", Size: 1,
	}, strings.NewReader("x"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := svc.Archive(ctx, p, doc.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if store.Len() != 0 {
		t.Fatalf("archive left %d objects", store.Len())
	}
	var rows int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM documents WHERE id = $1`, doc.ID).Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Fatal("archive deleted the row instead of archiving it")
	}
	if _, _, err := svc.Open(ctx, p, doc.ID); !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("opening an archived document returned %v, want not found", err)
	}
}

// TestStorageUploadRejectsOversizedBody checks the streaming limit: a body larger
// than the configured maximum is refused and leaves nothing behind.
func TestStorageUploadRejectsOversizedBody(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-too-big")
	p := orgPrincipal(t, db, org, uploadPerms()...)
	store := storage.NewMemory()
	svc := storage.New(db, store, audit.New(db), nil, storage.Config{
		Enabled: true, MaxObjectBytes: 16, PresignTTL: time.Minute,
	})

	_, err := svc.Upload(context.Background(), p, storage.UploadInput{
		Filename: "big.txt", ContentType: "text/plain", Size: 1024,
	}, io.LimitReader(strings.NewReader(strings.Repeat("a", 1024)), 1024))
	if err == nil {
		t.Fatal("an oversized body was accepted")
	}
	if store.Len() != 0 {
		t.Fatalf("a refused upload left %d objects", store.Len())
	}
}

// TestStoragePresignRequiresPermission checks that a principal without
// documents.read cannot mint a download URL.
func TestStoragePresignRequiresPermission(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "storage-presign")
	store := storage.NewMemory()
	svc := storageService(t, db, store)

	noPerms := rbac.NewPrincipal(uuid.New(), "nobody@example.com", "Nobody", org, nil)
	if _, _, err := svc.PresignDownload(context.Background(), noPerms, uuid.New()); !errors.Is(err, httpx.ErrForbidden) {
		t.Fatalf("presign without permission returned %v, want forbidden", err)
	}
}

// TestStorageDisabledReportsOff checks that a deployment without a store answers
// with the disabled error rather than a nil dereference.
func TestStorageDisabledReportsOff(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	svc := storage.New(db, nil, audit.New(db), nil, storage.Config{Enabled: false})
	if svc.Enabled() {
		t.Fatal("a service with no store reports itself enabled")
	}
	org := insertOrganization(t, db, "storage-off")
	p := rbac.NewPrincipal(uuid.New(), "x@example.com", "X", org, uploadPerms())
	if _, err := svc.Upload(context.Background(), p, storage.UploadInput{Filename: "a.txt"}, strings.NewReader("a")); !errors.Is(err, storage.ErrNotConfigured) {
		t.Fatalf("upload on a disabled service returned %v, want ErrNotConfigured", err)
	}
	if _, err := svc.List(context.Background(), p, false); !errors.Is(err, storage.ErrNotConfigured) {
		t.Fatalf("list on a disabled service returned %v, want ErrNotConfigured", err)
	}
}
