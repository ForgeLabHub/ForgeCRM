//go:build integration

// These tests exercise tenant isolation through the real HTTP stack: a real
// router, real middleware, real sessions and a real database. They are the
// counterpart to the raw-SQL check in internal/testsupport.
//
// The distinction matters. A query-level test proves the SQL is written
// correctly; it does not prove the handler is wired to that query, that the
// middleware resolves the right organization, or that a permission check runs
// before the lookup. A user must not be able to read another tenant's record
// merely because they guessed its identifier, and the only way to establish that
// is to send the request.
//
// Run with:
//
//	FORGECRM_TEST_DATABASE_URL=postgres://... go test -tags=integration ./internal/api/...
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/migrate"
	"github.com/geff1778/forgecrm/internal/ratelimit"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/wiring"
)

// harness is a fully assembled API server backed by a migrated database.
type harness struct {
	t      *testing.T
	db     *database.DB
	server http.Handler
	cfg    *config.Config
}

// newHarness resets the test database, applies every migration, builds the real
// service graph and returns a server ready to receive requests.
func newHarness(t *testing.T) *harness {
	t.Helper()

	url := os.Getenv("FORGECRM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping integration test")
	}

	resetSchema(t, url)
	return newHarnessWithStore(t, testConfig(t), ratelimit.NewMemoryStore(), url)
}

// resetSchema drops and rebuilds the public schema and applies every migration.
// It is separate from newHarness because a test that builds more than one server
// over one database — the cross-replica tests — must reset exactly once, before
// the first server is built.
func resetSchema(t *testing.T, url string) {
	t.Helper()

	ctx := context.Background()
	db, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	if _, err := db.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := migrate.Up(url, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// newHarnessWithStore builds one server over the given limiter store and
// database. It does not reset the schema, so a caller can build several servers
// over one database.
//
// Each call opens its own connection pool, which is what lets the cross-replica
// tests model two processes: nothing but PostgreSQL and the injected store is
// shared between them.
func newHarnessWithStore(t *testing.T, cfg *config.Config, store ratelimit.Store, url string) *harness {
	t.Helper()

	ctx := context.Background()
	db, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	keyring, err := secrets.NewKeyring(cfg.SecretKey)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	svcs, err := wiring.Build(cfg, db, keyring, logging.New("error", "text"), store)
	if err != nil {
		t.Fatalf("build services: %v", err)
	}

	// The trust list is derived from the config, the same way main.go derives it, so
	// a test that configures trusted proxies exercises the real parsing and matching
	// rather than a value the test injected.
	trusted, err := httpx.TrustedProxiesFrom(cfg.TrustedProxies)
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}

	srv, err := New(&Deps{
		Config: cfg, Log: logging.New("error", "text"), DB: db, Keyring: keyring,
		Audit: svcs.Audit, Auth: svcs.Auth, RBAC: svcs.RBAC, CRM: svcs.CRM,
		Jobs: svcs.Jobs, Outbox: svcs.Outbox, Imports: svcs.Imports,
		Migrations: svcs.Migrations, Integrations: svcs.Integrations,
		Events: svcs.Events, Dispatcher: svcs.Dispatcher,
		Email: svcs.Email, Telegram: svcs.Telegram, AI: svcs.AI, Tools: svcs.Tools,
		MCP:            svcs.MCP,
		Agent:          svcs.Agent,
		OIDC:           svcs.OIDC,
		MCPCredentials: svcs.MCPCredentials,
		Consent:        svcs.Consent,
		Version:        "test",
	}, store, trusted)
	if err != nil {
		t.Fatalf("build server: %v", err)
	}

	return &harness{t: t, db: db, server: srv.Handler(), cfg: cfg}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		Env:                  "test",
		HTTPAddr:             "127.0.0.1:0",
		SecretKey:            "test-secret-key-that-is-long-enough-for-hkdf-derivation",
		SessionCookieName:    "forgecrm_session",
		CSRFHeader:           "X-CSRF-Token",
		SessionTTL:           12 * 60 * 60 * 1e9,
		RateLimitPerMin:      100000, // the rate limiter must not interfere with tests
		LoginRateLimitPerMin: 100000, // the suite signs in repeatedly from one address
		ImportStorageDir:     dir,
		ImportTempDir:        dir,
	}
}

// actor is a user with a session, used to send authenticated requests.
type actor struct {
	userID      uuid.UUID
	orgID       uuid.UUID
	token       string
	csrf        string
	permissions []string
}

// api performs a request as the given actor and returns the recorder.
func (h *harness) do(a *actor, method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if a != nil {
		req.AddCookie(&http.Cookie{Name: h.cfg.SessionCookieName, Value: a.token})
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.Set(h.cfg.CSRFHeader, a.csrf)
		}
	}

	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	return rec
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// seedOrganization creates an organization and returns its id.
func (h *harness) seedOrganization(name string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	err := h.db.Pool().QueryRow(context.Background(),
		`INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id`,
		name, strings.ToLower(strings.ReplaceAll(name, " ", "-"))).Scan(&id)
	if err != nil {
		h.t.Fatalf("seed organization %q: %v", name, err)
	}
	return id
}

