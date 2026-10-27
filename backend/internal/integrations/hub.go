package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
)

// Adapter is the lifecycle every integration implements.
//
// WriteRecords is only meaningful for adapters whose configuration permits
// writing; an adapter that is read-only returns ErrWriteNotSupported, which keeps
// the "read by default" rule explicit rather than implied.
type Adapter interface {
	// Kind returns the adapter's identifier, matching integrations.kind.
	Kind() string
	ValidateConfig(ctx context.Context) error
	TestConnection(ctx context.Context) error
	DiscoverSchema(ctx context.Context) (Schema, error)
	ReadRecords(ctx context.Context, req ReadRequest) ([]Record, error)
	TransformRecords(ctx context.Context, records []Record, m map[string]string) ([]Record, error)
	WriteRecords(ctx context.Context, records []Record) (WriteResult, error)
	GetStatus(ctx context.Context) (Status, error)
	Close() error
}

// Schema describes a source's structure.
type Schema struct {
	Tables []Table `json:"tables"`
}

// Table is one table or resource.
type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
	Rows    *int64   `json:"rows,omitempty"`
}

// Column describes one field.
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primary_key"`
}

// Record is one row of source data.
type Record map[string]any

// ReadRequest bounds a read.
type ReadRequest struct {
	Table     string
	Columns   []string
	Offset    int64
	Limit     int
	Watermark map[string]any
	OrderBy   string
	Filters   map[string]any
}

// WriteResult reports what a write did.
type WriteResult struct {
	Written int64 `json:"written"`
	Failed  int64 `json:"failed"`
}

// Status reports an adapter's health.
type Status struct {
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
	LastError string    `json:"last_error,omitempty"`
}

// ErrWriteNotSupported is returned by read-only adapters.
var ErrWriteNotSupported = errors.New("this adapter does not support writing")

// ---------------------------------------------------------------------------
// Hub
// ---------------------------------------------------------------------------

// Integration is a configured connection.
type Integration struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Direction      string          `json:"direction"`
	Status         string          `json:"status"`
	Config         json.RawMessage `json:"config"`
	AllowWrite     bool            `json:"allow_write"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	MaxConcurrency int             `json:"max_concurrency"`
	QuotaPerHour   *int            `json:"quota_per_hour,omitempty"`
	LastTestAt     *time.Time      `json:"last_test_at,omitempty"`
	LastTestOK     *bool           `json:"last_test_ok,omitempty"`
	LastTestError  *string         `json:"last_test_error,omitempty"`
	LastSyncAt     *time.Time      `json:"last_sync_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	ArchivedAt     *time.Time      `json:"archived_at,omitempty"`
	// HasSecrets reports whether credentials are stored, without revealing them.
	HasSecrets bool `json:"has_secrets"`
}

// Hub manages integrations.
type Hub struct {
	db      *database.DB
	audit   *audit.Logger
	keyring *secrets.Keyring
	guard   *GuardedClient
}

// NewHub builds the integration hub.
func NewHub(db *database.DB, auditLog *audit.Logger, keyring *secrets.Keyring) *Hub {
	return &Hub{db: db, audit: auditLog, keyring: keyring, guard: NewGuardedClient(30 * time.Second)}
}

