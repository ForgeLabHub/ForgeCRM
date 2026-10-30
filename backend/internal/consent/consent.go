// Package consent records what a person agreed to be contacted about, and answers
// the one question every outbound channel has to ask first: may we contact this
// person, for this purpose, on this channel?
//
// The model is a decision record, not a flag. A decision has a state — granted,
// denied or withdrawn — and a missing record means "never asked", which is a third
// thing again. Collapsing those into a boolean is what makes "we emailed them
// because nobody said no" possible, and this package exists so that cannot happen
// silently: an absent record is never read as permission.
//
// History is preserved. Changing a decision writes a new revision and points the
// previous one at it, so what a person was told and when they agreed is not
// rewritten by a later withdrawal. The audit log keeps the same trail, but the
// record itself is what a policy check reads, and a policy check must not depend on
// reading the audit log.
//
// The service never touches CRM data. It reads and writes its own table, and the
// callers that do contact people ask it before they send.
package consent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// Purpose is what the person was asked about. The set is closed: a free-text
// purpose would make "may we email them about marketing" unanswerable by a query,
// which is the whole reason the record exists.
type Purpose string

// The supported purposes.
const (
	PurposeMarketing      Purpose = "marketing"
	PurposeTransactional  Purpose = "transactional"
	PurposeProductUpdates Purpose = "product_updates"
	PurposeResearch       Purpose = "research"
)

// Channel is how the person would be reached. It is part of the decision's key
// because agreeing to be emailed is not agreeing to be messaged on Telegram.
type Channel string

// The supported channels. Each names a real outbound path in the platform.
const (
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	ChannelWebhook  Channel = "webhook"
)

// State is the decision itself.
type State string

// The three states. `withdrawn` is distinct from `denied`: a denial is a refusal
// that never became permission, a withdrawal is permission that was taken back,
// and an operator reading the history cares about the difference.
const (
	StateGranted   State = "granted"
	StateDenied    State = "denied"
	StateWithdrawn State = "withdrawn"
)

var supportedPurposes = map[Purpose]struct{}{
	PurposeMarketing: {}, PurposeTransactional: {}, PurposeProductUpdates: {}, PurposeResearch: {},
}

var supportedChannels = map[Channel]struct{}{
	ChannelEmail: {}, ChannelTelegram: {}, ChannelWebhook: {},
}

var supportedStates = map[State]struct{}{
	StateGranted: {}, StateDenied: {}, StateWithdrawn: {},
}

// Valid reports whether p is a supported purpose.
func (p Purpose) Valid() bool { _, ok := supportedPurposes[p]; return ok }

// RequiresGrant reports whether this purpose may only be contacted on the strength
// of a recorded grant. Marketing, product updates and research may not be sent on
// the strength of silence, so an absent record blocks them.
//
// Transactional mail is the legitimate-interest category — a password reset, a
// receipt, a security notice — and is deliberately not gated: blocking it because
// nobody recorded a marketing consent would break account recovery and hide
// security notices. An explicit denial or withdrawal is still honoured, because a
// decision the person made is not overridden by a category default.
func (p Purpose) RequiresGrant() bool {
	switch p {
	case PurposeMarketing, PurposeProductUpdates, PurposeResearch:
		return true
	default:
		return false
	}
}

// Valid reports whether c is a supported channel.
func (c Channel) Valid() bool { _, ok := supportedChannels[c]; return ok }

// Valid reports whether s is a supported state.
func (s State) Valid() bool { _, ok := supportedStates[s]; return ok }

// ErrNotFound is returned when a record does not exist in the caller's scope.
var ErrNotFound = errors.New("consent record not found")

// ValidationError describes why a decision was refused, carrying the offending
// field so the API can render a 422 with per-field detail.
type ValidationError struct {
	Fields map[string]string
}

// Error implements error.
func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for k, v := range e.Fields {
		parts = append(parts, k+": "+v)
	}
	sort.Strings(parts)
	return "invalid consent record: " + strings.Join(parts, "; ")
}

// Subject is the person a decision is about. It is either a contact already in the
// CRM, or a bare email address for someone who is not — an unsubscribe link
// arrives from a mailbox, not from an id.
type Subject struct {
	ContactID *uuid.UUID
	Email     string
}

// SubjectForContact builds a subject from a CRM contact id.
func SubjectForContact(id uuid.UUID) Subject { return Subject{ContactID: &id} }

