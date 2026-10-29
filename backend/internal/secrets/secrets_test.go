package secrets

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The package is the only place passwords, session tokens and stored credentials
// are turned into or out of a protected form. A defect here is a credential
// disclosure or a lockout, and it would not be caught by any integration test —
// those exercise the happy path once, with one password, through a live server.
// These tests pin the properties that actually matter: verification is exact, a
// hash is salted, a wrong key does not decrypt, and a tampered ciphertext is
// refused.

func TestHashPasswordRoundTrips(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := VerifyPassword("correct horse battery staple", encoded); err != nil {
		t.Fatalf("verify of the correct password failed: %v", err)
	}
	if err := VerifyPassword("Correct horse battery staple", encoded); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("verify of a wrong password = %v, want ErrPasswordMismatch", err)
	}
}

// A hash that is not salted lets one leaked table be attacked with a rainbow
// table, and lets two users who chose the same password be recognised as such.
// Both hashes below are of the same input and must differ.
func TestHashPasswordIsSalted(t *testing.T) {
	first, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical: the salt is not random")
	}
	if err := VerifyPassword("same-password", second); err != nil {
		t.Fatalf("the second hash did not verify: %v", err)
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("an empty password was hashed; it must be refused")
	}
}

// The parameters are stored in the PHC string so they can be raised later without
// invalidating old hashes. A custom-parameter hash must therefore verify with
// those parameters, not the defaults.
func TestHashPasswordHonoursExplicitParams(t *testing.T) {
	params := Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	encoded, err := HashPasswordWithParams("pw", params)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.Contains(encoded, "m=8192,t=1,p=1") {
		t.Fatalf("the encoded hash does not carry the parameters used: %s", encoded)
	}
	if err := VerifyPassword("pw", encoded); err != nil {
		t.Fatalf("verify with stored parameters failed: %v", err)
	}
}

// A malformed stored hash is a storage bug, not a wrong password, and the caller
// must be able to tell them apart so it does not tell a user "wrong password"
// when the row is corrupt.
func TestVerifyPasswordDistinguishesMalformedHash(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"not phc":          "not-a-hash",
		"wrong algorithm":  "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$a2V5",
		"bad version":      "$argon2id$v=99$m=1,t=1,p=1$c2FsdA$a2V5",
		"bad params":       "$argon2id$v=19$m=x$c2FsdA$a2V5",
		"bad salt base64":  "$argon2id$v=19$m=1,t=1,p=1$!!!$a2V5",
		"bad key base64":   "$argon2id$v=19$m=1,t=1,p=1$c2FsdA$!!!",
		"too few sections": "$argon2id$v=19$m=1,t=1,p=1$c2FsdA",
	}
	for name, encoded := range cases {
		if err := VerifyPassword("pw", encoded); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%s: verify = %v, want ErrInvalidHash", name, err)
		}
	}
}

func TestRandomTokenIsURLSafeAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		token, err := RandomToken(32)
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		if strings.ContainsAny(token, "+/=") {
			t.Fatalf("token %q is not URL-safe (raw encoding expected)", token)
		}
		if seen[token] {
			t.Fatalf("token %q was produced twice", token)
		}
		seen[token] = true
	}
}

func TestHashTokenIsDeterministic(t *testing.T) {
	if !bytes.Equal(HashToken("abc"), HashToken("abc")) {
		t.Fatal("HashToken is not deterministic for the same input")
	}
	if bytes.Equal(HashToken("abc"), HashToken("abd")) {
		t.Fatal("HashToken collided on different inputs")
	}
	if len(HashToken("abc")) != 32 {
		t.Fatalf("HashToken returned %d bytes, want 32 (SHA-256)", len(HashToken("abc")))
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual([]byte("same"), []byte("same")) {
		t.Fatal("equal slices compared unequal")
	}
	if ConstantTimeEqual([]byte("same"), []byte("diff")) {
		t.Fatal("different slices compared equal")
	}
	if ConstantTimeEqual([]byte("short"), []byte("longer")) {
		t.Fatal("slices of different length compared equal")
	}
}

func TestRandomNumericCode(t *testing.T) {
	code, err := RandomNumericCode(6)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code %q has length %d, want 6", code, len(code))
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("code %q contains a non-digit %q", code, r)
		}
	}
	if _, err := RandomNumericCode(0); err == nil {
		t.Fatal("a zero-length code was accepted")
	}
	if _, err := RandomNumericCode(-1); err == nil {
		t.Fatal("a negative-length code was accepted")
	}
}

// ---------------------------------------------------------------------------
// Keyring
// ---------------------------------------------------------------------------

func newTestKeyring(t *testing.T, secret string) *Keyring {
	t.Helper()
	k, err := NewKeyring(secret)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return k
}

func TestNewKeyringRejectsEmptySecret(t *testing.T) {
	if _, err := NewKeyring(""); err == nil {
		t.Fatal("an empty platform secret produced a keyring; every key would be derived from nothing")
	}
}

