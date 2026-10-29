package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// These unit tests cover the pure parts of the storage layer: key construction,
// the extension and content-type guards, and the in-memory store's contract. They
// need no database, so they run in the default `go test ./...`.

func TestObjectKeyIsTenantScopedAndIgnoresTheClientName(t *testing.T) {
	org := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	doc := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	// A hostile filename must not change the key beyond a safe extension.
	key := objectKey(org, doc, "../../../etc/passwd")
	if !strings.HasPrefix(key, "org/"+org.String()+"/documents/") {
		t.Fatalf("key %q is not under the tenant prefix", key)
	}
	if strings.Contains(key, "..") || strings.Contains(key, "passwd") {
		t.Fatalf("key %q leaked the client filename", key)
	}
	if !strings.HasSuffix(key, doc.String()) {
		t.Fatalf("key %q does not end in the document id", key)
	}
}

func TestObjectKeyKeepsASafeExtension(t *testing.T) {
	org := uuid.New()
	doc := uuid.New()
	key := objectKey(org, doc, "report.PDF")
	if !strings.HasSuffix(key, ".pdf") {
		t.Fatalf("key %q did not keep the lowercased extension", key)
	}
	// A very long or dotted extension is dropped rather than truncated into the key.
	if got := objectKey(org, doc, "x.averylongextensionname"); strings.Contains(got, "averylong") {
		t.Fatalf("key %q kept an unsafe extension", got)
	}
}

func TestIsSafeExtension(t *testing.T) {
	for _, ext := range []string{".txt", ".pdf", ".tar", ".gz"} {
		if !isSafeExtension(ext) {
			t.Errorf("isSafeExtension(%q) = false, want true", ext)
		}
	}
	for _, ext := range []string{"", ".", "x", ".a/b", ".UPPER", ".a-b", "..", ".averylongextension"} {
		if isSafeExtension(ext) {
			t.Errorf("isSafeExtension(%q) = true, want false", ext)
		}
	}
}

func TestCheckContentTypeRefusesExecutablesByDefault(t *testing.T) {
	svc := &Service{cfg: Config{}}
	if err := svc.checkContentType("application/x-msdownload"); err == nil {
		t.Fatal("an executable was accepted with no allow-list")
	}
	if err := svc.checkContentType("image/png"); err != nil {
		t.Fatalf("a png was refused with no allow-list: %v", err)
	}

	svc.cfg.AllowedContentTypes = []string{"image/png"}
	if err := svc.checkContentType("text/plain"); err == nil {
		t.Fatal("a type outside the allow-list was accepted")
	}
	if err := svc.checkContentType("image/png"); err != nil {
		t.Fatalf("an allow-listed type was refused: %v", err)
	}
}

func TestMemoryStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()

	if !store.Enabled() {
		t.Fatal("a new memory store reports itself off")
	}
	if _, err := store.Stat(ctx, "absent"); err == nil {
		t.Fatal("stat of an absent object succeeded")
	}

	body := strings.NewReader("payload")
	info, err := store.Put(ctx, "org/a/documents/1.txt", body, 7, "text/plain")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if info.Size != 7 {
		t.Fatalf("size = %d, want 7", info.Size)
	}

	rc, _, err := store.Get(ctx, "org/a/documents/1.txt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	rc.Close()

	list, err := store.List(ctx, "org/a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list returned %d objects, want 1", len(list))
	}

	u, err := store.PresignGet(ctx, "org/a/documents/1.txt", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if u == nil || !strings.Contains(u.String(), "org/a/documents/1.txt") {
		t.Fatalf("presigned url = %v", u)
	}

	if err := store.Delete(ctx, "org/a/documents/1.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if store.Len() != 0 {
		t.Fatal("delete left the object")
	}
}

func TestEntityRefAcceptsAtMostOneEntity(t *testing.T) {
	a := uuid.New()
	b := uuid.New()

	if _, _, err := (UploadInput{AccountID: &a, DealID: &b}).entityRef(); err == nil {
		t.Fatal("two entity ids were accepted")
	}
	entityType, id, err := (UploadInput{AccountID: &a}).entityRef()
	if err != nil {
		t.Fatalf("one entity id was refused: %v", err)
	}
	if entityType != EntityAccount || id == nil || *id != a {
		t.Fatalf("entityRef = %q, %v", entityType, id)
	}
	if _, _, err := (UploadInput{}).entityRef(); err != nil {
		t.Fatalf("no entity id was refused: %v", err)
	}
}

func TestDisabledServiceReportsOff(t *testing.T) {
	svc := New(nil, nil, nil, nil, Config{Enabled: false})
	if svc.Enabled() {
		t.Fatal("a service with no store reports itself enabled")
	}
	if _, err := svc.List(context.Background(), nil, false); err != ErrNotConfigured {
		t.Fatalf("list on a disabled service = %v, want ErrNotConfigured", err)
	}
}

func TestNewS3RejectsAnIncompleteConfig(t *testing.T) {
	if _, err := NewS3(S3Config{Bucket: "b"}); err == nil {
		t.Fatal("an S3 store with no endpoint was built")
	}
	if _, err := NewS3(S3Config{Endpoint: "https://s3.example.com"}); err == nil {
		t.Fatal("an S3 store with no bucket was built")
	}
}

func TestNewS3RefusesAPrivateEndpointByDefault(t *testing.T) {
	_, err := NewS3(S3Config{
		Endpoint: "http://127.0.0.1:9000",
		Bucket:   "b",
	})
	if err == nil {
		t.Fatal("a loopback endpoint was accepted without the explicit opt-in")
	}
	if !strings.Contains(err.Error(), "private") && !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error %q does not explain the endpoint was refused", err)
	}
}

func TestUploadRejectsTwoEntities(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	svc := New(nil, NewMemory(), nil, nil, Config{Enabled: true, MaxObjectBytes: 1024})
	p := rbac.NewPrincipal(uuid.New(), "u@example.com", "U", uuid.New(),
		[]rbac.Permission{rbac.PermDocumentsCreate})
	_, err := svc.Upload(context.Background(), p, UploadInput{
		Filename: "x.txt", AccountID: &a, ContactID: &b,
	}, strings.NewReader("x"))
	if err == nil {
		t.Fatal("an upload naming two entities was accepted")
	}
	var apiErr *httpx.APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

func asAPIError(err error, target **httpx.APIError) bool {
	for err != nil {
		if e, ok := err.(*httpx.APIError); ok {
			*target = e
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
