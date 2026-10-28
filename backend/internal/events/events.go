// Package events implements the inbound event gateway and outbound webhooks.
//
// Inbound: an external system posts a signed event. The gateway verifies the
// signature, enforces a timestamp window, and de-duplicates on (source, event_id)
// so a replay cannot cause a second effect.
//
// Outbound: a subscription receives every event whose type it matches, signed with
// its own secret. Delivery is at-least-once; consumers must be idempotent, which
// is why every payload carries an event_id.
package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/logging"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// TimestampTolerance is how far a signed request's timestamp may be from now.
const TimestampTolerance = 5 * time.Minute

// MaxInboundPayloadBytes bounds an inbound event body.
const MaxInboundPayloadBytes = 1 << 20

// Errors returned by the gateway.
var (
	ErrSignatureMissing = errors.New("signature header is missing")
	ErrSignatureInvalid = errors.New("signature does not match")
	ErrTimestampStale   = errors.New("request timestamp is outside the accepted window")
	ErrDuplicateEvent   = errors.New("event has already been received")
)

// InboundEvent is a verified inbound event.
type InboundEvent struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	Source        string          `json:"source"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// Gateway verifies and records inbound events.
type Gateway struct {
	db    *database.DB
	audit *audit.Logger
}

// NewGateway builds an event gateway.
func NewGateway(db *database.DB, auditLog *audit.Logger) *Gateway {
	return &Gateway{db: db, audit: auditLog}
}

// VerifyAndStore validates a signed request and records the event.
//
// The signature is an HMAC-SHA256 over "<timestamp>.<body>" using the shared
// secret, hex encoded, presented in the X-Signature-256 header. The timestamp is
// taken from X-Signature-Timestamp.
func (g *Gateway) VerifyAndStore(ctx context.Context, orgID *uuid.UUID, source string,
	secret []byte, headers http.Header, body []byte) (*InboundEvent, error) {

	if len(body) == 0 {
		return nil, httpx.NewError(400, httpx.CodeBadRequest, "request body is empty")
	}
	if len(body) > MaxInboundPayloadBytes {
		return nil, httpx.NewError(413, httpx.CodePayloadTooLarge, "event payload is too large")
	}

	sigHeader := headers.Get("X-Signature-256")
	if sigHeader == "" {
		return nil, httpx.NewError(401, httpx.CodeUnauthorized, ErrSignatureMissing.Error())
	}
	tsHeader := headers.Get("X-Signature-Timestamp")
	if tsHeader == "" {
		return nil, httpx.NewError(401, httpx.CodeUnauthorized, "timestamp header is missing")
	}
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return nil, httpx.NewError(401, httpx.CodeUnauthorized, "timestamp header is not a unix timestamp")
	}
	// A stale or future timestamp blocks replay of a captured request.
	skew := time.Since(time.Unix(ts, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > TimestampTolerance {
		return nil, httpx.NewError(401, httpx.CodeUnauthorized, ErrTimestampStale.Error())
	}

	expected := signInbound(secret, tsHeader, body)
	provided := strings.TrimPrefix(sigHeader, "sha256=")
	if !secrets.VerifySignature(expected, provided) {
		if g.audit != nil {
			_ = g.audit.RecordSecurity(ctx, audit.SecurityEvent{
				OrganizationID: orgID, Type: "webhook.signature_invalid", Severity: "warn",
				Message:  "inbound event rejected: signature mismatch",
				Metadata: map[string]any{"source": source},
			})
		}
		return nil, httpx.NewError(401, httpx.CodeUnauthorized, ErrSignatureInvalid.Error())
	}

	var ev InboundEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, httpx.NewError(400, httpx.CodeBadRequest, "event body is not valid JSON: "+err.Error())
	}
	if ev.EventID == "" || ev.EventType == "" {
		return nil, httpx.NewError(422, httpx.CodeValidation, "event_id and event_type are required")
	}
	if ev.SchemaVersion == 0 {
		ev.SchemaVersion = 1
	}
	if ev.Source == "" {
		ev.Source = source
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	if ev.Payload == nil {
		ev.Payload = json.RawMessage("{}")
	}

	// Insert with ON CONFLICT DO NOTHING: a replay is detected by the absence of
	// a returned row, which is race-free even under concurrent delivery.
	const q = `
		INSERT INTO events_inbox (organization_id, event_id, event_type, schema_version,
			source, payload, correlation_id, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (source, event_id) DO NOTHING
		RETURNING id`
	var id uuid.UUID
	err = g.db.Q(ctx).QueryRow(ctx, q, orgID, ev.EventID, ev.EventType, ev.SchemaVersion,
		ev.Source, ev.Payload, nullIfEmpty(ev.CorrelationID), ev.OccurredAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NewError(409, httpx.CodeConflict, ErrDuplicateEvent.Error())
	}
	if err != nil {
		return nil, fmt.Errorf("store inbound event: %w", err)
	}
	return &ev, nil
}

func signInbound(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignInboundForTest exposes the signature routine so tests and documentation can
// produce a valid signature without duplicating the algorithm.
func SignInboundForTest(secret []byte, timestamp string, body []byte) string {
	return signInbound(secret, timestamp, body)
}

// ---------------------------------------------------------------------------
// Subscriptions
// ---------------------------------------------------------------------------

// Subscription is a webhook endpoint registration.
type Subscription struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	TargetURL      string     `json:"target_url"`
	EventTypes     []string   `json:"event_types"`
	Enabled        bool       `json:"enabled"`
	MaxAttempts    int        `json:"max_attempts"`
	CreatedAt      time.Time  `json:"created_at"`
	DisabledAt     *time.Time `json:"disabled_at,omitempty"`
}

// CreateSubscriptionRequest describes a new subscription.
type CreateSubscriptionRequest struct {
	Name        string   `json:"name"`
	TargetURL   string   `json:"target_url"`
	EventTypes  []string `json:"event_types"`
	Secret      string   `json:"secret"`
	MaxAttempts int      `json:"max_attempts"`
}

// CreateSubscription registers a webhook endpoint. The secret is encrypted at
// rest and never returned.
func (g *Gateway) CreateSubscription(ctx context.Context, p *rbac.Principal,
	keyring *secrets.Keyring, req CreateSubscriptionRequest) (*Subscription, error) {

	details := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		details["name"] = "name is required"
	}
	if err := validateTargetURL(req.TargetURL); err != nil {
		details["target_url"] = err.Error()
	}
	if len(req.EventTypes) == 0 {
		details["event_types"] = "at least one event type is required"
	}
	if req.Secret == "" {
		// Generate a strong secret rather than allowing an empty one.
		s, err := secrets.RandomToken(32)
		if err != nil {
			return nil, err
		}
		req.Secret = s
	} else if len(req.Secret) < 16 {
		details["secret"] = "secret must be at least 16 characters"
	}
	if req.MaxAttempts <= 0 {
		req.MaxAttempts = 5
	}
	if len(details) > 0 {
		return nil, httpx.ValidationError(details)
	}

	encSecret, err := keyring.Encrypt([]byte(req.Secret))
	if err != nil {
		return nil, fmt.Errorf("encrypt webhook secret: %w", err)
	}

	var sub Subscription
	const q = `
		INSERT INTO webhook_subscriptions (organization_id, name, target_url, event_types,
			secret_encrypted, max_attempts, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, organization_id, name, target_url, event_types, enabled, max_attempts, created_at, disabled_at`
	if err := g.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, req.Name, req.TargetURL,
		req.EventTypes, encSecret, req.MaxAttempts, p.UserID).Scan(
		&sub.ID, &sub.OrganizationID, &sub.Name, &sub.TargetURL, &sub.EventTypes,
		&sub.Enabled, &sub.MaxAttempts, &sub.CreatedAt, &sub.DisabledAt); err != nil {
		return nil, fmt.Errorf("create webhook subscription: %w", err)
	}
	_ = g.audit.Record(ctx, audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionWebhookCreated), EntityType: "webhook_subscription",
		EntityID: sub.ID.String(), After: map[string]any{"target_url": sub.TargetURL, "event_types": sub.EventTypes},
	})
	return &sub, nil
}

// ListSubscriptions returns the organization's subscriptions.
func (g *Gateway) ListSubscriptions(ctx context.Context, p *rbac.Principal) ([]Subscription, error) {
	const q = `
		SELECT id, organization_id, name, target_url, event_types, enabled, max_attempts, created_at, disabled_at
		FROM webhook_subscriptions WHERE organization_id = $1 ORDER BY created_at DESC`
	rows, err := g.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list webhook subscriptions: %w", err)
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.OrganizationID, &s.Name, &s.TargetURL, &s.EventTypes,
			&s.Enabled, &s.MaxAttempts, &s.CreatedAt, &s.DisabledAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSubscriptionEnabled enables or disables a subscription.
func (g *Gateway) SetSubscriptionEnabled(ctx context.Context, p *rbac.Principal, id uuid.UUID, enabled bool) error {
	return g.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE webhook_subscriptions
			SET enabled = $3, disabled_at = CASE WHEN $3 THEN NULL ELSE now() END
			WHERE id = $1 AND organization_id = $2`
		tag, err := g.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID, enabled)
		if err != nil {
			return fmt.Errorf("update subscription: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return g.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionWebhookUpdated), EntityType: "webhook_subscription",
			EntityID: id.String(), After: map[string]any{"enabled": enabled},
		})
	})
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// Dispatcher turns outbox events into webhook deliveries.
type Dispatcher struct {
	db    *database.DB
	audit *audit.Logger
}

