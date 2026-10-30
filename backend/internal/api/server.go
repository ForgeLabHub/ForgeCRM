// Package api wires the HTTP surface: routing, middleware, and handlers.
//
// Handlers are deliberately thin. They decode and validate input, call a domain
// service, and render the result. Business rules live in the domain packages so
// that the REST API and the MCP tools cannot diverge.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/agent"
	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/forecast"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/oidc"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/ratelimit"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/risk"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/simulate"
	"github.com/geff1778/forgecrm/internal/storage"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// Deps are the services the API needs.
type Deps struct {
	Config       *config.Config
	Log          *slog.Logger
	DB           *database.DB
	Keyring      *secrets.Keyring
	Audit        *audit.Logger
	Auth         *authn.Service
	RBAC         *rbac.Service
	CRM          *crm.Service
	Jobs         *jobs.Queue
	Outbox       *outbox.Store
	Imports      *importx.Service
	Migrations   *migratex.Service
	Integrations *integrations.Hub
	Events       *events.Gateway
	Dispatcher   *events.Dispatcher
	Email        *notify.EmailService
	Telegram     *notify.TelegramService
	AI           *ai.Service
	Tools        *ai.ToolRegistry
	// MCP speaks the Model Context Protocol over JSON-RPC 2.0. It is built from
	// Tools, so it cannot grant more authority than the tool registry already
	// enforces.
	MCP *ai.MCPServer
	// Agent is the human-in-the-loop execution state machine (ADR 0005). It is the
	// only path by which an approval-gated tool can run.
	Agent *agent.Service

	// Workflows is the automation engine. The API and the worker share one engine
	// type, so a workflow defined here runs with exactly the same authorization as
	// one run from the scheduler.
	Workflows *workflow.Engine
	// Simulation is the Simulation Lab. It computes saved scenarios against a
	// baseline read through the CRM service. It cannot modify CRM data.
	Simulation *simulate.Service
	// Forecast computes a revenue forecast from the organization's history. It
	// cannot modify CRM data.
	Forecast *forecast.Service
	// Risk scans the CRM for explainable risk conditions and keeps the signals an
	// operator works through. It cannot modify CRM data.
	Risk *risk.Service
	// Storage is the optional S3-compatible attachment store. A nil or disabled
	// service reports itself off; the document routes answer 503 rather than 500.
	Storage *storage.Service
	// OIDC is the optional corporate single sign-on service. A nil or disabled
	// service reports itself off; the SSO routes answer 503 rather than 500.
	OIDC *oidc.Service
	// StartedAt is used by the system info endpoint.
	StartedAt time.Time
	Version   string
	// BuildCommit identifies the running revision.
	BuildCommit string
}

// Server holds the router and its dependencies.
type Server struct {
	deps         *Deps
	router       chi.Router
	limiter      *ratelimit.Limiter
	loginLimiter *ratelimit.Limiter
	// limiterStore is kept so Close can release it. A Redis-backed store holds a
	// connection pool; leaving it open across a graceful shutdown leaks sockets
	// until the process exits.
	limiterStore ratelimit.Store
	trusted      httpx.TrustedProxies
}

// New builds the API server and its routes.
//
// The limiter store is passed in rather than constructed here, because whether
// counters are per-process or shared is a deployment decision that belongs with
// the rest of the configuration and must be able to fail at startup. See
// NewLimiterStore.
func New(deps *Deps, store ratelimit.Store, trusted httpx.TrustedProxies) (*Server, error) {
	// Authentication endpoints get a tighter budget: this is the endpoint an
	// attacker would use to brute-force a password. The budget is configurable
	// because a shared NAT address consumes it on behalf of everyone behind it.
	//
	// The two policies differ in what happens when the store cannot answer. The
	// login limit is the only barrier to unlimited password guessing, so it refuses
	// when it cannot count; ordinary API traffic allows, because refusing every read
	// over an unavailable counter turns a degraded dependency into an outage.
	loginLimiter, err := ratelimit.New(store, ratelimit.Policy{
		Name:      "login",
		Limit:     deps.Config.LoginRateLimitPerMin,
		Window:    time.Minute,
		FailMode:  ratelimit.FailClosed,
		KeyPrefix: "rl:login",
	})
	if err != nil {
		return nil, err
	}

	apiLimiter, err := ratelimit.New(store, ratelimit.Policy{
		Name:      "api",
		Limit:     deps.Config.RateLimitPerMin,
		Window:    time.Minute,
		FailMode:  ratelimit.FailOpen,
		KeyPrefix: "rl:api",
	})
	if err != nil {
		return nil, err
	}

	s := &Server{
		deps:         deps,
		limiter:      apiLimiter,
		loginLimiter: loginLimiter,
		limiterStore: store,
		trusted:      trusted,
	}
	s.router = s.buildRouter()
	return s, nil
}

