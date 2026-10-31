package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeOnRealRepo(t *testing.T) {
	repo := findRepoRoot(t)
	rep, err := Compute(repo)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	f := rep.Facts
	// Cross-check generator output against independent, simple measurements of
	// the same sources. These are deliberately different code paths from the
	// generator internals (file counting vs AST walking).
	if want := countGoFiles(t, filepath.Join(repo, "backend"), ".go"); f.GoFiles+f.GoTestFiles != want {
		t.Errorf("Go files: facts=%d+%d, recount=%d", f.GoFiles, f.GoTestFiles, want)
	}
	if got, want := f.MigrationsUp, countFiles(t, filepath.Join(repo, "backend/internal/migrations"), ".up.sql"); got != want {
		t.Errorf("migrations up: got %d want %d", got, want)
	}
	if got, want := f.MigrationsDown, countFiles(t, filepath.Join(repo, "backend/internal/migrations"), ".down.sql"); got != want {
		t.Errorf("migrations down: got %d want %d", got, want)
	}
	if f.RbacConsts != f.RbacCatalog {
		t.Errorf("rbac consts (%d) != catalog (%d)", f.RbacConsts, f.RbacCatalog)
	}
	if !rep.Consistency.RbacConstsMatchCatalog || !rep.Consistency.RbacSeededMatchCatalog {
		t.Errorf("consistency flags false on a healthy tree: %+v", rep.Consistency)
	}
	if f.OpenAPIPaths == 0 || f.OpenAPIOperations < f.OpenAPIPaths {
		t.Errorf("openapi implausible: paths=%d ops=%d", f.OpenAPIPaths, f.OpenAPIOperations)
	}
	if f.CIJobs == 0 || len(f.CIJobNames) != f.CIJobs {
		t.Errorf("ci jobs mismatch: %d vs %d", f.CIJobs, len(f.CIJobNames))
	}
	if !isSortedStrings(f.CIJobNames) {
		t.Error("CIJobNames not sorted")
	}
}

func TestDeterministicRendering(t *testing.T) {
	repo := findRepoRoot(t)
	r1, err := Compute(repo)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Compute(repo)
	if err != nil {
		t.Fatal(err)
	}
	a := RenderMarkdown(r1, "deadbeef")
	b := RenderMarkdown(r2, "deadbeef")
	if a != b {
		t.Error("rendering is not deterministic")
	}
	if !strings.Contains(a, "**"+itoa(r1.Facts.RbacConsts)+"**") {
		t.Error("rendered document missing RBAC metric value")
	}
}

func TestMetricsBlockMatch(t *testing.T) {
	repo := findRepoRoot(t)
	rep, err := Compute(repo)
	if err != nil {
		t.Fatal(err)
	}
	block := RenderMarkdown(rep, "abc123")
	doc := "# заголовок\n\nвступление человека\n\n" + autoBegin + "\n\n" + block + "\n" + autoEnd + "\n\nэпилог\n"
	if !MetricsBlockMatch(doc, block) {
		t.Error("freshly generated doc must match itself")
	}
	// Tamper with a metric value inside the committed document: the check must fail.
	i := strings.Index(doc, "**")
	j := strings.Index(doc[i+2:], "**")
	tampered := doc[:i] + "**999999**" + doc[i+2+j+2:]
	if MetricsBlockMatch(tampered, block) {
		t.Error("tampered metric value must fail -check")
	}
	wrongSHA := RenderMarkdown(rep, "ffffffff")
	if MetricsBlockMatch(doc, wrongSHA) {
		t.Error("commit stamp inside the block must be part of the comparison")
	}
	if MetricsBlockMatch("plain text", block) {
		t.Error("missing markers must fail the check")
	}
}

func TestExtractSeededPermissionsIgnoresNonPermissionStrings(t *testing.T) {
	text := `INSERT INTO permissions (code, description) VALUES
		('contacts.read', 'read contacts'),
		('contacts.read', 'dup');
		SELECT 'not.a.permission.inside.other.statement';`
	got := extractSeededPermissions(text)
	want := []string{"contacts.read"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestCountSystemRolesUsesColumnPosition(t *testing.T) {
	text := `INSERT INTO roles (name, description, is_system) VALUES
		('owner', 'x', true),
		('admin', 'true story', true),
		('guest', 'y', false);`
	if n := countSystemRoles(text); n != 2 {
		t.Errorf("got %d want 2", n)
	}
	// Reordered columns: is_system moves to position 0 and values follow it.
	reordered := `INSERT INTO roles (is_system, name, description) VALUES
		(true, 'owner', 'x'),
		(true, 'admin', 'true story'),
		(false, 'guest', 'y');`
	if n := countSystemRoles(reordered); n != 2 {
		t.Errorf("reordered: got %d want 2", n)
	}
}

func TestScanCISectionTracking(t *testing.T) {
	dir := t.TempDir()
	body := "name: x\non:\n  push:\n    branches: [main]\n  pull_request:\njobs:\n  alpha:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n  beta:\n    runs-on: ubuntu-latest\n"
	if err := os.WriteFile(filepath.Join(dir, "w.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	n, jobs, err := scanCI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("workflows=%d", n)
	}
	if strings.Join(jobs, ",") != "w.yml/alpha,w.yml/beta" {
		t.Errorf("jobs=%v — trigger keys under on: must not be counted", jobs)
	}
}

func TestParseRBACRejectsUnknownCatalogKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rbac.go")
	src := `package rbac

type Permission string

const PermA Permission = "a.b"

var catalog = map[Permission]struct{}{
	PermMissing: {},
}
`
	os.WriteFile(p, []byte(src), 0o644)
	if _, _, err := parseRBAC(p); err == nil || !strings.Contains(err.Error(), "no constant") {
		t.Errorf("expected unknown-key error, got %v", err)
	}
}

// --- helpers ------------------------------------------------------------------

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Normal layout: tests run in backend/cmd/facts-gen, repo root is three
	// levels up. Sandbox layout (flat module outside the repo): an explicit
	// FACTS_REPO override or the working directory itself may hold the marker.
	candidates := []string{
		os.Getenv("FACTS_REPO"),
		filepath.Clean(filepath.Join(wd, "..", "..", "..")),
		wd,
	}
	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(cand, "docs/api/openapi.json")); err == nil {
			return cand
		}
	}
	t.Fatalf("no repository root with docs/api/openapi.json found from %s", wd)
	return ""
}

func countGoFiles(t *testing.T, dir, suffix string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, suffix) {
			n++
		}
		return nil
	})
	return n
}

func countFiles(t *testing.T, dir, suffix string) int {
	return countGoFiles(t, dir, suffix)
}

func isSortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	// tiny local formatter without pulling strconv into tests unnecessarily
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
