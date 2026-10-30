//go:build integration

// Single sign-on through the real HTTP stack against a real OpenID Connect
// provider.
//
// The provider is an in-process HTTP server that publishes a genuine discovery
// document, a JWKS and a token endpoint, and signs ID tokens with a generated RSA
// key. Nothing is stubbed on the ForgeCRM side: go-oidc fetches the discovery
// document, verifies the signature against the JWKS and checks the issuer,
// audience, expiry and nonce, exactly as it does against Entra ID, Okta or
// Keycloak. A mock would not catch a wrong issuer URL or a claim read from the
// wrong place.
package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// ---------------------------------------------------------------------------
// A minimal but real OpenID Connect provider
// ---------------------------------------------------------------------------

type fakeIDP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string

	mu sync.Mutex
	// nonce the next token response embeds. The test copies it from the
	// authorization request, which is how a real provider learns it.
	nonce string
	// overrides let a test produce a token that is wrong in one specific way.
	audience  string
	issuer    string
	subject   string
	email     string
	verified  bool
	tokenCode string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate the provider key: %v", err)
	}
	idp := &fakeIDP{
		t:        t,
		key:      key,
		kid:      "test-key-1",
		subject:  "subject-abc",
		email:    "ada@acme.test",
		verified: true,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("/keys", idp.jwks)
	mux.HandleFunc("/token", idp.token)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not used by the test", http.StatusNotImplemented)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	idp.issuer = idp.server.URL
	return idp
}

func (i *fakeIDP) discovery(w http.ResponseWriter, r *http.Request) {
	i.mu.Lock()
	issuer := i.issuer
	i.mu.Unlock()

	writeJSON(w, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/authorize",
		"token_endpoint":                        issuer + "/token",
		"jwks_uri":                              issuer + "/keys",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (i *fakeIDP) jwks(w http.ResponseWriter, r *http.Request) {
	pub := i.key.Public().(*rsa.PublicKey)
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": i.kid,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(bigEndianBytes(pub.E)),
		}},
	})
}

