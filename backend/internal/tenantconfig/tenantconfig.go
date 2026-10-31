// Package tenantconfig stores an organization's own configuration: the defaults
// it chooses for itself, layered over the ones the platform ships.
//
// The problem it solves is that a platform serving several organizations cannot
// pick one value for all of them. The currency a deal is denominated in, the
// timezone a scheduled workflow runs in, the name an outbound email is sent
// under — each is a decision an organization makes about itself, and each was
// previously either hard-coded or deployment-wide. A single-tenant install can
// live with that; a second tenant cannot.
//
// Three rules shape the design.
//
//  1. The catalog is closed and versioned. A key is declared in code with its
//     kind, its default and the version of the schema it belongs to. An unknown
//     key is refused rather than stored, because a setting nothing reads is a
//     button that does nothing — the exact failure this platform has already been
//     bitten by once. The version travels with every stored value, so a value
//     written under an older schema is interpretable rather than guessed at.
//
//  2. An absent row is not a missing setting. Reading a key with no override
//     resolves to the catalog default. Defaults are therefore never materialised,
//     which keeps them in exactly one place; a seed step that wrote them out would
//     put the default in two places, and the two would drift the first time the
//     catalog changed.
//
//  3. Every change is recorded and previewed. A write returns the effective value
//     that will apply — not just an acknowledgement — so the caller sees the result
//     rather than inferring it, and it appends a revision carrying the previous
//     value, so the history of a shared default is not rewritten by the next
//     change.
//
// The service never touches CRM data. It reads and writes its own two tables and
// answers what an effective value is; the callers that need a default ask it.
package tenantconfig

