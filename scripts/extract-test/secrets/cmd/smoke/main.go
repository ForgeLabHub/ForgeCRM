// Command smoke exercises the extracted secrets package: hash a password, verify
// it, reject a wrong one, and round-trip a value through the keyring.
//
// It proves the module runs on its own. The work is deterministic in shape — a
// correct password verifies and a wrong one does not — so the exit code is a real
// assertion and not a print.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/secrets"
)

func main() {
	hash, err := secrets.HashPassword("extraction-smoke")
	if err != nil {
		fail("hash", err)
	}
	if err := secrets.VerifyPassword("extraction-smoke", hash); err != nil {
		fail("verify correct", err)
	}
	if err := secrets.VerifyPassword("wrong", hash); !errors.Is(err, secrets.ErrPasswordMismatch) {
		fail("verify wrong", fmt.Errorf("got %v, want ErrPasswordMismatch", err))
	}

	keyring, err := secrets.NewKeyring("smoke-platform-secret")
	if err != nil {
		fail("keyring", err)
	}
	plaintext := []byte("a stored credential")
	ct, err := keyring.Encrypt(plaintext)
	if err != nil {
		fail("encrypt", err)
	}
	got, err := keyring.Decrypt(ct)
	if err != nil {
		fail("decrypt", err)
	}
	if !bytes.Equal(got, plaintext) {
		fail("round trip", fmt.Errorf("got %q, want %q", got, plaintext))
	}
	fmt.Println("smoke: password verified=1 mismatch detected=1 keyring roundtrip=1")
}

func fail(step string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", step, err)
	os.Exit(1)
}