// Close releases the limiter store.
func (s *Server) Close() error {
	if s.limiterStore == nil {
		return nil
	}
	return s.limiterStore.Close()
}

// NewLimiterStore builds the counter store named by configuration.
//
// It is a function rather than a few lines in main so that the choice, and the
// failure when Redis is unreachable, is testable without starting a server. A
// Redis store is verified with a PING at construction: without that, a wrong
// address would surface as a per-request warning about an unenforced limit
// rather than as a startup failure next to the setting that caused it.
func NewLimiterStore(ctx context.Context, cfg *config.Config) (ratelimit.Store, error) {
	switch cfg.RateLimitStore {
	case "redis":
		opts, err := redisOptionsFromURL(cfg.RedisURL)
		if err != nil {
			return nil, err
		}
		return ratelimit.NewRedisStore(ctx, opts)
	case "memory", "":
		return ratelimit.NewMemoryStore(), nil
	default:
		// config.Validate already rejects an unknown store, so reaching here means
		// the value changed after validation. Refusing is still the right answer:
		// silently choosing a store would hide which one is running.
		return nil, fmt.Errorf("unknown rate limit store %q", cfg.RateLimitStore)
	}
}

// redisOptionsFromURL parses a redis:// or rediss:// connection string.
//
// A URL is used rather than host/port/password fields because it is one setting
// instead of four, and it can be pasted from a provider's console without being
// reassembled. A password in a URL is still a secret, so the URL is not logged.
func redisOptionsFromURL(raw string) (ratelimit.RedisOptions, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ratelimit.RedisOptions{}, fmt.Errorf("FORGECRM_REDIS_URL is not a valid URL: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return ratelimit.RedisOptions{}, fmt.Errorf("FORGECRM_REDIS_URL must use the redis:// or rediss:// scheme, got %q", u.Scheme)
	}
	if u.Host == "" {
		return ratelimit.RedisOptions{}, fmt.Errorf("FORGECRM_REDIS_URL has no host")
	}

	opts := ratelimit.RedisOptions{Addr: u.Host}

	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			opts.Password = pw
		}
	}

	// The path is the database index: redis://host:6379/2 selects database 2.
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil {
			return ratelimit.RedisOptions{}, fmt.Errorf("FORGECRM_REDIS_URL has a non-numeric database %q", db)
		}
		if n < 0 {
			return ratelimit.RedisOptions{}, fmt.Errorf("FORGECRM_REDIS_URL has a negative database %d", n)
		}
		opts.DB = n
	}

	return opts, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.router }

