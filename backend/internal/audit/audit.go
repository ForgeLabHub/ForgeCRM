// Package audit records user actions and security events.
//
// Two separate streams are kept, as the mission requires: an action trail
// (audit_log, append-only and enforced by a database trigger) and an
// authentication/security log (security_events). Neither ever stores secrets:
// callers pass already-redacted metadata, and the logging redactor is applied on
// the way out.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/logging"
)

// Entry describes one audited action.
type Entry struct {
	OrganizationID *uuid.UUID
	ActorUserID    *uuid.UUID
	ActorType      string // user | system | api_key | ai_agent
	Action         string
	EntityType     string
	EntityID       string
	Before         any
	After          any
	Metadata       map[string]any
	IP             string
}

// Logger writes audit and security records.
type Logger struct {
	db *database.DB
}

// New builds an audit logger.
func New(db *database.DB) *Logger { return &Logger{db: db} }

// Record appends an action to the audit trail.
//
// It uses the ambient transaction when one is present, which is what makes an
// audited write atomic with the write it describes.
func (l *Logger) Record(ctx context.Context, e Entry) error {
	if e.Action == "" || e.EntityType == "" {
		return fmt.Errorf("audit entry requires action and entity type")
	}
	if e.ActorType == "" {
		e.ActorType = "user"
	}
	before, err := marshalJSONOrNil(e.Before)
	if err != nil {
		return fmt.Errorf("marshal audit before state: %w", err)
	}
	after, err := marshalJSONOrNil(e.After)
	if err != nil {
		return fmt.Errorf("marshal audit after state: %w", err)
	}
	meta, err := json.Marshal(nonNilMap(e.Metadata))
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}

	const q = `
		INSERT INTO audit_log
			(organization_id, actor_user_id, actor_type, action, entity_type, entity_id,
			 before, after, metadata, correlation_id, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, '')::inet)`

	_, err = l.db.Q(ctx).Exec(ctx, q,
		e.OrganizationID, e.ActorUserID, e.ActorType, e.Action, e.EntityType,
		nullIfEmpty(e.EntityID), before, after, meta,
		nullIfEmpty(logging.CorrelationID(ctx)), e.IP)
	if err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

// SecurityEvent describes an authentication or security-relevant occurrence.
type SecurityEvent struct {
	OrganizationID *uuid.UUID
	UserID         *uuid.UUID
	Type           string
	Severity       string // info | warn | error | critical
	Message        string
	Metadata       map[string]any
	IP             string
	UserAgent      string
}

// RecordSecurity appends to the security event stream.
func (l *Logger) RecordSecurity(ctx context.Context, e SecurityEvent) error {
	if e.Type == "" {
		return fmt.Errorf("security event requires a type")
	}
	if e.Severity == "" {
		e.Severity = "info"
	}
	meta, err := json.Marshal(nonNilMap(e.Metadata))
	if err != nil {
		return fmt.Errorf("marshal security metadata: %w", err)
	}
	const q = `
		INSERT INTO security_events
			(organization_id, user_id, event_type, severity, message, metadata, ip, user_agent, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::inet, $8, $9)`
	_, err = l.db.Q(ctx).Exec(ctx, q,
		e.OrganizationID, e.UserID, e.Type, e.Severity, e.Message, meta,
		e.IP, nullIfEmpty(e.UserAgent), nullIfEmpty(logging.CorrelationID(ctx)))
	if err != nil {
		return fmt.Errorf("write security event: %w", err)
	}
	return nil
}

// RecordSecurityAsync is used where a security event must not fail the request,
// for example when logging a failed login for an address that may not exist.
func (l *Logger) RecordSecurityAsync(e SecurityEvent) {
	go func() {
		ctx := context.Background()
		if err := l.RecordSecurity(ctx, e); err != nil {
			// The only safe channel here is the process log.
			_ = err
		}
	}()
}

// Action is a typed constant for the most significant audited actions. Using
// constants keeps the action vocabulary stable for filtering and reporting.
type Action string

const (
	ActionLogin              Action = "auth.login"
	ActionLoginFailed        Action = "auth.login_failed"
	ActionLogout             Action = "auth.logout"
	ActionSessionRevoked     Action = "auth.session_revoked"
	ActionMFAEnabled         Action = "auth.mfa_enabled"
	ActionMFADisabled        Action = "auth.mfa_disabled"
	ActionPasswordReset      Action = "auth.password_reset"
	ActionPasswordChanged    Action = "auth.password_changed"
	ActionUserInvited        Action = "users.invited"
	ActionUserBlocked        Action = "users.blocked"
	ActionUserUnblocked      Action = "users.unblocked"
	ActionUserUpdated        Action = "users.updated"
	ActionRoleCreated        Action = "roles.created"
	ActionRoleUpdated        Action = "roles.updated"
	ActionRoleGranted        Action = "roles.granted"
	ActionRoleRevoked        Action = "roles.revoked"
	ActionOrgCreated         Action = "organizations.created"
	ActionOrgUpdated         Action = "organizations.updated"
	ActionImportCreated      Action = "imports.created"
	ActionImportExecuted     Action = "imports.executed"
	ActionImportCancelled    Action = "imports.cancelled"
	ActionExportExecuted     Action = "exports.executed"
	ActionMigrationCreated   Action = "migrations.created"
	ActionIntegrationCreated Action = "integrations.created"
	ActionIntegrationUpdated Action = "integrations.updated"
	ActionIntegrationRevoked Action = "integrations.revoked"
	ActionWebhookCreated     Action = "webhooks.created"
	ActionWebhookUpdated     Action = "webhooks.updated"
	ActionWebhookReplayed    Action = "webhooks.replayed"
	// ActionWebhookDeadLettered marks the moment a delivery exhausted its
	// attempts. It is written once per transition, not once per attempt, so an
	// operator can alert on it without the alert repeating forever.
	ActionWebhookDeadLettered  Action = "webhooks.delivery_dead_lettered"
	ActionDealDiscountApproved Action = "deals.discount_approved"
	ActionJobCancelled         Action = "jobs.cancelled"
	ActionJobRetried           Action = "jobs.retried"
	ActionSettingsUpdated      Action = "system.settings_updated"
	ActionAIToolInvoked        Action = "ai.tool_invoked"
	ActionEmailConfigured      Action = "notifications.email_configured"
	ActionTelegramLinked       Action = "notifications.telegram_linked"
	ActionTelegramUnlinked     Action = "notifications.telegram_unlinked"

	// Agent lifecycle. The agent actions are the write-before-execute record ADR
	// 0005 requires: `proposed` exists before any effect, `approved` records the
	// signed human decision, `executed` records the effect, and `rejected` /
	// `expired` close a proposal without an effect. An action can therefore never
	// exist without a record of why it was allowed.
	ActionAgentRunStarted     Action = "agent.run_started"
	ActionAgentRunCompleted   Action = "agent.run_completed"
	ActionAgentRunFailed      Action = "agent.run_failed"
	ActionAgentActionProposed Action = "agent.action_proposed"
	ActionAgentActionApproved Action = "agent.action_approved"
	ActionAgentActionRejected Action = "agent.action_rejected"
	ActionAgentActionExpired  Action = "agent.action_expired"
	ActionAgentActionExecuted Action = "agent.action_executed"
	ActionAgentActionFailed   Action = "agent.action_failed"
)

func marshalJSONOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return nil, nil
	}
	return b, nil
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UUIDPtr returns a pointer to a UUID, for building audit entries concisely.
func UUIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// StrPtr returns a pointer to s.
func StrPtr(s string) *string { return &s }
