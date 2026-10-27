// Package migratex implements data migration from external systems.
//
// Safety rules that shape the whole package:
//
//   - source access is READ-ONLY by default; the PostgreSQL session is opened
//     with default_transaction_read_only=on, so even a coding mistake in a query
//     cannot modify the customer's database;
//   - data is read in bounded batches so a large table cannot exhaust memory;
//   - connection credentials are encrypted at rest and never logged;
//   - reconciliation reports divergence instead of hiding it.
package migratex

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

// SourceKind identifies a migration source type.
type SourceKind string

// Supported source kinds. MySQL, MSSQL and Oracle are recognised as configuration
// values but are not implemented in this cycle; attempting to use one returns a
// clear "not implemented" error rather than a silent no-op.
const (
	SourcePostgres SourceKind = "postgres"
	SourceMySQL    SourceKind = "mysql"
	SourceMSSQL    SourceKind = "mssql"
	SourceSQLite   SourceKind = "sqlite"
	SourceOracle   SourceKind = "oracle"
	SourceREST     SourceKind = "rest_api"
)

// Implemented reports whether a source kind can actually be used.
func Implemented(k SourceKind) bool {
	switch k {
	case SourcePostgres, SourceSQLite, SourceREST:
		return true
	}
	return false
}

// Source is a readable external data source.
type Source interface {
	Kind() SourceKind
	TestConnection(ctx context.Context) error
	DiscoverSchema(ctx context.Context) ([]TableSchema, error)
	CountRows(ctx context.Context, table string) (int64, error)
	ReadBatch(ctx context.Context, table string, columns []string, watermark map[string]any, limit int) ([]map[string]any, error)
	Close() error
}

// TableSchema describes a source table.
type TableSchema struct {
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns"`
	Rows    int64        `json:"rows"`
}

// ColumnInfo describes a source column.
type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primary_key"`
}

// ConnectionConfig describes how to reach a source. Secrets are held separately
// and are never part of the persisted configuration.
type ConnectionConfig struct {
	Kind     SourceKind `json:"kind"`
	Host     string     `json:"host,omitempty"`
	Port     int        `json:"port,omitempty"`
	Database string     `json:"database,omitempty"`
	Username string     `json:"username,omitempty"`
	Path     string     `json:"path,omitempty"`
	BaseURL  string     `json:"base_url,omitempty"`
	ReadPath string     `json:"read_path,omitempty"`
	SSLMode  string     `json:"ssl_mode,omitempty"`
}

// Validate checks the configuration without connecting.
func (c *ConnectionConfig) Validate() error {
	details := map[string]string{}
	if c.Kind == "" {
		details["kind"] = "kind is required"
	} else if !Implemented(c.Kind) {
		details["kind"] = fmt.Sprintf("%s sources are not implemented in this release", c.Kind)
	}
	switch c.Kind {
	case SourcePostgres, SourceMySQL, SourceMSSQL, SourceOracle:
		if c.Host == "" {
			details["host"] = "host is required"
		}
		if c.Database == "" {
			details["database"] = "database is required"
		}
	case SourceSQLite:
		if c.Path == "" {
			details["path"] = "path is required for a SQLite source"
		}
	case SourceREST:
		if c.BaseURL == "" {
			details["base_url"] = "base_url is required for a REST source"
		}
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

// ---------------------------------------------------------------------------
// PostgreSQL source
// ---------------------------------------------------------------------------

type postgresSource struct {
	db *database.DB
}

// NewPostgresSource opens a read-only PostgreSQL connection.
func NewPostgresSource(ctx context.Context, cfg ConnectionConfig, password string) (Source, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	port := cfg.Port
	if port == 0 {
		port = 5432
	}
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s&default_transaction_read_only=on&application_name=forgecrm-migration",
		urlEscape(cfg.Username), urlEscape(password), cfg.Host, port, urlEscape(cfg.Database), sslMode)

	db, err := database.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to source database: %w", err)
	}
	return &postgresSource{db: db}, nil
}

func (s *postgresSource) Kind() SourceKind { return SourcePostgres }

