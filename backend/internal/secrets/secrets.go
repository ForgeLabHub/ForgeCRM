// Package secrets provides password hashing, token generation and authenticated
// encryption for credentials stored in the database.
//
// Three distinct keys are derived from the platform secret so that a leak of one
// purpose's key material does not compromise another:
//
//	"forgecrm:csrf:v1"      -> CSRF token derivation
//	"forgecrm:webhook:v1"   -> webhook HMAC signing
//	"forgecrm:secrets:v1"   -> AES-256-GCM key for stored integration credentials
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the Argon2id parameters. They are stored alongside each hash
// so they can be raised later without invalidating existing passwords.
type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params returns the parameters used for new passwords.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// ErrInvalidHash indicates a stored hash that does not match the expected format.
var ErrInvalidHash = errors.New("invalid password hash format")

// ErrPasswordMismatch is returned when a password does not match.
var ErrPasswordMismatch = errors.New("password does not match")

// HashPassword returns an Argon2id hash in the standard PHC string format.
func HashPassword(password string) (string, error) {
	return HashPasswordWithParams(password, DefaultArgon2Params())
}

// HashPasswordWithParams hashes with explicit parameters.
func HashPasswordWithParams(password string, p Argon2Params) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches the encoded hash. Comparison is
// constant-time. The returned error distinguishes a malformed hash (a storage
// bug) from a genuine mismatch.
func VerifyPassword(password, encoded string) error {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return err
	}
	other := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	if subtle.ConstantTimeCompare(key, other) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return Argon2Params{}, nil, nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidHash, version)
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}

// ---------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------

// RandomToken returns n random bytes, URL-safe base64 encoded without padding.
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the SHA-256 of a token. Tokens are stored only as hashes.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// ConstantTimeEqual compares two byte slices without leaking timing.
func ConstantTimeEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// RandomNumericCode returns a numeric code of the given length.
func RandomNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("length must be positive")
	}
	digits := make([]byte, length)
	for i := range digits {
		b := make([]byte, 1)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		digits[i] = '0' + (b[0] % 10)
	}
	return string(digits), nil
}

// ---------------------------------------------------------------------------
// Key derivation and encryption
// ---------------------------------------------------------------------------

// Keyring derives purpose-specific keys from the platform secret.
type Keyring struct {
	csrfKey    []byte
	webhookKey []byte
	aead       cipher.AEAD
}

// NewKeyring derives the purpose-specific keys.
func NewKeyring(platformSecret string) (*Keyring, error) {
	if platformSecret == "" {
		return nil, errors.New("platform secret must not be empty")
	}
	derive := func(purpose string) []byte {
		mac := hmac.New(sha256.New, []byte(platformSecret))
		mac.Write([]byte(purpose))
		return mac.Sum(nil)
	}
	encKey := derive("forgecrm:secrets:v1")
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Keyring{
		csrfKey:    derive("forgecrm:csrf:v1"),
		webhookKey: derive("forgecrm:webhook:v1"),
		aead:       aead,
	}, nil
}

// Encrypt seals plaintext with AES-256-GCM. The nonce is prepended to the output.
func (k *Keyring) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return k.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a ciphertext produced by Encrypt.
func (k *Keyring) Decrypt(ciphertext []byte) ([]byte, error) {
	ns := k.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	plain, err := k.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plain, nil
}

// DeriveCSRF produces a per-session CSRF token from the session's random secret.
// Binding the token to the session means a token stolen from another session is
// useless here.
func (k *Keyring) DeriveCSRF(sessionSecret []byte) string {
	mac := hmac.New(sha256.New, k.csrfKey)
	mac.Write(sessionSecret)
	return hex.EncodeToString(mac.Sum(nil))
}

// WebhookSign produces the HMAC-SHA256 signature for a webhook payload.
func (k *Keyring) WebhookSign(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, k.webhookKey)
	mac.Write(secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignPayload produces a plain HMAC-SHA256 over the payload, used for inbound
// verification where the sender's own secret is the key.
func SignPayload(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// sensitiveKeyMarkers are substrings that identify a setting key as a credential.
// Settings are displayed in the admin UI, so a value stored under such a key would
// be a secret disclosure.
var sensitiveKeyMarkers = []string{
	"password", "passwd", "secret", "token", "api_key", "apikey", "private_key",
	"credential", "client_secret", "access_key", "webhook_secret", "bot_token",
}

// IsSensitiveKey reports whether a setting key looks like it would hold a
// credential.
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range sensitiveKeyMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// VerifySignature compares a provided hex signature against the expected one.
func VerifySignature(expectedHex, providedHex string) bool {
	expected, err1 := hex.DecodeString(expectedHex)
	provided, err2 := hex.DecodeString(providedHex)
	if err1 != nil || err2 != nil {
		return false
	}
	return hmac.Equal(expected, provided)
}
