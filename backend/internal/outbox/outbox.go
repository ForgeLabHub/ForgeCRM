// Package outbox implements the transactional outbox pattern.
//
// A domain write that must also produce an event inserts the event row in the
// same transaction as the write. A worker then publishes pending rows. This gives
// atomicity between "the data changed" and "the event exists", at the cost of
// at-least-once delivery — which is the correct trade for a networked system and
// is why every consumer must be idempotent.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/logging"
)

// Event types emitted by the platform. Versioning is carried in SchemaVersion.
const (
	EventAccountCreated     = "crm.account.created"
	EventAccountUpdated     = "crm.account.updated"
	EventAccountArchived    = "crm.account.archived"
	EventContactCreated     = "crm.contact.created"
	EventContactUpdated     = "crm.contact.updated"
	EventContactArchived    = "crm.contact.archived"
	EventLeadCreated        = "crm.lead.created"
	EventLeadUpdated        = "crm.lead.updated"
	EventLeadConverted      = "crm.lead.converted"
	EventDealCreated        = "crm.deal.created"
	EventDealUpdated        = "crm.deal.updated"
	EventDealStageChanged   = "crm.deal.stage_changed"
	EventDealWon            = "crm.deal.won"
	EventDealLost           = "crm.deal.lost"
	EventTaskCreated        = "crm.task.created"
	EventTaskCompleted      = "crm.task.completed"
	EventTicketCreated      = "support.ticket.created"
	EventTicketUpdated      = "support.ticket.updated"
	EventTicketClosed       = "support.ticket.closed"
	EventImportCompleted    = "data.import.completed"
	EventImportFailed       = "data.import.failed"
	EventMigrationCompleted = "data.migration.completed"
	EventIntegrationRun     = "integration.run.completed"
	EventUserInvited        = "platform.user.invited"
	EventUserBlocked        = "platform.user.blocked"
)

// EventTypes lists every event type the platform can emit, in the exact spelling
// the dispatcher matches a subscription against.
//
// It is the single source of truth a client must offer when creating a webhook
// subscription. The interface once hard-coded a shortened list ("contact.created"
// rather than "crm.contact.created"); a subscription built from it selected no
// events, so the endpoint silently received nothing. A list derived from these
// constants cannot drift from what the dispatcher compares, which is what makes
// the failure impossible to reintroduce.
func EventTypes() []string {
	return []string{
		EventAccountCreated, EventAccountUpdated, EventAccountArchived,
		EventContactCreated, EventContactUpdated, EventContactArchived,
		EventLeadCreated, EventLeadUpdated, EventLeadConverted,
		EventDealCreated, EventDealUpdated, EventDealStageChanged,
		EventDealWon, EventDealLost,
		EventTaskCreated, EventTaskCompleted,
		EventTicketCreated, EventTicketUpdated, EventTicketClosed,
		EventImportCompleted, EventImportFailed, EventMigrationCompleted,
		EventIntegrationRun, EventUserInvited, EventUserBlocked,
	}
}

// maxPublishAttempts bounds how long a broken event is retried before it is
// dead-lettered. Without a bound the row stays unpublished forever and every
// drain re-reads it, so one bad payload costs work on every poll for the life of
// the deployment.
const maxPublishAttempts = 10

