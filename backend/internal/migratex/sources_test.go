package migratex

import "testing"

// TestImplementedSourcesMatchesImplemented is the guard that keeps the advertised
// vocabulary and the buildable set from drifting.
//
// migration_sources is what a client offers in the "source type" picker. If a kind
// appears there but BuildSource refuses it, the user fills in a form and gets a
// "not implemented" error at submit — the same class of bug as a webhook picker
// offering names the dispatcher does not match. A kind must be advertised exactly
// when Implemented reports true.
func TestImplementedSourcesMatchesImplemented(t *testing.T) {
	advertised := ImplementedSources()
	if len(advertised) == 0 {
		t.Fatal("ImplementedSources is empty; the migration picker would offer nothing")
	}
	seen := map[SourceKind]bool{}
	for _, s := range advertised {
		k := SourceKind(s)
		if !Implemented(k) {
			t.Errorf("%q is advertised but Implemented reports false", s)
		}
		if seen[k] {
			t.Errorf("%q is advertised twice", s)
		}
		seen[k] = true
	}

	// Every implemented kind must be advertised, or a working source would be
	// hidden from the picker.
	for _, k := range []SourceKind{SourcePostgres, SourceMySQL, SourceSQLite, SourceREST} {
		if !seen[k] {
			t.Errorf("%q is implemented but not advertised", k)
		}
	}
}

// TestMySQLIsImplementedAndValidated pins the direction this cycle added: MySQL is
// buildable and its configuration is validated the same way PostgreSQL's is, so a
// MySQL source is a first-class kind rather than a recognised-but-refused value.
func TestMySQLIsImplementedAndValidated(t *testing.T) {
	if !Implemented(SourceMySQL) {
		t.Fatal("SourceMySQL is not implemented")
	}

	// A MySQL config with no host or database is refused at validation, before any
	// connection is attempted.
	cfg := ConnectionConfig{Kind: SourceMySQL}
	if err := cfg.Validate(); err == nil {
		t.Error("a MySQL config with no host or database validated")
	}
	cfg = ConnectionConfig{Kind: SourceMySQL, Host: "db.internal", Database: "crm"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("a complete MySQL config was refused: %v", err)
	}
}
