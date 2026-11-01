//go:build integration

// Bootstrap over a real database.
//
// The first administrator is the one account the API cannot create for itself,
// so its rules are the ones an operator depends on when a fresh deployment is
// brought up: create once, never overwrite, fail clearly on a partial
// configuration, and never let two starters both create an owner. Those rules
// are enforced with a real advisory lock and a real partial unique index, which
// only a real PostgreSQL reports.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/bootstrap/...
package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/secrets"
)

const goodPassword = "correct-horse-battery-9"

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// freshDB resets the schema and applies every migration, so each test starts
// from a database in the same state a first deploy sees.
func freshDB(t *testing.T) *database.DB {
	t.Helper()

	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping integration test")
	}

	ctx := context.Background()
	db, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := migrate.Up(url, quietLog()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// ownerCount is the number of platform owners in the database.
func ownerCount(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Q(context.Background()).QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE is_platform_owner`).Scan(&n); err != nil {
		t.Fatalf("count owners: %v", err)
	}
	return n
}

// TestAdminCreatesTheFirstOwner walks the happy path and checks the account is
// genuinely usable: active, flagged as the platform owner, member of an
// organization, holding the tenant administrator role, and with a password hash
// the project's own verifier accepts.
func TestAdminCreatesTheFirstOwner(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()

	res, err := Admin(ctx, db, quietLog(), Options{Email: "Owner@Example.Test", Password: goodPassword})
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	if !res.Created || res.AlreadyExists {
		t.Fatalf("result = %+v, want Created", res)
	}
	if res.Email != "owner@example.test" {
		t.Fatalf("result email = %q, want the lower-cased address", res.Email)
	}
	if got := ownerCount(t, db); got != 1 {
		t.Fatalf("platform owners = %d, want 1", got)
	}

	var (
		hash   string
		status string
		owner  bool
	)
	if err := db.Q(ctx).QueryRow(ctx,
		`SELECT password_hash, status, is_platform_owner FROM users WHERE email = $1`,
		"owner@example.test").Scan(&hash, &status, &owner); err != nil {
		t.Fatalf("load the owner: %v", err)
	}
	if status != "active" || !owner {
		t.Fatalf("owner status=%q is_platform_owner=%v, want active/true", status, owner)
	}
	if err := secrets.VerifyPassword(goodPassword, hash); err != nil {
		t.Fatalf("the stored hash does not verify the password: %v", err)
	}
	if strings.Contains(hash, goodPassword) {
		t.Fatal("the password is stored in clear text")
	}

	// The owner must be able to work immediately: a membership and the tenant
	// administrator role are what the first request needs.
	var memberships, roles int
	if err := db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = $1`,
		"owner@example.test").Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if err := db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM user_roles ur JOIN users u ON u.id = ur.user_id WHERE u.email = $1`,
		"owner@example.test").Scan(&roles); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if memberships != 1 || roles != 1 {
		t.Fatalf("memberships=%d roles=%d, want 1 and 1", memberships, roles)
	}
}

// TestAdminRefusesToOverwriteAnExistingOwner is the rule that makes the
// variables safe to leave in a deploy script: a second run — with the same or a
// different address — must not touch the existing owner.
func TestAdminRefusesToOverwriteAnExistingOwner(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()

	if _, err := Admin(ctx, db, quietLog(), Options{Email: "first@example.test", Password: goodPassword}); err != nil {
		t.Fatalf("first Admin: %v", err)
	}
	var before string
	if err := db.Q(ctx).QueryRow(ctx,
		`SELECT password_hash FROM users WHERE email = $1`, "first@example.test").Scan(&before); err != nil {
		t.Fatalf("read hash: %v", err)
	}

	res, err := Admin(ctx, db, quietLog(), Options{Email: "second@example.test", Password: "another-strong-password-1"})
	if err != nil {
		t.Fatalf("second Admin: %v", err)
	}
	if res.Created || !res.AlreadyExists {
		t.Fatalf("second result = %+v, want AlreadyExists and no creation", res)
	}
	if got := ownerCount(t, db); got != 1 {
		t.Fatalf("platform owners = %d after a second run, want 1", got)
	}

	var after string
	if err := db.Q(ctx).QueryRow(ctx,
		`SELECT password_hash FROM users WHERE email = $1`, "first@example.test").Scan(&after); err != nil {
		t.Fatalf("re-read hash: %v", err)
	}
	if before != after {
		t.Fatal("the existing owner's password hash changed")
	}

	var second int
	if err := db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM users WHERE email = $1`, "second@example.test").Scan(&second); err != nil {
		t.Fatalf("count second: %v", err)
	}
	if second != 0 {
		t.Fatal("a second user was created despite an existing owner")
	}
}