// Router exposes the router itself, for tools that need to enumerate the route
// table rather than serve requests.
//
// The OpenAPI generator is the caller: it derives the specification from the same
// router the server serves, so the document cannot describe a route that does not
// exist. It is a separate method from Handler because Handler deliberately
// returns the narrower http.Handler, which chi.Walk cannot inspect.
func (s *Server) Router() chi.Router { return s.router }

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(httpx.RequestIDMiddleware)
	r.Use(httpx.RecoverMiddleware(s.deps.Log))
	r.Use(httpx.SecurityHeadersMiddleware)
	// The client address is resolved once, here, against the configured proxy trust
	// list, and carried in the request context. The rate limiter and the audit trail
	// then read the same value instead of each deriving its own from headers the
	// client can set.
	//
	// chi's middleware.RealIP is deliberately not used. It rewrites RemoteAddr from
	// X-Forwarded-For for every caller, which would defeat the trust check below and
	// let any client choose the address it is attributed to.
	r.Use(httpx.ClientIPMiddleware(s.trusted))
	r.Use(httpx.AccessLogMiddleware(s.deps.Log))

	// Health probes are unauthenticated and outside the API version prefix so
	// that infrastructure can reach them without knowing about the API. They are
	// deliberately outside the rate limiter: a throttled probe reports the
	// container as unhealthy, which turns a traffic spike into an outage.
	r.Get("/health", s.handleHealth)
	r.Get("/ready", s.handleReady)

	r.Route("/api/v1", func(r chi.Router) {
		// The rate limiter covers the API subtree only. Health probes above are
		// deliberately exempt so a busy API cannot make the container look dead.
		//
		// The subject is the client address, which ClientIPMiddleware resolved from
		// the peer address unless the peer is a trusted proxy. A client therefore
		// cannot move itself into a fresh bucket by setting a header.
		r.Use(httpx.RateLimitMiddleware(httpx.RateLimitOptions{
			Limiter: s.limiter,
			Subject: httpx.ClientIP,
			Log:     s.deps.Log,
		}))

		r.Route("/auth", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(httpx.RateLimitMiddleware(httpx.RateLimitOptions{
					Limiter: s.loginLimiter,
					Subject: httpx.ClientIP,
					Log:     s.deps.Log,
				}))
				r.Post("/login", s.handleLogin)
				r.Post("/mfa/verify", s.handleMFAVerify)
				r.Post("/password/forgot", s.handlePasswordForgot)
				r.Post("/password/reset", s.handlePasswordReset)
				// Single sign-on. The start and callback are browser navigations, so they
				// are anonymous: there is no session yet. The start route is rate-limited
				// with the login limiter because it is the entry to the credential flow.
				r.Get("/oidc/providers", s.handleListSSOProviders)
				r.With(httpx.RateLimitMiddleware(httpx.RateLimitOptions{
					Limiter: s.loginLimiter,
					Subject: httpx.ClientIP,
					Log:     s.deps.Log,
				})).Get("/oidc/start", s.handleOIDCStart)
				r.Get("/oidc/callback", s.handleOIDCCallback)
			})
			// The remaining auth endpoints require a session, and the
			// state-changing ones also require a CSRF token.
			r.Group(func(r chi.Router) {
				r.Use(s.requireSession)
				r.Get("/me", s.handleMe)
				r.Post("/logout", s.withCSRF(s.handleLogout))
				r.Get("/sessions", s.handleListSessions)
				r.Get("/identities", s.handleListMyIdentities)
				r.Delete("/sessions/{id}", s.withCSRF(s.handleRevokeSession))
				r.Post("/password/change", s.withCSRF(s.handlePasswordChange))
				r.Post("/mfa/enroll", s.withCSRF(s.handleMFAEnroll))
				r.Post("/mfa/confirm", s.withCSRF(s.handleMFAConfirm))
				r.Post("/mfa/disable", s.withCSRF(s.handleMFADisable))
				// Step-up refreshes the second-factor window a destructive agent
				// decision requires (ADR 0005). It is rate-limited with the login
				// limiter because it is a credential-verification endpoint.
				r.With(httpx.RateLimitMiddleware(httpx.RateLimitOptions{
					Limiter: s.loginLimiter,
					Subject: httpx.ClientIP,
					Log:     s.deps.Log,
				})).Post("/step-up", s.withCSRF(s.handleStepUp))
			})
		})

		// Everything below requires an authenticated, MFA-verified session.
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Use(s.withCSRFOnMutations)

			s.mountSystemRoutes(r)
			s.mountCRM(r)
			s.mountAdmin(r)
			s.mountImports(r)
			s.mountMigrations(r)
			s.mountIntegrations(r)
			s.mountEvents(r)
			s.mountJobs(r)
			s.mountAI(r)
			s.mountAIAssist(r)
			s.mountAgent(r)
			s.mountWorkflows(r)
			s.mountSimulation(r)
			s.mountIntelligence(r)
			s.mountDocuments(r)
			s.mountMCP(r)
			s.mountNotifications(r)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.NewError(405, httpx.CodeBadRequest, "method not allowed"))
	})
	return r
}

