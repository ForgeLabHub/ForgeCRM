//go:build integration

// Account lockout has to hold across replicas, because a replica is just another
// process reading the same rows. These tests run the real service against a real
// database and assert the properties that a per-process counter cannot provide:
// concurrent failures must all be counted, the lock must apply to every replica,
// and a lock must expire.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/authn/...
package authn

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

const testPassword = "correct-horse-battery"

// newLockoutFixture resets the schema, applies migrations and returns a
// configured service plus the id of one active user.
func newLockoutFixture(t *testing.T, threshold int, window time.Duration) (*Service, *database.DB, uuid.UUID, string) {
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

	// One schema reset per fixture. Tests that need isolation call this; tests in
	// the same package must not run in parallel, which the default is.
	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := migrate.Up(url, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	keyring, err := secrets.NewKeyring("test-secret-key-that-is-long-enough-for-hkdf-derivation")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	cfg := &config.Config{
		Env:               "test",
		SecretKey:         "test-secret-key-that-is-long-enough-for-hkdf-derivation",
		SessionCookieName: "forgecrm_session",
		SessionTTL:        12 * time.Hour,
		LockoutThreshold:  threshold,
		LockoutWindow:     window,
	}

	svc := NewService(db, cfg, keyring, audit.New(db), rbac.NewService(db))

	hash, err := secrets.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	email := "lockout-target@example.test"
	var id uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name, status)
		 VALUES ($1, $2, 'Lockout Target', 'active') RETURNING id`,
		email, hash).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return svc, db, id, email
}

// failedAttempts reads the counter the way an operator would.
func failedAttempts(t *testing.T, db *database.DB, id uuid.UUID) (int, *time.Time) {
	t.Helper()
	var attempts int
	var lockedUntil *time.Time
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT failed_attempts, locked_until FROM users WHERE id = $1`, id).
		Scan(&attempts, &lockedUntil); err != nil {
		t.Fatalf("read lockout state: %v", err)
	}
	return attempts, lockedUntil
}

// TestConcurrentFailuresAcrossReplicasAreAllCounted is the property the previous
// implementation did not have.
//
// The old code read failed_attempts, added one in Go, and wrote it back. Two
// processes doing that at the same time both read the same value and both write
// the same value, so one failure is lost. With a threshold of N, an attacker
// gets more than N attempts per window, and the number of extra attempts grows
// with the number of replicas — which is exactly when the limit matters.
//
// Each goroutine builds its own Service over its own pool, so this is two
// replicas rather than two calls into one object.
func TestConcurrentFailuresAcrossReplicasAreAllCounted(t *testing.T) {
	const replicas = 8
	const attemptsPerReplica = 5
	const threshold = 1000 // high, so nothing locks and every increment is observable

	_, db, id, email := newLockoutFixture(t, threshold, time.Minute)

	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	keyring, err := secrets.NewKeyring("test-secret-key-that-is-long-enough-for-hkdf-derivation")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	cfg := &config.Config{
		Env: "test", SecretKey: "test-secret-key-that-is-long-enough-for-hkdf-derivation",
		SessionCookieName: "forgecrm_session", SessionTTL: time.Hour,
		LockoutThreshold: threshold, LockoutWindow: time.Minute,
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, replicas)

	for i := 0; i < replicas; i++ {
		// A dedicated pool per replica, so the increments travel over distinct
		// connections the way they would from distinct processes. Sharing one pool
		// would let the pool serialise them and hide the defect.
		replicaDB, err := database.ConnectWithPool(ctx, url, 2)
		if err != nil {
			t.Fatalf("replica pool: %v", err)
		}
		defer replicaDB.Close()
		replicaSvc := NewService(replicaDB, cfg, keyring, audit.New(replicaDB), rbac.NewService(replicaDB))

		wg.Add(1)
		go func(idx int, svc *Service) {
			defer wg.Done()
			<-start // release all goroutines together to maximise overlap
			for j := 0; j < attemptsPerReplica; j++ {
				_, err := svc.Login(ctx, LoginRequest{Email: email, Password: "wrong-password", IP: "203.0.113.9"})
				if err != nil && !errors.Is(err, ErrInvalidCredentials) && !errors.Is(err, ErrAccountLocked) {
					errs[idx] = err
					return
				}
			}
		}(i, replicaSvc)
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d: %v", i, err)
		}
	}

	got, locked := failedAttempts(t, db, id)
	want := replicas * attemptsPerReplica
	if locked != nil {
		t.Fatalf("account locked with threshold %d; the counter must be the only effect", threshold)
	}
	if got != want {
		t.Fatalf("failed_attempts = %d, want %d: %d failure(s) were lost to a concurrent read-modify-write",
			got, want, want-got)
	}
}

