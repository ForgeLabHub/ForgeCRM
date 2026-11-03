//go:build integration

package testsupport

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Migration 0030 adds trigram indexes for the measured ILIKE search paths. Those
// indexes are not decoration: without them each filtered list and the
// cross-entity search falls back to a sequential scan that reads the whole
// table. Nothing in the code fails when one is missing — the query still
// returns the right rows, just slowly — so a dropped index or a migration that
// never applied would go unnoticed until production traffic found it.
//
// This test reads the database catalog rather than an EXPLAIN plan. A plan is a
// property of the planner's cost model on the data present at that moment, so a
// plan-based assertion is flaky on a nearly empty table. The catalog is the
// durable statement: the index exists, it is on the right table and expression,
// it is GIN with the trigram operator class, and it is valid and ready to use.

// trgmIndex is one index the migration promises, keyed by name.
type trgmIndex struct {
	table string
	// expr is the indexed expression as the planner must see it, normalised:
	// lowercased, whitespace removed, and `::text` casts stripped. Casts are
	// dropped because PostgreSQL's own formatting of them moves between
	// versions, and the cast is already implied by the citext column type.
	expr string
}

// wantTrgmIndexes is the full set from 0030_perf_trgm_indexes.up.sql. The count
// is asserted too, so adding an index to the migration without listing it here
// fails the test rather than passing silently.
var wantTrgmIndexes = map[string]trgmIndex{
	"contacts_first_name_trgm_idx": {"contacts", "coalesce(first_name,'')"},
	"contacts_last_name_trgm_idx":  {"contacts", "coalesce(last_name,'')"},
	"contacts_email_trgm_idx":      {"contacts", "coalesce(email,'')"},
	"accounts_name_trgm_idx":       {"accounts", "name"},
	"accounts_email_trgm_idx":      {"accounts", "coalesce(email,'')"},
	"leads_title_trgm_idx":         {"leads", "title"},
	"leads_company_trgm_idx":       {"leads", "coalesce(company,'')"},
	"leads_email_trgm_idx":         {"leads", "coalesce(email,'')"},
	"deals_title_trgm_idx":         {"deals", "title"},
	"tickets_subject_trgm_idx":     {"tickets", "subject"},
	"tasks_title_trgm_idx":         {"tasks", "title"},
	"activities_subject_trgm_idx":  {"activities", "subject"},
	"activities_body_trgm_idx":     {"activities", "coalesce(body,'')"},
	"users_email_trgm_idx":         {"users", "email"},
	"users_display_name_trgm_idx":  {"users", "display_name"},
	"app_logs_message_trgm_idx":    {"app_logs", "message"},
}

var (
	spaceRe = regexp.MustCompile(`\s+`)
	castRe  = regexp.MustCompile(`::text`)
)

// normaliseExpr renders an index expression in the one form the test compares
// against, so a formatting change in pg_get_indexdef does not read as a
// regression.
func normaliseExpr(s string) string {
	s = strings.ToLower(s)
	s = castRe.ReplaceAllString(s, "")
	s = spaceRe.ReplaceAllString(s, "")
	// A bare cast expression is wrapped by PostgreSQL: `(email::text)` becomes
	// `(email)`. Strip one balanced layer so it compares equal to `email`.
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}
	return s
}

// TestTrigramIndexesArePresentAndUsable is the regression test for a missing or
// broken search index. It proves the extension, every index, its table,
// expression and operator class, and that the index is valid — an index left
// invalid by a failed CREATE INDEX CONCURRENTLY would pass a name-only check and
// still not be used.
func TestTrigramIndexesArePresentAndUsable(t *testing.T) {
	db := connectOrSkip(t)
	freshSchema(t, db)
	ctx := context.Background()

	var extVersion string
	err := db.Pool().QueryRow(ctx,
		`SELECT extversion FROM pg_extension WHERE extname = 'pg_trgm'`).Scan(&extVersion)
	if err != nil {
		t.Fatalf("pg_trgm is not installed after migrations: %v", err)
	}

	// One row per trigram index: name, table, access method, operator class,
	// indexed expression, and the validity flags.
	rows, err := db.Pool().Query(ctx, `
		SELECT i.relname,
		       t.relname,
		       am.amname,
		       opc.opcname,
		       pg_get_indexdef(ix.indexrelid, 1, true),
		       ix.indisvalid,
		       ix.indisready
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_am am ON am.oid = i.relam
		JOIN pg_opclass opc ON opc.oid = ix.indclass[0]
		WHERE i.relname LIKE '%_trgm_idx'`)
	if err != nil {
		t.Fatalf("read the index catalog: %v", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var name, table, am, opclass, expr string
		var valid, ready bool
		if err := rows.Scan(&name, &table, &am, &opclass, &expr, &valid, &ready); err != nil {
			t.Fatalf("scan an index row: %v", err)
		}

		want, listed := wantTrgmIndexes[name]
		if !listed {
			t.Errorf("index %q exists in the database but is not listed in the test; "+
				"add it here so its shape is checked too", name)
			continue
		}
		found[name] = true

		if table != want.table {
			t.Errorf("index %q is on table %q, want %q", name, table, want.table)
		}
		if am != "gin" {
			t.Errorf("index %q uses access method %q, want gin", name, am)
		}
		if opclass != "gin_trgm_ops" {
			t.Errorf("index %q uses operator class %q, want gin_trgm_ops", name, opclass)
		}
		if got := normaliseExpr(expr); got != want.expr {
			t.Errorf("index %q indexes %q, want %q", name, got, want.expr)
		}
		if !valid {
			t.Errorf("index %q is not valid; a failed build leaves it unusable", name)
		}
		if !ready {
			t.Errorf("index %q is not ready; inserts will not maintain it", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the index catalog: %v", err)
	}

	for name := range wantTrgmIndexes {
		if !found[name] {
			t.Errorf("index %q is missing; the ILIKE search path it serves is back to a "+
				"sequential scan", name)
		}
	}
	if len(found) != len(wantTrgmIndexes) {
		t.Errorf("found %d trigram indexes, want %d", len(found), len(wantTrgmIndexes))
	}
}