// ---------------------------------------------------------------------------
// Middleware: session and CSRF
// ---------------------------------------------------------------------------

type principalCtxKey struct{}
type sessionCtxKey struct{}

// principalFrom returns the authenticated caller.
func principalFrom(ctx context.Context) (*rbac.Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(*rbac.Principal)
	return p, ok
}

func sessionFrom(ctx context.Context) (*authn.Session, bool) {
	s, ok := ctx.Value(sessionCtxKey{}).(*authn.Session)
	return s, ok
}

// requireSession resolves the session cookie, loads the user, resolves their
// permissions and attaches a principal to the request context.
//
// Every authorization decision downstream uses this principal, so there is a
// single place where identity is established.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.deps.Auth.SessionCookieName())
		if err != nil || cookie.Value == "" {
			httpx.WriteError(w, r, httpx.ErrUnauth)
			return
		}
		ctx := r.Context()
		sess, err := s.deps.Auth.ResolveSession(ctx, cookie.Value)
		if err != nil {
			httpx.WriteError(w, r, httpx.ErrUnauth)
			return
		}
		user, err := s.deps.Auth.LoadUser(ctx, sess.UserID)
		if err != nil {
			// A blocked user's session stops working immediately, without waiting
			// for the session row to expire.
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
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveOrganization picks the organization the request acts in.
//
// A platform owner with no membership still needs a working context, so a
// fallback organization is created for them on first use. Everyone else must be a
// member of an active organization.
func (s *Server) resolveOrganization(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	const q = `
		SELECT m.organization_id
		FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND m.status = 'active' AND o.status = 'active'
		ORDER BY m.is_org_owner DESC, m.created_at ASC
		LIMIT 1`
	var orgID uuid.UUID
	err := s.deps.DB.Pool().QueryRow(ctx, q, userID).Scan(&orgID)
	if err == nil {
		return orgID, nil
	}

	// No membership: only a platform owner may continue, and gets a default
	// organization so the platform is usable immediately after bootstrap.
	var isOwner bool
	const ownerQ = `SELECT is_platform_owner FROM users WHERE id = $1`
	if err := s.deps.DB.Pool().QueryRow(ctx, ownerQ, userID).Scan(&isOwner); err != nil || !isOwner {
		return uuid.Nil, httpx.NewError(403, httpx.CodeForbidden,
			"your account is not a member of any active organization")
	}
	return s.ensureDefaultOrganization(ctx, userID)
}

func (s *Server) ensureDefaultOrganization(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := s.deps.DB.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const find = `SELECT id FROM organizations WHERE slug = 'default' LIMIT 1`
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, find).Scan(&orgID); err == nil {
			// Membership may be missing if the organization predates this user.
			const mem = `
				INSERT INTO memberships (user_id, organization_id, is_org_owner)
				VALUES ($1, $2, true) ON CONFLICT (user_id, organization_id) DO NOTHING`
			_, err := s.deps.DB.Q(ctx).Exec(ctx, mem, userID, orgID)
			return err
		}
		const ins = `
			INSERT INTO organizations (name, slug) VALUES ('Default organization', 'default')
			RETURNING id`
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, ins).Scan(&orgID); err != nil {
			return fmt.Errorf("create default organization: %w", err)
		}
		const mem = `
			INSERT INTO memberships (user_id, organization_id, is_org_owner)
			VALUES ($1, $2, true) ON CONFLICT (user_id, organization_id) DO NOTHING`
		if _, err := s.deps.DB.Q(ctx).Exec(ctx, mem, userID, orgID); err != nil {
			return err
		}
		// Give the owner every tenant role so the platform is immediately usable.
		if _, err := s.deps.RBAC.ClonePlatformRoles(ctx, orgID); err != nil {
			return err
		}
		const grant = `
			INSERT INTO user_roles (user_id, role_id, organization_id)
			SELECT $1, r.id, $2 FROM roles r
			WHERE r.organization_id = $2 AND r.key = 'tenant_admin'
			ON CONFLICT (user_id, role_id) DO NOTHING`
		_, err := s.deps.DB.Q(ctx).Exec(ctx, grant, userID, orgID)
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	return orgID, nil
}

