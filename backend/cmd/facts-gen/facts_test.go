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
	// GoPackagesInternal is classified by path relative to the repo root. A
	// literal "backend/internal/" prefix test against the walk path made this
	// zero for every root but ".", so assert it against an independent count
	// and require it to be a strict subset of the total.
	if want := countGoDirs(t, filepath.Join(repo, "backend", "internal")); f.GoPackagesInternal != want {
		t.Errorf("internal packages: facts=%d recount=%d", f.GoPackagesInternal, want)
	}
	if f.GoPackagesInternal == 0 || f.GoPackagesInternal >= f.GoPackagesTotal {
		t.Errorf("internal packages %d must be a non-empty strict subset of total %d",
			f.GoPackagesInternal, f.GoPackagesTotal)
	}
	// Frontend src files must be counted from frontend/src only. The metric used
	// to include vendored node_modules sources (hundreds of files with a "src"
	// path segment), so it drifted with what happened to be installed.
	if want := countFESrc(t, filepath.Join(repo, "frontend", "src")); f.FESrcFiles != want {
		t.Errorf("frontend src files: facts=%d recount=%d (node_modules must be excluded)", f.FESrcFiles, want)
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
	doc := "# заголовок\n\nвступление человека\n\n" + block + "\nэпилог\n"
	if !MetricsBlockMatch(doc, block) {
		t.Error("freshly generated doc must match itself")
	}
	// The commit stamp legitimately changes between the committed SHA and a
	// fresh CI checkout; every other line must still match.
	freshSHA := RenderMarkdown(rep, "def456")
	if !MetricsBlockMatch(doc, freshSHA) {
		t.Error("commit stamp differences inside the block must not fail -check")
	}
	// Tamper with a metric value inside the committed document: the check must fail.
	i := strings.Index(doc, "**")
	j := strings.Index(doc[i+2:], "**")
	tampered := doc[:i] + "**999999**" + doc[i+2+j+2:]
	if MetricsBlockMatch(tampered, block) {
		t.Error("tampered metric value must fail -check")
	}
	// Tampering with a definition/row text (not just the number) must fail too.
	dropped := strings.Replace(doc, "\n", "", 5) // structural change inside block
	if MetricsBlockMatch(dropped, block) {
		t.Error("dropped lines must fail -check")
	}
	if MetricsBlockMatch("plain text", block) {
		t.Error("missing markers must fail the check")
	}
}

// TestReplaceBlockPreservesProse pins the -write path: regeneration must replace
// only the FACTS:AUTO region and leave hand-written text around it intact.
func TestReplaceBlockPreservesProse(t *testing.T) {
	doc := "# Заголовок\n\nвступление человека\n\n" +
		autoBegin + "\nстарое содержимое\n" + autoEnd + "\n\nэпилог\n"
	newBlock := autoBegin + "\nновое содержимое\n" + autoEnd + "\n"
	got, err := ReplaceBlock(doc, newBlock)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "# Заголовок") || !strings.Contains(got, "вступление человека") {
		t.Errorf("prose before the block was lost: %q", got)
	}
	if !strings.Contains(got, "эпилог") {
		t.Errorf("prose after the block was lost: %q", got)
	}
	if strings.Contains(got, "старое содержимое") {
		t.Errorf("old block content survived: %q", got)
	}
	if !strings.Contains(got, "новое содержимое") {
		t.Errorf("new block content missing: %q", got)
	}
	// Replacement is idempotent: running it again on its own output is a no-op.
	again, err := ReplaceBlock(got, newBlock)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Errorf("ReplaceBlock is not idempotent:\nfirst=%q\nagain=%q", got, again)
	}
	if _, err := ReplaceBlock("нет маркеров", newBlock); err == nil {
		t.Error("missing markers in the document must error")
	}
	if _, err := ReplaceBlock(doc, "нет маркеров"); err == nil {
		t.Error("block without markers must error")
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
	// Two statements with different column orders in one file. Reading the header
	// from the first statement (the original bug) makes the second one use the
	// wrong column index, so its system roles are miscounted.
	twoStatements := `INSERT INTO roles (key, name, description, is_system) VALUES
		('a', 'A', 'x', true),
		('b', 'B', 'y', false);
	INSERT INTO roles (is_system, key, name, description) VALUES
		(true, 'c', 'C', 'z'),
		(false, 'd', 'D', 'w');`
	if n := countSystemRoles(twoStatements); n != 2 {
		t.Errorf("per-statement column list: got %d want 2", n)
	}
}

