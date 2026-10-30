package oidc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// oidcProvider is the discovered metadata for an issuer: the authorization,
// token and JWKS endpoints. It is the go-oidc provider type, aliased so the rest
// of the package does not depend on the library's name.
type oidcProvider = gooidc.Provider

// discoveryClient builds the HTTP client used for discovery, the token exchange
// and JWKS fetching.
//
// It has a bounded timeout because a provider that hangs must not hold a request
// open indefinitely, and it is a plain client rather than the SSRF-guarded one:
// the issuer is operator configuration, and a corporate provider is frequently on
// a private address (an internal Keycloak, for example) which the guarded client
// deliberately refuses.
func discoveryClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// discover returns the provider metadata for an issuer, using the cache when it
// is fresh.
func (s *Service) discover(ctx context.Context, issuer string) (*oidcProvider, error) {
	s.mu.Lock()
	if c, ok := s.providers[issuer]; ok && time.Since(c.fetchedAt) < providerCacheTTL {
		s.mu.Unlock()
		return c.provider, nil
	}
	s.mu.Unlock()

	ctx = gooidc.ClientContext(ctx, discoveryClient())
	p, err := gooidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: discovery failed for %q: %v", ErrProviderUnreachable, issuer, err)
	}

	s.mu.Lock()
	s.providers[issuer] = cachedProvider{provider: p, fetchedAt: time.Now()}
	s.mu.Unlock()
	return p, nil
}

// oauthConfig builds the OAuth2 client configuration for a provider.
func (s *Service) oauthConfig(ctx context.Context, p *Provider) oauth2.Config {
	endpoint := oauth2.Endpoint{}
	if discovered, err := s.discover(ctx, p.Issuer); err == nil {
		endpoint = discovered.Endpoint()
	}
	return oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  s.RedirectURL(),
		Scopes:       defaultScopes(p.Scopes),
	}
}

// idTokenVerifier builds a verifier that checks the signature against the
// provider's JWKS and the standard claims against this client.
func (s *Service) idTokenVerifier(ctx context.Context, p *Provider) (*gooidc.IDTokenVerifier, error) {
	discovered, err := s.discover(ctx, p.Issuer)
	if err != nil {
		return nil, err
	}
	return discovered.Verifier(&gooidc.Config{ClientID: p.ClientID}), nil
}

// ---------------------------------------------------------------------------
// Thin wrappers around the library helpers, so the flow code names intent rather
// than the library.
// ---------------------------------------------------------------------------

// generateVerifier creates a PKCE code verifier.
func generateVerifier() string { return oauth2.GenerateVerifier() }

// s256ChallengeOption sends the S256 challenge for the verifier.
func s256ChallengeOption(verifier string) oauth2.AuthCodeOption {
	return oauth2.S256ChallengeOption(verifier)
}

// verifierOption presents the verifier at the token exchange.
func verifierOption(verifier string) oauth2.AuthCodeOption {
	return oauth2.VerifierOption(verifier)
}

// nonceOption adds the nonce to the authorization request.
func nonceOption(nonce string) oauth2.AuthCodeOption {
	return gooidc.Nonce(nonce)
}

// IsNoAccount reports whether an error is the "no linked account" case, so a
// handler can answer with guidance rather than a generic failure.
func IsNoAccount(err error) bool { return errors.Is(err, ErrNoAccount) }
