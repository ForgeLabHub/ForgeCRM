package importx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/importx/parsers"
)

// ---------------------------------------------------------------------------
// Field specifications
// ---------------------------------------------------------------------------

// fieldKind describes how a mapped value is coerced.
type fieldKind int

const (
	kindText fieldKind = iota
	kindNumber
	kindInt
	kindBool
	kindDate
	kindUUID
)

// fieldSpec describes one target field of an entity.
type fieldSpec struct {
	Name     string
	Kind     fieldKind
	Required bool
	// Default, when non-empty, is used if the mapped value is absent or blank.
	Default string
}

// entitySpec is the importable projection of an entity.
type entitySpec struct {
	Table  string
	Fields []fieldSpec
	// NaturalKeys are candidate deduplication key sets, in priority order. The
	// first set whose columns are all mapped is used.
	NaturalKeys [][]string
}

// specs is the allowlist of importable entities and their writable columns.
//
// This table is the only place that decides which columns an import can write.
// Nothing derived from user input is ever interpolated as an identifier, so a
// malicious mapping cannot reach a column that is not listed here.
var specs = map[string]entitySpec{
	"account": {
		Table: "accounts",
		Fields: []fieldSpec{
			{Name: "name", Kind: kindText, Required: true},
			{Name: "legal_name", Kind: kindText},
			{Name: "industry", Kind: kindText},
			{Name: "website", Kind: kindText},
			{Name: "phone", Kind: kindText},
			{Name: "email", Kind: kindText},
			{Name: "tax_id", Kind: kindText},
			{Name: "address_line1", Kind: kindText},
			{Name: "city", Kind: kindText},
			{Name: "region", Kind: kindText},
			{Name: "postal_code", Kind: kindText},
			{Name: "country", Kind: kindText},
			{Name: "annual_revenue", Kind: kindNumber},
			{Name: "employee_count", Kind: kindInt},
			{Name: "owner_user_id", Kind: kindUUID},
			{Name: "team_id", Kind: kindUUID},
		},
		NaturalKeys: [][]string{{"email"}, {"name"}},
	},
	"contact": {
		Table: "contacts",
		Fields: []fieldSpec{
			{Name: "first_name", Kind: kindText},
			{Name: "last_name", Kind: kindText},
			{Name: "email", Kind: kindText},
			{Name: "phone", Kind: kindText},
			{Name: "mobile", Kind: kindText},
			{Name: "job_title", Kind: kindText},
			{Name: "department", Kind: kindText},
			{Name: "account_id", Kind: kindUUID},
			{Name: "is_primary", Kind: kindBool},
			{Name: "owner_user_id", Kind: kindUUID},
			{Name: "team_id", Kind: kindUUID},
		},
		NaturalKeys: [][]string{{"email"}, {"first_name", "last_name"}},
	},
	"lead": {
		Table: "leads",
		Fields: []fieldSpec{
			{Name: "title", Kind: kindText},
			{Name: "first_name", Kind: kindText},
			{Name: "last_name", Kind: kindText},
			{Name: "email", Kind: kindText},
			{Name: "phone", Kind: kindText},
			{Name: "company", Kind: kindText},
			{Name: "source", Kind: kindText},
			{Name: "status", Kind: kindText, Default: "new"},
			{Name: "score", Kind: kindInt},
			{Name: "notes", Kind: kindText},
			{Name: "owner_user_id", Kind: kindUUID},
			{Name: "team_id", Kind: kindUUID},
		},
		NaturalKeys: [][]string{{"email"}, {"company", "last_name"}},
	},
	"deal": {
		Table: "deals",
		Fields: []fieldSpec{
			{Name: "title", Kind: kindText, Required: true},
			{Name: "description", Kind: kindText},
			{Name: "amount", Kind: kindNumber},
			{Name: "currency", Kind: kindText, Default: "USD"},
			{Name: "discount_percent", Kind: kindNumber},
			{Name: "expected_close_date", Kind: kindDate},
			{Name: "account_id", Kind: kindUUID},
			{Name: "primary_contact_id", Kind: kindUUID},
			{Name: "owner_user_id", Kind: kindUUID},
			{Name: "team_id", Kind: kindUUID},
		},
		NaturalKeys: [][]string{{"title", "account_id"}},
	},
	"task": {
		Table: "tasks",
		Fields: []fieldSpec{
			{Name: "title", Kind: kindText, Required: true},
			{Name: "description", Kind: kindText},
			{Name: "status", Kind: kindText, Default: "open"},
			{Name: "priority", Kind: kindText, Default: "normal"},
			{Name: "due_at", Kind: kindDate},
			{Name: "assignee_user_id", Kind: kindUUID},
			{Name: "team_id", Kind: kindUUID},
			{Name: "account_id", Kind: kindUUID},
			{Name: "contact_id", Kind: kindUUID},
			{Name: "deal_id", Kind: kindUUID},
		},
		NaturalKeys: [][]string{{"title", "due_at"}},
	},
	"ticket": {
		Table: "tickets",
		Fields: []fieldSpec{
			{Name: "subject", Kind: kindText, Required: true},
			{Name: "description", Kind: kindText},
			{Name: "status", Kind: kindText, Default: "new"},
			{Name: "priority", Kind: kindText, Default: "normal"},
			{Name: "channel", Kind: kindText, Default: "api"},
			{Name: "account_id", Kind: kindUUID},
			{Name: "contact_id", Kind: kindUUID},
			{Name: "assignee_user_id", Kind: kindUUID},
			{Name: "team_id", Kind: kindUUID},
		},
		NaturalKeys: [][]string{{"subject"}},
	},
	"activity": {
		Table: "activities",
		Fields: []fieldSpec{
			{Name: "type", Kind: kindText, Default: "note"},
			{Name: "subject", Kind: kindText},
			{Name: "body", Kind: kindText},
			{Name: "occurred_at", Kind: kindDate},
			{Name: "duration_minutes", Kind: kindInt},
			{Name: "outcome", Kind: kindText},
			{Name: "account_id", Kind: kindUUID},
			{Name: "contact_id", Kind: kindUUID},
			{Name: "deal_id", Kind: kindUUID},
		},
		NaturalKeys: nil,
	},
}

