package objectstore

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// newFakeS3Store starts an in-process S3 wire-protocol server and points the real
// S3 client at it.
//
// gofakes3 speaks the S3 REST protocol over loopback; it is not a mock of the
// ObjectStore interface. That distinction matters: the code under test is the real
// minio-go client — request signing, path-style addressing, the 404-to-
// ErrObjectMissing translation, paginated listing and presigning — none of which
// an interface mock would exercise. It needs no external service, so it runs in
// the default `go test ./...` rather than behind the integration tag.
//
// What it does NOT cover is a real MinIO/AWS server: signature verification,
// IAM/ACL behaviour, server-side limits and network faults. Those are recorded as
// unverified in docs/EXTRACT_READY.md and are a separate, opt-in run.
func newFakeS3Store(t *testing.T, bucket string) ObjectStore {
	t.Helper()
	backend := s3mem.New()
	handler := gofakes3.New(backend, gofakes3.WithAutoBucket(true)).Server()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	store, err := NewS3(S3Config{
		Endpoint:        srv.URL,
		Region:          "us-east-1",
		Bucket:          bucket,
		AccessKeyID:     "test",
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

// TestS3SatisfiesTheContract runs the same suite as Memory against the real S3
// client over the wire protocol. A disagreement between the two runs is a real
// defect: it is exactly the change a caller sees when switching providers.
func TestS3SatisfiesTheContract(t *testing.T) {
	runContract(t, func(t *testing.T) ObjectStore { return newFakeS3Store(t, "contract") })
}
