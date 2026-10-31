package config

import (
	"strings"
	"testing"
)

// These pin the validation added alongside the schema gate: a mistyped URL, a
// half-specified bootstrap, or a production deployment missing a security
// setting must fail at startup with an error that names the variable, rather
// than surfacing later as a redirect, a connection error or an insecure cookie.
//
// The bootstrap pairing lives in ValidateBootstrap rather than Validate, because
// the one-off `-bootstrap-admin` command legitimately supplies the address
// through a flag and the password through the environment. Those tests call the
// method the server-start path calls.

func TestURLsAreParsedNotMerelyPresent(t *testing.T) {
	cases := []struct {
		name string
		set  func(*testing.T)
		want string
	}{
		{
			name: "base url with a bogus scheme",
			set:  func(t *testing.T) { t.Setenv("FORGECRM_BASE_URL", "htp://example.com") },
			want: "FORGECRM_BASE_URL",
		},
		{
			name: "base url without a host",
			set:  func(t *testing.T) { t.Setenv("FORGECRM_BASE_URL", "https:///path") },
			want: "FORGECRM_BASE_URL",
		},
		{
			name: "database url that is not postgres",
			set:  func(t *testing.T) { t.Setenv("FORGECRM_DATABASE_URL", "mysql://user@host/db") },
			want: "FORGECRM_DATABASE_URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			tc.set(t)
			_, err := Load()
			if err == nil {
				t.Fatal("an invalid URL was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %s", err, tc.want)
			}
		})
	}
}

func TestBootstrapRequiresBothValues(t *testing.T) {
	cfg := &Config{Bootstrap: BootstrapConfig{AdminEmail: "owner@example.com"}}
	err := cfg.ValidateBootstrap()
	if err == nil {
		t.Fatal("a bootstrap email without a password was accepted; the account could not be created safely")
	}
	if !strings.Contains(err.Error(), "FORGECRM_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Fatalf("the error does not name the missing variable: %v", err)
	}
}

func TestBootstrapPasswordWithoutEmailIsRejected(t *testing.T) {
	cfg := &Config{Bootstrap: BootstrapConfig{AdminPassword: "Str0ng-Password-Here"}}
	err := cfg.ValidateBootstrap()
	if err == nil {
		t.Fatal("a bootstrap password without an email was accepted")
	}
	if !strings.Contains(err.Error(), "FORGECRM_BOOTSTRAP_ADMIN_EMAIL") {
		t.Fatalf("the error does not name the missing variable: %v", err)
	}
}

func TestNeitherBootstrapValueIsFine(t *testing.T) {
	baseEnv(t)
	if _, err := Load(); err != nil {
		t.Fatalf("a deployment without bootstrap variables must be valid: %v", err)
	}
	if err := (&Config{}).ValidateBootstrap(); err != nil {
		t.Fatalf("an empty bootstrap must be a no-op, not an error: %v", err)
	}
}

// Validate must accept a half-specified bootstrap, because the command path
// combines a flag with the environment and calls Validate before it runs. The
// pairing is enforced by ValidateBootstrap and by bootstrap.Admin.
func TestValidateAcceptsAFlagDrivenBootstrap(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_BOOTSTRAP_ADMIN_PASSWORD", "Str0ng-Password-Here")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a password supplied for the -bootstrap-admin command was rejected at load: %v", err)
	}
	if err := cfg.ValidateBootstrap(); err == nil {
		t.Fatal("ValidateBootstrap accepted a password with no environment email")
	}
}

func TestProductionRequiresHardenings(t *testing.T) {
	cases := []struct {
		name string
		set  func(*testing.T)
		want string
	}{
		{
			name: "short secret key",
			set: func(t *testing.T) {
				t.Setenv("FORGECRM_SECRET_KEY", "too-short")
				t.Setenv("FORGECRM_COOKIE_SECURE", "true")
				t.Setenv("FORGECRM_BASE_URL", "https://crm.example.com")
			},
			want: "FORGECRM_SECRET_KEY",
		},
		{
			name: "cookie not secure",
			set: func(t *testing.T) {
				t.Setenv("FORGECRM_SECRET_KEY", strings.Repeat("k", 40))
				t.Setenv("FORGECRM_COOKIE_SECURE", "false")
				t.Setenv("FORGECRM_BASE_URL", "https://crm.example.com")
			},
			want: "FORGECRM_COOKIE_SECURE",
		},
		{
			name: "base url over plain http",
			set: func(t *testing.T) {
				t.Setenv("FORGECRM_SECRET_KEY", strings.Repeat("k", 40))
				t.Setenv("FORGECRM_COOKIE_SECURE", "true")
				t.Setenv("FORGECRM_BASE_URL", "http://crm.example.com")
			},
			want: "FORGECRM_BASE_URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			t.Setenv("FORGECRM_ENV", "production")
			tc.set(t)
			_, err := Load()
			if err == nil {
				t.Fatal("a production configuration with a security downgrade was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %s", err, tc.want)
			}
		})
	}
}

func TestProductionHappyPathLoads(t *testing.T) {
	baseEnv(t)
	t.Setenv("FORGECRM_ENV", "production")
	t.Setenv("FORGECRM_SECRET_KEY", strings.Repeat("k", 40))
	t.Setenv("FORGECRM_COOKIE_SECURE", "true")
	t.Setenv("FORGECRM_BASE_URL", "https://crm.example.com")

	if _, err := Load(); err != nil {
		t.Fatalf("a correct production configuration was rejected: %v", err)
	}
}

// The redacted rendering is logged at startup, so it must never carry a secret
// value even though it deliberately reports whether each secret is present.
func TestRedactedNeverContainsSecretValues(t *testing.T) {
	baseEnv(t)
	secret := "super-secret-signing-material-0123456789"
	t.Setenv("FORGECRM_SECRET_KEY", secret)
	t.Setenv("FORGECRM_METRICS_TOKEN", "metrics-token-value")
	t.Setenv("FORGECRM_BOOTSTRAP_ADMIN_EMAIL", "owner@example.com")
	t.Setenv("FORGECRM_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-password-value")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rendered := ""
	for _, attr := range cfg.Redacted() {
		rendered += toString(attr) + " "
	}
	for _, leak := range []string{secret, "metrics-token-value", "bootstrap-password-value"} {
		if strings.Contains(rendered, leak) {
			t.Fatalf("the redacted configuration leaked %q: %s", leak, rendered)
		}
	}
	if !strings.Contains(rendered, "present") {
		t.Fatalf("the redacted configuration does not report presence: %s", rendered)
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