// TestScanFrontendIgnoresNodeModules pins the exclusion that the substring-based
// classifier missed: a vendored .ts file whose path contains "src" must not be
// counted, and classification must not depend on the prefix of dir.
func TestScanFrontendIgnoresNodeModules(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/App.tsx", `<Routes><Route path="/a"/></Routes>`)
	write("src/pages/HomePage.tsx", "export const X = 1")
	write("src/pages/HomePage.test.tsx", "it('x', () => {})")
	write("src/util.ts", "export const u = 1")
	write("src/util.test.ts", "test('u', () => {})")
	write("e2e/login.spec.ts", "test('login', () => {})")
	// Vendored sources with a "src" segment and .ts/.tsx names.
	write("node_modules/pkg/src/index.ts", "export const vendored = 1")
	write("node_modules/@scope/lib/src/deep/Comp.tsx", "export const C = 1")
	// A nested node_modules inside src: without an explicit skip this path
	// starts with "src/" and would be counted as project source.
	write("src/node_modules/vendored/src/index.ts", "export const nested = 1")

	r, err := scanFrontend(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.srcFiles != 5 {
		t.Errorf("srcFiles=%d want 5 (node_modules must be excluded)", r.srcFiles)
	}
	if r.testFiles != 2 {
		t.Errorf("testFiles=%d want 2", r.testFiles)
	}
	if r.pageFiles != 1 || r.pageTests != 1 {
		t.Errorf("pageFiles=%d pageTests=%d want 1/1", r.pageFiles, r.pageTests)
	}
	if r.e2eFiles != 1 || r.e2eTests != 1 {
		t.Errorf("e2eFiles=%d e2eTests=%d want 1/1", r.e2eFiles, r.e2eTests)
	}
	if r.routes != 1 || r.uniqueRoutes != 1 {
		t.Errorf("routes=%d unique=%d want 1/1", r.routes, r.uniqueRoutes)
	}

	// The same tree reached through a relative path must yield identical counts.
	parent := filepath.Dir(dir)
	rel, err := filepath.Rel(parent, dir)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	r2, err := scanFrontend(rel)
	if err != nil {
		t.Fatal(err)
	}
	if r2.srcFiles != r.srcFiles || r2.e2eFiles != r.e2eFiles || r2.pageFiles != r.pageFiles {
		t.Errorf("relative root changed counts: abs=%+v rel=%+v", r, r2)
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

func TestScanAppRoutesSplitsByTree(t *testing.T) {
	src := `
	const anonymous = (
	  <Routes>
	    <Route path="/login" element={A}/>
	    <Route path="*" element={B}/>
	  </Routes>
	)
	const authed = (
	  <Routes>
	    <Route path="/dashboard" element={C}/>
	    <Route path="/login" element={<Navigate to="/"/>}/>
	    <Route path="*" element={D}/>
	  </Routes>
	)`
	total, unique, dups, err := scanAppRoutes(src)
	if err != nil {
		t.Fatal(err)
	}
	// /login and * legitimately repeat across the two mutually exclusive
	// trees; distinct paths are /login, *, /dashboard.
	if total != 5 || unique != 3 {
		t.Fatalf("total=%d unique=%d, want 5/3", total, unique)
	}
	if len(dups) != 0 {
		t.Fatalf("cross-tree repeats must not count as duplicates: %v", dups)
	}

	dupInOneTree := `<Routes><Route path="/a"/><Route path="/a"/></Routes>`
	_, _, dups2, err := scanAppRoutes(dupInOneTree)
	if err != nil {
		t.Fatal(err)
	}
	if tree, ok := dups2["/a"]; !ok || tree != "tree#1" {
		t.Fatalf("within-tree duplicate not detected: %v", dups2)
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

// countGoDirs counts distinct directories holding at least one .go file.
func countGoDirs(t *testing.T, dir string) int {
	t.Helper()
	dirs := map[string]struct{}{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") {
			dirs[filepath.Dir(p)] = struct{}{}
		}
		return nil
	})
	return len(dirs)
}

// countFESrc counts .ts/.tsx files under dir, excluding test files is not the
// point here — it mirrors the generator's "frontend/src/**, including tests"
// definition and never descends into node_modules.
func countFESrc(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx") {
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
