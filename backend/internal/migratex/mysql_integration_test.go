//go:build integration

// Integration tests exercise the MySQL/MariaDB migration source against a real
// server. They are excluded from the default build because they need one; run them
// with
//
//	FORGECRM_TEST_MYSQL_URL='mysql://user:pass@host:3306/dbname' \
//	  go test -tags=integration ./internal/migratex/...
//
// They deliberately do not use a mock: the properties under test — that the
// session is genuinely read-only, that information_schema discovery finds the real
// tables, and that a batch read returns the rows — are statements about the
// server's behaviour, so a mock would prove nothing.
package migratex

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"testing"
)

// mysqlTestConfig parses FORGECRM_TEST_MYSQL_URL, or skips the test when it is
// unset so the default `go test ./...` stays green.
func mysqlTestConfig(t *testing.T) (ConnectionConfig, string) {
	t.Helper()
	raw := os.Getenv("FORGECRM_TEST_MYSQL_URL")
	if raw == "" {
		t.Skip("FORGECRM_TEST_MYSQL_URL is not set; skipping MySQL integration test")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse FORGECRM_TEST_MYSQL_URL: %v", err)
	}
	password, _ := u.User.Password()
	port := 3306
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("parse port: %v", err)
		}
	}
	return ConnectionConfig{
		Kind:     SourceMySQL,
		Host:     u.Hostname(),
		Port:     port,
		Database: u.Path[1:], // strip the leading slash
		Username: u.User.Username(),
	}, password
}

// TestMySQLSourceIsReadOnlyAndReadsRealData is the central correctness check for
// the MySQL adapter: the connection is genuinely read-only, discovery finds the
// real schema, and a batch read returns the rows the server holds.
func TestMySQLSourceIsReadOnlyAndReadsRealData(t *testing.T) {
	cfg, password := mysqlTestConfig(t)
	ctx := context.Background()

	src, err := NewMySQLSource(ctx, cfg, password)
	if err != nil {
		t.Fatalf("open MySQL source: %v", err)
	}
	defer src.Close()

	if src.Kind() != SourceMySQL {
		t.Fatalf("Kind() = %q, want %q", src.Kind(), SourceMySQL)
	}
	if err := src.TestConnection(ctx); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	// A write must be refused by the server, not merely discouraged. The read-only
	// system variable is what makes that true; if it were not set, this CREATE would
	// succeed and the test would fail on the absence of an error.
	db := src.(*mysqlSource).db
	if _, err := db.ExecContext(ctx, "CREATE TABLE forgecrm_readonly_probe (id int)"); err == nil {
		t.Fatal("a write succeeded on the source session; it is not read-only")
	}

	tables, err := src.DiscoverSchema(ctx)
	if err != nil {
		t.Fatalf("DiscoverSchema: %v", err)
	}
	var found *TableSchema
	for i := range tables {
		if tables[i].Name == "widgets" {
			found = &tables[i]
		}
	}
	if found == nil {
		t.Fatalf("DiscoverSchema did not find the widgets table; got %d tables", len(tables))
	}
	if len(found.Columns) == 0 {
		t.Fatal("the discovered widgets table has no columns")
	}
	var sawPK bool
	for _, c := range found.Columns {
		if c.Name == "id" && c.PrimaryKey {
			sawPK = true
		}
	}
	if !sawPK {
		t.Error("the primary key column of widgets was not reported")
	}

	n, err := src.CountRows(ctx, "widgets")
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if n != 3 {
		t.Fatalf("CountRows = %d, want 3", n)
	}

	cols := make([]string, 0, len(found.Columns))
	for _, c := range found.Columns {
		cols = append(cols, c.Name)
	}
	rows, err := src.ReadBatch(ctx, "widgets", cols, map[string]any{"column": "id"}, 2)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ReadBatch returned %d rows, want 2", len(rows))
	}
	// A DATETIME column is normalised to RFC3339, the same rendering the other
	// sources produce, so a downstream consumer sees one format.
	if _, ok := rows[0]["created_at"].(string); !ok {
		t.Errorf("created_at was not normalised to a string: %T", rows[0]["created_at"])
	}
}

// TestMySQLIdentifierQuotingRejectsInjection proves the one place an identifier
// enters SQL cannot be used to smuggle a statement. A backtick or a semicolon in a
// name is refused before any query is built.
func TestMySQLIdentifierQuotingRejectsInjection(t *testing.T) {
	for _, bad := range []string{"", "widgets; DROP TABLE users", "wid`gets", "widgets--", "widgets/*x*/"} {
		if _, err := mysqlQuoteIdentifier(bad); err == nil {
			t.Errorf("mysqlQuoteIdentifier accepted %q, which must be refused", bad)
		}
	}
	got, err := mysqlQuoteIdentifier("widgets")
	if err != nil {
		t.Fatalf("mysqlQuoteIdentifier refused a plain name: %v", err)
	}
	if got != "`widgets`" {
		t.Errorf("mysqlQuoteIdentifier(%q) = %q, want `widgets`", "widgets", got)
	}
}
