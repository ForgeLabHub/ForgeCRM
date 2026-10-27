package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// contextContext, txLike and pgxRows are short aliases that keep the handler
// signatures readable without importing pgx types at every call site.
type (
	contextContext = context.Context
	txLike         = pgx.Tx
	pgxRows        = pgx.Rows
)

// mountAdmin registers user, role, organization, team, audit and diagnostics
// routes.
func (s *Server) mountAdmin(r chi.Router) {
	// Users
	r.Route("/users", func(r chi.Router) {
		r.With(s.perm(rbac.PermUsersRead)).Get("/", s.handleListUsers)
		r.With(s.perm(rbac.PermUsersManage)).Post("/", s.handleInviteUser)
		r.Route("/{id}", func(r chi.Router) {
			r.With(s.perm(rbac.PermUsersRead)).Get("/", s.handleGetUser)
			r.With(s.perm(rbac.PermUsersManage)).Put("/", s.handleUpdateUser)
			r.With(s.perm(rbac.PermUsersManage)).Post("/block", s.handleBlockUser)
			r.With(s.perm(rbac.PermUsersManage)).Post("/unblock", s.handleUnblockUser)
			r.With(s.perm(rbac.PermUsersManage)).Post("/roles", s.handleAssignRole)
			r.With(s.perm(rbac.PermUsersManage)).Delete("/roles/{roleId}", s.handleRevokeRole)
			r.With(s.perm(rbac.PermUsersManage)).Post("/sessions/revoke", s.handleRevokeUserSessions)
		})
	})

	// Roles and permissions
	r.Route("/roles", func(r chi.Router) {
		r.With(s.perm(rbac.PermRolesRead)).Get("/", s.handleListRoles)
		r.With(s.perm(rbac.PermRolesRead)).Get("/permissions", s.handleListPermissions)
		r.With(s.perm(rbac.PermRolesManage)).Post("/", s.handleCreateRole)
		r.With(s.perm(rbac.PermRolesManage)).Put("/{id}/permissions", s.handleSetRolePermissions)
	})

	// Organizations
	r.Route("/organizations", func(r chi.Router) {
		r.With(s.perm(rbac.PermOrganizationsRead)).Get("/", s.handleListOrganizations)
		r.With(s.perm(rbac.PermOrganizationsManage)).Post("/", s.handleCreateOrganization)
	})

	// Teams
	r.Route("/teams", func(r chi.Router) {
		r.With(s.perm(rbac.PermTeamsManage)).Get("/", s.handleListTeams)
		r.With(s.perm(rbac.PermTeamsManage)).Post("/", s.handleCreateTeam)
		r.With(s.perm(rbac.PermTeamsManage)).Post("/{id}/members", s.handleAddTeamMember)
	})

	// Audit, security and logs
	r.With(s.perm(rbac.PermAuditRead)).Get("/audit", s.handleListAudit)
	r.With(s.perm(rbac.PermSecurityRead)).Get("/security-events", s.handleListSecurityEvents)
	r.With(s.perm(rbac.PermLogsRead)).Get("/admin/logs", s.handleListLogs)
	r.With(s.perm(rbac.PermMetricsRead)).Get("/admin/diagnostics", s.handleDiagnostics)
	r.With(s.perm(rbac.PermSystemSettings)).Get("/admin/settings", s.handleListSettings)
	r.With(s.perm(rbac.PermSystemSettings)).Put("/admin/settings", s.handleUpdateSetting)
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

type userSummary struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	Status          string     `json:"status"`
	IsPlatformOwner bool       `json:"is_platform_owner"`
	MFAEnabled      bool       `json:"mfa_enabled"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	Roles           []string   `json:"roles,omitempty"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")

	args := []any{p.OrganizationID}
	where := `WHERE m.organization_id = $1`
	if search != "" {
		args = append(args, "%"+search+"%")
		where += fmt.Sprintf(" AND (u.email ILIKE $%d OR u.display_name ILIKE $%d)", len(args), len(args))
	}
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND u.status = $%d", len(args))
	}

	var total int64
	countQ := `SELECT count(*) FROM users u JOIN memberships m ON m.user_id = u.id ` + where
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), countQ, args...).Scan(&total); err != nil {
		s.respondDomainError(w, r, err)
		return
	}

	args = append(args, pg.Limit, pg.Offset)
	q := `
		SELECT u.id, u.email, u.display_name, u.status, u.is_platform_owner, u.mfa_enabled,
		       u.last_login_at, u.created_at,
		       COALESCE(array_agg(ro.key ORDER BY ro.key) FILTER (WHERE ro.key IS NOT NULL), '{}')
		FROM users u
		JOIN memberships m ON m.user_id = u.id
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles ro ON ro.id = ur.role_id
		` + where + `
		GROUP BY u.id
		ORDER BY u.created_at DESC` + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(), q, args...)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()

	var out []userSummary
	for rows.Next() {
		var u userSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.IsPlatformOwner,
			&u.MFAEnabled, &u.LastLoginAt, &u.CreatedAt, &u.Roles); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out = append(out, u)
	}
	httpx.WriteJSON(w, httpx.NewListResponse(out, total, pg))
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	// A user must be a member of the caller's organization to be visible.
	var u userSummary
	const q = `
		SELECT u.id, u.email, u.display_name, u.status, u.is_platform_owner, u.mfa_enabled,
		       u.last_login_at, u.created_at
		FROM users u JOIN memberships m ON m.user_id = u.id
		WHERE u.id = $1 AND m.organization_id = $2`
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), q, id, p.OrganizationID).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.IsPlatformOwner, &u.MFAEnabled,
		&u.LastLoginAt, &u.CreatedAt); err != nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, u)
}

type inviteUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	RoleKey     string `json:"role_key"`
}

// handleInviteUser creates an account in the invited state.
//
// The account has no password: the invitee sets one through the reset flow, so no
// credential is ever transmitted by the inviting administrator.
func (s *Server) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req inviteUserRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	details := map[string]string{}
	if !looksLikeEmailAddress(req.Email) {
		details["email"] = "a valid email address is required"
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		details["display_name"] = "display_name is required"
	}
	if len(details) > 0 {
		httpx.WriteError(w, r, httpx.ValidationError(details))
		return
	}

	var (
		userID uuid.UUID
		token  string
	)
	err := s.deps.DB.WithTx(r.Context(), func(ctx contextContext, _ txLike) error {
		const ins = `
			INSERT INTO users (email, display_name, status)
			VALUES ($1, $2, 'invited')
			RETURNING id`
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, ins,
			strings.ToLower(strings.TrimSpace(req.Email)), req.DisplayName).Scan(&userID); err != nil {
			if isUniqueViolation(err) {
				return httpx.NewError(409, httpx.CodeConflict, "a user with this email already exists")
			}
			return err
		}
		const mem = `
			INSERT INTO memberships (user_id, organization_id) VALUES ($1, $2)`
		if _, err := s.deps.DB.Q(ctx).Exec(ctx, mem, userID, p.OrganizationID); err != nil {
			return err
		}
		// Grant a role if one was named; the default is the read-only role so an
		// invite never silently confers more access than intended.
		roleKey := req.RoleKey
		if roleKey == "" {
			roleKey = "employee"
		}
		if err := s.grantRoleByKey(ctx, userID, p.OrganizationID, roleKey, p.UserID); err != nil {
			return err
		}
		// Issue a reset token so the invitee can set a password.
		t, _, err := s.deps.Auth.RequestPasswordReset(ctx, strings.ToLower(strings.TrimSpace(req.Email)))
		if err != nil {
			return err
		}
		token = t
		return s.deps.Audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionUserInvited), EntityType: "user", EntityID: userID.String(),
			After: map[string]any{"email": req.Email, "role_key": roleKey},
		})
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}

	response := map[string]any{
		"id":     userID,
		"status": "invited",
		"note":   "the user must set a password before signing in",
	}
	if token != "" {
		if s.deps.Email.Enabled() {
			link := s.deps.Config.BaseURL + "/reset-password?token=" + token
			go s.sendResetEmail(userID, link)
		} else if !s.deps.Config.IsProduction() {
			// Development convenience only; never returned in production.
			response["reset_token"] = token
		}
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, response)
}