func (s *postgresSource) TestConnection(ctx context.Context) error {
	var readOnly string
	if err := s.db.Pool().QueryRow(ctx, "SHOW default_transaction_read_only").Scan(&readOnly); err != nil {
		return fmt.Errorf("source connection test failed: %w", err)
	}
	if strings.ToLower(readOnly) != "on" {
		return errors.New("source session is not read-only; refusing to continue")
	}
	return nil
}

func (s *postgresSource) DiscoverSchema(ctx context.Context) ([]TableSchema, error) {
	const q = `
		SELECT c.table_name, c.column_name, c.data_type, c.is_nullable = 'YES',
		       COALESCE(pk.is_primary, false)
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT kcu.table_name, kcu.column_name, true AS is_primary
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_name = tc.constraint_name
			 AND kcu.table_schema = tc.table_schema
			WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = 'public'
		) pk ON pk.table_name = c.table_name AND pk.column_name = c.column_name
		WHERE c.table_schema = 'public'
		ORDER BY c.table_name, c.ordinal_position`
	rows, err := s.db.Pool().Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("discover schema: %w", err)
	}
	defer rows.Close()

	byName := map[string]*TableSchema{}
	var order []string
	for rows.Next() {
		var table, column, dataType string
		var nullable, primary bool
		if err := rows.Scan(&table, &column, &dataType, &nullable, &primary); err != nil {
			return nil, err
		}
		t, ok := byName[table]
		if !ok {
			t = &TableSchema{Name: table}
			byName[table] = t
			order = append(order, table)
		}
		t.Columns = append(t.Columns, ColumnInfo{Name: column, Type: dataType, Nullable: nullable, PrimaryKey: primary})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]TableSchema, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

func (s *postgresSource) CountRows(ctx context.Context, table string) (int64, error) {
	quoted, err := quoteIdentifier(table)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := s.db.Pool().QueryRow(ctx, "SELECT count(*) FROM "+quoted).Scan(&n); err != nil {
		return 0, fmt.Errorf("count rows in %s: %w", table, err)
	}
	return n, nil
}

func (s *postgresSource) ReadBatch(ctx context.Context, table string, columns []string,
	watermark map[string]any, limit int) ([]map[string]any, error) {

	quotedTable, err := quoteIdentifier(table)
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, errors.New("at least one column is required")
	}
	quotedCols := make([]string, 0, len(columns))
	for _, c := range columns {
		q, err := quoteIdentifier(c)
		if err != nil {
			return nil, err
		}
		quotedCols = append(quotedCols, q)
	}
	if limit <= 0 || limit > 100000 {
		limit = 1000
	}

	orderBy := ""
	if col, ok := watermark["column"].(string); ok && col != "" {
		q, err := quoteIdentifier(col)
		if err != nil {
			return nil, err
		}
		orderBy = " ORDER BY " + q + " ASC"
	}

	q := fmt.Sprintf("SELECT %s FROM %s%s LIMIT $1", strings.Join(quotedCols, ", "), quotedTable, orderBy)
	rows, err := s.db.Pool().Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("read from %s: %w", table, err)
	}
	defer rows.Close()

	out, err := scanDynamicRows(rows, columns)
	if err != nil {
		return nil, err
	}
	if since, ok := watermark["value"]; ok && since != nil && len(out) > 0 {
		col, _ := watermark["column"].(string)
		out = filterAfter(out, col, since)
	}
	return out, nil
}

func (s *postgresSource) Close() error {
	s.db.Close()
	return nil
}

func scanDynamicRows(rows pgx.Rows, columns []string) ([]map[string]any, error) {
	var out []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		rec := make(map[string]any, len(columns))
		for i, name := range columns {
			if i < len(values) {
				rec[name] = normaliseValue(values[i])
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func normaliseValue(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.Format(time.RFC3339)
	case []byte:
		return string(t)
	case [16]byte:
		id, err := uuid.FromBytes(t[:])
		if err == nil {
			return id.String()
		}
		return fmt.Sprintf("%x", t)
	default:
		return v
	}
}

func filterAfter(records []map[string]any, column string, since any) []map[string]any {
	if column == "" {
		return records
	}
	sinceStr := fmt.Sprint(since)
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		v, ok := rec[column]
		if !ok {
			continue
		}
		if fmt.Sprint(v) > sinceStr {
			out = append(out, rec)
		}
	}
	return out
}

// quoteIdentifier validates and quotes a SQL identifier.
//
// Identifiers cannot be passed as bind parameters, so the only safe approach is
// to reject anything that is not a plain identifier and then quote it. This is
// the single place identifiers enter SQL.
func quoteIdentifier(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty identifier")
	}
	if len(name) > 63 {
		return "", fmt.Errorf("identifier %q is too long", name)
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '$' {
			continue
		}
		return "", fmt.Errorf("identifier %q contains an unsupported character", name)
	}
	return `"` + name + `"`, nil
}