// TestAdminRefusesIncompleteConfiguration checks the failure an operator is
// most likely to hit names the variable that is missing, and never the value
// that was supplied.
func TestAdminRefusesIncompleteConfiguration(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()

	_, err := Admin(ctx, db, quietLog(), Options{Email: "owner@example.test"})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("email-only error = %v, want ErrIncomplete", err)
	}
	if !strings.Contains(err.Error(), "FORGECRM_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("email-only error does not name the password variable: %v", err)
	}

	_, err = Admin(ctx, db, quietLog(), Options{Password: goodPassword})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("password-only error = %v, want ErrIncomplete", err)
	}
	if !strings.Contains(err.Error(), "FORGECRM_BOOTSTRAP_ADMIN_EMAIL") {
		t.Fatalf("password-only error does not name the email variable: %v", err)
	}
	if strings.Contains(err.Error(), goodPassword) {
		t.Fatal("the error message contains the password")
	}

	if got := ownerCount(t, db); got != 0 {
		t.Fatalf("owners = %d after a refused bootstrap, want 0", got)
	}
}

// TestAdminIsANoOpWithoutConfiguration keeps the API able to call Admin on every
// start in production, where the bootstrap variables are deliberately absent.
func TestAdminIsANoOpWithoutConfiguration(t *testing.T) {
	db := freshDB(t)

	res, err := Admin(context.Background(), db, quietLog(), Options{})
	if err != nil {
		t.Fatalf("Admin with no configuration: %v", err)
	}
	if res.Created || res.AlreadyExists {
		t.Fatalf("result = %+v, want a plain no-op", res)
	}
	if got := ownerCount(t, db); got != 0 {
		t.Fatalf("owners = %d, want 0", got)
	}
}

// TestAdminRejectsAWeakPasswordOrAMalformedAddress checks the account is never
// created from an input the sign-in path would refuse anyway.
func TestAdminRejectsAWeakPasswordOrAMalformedAddress(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()

	if _, err := Admin(ctx, db, quietLog(), Options{Email: "owner@example.test", Password: "short"}); err == nil {
		t.Fatal("a too-short password was accepted")
	}
	if _, err := Admin(ctx, db, quietLog(), Options{Email: "owner@example.test", Password: "password12345"}); err == nil {
		t.Fatal("a common password was accepted")
	}
	if _, err := Admin(ctx, db, quietLog(), Options{Email: "not-an-address", Password: goodPassword}); err == nil {
		t.Fatal("a malformed address was accepted")
	}
	if got := ownerCount(t, db); got != 0 {
		t.Fatalf("owners = %d after rejected input, want 0", got)
	}
}

// TestAdminConcurrentRunsCreateExactlyOne models two replicas starting at once.
// The advisory lock and the re-check inside it must leave exactly one owner,
// even when the two runs carry different addresses.
func TestAdminConcurrentRunsCreateExactlyOne(t *testing.T) {
	db := freshDB(t)
	ctx := context.Background()

	type outcome struct {
		res Result
		err error
	}
	const runners = 4
	results := make([]outcome, runners)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < runners; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := Admin(ctx, db, quietLog(), Options{
				Email:    "owner" + string(rune('a'+i)) + "@example.test",
				Password: goodPassword,
			})
			results[i] = outcome{res, err}
		}(i)
	}
	close(start)
	wg.Wait()

	created := 0
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("runner %d failed: %v", i, r.err)
		}
		if r.res.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d runners reported creation, want exactly 1", created)
	}
	if got := ownerCount(t, db); got != 1 {
		t.Fatalf("platform owners = %d after concurrent runs, want 1", got)
	}
}

// TestAdminNeverLogsThePassword captures the log output and asserts the secret
// does not appear, which is the guarantee an operator relies on when the
// password is supplied through the environment.
func TestAdminNeverLogsThePassword(t *testing.T) {
	db := freshDB(t)

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := Admin(context.Background(), db, log, Options{Email: "owner@example.test", Password: goodPassword}); err != nil {
		t.Fatalf("Admin: %v", err)
	}
	if strings.Contains(buf.String(), goodPassword) {
		t.Fatalf("the password appears in the log output: %s", buf.String())
	}
}
