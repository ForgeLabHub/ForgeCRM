package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/tracectx"
)

// Poster performs the outbound POST for a delivery. It is an interface so the
// delivery mechanics (claim, sign, record) can be tested against a local server;
// production always uses the SSRF-guarded client.
type Poster interface {
	PostJSON(ctx context.Context, target string, body []byte, headers map[string]string) (int, []byte, error)
}

// DeliverySender claims and delivers pending webhook deliveries.
//
// It exists because, until now, nothing sent them: the dispatcher created a
// delivery row for every matching subscription and no code ever selected those
// rows, so an outgoing webhook was never called. The sender is the consumer of
// that queue, and it is deliberately a claim-then-send loop rather than a plain
// read, so several workers can run it without sending the same delivery twice.
type DeliverySender struct {
	db      *database.DB
	audit   *audit.Logger
	keyring *secrets.Keyring
	poster  Poster
	log     *slog.Logger
}

// NewDeliverySender builds a sender. The keyring decrypts each subscription's
// signing secret; it is the same keyring the rest of the platform uses, so no
// second source of key material is introduced.
func NewDeliverySender(db *database.DB, auditLog *audit.Logger, keyring *secrets.Keyring) *DeliverySender {
	// The guard refuses an internal or cloud-metadata destination on every hop,
	// including redirects, which is what stops a subscriber from pointing a
	// webhook at 169.254.169.254.
	return NewDeliverySenderWithPoster(db, auditLog, keyring, integrations.NewGuardedClient(20*time.Second))
}

// NewDeliverySenderWithPoster is the injection point for the outbound transport.
// It exists for tests that must deliver to a local server; callers in production
// use NewDeliverySender so the SSRF guard is never bypassed.
func NewDeliverySenderWithPoster(db *database.DB, auditLog *audit.Logger, keyring *secrets.Keyring, poster Poster) *DeliverySender {
	return &DeliverySender{
		db:      db,
		audit:   auditLog,
		keyring: keyring,
		poster:  poster,
		log:     slog.Default(),
	}
}

// plainPoster is an unguarded HTTP poster. It exists only so tests can point a
// delivery at a local server, which the SSRF guard is right to refuse. It is
// deliberately not exported as a general-purpose client: nothing in production
// may construct one, or the guard could be bypassed by accident.
type plainPoster struct {
	client *http.Client
}

// NewPlainPoster returns a poster with no SSRF guard, for tests that deliver to a
// loopback test server. Production code must use NewDeliverySender.
func NewPlainPoster(timeout time.Duration) Poster {
	return &plainPoster{client: &http.Client{Timeout: timeout}}
}

func (p *plainPoster) PostJSON(ctx context.Context, target string, body []byte, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, respBody, nil
}

// ClaimDelivery atomically takes one pending delivery for sending.
//
// The claim is an UPDATE ... SKIP LOCKED, so two workers polling at the same
// instant take different rows and neither takes a row the other holds. A row is
// eligible when it is pending or failed and due, or when it was claimed but the
// claim is older than the lease — that last case is the only way a delivery
// abandoned by a crashed worker is recovered rather than stranded.
//
// Returns nil when nothing is due.
func (s *DeliverySender) ClaimDelivery(ctx context.Context, workerID string, lease time.Duration) (*Delivery, error) {
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	const q = `
		UPDATE webhook_deliveries SET
			status = 'delivering',
			locked_at = now(),
			locked_by = $1
		WHERE id = (
			SELECT id FROM webhook_deliveries
			WHERE (
				(status IN ('pending', 'failed') AND next_attempt_at <= now())
				OR (status = 'delivering' AND locked_at < now() - $2::interval)
			)
			ORDER BY next_attempt_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, subscription_id, event_type, status, attempts, next_attempt_at`
	var d Delivery
	err := s.db.Pool().QueryRow(ctx, q, workerID, lease.String()).
		Scan(&d.ID, &d.SubscriptionID, &d.EventType, &d.Status, &d.Attempts, &d.NextAttemptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim webhook delivery: %w", err)
	}
	return &d, nil
}

