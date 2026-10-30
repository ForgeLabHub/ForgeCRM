// Command smoke exercises the oidc package's public API in a clean module.
//
// A real sign-in needs a database and a provider, so it is not driven here. What
// is driven is the surface an operator and an anonymous visitor depend on and that
// needs no server: a provider's public shape carries no client secret, a service
// with no configuration reports off instead of panicking, and the callback URL is
// derived from the configured base URL.
package main

import (
	"fmt"
	"os"
	"reflect"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/oidc"
)

func main() {
	// A provider's public shape is the only thing an unauthenticated client
	// receives, so it must not be able to carry the secret. This is checked by
	// reflection rather than by reading a field, because the guarantee is about the
	// type: a field added later would otherwise leak silently.
	secret := oidc.Provider{
		Key:          "acme",
		DisplayName:  "Acme",
		Issuer:       "https://idp.example.com",
		ClientID:     "client",
		ClientSecret: "super-secret-value",
	}
	pub := secret.Public()
	if pub.Key != "acme" || pub.DisplayName != "Acme" {
		fail("Public() dropped the identity fields: %+v", pub)
	}
	pubType := reflect.TypeOf(pub)
	for i := 0; i < pubType.NumField(); i++ {
		if name := pubType.Field(i).Name; name == "ClientSecret" || name == "Secret" {
			fail("PublicProvider exposes a secret field %q", name)
		}
	}

	// Enabled() is nil-safe: a subsystem that was never constructed reports off.
	var unbuilt *oidc.Service
	if unbuilt.Enabled() {
		fail("a nil service reported enabled")
	}

	cfg := &config.Config{}
	svc := oidc.NewService(nil, cfg, nil, nil, nil)
	if svc.Enabled() {
		fail("an unconfigured service reported enabled")
	}
	cfg.OIDC.Enabled = true
	if !svc.Enabled() {
		fail("an enabled service reported off")
	}

	// The callback URL is derived from the base URL, trimming a trailing slash so
	// the value registered with a provider is exact.
	cfg.BaseURL = "https://crm.example.com/"
	want := "https://crm.example.com/api/v1/auth/oidc/callback"
	if got := svc.RedirectURL(); got != want {
		fail("RedirectURL() = %q, want %q", got, want)
	}
	// An explicit redirect URL wins over the derived one.
	cfg.OIDC.RedirectURL = "https://sso.example.com/callback"
	if got := svc.RedirectURL(); got != "https://sso.example.com/callback" {
		fail("RedirectURL() ignored the configured value: %q", got)
	}

	// LoginURL builds the route a caller follows to start a sign-in.
	if got := svc.LoginURL("acme", "/deals"); got != "/api/v1/auth/oidc/start?provider=acme&return_to=%2Fdeals" {
		fail("LoginURL() = %q", got)
	}

	fmt.Println("smoke: oidc ok")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
