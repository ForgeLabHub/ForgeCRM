// Package rbac defines the permission catalog and the authorization checks that
// every API and MCP tool must go through.
//
// Permissions are data. A role is a named bundle of permissions stored in the
// database (see migration 0004). Nothing in this package or any handler should
// branch on a role key: the only question asked is "does this principal hold this
// permission, in this organization?".
package rbac

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
)

// Permission is a dotted, namespaced capability identifier.
type Permission string

// Permission catalog. The set must stay in sync with migration 0004; the
// TestPermissionCatalogMatchesDatabase test enforces that.
const (
	PermContactsRead    Permission = "contacts.read"
	PermContactsCreate  Permission = "contacts.create"
	PermContactsUpdate  Permission = "contacts.update"
	PermContactsArchive Permission = "contacts.archive"
	PermContactsExport  Permission = "contacts.export"

	PermAccountsRead    Permission = "accounts.read"
	PermAccountsCreate  Permission = "accounts.create"
	PermAccountsUpdate  Permission = "accounts.update"
	PermAccountsArchive Permission = "accounts.archive"
	PermAccountsExport  Permission = "accounts.export"

	PermLeadsRead    Permission = "leads.read"
	PermLeadsCreate  Permission = "leads.create"
	PermLeadsUpdate  Permission = "leads.update"
	PermLeadsArchive Permission = "leads.archive"
	PermLeadsConvert Permission = "leads.convert"

	PermDealsRead            Permission = "deals.read"
	PermDealsCreate          Permission = "deals.create"
	PermDealsUpdate          Permission = "deals.update"
	PermDealsArchive         Permission = "deals.archive"
	PermDealsExport          Permission = "deals.export"
	PermDealsApproveDiscount Permission = "deals.approve_discount"
	PermDealsChangeStage     Permission = "deals.change_stage"

	PermPipelinesRead   Permission = "pipelines.read"
	PermPipelinesManage Permission = "pipelines.manage"

	PermTasksRead        Permission = "tasks.read"
	PermTasksCreate      Permission = "tasks.create"
	PermTasksUpdate      Permission = "tasks.update"
	PermTasksAssign      Permission = "tasks.assign"
	PermActivitiesRead   Permission = "activities.read"
	PermActivitiesCreate Permission = "activities.create"

	PermTicketsRead   Permission = "tickets.read"
	PermTicketsCreate Permission = "tickets.create"
	PermTicketsUpdate Permission = "tickets.update"
	PermTicketsAssign Permission = "tickets.assign"
	PermTicketsClose  Permission = "tickets.close"
	PermSLAManage     Permission = "sla.manage"

	PermDocumentsRead      Permission = "documents.read"
	PermDocumentsCreate    Permission = "documents.create"
	PermDocumentsArchive   Permission = "documents.archive"
	PermTagsManage         Permission = "tags.manage"
	PermCustomFieldsManage Permission = "custom_fields.manage"
	PermCommentsCreate     Permission = "comments.create"

	PermUsersRead           Permission = "users.read"
	PermUsersManage         Permission = "users.manage"
	PermRolesRead           Permission = "roles.read"
	PermRolesManage         Permission = "roles.manage"
	PermOrganizationsRead   Permission = "organizations.read"
	PermOrganizationsManage Permission = "organizations.manage"
	PermTeamsManage         Permission = "teams.manage"

	PermImportsRead       Permission = "imports.read"
	PermImportsCreate     Permission = "imports.create"
	PermImportsExecute    Permission = "imports.execute"
	PermImportsApprove    Permission = "imports.approve"
	PermExportsRead       Permission = "exports.read"
	PermExportsExecute    Permission = "exports.execute"
	PermMigrationsRead    Permission = "migrations.read"
	PermMigrationsManage  Permission = "migrations.manage"
	PermDataManageQuality Permission = "data.manage_quality"

	PermIntegrationsRead      Permission = "integrations.read"
	PermIntegrationsConfigure Permission = "integrations.configure"
	PermEventsRead            Permission = "events.read"
	PermEventsManage          Permission = "events.manage"
	PermWebhooksManage        Permission = "webhooks.manage"

	PermAuditRead         Permission = "audit.read"
	PermSecurityRead      Permission = "security.read"
	PermSecurityConfigure Permission = "security.configure"
	PermLogsRead          Permission = "logs.read"
	PermMetricsRead       Permission = "metrics.read"
	PermJobsRead          Permission = "jobs.read"
	PermJobsManage        Permission = "jobs.manage"

	PermReportsRead   Permission = "reports.read"
	PermReportsExport Permission = "reports.export"

	PermNotificationsRead Permission = "notifications.read"
	PermEmailManage       Permission = "email.manage"
	PermTelegramManage    Permission = "telegram.manage"

	PermAIRead         Permission = "ai.read"
	PermAIToolsExecute Permission = "ai.tools.execute"
	PermAIConfigure    Permission = "ai.configure"

	PermApprovalsRead   Permission = "approvals.read"
	PermApprovalsDecide Permission = "approvals.decide"
	PermSystemSettings  Permission = "system.settings"
)