// SubjectForEmail builds a subject from an email address. The address is
// normalised so that a decision recorded for "Ada@Example.com" is found again for
// "ada@example.com".
func SubjectForEmail(email string) Subject { return Subject{Email: normalizeEmail(email)} }

// IsZero reports whether the subject names nobody.
func (s Subject) IsZero() bool {
	return s.ContactID == nil && strings.TrimSpace(s.Email) == ""
}

// normalizeEmail lower-cases and trims an address so decisions match regardless of
// how the address was typed.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Record is one decision, at one revision.
type Record struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	SubjectEmail   *string    `json:"subject_email,omitempty"`
	Purpose        string     `json:"purpose"`
	Channel        string     `json:"channel"`
	State          string     `json:"state"`
	Basis          *string    `json:"basis,omitempty"`
	Source         *string    `json:"source,omitempty"`
	Evidence       *string    `json:"evidence,omitempty"`
	Revision       int        `json:"revision"`
	SupersededBy   *uuid.UUID `json:"superseded_by,omitempty"`
	RecordedBy     *uuid.UUID `json:"recorded_by,omitempty"`
	DecidedAt      time.Time  `json:"decided_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// Current is derived at read time: true when this revision is still in force.
	// It is not stored, because "current" is a fact about the whole set, not one row.
	Current bool `json:"current"`
}

// Input is the body that records a decision.
type Input struct {
	ContactID    *uuid.UUID `json:"contact_id"`
	SubjectEmail string     `json:"subject_email"`
	Purpose      string     `json:"purpose"`
	Channel      string     `json:"channel"`
	State        string     `json:"state"`
	Basis        string     `json:"basis"`
	Source       string     `json:"source"`
	Evidence     string     `json:"evidence"`
	ExpiresAt    *time.Time `json:"expires_at"`
	// DecidedAt lets an operator record an agreement discovered during migration,
	// rather than stamping it with the moment of entry.
	DecidedAt *time.Time `json:"decided_at"`
}

// subject extracts the subject an input names. A contact id wins over an address,
// but only after validate has refused the case where both are set: silently
// preferring one over the other is how a decision gets recorded against the wrong
// person.
func (in Input) subject() Subject {
	if in.ContactID != nil {
		return Subject{ContactID: in.ContactID}
	}
	return SubjectForEmail(in.SubjectEmail)
}

// Decision is the answer a channel acts on, together with the reason for it.
//
// The reason is not decoration. An operator who sees a send blocked must be able to
// read why without querying the database, and "no consent on record" and "consent
// withdrawn" call for different follow-ups.
type Decision struct {
	Allowed   bool       `json:"allowed"`
	Reason    string     `json:"reason"`
	State     string     `json:"state,omitempty"`
	RecordID  *uuid.UUID `json:"record_id,omitempty"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Service provides consent operations.
type Service struct {
	db    *database.DB
	audit *audit.Logger
	now   func() time.Time
}

// NewService builds the consent service.
func NewService(db *database.DB, auditLog *audit.Logger) *Service {
	return &Service{db: db, audit: auditLog, now: time.Now}
}

const recordColumns = `id, organization_id, contact_id, subject_email, purpose, channel, state,
        basis, source, evidence, revision, superseded_by, recorded_by, decided_at, expires_at,
        created_at, updated_at`

