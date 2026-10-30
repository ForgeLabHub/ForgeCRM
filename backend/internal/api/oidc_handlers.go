package api

import (
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
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	providerKey := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerKey == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"provider": "the provider query parameter is required"}))
		return
	}
	target, err := s.deps.OIDC.Begin(r.Context(), providerKey, r.URL.Query().Get("return_to"))
	if err != nil {
		s.respondOIDCError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
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

	identity, err := s.deps.OIDC.Complete(r.Context(), q.Get("code"), q.Get("state"),
		httpx.ClientIP(r), r.UserAgent())
	if err != nil {
		s.redirectSSOFailure(w, r, ssoErrorCode(err))
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
			"linked":      identity.Linked,
			"provisioned": identity.Provisioned,
		},
		IP: httpx.ClientIP(r), UserAgent: r.UserAgent(),
	})

	// A relative path only; sanitizeReturnTo guarantees this at Begin time and the
	// value is re-validated here.
	http.Redirect(w, r, safeSSOReturn(q.Get("return_to")), http.StatusFound)
}

// redirectSSOFailure sends the browser back to the sign-in page with a code the
// interface maps to a message.
func (s *Server) redirectSSOFailure(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?sso_error="+code, http.StatusFound)
}

// safeSSOReturn validates the return target again on the way out, so a value that
// somehow bypassed Begin cannot become an open redirect.
func safeSSOReturn(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.ContainsAny(raw, "\r\n") {
		return "/"
	}
	return raw
}

// ssoErrorCode maps an SSO error to a stable code for the interface.
func ssoErrorCode(err error) string {
	switch {
	case oidc.IsNoAccount(err):
		return "no_account"
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
	case err == oidc.ErrUnknownProvider:
		httpx.WriteError(w, r, httpx.NewError(404, httpx.CodeNotFound, err.Error()))
	case err == oidc.ErrStateInvalid:
		httpx.WriteError(w, r, httpx.NewError(400, httpx.CodeBadRequest, err.Error()))
	case err == oidc.ErrTokenInvalid, err == oidc.ErrEmailNotAllowed:
		httpx.WriteError(w, r, httpx.NewError(401, httpx.CodeUnauthorized, err.Error()))
	case err == oidc.ErrNoAccount:
		httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden, err.Error()))
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
	// A provider created by a tenant administrator is scoped to that tenant unless
	// the caller is a platform owner, who may create a platform-wide one.
	in := req.toServiceInput()
	if in.OrganizationID == nil && !p.IsPlatformOwner {
		orgID := p.OrganizationID
		in.OrganizationID = &orgID
	}
	provider, err := s.deps.OIDC.Create(r.Context(), in, p.UserID)
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
	in := req.toServiceInput()
	provider, err := s.deps.OIDC.Update(r.Context(), id, in, p.UserID)
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
	if err := s.deps.OIDC.Delete(r.Context(), id); err != nil {
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
	if identities == nil {
		identities = []map[string]any{}
	}
	httpx.WriteJSON(w, map[string]any{"items": identities})
}