func urlEscape(s string) string {
	replacer := strings.NewReplacer(
		"@", "%40", ":", "%3A", "/", "%2F", "?", "%3F", "#", "%23", " ", "%20", "&", "%26",
	)
	return replacer.Replace(s)
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// MigrationJob is a configured migration.
type MigrationJob struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	Name           string          `json:"name"`
	SourceKind     string          `json:"source_kind"`
	Strategy       string          `json:"strategy"`
	Status         string          `json:"status"`
	Config         json.RawMessage `json:"config"`
	Mapping        json.RawMessage `json:"mapping"`
	Watermark      json.RawMessage `json:"watermark"`
	Report         json.RawMessage `json:"report"`
	SourceTables   json.RawMessage `json:"source_tables"`
	CreatedAt      time.Time       `json:"created_at"`
	LastRunAt      *time.Time      `json:"last_run_at,omitempty"`
	Error          *string         `json:"error,omitempty"`
}

// Service manages migrations.
type Service struct {
	db      *database.DB
	audit   *audit.Logger
	keyring *secrets.Keyring
}

// NewService builds the migration service.
func NewService(db *database.DB, auditLog *audit.Logger, keyring *secrets.Keyring) *Service {
	return &Service{db: db, audit: auditLog, keyring: keyring}
}

// CreateRequest registers a migration.
type CreateRequest struct {
	Name         string           `json:"name"`
	Config       ConnectionConfig `json:"config"`
	Strategy     string           `json:"strategy"`
	Password     string           `json:"password"`
	SourceTables []string         `json:"source_tables"`
}

const migrationColumns = `
	id, organization_id, name, source_kind, strategy, status, config,
	mapping, watermark, report, source_tables, created_at, last_run_at, error`