import (
	"context"
	"encoding/json"
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

// SchemaVersion is the version of the catalog this build understands.
//
// It is stored with every value. When the catalog changes shape — a key is added,
// a default is corrected, a value's meaning is narrowed — this number is raised,
// and a stored value carrying an older number is a value written under different
// rules rather than a value that is simply current.
const SchemaVersion = 1

// Kind is the type of a setting's value. It is closed, so validation is a switch
// rather than a guess, and the API can render the right editor for the right kind.
type Kind string

// The supported kinds.
const (
	// KindString is free text with a length bound.
	KindString Kind = "string"
	// KindEnum is one of a declared set of values.
	KindEnum Kind = "enum"
	// KindInteger is a whole number within declared bounds.
	KindInteger Kind = "integer"
	// KindBoolean is true or false.
	KindBoolean Kind = "boolean"
)

// Definition describes one setting the platform understands.
//
// It is data, not a function: the API publishes the whole catalog so the UI can
// render an editor per key without a second description of the same thing, and a
// test can check that the catalog and the storage agree.
type Definition struct {
	// Key is the stable name, e.g. 'crm.default_currency'. It is what a value is
	// stored under, so it is never renamed in place — a rename would orphan every
	// stored override. A superseded key is removed from the catalog and its row is
	// cleaned up by a migration, not silently re-pointed.
	Key string
	// Title is a human label for the editor.
	Title string
	// Description says what the setting affects, in the operator's terms.
	Description string
	// Kind is the value's type.
	Kind Kind
	// Default is the value that applies when no override is stored. It is a Go value
	// of the kind's type (string, int64 or bool), and it is what an absent row
	// resolves to.
	Default any
	// Enum is the permitted set for KindEnum, in declaration order.
	Enum []string
	// Min and Max bound a KindInteger. They are pointers so that zero is a real
	// bound rather than "unset": a minimum of 0 is a bound, not the absence of one.
	Min *int64
	Max *int64
	// MaxLength bounds a KindString.
	MaxLength int
	// Since is the schema version that introduced the key.
	Since int
}

// catalog is the closed set of settings. A key absent from this list cannot be
// written: Set refuses it. That is what keeps "there is a setting for that" from
// becoming a promise the platform does not keep.
//
// The keys are grouped by the module that reads them, because the reason a key
// exists is the code that consumes it. A key with no consumer does not belong
// here.
var catalog = []Definition{
	{
		Key:         "crm.default_currency",
		Title:       "Default currency",
		Description: "The currency a new deal is denominated in when the caller does not name one. It is an ISO 4217 code; amounts in other currencies are not converted, and the analytics baseline reports the currency it computed in and the ones it excluded.",
		Kind:        KindString,
		Default:     "USD",
		MaxLength:   3,
		Since:       1,
	},
	{
		Key:         "tasks.default_priority",
		Title:       "Default task priority",
		Description: "The priority a new task is given when the caller does not name one.",
		Kind:        KindEnum,
		Default:     "normal",
		Enum:        []string{"low", "normal", "high", "urgent"},
		Since:       1,
	},
	{
		Key:         "workflow.default_timezone",
		Title:       "Default workflow timezone",
		Description: "The IANA timezone a scheduled workflow runs in when it does not name one. A workflow with its own schedule_timezone is unaffected.",
		Kind:        KindString,
		Default:     "UTC",
		MaxLength:   64,
		Since:       1,
	},
}

// catalogIndex is the lookup built once from catalog.
var catalogIndex = func() map[string]Definition {
	m := make(map[string]Definition, len(catalog))
	for _, d := range catalog {
		if _, dup := m[d.Key]; dup {
			panic("tenantconfig: duplicate catalog key " + d.Key)
		}
		m[d.Key] = d
	}
	return m
}()

// Catalog returns a copy of the catalog, sorted by key, so a caller cannot mutate
// the registry and the API can publish it deterministically.
func Catalog() []Definition {
	out := make([]Definition, len(catalog))
	copy(out, catalog)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Lookup returns the definition for a key.
func Lookup(key string) (Definition, bool) {
	d, ok := catalogIndex[key]
	return d, ok
}

// Keys returns every catalog key, sorted.
func Keys() []string {
	out := make([]string, 0, len(catalogIndex))
	for k := range catalogIndex {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func int64Ptr(v int64) *int64 { return &v }

// ErrNotFound is returned when a key is not in the catalog.
var ErrNotFound = errors.New("tenant setting not found")

// ValidationError describes why a value was refused, carrying the offending field
// so the API can render a 422 with per-field detail.
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
	return "invalid tenant setting: " + strings.Join(parts, "; ")
}

// Setting is one key's state in one organization: the catalog definition, the
// stored override if there is one, and the effective value that actually applies.
//
// Override and Effective are both present because the difference between them is
// the thing an operator needs to see. Showing only the effective value hides
// whether the organization chose it or inherited it; showing only the override
// hides what the platform is doing.
type Setting struct {
	Key             string     `json:"key"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Kind            string     `json:"kind"`
	Enum            []string   `json:"enum,omitempty"`
	Min             *int64     `json:"min,omitempty"`
	Max             *int64     `json:"max,omitempty"`
	MaxLength       int        `json:"max_length,omitempty"`
	Default         any        `json:"default"`
	Override        any        `json:"override,omitempty"`
	HasOverride     bool       `json:"has_override"`
	Effective       any        `json:"effective"`
	SchemaVersion   int        `json:"schema_version"`
	StoredUnderOld  bool       `json:"stored_under_older_schema,omitempty"`
	OverrideUpdated *time.Time `json:"override_updated_at,omitempty"`
	UpdatedBy       *uuid.UUID `json:"override_updated_by,omitempty"`
}

// Revision is one entry in a key's history.
type Revision struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Key            string     `json:"key"`
	Revision       int        `json:"revision"`
	Value          any        `json:"value,omitempty"`
	ResetToDefault bool       `json:"reset_to_default"`
	PreviousValue  any        `json:"previous_value,omitempty"`
	SchemaVersion  int        `json:"schema_version"`
	ChangedBy      *uuid.UUID `json:"changed_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Service provides tenant configuration operations.
type Service struct {
	db    *database.DB
	audit *audit.Logger
	now   func() time.Time
}

// NewService builds the tenant configuration service.
func NewService(db *database.DB, auditLog *audit.Logger) *Service {
	return &Service{db: db, audit: auditLog, now: time.Now}
}

// validate checks a raw JSON value against a definition and returns the
// normalised Go value.
//
// It is the same check every caller gets — the API, an import, a bootstrap — so a
// value that reaches storage has been through it exactly once, in one place.
func validate(d Definition, raw any) (any, error) {
	switch d.Kind {
	case KindString:
		s, ok := raw.(string)
		if !ok {
			return nil, &ValidationError{Fields: map[string]string{d.Key: "must be a string"}}
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, &ValidationError{Fields: map[string]string{d.Key: "must not be empty"}}
		}
		if d.MaxLength > 0 && len([]rune(s)) > d.MaxLength {
			return nil, &ValidationError{Fields: map[string]string{
				d.Key: fmt.Sprintf("must be at most %d characters", d.MaxLength),
			}}
		}
		return s, nil

	case KindEnum:
		s, ok := raw.(string)
		if !ok {
			return nil, &ValidationError{Fields: map[string]string{d.Key: "must be a string"}}
		}
		s = strings.TrimSpace(s)
		for _, allowed := range d.Enum {
			if s == allowed {
				return s, nil
			}
		}
		return nil, &ValidationError{Fields: map[string]string{
			d.Key: "must be one of " + strings.Join(d.Enum, ", "),
		}}

	case KindInteger:
		// JSON numbers decode to float64. A value that is not whole is refused rather
		// than truncated: silently rounding an operator's input is how a bound gets
		// crossed by a number they did not type.
		n, err := toInt64(raw)
		if err != nil {
			return nil, &ValidationError{Fields: map[string]string{d.Key: err.Error()}}
		}
		if d.Min != nil && n < *d.Min {
			return nil, &ValidationError{Fields: map[string]string{
				d.Key: fmt.Sprintf("must be at least %d", *d.Min),
			}}
		}
		if d.Max != nil && n > *d.Max {
			return nil, &ValidationError{Fields: map[string]string{
				d.Key: fmt.Sprintf("must be at most %d", *d.Max),
			}}
		}
		return n, nil

	case KindBoolean:
		b, ok := raw.(bool)
		if !ok {
			return nil, &ValidationError{Fields: map[string]string{d.Key: "must be true or false"}}
		}
		return b, nil

	default:
		return nil, &ValidationError{Fields: map[string]string{d.Key: "has an unsupported kind"}}
	}
}

// toInt64 accepts the numeric shapes a JSON body can produce and refuses the rest.
func toInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case float64:
		if v != float64(int64(v)) {
			return 0, errors.New("must be a whole number")
		}
		return int64(v), nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, errors.New("must be a whole number")
		}
		return n, nil
	default:
		return 0, errors.New("must be a whole number")
	}
}

// storedValue is one row of tenant_settings as read from the database.
type storedValue struct {
	value         []byte
	revision      int
	schemaVersion int
	updatedBy     *uuid.UUID
	updatedAt     time.Time
}

// readOverrides returns the stored overrides for an organization, keyed by key.
func (s *Service) readOverrides(ctx context.Context, orgID uuid.UUID) (map[string]storedValue, error) {
	const q = `SELECT key, value, revision, schema_version, updated_by, updated_at
	           FROM tenant_settings WHERE organization_id = $1`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("read tenant settings: %w", err)
	}
	defer rows.Close()

	out := map[string]storedValue{}
	for rows.Next() {
		var (
			key string
			sv  storedValue
		)
		if err := rows.Scan(&key, &sv.value, &sv.revision, &sv.schemaVersion, &sv.updatedBy, &sv.updatedAt); err != nil {
			return nil, err
		}
		out[key] = sv
	}
	return out, rows.Err()
}