// FieldList returns the importable field names for an entity, for the wizard UI.
func FieldList(entityType string) []string {
	spec, ok := specs[entityType]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(spec.Fields))
	for _, f := range spec.Fields {
		out = append(out, f.Name)
	}
	return out
}

// ---------------------------------------------------------------------------
// Mapping and coercion
// ---------------------------------------------------------------------------

// coerce converts a raw source value to the type the target column expects.
func coerce(v any, kind fieldKind) (any, error) {
	if v == nil {
		return nil, nil
	}
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case float64:
		s = trimFloat(t)
	case bool:
		if kind == kindBool {
			return t, nil
		}
		s = fmt.Sprint(t)
	default:
		s = fmt.Sprint(v)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	switch kind {
	case kindText:
		return s, nil
	case kindNumber:
		f, ok := parsers.ParseNumber(s)
		if !ok {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return f, nil
	case kindInt:
		if f, ok := parsers.ParseNumber(s); ok {
			return int(f), nil
		}
		return nil, fmt.Errorf("%q is not an integer", s)
	case kindBool:
		b, ok := parsers.ParseBool(s)
		if !ok {
			return nil, fmt.Errorf("%q is not a boolean", s)
		}
		return b, nil
	case kindDate:
		if t, ok := parsers.ParseDate(s); ok {
			return t, nil
		}
		return nil, fmt.Errorf("%q is not a recognised date", s)
	case kindUUID:
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a UUID", s)
		}
		return id, nil
	}
	return s, nil
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%f", f)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

// buildValues applies the mapping to one source row, producing target values.
// It returns the values and any per-field problems.
func buildValues(spec entitySpec, mapping []MappingEntry, row parsers.Row) (map[string]any, map[string]string) {
	values := map[string]any{}
	problems := map[string]string{}

	byName := map[string]fieldSpec{}
	for _, f := range spec.Fields {
		byName[f.Name] = f
	}

	for _, m := range mapping {
		fs, ok := byName[m.TargetField]
		if !ok {
			problems[m.TargetField] = "not an importable field for this entity"
			continue
		}
		raw, present := row[m.SourceColumn]
		if !present {
			continue
		}
		// An explicit transform can pre-process the text.
		if m.Transform != "" {
			raw = applyTransform(raw, m.Transform)
		}
		val, err := coerce(raw, fs.Kind)
		if err != nil {
			problems[m.TargetField] = err.Error()
			continue
		}
		if val != nil {
			values[m.TargetField] = val
		}
	}

	for _, f := range spec.Fields {
		if _, ok := values[f.Name]; ok {
			continue
		}
		if f.Default != "" {
			if v, err := coerce(f.Default, f.Kind); err == nil && v != nil {
				values[f.Name] = v
			}
		}
	}
	return values, problems
}

func applyTransform(v any, transform string) any {
	if v == nil {
		return nil
	}
	s := fmt.Sprint(v)
	switch strings.ToLower(transform) {
	case "trim":
		return strings.TrimSpace(s)
	case "upper":
		return strings.ToUpper(strings.TrimSpace(s))
	case "lower":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return v
	}
}

// validateAgainstEntityInput reuses the domain validation rules so that an
// imported row obeys exactly the same constraints as an API-created one.
func validateAgainstEntityInput(entityType string, values map[string]any) error {
	enc, err := json.Marshal(values)
	if err != nil {
		return err
	}
	switch entityType {
	case "account":
		var in crm.AccountInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	case "contact":
		var in crm.ContactInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	case "lead":
		var in crm.LeadInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	case "deal":
		var in crm.DealInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	case "task":
		var in crm.TaskInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	case "ticket":
		var in crm.TicketInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	case "activity":
		var in crm.ActivityInput
		if err := json.Unmarshal(enc, &in); err != nil {
			return err
		}
		return in.Validate()
	}
	return fmt.Errorf("unsupported entity type %q", entityType)
}

// ---------------------------------------------------------------------------
// Writing
// ---------------------------------------------------------------------------

// writeOutcome describes what happened to one row.
type writeOutcome int

const (
	outcomeCreated writeOutcome = iota
	outcomeUpdated
	outcomeSkipped
	outcomeFailed
)

// findExisting locates a duplicate using the natural keys or the external id.
func (s *Service) findExisting(ctx context.Context, orgID uuid.UUID, spec entitySpec,
	opts Options, values map[string]any) (*uuid.UUID, error) {

	// An explicit external identifier always wins, because it is the only key the
	// source system itself guarantees.
	if opts.ExternalSource != "" {
		extID, _ := values["external_id"].(string)
		if extID == "" {
			if v, ok := values["external_id"]; ok && v != nil {
				extID = fmt.Sprint(v)
			}
		}
		if extID != "" {
			q := fmt.Sprintf(`SELECT id FROM %s WHERE organization_id = $1 AND external_source = $2 AND external_id = $3`,
				spec.Table)
			var id uuid.UUID
			err := s.db.Q(ctx).QueryRow(ctx, q, orgID, opts.ExternalSource, extID).Scan(&id)
			if err == nil {
				return &id, nil
			}
			if err != pgx.ErrNoRows {
				return nil, err
			}
			return nil, nil
		}
	}

	keys := opts.DuplicateKey
	if len(keys) == 0 {
		for _, candidate := range spec.NaturalKeys {
			allPresent := true
			for _, k := range candidate {
				if _, ok := values[k]; !ok {
					allPresent = false
					break
				}
			}
			if allPresent {
				keys = candidate
				break
			}
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}

	// Every key column is checked against the field allowlist, so the identifiers
	// in the statement below can never come from unmapped input.
	allowed := map[string]fieldSpec{}
	for _, f := range spec.Fields {
		allowed[f.Name] = f
	}
	conds := make([]string, 0, len(keys))
	args := []any{orgID}
	for i, k := range keys {
		if _, ok := allowed[k]; !ok {
			return nil, httpx.ValidationError(map[string]string{
				"duplicate_key": fmt.Sprintf("%q is not an importable field for this entity", k)})
		}
		v, ok := values[k]
		if !ok {
			return nil, nil
		}
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("%s = $%d", k, i+2))
	}
	q := fmt.Sprintf(`SELECT id FROM %s WHERE organization_id = $1 AND %s LIMIT 1`,
		spec.Table, strings.Join(conds, " AND "))
	var id uuid.UUID
	err := s.db.Q(ctx).QueryRow(ctx, q, args...).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// writeRow inserts or updates one row and returns the outcome.
func (s *Service) writeRow(ctx context.Context, orgID, actorID uuid.UUID, entityType string,
	spec entitySpec, opts Options, values map[string]any) (writeOutcome, error) {

	// Attach the external identifier when the source provides one.
	if opts.ExternalSource != "" {
		if v, ok := values["external_id"]; ok && v != nil && fmt.Sprint(v) != "" {
			values["external_source"] = opts.ExternalSource
		}
	}
	if opts.DefaultOwnerID != nil {
		if _, ok := values["owner_user_id"]; !ok {
			values["owner_user_id"] = *opts.DefaultOwnerID
		}
	}

	existing, err := s.findExisting(ctx, orgID, spec, opts, values)
	if err != nil {
		return outcomeFailed, err
	}
	if existing != nil {
		switch opts.Duplicates {
		case DuplicatesSkip:
			return outcomeSkipped, nil
		case DuplicatesFail:
			return outcomeFailed, fmt.Errorf("a matching record already exists")
		case DuplicatesUpdate:
			return s.updateRow(ctx, orgID, spec, *existing, values)
		case DuplicatesCreate, "":
			// Fall through to an insert; the unique index will reject a true
			// duplicate of the external identifier, which is reported as an error.
		default:
			return outcomeFailed, fmt.Errorf("unknown duplicate strategy %q", opts.Duplicates)
		}
	}

	cols := []string{"organization_id"}
	placeholders := []string{"$1"}
	args := []any{orgID}
	for _, f := range spec.Fields {
		v, ok := values[f.Name]
		if !ok {
			continue
		}
		args = append(args, v)
		cols = append(cols, f.Name)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	if v, ok := values["external_source"]; ok {
		args = append(args, v)
		cols = append(cols, "external_source")
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	if v, ok := values["external_id"]; ok {
		args = append(args, v)
		cols = append(cols, "external_id")
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	args = append(args, actorID)
	cols = append(cols, "created_by")
	placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))

	// Deals require a pipeline and stage. When the import does not supply them,
	// the configured defaults are used, and their absence is a configuration
	// error rather than a silent data problem.
	if entityType == "deal" {
		if _, ok := values["pipeline_id"]; !ok {
			if opts.DefaultPipelineID == nil || opts.DefaultStageID == nil {
				return outcomeFailed, fmt.Errorf("importing deals requires default_pipeline_id and default_stage_id in the import options")
			}
			args = append(args, *opts.DefaultPipelineID)
			cols = append(cols, "pipeline_id")
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
			args = append(args, *opts.DefaultStageID)
			cols = append(cols, "stage_id")
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
	}

	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) RETURNING id`,
		spec.Table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	var newID uuid.UUID
	if err := s.db.Q(ctx).QueryRow(ctx, q, args...).Scan(&newID); err != nil {
		return outcomeFailed, describeWriteError(err)
	}
	if entityType == "deal" {
		const hist = `
			INSERT INTO deal_stage_history (deal_id, organization_id, from_stage_id, to_stage_id, changed_by, note)
			VALUES ($1,$2,NULL,$3,$4,'imported')`
		if _, err := s.db.Q(ctx).Exec(ctx, hist, newID, orgID, *opts.DefaultStageID, actorID); err != nil {
			return outcomeFailed, fmt.Errorf("record imported deal stage: %w", err)
		}
	}
	return outcomeCreated, nil
}

func (s *Service) updateRow(ctx context.Context, orgID uuid.UUID, spec entitySpec,
	id uuid.UUID, values map[string]any) (writeOutcome, error) {

	assignments := []string{}
	args := []any{id, orgID}
	for _, f := range spec.Fields {
		v, ok := values[f.Name]
		if !ok {
			continue
		}
		args = append(args, v)
		assignments = append(assignments, fmt.Sprintf("%s = $%d", f.Name, len(args)))
	}
	if v, ok := values["external_source"]; ok {
		args = append(args, v)
		assignments = append(assignments, fmt.Sprintf("external_source = $%d", len(args)))
	}
	if v, ok := values["external_id"]; ok {
		args = append(args, v)
		assignments = append(assignments, fmt.Sprintf("external_id = $%d", len(args)))
	}
	if len(assignments) == 0 {
		return outcomeSkipped, nil
	}
	q := fmt.Sprintf(`UPDATE %s SET %s WHERE id = $1 AND organization_id = $2`,
		spec.Table, strings.Join(assignments, ", "))
	tag, err := s.db.Q(ctx).Exec(ctx, q, args...)
	if err != nil {
		return outcomeFailed, describeWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return outcomeFailed, fmt.Errorf("record disappeared during import")
	}
	return outcomeUpdated, nil
}

// describeWriteError turns a database constraint violation into a message an
// operator can act on, without leaking schema internals.
func describeWriteError(err error) error {
	switch {
	case database.IsUniqueViolation(err):
		return fmt.Errorf("a record with the same unique values already exists")
	case database.IsForeignKeyViolation(err):
		return fmt.Errorf("a referenced record does not exist in this organization")
	case database.IsCheckViolation(err):
		return fmt.Errorf("a value violates a database constraint (for example a status outside the allowed set)")
	default:
		return err
	}
}

// ---------------------------------------------------------------------------
// Row-by-row processing
// ---------------------------------------------------------------------------

// processRows runs the pipeline over a parsed result, calling write for each row.
type rowProcessor struct {
	svc      *Service
	orgID    uuid.UUID
	actorID  uuid.UUID
	entity   string
	spec     entitySpec
	opts     Options
	mapping  []MappingEntry
	onResult func(rowNum int64, outcome writeOutcome, err error)
}

// run processes every row, isolating failures so one bad row cannot abort the
// import.
func (p *rowProcessor) run(ctx context.Context, res *parsers.Result, startRow int64) error {
	for i, row := range res.Rows {
		rowNum := startRow + int64(i) + 1
		values, problems := buildValues(p.spec, p.mapping, row)
		if len(problems) > 0 {
			p.onResult(rowNum, outcomeFailed, validationProblems(problems))
			continue
		}
		if err := validateAgainstEntityInput(p.entity, values); err != nil {
			p.onResult(rowNum, outcomeFailed, err)
			continue
		}
		outcome, err := p.svc.writeRow(ctx, p.orgID, p.actorID, p.entity, p.spec, p.opts, values)
		p.onResult(rowNum, outcome, err)
	}
	return nil
}

func validationProblems(problems map[string]string) error {
	parts := make([]string, 0, len(problems))
	for k, v := range problems {
		parts = append(parts, k+": "+v)
	}
	sortStrings(parts)
	return fmt.Errorf("field conversion failed: %s", strings.Join(parts, "; "))
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

// Report summarises an import run.
type Report struct {
	TotalRows    int64     `json:"total_rows"`
	Imported     int64     `json:"imported"`
	Updated      int64     `json:"updated"`
	Skipped      int64     `json:"skipped"`
	Failed       int64     `json:"failed"`
	DryRun       bool      `json:"dry_run"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMS   int64     `json:"duration_ms"`
	RowsPerSec   float64   `json:"rows_per_second"`
	Checksum     string    `json:"checksum_sha256,omitempty"`
	Notes        []string  `json:"notes,omitempty"`
	ColumnCount  int       `json:"column_count"`
	MappedFields []string  `json:"mapped_fields"`
}

// RowError is one rejected row, retrievable from the API.
type RowError struct {
	RowNumber int64  `json:"row_number"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}