// grantRoleByKey grants a role by its key, cloning the tenant role set first if
// the organization does not yet have its own roles.
func (s *Server) grantRoleByKey(ctx contextContext, userID, orgID uuid.UUID, roleKey string, grantedBy uuid.UUID) error {
	var roleID uuid.UUID
	const find = `SELECT id FROM roles WHERE key = $1 AND organization_id = $2`
	err := s.deps.DB.Q(ctx).QueryRow(ctx, find, roleKey, orgID).Scan(&roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Fall back to the platform template.
		const findTemplate = `SELECT id FROM roles WHERE key = $1 AND organization_id IS NULL`
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, findTemplate, roleKey).Scan(&roleID); err != nil {
			return httpx.ValidationError(map[string]string{"role_key": "no role with this key exists"})
		}
	} else if err != nil {
		return err
	}
	return s.deps.RBAC.GrantRole(ctx, userID, roleID, &orgID, grantedBy)
}

type updateUserRequest struct {
	DisplayName string `json:"display_name"`
	Locale      string `json:"locale"`
	Timezone    string `json:"timezone"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if !s.assertSameOrganization(w, r, p, id) {
		return
	}
	var req updateUserRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	const q = `
		UPDATE users SET
			display_name = COALESCE(NULLIF($2, ''), display_name),
			locale = COALESCE(NULLIF($3, ''), locale),
			timezone = COALESCE(NULLIF($4, ''), timezone)
		WHERE id = $1`
	if _, err := s.deps.DB.Q(r.Context()).Exec(r.Context(), q, id, req.DisplayName, req.Locale, req.Timezone); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionUserUpdated), EntityType: "user", EntityID: id.String(),
	})
	httpx.WriteJSON(w, map[string]any{"status": "updated"})
}

// handleBlockUser blocks a user and immediately revokes every session.
//
// The revocation is part of the same transaction as the status change, so there
// is no window in which a blocked user still holds a working session.
func (s *Server) handleBlockUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if id == p.UserID {
		httpx.WriteError(w, r, httpx.NewError(422, httpx.CodeValidation, "you cannot block your own account"))
		return
	}
	if !s.assertSameOrganization(w, r, p, id) {
		return
	}
	err := s.deps.DB.WithTx(r.Context(), func(ctx contextContext, _ txLike) error {
		const upd = `UPDATE users SET status = 'blocked' WHERE id = $1`
		if _, err := s.deps.DB.Q(ctx).Exec(ctx, upd, id); err != nil {
			return err
		}
		const revoke = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
		if _, err := s.deps.DB.Q(ctx).Exec(ctx, revoke, id); err != nil {
			return err
		}
		return s.deps.Audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionUserBlocked), EntityType: "user", EntityID: id.String(),
		})
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "blocked", "sessions_revoked": true})
}

func (s *Server) handleUnblockUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if !s.assertSameOrganization(w, r, p, id) {
		return
	}
	err := s.deps.DB.WithTx(r.Context(), func(ctx contextContext, _ txLike) error {
		const upd = `UPDATE users SET status = 'active', failed_attempts = 0, locked_until = NULL WHERE id = $1`
		if _, err := s.deps.DB.Q(ctx).Exec(ctx, upd, id); err != nil {
			return err
		}
		return s.deps.Audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionUserUnblocked), EntityType: "user", EntityID: id.String(),
		})
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "unblocked"})
}

type assignRoleRequest struct {
	RoleID string `json:"role_id"`
}

func (s *Server) handleAssignRole(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if !s.assertSameOrganization(w, r, p, userID) {
		return
	}
	var req assignRoleRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	roleID, ok := parseUUID(req.RoleID)
	if !ok {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"role_id": "must be a UUID"}))
		return
	}
	// The role must be visible to this organization, which prevents granting a
	// role from another tenant.
	var exists int
	const check = `SELECT 1 FROM roles WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2)`
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), check, roleID, p.OrganizationID).Scan(&exists); err != nil {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
			"role_id": "no such role in this organization"}))
		return
	}
	if err := s.deps.RBAC.GrantRole(r.Context(), userID, roleID, &p.OrganizationID, p.UserID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionRoleGranted), EntityType: "user", EntityID: userID.String(),
		After: map[string]any{"role_id": roleID},
	})
	httpx.WriteJSON(w, map[string]any{"status": "role granted"})
}

func (s *Server) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	roleID, ok := pathUUID(w, r, "roleId")
	if !ok {
		return
	}
	if !s.assertSameOrganization(w, r, p, userID) {
		return
	}
	if err := s.deps.RBAC.RevokeRole(r.Context(), userID, roleID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionRoleRevoked), EntityType: "user", EntityID: userID.String(),
		Before: map[string]any{"role_id": roleID},
	})
	httpx.WriteJSON(w, map[string]any{"status": "role revoked"})
}

func (s *Server) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	userID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if !s.assertSameOrganization(w, r, p, userID) {
		return
	}
	n, err := s.deps.Auth.RevokeAllSessions(r.Context(), userID, nil)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionSessionRevoked), EntityType: "user", EntityID: userID.String(),
		Metadata: map[string]any{"revoked_count": n},
	})
	httpx.WriteJSON(w, map[string]any{"status": "sessions revoked", "count": n})
}

// assertSameOrganization reports whether a target user belongs to the caller's
// organization, writing a 404 if not. Returning 404 rather than 403 avoids
// disclosing that the account exists elsewhere.
func (s *Server) assertSameOrganization(w http.ResponseWriter, r *http.Request, p *rbac.Principal, userID uuid.UUID) bool {
	var exists int
	const q = `SELECT 1 FROM memberships WHERE user_id = $1 AND organization_id = $2`
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), q, userID, p.OrganizationID).Scan(&exists); err != nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Roles and permissions
// ---------------------------------------------------------------------------

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	roles, err := s.deps.RBAC.ListRoles(r.Context(), p.OrganizationID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONList(w, roles)
}

func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	perms := rbac.All()
	out := make([]map[string]string, 0, len(perms))
	for _, perm := range perms {
		out = append(out, map[string]string{"key": string(perm)})
	}
	httpx.WriteJSONList(w, out)
}

type createRoleRequest struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createRoleRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	details := map[string]string{}
	key := strings.ToLower(strings.TrimSpace(req.Key))
	if key == "" || !isValidRoleKey(key) {
		details["key"] = "must start with a letter and contain only lowercase letters, digits and underscores"
	}
	if strings.TrimSpace(req.Name) == "" {
		details["name"] = "name is required"
	}
	// A role cannot be granted a permission that is not in the catalog: this stops
	// a typo from creating a role that silently grants nothing.
	for _, perm := range req.Permissions {
		if !rbac.Known(rbac.Permission(perm)) {
			details["permissions"] = fmt.Sprintf("unknown permission %q", perm)
			break
		}
	}
	if len(details) > 0 {
		httpx.WriteError(w, r, httpx.ValidationError(details))
		return
	}

	var roleID uuid.UUID
	err := s.deps.DB.WithTx(r.Context(), func(ctx contextContext, _ txLike) error {
		const ins = `
			INSERT INTO roles (organization_id, key, name, description, is_system)
			VALUES ($1,$2,$3,$4,false)
			RETURNING id`
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, ins, p.OrganizationID, key, req.Name,
			req.Description).Scan(&roleID); err != nil {
			if isUniqueViolation(err) {
				return httpx.NewError(409, httpx.CodeConflict, "a role with this key already exists")
			}
			return err
		}
		for _, perm := range req.Permissions {
			const grant = `INSERT INTO role_permissions (role_id, permission_key) VALUES ($1,$2)`
			if _, err := s.deps.DB.Q(ctx).Exec(ctx, grant, roleID, perm); err != nil {
				return err
			}
		}
		return s.deps.Audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionRoleCreated), EntityType: "role", EntityID: roleID.String(),
			After: map[string]any{"key": key, "permissions": req.Permissions},
		})
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, map[string]any{"id": roleID, "key": key})
}

type setRolePermissionsRequest struct {
	Permissions []string `json:"permissions"`
}

func (s *Server) handleSetRolePermissions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	roleID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req setRolePermissionsRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	perms := make([]rbac.Permission, 0, len(req.Permissions))
	for _, perm := range req.Permissions {
		if !rbac.Known(rbac.Permission(perm)) {
			httpx.WriteError(w, r, httpx.ValidationError(map[string]string{
				"permissions": fmt.Sprintf("unknown permission %q", perm)}))
			return
		}
		perms = append(perms, rbac.Permission(perm))
	}

	// Capture the previous set so the audit entry records a real diff.
	before, _ := s.rolePermissionKeys(r.Context(), roleID)
	if err := s.deps.RBAC.SetRolePermissions(r.Context(), p.OrganizationID, roleID, perms); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionRoleUpdated), EntityType: "role", EntityID: roleID.String(),
		Before: map[string]any{"permissions": before},
		After:  map[string]any{"permissions": req.Permissions},
	})
	httpx.WriteJSON(w, map[string]any{"status": "permissions updated"})
}

func (s *Server) rolePermissionKeys(ctx contextContext, roleID uuid.UUID) ([]string, error) {
	const q = `SELECT permission_key FROM role_permissions WHERE role_id = $1 ORDER BY permission_key`
	rows, err := s.deps.DB.Q(ctx).Query(ctx, q, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Organizations and teams
// ---------------------------------------------------------------------------

func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	// A non-owner sees only their own organization; a platform owner sees all.
	var (
		rows pgxRows
		err  error
	)
	if p.IsPlatformOwner {
		rows, err = s.deps.DB.Q(r.Context()).Query(r.Context(),
			`SELECT id, name, slug, status, created_at FROM organizations WHERE archived_at IS NULL ORDER BY name`)
	} else {
		rows, err = s.deps.DB.Q(r.Context()).Query(r.Context(), `
			SELECT o.id, o.name, o.slug, o.status, o.created_at
			FROM organizations o JOIN memberships m ON m.organization_id = o.id
			WHERE m.user_id = $1 AND o.archived_at IS NULL ORDER BY o.name`, p.UserID)
	}
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id        uuid.UUID
			name      string
			slug      string
			status    string
			createdAt time.Time
		)
		if err := rows.Scan(&id, &name, &slug, &status, &createdAt); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "name": name, "slug": slug, "status": status, "created_at": createdAt,
		})
	}
	httpx.WriteJSONList(w, out)
}

type createOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createOrganizationRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	details := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		details["name"] = "name is required"
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if slug == "" {
		slug = slugify(req.Name)
	}
	if !isValidSlug(slug) {
		details["slug"] = "must be 2-63 characters of lowercase letters, digits and hyphens"
	}
	if len(details) > 0 {
		httpx.WriteError(w, r, httpx.ValidationError(details))
		return
	}

	var orgID uuid.UUID
	err := s.deps.DB.WithTx(r.Context(), func(ctx contextContext, _ txLike) error {
		const ins = `INSERT INTO organizations (name, slug) VALUES ($1,$2) RETURNING id`
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, ins, req.Name, slug).Scan(&orgID); err != nil {
			if isUniqueViolation(err) {
				return httpx.NewError(409, httpx.CodeConflict, "an organization with this slug already exists")
			}
			return err
		}
		// Clone the platform role templates so the tenant can manage its own roles.
		if _, err := s.deps.RBAC.ClonePlatformRoles(ctx, orgID); err != nil {
			return err
		}
		// The creator becomes the organization's owner.
		const mem = `
			INSERT INTO memberships (user_id, organization_id, is_org_owner)
			VALUES ($1,$2,true) ON CONFLICT (user_id, organization_id) DO UPDATE SET is_org_owner = true`
		if _, err := s.deps.DB.Q(ctx).Exec(ctx, mem, p.UserID, orgID); err != nil {
			return err
		}
		return s.grantRoleByKey(ctx, p.UserID, orgID, "tenant_admin", p.UserID)
	})
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, map[string]any{"id": orgID, "slug": slug})
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	const q = `
		SELECT t.id, t.name, t.description, t.created_at,
		       COALESCE(count(tm.user_id), 0)
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.organization_id = $1 AND t.archived_at IS NULL
		GROUP BY t.id ORDER BY t.name`
	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(), q, p.OrganizationID)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id          uuid.UUID
			name        string
			description *string
			createdAt   time.Time
			members     int64
		)
		if err := rows.Scan(&id, &name, &description, &createdAt, &members); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "name": name, "description": description,
			"created_at": createdAt, "member_count": members,
		})
	}
	httpx.WriteJSONList(w, out)
}

type createTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	var req createTeamRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"name": "name is required"}))
		return
	}
	var teamID uuid.UUID
	const ins = `
		INSERT INTO teams (organization_id, name, description) VALUES ($1,$2,$3) RETURNING id`
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), ins, p.OrganizationID,
		req.Name, nullIfBlank(req.Description)).Scan(&teamID); err != nil {
		if isUniqueViolation(err) {
			httpx.WriteError(w, r, httpx.NewError(409, httpx.CodeConflict, "a team with this name already exists"))
			return
		}
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSONStatus(w, http.StatusCreated, map[string]any{"id": teamID})
}

type addTeamMemberRequest struct {
	UserID string `json:"user_id"`
}

func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	teamID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req addTeamMemberRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	userID, ok := parseUUID(req.UserID)
	if !ok {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"user_id": "must be a UUID"}))
		return
	}
	// The team must belong to the caller's organization and so must the user.
	var exists int
	const checkTeam = `SELECT 1 FROM teams WHERE id = $1 AND organization_id = $2`
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(), checkTeam, teamID, p.OrganizationID).Scan(&exists); err != nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if !s.assertSameOrganization(w, r, p, userID) {
		return
	}
	const ins = `INSERT INTO team_members (team_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`
	if _, err := s.deps.DB.Q(r.Context()).Exec(r.Context(), ins, teamID, userID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, map[string]any{"status": "member added"})
}

// ---------------------------------------------------------------------------
// Audit, security, logs, diagnostics
// ---------------------------------------------------------------------------

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	args := []any{}
	where := "WHERE 1 = 1"

	// A non-platform-owner sees only their own organization's audit trail.
	if !p.IsPlatformOwner {
		args = append(args, p.OrganizationID)
		where += fmt.Sprintf(" AND organization_id = $%d", len(args))
	}
	if v := r.URL.Query().Get("action"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND action = $%d", len(args))
	}
	if v := r.URL.Query().Get("entity_type"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND entity_type = $%d", len(args))
	}
	if v := r.URL.Query().Get("entity_id"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND entity_id = $%d", len(args))
	}
	if v := r.URL.Query().Get("actor_user_id"); v != "" {
		if id, ok := parseUUID(v); ok {
			args = append(args, id)
			where += fmt.Sprintf(" AND actor_user_id = $%d", len(args))
		}
	}
	if v := r.URL.Query().Get("correlation_id"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND correlation_id = $%d", len(args))
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			args = append(args, t)
			where += fmt.Sprintf(" AND created_at >= $%d", len(args))
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			args = append(args, t)
			where += fmt.Sprintf(" AND created_at <= $%d", len(args))
		}
	}

	var total int64
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(),
		"SELECT count(*) FROM audit_log "+where, args...).Scan(&total); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	args = append(args, pg.Limit, pg.Offset)
	q := `
		SELECT id, organization_id, actor_user_id, actor_type, action, entity_type, entity_id,
		       metadata, correlation_id, host(ip), created_at
		FROM audit_log ` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		len(args)-1, len(args))
	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(), q, args...)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id         int64
			orgID      *uuid.UUID
			actorID    *uuid.UUID
			actorType  string
			action     string
			entityType string
			entityID   *string
			metadata   []byte
			corrID     *string
			ip         *string
			createdAt  time.Time
		)
		if err := rows.Scan(&id, &orgID, &actorID, &actorType, &action, &entityType, &entityID,
			&metadata, &corrID, &ip, &createdAt); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "organization_id": orgID, "actor_user_id": actorID, "actor_type": actorType,
			"action": action, "entity_type": entityType, "entity_id": entityID,
			"metadata": rawJSON(metadata), "correlation_id": corrID, "ip": ip, "created_at": createdAt,
		})
	}
	httpx.WriteJSON(w, httpx.NewListResponse(out, total, pg))
}

func (s *Server) handleListSecurityEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	pg := httpx.ParsePagination(r)
	args := []any{}
	where := "WHERE 1 = 1"
	if !p.IsPlatformOwner {
		args = append(args, p.OrganizationID)
		where += fmt.Sprintf(" AND (organization_id = $%d OR organization_id IS NULL)", len(args))
	}
	if v := r.URL.Query().Get("severity"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND severity = $%d", len(args))
	}
	if v := r.URL.Query().Get("event_type"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND event_type = $%d", len(args))
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			args = append(args, t)
			where += fmt.Sprintf(" AND created_at >= $%d", len(args))
		}
	}

	var total int64
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(),
		"SELECT count(*) FROM security_events "+where, args...).Scan(&total); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	args = append(args, pg.Limit, pg.Offset)
	q := `
		SELECT id, organization_id, user_id, event_type, severity, message, metadata,
		       host(ip), correlation_id, created_at
		FROM security_events ` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		len(args)-1, len(args))
	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(), q, args...)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id        int64
			orgID     *uuid.UUID
			userID    *uuid.UUID
			eventType string
			severity  string
			message   string
			metadata  []byte
			ip        *string
			corrID    *string
			createdAt time.Time
		)
		if err := rows.Scan(&id, &orgID, &userID, &eventType, &severity, &message, &metadata,
			&ip, &corrID, &createdAt); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "organization_id": orgID, "user_id": userID, "event_type": eventType,
			"severity": severity, "message": message, "metadata": rawJSON(metadata),
			"ip": ip, "correlation_id": corrID, "created_at": createdAt,
		})
	}
	httpx.WriteJSON(w, httpx.NewListResponse(out, total, pg))
}

func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	pg := httpx.ParsePagination(r)
	args := []any{}
	where := "WHERE 1 = 1"
	if v := r.URL.Query().Get("level"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND level = $%d", len(args))
	}
	if v := r.URL.Query().Get("component"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND component = $%d", len(args))
	}
	if v := r.URL.Query().Get("correlation_id"); v != "" {
		args = append(args, v)
		where += fmt.Sprintf(" AND correlation_id = $%d", len(args))
	}
	if v := r.URL.Query().Get("q"); v != "" {
		args = append(args, "%"+v+"%")
		where += fmt.Sprintf(" AND message ILIKE $%d", len(args))
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			args = append(args, t)
			where += fmt.Sprintf(" AND created_at >= $%d", len(args))
		}
	}

	var total int64
	if err := s.deps.DB.Q(r.Context()).QueryRow(r.Context(),
		"SELECT count(*) FROM app_logs "+where, args...).Scan(&total); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	args = append(args, pg.Limit, pg.Offset)
	q := `
		SELECT id, level, component, message, fields, correlation_id, created_at
		FROM app_logs ` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		len(args)-1, len(args))
	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(), q, args...)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id        int64
			level     string
			component string
			message   string
			fields    []byte
			corrID    *string
			createdAt time.Time
		)
		if err := rows.Scan(&id, &level, &component, &message, &fields, &corrID, &createdAt); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out = append(out, map[string]any{
			"id": id, "level": level, "component": component, "message": message,
			"fields": rawJSON(fields), "correlation_id": corrID, "created_at": createdAt,
		})
	}
	httpx.WriteJSON(w, httpx.NewListResponse(out, total, pg))
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobStats, _ := s.deps.Jobs.Stats(ctx, nil)

	counts := map[string]int64{}
	for name, query := range map[string]string{
		"users":              `SELECT count(*) FROM users`,
		"organizations":      `SELECT count(*) FROM organizations`,
		"accounts":           `SELECT count(*) FROM accounts`,
		"contacts":           `SELECT count(*) FROM contacts`,
		"deals":              `SELECT count(*) FROM deals`,
		"tickets":            `SELECT count(*) FROM tickets`,
		"import_jobs":        `SELECT count(*) FROM import_jobs`,
		"integrations":       `SELECT count(*) FROM integrations`,
		"webhook_deliveries": `SELECT count(*) FROM webhook_deliveries`,
		"audit_entries":      `SELECT count(*) FROM audit_log`,
		"security_events":    `SELECT count(*) FROM security_events`,
	} {
		var n int64
		if err := s.deps.DB.Q(ctx).QueryRow(ctx, query).Scan(&n); err == nil {
			counts[name] = n
		}
	}

	// A migration version tells an operator which schema the running binary
	// expects, which is the first question during an incident.
	var schemaVersion *int
	var dirty *bool
	_ = s.deps.DB.Q(ctx).QueryRow(ctx,
		`SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&schemaVersion, &dirty)

	httpx.WriteJSON(w, map[string]any{
		"service":        "forgecrm",
		"version":        s.deps.Version,
		"commit":         s.deps.BuildCommit,
		"environment":    s.deps.Config.Env,
		"uptime_seconds": int64(time.Since(s.deps.StartedAt).Seconds()),
		"started_at":     s.deps.StartedAt,
		"schema_version": schemaVersion,
		"schema_dirty":   dirty,
		"row_counts":     counts,
		"jobs_by_status": jobStats,
		"subsystems": map[string]any{
			"email":    s.deps.Email.Enabled(),
			"telegram": s.deps.Telegram.Enabled(),
			"ai":       s.deps.AI.Enabled(),
			"oidc":     s.deps.Config.OIDC.Enabled,
		},
		"limits": map[string]any{
			"import_max_upload_bytes":   s.deps.Config.ImportMaxUploadBytes,
			"import_max_unpacked_bytes": s.deps.Config.ImportMaxUnpackedBytes,
			"import_max_archive_files":  s.deps.Config.ImportMaxArchiveFiles,
			"import_max_archive_depth":  s.deps.Config.ImportMaxArchiveDepth,
			"import_max_ratio":          s.deps.Config.ImportMaxRatio,
			"session_ttl":               s.deps.Config.SessionTTL.String(),
			"rate_limit_per_minute":     s.deps.Config.RateLimitPerMin,
		},
	})
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (s *Server) handleListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.deps.DB.Q(r.Context()).Query(r.Context(),
		`SELECT key, value, updated_at FROM app_settings ORDER BY key`)
	if err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var (
			key       string
			value     []byte
			updatedAt time.Time
		)
		if err := rows.Scan(&key, &value, &updatedAt); err != nil {
			s.respondDomainError(w, r, err)
			return
		}
		out[key] = map[string]any{"value": rawJSON(value), "updated_at": updatedAt}
	}
	httpx.WriteJSON(w, map[string]any{"items": out})
}

