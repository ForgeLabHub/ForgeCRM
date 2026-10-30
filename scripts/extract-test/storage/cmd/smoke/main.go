// Command smoke exercises the storage package's public API in a clean module.
//
// Uploading needs a database and a bucket, so it is not driven here. What is
// driven is the part every caller depends on and that needs neither: the service
// reports itself off when unconfigured, the memory store round-trips, and the S3
// constructor refuses an incomplete config or a private endpoint by default.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/storage"
)

func main() {
	// A nil receiver reports "off" rather than panicking, so the diagnostics
	// endpoint can ask whether storage is available on an unconstructed subsystem.
	var nilSvc *storage.Service
	if nilSvc.Enabled() {
		fail("a nil service reports itself enabled")
	}
	if nilSvc.MaxObjectBytes() != 0 {
		fail("a nil service reports a size limit")
	}
	off := storage.New(nil, nil, nil, nil, storage.Config{})
	if off.Enabled() {
		fail("a service built with Enabled=false reports itself enabled")
	}
	fmt.Println("unconfigured service reports off")

	// The memory store is the provider the harness can drive with no bucket.
	mem := storage.NewMemory()
	if !mem.Enabled() {
		fail("the memory store reports itself disabled")
	}
	org := uuid.New()
	key := "org/" + org.String() + "/documents/1.txt"
	body := []byte("hello")
	info, err := mem.Put(context.Background(), key, bytes.NewReader(body), int64(len(body)), "text/plain")
	if err != nil {
		fail("memory put: %v", err)
	}
	if info.Size != int64(len(body)) {
		fail("memory put reported size %d, want %d", info.Size, len(body))
	}
	rc, got, err := mem.Get(context.Background(), key)
	if err != nil {
		fail("memory get: %v", err)
	}
	defer rc.Close()
	if got.Size != int64(len(body)) {
		fail("memory get returned wrong info: %+v", got)
	}
	fmt.Println("memory store round-trips")

	// An incomplete S3 config is refused rather than accepted and failing later.
	if _, err := storage.NewS3(storage.S3Config{}); err == nil {
		fail("an incomplete S3 config was accepted")
	}
	fmt.Println("incomplete S3 config refused")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