// CreateRequest registers an integration.
type CreateRequest struct {
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	Direction      string            `json:"direction"`
	Config         json.RawMessage   `json:"config"`
	Secrets        map[string]string `json:"secrets"`
	AllowWrite     bool              `json:"allow_write"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	MaxConcurrency int               `json:"max_concurrency"`
	QuotaPerHour   *int              `json:"quota_per_hour"`
}

const integrationColumns = `
	id, organization_id, name, kind, direction, status, config, allow_write,
	timeout_seconds, max_concurrency, quota_per_hour, last_test_at, last_test_ok,
	last_test_error, last_sync_at, created_at, archived_at,
	(secrets_encrypted IS NOT NULL) AS has_secrets`

func scanIntegration(row rowScanner) (*Integration, error) {
	var i Integration
	err := row.Scan(&i.ID, &i.OrganizationID, &i.Name, &i.Kind, &i.Direction, &i.Status,
		&i.Config, &i.AllowWrite, &i.TimeoutSeconds, &i.MaxConcurrency, &i.QuotaPerHour,
		&i.LastTestAt, &i.LastTestOK, &i.LastTestError, &i.LastSyncAt, &i.CreatedAt,
		&i.ArchivedAt, &i.HasSecrets)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

type rowScanner interface{ Scan(dest ...any) error }

// Create registers an integration. Credentials are encrypted at rest and are
// never returned by any read path.
func (h *Hub) Create(ctx context.Context, p *rbac.Principal, req CreateRequest) (*Integration, error) {
	details := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		details["name"] = "name is required"
	}
	if strings.TrimSpace(req.Kind) == "" {
		details["kind"] = "kind is required"
	}
	switch req.Direction {
	case "", "inbound", "outbound", "bidirectional":
	default:
		details["direction"] = "must be inbound, outbound or bidirectional"
	}
	if req.Direction == "" {
		req.Direction = "inbound"
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}
	if req.TimeoutSeconds > 600 {
		details["timeout_seconds"] = "must not exceed 600"
	}
	if req.MaxConcurrency <= 0 {
		req.MaxConcurrency = 1
	}
	if len(details) > 0 {
		return nil, httpx.ValidationError(details)
	}

	var encSecrets []byte
	if len(req.Secrets) > 0 {
		plain, err := json.Marshal(req.Secrets)
		if err != nil {
			return nil, err
		}
		encSecrets, err = h.keyring.Encrypt(plain)
		if err != nil {
			return nil, fmt.Errorf("encrypt integration secrets: %w", err)
		}
	}
	cfg := req.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}

	var created *Integration
	err := h.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO integrations (organization_id, name, kind, direction, config,
				secrets_encrypted, allow_write, timeout_seconds, max_concurrency, quota_per_hour, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			RETURNING ` + integrationColumns
		i, err := scanIntegration(h.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, req.Name,
			req.Kind, req.Direction, cfg, encSecrets, req.AllowWrite, req.TimeoutSeconds,
			req.MaxConcurrency, req.QuotaPerHour, p.UserID))
		if err != nil {
			return describeIntegrationError(err)
		}
		created = i
		return h.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionIntegrationCreated), EntityType: "integration",
			EntityID: i.ID.String(),
			Metadata: map[string]any{"kind": i.Kind, "direction": i.Direction, "allow_write": i.AllowWrite},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// List returns the organization's integrations.
func (h *Hub) List(ctx context.Context, p *rbac.Principal) ([]Integration, error) {
	const q = `SELECT ` + integrationColumns + `
		FROM integrations WHERE organization_id = $1 AND archived_at IS NULL
		ORDER BY created_at DESC`
	rows, err := h.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	defer rows.Close()
	var out []Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// Get loads one integration in scope.
func (h *Hub) Get(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Integration, error) {
	const q = `SELECT ` + integrationColumns + ` FROM integrations WHERE id = $1 AND organization_id = $2`
	i, err := scanIntegration(h.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return i, nil
}

// Secrets decrypts an integration's stored credentials for the adapter's use.
// This is the only function that returns plaintext secrets, and it is never
// reachable from a read-only HTTP handler.
func (h *Hub) Secrets(ctx context.Context, id, orgID uuid.UUID) (map[string]string, error) {
	const q = `SELECT secrets_encrypted FROM integrations WHERE id = $1 AND organization_id = $2`
	var enc []byte
	if err := h.db.Q(ctx).QueryRow(ctx, q, id, orgID).Scan(&enc); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	if len(enc) == 0 {
		return map[string]string{}, nil
	}
	plain, err := h.keyring.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt integration secrets: %w", err)
	}
	var out map[string]string
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("decode integration secrets: %w", err)
	}
	return out, nil
}

// TestConnection validates configuration and connectivity, recording the result.
//
// The distinction matters: a successful TCP connection is not proof that
// credentials work, so the adapter reports both separately.
func (h *Hub) TestConnection(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*Status, error) {
	integration, err := h.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	adapter, err := h.BuildAdapter(ctx, integration)
	if err != nil {
		return nil, err
	}
	defer adapter.Close()

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(integration.TimeoutSeconds)*time.Second)
	defer cancel()

	var (
		status  Status
		testErr error
	)
	if err := adapter.ValidateConfig(timeoutCtx); err != nil {
		testErr = err
	} else if err := adapter.TestConnection(timeoutCtx); err != nil {
		testErr = err
	} else {
		status = Status{Healthy: true, Message: "connection and credentials verified", CheckedAt: time.Now()}
	}

	if testErr != nil {
		status = Status{Healthy: false, Message: "connection failed", CheckedAt: time.Now(),
			LastError: testErr.Error()}
		// The integration is marked in error but nothing else stops working.
		const upd = `UPDATE integrations SET last_test_at=now(), last_test_ok=false, last_test_error=$3 WHERE id=$1 AND organization_id=$2`
		if _, e := h.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID, truncate(testErr.Error(), 1000)); e != nil {
			return nil, e
		}
		return &status, nil
	}

	const upd = `UPDATE integrations SET last_test_at=now(), last_test_ok=true, last_test_error=NULL, status='enabled' WHERE id=$1 AND organization_id=$2`
	if _, err := h.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID); err != nil {
		return nil, err
	}
	return &status, nil
}