func (s *Server) userTeams(ctx context.Context, userID, orgID uuid.UUID) ([]uuid.UUID, error) {
	const q = `
		SELECT tm.team_id FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE tm.user_id = $1 AND t.organization_id = $2 AND t.archived_at IS NULL`
	rows, err := s.deps.DB.Q(ctx).Query(ctx, q, userID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// withCSRF requires a valid CSRF token for a single state-changing route.
func (s *Server) withCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkCSRF(w, r) {
			return
		}
		next(w, r)
	}
}

// withCSRFOnMutations applies the CSRF check to every mutating method.
//
// Applying it in one middleware rather than per route means a new endpoint cannot
// be added without protection by accident.
func (s *Server) withCSRFOnMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !s.checkCSRF(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return false
	}
	token := r.Header.Get(s.deps.Config.CSRFHeader)
	if token == "" {
		// A double-submit fallback: the token may also be presented in a form
		// field for non-JavaScript clients.
		token = r.FormValue("csrf_token")
	}
	if err := s.deps.Auth.VerifyCSRF(sess, token); err != nil {
		httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden,
			"CSRF token is missing or invalid"))
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Middleware: permissions
// ---------------------------------------------------------------------------

// requirePermission builds middleware that enforces one permission.
//
// Denials are audited, because a pattern of denials is itself a security signal.
func (s *Server) requirePermission(perm rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := principalFrom(r.Context())
			if !ok {
				httpx.WriteError(w, r, httpx.ErrUnauth)
				return
			}
			if !p.Has(perm) {
				_ = s.deps.Audit.Record(r.Context(), audit.Entry{
					OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
					Action: "authorization.denied", EntityType: "permission", EntityID: string(perm),
					Metadata: map[string]any{"method": r.Method, "path": r.URL.Path},
					IP:       httpx.ClientIP(r),
				})
				httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden,
					fmt.Sprintf("permission %s is required", perm)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// perm is shorthand for mounting a route with a permission requirement.
func (s *Server) perm(perm rbac.Permission) func(http.Handler) http.Handler {
	return s.requirePermission(perm)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mustPrincipal returns the caller, or writes a 401 and reports false.
func (s *Server) mustPrincipal(w http.ResponseWriter, r *http.Request) (*rbac.Principal, bool) {
	p, ok := principalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.ErrUnauth)
		return nil, false
	}
	return p, true
}

// pathUUID parses a UUID path parameter.
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := httpx.PathParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{name: "must be a UUID"}))
		return uuid.Nil, false
	}
	return id, true
}