func scanMigration(row rowScanner) (*MigrationJob, error) {
	var m MigrationJob
	err := row.Scan(&m.ID, &m.OrganizationID, &m.Name, &m.SourceKind, &m.Strategy, &m.Status,
		&m.Config, &m.Mapping, &m.Watermark, &m.Report, &m.SourceTables, &m.CreatedAt,
		&m.LastRunAt, &m.Error)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

type rowScanner interface{ Scan(dest ...any) error }

// Create registers a migration job with encrypted credentials.
func (s *Service) Create(ctx context.Context, p *rbac.Principal, req CreateRequest) (*MigrationJob, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, httpx.ValidationError(map[string]string{"name": "name is required"})
	}
	if err := req.Config.Validate(); err != nil {
		return nil, err
	}
	switch req.Strategy {
	case "", "one_time", "periodic", "incremental":
	case "cdc":
		return nil, httpx.NewError(501, httpx.CodeUnsupported,
			"change data capture is not implemented in this release")
	default:
		return nil, httpx.ValidationError(map[string]string{
			"strategy": "must be one_time, periodic, incremental or cdc"})
	}
	if req.Strategy == "" {
		req.Strategy = "one_time"
	}

	cfgJSON, err := json.Marshal(req.Config)
	if err != nil {
		return nil, err
	}
	var encPassword []byte
	if req.Password != "" {
		encPassword, err = s.keyring.Encrypt([]byte(req.Password))
		if err != nil {
			return nil, fmt.Errorf("encrypt source password: %w", err)
		}
	}
	tablesJSON, err := json.Marshal(req.SourceTables)
	if err != nil {
		return nil, err
	}

	var created *MigrationJob
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO migration_jobs (organization_id, name, source_kind, strategy, config,
				secrets_encrypted, source_tables, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING ` + migrationColumns
		m, err := scanMigration(s.db.Q(ctx).QueryRow(ctx, q, p.OrganizationID, req.Name,
			string(req.Config.Kind), req.Strategy, cfgJSON, encPassword, tablesJSON, p.UserID))
		if err != nil {
			return fmt.Errorf("create migration job: %w", err)
		}
		created = m
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
			Action: string(audit.ActionMigrationCreated), EntityType: "migration_job",
			EntityID: m.ID.String(),
			Metadata: map[string]any{"source_kind": m.SourceKind, "strategy": m.Strategy},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Get loads a migration in scope.
func (s *Service) Get(ctx context.Context, p *rbac.Principal, id uuid.UUID) (*MigrationJob, error) {
	const q = `SELECT ` + migrationColumns + ` FROM migration_jobs WHERE id = $1 AND organization_id = $2`
	m, err := scanMigration(s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// List returns the organization's migrations.
func (s *Service) List(ctx context.Context, p *rbac.Principal) ([]MigrationJob, error) {
	const q = `SELECT ` + migrationColumns + `
		FROM migration_jobs WHERE organization_id = $1 ORDER BY created_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, p.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list migration jobs: %w", err)
	}
	defer rows.Close()
	var out []MigrationJob
	for rows.Next() {
		m, err := scanMigration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// BuildSource constructs the source adapter for a migration, decrypting the
// stored password for the lifetime of the connection only.
func (s *Service) BuildSource(ctx context.Context, p *rbac.Principal, id uuid.UUID) (Source, error) {
	m, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	var cfg ConnectionConfig
	if err := json.Unmarshal(m.Config, &cfg); err != nil {
		return nil, httpx.NewError(422, httpx.CodeValidation, "stored migration config is not valid JSON")
	}
	var password string
	{
		const q = `SELECT secrets_encrypted FROM migration_jobs WHERE id = $1 AND organization_id = $2`
		var enc []byte
		if err := s.db.Q(ctx).QueryRow(ctx, q, id, p.OrganizationID).Scan(&enc); err != nil {
			return nil, err
		}
		if len(enc) > 0 {
			plain, err := s.keyring.Decrypt(enc)
			if err != nil {
				return nil, fmt.Errorf("decrypt source password: %w", err)
			}
			password = string(plain)
		}
	}

	switch cfg.Kind {
	case SourcePostgres:
		return NewPostgresSource(ctx, cfg, password)
	case SourceSQLite:
		return NewSQLiteSource(ctx, cfg)
	case SourceREST:
		return NewRESTSource(cfg, password)
	default:
		return nil, httpx.NewError(501, httpx.CodeUnsupported,
			fmt.Sprintf("%s sources are not implemented in this release", cfg.Kind))
	}
}

// DiscoverSchema analyses the source and records the result.
func (s *Service) DiscoverSchema(ctx context.Context, p *rbac.Principal, id uuid.UUID) ([]TableSchema, error) {
	src, err := s.BuildSource(ctx, p, id)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := src.TestConnection(timeoutCtx); err != nil {
		return nil, httpx.NewError(502, httpx.CodeUnsupported, "source connection failed: "+err.Error())
	}
	tables, err := src.DiscoverSchema(timeoutCtx)
	if err != nil {
		return nil, httpx.NewError(502, httpx.CodeUnsupported, "schema discovery failed: "+err.Error())
	}
	for i := range tables {
		if n, err := src.CountRows(timeoutCtx, tables[i].Name); err == nil {
			tables[i].Rows = n
		}
	}

	schemaJSON, _ := json.Marshal(tables)
	const upd = `UPDATE migration_jobs SET report = $3, status = 'analyzed' WHERE id = $1 AND organization_id = $2`
	if _, err := s.db.Q(ctx).Exec(ctx, upd, id, p.OrganizationID, schemaJSON); err != nil {
		return nil, fmt.Errorf("persist discovered schema: %w", err)
	}
	return tables, nil
}

// Reconciliation is the outcome of comparing source and target.
type Reconciliation struct {
	ID              uuid.UUID `json:"id"`
	SourceTable     string    `json:"source_table"`
	TargetEntity    string    `json:"target_entity"`
	SourceCount     int64     `json:"source_count"`
	TargetCount     int64     `json:"target_count"`
	MatchedCount    int64     `json:"matched_count"`
	MismatchedCount int64     `json:"mismatched_count"`
	Status          string    `json:"status"`
	Details         JSONMap   `json:"details"`
	CreatedAt       time.Time `json:"created_at"`
}

// JSONMap is a jsonb column.
type JSONMap map[string]any

// Value implements the driver Valuer interface.
func (m JSONMap) Value() (any, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

// Scan implements the sql.Scanner interface.
func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = JSONMap{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONMap", src)
	}
	if len(b) == 0 {
		*m = JSONMap{}
		return nil
	}
	return json.Unmarshal(b, m)
}

// Reconcile compares row counts between a source table and the target entity, and
// records the outcome. A mismatch is reported, never silently accepted.
func (s *Service) Reconcile(ctx context.Context, p *rbac.Principal, id uuid.UUID,
	sourceTable, targetEntity string) (*Reconciliation, error) {

	table, ok := targetTableFor(targetEntity)
	if !ok {
		return nil, httpx.ValidationError(map[string]string{"target_entity": "unsupported target entity"})
	}
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, err
	}

	src, err := s.BuildSource(ctx, p, id)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	timeoutCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	sourceCount, err := src.CountRows(timeoutCtx, sourceTable)
	if err != nil {
		return nil, httpx.NewError(502, httpx.CodeUnsupported, "could not count source rows: "+err.Error())
	}

	var targetCount int64
	if err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT count(*) FROM `+table+` WHERE organization_id = $1 AND archived_at IS NULL`,
		p.OrganizationID).Scan(&targetCount); err != nil {
		return nil, fmt.Errorf("count target rows: %w", err)
	}

	status := "match"
	if sourceCount != targetCount {
		status = "mismatch"
	}
	details := JSONMap{
		"note":  "counts are compared after an import; a mismatch is expected if the import is partial or if records were created elsewhere",
		"delta": sourceCount - targetCount,
	}

	var rec Reconciliation
	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		const q = `
			INSERT INTO migration_reconciliations (migration_job_id, source_table, target_entity,
				source_count, target_count, matched_count, mismatched_count, status, details)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING id, source_table, target_entity, source_count, target_count, matched_count,
				mismatched_count, status, details, created_at`
		r, err := scanReconciliation(s.db.Q(ctx).QueryRow(ctx, q, id, sourceTable, targetEntity,
			sourceCount, targetCount, min64(sourceCount, targetCount), abs64(sourceCount-targetCount),
			status, details))
		if err != nil {
			return fmt.Errorf("record reconciliation: %w", err)
		}
		rec = *r
		const touch = `UPDATE migration_jobs SET last_run_at = now() WHERE id = $1`
		if _, err := s.db.Q(ctx).Exec(ctx, touch, id); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func scanReconciliation(row rowScanner) (*Reconciliation, error) {
	var r Reconciliation
	err := row.Scan(&r.ID, &r.SourceTable, &r.TargetEntity, &r.SourceCount, &r.TargetCount,
		&r.MatchedCount, &r.MismatchedCount, &r.Status, &r.Details, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListReconciliations returns the reconciliation history for a migration.
func (s *Service) ListReconciliations(ctx context.Context, p *rbac.Principal, id uuid.UUID) ([]Reconciliation, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, err
	}
	const q = `
		SELECT id, source_table, target_entity, source_count, target_count, matched_count,
		       mismatched_count, status, details, created_at
		FROM migration_reconciliations WHERE migration_job_id = $1 ORDER BY created_at DESC`
	rows, err := s.db.Q(ctx).Query(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("list reconciliations: %w", err)
	}
	defer rows.Close()
	var out []Reconciliation
	for rows.Next() {
		r, err := scanReconciliation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// targetTableFor maps an entity type to its table, using an allowlist so no
// caller-supplied identifier reaches SQL.
func targetTableFor(entity string) (string, bool) {
	switch entity {
	case "account":
		return "accounts", true
	case "contact":
		return "contacts", true
	case "lead":
		return "leads", true
	case "deal":
		return "deals", true
	case "task":
		return "tasks", true
	case "ticket":
		return "tickets", true
	case "activity":
		return "activities", true
	}
	return "", false
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func abs64(a int64) int64 {
	if a < 0 {
		return -a
	}
	return a
}
