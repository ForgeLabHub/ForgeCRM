// Package audit records user actions and security events.
//
// Two separate streams are kept, as the mission requires: an action trail
// (audit_log, append-only and enforced by a database trigger) and an
// authentication/security log (security_events). Neither ever stores secrets:
// callers pass already-redacted metadata, and the logging redactor is applied on
// the way out.
package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"

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

	// SkipChain writes the entry without a hash-chain link. It exists only for
	// the rare caller that must write to the trail outside a transaction it can
	// serialize (for example a best-effort security note taken on a detached
	// context). An unchained entry is still append-only; it simply does not
	// participate in the chain and the verifier counts it separately.
	SkipChain bool
}

// auditChainLockClass namespaces the advisory lock the chain takes, so it cannot
// collide with a lock taken elsewhere in the process.
const auditChainLockClass = 918273645

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
//
// The entry is linked into its organization's hash chain. When the caller is
// already inside a transaction the link is taken on that transaction's
// connection, so the row and its chain position commit together. When there is
// no ambient transaction the append runs in its own short transaction that also
// takes the chain lock, so a bare Record is still chained rather than silently
// unchained.
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
			 before, after, metadata, correlation_id, ip, prev_hash, entry_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, '')::inet, $12, $13)`

	insert := func(ctx context.Context, prev, hash []byte) error {
		_, err := l.db.Q(ctx).Exec(ctx, q,
			e.OrganizationID, e.ActorUserID, e.ActorType, e.Action, e.EntityType,
			nullIfEmpty(e.EntityID), before, after, meta,
			nullIfEmpty(logging.CorrelationID(ctx)), e.IP, prev, hash)
		if err != nil {
			return fmt.Errorf("write audit entry: %w", err)
		}
		return nil
	}

	// Already inside a transaction: the caller owns the connection and the
	// commit, so chain on it directly.
	if _, ok := database.TxFromContext(ctx); ok {
		if e.SkipChain {
			return insert(ctx, nil, nil)
		}
		prev, hash, err := l.chainHash(ctx, e, before, after, meta)
		if err != nil {
			return err
		}
		return insert(ctx, prev, hash)
	}

	// No ambient transaction. Take one so the chain lock, the tail read and the
	// insert are one atomic unit.
	return l.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if e.SkipChain {
			return insert(ctx, nil, nil)
		}
		prev, hash, err := l.chainHash(ctx, e, before, after, meta)
		if err != nil {
			return err
		}
		return insert(ctx, prev, hash)
	})
}

// chainHash computes the link for a new entry.
//
// The chain is per organization, so the lock and the tail lookup are both keyed
// by the organization (the nil UUID for platform-level entries). The lock is
// transaction-scoped: it is released when the surrounding transaction commits or
// rolls back, so a caller that already holds it cannot deadlock against itself.
func (l *Logger) chainHash(ctx context.Context, e Entry, before, after, meta []byte) (prev, hash []byte, err error) {
	lockKey := int32(0)
	if e.OrganizationID != nil {
		// A stable 32-bit key derived from the organization UUID. Collisions
		// between two organizations only serialize them against each other; they
		// never corrupt a chain, because the tail read is still scoped by
		// organization_id.
		lockKey = orgLockKey(*e.OrganizationID)
	}
	if _, err = l.db.Q(ctx).Exec(ctx,
		"SELECT pg_advisory_xact_lock($1, $2)", auditChainLockClass, lockKey); err != nil {
		return nil, nil, fmt.Errorf("lock audit chain: %w", err)
	}

	const tail = `
		SELECT entry_hash FROM audit_log
		WHERE organization_id IS NOT DISTINCT FROM $1 AND entry_hash IS NOT NULL
		ORDER BY id DESC LIMIT 1`
	var tailHash []byte
	row := l.db.Q(ctx).QueryRow(ctx, tail, e.OrganizationID)
	switch err := row.Scan(&tailHash); {
	case err == nil:
		prev = tailHash
	case database.IsNoRows(err):
		prev = nil
	default:
		return nil, nil, fmt.Errorf("read audit chain tail: %w", err)
	}

	hash = hashEntry(prev, e, before, after, meta)
	return prev, hash, nil
}

// Verify walks an organization's chain and reports the first entry whose link
// does not match. It returns nil when the chain is intact.
//
// Rows written before the chain existed (entry_hash IS NULL) are skipped, since
// they have no link to check; the count of such rows is returned so an operator
// can see how much of the trail predates the chain. The walk is ordered by id,
// which is the order the chain was built in.
func (l *Logger) Verify(ctx context.Context, orgID *uuid.UUID) (*VerificationResult, error) {
	const q = `
		SELECT id, organization_id, actor_user_id, actor_type, action, entity_type, entity_id,
		       host(ip), before, after, metadata, prev_hash, entry_hash
		FROM audit_log
		WHERE organization_id IS NOT DISTINCT FROM $1
		ORDER BY id ASC`
	rows, err := l.db.Q(ctx).Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("read audit chain: %w", err)
	}
	defer rows.Close()

	res := &VerificationResult{Intact: true}
	var prev []byte
	for rows.Next() {
		var (
			id       int64
			rowOrgID *uuid.UUID
			actorID  *uuid.UUID
			actorTyp string
			action   string
			entType  string
			entID    *string
			ip       *string
			before   []byte
			after    []byte
			meta     []byte
			prevHash []byte
			entry    []byte
		)
		if err := rows.Scan(&id, &rowOrgID, &actorID, &actorTyp, &action, &entType, &entID,
			&ip, &before, &after, &meta, &prevHash, &entry); err != nil {
			return nil, fmt.Errorf("scan audit chain row: %w", err)
		}
		res.Checked++
		if entry == nil {
			// Pre-chain row: nothing to verify, and it does not advance the link.
			res.Unchained++
			continue
		}
		if !bytes.Equal(prevHash, prev) {
			res.Intact = false
			res.FirstBreakID = id
			res.Reason = "prev_hash does not match the previous entry's hash"
			return res, nil
		}
		want := hashEntry(prev, Entry{
			OrganizationID: rowOrgID,
			ActorUserID:    actorID,
			ActorType:      actorTyp,
			Action:         action,
			EntityType:     entType,
			EntityID:       derefString(entID),
			IP:             derefString(ip),
		}, before, after, meta)
		if !bytes.Equal(want, entry) {
			res.Intact = false
			res.FirstBreakID = id
			res.Reason = "entry_hash does not match the entry contents"
			return res, nil
		}
		prev = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit chain: %w", err)
	}
	if res.Checked == 0 {
		res.Reason = "no audit entries for this scope"
	}
	return res, nil
}

// VerificationResult reports the outcome of walking one organization's chain.
type VerificationResult struct {
	// Intact is true when every chained entry matched.
	Intact bool `json:"intact"`
	// Checked is the number of chained entries examined.
	Checked int64 `json:"checked"`
	// Unchained is the number of rows that predate the chain.
	Unchained int64 `json:"unchained"`
	// FirstBreakID is the id of the first entry that failed, when Intact is false.
	FirstBreakID int64 `json:"first_break_id,omitempty"`
	// Reason explains the outcome in one line.
	Reason string `json:"reason,omitempty"`
}

// hashEntry computes the SHA-256 link for an entry.
//
// The hashed bytes are length-prefixed per field so that a value moved from one
// field to another cannot produce the same hash as the original. The JSON fields
// are normalised by canonicalJSON first, because PostgreSQL's jsonb does not
// return the bytes it was given and the hash has to be stable across the write
// and the read. The actor user id and source IP are covered too, so editing
// either is detected.
func hashEntry(prev []byte, e Entry, before, after, meta []byte) []byte {
	h := sha256.New()
	writeChunk(h, prev)
	writeChunk(h, actorBytes(e.ActorUserID))
	writeChunk(h, []byte(e.ActorType))
	writeChunk(h, []byte(e.Action))
	writeChunk(h, []byte(e.EntityType))
	writeChunk(h, []byte(e.EntityID))
	writeChunk(h, []byte(e.IP))
	writeChunk(h, canonicalJSON(before))
	writeChunk(h, canonicalJSON(after))
	writeChunk(h, canonicalJSON(meta))
	return h.Sum(nil)
}

// canonicalJSON re-encodes JSON in a form that is stable across the write and the
// read.
//
// This is required because before/after/metadata are jsonb columns, and
// PostgreSQL does not return the bytes it was given: it re-serialises with its own
// key order and spacing. Hashing the raw stored bytes would therefore never match
// the bytes hashed at write time. Both sides are passed through this function, so
// the comparison is on the value, not on a storage detail. Numbers are decoded as
// json.Number so a literal like 1 stays 1 rather than becoming a float.
//
// Non-JSON input is returned unchanged: hashEntry is also called from unit tests
// with arbitrary bytes, and there is nothing to normalise there.
func canonicalJSON(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return b
	}
	out, err := json.Marshal(v)
	if err != nil {
		return b
	}
	return out
}

// actorBytes renders an optional actor id as its raw UUID bytes, or nothing when
// the actor is not a user (a system or API-key action).
func actorBytes(id *uuid.UUID) []byte {
	if id == nil {
		return nil
	}
	return id[:]
}

// writeChunk writes a big-endian length prefix followed by the bytes, so two
// concatenations that differ only in where one field ends cannot collide.
func writeChunk(h hash.Hash, b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	h.Write(n[:])
	h.Write(b)
}

// orgLockKey derives a stable 32-bit advisory-lock key from an organization id.
func orgLockKey(id uuid.UUID) int32 {
	sum := sha256.Sum256(id[:])
	return int32(binary.BigEndian.Uint32(sum[:4]))
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

	// ActionRetentionSweep records one retention run that removed rows. It is
	// written by the system actor and names the count per policy, so a deletion is
	// attributable after the fact without recording what was deleted.
	ActionRetentionSweep Action = "system.retention_sweep"

	// Document attachments. Uploading and archiving are recorded because a file is
	// data leaving or entering the deployment, and an operator needs to answer "who
	// put this here" and "when was it removed".
	ActionDocumentUploaded Action = "documents.uploaded"
	ActionDocumentArchived Action = "documents.archived"

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

	// Workflow automation lifecycle. created, updated, enabled and archived are
	// authored actions; run.* are written by the engine when a run reaches a
	// terminal state, so the trail shows what automation did as well as what people
	// did.
	ActionWorkflowCreated  Action = "workflows.created"
	ActionWorkflowUpdated  Action = "workflows.updated"
	ActionWorkflowEnabled  Action = "workflows.enabled"
	ActionWorkflowArchived Action = "workflows.archived"
	ActionWorkflowRunOK    Action = "workflow.run.succeeded"
	ActionWorkflowRunFail  Action = "workflow.run.failed"

	// Contextual AI. One entry per assistance request, recording who asked about
	// which record and what it cost. The analysis itself is not recorded: the
	// trail shows that a model was consulted, not a second copy of its answer.
	ActionAIInsightGenerated Action = "ai.insight_generated"

	// Saved list views. A view is a stored filter, so the trail records who named
	// one, what it was called and whether it was shared — enough to answer "who
	// exposed this filter to the organization" without storing the filter twice.
	ActionViewCreated Action = "views.created"
	ActionViewUpdated Action = "views.updated"
	ActionViewDeleted Action = "views.deleted"

	// MCP client credentials. Minting a credential delegates a slice of the owner's
	// authority to an external client, so the trail records that it happened, to whom
	// and with which scope — but never the token itself, which is stored only as a
	// hash. A use is recorded as an "api_key" actor so a credential's activity is
	// distinguishable from the owner's own browser activity.
	ActionMCPCredentialCreated Action = "mcp.credentials.created"
	ActionMCPCredentialRevoked Action = "mcp.credentials.revoked"
	ActionMCPCredentialRotated Action = "mcp.credentials.rotated"

	// Consent. Recording a decision changes whether a person may be contacted, so the
	// trail records it. The entry names the purpose, channel and state but not the
	// evidence text, which may quote the person and belongs in the record, not in a log
	// that is read more widely.
	ActionConsentRecorded  Action = "consent.recorded"
	ActionConsentWithdrawn Action = "consent.withdrawn"
	// ActionConsentBlockedSend records a send that a channel refused because consent was
	// absent, denied, withdrawn or expired. It is a system action, not a user one, and it
	// is what makes "we blocked N sends this week" answerable.
	ActionConsentBlockedSend Action = "consent.send_blocked"

	// Tenant configuration. A setting is a default the whole organization inherits, so a
	// change to one is recorded with the value it replaced. The revision chain in
	// tenant_setting_revisions carries the same history; the audit entry is what makes the
	// change visible in the trail an auditor already reads, and it is written in the same
	// transaction as the value.
	ActionTenantSettingUpdated Action = "tenant.settings.updated"
	ActionTenantSettingReset   Action = "tenant.settings.reset"
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