// SendDelivery posts one claimed delivery and records the outcome.
//
// Delivery is at-least-once, not exactly-once: a worker that dies after the
// endpoint accepted the POST but before the row was marked delivered leaves the
// claim to expire and the delivery is sent again. That is the honest guarantee
// for a networked POST, and it is why every payload carries a stable event_id for
// the receiver to de-duplicate on.
func (s *DeliverySender) SendDelivery(ctx context.Context, deliveryID uuid.UUID) error {
	const sel = `
		SELECT d.id, d.subscription_id, d.payload, s.target_url, s.secret_encrypted,
		       s.max_attempts, d.attempts
		FROM webhook_deliveries d
		JOIN webhook_subscriptions s ON s.id = d.subscription_id
		WHERE d.id = $1`
	var (
		id          uuid.UUID
		subID       uuid.UUID
		payload     []byte
		targetURL   string
		secretEnc   []byte
		maxAttempts int
		attempts    int
	)
	if err := s.db.Pool().QueryRow(ctx, sel, deliveryID).Scan(&id, &subID, &payload,
		&targetURL, &secretEnc, &maxAttempts, &attempts); err != nil {
		return fmt.Errorf("load webhook delivery: %w", err)
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	secret, err := s.keyring.Decrypt(secretEnc)
	if err != nil {
		return fmt.Errorf("decrypt webhook secret: %w", err)
	}

	timestamp := fmt.Sprint(time.Now().Unix())
	signature := s.keyring.WebhookSign(secret, timestamp, payload)
	headers := map[string]string{
		"X-Signature-256":       "sha256=" + signature,
		"X-Signature-Timestamp": timestamp,
		"X-ForgeCRM-Delivery":   id.String(),
	}
	// Continue the trace across the boundary. The delivery id is the seed, so the
	// first attempt and every retry share one trace id and a subscriber that logs
	// traceparent can be correlated with our own history of the same delivery. The
	// value is a child span of any trace already on the context, so a delivery
	// triggered inside a request stays part of that request's trace.
	if trace, ok := tracectx.FromContext(ctx); ok {
		headers[tracectx.Header] = trace.StartSpan().Format()
	} else {
		headers[tracectx.Header] = tracectx.FromSeed(id.String()).Format()
	}

	status, body, err := s.poster.PostJSON(ctx, targetURL, payload, headers)
	_ = body
	attempts++

	if err != nil {
		s.retryOrFail(ctx, id, attempts, maxAttempts, err.Error())
		return nil
	}
	if status >= 400 {
		s.retryOrFail(ctx, id, attempts, maxAttempts, fmt.Sprintf("the endpoint returned HTTP %d", status))
		return nil
	}

	const ok = `
		UPDATE webhook_deliveries
		SET status='delivered', attempts=$2, delivered_at=now(), response_status=$3,
		    last_error=NULL, locked_at=NULL, locked_by=NULL
		WHERE id=$1`
	if _, err := s.db.Pool().Exec(ctx, ok, id, attempts, status); err != nil {
		return fmt.Errorf("record delivered webhook: %w", err)
	}
	return nil
}

// retryOrFail schedules another attempt with exponential backoff, or marks the
// delivery dead once the attempts are exhausted.
//
// A dead delivery is terminal and visible; a delivery that retried forever would
// keep a broken subscriber in the pending set and hide the failure. The claim is
// released either way so the row is immediately eligible again rather than
// waiting out the lease.
func (s *DeliverySender) retryOrFail(ctx context.Context, id uuid.UUID, attempts, maxAttempts int, cause string) {
	if len(cause) > 1000 {
		cause = cause[:1000]
	}
	if attempts >= maxAttempts {
		s.deadLetter(ctx, id, attempts, cause)
		return
	}
	backoff := time.Duration(1<<uint(minInt(attempts, 8))) * time.Second
	_, _ = s.db.Pool().Exec(ctx, `
		UPDATE webhook_deliveries
		SET status='failed', attempts=$2, last_error=$3,
		    next_attempt_at = now() + $4::interval, locked_at=NULL, locked_by=NULL
		WHERE id=$1`, id, attempts, cause, fmt.Sprintf("%d seconds", int(backoff.Seconds())))
}

// deadLetter moves a delivery to the terminal dead state and records the
// transition in the audit trail exactly once.
//
// The transition and the guard are the same statement: `WHERE status <> 'dead'`
// means a second worker that also decides the delivery is exhausted updates no
// row, so it writes no audit entry and increments no metric. Without that guard a
// delivery that several workers raced to fail would produce several audit entries
// for one event, and an alert built on the audit count would fire repeatedly for a
// single failure.
//
// A replay deliberately re-arms a dead delivery (status returns to 'pending'), so
// a delivery that dies again after a replay is a genuine second transition and is
// audited again. That is the distinction the guard draws: one audit per time the
// delivery actually reaches the dead state, not one per attempt and not one ever.
func (s *DeliverySender) deadLetter(ctx context.Context, id uuid.UUID, attempts int, cause string) {
	const q = `
		UPDATE webhook_deliveries
		SET status='dead', attempts=$2, last_error=$3, dead_lettered_at=now(),
		    locked_at=NULL, locked_by=NULL
		WHERE id=$1 AND status <> 'dead'
		RETURNING organization_id, subscription_id, event_type`
	var (
		orgID     uuid.UUID
		subID     uuid.UUID
		eventType string
	)
	err := s.db.Pool().QueryRow(ctx, q, id, attempts, cause).Scan(&orgID, &subID, &eventType)
	if errors.Is(err, pgx.ErrNoRows) {
		// Another worker already performed this transition. Doing nothing here is
		// the point: the audit entry and the metric must reflect the transition, not
		// the number of workers that observed it.
		return
	}
	if err != nil {
		s.log.Warn("could not mark a webhook delivery dead", "delivery_id", id, "error", err)
		return
	}

	// The audit entry is the durable, cross-replica record an operator alerts on.
	// It carries no target URL and no payload: the subscription id is enough to
	// find the endpoint, and a URL can carry a credential in its query string.
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.Entry{
			OrganizationID: &orgID,
			ActorType:      "system",
			Action:         string(audit.ActionWebhookDeadLettered),
			EntityType:     "webhook_delivery",
			EntityID:       id.String(),
			Metadata: map[string]any{
				"subscription_id": subID.String(),
				"event_type":      eventType,
				"attempts":        attempts,
				"last_error":      cause,
			},
		})
	}
}

// minInt is the two-argument minimum; Go 1.21+ has a builtin but this package
// already defines its own in one place, and a local helper avoids importing math.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ReleaseStaleDeliveries returns deliveries abandoned by a crashed sender to the
// pending set. Called at worker start so an unclean shutdown does not strand one.
func (s *DeliverySender) ReleaseStaleDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	const q = `
		UPDATE webhook_deliveries
		SET status='pending', locked_at=NULL, locked_by=NULL
		WHERE status='delivering' AND locked_at < now() - $1::interval`
	tag, err := s.db.Pool().Exec(ctx, q, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("release stale webhook deliveries: %w", err)
	}
	return tag.RowsAffected(), nil
}
