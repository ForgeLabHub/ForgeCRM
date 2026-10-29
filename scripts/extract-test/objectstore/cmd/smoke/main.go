// Command smoke exercises the extracted objectstore package through the in-memory
// provider: store an object, read it back, list it, delete it.
//
// It proves the module runs on its own. The S3 provider is exercised by the
// package's own contract test (s3_contract_test.go), which runs in the extracted
// module's `go test` against an in-process S3 wire-protocol server.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/geff1778/forgecrm/internal/objectstore"
)

func main() {
	ctx := context.Background()
	store := objectstore.NewMemory()

	const key = "org/11111111-1111-1111-1111-111111111111/documents/22222222-2222-2222-2222-222222222222.txt"
	body := []byte("extraction smoke")

	if _, err := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		fail("put", err)
	}
	rc, _, err := store.Get(ctx, key)
	if err != nil {
		fail("get", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		fail("read", err)
	}
	if !bytes.Equal(got, body) {
		fmt.Fprintf(os.Stderr, "round trip mismatch: %q\n", got)
		os.Exit(1)
	}
	list, err := store.List(ctx, "org/")
	if err != nil {
		fail("list", err)
	}
	if _, err := store.PresignGet(ctx, key, time.Minute); err != nil {
		fail("presign", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		fail("delete", err)
	}
	fmt.Printf("smoke: roundtrip=%d listed=%d\n", len(got), len(list))
}

func fail(step string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", step, err)
	os.Exit(1)
}
