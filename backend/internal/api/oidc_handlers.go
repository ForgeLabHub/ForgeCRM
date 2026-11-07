package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/oidc"
)

// ---------------------------------------------------------------------------
// Sign-in flow (anonymous)
// ---------------------------------------------------------------------------

// ssoProvidersResponse is what the sign-in page reads to decide whether to show a
// single sign-on button. It contains only the providers an unauthenticated
// visitor may choose from, with no secrets.
type ssoProvidersResponse struct {
	Enabled   bool                  `json:"enabled"`
	Providers []oidc.PublicProvider `json:"providers"`
}

// ssoStateCookieName carries the browser binding for a sign-in that is in flight.
// It is separate from the session cookie: it is short lived and is cleared as soon
// as the callback reads it.
const ssoStateCookieName = "forgecrm_sso_state"

// setSSOStateCookie records the binding secret the callback must see again. It is
// HttpOnly so script cannot read it, and its lifetime matches the state row.
func (s *Server) setSSOStateCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     ssoStateCookieName,
		Value:    value,
		Path:     "/",
		Domain:   s.deps.Config.CookieDomain,
		MaxAge:   int(oidc.StateTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.deps.Config.CookieSecure,
		SameSite: s.ssoStateCookieSameSite(),
	})
}

// clearSSOStateCookie removes the binding cookie. It is called on every callback,
// success or failure, so a completed or rejected state leaves nothing behind.
func (s *Server) clearSSOStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     ssoStateCookieName,
		Value:    "",
		Path:     "/",
		Domain:   s.deps.Config.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.deps.Config.CookieSecure,
		SameSite: s.ssoStateCookieSameSite(),
	})
}

// ssoStateCookieSameSite never returns Strict: the identity provider sends the
// browser back with a cross-site top-level navigation, and a Strict cookie is
// withheld on that navigation, which would break every sign-in.
func (s *Server) ssoStateCookieSameSite() http.SameSite {
	if s.deps.Config.CookieSameSite == "none" {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

func (s *Server) handleListSSOProviders(w http.ResponseWriter, r *http.Request) {
	if !s.deps.OIDC.Enabled() {
		httpx.WriteJSON(w, ssoProvidersResponse{Enabled: false, Providers: []oidc.PublicProvider{}})
		return
	}
	providers, err := s.deps.OIDC.EnabledProviders(r.Context())
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	if providers == nil {
		providers = []oidc.PublicProvider{}
	}
	httpx.WriteJSON(w, ssoProvidersResponse{Enabled: true, Providers: providers})
}

// handleOIDCStart begins the authorization-code flow and redirects the browser to
// the identity provider.
//
// It is a redirect rather than a JSON response because the browser must leave the
// application for the provider's own sign-in page.
//
// This route is anonymous: it only ever starts a sign-in, never a binding, so a
// request that carries a session still resolves the account by subject alone. The
// explicit-binding entry point is handleOIDCLinkStart, which is POST and CSRF-checked.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	providerKey := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerKey == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"provider": "the provider query parameter is required"}))
		return
	}
	target, binding, err := s.deps.OIDC.Begin(r.Context(), providerKey, r.URL.Query().Get("return_to"), uuid.Nil)
	if err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	// The browser must present this cookie when the provider redirects back, so a
	// state captured from this flow cannot be completed in another browser.
	s.setSSOStateCookie(w, binding)
	http.Redirect(w, r, target, http.StatusFound)
}

// oidcLinkStartRequest asks for the authorization URL that binds a provider to the
// caller's own account.
type oidcLinkStartRequest struct {
	Provider string `json:"provider"`
}

// oidcLinkStartResponse carries the URL the browser must visit to complete the
// binding at the provider.
type oidcLinkStartResponse struct {
	AuthorizationURL string `json:"authorization_url"`
}

