// Command smoke exercises the extracted authn package: the password policy is the
// boundary a weak credential meets, and the step-up window is the contract the
// second factor depends on.
//
// The service needs a database and a keyring, so the smoke does not construct it;
// what it pins is the pure policy the rest of authentication leans on.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/authn"
)

func main() {
	// A password below the minimum length is refused, and the error is the sentinel
	// a caller checks rather than a string match.
	if err := authn.ValidatePassword(strings.Repeat("a", authn.MinPasswordLength-1)); !errors.Is(err, authn.ErrWeakPassword) {
		fail("a short password was not refused with ErrWeakPassword: %v", err)
	}
	// A common password of legal length is still refused.
	if err := authn.ValidatePassword("password12345"); !errors.Is(err, authn.ErrWeakPassword) {
		fail("a common password was accepted: %v", err)
	}
	// A blank password is refused even at length.
	if err := authn.ValidatePassword(strings.Repeat(" ", authn.MinPasswordLength)); !errors.Is(err, authn.ErrWeakPassword) {
		fail("a blank password was accepted: %v", err)
	}
	// A strong password passes.
	if err := authn.ValidatePassword("a-perfectly-fine-passphrase"); err != nil {
		fail("a strong password was refused: %v", err)
	}
	fmt.Printf("password policy ok: minimum %d characters\n", authn.MinPasswordLength)

	// A session that never stepped up is not stepped up.
	var sess authn.Session
	if sess.SteppedUp() {
		fail("a session with no step-up reported stepped up")
	}
	now := time.Now()
	sess.StepUpAt = &now
	if !sess.SteppedUp() {
		fail("a session that just stepped up did not report stepped up")
	}
	old := now.Add(-authn.StepUpWindow - time.Minute)
	sess.StepUpAt = &old
	if sess.SteppedUp() {
		fail("a session stepped up outside the window reported stepped up")
	}
	fmt.Printf("step-up window ok: %s\n", authn.StepUpWindow)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