// validate checks an input and returns a per-field explanation when it is
// unusable. It is the same check the API performs, kept here so any caller — the
// API, an import, a workflow — cannot bypass it.
func validate(in Input) error {
	fields := map[string]string{}
	switch {
	case in.ContactID != nil && strings.TrimSpace(in.SubjectEmail) != "":
		fields["subject"] = "a consent record is about a contact or an email address, not both"
	case in.ContactID == nil && strings.TrimSpace(in.SubjectEmail) == "":
		fields["subject"] = "a consent record must name a contact or an email address"
	}
	if !Purpose(in.Purpose).Valid() {
		fields["purpose"] = "must be one of marketing, transactional, product_updates, research"
	}
	if !Channel(in.Channel).Valid() {
		fields["channel"] = "must be one of email, telegram, webhook"
	}
	if !State(in.State).Valid() {
		fields["state"] = "must be one of granted, denied, withdrawn"
	}
	if in.ExpiresAt != nil && in.DecidedAt != nil && in.ExpiresAt.Before(*in.DecidedAt) {
		fields["expires_at"] = "must not be before the decision date"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// Record stores a new decision, superseding whatever was in force for the same
// subject, purpose and channel.
//
// The previous current row is not overwritten: it is marked superseded and points
// at its successor, so the history of the decision is preserved. The write is
// serialised per key with a transaction-scoped advisory lock, so two concurrent
// writers cannot both believe they are writing revision 1.
func (s *Service) Record(ctx context.Context, p *rbac.Principal, in Input) (*Record, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	subject := in.subject()
	newID := uuid.New()
	decidedAt := s.now()
	if in.DecidedAt != nil {
		decidedAt = *in.DecidedAt
	}

	var rec Record
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockKey(ctx, p.OrganizationID, subject, in.Purpose, in.Channel); err != nil {
			return err
		}

		prev, err := s.currentRow(ctx, p.OrganizationID, subject, Purpose(in.Purpose), Channel(in.Channel))
		if err != nil {
			return err
		}
		revision := 1
		if prev != nil {
			revision = prev.Revision + 1
		}

		// The new row is inserted before the previous one is superseded, because
		// superseded_by references it: pointing at a row that does not exist yet would
		// fail the foreign key.
		const insert = `
                        INSERT INTO consent_records
                                (id, organization_id, contact_id, subject_email, purpose, channel, state,
                                 basis, source, evidence, revision, recorded_by, decided_at, expires_at)
                        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
                        RETURNING ` + recordColumns
		scanned, err := scanRecord(s.db.Q(ctx).QueryRow(ctx, insert,
			newID, p.OrganizationID, in.ContactID, nullIfEmpty(normalizeEmail(in.SubjectEmail)),
			in.Purpose, in.Channel, in.State,
			nullIfEmpty(in.Basis), nullIfEmpty(in.Source), nullIfEmpty(in.Evidence),
			revision, p.UserID, decidedAt, in.ExpiresAt))
		if err != nil {
			return err
		}
		rec = *scanned
		rec.Current = true

		if prev != nil {
			const supersede = `UPDATE consent_records SET superseded_by = $2 WHERE id = $1`
			if _, err := s.db.Q(ctx).Exec(ctx, supersede, prev.ID, newID); err != nil {
				return fmt.Errorf("supersede the previous consent record: %w", err)
			}
		}

		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionConsentRecorded),
			EntityType:     "consent_record",
			EntityID:       rec.ID.String(),
			After: map[string]any{
				"purpose": in.Purpose, "channel": in.Channel, "state": in.State,
				"revision": revision,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// Withdraw records a withdrawal for a subject, purpose and channel. It is the same
// write as Record with state withdrawn, named for the act so a caller does not have
// to remember which state means "take it back".
func (s *Service) Withdraw(ctx context.Context, p *rbac.Principal, subject Subject, purpose Purpose, channel Channel, reason string) (*Record, error) {
	in := Input{
		Purpose: string(purpose),
		Channel: string(channel),
		State:   string(StateWithdrawn),
		Source:  reason,
	}
	if subject.ContactID != nil {
		in.ContactID = subject.ContactID
	} else {
		in.SubjectEmail = subject.Email
	}
	return s.Record(ctx, p, in)
}

// Decide answers whether a subject may be contacted for a purpose on a channel, and
// why. It is the function every outbound channel calls before sending.
//
// It takes the organization id rather than a principal because enforcement must
// work wherever a send originates — a workflow runs as its author, but the policy
// is about the person being contacted, not about who pressed go.
func (s *Service) Decide(ctx context.Context, orgID uuid.UUID, subject Subject, purpose Purpose, channel Channel) (Decision, error) {
	if subject.IsZero() {
		return Decision{}, &ValidationError{Fields: map[string]string{"subject": "is required"}}
	}
	if !purpose.Valid() {
		return Decision{}, &ValidationError{Fields: map[string]string{"purpose": "is not supported"}}
	}
	if !channel.Valid() {
		return Decision{}, &ValidationError{Fields: map[string]string{"channel": "is not supported"}}
	}

	rec, err := s.currentRow(ctx, orgID, subject, purpose, channel)
	if err != nil {
		return Decision{}, err
	}
	if rec == nil {
		// No record. Whether that blocks the send depends on the purpose: a
		// marketing message needs a recorded grant, a transactional one does not.
		// The reason says which rule was applied, so an operator reading a
		// "blocked" line knows it was the absence of a grant and not a refusal.
		if purpose.RequiresGrant() {
			return Decision{
				Allowed: false,
				Reason: fmt.Sprintf("no consent on record for %s via %s; %s may not be sent on the strength of silence",
					purpose, channel, purpose),
			}, nil
		}
		return Decision{
			Allowed: true,
			Reason: fmt.Sprintf("no consent on record for %s via %s; %s does not require a recorded grant, so it is allowed",
				purpose, channel, purpose),
		}, nil
	}

	d := Decision{
		State:     rec.State,
		RecordID:  &rec.ID,
		DecidedAt: &rec.DecidedAt,
		ExpiresAt: rec.ExpiresAt,
	}
	switch State(rec.State) {
	case StateGranted:
		if rec.ExpiresAt != nil && !rec.ExpiresAt.After(s.now()) {
			d.Allowed = false
			d.Reason = fmt.Sprintf("consent for %s via %s expired on %s",
				purpose, channel, rec.ExpiresAt.UTC().Format(time.RFC3339))
			return d, nil
		}
		d.Allowed = true
		d.Reason = fmt.Sprintf("consent granted for %s via %s on %s",
			purpose, channel, rec.DecidedAt.UTC().Format(time.RFC3339))
	case StateDenied:
		d.Reason = fmt.Sprintf("the person refused %s via %s on %s",
			purpose, channel, rec.DecidedAt.UTC().Format(time.RFC3339))
	case StateWithdrawn:
		d.Reason = fmt.Sprintf("consent for %s via %s was withdrawn on %s",
			purpose, channel, rec.DecidedAt.UTC().Format(time.RFC3339))
	default:
		d.Reason = fmt.Sprintf("consent for %s via %s is in an unrecognised state %q", purpose, channel, rec.State)
	}
	return d, nil
}

// Allowed is the boolean a channel actually needs. It is Decide with the reason
// dropped, for callers that only branch on the answer.
func (s *Service) Allowed(ctx context.Context, orgID uuid.UUID, subject Subject, purpose Purpose, channel Channel) (bool, error) {
	d, err := s.Decide(ctx, orgID, subject, purpose, channel)
	if err != nil {
		return false, err
	}
	return d.Allowed, nil
}

// Enforce is the check a channel performs immediately before it sends. It is Decide
// plus the one side effect an operator needs afterwards: when the send is blocked,
// it writes a block entry to the audit trail, so "how many sends did consent stop,
// and why" is answerable rather than being an invisible refusal.
//
// It returns the decision even when it blocks, so the caller can surface the reason.
// The entry is written in the same call that makes the decision, so a blocked send
// cannot be forgotten by a caller that only checked the boolean.
func (s *Service) Enforce(ctx context.Context, orgID uuid.UUID, actor *uuid.UUID, subject Subject, purpose Purpose, channel Channel) (Decision, error) {
	d, err := s.Decide(ctx, orgID, subject, purpose, channel)
	if err != nil {
		return Decision{}, err
	}
	if d.Allowed {
		return d, nil
	}
	actorType := "system"
	if actor != nil {
		actorType = "user"
	}
	// The audit entry names the subject and the reason but not the message body,
	// which may quote the person and belongs in the message, not in a log read more
	// widely. A failure to write the trail must not be swallowed: a block nobody can
	// see is the state this feature exists to end, so the error is returned.
	if err := s.audit.Record(ctx, audit.Entry{
		OrganizationID: &orgID,
		ActorUserID:    actor,
		ActorType:      actorType,
		Action:         string(audit.ActionConsentBlockedSend),
		EntityType:     "consent_send",
		EntityID:       subjectKey(subject),
		Metadata: map[string]any{
			"purpose": string(purpose),
			"channel": string(channel),
			"state":   d.State,
			"reason":  d.Reason,
		},
	}); err != nil {
		return d, fmt.Errorf("record the blocked send: %w", err)
	}
	return d, nil
}

// List returns the full decision history for a subject, newest first.
func (s *Service) List(ctx context.Context, p *rbac.Principal, subject Subject) ([]Record, error) {
	if subject.IsZero() {
		return nil, &ValidationError{Fields: map[string]string{"subject": "is required"}}
	}
	const q = `
                SELECT ` + recordColumns + `
                FROM consent_records
                WHERE organization_id = $1 AND ` + subjectPredicate + `
                ORDER BY decided_at DESC, created_at DESC`
	args := append([]any{p.OrganizationID}, subjectArgs(subject)...)
	return s.query(ctx, q, args...)
}

// Current returns only the decisions still in force for a subject — at most one per
// purpose and channel. This is what a contact card shows.
func (s *Service) Current(ctx context.Context, p *rbac.Principal, subject Subject) ([]Record, error) {
	if subject.IsZero() {
		return nil, &ValidationError{Fields: map[string]string{"subject": "is required"}}
	}
	const q = `
                SELECT ` + recordColumns + `
                FROM consent_records
                WHERE organization_id = $1 AND ` + subjectPredicate + ` AND superseded_by IS NULL
                ORDER BY purpose, channel`
	args := append([]any{p.OrganizationID}, subjectArgs(subject)...)
	records, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Current = true
	}
	return records, nil
}

// History returns every revision for one subject, purpose and channel, newest
// first, so an operator can see how a decision changed.
func (s *Service) History(ctx context.Context, p *rbac.Principal, subject Subject, purpose Purpose, channel Channel) ([]Record, error) {
	if subject.IsZero() {
		return nil, &ValidationError{Fields: map[string]string{"subject": "is required"}}
	}
	if !purpose.Valid() || !channel.Valid() {
		return nil, &ValidationError{Fields: map[string]string{
			"purpose": "purpose and channel must be supported",
		}}
	}
	const q = `
                SELECT ` + recordColumns + `
                FROM consent_records
                WHERE organization_id = $1 AND ` + subjectPredicate + ` AND purpose = $4 AND channel = $5
                ORDER BY revision DESC`
	args := append([]any{p.OrganizationID}, subjectArgs(subject)...)
	args = append(args, string(purpose), string(channel))
	records, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Current = records[i].SupersededBy == nil
	}
	return records, nil
}

// subjectPredicate matches either subject key. The arguments are always passed as
// (contact_id, email), so the predicate is written once and reused by every read.
const subjectPredicate = `(($2::uuid IS NOT NULL AND contact_id = $2::uuid)
        OR ($2::uuid IS NULL AND subject_email = $3))`

// subjectArgs returns the two parameters subjectPredicate expects.
func subjectArgs(subject Subject) []any {
	var email any
	if subject.ContactID == nil {
		email = normalizeEmail(subject.Email)
	}
	return []any{subject.ContactID, email}
}

// currentRow loads the decision in force for a subject, purpose and channel, or nil
// when none has been recorded.
func (s *Service) currentRow(ctx context.Context, orgID uuid.UUID, subject Subject, purpose Purpose, channel Channel) (*Record, error) {
	const q = `
                SELECT ` + recordColumns + `
                FROM consent_records
                WHERE organization_id = $1 AND ` + subjectPredicate + `
                  AND purpose = $4 AND channel = $5 AND superseded_by IS NULL`
	args := append([]any{orgID}, subjectArgs(subject)...)
	args = append(args, string(purpose), string(channel))
	rec, err := scanRecord(s.db.Q(ctx).QueryRow(ctx, q, args...))
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load the current consent record: %w", err)
	}
	return rec, nil
}

