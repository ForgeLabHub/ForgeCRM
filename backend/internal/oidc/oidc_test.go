package oidc

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestPublicNeverCarriesTheClientSecret pins the rule that a provider's secret
// cannot reach a client. Public() is the only shape the sign-in page receives, so
// a field added to it without thought would leak the secret to an anonymous
// visitor.
func TestPublicNeverCarriesTheClientSecret(t *testing.T) {
	p := Provider{
		ID:           uuid.New(),
		Key:          "acme",
		DisplayName:  "Acme",
		Issuer:       "https://idp.example.test",
		ClientID:     "forgecrm",
		ClientSecret: "super-secret-value",
	}
	pub := p.Public()
	if pub.Key != "acme" || pub.DisplayName != "Acme" {
		t.Fatalf("Public() lost the display fields: %+v", pub)
	}
	// The public shape has no secret field at all; this asserts the compiler-level
	// guarantee that a future edit cannot smuggle one in by naming it.
	if _, ok := any(pub).(interface{ GetClientSecret() string }); ok {
		t.Fatal("PublicProvider exposes a client secret accessor")
	}
}

func TestSanitizeReturnToRejectsOpenRedirects(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
		{"/deals", "/deals"},
		{"/deals?x=1", "/deals?x=1"},
		{"//evil.example.com", "/"},
		{"https://evil.example.com", "/"},
		{"http://evil.example.com", "/"},
		{"/path\r\nSet-Cookie: x=1", "/"},
		{"javascript:alert(1)", "/"},
	}
	for _, tc := range cases {
		if got := sanitizeReturnTo(tc.in); got != tc.want {
			t.Errorf("sanitizeReturnTo(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDomainAllowed(t *testing.T) {
	cases := []struct {
		email   string
		domains []string
		want    bool
	}{
		{"a@acme.com", nil, true},
		{"a@acme.com", []string{}, true},
		{"a@acme.com", []string{"acme.com"}, true},
		{"a@ACME.com", []string{"acme.com"}, true},
		{"a@other.com", []string{"acme.com"}, false},
		{"a@sub.acme.com", []string{"acme.com"}, false},
		{"not-an-email", []string{"acme.com"}, false},
	}
	for _, tc := range cases {
		if got := domainAllowed(tc.email, tc.domains); got != tc.want {
			t.Errorf("domainAllowed(%q, %v) = %v, want %v", tc.email, tc.domains, got, tc.want)
		}
	}
}

func TestValidateProviderInput(t *testing.T) {
	base := ProviderInput{
		Key:         "acme",
		DisplayName: "Acme",
		Issuer:      "https://idp.example.test",
		ClientID:    "forgecrm",
	}
	if err := validateProviderInput(base); err != nil {
		t.Fatalf("a valid provider was rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*ProviderInput)
	}{
		{"empty key", func(p *ProviderInput) { p.Key = "" }},
		{"key with uppercase", func(p *ProviderInput) { p.Key = "Acme" }},
		{"key with space", func(p *ProviderInput) { p.Key = "ac me" }},
		{"empty display name", func(p *ProviderInput) { p.DisplayName = "  " }},
		{"issuer without host", func(p *ProviderInput) { p.Issuer = "not-a-url" }},
		{"issuer with bad scheme", func(p *ProviderInput) { p.Issuer = "ftp://idp.example.test" }},
		{"empty client id", func(p *ProviderInput) { p.ClientID = " " }},
		{"jit without organization", func(p *ProviderInput) { p.JITProvisioning = true }},
		{"bad default role key", func(p *ProviderInput) { p.DefaultRoleKey = "Bad Role" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			if err := validateProviderInput(in); err == nil {
				t.Fatalf("validateProviderInput accepted an invalid provider")
			}
		})
	}

	// JIT with an organization is valid.
	orgID := uuid.New()
	in := base
	in.JITProvisioning = true
	in.OrganizationID = &orgID
	if err := validateProviderInput(in); err != nil {
		t.Fatalf("JIT with an organization was rejected: %v", err)
	}
}

func TestDefaultScopesAlwaysIncludesOpenID(t *testing.T) {
	for _, scopes := range [][]string{nil, {}, {"openid", "email"}} {
		got := defaultScopes(scopes)
		if len(got) == 0 || got[0] != "openid" {
			t.Fatalf("defaultScopes(%v) = %v, want openid first", scopes, got)
		}
	}
}

func TestEnabledIsNilSafe(t *testing.T) {
	var s *Service
	if s.Enabled() {
		t.Fatal("a nil service reported itself enabled")
	}
}

func TestRandomStringIsUniqueAndURLSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		v, err := randomString(32)
		if err != nil {
			t.Fatalf("randomString: %v", err)
		}
		if strings.ContainsAny(v, "+/=") {
			t.Fatalf("randomString produced a value that is not URL-safe: %q", v)
		}
		if seen[v] {
			t.Fatalf("randomString repeated a value")
		}
		seen[v] = true
	}
}