// TestLockAppliesAcrossReplicas proves the lock is not per-process. Once one
// replica has locked the account, a different replica must refuse the correct
// password too, otherwise the control only exists where it was triggered.
func TestLockAppliesAcrossReplicas(t *testing.T) {
	svcA, db, _, email := newLockoutFixture(t, 3, 15*time.Minute)

	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	keyring, err := secrets.NewKeyring("test-secret-key-that-is-long-enough-for-hkdf-derivation")
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	cfg := &config.Config{
		Env: "test", SecretKey: "test-secret-key-that-is-long-enough-for-hkdf-derivation",
		SessionCookieName: "forgecrm_session", SessionTTL: time.Hour,
		LockoutThreshold: 3, LockoutWindow: 15 * time.Minute,
	}
	poolB, err := database.ConnectWithPool(context.Background(), url, 2)
	if err != nil {
		t.Fatalf("replica B pool: %v", err)
	}
	defer poolB.Close()
	svcB := NewService(poolB, cfg, keyring, audit.New(poolB), rbac.NewService(poolB))

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := svcA.Login(ctx, LoginRequest{Email: email, Password: "wrong-password", IP: "203.0.113.10"}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d on replica A: got %v, want ErrInvalidCredentials", i+1, err)
		}
	}

	// The correct password from a different replica must now be refused.
	_, err = svcB.Login(ctx, LoginRequest{Email: email, Password: testPassword, IP: "203.0.113.11"})
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("replica B with the correct password: got %v, want ErrAccountLocked", err)
	}

	attempts, locked := failedAttempts(t, db, idOf(t, db, email))
	if attempts != 3 {
		t.Fatalf("failed_attempts = %d, want 3", attempts)
	}
	if locked == nil {
		t.Fatal("locked_until is NULL after the threshold was reached")
	}
}

// TestLockExpiresAndSuccessfulLoginRecovers covers the recovery path: a lock is
// temporary, and once it has passed a correct password clears the counter.
func TestLockExpiresAndSuccessfulLoginRecovers(t *testing.T) {
	svc, db, id, email := newLockoutFixture(t, 2, 150*time.Millisecond)

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		_, _ = svc.Login(ctx, LoginRequest{Email: email, Password: "wrong-password", IP: "203.0.113.12"})
	}
	if _, locked := failedAttempts(t, db, id); locked == nil {
		t.Fatal("the account was not locked after reaching the threshold")
	}

	time.Sleep(250 * time.Millisecond)

	// Past the window, the lock has lapsed. The correct password must work and
	// must clear the counter, not leave it at the threshold so the next failure
	// re-locks immediately.
	res, err := svc.Login(ctx, LoginRequest{Email: email, Password: testPassword, IP: "203.0.113.13"})
	if err != nil {
		t.Fatalf("login after the lock expired: %v", err)
	}
	if res == nil || res.User == nil {
		t.Fatal("login after the lock expired returned no user")
	}

	attempts, locked := failedAttempts(t, db, id)
	if attempts != 0 {
		t.Fatalf("failed_attempts = %d after a successful login, want 0", attempts)
	}
	if locked != nil {
		t.Fatal("locked_until was not cleared by a successful login")
	}
}

// TestSuccessfulLoginBeforeTheThresholdClearsTheCounter guards against the
// counter being merely decremented or left behind: one success resets it, so an
// occasional typo does not accumulate toward a lock over days.
func TestSuccessfulLoginBeforeTheThresholdClearsTheCounter(t *testing.T) {
	svc, db, id, email := newLockoutFixture(t, 5, 15*time.Minute)

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_, _ = svc.Login(ctx, LoginRequest{Email: email, Password: "wrong-password", IP: "203.0.113.14"})
	}
	if attempts, _ := failedAttempts(t, db, id); attempts != 4 {
		t.Fatalf("failed_attempts = %d, want 4", attempts)
	}

	if _, err := svc.Login(ctx, LoginRequest{Email: email, Password: testPassword, IP: "203.0.113.15"}); err != nil {
		t.Fatalf("successful login: %v", err)
	}

	if attempts, locked := failedAttempts(t, db, id); attempts != 0 || locked != nil {
		t.Fatalf("after success: failed_attempts = %d, locked_until = %v; want 0 and nil", attempts, locked)
	}
}

// TestUnknownAccountIsNotLockable checks the enumeration property: an address
// with no account must never produce a lock, and must not create a row that a
// later attacker could distinguish by timing or by a lock message.
func TestUnknownAccountIsNotLockable(t *testing.T) {
	svc, db, _, _ := newLockoutFixture(t, 2, 15*time.Minute)

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		_, err := svc.Login(ctx, LoginRequest{Email: "nobody@example.test", Password: "wrong-password", IP: "203.0.113.16"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("unknown account attempt %d: got %v, want ErrInvalidCredentials", i+1, err)
		}
	}

	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM users WHERE email = 'nobody@example.test'`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("a failed login for an unknown address created %d user row(s)", count)
	}
}

// idOf looks up a user id by email, for assertions that only have the address.
func idOf(t *testing.T, db *database.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("look up %q: %v", email, err)
	}
	return id
}