func (i *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	i.mu.Lock()
	nonce := i.nonce
	audience := i.audience
	issuer := i.issuer
	subject := i.subject
	email := i.email
	verified := i.verified
	code := i.tokenCode
	i.mu.Unlock()

	if audience == "" {
		audience = "forgecrm"
	}
	if code != "" && r.Form.Get("code") != code {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: i.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", i.kid),
	)
	if err != nil {
		http.Error(w, "signer", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	claims := jwt.Claims{
		Issuer:   issuer,
		Subject:  subject,
		Audience: jwt.Audience{audience},
		Expiry:   jwt.NewNumericDate(now.Add(5 * time.Minute)),
		IssuedAt: jwt.NewNumericDate(now),
	}
	private := map[string]any{
		"nonce":          nonce,
		"email":          email,
		"email_verified": verified,
		"name":           "Ada Lovelace",
	}
	signed, err := jwt.Signed(signer).Claims(claims).Claims(private).Serialize()
	if err != nil {
		http.Error(w, "serialize", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"access_token": "access-token-value",
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     signed,
	})
}

func (i *fakeIDP) setNonce(n string) {
	i.mu.Lock()
	i.nonce = n
	i.mu.Unlock()
}

func bigEndianBytes(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	var out []byte
	for v > 0 {
		out = append([]byte{byte(v & 0xff)}, out...)
		v >>= 8
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// ssoHarness builds a server with single sign-on enabled.
func ssoHarness(t *testing.T) *harness {
	t.Helper()

	url := dbURL(t)
	resetSchema(t, url)

	cfg := testConfig(t)
	cfg.BaseURL = "https://crm.example.test"
	cfg.OIDC.Enabled = true
	return newHarnessWithStore(t, cfg, ratelimit.NewMemoryStore(), url)
}

// createProvider registers a provider through the API, as an operator would.
func (h *harness) createProvider(a *actor, idp *fakeIDP, body string) {
	h.t.Helper()
	if body == "" {
		body = fmt.Sprintf(`{
			"key":"acme",
			"display_name":"Acme Identity",
			"issuer":%q,
			"client_id":"forgecrm",
			"client_secret":"client-secret-value",
			"jit_provisioning":true,
			"enabled":true
		}`, idp.issuer)
	}
	rec := h.do(a, http.MethodPost, "/api/v1/admin/sso/providers", body)
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create provider: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// startSignIn begins the flow and returns the authorization URL.
func (h *harness) startSignIn(providerKey string) *url.URL {
	h.t.Helper()
	rec := h.do(nil, http.MethodGet, "/api/v1/auth/oidc/start?provider="+providerKey, "")
	if rec.Code != http.StatusFound {
		h.t.Fatalf("start sign-in: status %d, body %s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		h.t.Fatalf("parse the authorization URL: %v", err)
	}
	return loc
}

// completeSignIn drives the callback and returns the recorder.
func (h *harness) completeSignIn(state, code string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/oidc/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), nil)
	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	return rec
}

// dbURL reads the test database URL, skipping when it is unset.
func dbURL(t *testing.T) string {
	t.Helper()
	u := strings.TrimSpace(os.Getenv("FORGECRM_TEST_DATABASE_URL"))
	if u == "" {
		t.Skip("FORGECRM_TEST_DATABASE_URL is not set; skipping integration test")
	}
	return u
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSSOSignInProvisionsAnAccountAndEstablishesASession is the whole flow end to
// end: register a provider, begin sign-in, complete it against the provider, and
// use the resulting session.
func TestSSOSignInProvisionsAnAccountAndEstablishesASession(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Acme")
	admin := h.seedUser("admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("admin@acme.test", "Admin-Passw0rd-2026!!!")

	h.createProvider(a, idp, "")

	// The sign-in page learns about the provider without a session.
	rec := h.do(nil, http.MethodGet, "/api/v1/auth/oidc/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list providers: status %d, body %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Enabled   bool `json:"enabled"`
		Providers []struct {
			Key         string `json:"key"`
			DisplayName string `json:"display_name"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if !listed.Enabled || len(listed.Providers) != 1 || listed.Providers[0].Key != "acme" {
		t.Fatalf("provider list is wrong: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "client-secret-value") {
		t.Fatal("the anonymous provider list leaked the client secret")
	}

	// Begin the flow and hand the provider the nonce it must echo.
	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	if state == "" {
		t.Fatal("the authorization URL carries no state")
	}
	if authURL.Query().Get("code_challenge") == "" {
		t.Fatal("the authorization URL carries no PKCE challenge")
	}
	idp.setNonce(authURL.Query().Get("nonce"))

	rec = h.completeSignIn(state, "authorization-code-1")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: status %d, body %s", rec.Code, rec.Body.String())
	}

	// The account was provisioned, joined to the organization and linked.
	var (
		userID uuid.UUID
		status string
	)
	if err := h.pool().QueryRow(context.Background(),
		`SELECT id, status FROM users WHERE email = 'ada@acme.test'`).Scan(&userID, &status); err != nil {
		t.Fatalf("the provisioned account does not exist: %v", err)
	}
	if status != "active" {
		t.Fatalf("provisioned account status = %q, want active", status)
	}
	var identities int
	if err := h.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM user_identities WHERE user_id = $1 AND subject = 'subject-abc'`,
		userID).Scan(&identities); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identities != 1 {
		t.Fatalf("identity links = %d, want 1", identities)
	}

	// The session the callback set is a real session: it authenticates.
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("the callback set no session cookie")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	meRec := httptest.NewRecorder()
	h.server.ServeHTTP(meRec, req)
	if meRec.Code != http.StatusOK {
		t.Fatalf("the SSO session does not authenticate: status %d, body %s",
			meRec.Code, meRec.Body.String())
	}
}

// TestSSOStateCannotBeReplayed pins the single-use state. A second presentation of
// the same authorization response must be refused, or a leaked response could
// mint a second session.
func TestSSOStateCannotBeReplayed(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Replay")
	admin := h.seedUser("replay-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("replay-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, "")

	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	idp.setNonce(authURL.Query().Get("nonce"))

	if rec := h.completeSignIn(state, "code-1"); rec.Code != http.StatusFound {
		t.Fatalf("first callback: status %d, body %s", rec.Code, rec.Body.String())
	}
	rec := h.completeSignIn(state, "code-1")
	if rec.Code != http.StatusFound {
		t.Fatalf("second callback: status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "sso_error=") {
		t.Fatalf("a replayed state was not refused: redirect to %q", loc)
	}
}

// TestSSORejectsATokenForAnotherAudience checks the audience check. A token minted
// for a different client must not be accepted, or any client of the same provider
// could sign into ForgeCRM.
func TestSSORejectsATokenForAnotherAudience(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Audience")
	admin := h.seedUser("aud-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("aud-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, "")

	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	idp.setNonce(authURL.Query().Get("nonce"))
	idp.audience = "some-other-application"

	rec := h.completeSignIn(state, "code-aud")
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "sso_error=") {
		t.Fatalf("a token for another audience was accepted: redirect to %q", loc)
	}
	var count int
	if err := h.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE email = 'ada@acme.test'`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatal("a rejected token still provisioned an account")
	}
}

// TestSSORejectsAnAddressOutsideTheAllowedDomains checks the domain allowlist. A
// provider that lets a user set an arbitrary verified address must not be able to
// claim an address in the tenant's domain.
func TestSSORejectsAnAddressOutsideTheAllowedDomains(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Domains")
	admin := h.seedUser("dom-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("dom-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, fmt.Sprintf(`{
		"key":"acme",
		"display_name":"Acme",
		"issuer":%q,
		"client_id":"forgecrm",
		"client_secret":"secret",
		"allowed_email_domains":["acme.test"],
		"jit_provisioning":true,
		"enabled":true
	}`, idp.issuer))

	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	idp.setNonce(authURL.Query().Get("nonce"))
	idp.email = "attacker@evil.example"

	rec := h.completeSignIn(state, "code-dom")
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "sso_error=") {
		t.Fatalf("an address outside the allowed domains was accepted: %q", loc)
	}
}

// TestSSORejectsAnUnverifiedAddress checks the email_verified requirement. An
// unverified address is attacker-controlled, so linking by it would be an
// account-takeover path.
func TestSSORejectsAnUnverifiedAddress(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Unverified")
	admin := h.seedUser("unv-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("unv-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, "")

	// An existing account with the same address exists; the unverified claim must
	// not be allowed to take it over.
	h.seedUser("ada@acme.test", "Ada-Passw0rd-2026!!!!!", "Ada")

	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	idp.setNonce(authURL.Query().Get("nonce"))
	idp.verified = false

	rec := h.completeSignIn(state, "code-unverified")
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "sso_error=") {
		t.Fatalf("an unverified address was accepted: %q", loc)
	}
	var links int
	if err := h.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM user_identities`).Scan(&links); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if links != 0 {
		t.Fatal("an unverified address was linked to an account")
	}
}

// TestSSOWithoutJITRefusesAnUnknownAccount checks that provisioning is opt-in.
func TestSSOWithoutJITRefusesAnUnknownAccount(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("NoJIT")
	admin := h.seedUser("nojit-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("nojit-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, fmt.Sprintf(`{
		"key":"acme",
		"display_name":"Acme",
		"issuer":%q,
		"client_id":"forgecrm",
		"client_secret":"secret",
		"jit_provisioning":false,
		"enabled":true
	}`, idp.issuer))

	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	idp.setNonce(authURL.Query().Get("nonce"))

	rec := h.completeSignIn(state, "code-nojit")
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "sso_error=no_account") {
		t.Fatalf("an unknown account was accepted or misreported: %q", loc)
	}
}

// TestSSOLinksAnExistingAccountByVerifiedEmail checks the linking path: an account
// that already exists is linked rather than duplicated.
func TestSSOLinksAnExistingAccountByVerifiedEmail(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Link")
	admin := h.seedUser("link-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("link-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, "")

	existing := h.seedUser("ada@acme.test", "Ada-Passw0rd-2026!!!!!", "Ada")
	h.joinOrganization(existing, org, "sales_representative")

	authURL := h.startSignIn("acme")
	state := authURL.Query().Get("state")
	idp.setNonce(authURL.Query().Get("nonce"))

	if rec := h.completeSignIn(state, "code-link"); rec.Code != http.StatusFound {
		t.Fatalf("callback: status %d", rec.Code)
	}

	var (
		linkedUser uuid.UUID
		users      int
	)
	if err := h.pool().QueryRow(context.Background(),
		`SELECT count(*) FROM users WHERE email = 'ada@acme.test'`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("users with that address = %d, want 1 (a duplicate was created)", users)
	}
	if err := h.pool().QueryRow(context.Background(),
		`SELECT user_id FROM user_identities WHERE subject = 'subject-abc'`).Scan(&linkedUser); err != nil {
		t.Fatalf("read the identity link: %v", err)
	}
	if linkedUser != existing {
		t.Fatalf("the identity was linked to %s, want the existing account %s", linkedUser, existing)
	}
}

// TestSSOProviderAdministrationRequiresItsPermission checks that the routes are
// gated: a user without sso.manage cannot create a provider, and one without
// sso.read cannot list them.
func TestSSOProviderAdministrationRequiresItsPermission(t *testing.T) {
	h := ssoHarness(t)
	idp := newFakeIDP(t)

	org := h.seedOrganization("Gate")
	admin := h.seedUser("gate-admin@acme.test", "Admin-Passw0rd-2026!!!", "Admin")
	h.joinOrganization(admin, org, "platform_admin")
	a := h.login("gate-admin@acme.test", "Admin-Passw0rd-2026!!!")
	h.createProvider(a, idp, "")

	// A sales representative holds neither permission.
	sales := h.seedUser("sales@acme.test", "Sales-Passw0rd-2026!!!!", "Sales")
	h.joinOrganization(sales, org, "sales_representative")
	sa := h.login("sales@acme.test", "Sales-Passw0rd-2026!!!!")

	if rec := h.do(sa, http.MethodGet, "/api/v1/admin/sso/providers", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("listing providers without sso.read: status %d, want 403", rec.Code)
	}
	rec := h.do(sa, http.MethodPost, "/api/v1/admin/sso/providers", fmt.Sprintf(`{
		"key":"evil","display_name":"Evil","issuer":%q,"client_id":"x"
	}`, idp.issuer))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("creating a provider without sso.manage: status %d, want 403", rec.Code)
	}
}

// TestSSOStartRefusesAnUnknownProvider checks that a bogus provider key is a clean
// 404 rather than a redirect into nowhere.
func TestSSOStartRefusesAnUnknownProvider(t *testing.T) {
	h := ssoHarness(t)
	rec := h.do(nil, http.MethodGet, "/api/v1/auth/oidc/start?provider=does-not-exist", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown provider: status %d, want 404", rec.Code)
	}
}

// TestSSOCallbackWithoutStateIsRefused checks the callback's input guard.
func TestSSOCallbackWithoutStateIsRefused(t *testing.T) {
	h := ssoHarness(t)
	rec := h.completeSignIn("", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback without state: status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "sso_error=") {
		t.Fatalf("callback without state was not refused: %q", loc)
	}
}

// TestSSOIsInertWhenNotConfigured checks that the whole surface reports itself off
// rather than failing when the deployment has no SSO settings.
func TestSSOIsInertWhenNotConfigured(t *testing.T) {
	url := dbURL(t)
	resetSchema(t, url)
	cfg := testConfig(t) // OIDC disabled
	h := newHarnessWithStore(t, cfg, ratelimit.NewMemoryStore(), url)

	rec := h.do(nil, http.MethodGet, "/api/v1/auth/oidc/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("provider list when disabled: status %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Enabled   bool `json:"enabled"`
		Providers []struct {
			Key string `json:"key"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enabled {
		t.Fatal("the provider list reported SSO enabled when it is not configured")
	}

	rec = h.do(nil, http.MethodGet, "/api/v1/auth/oidc/start?provider=acme", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("start when disabled: status %d, want 503", rec.Code)
	}
}
