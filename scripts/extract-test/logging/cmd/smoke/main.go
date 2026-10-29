// Command smoke exercises the extracted logging package: build a logger with the
// package's own constructor and check that the redaction control removes a secret
// from the output.
//
// It proves the module runs on its own, and it asserts the property the package
// exists for rather than merely that it compiles. New writes to stdout, so the
// smoke captures stdout for the duration of the check.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/geff1778/forgecrm/internal/logging"
)

func main() {
	// Context helpers carry request identity.
	ctx := logging.WithCorrelationID(context.Background(), "corr-smoke")
	if logging.CorrelationID(ctx) != "corr-smoke" {
		fail("context", "correlation id did not round trip")
	}

	// The redaction decision is the security property.
	if !logging.IsSensitiveKey("db_password") {
		fail("redact", "a password key was not classified as sensitive")
	}
	if logging.IsSensitiveKey("company_name") {
		fail("redact", "a benign key was classified as sensitive")
	}

	// Render through the real constructor and assert the secret is gone.
	out := captureStdout(func() {
		logger := logging.New("debug", "json")
		logger.Info("login", "email", "a@b.c", "password", "smoke-secret-value")
	})
	if strings.Contains(out, "smoke-secret-value") {
		fail("redact", "the secret appeared in the log output: "+out)
	}
	if !strings.Contains(out, "a@b.c") {
		fail("redact", "a benign field was dropped: "+out)
	}
	fmt.Println("smoke: correlation roundtrip=1 secret redacted=1 benign kept=1")
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what was
// written.
func captureStdout(fn func()) string {
	r, w, err := os.Pipe()
	if err != nil {
		fail("pipe", err.Error())
	}
	saved := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = saved
	w.Close()
	data, _ := io.ReadAll(r)
	r.Close()
	return string(data)
}

func fail(step, msg string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", step, msg)
	os.Exit(1)
}