// handleOIDCLinkStart begins an explicit binding: the caller, already signed in,
// asks to attach a provider identity to their own account.
//
// It is a POST that returns the URL rather than a redirect, so the request carries
// the session cookie *and* a CSRF token. A top-level GET navigation would send the
// cookie under the SameSite=Lax default without proving the user's intent, which is
// exactly the shape of an attack that binds an attacker's identity to a victim's
// account.
func (s *Server) handleOIDCLinkStart(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}
	var req oidcLinkStartRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	providerKey := strings.TrimSpace(req.Provider)
	if providerKey == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"provider": "the provider is required"}))
		return
	}
	target, binding, err := s.deps.OIDC.Begin(r.Context(), providerKey, "/admin/security", sess.UserID)
	if err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	s.setSSOStateCookie(w, binding)
	httpx.WriteJSON(w, oidcLinkStartResponse{AuthorizationURL: target})
}

// handleOIDCCallback completes the flow: it exchanges the code, verifies the ID
// token, resolves the local account and issues a session.
//
// Failures redirect back to the sign-in page with an error code rather than
// returning JSON, because the browser is mid-navigation and cannot read a body.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// The provider may report a failure instead of returning a code.
	if errCode := q.Get("error"); errCode != "" {
		s.deps.Log.Warn("identity provider returned an error", "error", errCode,
			"description", q.Get("error_description"))
		s.redirectSSOFailure(w, r, "provider_error")
		return
	}

	// Read the binding before anything else can clear it, then drop the cookie so
	// it cannot be replayed. The service compares it against the hash stored with
	// the state; a state presented without its cookie is refused.
	binding := ""
	if c, err := r.Cookie(ssoStateCookieName); err == nil {
		binding = c.Value
	}
	s.clearSSOStateCookie(w)

	identity, err := s.deps.OIDC.Complete(r.Context(), q.Get("code"), q.Get("state"), binding,
		httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		if oidc.IsBindingError(err) {
			s.redirectSSOBindingFailure(w, r, ssoErrorCode(err))
			return
		}
		s.redirectSSOFailure(w, r, ssoErrorCode(err))
		return
	}

	if identity.Bound {
		// An explicit binding. The caller was already signed in and keeps that
		// session; issuing a new one would only churn sessions for no gain.
		http.Redirect(w, r, "/admin/security?identity_linked=1", http.StatusFound)
		return
	}

	if identity.OrganizationID == nil {
		// The account authenticated but belongs to no organization, so it has no
		// tenant to act in. Refusing here is clearer than issuing a session that
		// every tenant-scoped request would reject.
		s.deps.Log.Warn("SSO sign-in for an account with no membership", "user_id", identity.UserID)
		s.redirectSSOFailure(w, r, "no_organization")
		return
	}

	sess, token, err := s.deps.Auth.IssueSSOSession(r.Context(), identity.UserID,
		httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		s.redirectSSOFailure(w, r, ssoErrorCode(err))
		return
	}

	s.deps.Auth.SetSessionCookie(w, token, sess.ExpiresAt)

	s.deps.Audit.RecordSecurityAsync(audit.SecurityEvent{
		UserID: audit.UUIDPtr(identity.UserID), Type: "auth.sso_login", Severity: "info",
		Message: "single sign-on login succeeded",
		Metadata: map[string]any{
			"provider":    identity.ProviderKey,
			"linked":      identity.IdentityExists,
			"provisioned": identity.Provisioned,
		},
		IP: httpx.ClientIP(r), UserAgent: r.UserAgent(),
	})

	// A relative path only; SafeReturnTo guarantees this at Begin time and the
	// value is re-validated here.
	http.Redirect(w, r, safeSSOReturn(q.Get("return_to")), http.StatusFound)
}

// redirectSSOFailure sends the browser back to the sign-in page with a code the
// interface maps to a message. A binding failure takes the other path, through
// redirectSSOBindingFailure, because the user is still signed in.
func (s *Server) redirectSSOFailure(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?sso_error="+code, http.StatusFound)
}

// redirectSSOBindingFailure lands a failed binding on the Security Center, where
// the user started it, instead of on the sign-in page. The user is still signed in
// (binding never re-authenticates), so sending them to sign in would be confusing.
func (s *Server) redirectSSOBindingFailure(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/admin/security?sso_error="+code, http.StatusFound)
}