// Event is one entry in the outbox.
type Event struct {
	ID             uuid.UUID       `json:"event_id"`
	OrganizationID *uuid.UUID      `json:"tenant_id,omitempty"`
	Type           string          `json:"event_type"`
	SchemaVersion  int             `json:"schema_version"`
	Source         string          `json:"source"`
	Payload        json.RawMessage `json:"payload"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	PublishedAt    *time.Time      `json:"published_at,omitempty"`
	Attempts       int             `json:"attempts"`
	LastError      *string         `json:"last_error,omitempty"`
	// DeadLetteredAt is set when the event exhausted its publish attempts. A dead
	// event is never retried again, so it stops costing work on every drain and
	// becomes visible to an operator instead of retrying silently forever.
	DeadLetteredAt *time.Time `json:"dead_lettered_at,omitempty"`
}

// Envelope is the canonical external representation of an event.
type Envelope struct {
	EventID       uuid.UUID       `json:"event_id"`
	EventType     string          `json:"event_type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Source        string          `json:"source"`
	SchemaVersion int             `json:"schema_version"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	TenantID      *uuid.UUID      `json:"tenant_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// Store reads and writes outbox entries.
type Store struct {
	db *database.DB
}

// NewStore builds an outbox store.
func NewStore(db *database.DB) *Store { return &Store{db: db} }

// Emit appends an event. When called with a transaction in the context the insert
// joins it, which is the whole point of the pattern.
func (s *Store) Emit(ctx context.Context, orgID *uuid.UUID, eventType string, payload any) error {
	if eventType == "" {
		return errors.New("event type is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	const q = `
		INSERT INTO outbox_events (organization_id, event_type, schema_version, source, payload, correlation_id)
		VALUES ($1, $2, 1, 'forgecrm', $3, NULLIF($4, ''))`
	if _, err := s.db.Q(ctx).Exec(ctx, q, orgID, eventType, body, logging.CorrelationID(ctx)); err != nil {
		return fmt.Errorf("emit event %s: %w", eventType, err)
	}
	return nil
}

// ClaimPending atomically claims up to limit unpublished events for one drain.
//
// The previous read left the rows pending, so two workers polling at the same
// instant both read the same events and both dispatched them. The claim marks the
// rows claimed in the same statement that selects them: the caller now owns them
// and a second worker skips them. A worker that dies mid-dispatch leaves the row
// claimed, and ReleaseStale returns it after a timeout, so a crash costs a
// redelivery rather than a lost event.
func (s *Store) ClaimPending(ctx context.Context, workerID string, limit int) ([]Event, error) {
	const q = `
UPDATE outbox_events SET
claimed_at = now(),
claimed_by = $1
WHERE id IN (
SELECT id FROM outbox_events
WHERE published_at IS NULL
  AND dead_lettered_at IS NULL
  AND attempts < $3
  AND (claimed_at IS NULL OR claimed_at < now() - interval '5 minutes')
ORDER BY occurred_at
FOR UPDATE SKIP LOCKED
LIMIT $2
)
RETURNING id, organization_id, event_type, schema_version, source, payload,
          correlation_id, occurred_at, published_at, attempts, last_error`
	rows, err := s.db.Pool().Query(ctx, q, workerID, limit, maxPublishAttempts)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var correlationID *string
		if err := rows.Scan(&e.ID, &e.OrganizationID, &e.Type, &e.SchemaVersion, &e.Source,
			&e.Payload, &correlationID, &e.OccurredAt, &e.PublishedAt, &e.Attempts, &e.LastError); err != nil {
			return nil, err
		}
		if correlationID != nil {
			e.CorrelationID = *correlationID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// FetchPending returns unpublished events, oldest first.
//
// Read-only, and used by the admin event log and tests. The drain path uses
// ClaimPending, because reading without claiming is what allowed two workers to
// dispatch the same event.
func (s *Store) FetchPending(ctx context.Context, limit int) ([]Event, error) {
	const q = `
SELECT id, organization_id, event_type, schema_version, source, payload,
       correlation_id, occurred_at, published_at, attempts, last_error
FROM outbox_events
WHERE published_at IS NULL AND dead_lettered_at IS NULL
ORDER BY occurred_at
LIMIT $1`
	rows, err := s.db.Q(ctx).Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch outbox: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var correlationID *string
		if err := rows.Scan(&e.ID, &e.OrganizationID, &e.Type, &e.SchemaVersion, &e.Source,
			&e.Payload, &correlationID, &e.OccurredAt, &e.PublishedAt, &e.Attempts, &e.LastError); err != nil {
			return nil, err
		}
		if correlationID != nil {
			e.CorrelationID = *correlationID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkPublished records successful publication.
func (s *Store) MarkPublished(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE outbox_events SET published_at = now(), claimed_at = NULL, claimed_by = NULL WHERE id = $1`
	if _, err := s.db.Pool().Exec(ctx, q, id); err != nil {
		return fmt.Errorf("mark event published: %w", err)
	}
	return nil
}

// MarkFailed records a publication failure and increments the attempt counter.
//
// The claim is released so the next drain can retry immediately rather than
// waiting out the stale-claim timeout, and the row is dead-lettered once the
// attempts are exhausted so the pending set stays bounded.
func (s *Store) MarkFailed(ctx context.Context, id uuid.UUID, cause error) error {
	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	const q = `
UPDATE outbox_events SET
attempts = attempts + 1,
last_error = $2,
claimed_at = NULL,
claimed_by = NULL,
dead_lettered_at = CASE WHEN attempts + 1 >= $3 THEN now() ELSE dead_lettered_at END
WHERE id = $1`
	if _, err := s.db.Pool().Exec(ctx, q, id, msg, maxPublishAttempts); err != nil {
		return fmt.Errorf("mark event failed: %w", err)
	}
	return nil
}

// ReleaseStale returns events abandoned by a crashed drain to the pending set.
// Called at worker start, so an unclean shutdown does not strand an event behind
// a claim that will never be released.
func (s *Store) ReleaseStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	const q = `
UPDATE outbox_events SET claimed_at = NULL, claimed_by = NULL
WHERE published_at IS NULL
  AND dead_lettered_at IS NULL
  AND claimed_at IS NOT NULL
  AND claimed_at < now() - $1::interval`
	tag, err := s.db.Pool().Exec(ctx, q, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("release stale outbox claims: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ToEnvelope converts a stored event to its external representation.
func (e Event) ToEnvelope() Envelope {
	return Envelope{
		EventID:       e.ID,
		EventType:     e.Type,
		OccurredAt:    e.OccurredAt,
		Source:        e.Source,
		SchemaVersion: e.SchemaVersion,
		CorrelationID: e.CorrelationID,
		TenantID:      e.OrganizationID,
		Payload:       e.Payload,
	}
}

// Get loads one event by id.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Event, error) {
	const q = `
		SELECT id, organization_id, event_type, schema_version, source, payload,
		       correlation_id, occurred_at, published_at, attempts, last_error
		FROM outbox_events WHERE id = $1`
	var e Event
	err := s.db.Q(ctx).QueryRow(ctx, q, id).Scan(&e.ID, &e.OrganizationID, &e.Type,
		&e.SchemaVersion, &e.Source, &e.Payload, &e.CorrelationID, &e.OccurredAt,
		&e.PublishedAt, &e.Attempts, &e.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load event: %w", err)
	}
	return &e, nil
}

// List returns recent events for an organization, for the admin event log.
func (s *Store) List(ctx context.Context, orgID *uuid.UUID, eventType string, limit, offset int) ([]Event, int64, error) {
	args := []any{}
	where := "WHERE 1 = 1"
	if orgID != nil {
		args = append(args, *orgID)
		where += fmt.Sprintf(" AND organization_id = $%d", len(args))
	}
	if eventType != "" {
		args = append(args, eventType)
		where += fmt.Sprintf(" AND event_type = $%d", len(args))
	}
	var total int64
	if err := s.db.Q(ctx).QueryRow(ctx, "SELECT count(*) FROM outbox_events "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count events: %w", err)
	}
	args = append(args, limit, offset)
	q := `SELECT id, organization_id, event_type, schema_version, source, payload,
	             correlation_id, occurred_at, published_at, attempts, last_error
	      FROM outbox_events ` + where +
		fmt.Sprintf(" ORDER BY occurred_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.db.Q(ctx).Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OrganizationID, &e.Type, &e.SchemaVersion, &e.Source,
			&e.Payload, &e.CorrelationID, &e.OccurredAt, &e.PublishedAt, &e.Attempts, &e.LastError); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