// All returns every permission in the catalog, sorted.
func All() []Permission {
	out := make([]Permission, 0, len(catalog))
	for p := range catalog {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

var catalog = map[Permission]struct{}{
	PermContactsRead: {}, PermContactsCreate: {}, PermContactsUpdate: {},
	PermContactsArchive: {}, PermContactsExport: {},
	PermAccountsRead: {}, PermAccountsCreate: {}, PermAccountsUpdate: {},
	PermAccountsArchive: {}, PermAccountsExport: {},
	PermLeadsRead: {}, PermLeadsCreate: {}, PermLeadsUpdate: {},
	PermLeadsArchive: {}, PermLeadsConvert: {},
	PermDealsRead: {}, PermDealsCreate: {}, PermDealsUpdate: {},
	PermDealsArchive: {}, PermDealsExport: {}, PermDealsApproveDiscount: {},
	PermDealsChangeStage: {},
	PermPipelinesRead:    {}, PermPipelinesManage: {},
	PermTasksRead: {}, PermTasksCreate: {}, PermTasksUpdate: {}, PermTasksAssign: {},
	PermActivitiesRead: {}, PermActivitiesCreate: {},
	PermTicketsRead: {}, PermTicketsCreate: {}, PermTicketsUpdate: {},
	PermTicketsAssign: {}, PermTicketsClose: {}, PermSLAManage: {},
	PermDocumentsRead: {}, PermDocumentsCreate: {}, PermDocumentsArchive: {},
	PermTagsManage: {}, PermCustomFieldsManage: {}, PermCommentsCreate: {},
	PermUsersRead: {}, PermUsersManage: {}, PermRolesRead: {}, PermRolesManage: {},
	PermOrganizationsRead: {}, PermOrganizationsManage: {}, PermTeamsManage: {},
	PermImportsRead: {}, PermImportsCreate: {}, PermImportsExecute: {},
	PermImportsApprove: {}, PermExportsRead: {}, PermExportsExecute: {},
	PermMigrationsRead: {}, PermMigrationsManage: {}, PermDataManageQuality: {},
	PermIntegrationsRead: {}, PermIntegrationsConfigure: {},
	PermEventsRead: {}, PermEventsManage: {}, PermWebhooksManage: {},
	PermAuditRead: {}, PermSecurityRead: {}, PermSecurityConfigure: {},
	PermLogsRead: {}, PermMetricsRead: {}, PermJobsRead: {}, PermJobsManage: {},
	PermReportsRead: {}, PermReportsExport: {},
	PermNotificationsRead: {}, PermEmailManage: {}, PermTelegramManage: {},
	PermAIRead: {}, PermAIToolsExecute: {}, PermAIConfigure: {},
	PermApprovalsRead: {}, PermApprovalsDecide: {}, PermSystemSettings: {},
}

// Known reports whether p is part of the catalog.
func Known(p Permission) bool {
	_, ok := catalog[p]
	return ok
}

// ErrForbidden is returned when a principal lacks a permission.
var ErrForbidden = errors.New("forbidden")

// Principal is an authenticated caller together with the permissions resolved for
// the organization it is currently acting in.
type Principal struct {
	UserID           uuid.UUID
	Email            string
	DisplayName      string
	IsPlatformOwner  bool
	OrganizationID   uuid.UUID
	OrganizationName string
	IsOrgOwner       bool
	SessionID        uuid.UUID
	TeamIDs          []uuid.UUID
	permissions      map[Permission]struct{}
}

// NewPrincipal builds a principal from resolved permissions.
func NewPrincipal(userID uuid.UUID, email, displayName string, orgID uuid.UUID, perms []Permission) *Principal {
	set := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		set[p] = struct{}{}
	}
	return &Principal{
		UserID:         userID,
		Email:          email,
		DisplayName:    displayName,
		OrganizationID: orgID,
		permissions:    set,
	}
}

// Has reports whether the principal holds a permission. A platform owner holds
// every permission, which keeps the bootstrap account usable without granting it
// the whole catalog explicitly.
func (p *Principal) Has(perm Permission) bool {
	if p == nil {
		return false
	}
	if p.IsPlatformOwner {
		return true
	}
	_, ok := p.permissions[perm]
	return ok
}

// HasAll reports whether every listed permission is held.
func (p *Principal) HasAll(perms ...Permission) bool {
	for _, perm := range perms {
		if !p.Has(perm) {
			return false
		}
	}
	return true
}

// HasAny reports whether at least one listed permission is held.
func (p *Principal) HasAny(perms ...Permission) bool {
	for _, perm := range perms {
		if p.Has(perm) {
			return true
		}
	}
	return false
}

// Permissions returns the sorted permission set, for /auth/me and diagnostics.
func (p *Principal) Permissions() []Permission {
	out := make([]Permission, 0, len(p.permissions))
	for perm := range p.permissions {
		out = append(out, perm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// InTeam reports whether the principal belongs to a team, used by record-level
// access rules.
func (p *Principal) InTeam(teamID uuid.UUID) bool {
	for _, id := range p.TeamIDs {
		if id == teamID {
			return true
		}
	}
	return false
}

// Require returns ErrForbidden unless the principal holds perm.
func (p *Principal) Require(perm Permission) error {
	if !p.Has(perm) {
		return fmt.Errorf("%w: %s", ErrForbidden, perm)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// Service resolves a user's effective permissions in an organization.
type Service struct {
	db *database.DB
}

// NewService builds an RBAC service.
func NewService(db *database.DB) *Service { return &Service{db: db} }

// EffectivePermissions returns the union of permissions granted by every role the
// user holds in the organization, plus any organization-independent roles.
//
// The query is deliberately a single statement so that a partial failure cannot
// produce an over-permissive result.
func (s *Service) EffectivePermissions(ctx context.Context, userID, orgID uuid.UUID) ([]Permission, error) {
	const q = `
		SELECT DISTINCT rp.permission_key
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.user_id = $1
		  AND (ur.organization_id IS NULL OR ur.organization_id = $2)
		ORDER BY rp.permission_key`

	rows, err := s.db.Q(ctx).Query(ctx, q, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("resolve permissions: %w", err)
	}
	defer rows.Close()

	var perms []Permission
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, Permission(key))
	}
	return perms, rows.Err()
}

// RoleIDsForUser returns the role IDs granted to a user in an organization.
func (s *Service) RoleIDsForUser(ctx context.Context, userID, orgID uuid.UUID) ([]uuid.UUID, error) {
	const q = `
		SELECT role_id FROM user_roles
		WHERE user_id = $1 AND (organization_id IS NULL OR organization_id = $2)`
	rows, err := s.db.Q(ctx).Query(ctx, q, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
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

// ClonePlatformRoles copies the platform role templates into an organization so
// that the tenant can adjust its own permission bundles without affecting other
// tenants. Returns the number of roles created.
func (s *Service) ClonePlatformRoles(ctx context.Context, orgID uuid.UUID) (int, error) {
	const q = `
		WITH templates AS (
			SELECT r.id, r.key, r.name, r.description
			FROM roles r
			WHERE r.organization_id IS NULL AND r.is_system
		),
		inserted AS (
			INSERT INTO roles (organization_id, key, name, description, is_system)
			SELECT $1, t.key, t.name, t.description, true FROM templates t
			ON CONFLICT DO NOTHING
			RETURNING id, key
		)
		INSERT INTO role_permissions (role_id, permission_key)
		SELECT i.id, rp.permission_key
		FROM inserted i
		JOIN templates t ON t.key = i.key
		JOIN role_permissions rp ON rp.role_id = t.id
		ON CONFLICT DO NOTHING`

	tag, err := s.db.Q(ctx).Exec(ctx, q, orgID)
	if err != nil {
		return 0, fmt.Errorf("clone platform roles: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// GrantRole assigns a role to a user, optionally scoped to an organization.
func (s *Service) GrantRole(ctx context.Context, userID, roleID uuid.UUID, orgID *uuid.UUID, grantedBy uuid.UUID) error {
	const q = `
		INSERT INTO user_roles (user_id, role_id, organization_id, granted_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, role_id) DO UPDATE
		SET organization_id = EXCLUDED.organization_id, granted_by = EXCLUDED.granted_by`
	_, err := s.db.Q(ctx).Exec(ctx, q, userID, roleID, orgID, grantedBy)
	if err != nil {
		return fmt.Errorf("grant role: %w", err)
	}
	return nil
}

// RevokeRole removes a role assignment.
func (s *Service) RevokeRole(ctx context.Context, userID, roleID uuid.UUID) error {
	const q = `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`
	_, err := s.db.Q(ctx).Exec(ctx, q, userID, roleID)
	if err != nil {
		return fmt.Errorf("revoke role: %w", err)
	}
	return nil
}

// Role is a named permission bundle.
type Role struct {
	ID             uuid.UUID    `json:"id"`
	OrganizationID *uuid.UUID   `json:"organization_id,omitempty"`
	Key            string       `json:"key"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	IsSystem       bool         `json:"is_system"`
	Permissions    []Permission `json:"permissions"`
}

// ListRoles returns roles visible to an organization: its own roles plus the
// platform templates.
func (s *Service) ListRoles(ctx context.Context, orgID uuid.UUID) ([]Role, error) {
	const q = `
		SELECT r.id, r.organization_id, r.key, r.name, r.description, r.is_system,
		       COALESCE(array_agg(rp.permission_key ORDER BY rp.permission_key)
		                FILTER (WHERE rp.permission_key IS NOT NULL), '{}') AS perms
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.organization_id IS NULL OR r.organization_id = $1
		GROUP BY r.id
		ORDER BY r.is_system DESC, r.key`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var out []Role
	for rows.Next() {
		var r Role
		var perms []string
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.Key, &r.Name, &r.Description, &r.IsSystem, &perms); err != nil {
			return nil, err
		}
		for _, p := range perms {
			r.Permissions = append(r.Permissions, Permission(p))
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRolePermissions replaces a role's permission set. It refuses to modify
// platform templates so that tenants cannot weaken or escalate the shared
// baseline for everyone.
func (s *Service) SetRolePermissions(ctx context.Context, orgID, roleID uuid.UUID, perms []Permission) error {
	return s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		var isSystem bool
		var roleOrg *uuid.UUID
		const sel = `SELECT is_system, organization_id FROM roles WHERE id = $1`
		if err := s.db.Q(ctx).QueryRow(ctx, sel, roleID).Scan(&isSystem, &roleOrg); err != nil {
			return fmt.Errorf("load role: %w", err)
		}
		if roleOrg == nil {
			return fmt.Errorf("%w: platform role templates cannot be modified", ErrForbidden)
		}
		if *roleOrg != orgID {
			return fmt.Errorf("%w: role belongs to another organization", ErrForbidden)
		}
		for _, p := range perms {
			if !Known(p) {
				return fmt.Errorf("unknown permission %q", p)
			}
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
			return fmt.Errorf("clear role permissions: %w", err)
		}
		for _, p := range perms {
			const ins = `INSERT INTO role_permissions (role_id, permission_key) VALUES ($1, $2)`
			if _, err := s.db.Q(ctx).Exec(ctx, ins, roleID, string(p)); err != nil {
				return fmt.Errorf("grant %s: %w", p, err)
			}
		}
		return nil
	})
}