// List returns every catalog key with its effective value for the organization.
//
// A key with no stored override is reported with HasOverride false and the catalog
// default as its effective value. That is the honest description: the platform is
// supplying the value, not the organization.
func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]Setting, error) {
	overrides, err := s.readOverrides(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]Setting, 0, len(catalog))
	for _, d := range Catalog() {
		setting := Setting{
			Key:           d.Key,
			Title:         d.Title,
			Description:   d.Description,
			Kind:          string(d.Kind),
			Enum:          d.Enum,
			Min:           d.Min,
			Max:           d.Max,
			MaxLength:     d.MaxLength,
			Default:       d.Default,
			Effective:     d.Default,
			SchemaVersion: d.Since,
		}
		if sv, ok := overrides[d.Key]; ok {
			decoded, err := decodeStored(sv.value)
			if err == nil {
				if normalised, verr := validate(d, decoded); verr == nil {
					setting.Override = normalised
					setting.HasOverride = true
					setting.Effective = normalised
				} else {
					// A stored value that no longer passes validation is not trusted into
					// the effective value: the catalog is the authority, and a value that
					// predates a narrowing must not silently apply. The default is reported
					// instead, with the flag making the reason visible, because one stale
					// row must not make the configuration page unusable.
					setting.StoredUnderOld = true
				}
			} else {
				setting.StoredUnderOld = true
			}
			updatedAt := sv.updatedAt
			setting.OverrideUpdated = &updatedAt
			setting.UpdatedBy = sv.updatedBy
			if sv.schemaVersion < d.Since {
				setting.StoredUnderOld = true
			}
		}
		out = append(out, setting)
	}
	return out, nil
}