// Run records an adapter operation in the runs history.
type Run struct {
	ID             uuid.UUID  `json:"id"`
	IntegrationID  uuid.UUID  `json:"integration_id"`
	Operation      string     `json:"operation"`
	Status         string     `json:"status"`
	RecordsRead    int64      `json:"records_read"`
	RecordsWritten int64      `json:"records_written"`
	Error          *string    `json:"error,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// RecordRun stores an integration run outcome.
func (h *Hub) RecordRun(ctx context.Context, integrationID, orgID uuid.UUID, operation,
	status string, read, written int64, runErr error) error {
	var errText *string
	if runErr != nil {
		s := truncate(runErr.Error(), 2000)
		errText = &s
	}
	const q = `
		INSERT INTO integration_runs (integration_id, organization_id, operation, status,
			records_read, records_written, error, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7, now())`
	if _, err := h.db.Q(ctx).Exec(ctx, q, integrationID, orgID, operation, status,
		read, written, errText); err != nil {
		return fmt.Errorf("record integration run: %w", err)
	}
	return nil
}

// ListRuns returns the run history for an integration.
func (h *Hub) ListRuns(ctx context.Context, p *rbac.Principal, integrationID uuid.UUID, limit, offset int) ([]Run, int64, error) {
	if _, err := h.Get(ctx, p, integrationID); err != nil {
		return nil, 0, err
	}
	var total int64
	const countQ = `SELECT count(*) FROM integration_runs WHERE integration_id = $1`
	if err := h.db.Q(ctx).QueryRow(ctx, countQ, integrationID).Scan(&total); err != nil {
		return nil, 0, err
	}
	const q = `
		SELECT id, integration_id, operation, status, records_read, records_written,
		       error, started_at, finished_at
		FROM integration_runs WHERE integration_id = $1
		ORDER BY started_at DESC LIMIT $2 OFFSET $3`
	rows, err := h.db.Q(ctx).Query(ctx, q, integrationID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list integration runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.IntegrationID, &r.Operation, &r.Status, &r.RecordsRead,
			&r.RecordsWritten, &r.Error, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// Revoke disables an integration and clears its stored credentials.
func (h *Hub) Revoke(ctx context.Context, p *rbac.Principal, id uuid.UUID) error {
	return h.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			UPDATE integrations SET status='revoked', secrets_encrypted=NULL, archived_at=now()
			WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL`
		tag, err := h.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID)
		if err != nil {
			return fmt.Errorf("revoke integration: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return h.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionIntegrationRevoked), EntityType: "integration", EntityID: id.String(),
		})
	})
}

// SetStatus changes an integration's status without revoking it.
func (h *Hub) SetStatus(ctx context.Context, p *rbac.Principal, id uuid.UUID, status string) error {
	switch status {
	case "enabled", "disabled", "error":
	default:
		return httpx.ValidationError(map[string]string{"status": "must be enabled, disabled or error"})
	}
	return h.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `UPDATE integrations SET status=$3 WHERE id=$1 AND organization_id=$2 AND archived_at IS NULL`
		tag, err := h.db.Q(ctx).Exec(ctx, q, id, p.OrganizationID, status)
		if err != nil {
			return fmt.Errorf("update integration status: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return h.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionIntegrationUpdated), EntityType: "integration",
			EntityID: id.String(), After: map[string]any{"status": status},
		})
	})
}

// Guard exposes the SSRF-guarded client for adapters that make HTTP calls.
func (h *Hub) Guard() *GuardedClient { return h.guard }

func describeIntegrationError(err error) error {
	switch {
	case database.IsUniqueViolation(err):
		return httpx.NewError(409, httpx.CodeConflict, "an integration with this name already exists")
	default:
		return fmt.Errorf("create integration: %w", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