type updateSettingRequest struct {
	Value any `json:"value"`
}

func (s *Server) handleUpdateSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := s.mustPrincipal(w, r)
	if !ok {
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"key": "the key query parameter is required"}))
		return
	}
	// Refuse to store anything that looks like a credential: settings are shown in
	// the admin UI and must never hold a secret.
	if secrets.IsSensitiveKey(key) {
		httpx.WriteError(w, r, httpx.NewError(422, httpx.CodeValidation,
			"this key looks like a credential; use the relevant integration or notification settings instead"))
		return
	}
	var req updateSettingRequest
	if err := decodeBody(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	payload, err := jsonMarshal(req.Value)
	if err != nil {
		httpx.WriteError(w, r, httpx.ValidationError(map[string]string{"value": "must be valid JSON"}))
		return
	}
	const q = `
		INSERT INTO app_settings (key, value, updated_by) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`
	if _, err := s.deps.DB.Q(r.Context()).Exec(r.Context(), q, key, payload, p.UserID); err != nil {
		s.respondDomainError(w, r, err)
		return
	}
	_ = s.deps.Audit.Record(r.Context(), audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionSettingsUpdated), EntityType: "app_setting", EntityID: key,
		After: map[string]any{"value": req.Value},
	})
	httpx.WriteJSON(w, map[string]any{"status": "updated"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isValidRoleKey(k string) bool {
	if k == "" || k[0] < 'a' || k[0] > 'z' {
		return false
	}
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isValidSlug(s string) bool {
	if len(s) < 2 || len(s) > 63 {
		return false
	}
	if s[0] < 'a' || s[0] > 'z' {
		if s[0] < '0' || s[0] > '9' {
			return false
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func looksLikeEmailAddress(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	domain := s[at+1:]
	return strings.Contains(domain, ".") && !strings.ContainsAny(s, " \t\r\n")
}

func nullIfBlank(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// rawJSON renders a jsonb column value as JSON rather than as base64.
func rawJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func isUniqueViolation(err error) bool { return database.IsUniqueViolation(err) }
