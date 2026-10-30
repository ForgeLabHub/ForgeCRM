package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/mcpcred"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// mountMCPCredentials registers the MCP credential lifecycle routes.
//
// These are session-only routes, deliberately: a credential may not mint another
// credential, so an external client that has one cannot extend its own reach. Only
// an operator with a browser session and the manage permission can create, rotate
// or revoke. Reading the list needs the read permission.
func (s *Server) mountMCPCredentials(r chi.Router) {
	r.Route("/mcp/credentials", func(r chi.Router) {
		r.Use(s.requireService(func() bool { return s.deps.MCPCredentials != nil }, "MCP credentials"))
		r.With(s.perm(rbac.PermMCPCredentialsRead)).Get("/", s.handleListMCPCredentials)
		r.With(s.perm(rbac.PermMCPCredentialsManage)).Post("/", s.handleCreateMCPCredential)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermMCPCredentialsManage)).Delete("/", s.handleRevokeMCPCredential)
			r.With(s.perm(rbac.PermMCPCredentialsManage)).Post("/rotate", s.handleRotateMCPCredential)
		})
	})
}

func (s *Server) handleListMCPCredentials(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.deps.MCPCredentials.List(r.Context(), p)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, items)
}

func (s *Server) handleCreateMCPCredential(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var in mcpcred.CreateInput
	if err := httpx.DecodeJSON(w, r, &in, 16*1024); err != nil {
		return
	}
	created, err := s.deps.MCPCredentials.Create(r.Context(), p, in)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, created)
}

func (s *Server) handleRevokeMCPCredential(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.MCPCredentials.Revoke(r.Context(), p, id); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateMCPCredential(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	created, err := s.deps.MCPCredentials.Rotate(r.Context(), p, id)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, created)
}

// ---------------------------------------------------------------------------
// Middleware: MCP authentication
// ---------------------------------------------------------------------------

// requireMCPAuth authenticates the MCP protocol endpoints with either a session
// cookie or a bearer credential.
//
// This is what makes a real MCP client possible: a long-running client cannot hold
// an interactive session, so it presents an `Authorization: Bearer fcmcp_...`
// credential instead. The two paths converge on one principal, so every tool call
// downstream sees exactly the authority of whichever was presented — the session's
// roles, or the credential's own narrower scope. Neither is widened here.
//
// A session is tried first, because the admin UI already has one and should keep
// working unchanged. A request with no cookie falls through to the credential. A
// request with a cookie that does not resolve is not retried against the header:
// presenting an expired session is an authentication failure, not an invitation to
// try a different credential, and silently switching would make a revocation look
// like it did not take effect.
func (s *Server) requireMCPAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if cookie, err := r.Cookie(s.deps.Auth.SessionCookieName()); err == nil && cookie.Value != "" {
			s.authenticateMCPWithSession(ctx, w, r, cookie.Value, next)
			return
		}

		token := bearerToken(r)
		if token == "" {
			httpx.WriteError(w, r, httpx.ErrUnauth)
			return
		}
		if s.deps.MCPCredentials == nil {
			httpx.WriteError(w, r, httpx.ErrUnauth)
			return
		}
		principal, cred, err := s.deps.MCPCredentials.Authenticate(ctx, token)
		if err != nil {
			// The reason is not echoed: whether a token is unknown, revoked or expired is
			// not the caller's business, and saying so helps an attacker probe.
			s.deps.Log.Warn("MCP credential rejected", "error", err)
			httpx.WriteError(w, r, httpx.ErrUnauth)
			return
		}

		// The per-credential budget is spent here, after authentication so the key is a
		// real credential id, and before the tool runs so a noisy client is refused
		// before it costs a query.
		if s.mcpLimiter != nil {
			decision := s.mcpLimiter.Allow(ctx, cred.ID.String(), time.Now())
			httpx.WriteRateLimitHeaders(w, decision)
			if !decision.Allowed {
				httpx.WriteError(w, r, httpx.NewError(http.StatusTooManyRequests, httpx.CodeRateLimited,
					"this MCP credential has exceeded its request budget"))
				return
			}
		}

		ctx = context.WithValue(ctx, principalCtxKey{}, principal)
		ctx = logging.WithActor(ctx, principal.UserID.String())
		ctx = logging.WithOrganization(ctx, principal.OrganizationID.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticateMCPWithSession resolves a session cookie to a principal, mirroring
// requireSession but without the CSRF layer, which a bearer client does not have.
func (s *Server) authenticateMCPWithSession(ctx context.Context, w http.ResponseWriter, r *http.Request,
	cookie string, next http.Handler) {

	sess, err := s.deps.Auth.ResolveSession(ctx, cookie)
	if err != nil {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}
	user, err := s.deps.Auth.LoadUser(ctx, sess.UserID)
	if err != nil {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return
	}
	orgID, err := s.resolveOrganization(ctx, user.ID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	perms, err := s.deps.RBAC.EffectivePermissions(ctx, user.ID, orgID)
	if err != nil {
		s.deps.Log.Error("resolve permissions", "error", err, "user_id", user.ID)
		httpx.WriteError(w, r, httpx.ErrInternal)
		return
	}
	principal := rbac.NewPrincipal(user.ID, user.Email, user.DisplayName, orgID, perms)
	principal.IsPlatformOwner = user.IsPlatformOwner
	principal.SessionID = sess.ID
	if teams, err := s.userTeams(ctx, user.ID, orgID); err == nil {
		principal.TeamIDs = teams
	}

	ctx = context.WithValue(ctx, principalCtxKey{}, principal)
	ctx = context.WithValue(ctx, sessionCtxKey{}, sess)
	ctx = logging.WithActor(ctx, user.ID.String())
	ctx = logging.WithOrganization(ctx, orgID.String())
	s.deps.Auth.TouchSession(ctx, sess.ID)

	// A session-authenticated call is still a browser call and keeps the CSRF
	// double-submit check on mutations, exactly as the session-only routes do. A
	// bearer-authenticated call has no cookie to ride and needs no CSRF token, so it
	// is not subject to this check.
	r = r.WithContext(ctx)
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if !s.checkCSRF(w, r) {
			return
		}
	}
	next.ServeHTTP(w, r)
}

// bearerToken extracts a credential from the Authorization header, if present.
func bearerToken(r *http.Request) string {
	raw := r.Header.Get("Authorization")
	if raw == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(raw[len(prefix):])
}