// Get returns one key's state.
func (s *Service) Get(ctx context.Context, orgID uuid.UUID, key string) (*Setting, error) {
	d, ok := Lookup(key)
	if !ok {
		return nil, ErrNotFound
	}
	all, err := s.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Key == d.Key {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

// Effective returns the effective value of one key, which is the function every
// consumer calls.
//
// It takes the organization id rather than a principal because a consumer runs in
// the background as often as in a request: a queued email and a scheduled
// workflow both need the organization's default and neither has a caller's
// principal.
//
// An unknown key, or a stored value that no longer validates, resolves to the
// catalog default rather than an error. A consumer asking for a default must get a
// usable answer: failing a send because a settings row is stale would turn a
// cosmetic misconfiguration into an outage.
func (s *Service) Effective(ctx context.Context, orgID uuid.UUID, key string) (any, error) {
	d, ok := Lookup(key)
	if !ok {
		return nil, ErrNotFound
	}
	var raw []byte
	const q = `SELECT value FROM tenant_settings WHERE organization_id = $1 AND key = $2`
	if err := s.db.Q(ctx).QueryRow(ctx, q, orgID, key).Scan(&raw); err != nil {
		return d.Default, nil
	}
	decoded, err := decodeStored(raw)
	if err != nil {
		return d.Default, nil
	}
	normalised, err := validate(d, decoded)
	if err != nil {
		return d.Default, nil
	}
	return normalised, nil
}

// EffectiveString is Effective for a string-valued key, returning "" when the key
// is unknown.
func (s *Service) EffectiveString(ctx context.Context, orgID uuid.UUID, key string) string {
	v, err := s.Effective(ctx, orgID, key)
	if err != nil {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	return ""
}

// EffectiveInt is Effective for an integer-valued key.
func (s *Service) EffectiveInt(ctx context.Context, orgID uuid.UUID, key string) int64 {
	v, err := s.Effective(ctx, orgID, key)
	if err != nil {
		return 0
	}
	if n, ok := v.(int64); ok {
		return n
	}
	return 0
}

// EffectiveBool is Effective for a boolean-valued key.
func (s *Service) EffectiveBool(ctx context.Context, orgID uuid.UUID, key string) bool {
	v, err := s.Effective(ctx, orgID, key)
	if err != nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// Set writes an override for one key and returns the key's new state, including
// the effective value that will apply.
//
// The write is serialised per key with a transaction-scoped advisory lock, so two
// concurrent writers cannot both believe they are writing the same revision. The
// previous value is carried into the revision row, so the history is a chain that
// can be read forwards without reconstructing it.
func (s *Service) Set(ctx context.Context, p *rbac.Principal, key string, raw any) (*Setting, error) {
	d, ok := Lookup(key)
	if !ok {
		return nil, ErrNotFound
	}
	normalised, err := validate(d, raw)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(normalised)
	if err != nil {
		return nil, &ValidationError{Fields: map[string]string{key: "could not be encoded"}}
	}

	err = s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockKey(ctx, p.OrganizationID, key); err != nil {
			return err
		}
		previous, revision, err := s.currentRevision(ctx, p.OrganizationID, key)
		if err != nil {
			return err
		}
		const upsert = `
			INSERT INTO tenant_settings (organization_id, key, value, revision, schema_version, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (organization_id, key) DO UPDATE
				SET value = EXCLUDED.value,
				    revision = EXCLUDED.revision,
				    schema_version = EXCLUDED.schema_version,
				    updated_by = EXCLUDED.updated_by,
				    updated_at = now()`
		if _, err := s.db.Q(ctx).Exec(ctx, upsert,
			p.OrganizationID, key, payload, revision, SchemaVersion, p.UserID); err != nil {
			return fmt.Errorf("store tenant setting %q: %w", key, err)
		}

		const insertRev = `
			INSERT INTO tenant_setting_revisions
				(organization_id, key, revision, value, reset_to_default, schema_version, previous_value, changed_by)
			VALUES ($1,$2,$3,$4,false,$5,$6,$7)`
		if _, err := s.db.Q(ctx).Exec(ctx, insertRev,
			p.OrganizationID, key, revision, payload, SchemaVersion, previous, p.UserID); err != nil {
			return fmt.Errorf("record revision for %q: %w", key, err)
		}

		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionTenantSettingUpdated),
			EntityType:     "tenant_setting",
			EntityID:       key,
			Before:         jsonOrNil(previous),
			After:          map[string]any{"value": normalised},
			Metadata:       map[string]any{"revision": revision},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p.OrganizationID, key)
}

// Reset removes an override so the catalog default applies again.
//
// It is a write, not a delete: the revision row records that the default was
// restored, with the value that was removed as its previous value. A reset that
// left no trace would make "why did this change?" unanswerable, which is the
// question the history exists to answer.
func (s *Service) Reset(ctx context.Context, p *rbac.Principal, key string) (*Setting, error) {
	d, ok := Lookup(key)
	if !ok {
		return nil, ErrNotFound
	}
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockKey(ctx, p.OrganizationID, key); err != nil {
			return err
		}
		previous, revision, err := s.currentRevision(ctx, p.OrganizationID, key)
		if err != nil {
			return err
		}
		if _, err := s.db.Q(ctx).Exec(ctx,
			`DELETE FROM tenant_settings WHERE organization_id = $1 AND key = $2`,
			p.OrganizationID, key); err != nil {
			return fmt.Errorf("remove tenant setting %q: %w", key, err)
		}
		const insertRev = `
			INSERT INTO tenant_setting_revisions
				(organization_id, key, revision, value, reset_to_default, schema_version, previous_value, changed_by)
			VALUES ($1,$2,$3,NULL,true,$4,$5,$6)`
		if _, err := s.db.Q(ctx).Exec(ctx, insertRev,
			p.OrganizationID, key, revision, SchemaVersion, previous, p.UserID); err != nil {
			return fmt.Errorf("record reset of %q: %w", key, err)
		}
		return s.audit.Record(ctx, audit.Entry{
			OrganizationID: &p.OrganizationID,
			ActorUserID:    &p.UserID,
			Action:         string(audit.ActionTenantSettingReset),
			EntityType:     "tenant_setting",
			EntityID:       key,
			Before:         jsonOrNil(previous),
			After:          map[string]any{"value": d.Default, "reset_to_default": true},
			Metadata:       map[string]any{"revision": revision},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p.OrganizationID, key)
}

// History returns a key's revisions, newest first.
func (s *Service) History(ctx context.Context, orgID uuid.UUID, key string, limit int) ([]Revision, error) {
	if _, ok := Lookup(key); !ok {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, organization_id, key, revision, value, reset_to_default,
		       previous_value, schema_version, changed_by, created_at
		FROM tenant_setting_revisions
		WHERE organization_id = $1 AND key = $2
		ORDER BY revision DESC
		LIMIT $3`
	rows, err := s.db.Q(ctx).Query(ctx, q, orgID, key, limit)
	if err != nil {
		return nil, fmt.Errorf("read revisions for %q: %w", key, err)
	}
	defer rows.Close()

	out := []Revision{}
	for rows.Next() {
		var (
			r        Revision
			value    []byte
			previous []byte
		)
		if err := rows.Scan(&r.ID, &r.OrganizationID, &r.Key, &r.Revision, &value, &r.ResetToDefault,
			&previous, &r.SchemaVersion, &r.ChangedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		if len(value) > 0 {
			if v, err := decodeStored(value); err == nil {
				r.Value = v
			}
		}
		if len(previous) > 0 {
			if v, err := decodeStored(previous); err == nil {
				r.PreviousValue = v
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// lockKey takes a transaction-scoped advisory lock for one organization and key.
//
// Two administrators saving the same key at once would otherwise both read the
// same current revision and both write revision N+1; the unique constraint would
// turn that into a failed request for one of them, which is better than silent
// history loss but worse than simply serialising the two writes. The lock is
// released when the transaction ends, so it cannot be leaked by a panic.
func (s *Service) lockKey(ctx context.Context, orgID uuid.UUID, key string) error {
	const q = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`
	if _, err := s.db.Q(ctx).Exec(ctx, q, orgID.String()+"/"+key); err != nil {
		return fmt.Errorf("lock tenant setting %q: %w", key, err)
	}
	return nil
}

// currentRevision returns the value currently stored and the revision number the
// next write should carry. It returns (nil, 1) when nothing is stored, so the
// first write is revision 1 with no previous value.
func (s *Service) currentRevision(ctx context.Context, orgID uuid.UUID, key string) ([]byte, int, error) {
	var raw []byte
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT value FROM tenant_settings WHERE organization_id = $1 AND key = $2`,
		orgID, key).Scan(&raw)
	if err != nil {
		raw = nil
	}
	// The revision counter comes from the history, not the current row, so a reset
	// (which deletes the current row) does not restart the numbering.
	var maxRev int
	if err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT COALESCE(max(revision), 0) FROM tenant_setting_revisions
		 WHERE organization_id = $1 AND key = $2`,
		orgID, key).Scan(&maxRev); err != nil {
		return nil, 0, fmt.Errorf("read revision counter for %q: %w", key, err)
	}
	return raw, maxRev + 1, nil
}

// decodeStored turns a stored jsonb value into a Go value.
func decodeStored(raw []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// jsonOrNil returns raw as a JSON value, or nil when it is empty. The audit logger
// marshals its Before field, and a nil []byte would be written as a base64 string
// rather than null.
func jsonOrNil(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	v, err := decodeStored(raw)
	if err != nil {
		return nil
	}
	return v
}
