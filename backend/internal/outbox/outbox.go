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

// FetchPending returns unpublished events, oldest first.
func (s *Store) FetchPending(ctx context.Context, limit int) ([]Event, error) {
	const q = `
		SELECT id, organization_id, event_type, schema_version, source, payload,
		       correlation_id, occurred_at, published_at, attempts, last_error
		FROM outbox_events
		WHERE published_at IS NULL
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
	const q = `UPDATE outbox_events SET published_at = now() WHERE id = $1`
	if _, err := s.db.Q(ctx).Exec(ctx, q, id); err != nil {
		return fmt.Errorf("mark event published: %w", err)
	}
	return nil
}

// MarkFailed records a publication failure and increments the attempt counter.
func (s *Store) MarkFailed(ctx context.Context, id uuid.UUID, cause error) error {
	msg := cause.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	const q = `UPDATE outbox_events SET attempts = attempts + 1, last_error = $2 WHERE id = $1`
	if _, err := s.db.Q(ctx).Exec(ctx, q, id, msg); err != nil {
		return fmt.Errorf("mark event failed: %w", err)
	}
	return nil
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
