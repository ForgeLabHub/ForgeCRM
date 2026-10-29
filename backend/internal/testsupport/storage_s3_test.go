//go:build integration

package testsupport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/storage"
)

// These tests drive the real S3 client through a real S3 wire protocol server
// (gofakes3, an in-process S3 implementation). The point is to exercise the code
// that the in-memory store does not: request signing, path-style addressing, the
// error translation from an S3 status code, paginated listing and presigning. A
// mock of ObjectStore would skip every one of those.

// newFakeS3 starts an in-process S3 server and returns a store pointed at it.
func newFakeS3(t *testing.T, bucket string) *storage.S3 {
	t.Helper()
	backend := s3mem.New()
	handler := gofakes3.New(backend, gofakes3.WithAutoBucket(true)).Server()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	store, err := storage.NewS3(storage.S3Config{
		Endpoint:    srv.URL,
		Region:      "us-east-1",
		Bucket:      bucket,
		AccessKeyID: "test",
		// A non-empty secret is required for signing even though the fake server does
		// not verify the signature.
		SecretAccessKey: "test-secret",
		UsePathStyle:    true,
		// The fake server listens on loopback, which the default guard refuses.
		AllowPrivateEndpoint: true,
		Timeout:              10 * time.Second,
	})
	if err != nil {
		t.Fatalf("build the S3 store: %v", err)
	}
	if err := store.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}
	return store
}

// TestS3RoundTrip drives put, stat, get, list and delete through the client.
func TestS3RoundTrip(t *testing.T) {
	store := newFakeS3(t, "round-trip")
	ctx := context.Background()

	body := []byte("the quick brown fox")
	info, err := store.Put(ctx, "org/x/documents/one.txt", bytes.NewReader(body), int64(len(body)), "text/plain")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if info.Size != int64(len(body)) {
		t.Fatalf("put reported size %d, want %d", info.Size, len(body))
	}

	stat, err := store.Stat(ctx, "org/x/documents/one.txt")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.Size != int64(len(body)) {
		t.Fatalf("stat size = %d, want %d", stat.Size, len(body))
	}

	rc, _, err := store.Get(ctx, "org/x/documents/one.txt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("object body = %q, want %q", got, body)
	}

	// A second object so the listing has to return more than one entry.
	if _, err := store.Put(ctx, "org/x/documents/two.bin", strings.NewReader("22"), 2, "application/octet-stream"); err != nil {
		t.Fatalf("put second: %v", err)
	}
	list, err := store.List(ctx, "org/x/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list returned %d objects, want 2", len(list))
	}

	if err := store.Delete(ctx, "org/x/documents/one.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Stat(ctx, "org/x/documents/one.txt"); err == nil {
		t.Fatal("stat after delete succeeded")
	}
}

// TestS3MissingObjectIsNamed checks the error translation: a 404 from the server
// becomes ErrObjectMissing, which the handler turns into a 404 rather than a 500.
func TestS3MissingObjectIsNamed(t *testing.T) {
	store := newFakeS3(t, "missing")
	if _, err := store.Stat(context.Background(), "org/none/documents/absent.txt"); err == nil {
		t.Fatal("stat of a missing object succeeded")
	} else if !strings.Contains(err.Error(), storage.ErrObjectMissing.Error()) &&
		!errorIs(err, storage.ErrObjectMissing) {
		t.Fatalf("missing object error = %v, want ErrObjectMissing", err)
	}
}

// TestS3PresignServesTheObject checks that a presigned URL actually returns the
// bytes: a link that does not work is worse than no link.
func TestS3PresignServesTheObject(t *testing.T) {
	store := newFakeS3(t, "presign")
	ctx := context.Background()
	body := []byte("presigned payload")
	if _, err := store.Put(ctx, "org/p/documents/doc.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}

	u, err := store.PresignGet(ctx, "org/p/documents/doc.txt", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	resp, err := http.Get(u.String())
	if err != nil {
		t.Fatalf("fetch presigned url: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned url returned %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, body) {
		t.Fatalf("presigned body = %q, want %q", got, body)
	}
}

// TestS3ServiceOverRealStore runs the whole service against the fake S3 server, so
// the key construction, checksum, insert and object all pass through real code.
func TestS3ServiceOverRealStore(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)

	org := insertOrganization(t, db, "s3-service")
	account := insertAccount(t, db, org, "S3 Co")
	p := orgPrincipal(t, db, org, uploadPerms()...)

	store := newFakeS3(t, "service")
	svc := storage.New(db, store, audit.New(db), nil, storage.Config{
		Enabled: true, MaxObjectBytes: 4 << 20, PresignTTL: time.Minute,
	})
	ctx := context.Background()

	body := []byte("attached through the real client")
	doc, err := svc.Upload(ctx, p, storage.UploadInput{
		Filename: "report.pdf", ContentType: "application/pdf",
		Size: int64(len(body)), AccountID: &account,
	}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, rc, err := svc.Open(ctx, p, doc.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded body = %q, want %q", got, body)
	}

	if err := svc.Archive(ctx, p, doc.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := store.Stat(ctx, doc.StoragePath); err == nil {
		t.Fatal("archive left the object in the bucket")
	}
}

func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