// seedUser creates an active user with a known password and returns the id.
func (h *harness) seedUser(email, password, displayName string) uuid.UUID {
	h.t.Helper()

	hash, err := secrets.HashPassword(password)
	if err != nil {
		h.t.Fatalf("hash password: %v", err)
	}

	var id uuid.UUID
	err = h.db.Pool().QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash, display_name, status)
		 VALUES ($1, $2, $3, 'active') RETURNING id`,
		email, hash, displayName).Scan(&id)
	if err != nil {
		h.t.Fatalf("seed user %q: %v", email, err)
	}
	return id
}

// joinOrganization makes a user a member of an organization with the given role
// key, so the RBAC layer resolves real permissions for them.
func (h *harness) joinOrganization(userID, orgID uuid.UUID, roleKey string) {
	h.t.Helper()
	ctx := context.Background()

	if _, err := h.db.Pool().Exec(ctx,
		`INSERT INTO memberships (user_id, organization_id, status) VALUES ($1, $2, 'active')`,
		userID, orgID); err != nil {
		h.t.Fatalf("add membership: %v", err)
	}

	// The role templates are seeded with a NULL organization_id. Copy the chosen
	// template into the organization so the grant is scoped to this tenant, which
	// is what the real provisioning path does.
	var roleID uuid.UUID
	err := h.db.Pool().QueryRow(ctx,
		`INSERT INTO roles (organization_id, key, name, is_system)
		 SELECT $1, key, name, true FROM roles
		 WHERE organization_id IS NULL AND key = $2
		 RETURNING id`, orgID, roleKey).Scan(&roleID)
	if err != nil {
		h.t.Fatalf("provision role %q: %v", roleKey, err)
	}

	if _, err := h.db.Pool().Exec(ctx,
		`INSERT INTO role_permissions (role_id, permission_key)
		 SELECT $1, permission_key FROM role_permissions rp
		 JOIN roles r ON r.id = rp.role_id
		 WHERE r.organization_id IS NULL AND r.key = $2`, roleID, roleKey); err != nil {
		h.t.Fatalf("copy role permissions for %q: %v", roleKey, err)
	}

	if _, err := h.db.Pool().Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id, organization_id) VALUES ($1, $2, $3)`,
		userID, roleID, orgID); err != nil {
		h.t.Fatalf("grant role %q: %v", roleKey, err)
	}
}

// login signs the user in through the real login endpoint and returns the actor.
func (h *harness) login(email, password string) *actor {
	h.t.Helper()

	rec := h.do(nil, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login %s: status %d, body %s", email, rec.Code, rec.Body.String())
	}

	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == h.cfg.SessionCookieName {
			cookie = c.Value
		}
	}
	if cookie == "" {
		h.t.Fatal("login did not set a session cookie")
	}

	// /auth/me returns the CSRF token and the acting organization.
	me := h.do(&actor{token: cookie}, http.MethodGet, "/api/v1/auth/me", "")
	if me.Code != http.StatusOK {
		h.t.Fatalf("auth/me: status %d, body %s", me.Code, me.Body.String())
	}
	var meBody struct {
		CSRFToken    string   `json:"csrf_token"`
		Permissions  []string `json:"permissions"`
		Organization struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		h.t.Fatalf("decode auth/me: %v (body %s)", err, me.Body.String())
	}

	a := &actor{token: cookie, csrf: meBody.CSRFToken, permissions: meBody.Permissions}
	if meBody.Organization.ID != "" {
		a.orgID, _ = uuid.Parse(meBody.Organization.ID)
	}

	var userID uuid.UUID
	if err := h.db.Pool().QueryRow(context.Background(),
		`SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		h.t.Fatalf("look up %s: %v", email, err)
	}
	a.userID = userID
	return a
}

// seedAccount inserts an account directly and returns its id.
func (h *harness) seedAccount(orgID uuid.UUID, name string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	err := h.db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (organization_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name).Scan(&id)
	if err != nil {
		h.t.Fatalf("seed account %q: %v", name, err)
	}
	return id
}

// countAccountsIn reports how many accounts exist in an organization.
func (h *harness) countAccountsIn(orgID uuid.UUID) int {
	h.t.Helper()
	var n int
	if err := h.db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM accounts WHERE organization_id = $1`, orgID).Scan(&n); err != nil {
		h.t.Fatalf("count accounts: %v", err)
	}
	return n
}

// pool exposes the pool for assertions that need raw SQL.
func (h *harness) pool() *pgxpool.Pool { return h.db.Pool() }

// hasPermission reports whether the API told this actor it holds a permission.
// It reads the value from /auth/me rather than querying the database, so the
// assertion is about what the server actually decided to grant.
func (h *harness) hasPermission(a *actor, perm rbac.Permission) bool {
	h.t.Helper()
	for _, p := range a.permissions {
		if p == string(perm) {
			return true
		}
	}
	return false
}