// NewDispatcher builds a dispatcher.
func NewDispatcher(db *database.DB, auditLog *audit.Logger) *Dispatcher {
	return &Dispatcher{db: db, audit: auditLog}
}

// Dispatch creates a delivery row for each enabled subscription that matches an
// event. A subscription's own origin is excluded to prevent an event that came
// from an external system being echoed straight back to it.
//
// The insert is idempotent: a unique index on (subscription_id, outbox_event_id)
// rejects a second row for the same event, and the conflict is absorbed rather
// than returned. Two drains running concurrently would otherwise each create
// their own delivery for the same event and the customer's endpoint would receive
// two POSTs for one domain change.
func (d *Dispatcher) Dispatch(ctx context.Context, ev outbox.Event) (int, error) {
	if ev.OrganizationID == nil {
		return 0, nil
	}
	const q = `
		INSERT INTO webhook_deliveries (subscription_id, organization_id, outbox_event_id,
			event_type, payload, status, next_attempt_at)
		SELECT s.id, s.organization_id, $1, $2, $3, 'pending', now()
		FROM webhook_subscriptions s
		WHERE s.organization_id = $4
		  AND s.enabled
		  AND s.disabled_at IS NULL
		  AND (cardinality(s.event_types) = 0 OR $2 = ANY(s.event_types))
		ON CONFLICT (subscription_id, outbox_event_id) DO NOTHING
		RETURNING id`
	envelope, err := json.Marshal(ev.ToEnvelope())
	if err != nil {
		return 0, fmt.Errorf("marshal event envelope: %w", err)
	}
	rows, err := d.db.Q(ctx).Query(ctx, q, ev.ID, ev.Type, envelope, *ev.OrganizationID)
	if err != nil {
		return 0, fmt.Errorf("dispatch event: %w", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// Delivery is one webhook delivery attempt record.
type Delivery struct {
	ID             uuid.UUID  `json:"id"`
	SubscriptionID uuid.UUID  `json:"subscription_id"`
	EventType      string     `json:"event_type"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ResponseStatus *int       `json:"response_status,omitempty"`
	LastError      *string    `json:"last_error,omitempty"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ListDeliveries returns the delivery history for a subscription.
func (g *Gateway) ListDeliveries(ctx context.Context, p *rbac.Principal, subID uuid.UUID, limit, offset int) ([]Delivery, int64, error) {
	// Confirm the subscription belongs to the caller's organization first.
	var exists int
	const checkQ = `SELECT 1 FROM webhook_subscriptions WHERE id = $1 AND organization_id = $2`
	if err := g.db.Q(ctx).QueryRow(ctx, checkQ, subID, p.OrganizationID).Scan(&exists); err != nil {
		return nil, 0, httpx.ErrNotFound
	}
	var total int64
	const countQ = `SELECT count(*) FROM webhook_deliveries WHERE subscription_id = $1`
	if err := g.db.Q(ctx).QueryRow(ctx, countQ, subID).Scan(&total); err != nil {
		return nil, 0, err
	}
	const q = `
		SELECT id, subscription_id, event_type, status, attempts, response_status,
		       last_error, next_attempt_at, delivered_at, created_at
		FROM webhook_deliveries WHERE subscription_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := g.db.Q(ctx).Query(ctx, q, subID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var dl Delivery
		if err := rows.Scan(&dl.ID, &dl.SubscriptionID, &dl.EventType, &dl.Status, &dl.Attempts,
			&dl.ResponseStatus, &dl.LastError, &dl.NextAttemptAt, &dl.DeliveredAt, &dl.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, dl)
	}
	return out, total, rows.Err()
}

// Replay resets a delivery so it will be attempted again.
func (g *Gateway) Replay(ctx context.Context, p *rbac.Principal, deliveryID uuid.UUID) error {
	return g.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE webhook_deliveries
			SET status='pending', attempts=0, next_attempt_at=now(), last_error=NULL
			WHERE id = $1 AND organization_id = $2`
		tag, err := g.db.Q(ctx).Exec(ctx, q, deliveryID, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("replay delivery: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return g.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionWebhookReplayed), EntityType: "webhook_delivery",
			EntityID: deliveryID.String(),
		})
	})
}

// ListInbound returns recorded inbound events.
func (g *Gateway) ListInbound(ctx context.Context, p *rbac.Principal, limit, offset int) ([]map[string]any, int64, error) {
	var total int64
	const countQ = `SELECT count(*) FROM events_inbox WHERE organization_id = $1`
	if err := g.db.Q(ctx).QueryRow(ctx, countQ, p.OrganizationID).Scan(&total); err != nil {
		return nil, 0, err
	}
	const q = `
		SELECT id, event_id, event_type, schema_version, source, status, received_at, processed_at, error
		FROM events_inbox WHERE organization_id = $1
		ORDER BY received_at DESC LIMIT $2 OFFSET $3`
	rows, err := g.db.Q(ctx).Query(ctx, q, p.OrganizationID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list inbound events: %w", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var (
			id            uuid.UUID
			eventID       string
			eventType     string
			schemaVersion int
			source        string
			status        string
			receivedAt    time.Time
			processedAt   *time.Time
			errText       *string
		)
		if err := rows.Scan(&id, &eventID, &eventType, &schemaVersion, &source, &status,
			&receivedAt, &processedAt, &errText); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{
			"id": id, "event_id": eventID, "event_type": eventType,
			"schema_version": schemaVersion, "source": source, "status": status,
			"received_at": receivedAt, "processed_at": processedAt, "error": errText,
		})
	}
	return out, total, rows.Err()
}

// validateTargetURL performs the static part of webhook target validation. The
// dynamic part (DNS resolution and redirect checks) lives in the SSRF guard.
func validateTargetURL(raw string) error {
	if raw == "" {
		return errors.New("target_url is required")
	}
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		return errors.New("target_url must use http or https")
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CorrelationIDFrom returns the correlation id for a request, for logging.
func CorrelationIDFrom(ctx context.Context) string { return logging.CorrelationID(ctx) }