// safeSSOReturn validates the return target again on the way out, so a value that
// somehow bypassed Begin cannot become an open redirect. It delegates to the one
// implementation in internal/oidc: a second copy of this check is free to drift,
// and the drift is the open redirect.
func safeSSOReturn(raw string) string {
	return oidc.SafeReturnTo(raw)
}

// ssoErrorCode maps an SSO error to a stable code for the interface.
func ssoErrorCode(err error) string {
	switch {
	case oidc.IsNoAccount(err):
		return "no_account"
	case oidc.IsBindingConflict(err):
		return "identity_conflict"
	case errors.Is(err, oidc.ErrAccountExists):
		return "account_exists"
	case strings.Contains(err.Error(), "not active"):
		return "account_inactive"
	default:
		return "sign_in_failed"
	}
}

func (s *Server) respondOIDCError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == oidc.ErrDisabled:
		httpx.WriteError(w, r, httpx.ErrDisabled)
	case err == oidc.ErrUnknownProvider, err == oidc.ErrProviderNotInScope:
		httpx.WriteError(w, r, httpx.NewError(404, httpx.CodeNotFound, err.Error()))
	case err == oidc.ErrStateInvalid:
		httpx.WriteError(w, r, httpx.NewError(400, httpx.CodeBadRequest, err.Error()))
	case err == oidc.ErrTokenInvalid, err == oidc.ErrEmailNotAllowed:
		httpx.WriteError(w, r, httpx.NewError(401, httpx.CodeUnauthorized, err.Error()))
	case err == oidc.ErrNoAccount:
		httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden, err.Error()))
	case err == oidc.ErrIdentityAlreadyLinked, err == oidc.ErrIdentityInUse:
		httpx.WriteError(w, r, httpx.NewError(409, httpx.CodeConflict, err.Error()))
	case err == oidc.ErrIdentityNotFound:
		httpx.WriteError(w, r, httpx.NewError(404, httpx.CodeNotFound, err.Error()))
	case err == oidc.ErrProviderUnreachable:
		httpx.WriteError(w, r, httpx.NewError(502, httpx.CodeInternal, err.Error()))
	default:
		httpx.WriteError(w, r, httpx.NewError(422, httpx.CodeValidation, err.Error()))
	}
}

// ---------------------------------------------------------------------------
// Administration (sso.read / sso.manage)
// ---------------------------------------------------------------------------

type oidcProviderResponse struct {
	ID                  uuid.UUID  `json:"id"`
	OrganizationID      *uuid.UUID `json:"organization_id,omitempty"`
	Key                 string     `json:"key"`
	DisplayName         string     `json:"display_name"`
	Issuer              string     `json:"issuer"`
	ClientID            string     `json:"client_id"`
	Scopes              []string   `json:"scopes"`
	AllowedEmailDomains []string   `json:"allowed_email_domains"`
	JITProvisioning     bool       `json:"jit_provisioning"`
	DefaultRoleKey      string     `json:"default_role_key,omitempty"`
	Enabled             bool       `json:"enabled"`
	// HasSecret reports whether a client secret is stored, without revealing it.
	HasSecret bool `json:"has_secret"`
}

func toOIDCProviderResponse(p oidc.Provider) oidcProviderResponse {
	return oidcProviderResponse{
		ID:                  p.ID,
		OrganizationID:      p.OrganizationID,
		Key:                 p.Key,
		DisplayName:         p.DisplayName,
		Issuer:              p.Issuer,
		ClientID:            p.ClientID,
		Scopes:              p.Scopes,
		AllowedEmailDomains: p.AllowedEmailDomains,
		JITProvisioning:     p.JITProvisioning,
		DefaultRoleKey:      p.DefaultRoleKey,
		Enabled:             p.Enabled,
		HasSecret:           p.ClientSecret != "",
	}
}

// oidcProviderListResponse is the administration list. RedirectURL is included so
// an operator can copy the exact callback to register with the provider, rather
// than reconstructing it from the base URL and getting it subtly wrong.
type oidcProviderListResponse struct {
	Items       []oidcProviderResponse `json:"items"`
	RedirectURL string                 `json:"redirect_url"`
}