// respondDomainError maps a domain error to the API error envelope.
func (s *Server) respondDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, crm.ErrNotFound), errors.Is(err, httpx.ErrNotFound):
		httpx.WriteError(w, r, httpx.ErrNotFound)
	case errors.Is(err, crm.ErrConflict):
		httpx.WriteError(w, r, httpx.NewError(409, httpx.CodeConflict, err.Error()))
	case errors.Is(err, rbac.ErrForbidden):
		httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden, err.Error()))
	case errors.Is(err, authn.ErrMFAInvalid), errors.Is(err, authn.ErrInvalidCredentials),
		errors.Is(err, authn.ErrAccountLocked), errors.Is(err, authn.ErrAccountInactive),
		errors.Is(err, authn.ErrSessionInvalid), errors.Is(err, authn.ErrResetTokenInvalid),
		errors.Is(err, authn.ErrWeakPassword), errors.Is(err, authn.ErrCSRF):
		httpx.WriteError(w, r, mapAuthError(err))
	case errors.Is(err, ai.ErrDisabled), errors.Is(err, ai.ErrRateLimited),
		errors.Is(err, ai.ErrProviderUnavailable):
		httpx.WriteError(w, r, ai.HTTPErrorFrom(err))
	case errors.Is(err, oidc.ErrDisabled):
		httpx.WriteError(w, r, httpx.ErrDisabled)
	case errors.Is(err, oidc.ErrUnknownProvider):
		httpx.WriteError(w, r, httpx.NewError(404, httpx.CodeNotFound, err.Error()))
	case errors.Is(err, oidc.ErrStateInvalid):
		httpx.WriteError(w, r, httpx.NewError(400, httpx.CodeBadRequest, err.Error()))
	case errors.Is(err, oidc.ErrTokenInvalid), errors.Is(err, oidc.ErrEmailNotAllowed):
		httpx.WriteError(w, r, httpx.NewError(401, httpx.CodeUnauthorized, err.Error()))
	case errors.Is(err, oidc.ErrNoAccount):
		httpx.WriteError(w, r, httpx.NewError(403, httpx.CodeForbidden, err.Error()))
	case errors.Is(err, oidc.ErrProviderUnreachable):
		httpx.WriteError(w, r, httpx.NewError(502, httpx.CodeInternal, err.Error()))
	case errors.Is(err, oidc.ErrInvalidProvider):
		httpx.WriteError(w, r, httpx.NewError(422, httpx.CodeValidation, err.Error()))
	case errors.Is(err, storage.ErrNotConfigured):
		httpx.WriteError(w, r, httpx.ErrDisabled)
	case errors.Is(err, storage.ErrObjectMissing):
		// The row exists but the object is gone. Reported as not found rather than as a
		// server error, because there is no object to serve and the client cannot fix
		// it by retrying.
		httpx.WriteError(w, r, httpx.ErrNotFound)
	default:
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			httpx.WriteError(w, r, apiErr)
			return
		}
		// An unexpected error is logged with detail and returned as a generic 500,
		// so internal structure never reaches the client.
		s.deps.Log.Error("unhandled handler error",
			"path", r.URL.Path, "request_id", httpx.RequestID(r.Context()), "error", err)
		httpx.WriteError(w, r, httpx.ErrInternal)
	}
}

func mapAuthError(err error) error {
	switch {
	case errors.Is(err, authn.ErrInvalidCredentials):
		return httpx.NewError(401, httpx.CodeUnauthorized, "invalid email or password")
	case errors.Is(err, authn.ErrAccountLocked):
		return httpx.NewError(423, httpx.CodeForbidden,
			"this account is temporarily locked after repeated failed sign-ins")
	case errors.Is(err, authn.ErrAccountInactive):
		return httpx.NewError(403, httpx.CodeForbidden, "this account is not active")
	case errors.Is(err, authn.ErrSessionInvalid):
		return httpx.NewError(401, httpx.CodeUnauthorized, "session is invalid or expired")
	case errors.Is(err, authn.ErrMFAInvalid):
		return httpx.NewError(401, httpx.CodeUnauthorized, "invalid multi-factor code")
	case errors.Is(err, authn.ErrResetTokenInvalid):
		return httpx.NewError(400, httpx.CodeBadRequest, "the password reset link is invalid or has expired")
	case errors.Is(err, authn.ErrWeakPassword):
		return httpx.NewError(422, httpx.CodeValidation, err.Error())
	case errors.Is(err, authn.ErrCSRF):
		return httpx.NewError(403, httpx.CodeForbidden, "CSRF token is missing or invalid")
	default:
		return httpx.ErrInternal
	}
}

// decodeBody is a convenience wrapper with the default size limit.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	return httpx.DecodeJSON(w, r, dst, 1<<20)
}

// decodeBodyLimited decodes a body with an explicit size limit.
func decodeBodyLimited(w http.ResponseWriter, r *http.Request, dst any, max int64) error {
	return httpx.DecodeJSON(w, r, dst, max)
}

// queryBool reads a boolean query parameter.
func queryBool(r *http.Request, name string, def bool) bool {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// marshalJSONOrEmpty returns "{}" for an empty value.
func marshalJSONOrEmpty(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 {
		return json.RawMessage("{}")
	}
	return b
}
