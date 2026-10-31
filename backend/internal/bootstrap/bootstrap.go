// Package bootstrap creates the first platform administrator.
//
// The first administrator is the one account that cannot be created through the
// API: before it exists there is nobody who may grant anything. It is therefore
// provisioned from deployment configuration, under three rules that make it safe
// to leave in a deploy script:
//
//   - it only ever creates. If any platform owner already exists the command
//     reports that and exits successfully, so a repeated deploy cannot overwrite
//     or downgrade the owner, and an ENV value left behind after the first run
//     cannot be used to reset the account.
//   - the password comes from the environment and is never defaulted, echoed, or
//     logged. A missing password is a clear error, not a silent weak account.
//   - creation is serialised by a PostgreSQL advisory lock and re-checks the
//     owner inside that lock, so two replicas starting at once cannot both
//     create one. The partial unique index users_single_platform_owner is the
//     database-level backstop if the lock is ever bypassed.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// advisoryLockKey namespaces the bootstrap lock. It is a fixed constant so every
// process contends for the same lock regardless of the database it connects to
// (advisory locks are already scoped to the database).
const advisoryLockKey = 0x464f52474543 // "FORGEC" in hex

// Options are the inputs to the bootstrap. They are resolved from the
// environment (and, for one-off runs, overridden by flags) before this is called.
type Options struct {
	Email       string
	Password    string
	DisplayName string
}

// ErrIncomplete reports a partial bootstrap configuration: one of the two
// required values is set and the other is not. It is distinct from "nothing was
// configured", which is not an error.
var ErrIncomplete = errors.New("incomplete bootstrap configuration")

// Result reports what the run did.
type Result struct {
	// Created is true when a new owner was created in this run.
	Created bool
	// AlreadyExists is true when a platform owner already existed.
	AlreadyExists bool
	// Email is the owner address, for a log line. Never the password.
	Email string
}

// Admin creates the first platform administrator when none exists.
//
// It returns ErrIncomplete when exactly one of email/password is set, so a typo
// in the deployment variables is a clear failure rather than a half-configured
// account. When neither is set it is a no-op, so the API can call it on every
// start without requiring bootstrap variables in production.
func Admin(ctx context.Context, db *database.DB, log *slog.Logger, opts Options) (Result, error) {
	email := strings.ToLower(strings.TrimSpace(opts.Email))
	password := opts.Password

	if email == "" && password == "" {
		return Result{}, nil
	}
	if email == "" || password == "" {
		// Name the missing side, never the value of the side that is present.
		missing := "FORGECRM_BOOTSTRAP_ADMIN_PASSWORD"
		if password != "" {
			missing = "FORGECRM_BOOTSTRAP_ADMIN_EMAIL"
		}
		return Result{}, fmt.Errorf("%w: %s must also be set", ErrIncomplete, missing)
	}
	if !strings.Contains(email, "@") {
		return Result{}, fmt.Errorf("the bootstrap administrator email %q is not an address", email)
	}
	if err := authn.ValidatePassword(password); err != nil {
		// ValidatePassword never echoes the password, only why it was rejected.
		return Result{}, fmt.Errorf("the bootstrap administrator password is not acceptable: %w", err)
	}

	hash, err := secrets.HashPassword(password)
	if err != nil {
		return Result{}, fmt.Errorf("hash the bootstrap password: %w", err)
	}

	displayName := strings.TrimSpace(opts.DisplayName)
	if displayName == "" {
		displayName = "Platform owner"
	}

	auditLog := audit.New(db)
	rbacSvc := rbac.NewService(db)

	var result Result
	err = db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		// Serialise concurrent bootstraps. The lock is released when the
		// transaction ends, so a crashed run does not hold it.
		if _, err := db.Q(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(advisoryLockKey)); err != nil {
			return fmt.Errorf("acquire the bootstrap lock: %w", err)
		}

		// Re-check inside the lock: the check outside would race two starters.
		var existing int64
		if err := db.Q(ctx).QueryRow(ctx,
			`SELECT count(*) FROM users WHERE is_platform_owner`).Scan(&existing); err != nil {
			return fmt.Errorf("check for an existing platform owner: %w", err)
		}
		if existing > 0 {
			result.AlreadyExists = true
			result.Email = email
			return nil
		}

		var userID uuid.UUID
		const insUser = `
INSERT INTO users (email, password_hash, display_name, status, is_platform_owner)
VALUES ($1, $2, $3, 'active', true)
RETURNING id`
		if err := db.Q(ctx).QueryRow(ctx, insUser, email, hash, displayName).Scan(&userID); err != nil {
			return fmt.Errorf("create the bootstrap user: %w", err)
		}

		// Give the owner its own organization so the platform is usable at once.
		var orgID uuid.UUID
		const insOrg = `
INSERT INTO organizations (name, slug) VALUES ('Default organization', 'default')
ON CONFLICT (slug) DO UPDATE SET name = organizations.name
RETURNING id`
		if err := db.Q(ctx).QueryRow(ctx, insOrg).Scan(&orgID); err != nil {
			return fmt.Errorf("create the default organization: %w", err)
		}
		const insMember = `
INSERT INTO memberships (user_id, organization_id, is_org_owner)
VALUES ($1, $2, true) ON CONFLICT (user_id, organization_id) DO UPDATE SET is_org_owner = true`
		if _, err := db.Q(ctx).Exec(ctx, insMember, userID, orgID); err != nil {
			return fmt.Errorf("add the owner to the organization: %w", err)
		}
		if _, err := rbacSvc.ClonePlatformRoles(ctx, orgID); err != nil {
			return fmt.Errorf("seed tenant roles: %w", err)
		}
		const grant = `
INSERT INTO user_roles (user_id, role_id, organization_id)
SELECT $1, r.id, $2 FROM roles r
WHERE r.organization_id = $2 AND r.key = 'tenant_admin'
ON CONFLICT (user_id, role_id) DO NOTHING`
		if _, err := db.Q(ctx).Exec(ctx, grant, userID, orgID); err != nil {
			return fmt.Errorf("grant the tenant administrator role: %w", err)
		}
		if err := auditLog.Record(ctx, audit.Entry{
			OrganizationID: &orgID, ActorUserID: &userID, ActorType: "system",
			Action: "platform.bootstrap_owner_created", EntityType: "user", EntityID: userID.String(),
		}); err != nil {
			return fmt.Errorf("record the bootstrap in the audit log: %w", err)
		}

		result.Created = true
		result.Email = email
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	if result.AlreadyExists {
		log.Info("a platform owner already exists; bootstrap is a no-op")
	} else {
		// The address is safe to log; the password never is.
		log.Info("platform owner created", "email", result.Email)
	}
	return result, nil
}