// lockKey serialises writers for one decision key. Without it two concurrent
// recordings of the same key could both read "no current row" and both insert
// revision 1, leaving two decisions in force at once.
func (s *Service) lockKey(ctx context.Context, orgID uuid.UUID, subject Subject, purpose, channel string) error {
	key := fmt.Sprintf("%s|%s|%s|%s|%s", orgID, subjectKey(subject), purpose, channel, "consent")
	if _, err := s.db.Q(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("serialise consent writes: %w", err)
	}
	return nil
}

// subjectKey renders a subject as a stable string for the advisory lock.
func subjectKey(subject Subject) string {
	if subject.ContactID != nil {
		return "c:" + subject.ContactID.String()
	}
	return "e:" + normalizeEmail(subject.Email)
}

func (s *Service) query(ctx context.Context, q string, args ...any) ([]Record, error) {
	rows, err := s.db.Q(ctx).Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list consent records: %w", err)
	}
	defer rows.Close()

	out := make([]Record, 0)
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(row rowScanner) (*Record, error) {
	var r Record
	if err := row.Scan(&r.ID, &r.OrganizationID, &r.ContactID, &r.SubjectEmail, &r.Purpose,
		&r.Channel, &r.State, &r.Basis, &r.Source, &r.Evidence, &r.Revision,
		&r.SupersededBy, &r.RecordedBy, &r.DecidedAt, &r.ExpiresAt,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Current = r.SupersededBy == nil
	return &r, nil
}

// nullIfEmpty returns nil for a blank string so an optional column is stored as
// NULL rather than as an empty string that reads as present.
func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