func (s *Server) handleListOIDCProviders(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	providers, err := s.deps.OIDC.List(r.Context(), &p.OrganizationID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	items := make([]oidcProviderResponse, 0, len(providers))
	for _, pr := range providers {
		items = append(items, toOIDCProviderResponse(pr))
	}
	httpx.WriteJSON(w, oidcProviderListResponse{
		Items:       items,
		RedirectURL: s.deps.OIDC.RedirectURL(),
	})
}

type oidcProviderInput struct {
	OrganizationID      *uuid.UUID `json:"organization_id"`
	Key                 string     `json:"key"`
	DisplayName         string     `json:"display_name"`
	Issuer              string     `json:"issuer"`
	ClientID            string     `json:"client_id"`
	ClientSecret        string     `json:"client_secret"`
	Scopes              []string   `json:"scopes"`
	AllowedEmailDomains []string   `json:"allowed_email_domains"`
	JITProvisioning     bool       `json:"jit_provisioning"`
	DefaultRoleKey      string     `json:"default_role_key"`
	Enabled             *bool      `json:"enabled"`
}

func (in oidcProviderInput) toServiceInput() oidc.ProviderInput {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	return oidc.ProviderInput{
		OrganizationID:      in.OrganizationID,
		Key:                 strings.TrimSpace(in.Key),
		DisplayName:         strings.TrimSpace(in.DisplayName),
		Issuer:              strings.TrimSpace(in.Issuer),
		ClientID:            strings.TrimSpace(in.ClientID),
		ClientSecret:        in.ClientSecret,
		Scopes:              in.Scopes,
		AllowedEmailDomains: in.AllowedEmailDomains,
		JITProvisioning:     in.JITProvisioning,
		DefaultRoleKey:      strings.TrimSpace(in.DefaultRoleKey),
		Enabled:             enabled,
	}
}

func (s *Server) handleCreateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req oidcProviderInput
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// A tenant administrator creates a provider inside their own organization
	// whatever the body asked for; only a platform owner may create the
	// platform-wide provider every tenant signs in through. The scope is passed to
	// the service so the decision does not depend on the handler alone.
	scope := oidc.ProviderScope{OrgID: p.OrganizationID, IsPlatformOwner: p.IsPlatformOwner}
	provider, err := s.deps.OIDC.Create(r.Context(), req.toServiceInput(), scope, p.UserID)
	if err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: "sso.provider_created", EntityType: "oidc_provider", EntityID: provider.Key,
		After: map[string]any{"key": provider.Key, "issuer": provider.Issuer},
	})
	httpx.WriteJSONStatus(w, http.StatusCreated, toOIDCProviderResponse(*provider))
}

func (s *Server) handleUpdateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req oidcProviderInput
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	scope := oidc.ProviderScope{OrgID: p.OrganizationID, IsPlatformOwner: p.IsPlatformOwner}
	provider, err := s.deps.OIDC.Update(r.Context(), id, req.toServiceInput(), scope, p.UserID)
	if err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: "sso.provider_updated", EntityType: "oidc_provider", EntityID: provider.Key,
		After: map[string]any{"key": provider.Key, "enabled": provider.Enabled},
	})
	httpx.WriteJSON(w, toOIDCProviderResponse(*provider))
}

func (s *Server) handleDeleteOIDCProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	scope := oidc.ProviderScope{OrgID: p.OrganizationID, IsPlatformOwner: p.IsPlatformOwner}
	if err := s.deps.OIDC.Delete(r.Context(), id, scope); err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: "sso.provider_deleted", EntityType: "oidc_provider", EntityID: id.String(),
	})
	httpx.WriteJSON(w, map[string]any{"status": "deleted"})
}

// handleListMyIdentities lists the caller's own provider links.
func (s *Server) handleListMyIdentities(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}
	identities, err := s.deps.OIDC.ListIdentities(r.Context(), sess.UserID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, identities)
}

// handleUnlinkMyIdentity removes one of the caller's own provider links. The user
// id comes from the session and is part of the delete predicate, so a caller can
// only remove a link they own.
func (s *Server) handleUnlinkMyIdentity(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.OIDC.UnlinkIdentity(r.Context(), sess.UserID, id, httpx.ClientIP(r), r.UserAgent()); err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