func TestEncryptDecryptRoundTrips(t *testing.T) {
	k := newTestKeyring(t, "platform-secret")
	plaintext := []byte("an integration credential")
	ct, err := k.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatal("the ciphertext contains the plaintext")
	}
	got, err := k.Decrypt(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip = %q, want %q", got, plaintext)
	}
}

// A nonce is random per call, so two encryptions of the same plaintext differ.
// Reusing a nonce with GCM is a catastrophic failure, so this is the cheap guard
// against a refactor that hoists the nonce out of the function.
func TestEncryptUsesAFreshNonce(t *testing.T) {
	k := newTestKeyring(t, "platform-secret")
	first, err := k.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	second, err := k.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two encryptions of the same plaintext are identical: the nonce is reused")
	}
}

// The whole point of a keyring derived from the platform secret is that a
// different secret cannot read the data. If this passed, rotating the secret
// would silently do nothing.
func TestDecryptWithADifferentSecretFails(t *testing.T) {
	a := newTestKeyring(t, "secret-one")
	b := newTestKeyring(t, "secret-two")
	ct, err := a.Encrypt([]byte("classified"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := b.Decrypt(ct); err == nil {
		t.Fatal("a ciphertext was decrypted with the wrong keyring")
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	k := newTestKeyring(t, "platform-secret")
	ct, err := k.Encrypt([]byte("integrity matters"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip one bit in the sealed body, after the nonce.
	ct[len(ct)-1] ^= 0x01
	if _, err := k.Decrypt(ct); err == nil {
		t.Fatal("a tampered ciphertext decrypted without error; GCM authentication is not being checked")
	}
}

func TestDecryptRejectsShortCiphertext(t *testing.T) {
	k := newTestKeyring(t, "platform-secret")
	if _, err := k.Decrypt([]byte("short")); err == nil {
		t.Fatal("a ciphertext shorter than the nonce was accepted")
	}
}

// DeriveCSRF binds a token to the session secret, so the same session always gets
// the same token and two sessions never share one.
func TestDeriveCSRFIsStablePerSession(t *testing.T) {
	k := newTestKeyring(t, "platform-secret")
	sessionA := []byte("session-a-secret")
	sessionB := []byte("session-b-secret")

	if k.DeriveCSRF(sessionA) != k.DeriveCSRF(sessionA) {
		t.Fatal("DeriveCSRF is not stable for the same session")
	}
	if k.DeriveCSRF(sessionA) == k.DeriveCSRF(sessionB) {
		t.Fatal("two sessions produced the same CSRF token")
	}
	if _, err := hex.DecodeString(k.DeriveCSRF(sessionA)); err != nil {
		t.Fatalf("CSRF token is not hex: %v", err)
	}
}

func TestWebhookSignCoversSecretTimestampAndBody(t *testing.T) {
	k := newTestKeyring(t, "platform-secret")
	base := k.WebhookSign([]byte("whsec"), "1700000000", []byte(`{"a":1}`))

	if base != k.WebhookSign([]byte("whsec"), "1700000000", []byte(`{"a":1}`)) {
		t.Fatal("WebhookSign is not deterministic")
	}
	if base == k.WebhookSign([]byte("whsec"), "1700000001", []byte(`{"a":1}`)) {
		t.Fatal("changing the timestamp did not change the signature; a replay would be accepted")
	}
	if base == k.WebhookSign([]byte("whsec"), "1700000000", []byte(`{"a":2}`)) {
		t.Fatal("changing the body did not change the signature")
	}
	if base == k.WebhookSign([]byte("other"), "1700000000", []byte(`{"a":1}`)) {
		t.Fatal("changing the secret did not change the signature")
	}
}

func TestSignPayloadAndVerifySignature(t *testing.T) {
	secret := []byte("sender-secret")
	body := []byte(`{"event":"x"}`)
	sig := SignPayload(secret, body)
	if sig != SignPayload(secret, body) {
		t.Fatal("SignPayload is not deterministic")
	}
	if !VerifySignature(sig, sig) {
		t.Fatal("a signature did not verify against itself")
	}
	if VerifySignature(sig, SignPayload([]byte("other"), body)) {
		t.Fatal("a signature verified against the wrong secret")
	}
	if VerifySignature(sig, "not-hex") {
		t.Fatal("a non-hex provided signature was accepted")
	}
	if VerifySignature("not-hex", sig) {
		t.Fatal("a non-hex expected signature was accepted")
	}
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{"db_password", "SMTP_Password", "api_key", "stripe_client_secret", "bot_token", "private_key"}
	for _, key := range sensitive {
		if !IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", key)
		}
	}
	benign := []string{"company_name", "timezone", "default_currency", "logo_url"}
	for _, key := range benign {
		if IsSensitiveKey(key) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", key)
		}
	}
}
