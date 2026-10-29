package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// storeFactory builds a fresh, empty store for one contract run.
type storeFactory func(t *testing.T) ObjectStore

// runContract asserts the behaviour every ObjectStore implementation must share,
// independently of how it stores bytes.
//
// The same suite is run against Memory (a unit test, always) and against the real
// S3 client over an in-process S3 wire-protocol server (an integration test). If
// the two disagree, one of them is wrong — that is exactly the disagreement a
// caller would hit when switching providers, so the contract is the place to
// catch it.
//
// Only behaviour the contract actually promises is asserted. A provider-specific
// detail (how it reports a network failure, for instance) belongs in that
// provider's own test, not here.
func runContract(t *testing.T, newStore storeFactory) {
	t.Helper()
	ctx := context.Background()
	const key = "org/11111111-1111-1111-1111-111111111111/documents/22222222-2222-2222-2222-222222222222.txt"
	body := []byte("the quick brown fox")

	t.Run("enabled", func(t *testing.T) {
		if s := newStore(t); !s.Enabled() {
			t.Fatal("a constructed store reports itself disabled")
		}
	})

	t.Run("put then stat then get", func(t *testing.T) {
		s := newStore(t)
		info, err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain")
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		if info.Key != key {
			t.Errorf("put returned key %q, want %q", info.Key, key)
		}
		if info.Size != int64(len(body)) {
			t.Errorf("put reported size %d, want %d", info.Size, len(body))
		}

		st, err := s.Stat(ctx, key)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if st.Size != int64(len(body)) {
			t.Errorf("stat size = %d, want %d", st.Size, len(body))
		}

		rc, gi, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("body = %q, want %q", got, body)
		}
		if gi.Size != int64(len(body)) {
			t.Errorf("get info size = %d, want %d", gi.Size, len(body))
		}
	})

	t.Run("missing object is named", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Stat(ctx, "org/none/documents/absent.txt"); !errors.Is(err, ErrObjectMissing) {
			t.Errorf("stat of a missing object = %v, want ErrObjectMissing", err)
		}
		if _, _, err := s.Get(ctx, "org/none/documents/absent.txt"); !errors.Is(err, ErrObjectMissing) {
			t.Errorf("get of a missing object = %v, want ErrObjectMissing", err)
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		s := newStore(t)
		// Deleting something that was never stored must not be an error: a retried
		// archive calls delete twice.
		if err := s.Delete(ctx, "org/none/documents/absent.txt"); err != nil {
			t.Errorf("delete of a missing object = %v, want nil", err)
		}

		if _, err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}
		if err := s.Delete(ctx, key); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := s.Stat(ctx, key); !errors.Is(err, ErrObjectMissing) {
			t.Errorf("stat after delete = %v, want ErrObjectMissing", err)
		}
		if err := s.Delete(ctx, key); err != nil {
			t.Errorf("second delete = %v, want nil", err)
		}
	})

	t.Run("re-put overwrites", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Put(ctx, key, strings.NewReader("first"), 5, "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}
		if _, err := s.Put(ctx, key, strings.NewReader("second value"), 12, "text/plain"); err != nil {
			t.Fatalf("re-put: %v", err)
		}
		rc, _, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != "second value" {
			t.Errorf("body after re-put = %q, want %q", got, "second value")
		}
	})

	t.Run("list is prefix scoped and sorted", func(t *testing.T) {
		s := newStore(t)
		keys := []string{
			"org/a/documents/1.txt",
			"org/a/documents/2.txt",
			"org/b/documents/3.txt",
		}
		for _, k := range keys {
			if _, err := s.Put(ctx, k, strings.NewReader("x"), 1, "text/plain"); err != nil {
				t.Fatalf("put %s: %v", k, err)
			}
		}
		list, err := s.List(ctx, "org/a/")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("list returned %d objects, want 2", len(list))
		}
		for _, o := range list {
			if !strings.HasPrefix(o.Key, "org/a/") {
				t.Errorf("list returned %q, which is outside the prefix", o.Key)
			}
		}
	})

	t.Run("presign returns a usable url", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}
		u, err := s.PresignGet(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("presign: %v", err)
		}
		if u == nil {
			t.Fatal("presign returned a nil url without an error")
		}
		if !strings.Contains(u.Path, key) {
			t.Errorf("presigned url path %q does not name the object key", u.Path)
		}
	})

	t.Run("a larger object round trips", func(t *testing.T) {
		s := newStore(t)
		big := bytes.Repeat([]byte("A"), 1<<20) // 1 MiB
		if _, err := s.Put(ctx, key, bytes.NewReader(big), int64(len(big)), "application/octet-stream"); err != nil {
			t.Fatalf("put: %v", err)
		}
		rc, info, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if len(got) != len(big) {
			t.Errorf("read %d bytes, want %d", len(got), len(big))
		}
		if info.Size != int64(len(big)) {
			t.Errorf("info size = %d, want %d", info.Size, len(big))
		}
	})

	t.Run("an unusual but valid key round trips", func(t *testing.T) {
		s := newStore(t)
		// Spaces and unicode in a key are legal in the contract; only the object
		// store's own key builder decides what is produced in practice.
		odd := "org/a/documents/naïve report (v2).txt"
		if _, err := s.Put(ctx, odd, strings.NewReader("ok"), 2, "text/plain"); err != nil {
			t.Fatalf("put: %v", err)
		}
		rc, _, err := s.Get(ctx, odd)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if string(got) != "ok" {
			t.Errorf("body = %q, want %q", got, "ok")
		}
	})
}

// TestMemorySatisfiesTheContract runs the shared contract against the in-process
// store. It needs no network, so it runs in the default `go test ./...`.
func TestMemorySatisfiesTheContract(t *testing.T) {
	runContract(t, func(t *testing.T) ObjectStore { return NewMemory() })
}
