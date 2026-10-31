package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Facts holds every metric the generator reports. Field order defines rendering
// order; all slices are sorted before output so runs are byte-for-byte identical.
type Facts struct {
	GoPackagesTotal    int      `json:"go_packages_total"`    // distinct dirs with .go files under backend/
	GoPackagesInternal int      `json:"go_packages_internal"` // same, restricted to backend/internal/
	GoFiles            int      `json:"go_files"`             // non-test .go files under backend/
	GoTestFiles        int      `json:"go_test_files"`        // *_test.go files under backend/
	GoTestFuncs        int      `json:"go_test_funcs"`        // top-level func TestXxx (excluding TestMain)
	GoBenchmarks       int      `json:"go_benchmarks"`        // top-level func BenchmarkXxx
	GoFuzzers          int      `json:"go_fuzzers"`           // top-level func FuzzXxx
	GoExamples         int      `json:"go_examples"`          // top-level func ExampleXxx
	MigrationsUp       int      `json:"migrations_up"`        // *.up.sql in internal/migrations
	MigrationsDown     int      `json:"migrations_down"`      // *.down.sql in internal/migrations
	DBTables           int      `json:"db_tables"`            // unique CREATE TABLE names across up migrations
	RbacConsts         int      `json:"rbac_consts"`          // Permission-typed string constants in rbac.go
	RbacCatalog        int      `json:"rbac_catalog"`         // entries of the catalog map in rbac.go
	RbacSeeded         int      `json:"rbac_seeded"`          // permission codes seeded via INSERT INTO permissions
	SystemRoles        int      `json:"system_roles"`         // role tuples inserted with is_system = true
	OpenAPIPaths       int      `json:"openapi_paths"`        // paths in docs/api/openapi.json
	OpenAPIOperations  int      `json:"openapi_operations"`   // operations in docs/api/openapi.json
	FESrcFiles         int      `json:"frontend_src_files"`   // .ts/.tsx under frontend/src
	FETestFiles        int      `json:"frontend_test_files"`  // *.test.*/*.spec.* under frontend/src
	FETestDecls        int      `json:"frontend_test_decls"`  // it()/test() declarations in frontend/src (describe excluded)
	FEPageFiles        int      `json:"frontend_page_files"`  // component files under frontend/src/pages
	FEPageTests        int      `json:"frontend_page_tests"`  // test files under frontend/src/pages
	FERoutes           int      `json:"frontend_routes"`      // <Route path="..."> declarations in App.tsx
	E2EFiles           int      `json:"e2e_spec_files"`       // *.spec.ts under frontend/e2e
	E2ETests           int      `json:"e2e_tests"`            // test() declarations under frontend/e2e (describe excluded)
	CIWorkflows        int      `json:"ci_workflows"`         // workflow files in .github/workflows
	CIJobs             int      `json:"ci_jobs"`              // jobs across all workflows
	CIJobNames         []string `json:"ci_job_names"`         // sorted "workflow.yml/job" pairs
}

// Consistency records whether independent sources agree. Any false value means
// the repository contradicts itself and -check must fail.
type Consistency struct {
	RbacConstsMatchCatalog  bool `json:"rbac_consts_match_catalog"`
	RbacSeededMatchCatalog  bool `json:"rbac_seeded_match_catalog"`
	MigrationPairsSymmetric bool `json:"migration_pairs_symmetric"`
	RoutePathsUnique        bool `json:"route_paths_unique"`
}

// Report is the full generator output.
type Report struct {
	Facts       Facts       `json:"facts"`
	Consistency Consistency `json:"consistency"`
}

// Compute walks the repository rooted at repo and derives every metric from
// source. It fails only when a required file is missing or unparseable.
func Compute(repo string) (*Report, error) {
	f := &Facts{}
	c := &Consistency{}

	// --- Go tree -------------------------------------------------------------
	pkgs := map[string]struct{}{}
	internalPkgs := map[string]struct{}{}
	var tests, benches, fuzzers, examples int
	err := filepath.WalkDir(filepath.Join(repo, "backend"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(p))
		pkgs[dir] = struct{}{}
		if dir == "backend/internal" || strings.HasPrefix(dir, "backend/internal/") {
			internalPkgs[dir] = struct{}{}
		}
		if strings.HasSuffix(filepath.Base(p), "_test.go") {
			f.GoTestFiles++
			n, b, fu, e, perr := countGoTestFuncs(p)
			if perr != nil {
				return fmt.Errorf("parse %s: %w", p, perr)
			}
			tests += n
			benches += b
			fuzzers += fu
			examples += e
		} else {
			f.GoFiles++
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk backend: %w", err)
	}
	f.GoPackagesTotal = len(pkgs)
	f.GoPackagesInternal = len(internalPkgs)
	f.GoTestFuncs, f.GoBenchmarks, f.GoFuzzers, f.GoExamples = tests, benches, fuzzers, examples

	// --- RBAC catalog (AST) ----------------------------------------------------
	consts, catalog, err := parseRBAC(filepath.Join(repo, "backend/internal/rbac/rbac.go"))
	if err != nil {
		return nil, err
	}
	f.RbacConsts = len(consts)
	f.RbacCatalog = len(catalog)
	c.RbacConstsMatchCatalog = setEqual(consts, catalog)

	// --- Migrations --------------------------------------------------------------
	ups, downs, tables, seeded, systemRoles, err := scanMigrations(filepath.Join(repo, "backend/internal/migrations"))
	if err != nil {
		return nil, err
	}
	f.MigrationsUp, f.MigrationsDown = ups, downs
	f.DBTables, f.RbacSeeded, f.SystemRoles = len(tables), len(seeded), systemRoles
	c.MigrationPairsSymmetric = ups == downs
	// platform.owner is seeded into the permissions table but is deliberately not
	// part of the capability catalog (it is assigned through role membership, not
	// granted per-route). Every other seeded code must exist in the catalog and
	// every catalog entry must be seeded by some migration.
	synced := true
	for p := range seeded {
		if p == "platform.owner" {
			continue
		}
		if _, ok := consts[p]; !ok {
			synced = false
		}
	}
	for p := range consts {
		if _, ok := seeded[p]; !ok {
			synced = false
		}
	}
	c.RbacSeededMatchCatalog = synced

	// --- OpenAPI -------------------------------------------------------------------
	paths, ops, err := countOpenAPI(filepath.Join(repo, "docs/api/openapi.json"))
	if err != nil {
		return nil, err
	}
	f.OpenAPIPaths, f.OpenAPIOperations = paths, ops

	// --- Frontend --------------------------------------------------------------------
	res, err := scanFrontend(filepath.Join(repo, "frontend"))
	if err != nil {
		return nil, err
	}
	f.FESrcFiles, f.FETestFiles, f.FETestDecls = res.srcFiles, res.testFiles, res.testDecls
	f.FEPageFiles, f.FEPageTests, f.FERoutes = res.pageFiles, res.pageTests, res.routes
	c.RoutePathsUnique = res.duplicateRoutes == 0
	f.E2EFiles, f.E2ETests = res.e2eFiles, res.e2eTests

	// --- CI ----------------------------------------------------------------------------
	workflows, jobPairs, err := scanCI(filepath.Join(repo, ".github/workflows"))
	if err != nil {
		return nil, err
	}
	f.CIWorkflows, f.CIJobs, f.CIJobNames = workflows, len(jobPairs), jobPairs

	return &Report{Facts: *f, Consistency: *c}, nil
}

// countGoTestFuncs parses one Go file and returns top-level counts of
// Test/Benchmark/Fuzz/Example functions. TestMain is the harness entry point
// and is not counted as a test.
func countGoTestFuncs(path string) (tests, benches, fuzzers, examples int, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		switch {
		case strings.HasPrefix(name, "Benchmark"):
			benches++
		case strings.HasPrefix(name, "Fuzz"):
			fuzzers++
		case strings.HasPrefix(name, "Example"):
			examples++
		case name == "TestMain":
		case strings.HasPrefix(name, "Test"):
			tests++
		}
	}
	return tests, benches, fuzzers, examples, nil
}

// parseRBAC extracts permission constant values and catalog-map keys from
// rbac.go. Catalog keys are identifiers resolved through the constant table,
// so both sides compare as dotted permission strings.
func parseRBAC(path string) (consts, catalog map[string]struct{}, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, fmt.Errorf("parse rbac: %w", err)
	}
	consts = map[string]struct{}{}
	names := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) != len(vs.Names) {
				continue
			}
			typName := ""
			if id, ok := vs.Type.(*ast.Ident); ok {
				typName = id.Name
			}
			for i, nm := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				names[nm.Name] = val
				if typName == "Permission" || strings.HasPrefix(nm.Name, "Perm") {
					consts[val] = struct{}{}
				}
			}
		}
	}
	catalog = map[string]struct{}{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 || vs.Names[0].Name != "catalog" || len(vs.Values) == 0 {
				continue
			}
			cl, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, nil, fmt.Errorf("rbac: catalog is not a composite literal")
			}
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					return nil, nil, fmt.Errorf("rbac: catalog entry is not key:value")
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					return nil, nil, fmt.Errorf("rbac: catalog key is not an identifier")
				}
				val, ok := names[key.Name]
				if !ok {
					return nil, nil, fmt.Errorf("rbac: catalog key %s has no constant", key.Name)
				}
				catalog[val] = struct{}{}
			}
		}
	}
	if len(catalog) == 0 {
		return nil, nil, fmt.Errorf("rbac: catalog variable not found")
	}
	return consts, catalog, nil
}

var createTableRe = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([a-zA-Z_][a-zA-Z0-9_]*)`?")

// scanMigrations reads every *.sql file in dir. Tables and seeds come from up
// migrations only. Returns sets-as-maps for tables/seeded so callers can diff.
func scanMigrations(dir string) (ups, downs int, tables, seeded map[string]struct{}, systemRoles int, err error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return 0, 0, nil, nil, 0, err
	}
	if len(entries) == 0 {
		return 0, 0, nil, nil, 0, fmt.Errorf("migrations: no sql files in %s", dir)
	}
	tables = map[string]struct{}{}
	seeded = map[string]struct{}{}
	for _, e := range entries {
		if !strings.HasSuffix(filepath.Base(e), ".up.sql") {
			if strings.HasSuffix(filepath.Base(e), ".down.sql") {
				downs++
			}
			continue
		}
		ups++
		data, err := os.ReadFile(e)
		if err != nil {
			return 0, 0, nil, nil, 0, err
		}
		text := string(data)
		for _, m := range createTableRe.FindAllStringSubmatch(text, -1) {
			tables[strings.ToLower(m[1])] = struct{}{}
		}
		for _, code := range extractSeededPermissions(text) {
			seeded[code] = struct{}{}
		}
		systemRoles += countSystemRoles(text)
	}
	return ups, downs, tables, seeded, systemRoles, nil
}

var insertPermsRe = regexp.MustCompile(`(?is)INSERT\s+INTO\s+permissions[^;]*?VALUES(.*?);`)
var quotedCodeRe = regexp.MustCompile(`'([a-z][a-z0-9_]*(?:\.[a-z0-9_*]+)+)'`)

// extractSeededPermissions returns unique dotted permission codes inserted into
// the permissions table by this migration text.
func extractSeededPermissions(text string) []string {
	seen := map[string]struct{}{}
	for _, block := range insertPermsRe.FindAllStringSubmatch(text, -1) {
		for _, m := range quotedCodeRe.FindAllStringSubmatch(block[1], -1) {
			seen[m[1]] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

var insertRolesRe = regexp.MustCompile(`(?is)INSERT\s+INTO\s+roles[^;]*?VALUES(.*?);`)
var tupleRe = regexp.MustCompile(`\(([^()]*(?:'[^']*'[^()]*)*)\)`)

// countSystemRoles counts role value tuples whose is_system column is TRUE.
// Column position is taken from the INSERT column list, so reordering columns
// in a future migration cannot silently change the count.
func countSystemRoles(text string) int {
	n := 0
	for _, ins := range insertRolesRe.FindAllStringSubmatch(text, -1) {
		header := insertRolesRe.FindString(text)
		cols := splitSQLTuple(extractColumnList(header))
		idx := -1
		for i, col := range cols {
			if strings.EqualFold(strings.TrimSpace(col), "is_system") {
				idx = i
			}
		}
		if idx < 0 {
			continue
		}
		for _, tuple := range tupleRe.FindAllStringSubmatch(ins[1], -1) {
			vals := splitSQLTuple(tuple[1])
			if idx < len(vals) && strings.EqualFold(strings.TrimSpace(vals[idx]), "true") {
				n++
			}
		}
	}
	return n
}

func extractColumnList(insert string) string {
	i := strings.Index(insert, "(")
	j := strings.Index(insert, ")")
	if i < 0 || j < 0 || j <= i {
		return ""
	}
	return insert[i+1 : j]
}

// splitSQLTuple splits a comma-separated SQL list, respecting quotes and nesting.
func splitSQLTuple(s string) []string {
	var parts []string
	depth, inStr, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\'':
			inStr = !inStr
		case !inStr && ch == '(':
			depth++
		case !inStr && ch == ')':
			depth--
		case !inStr && depth == 0 && ch == ',':
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

// countOpenAPI reads the committed specification and counts paths and operations.
func countOpenAPI(path string) (paths, ops int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, 0, fmt.Errorf("parse %s: %w", path, err)
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	for _, item := range doc.Paths {
		paths++
		for m := range item {
			if methods[strings.ToLower(m)] {
				ops++
			}
		}
	}
	return paths, ops, nil
}

type frontendScan struct {
	srcFiles, testFiles, testDecls int
	pageFiles, pageTests           int
	routes, duplicateRoutes        int
	e2eFiles, e2eTests             int
}

var (
	itRe       = regexp.MustCompile(`(?m)^[ \t]*(?:it|test)(?:\.\w+)*\(`)
	describeRe = regexp.MustCompile(`(?m)^[ \t]*test\.describe\(`)
	testFileRe = regexp.MustCompile(`\.(test|spec)\.tsx?$`)
	routeRe    = regexp.MustCompile(`<Route\b[^>]*?path="([^"]*)"`)
)

// scanFrontend derives frontend metrics from source files only. Declarations of
// describe blocks are subtracted so "tests" means runnable cases, not suites.
func scanFrontend(dir string) (frontendScan, error) {
	var r frontendScan
	routesSeen := map[string]int{}
	walk := func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(p)
		slash := filepath.ToSlash(p)
		read := func() ([]byte, error) { return os.ReadFile(p) }
		switch {
		case strings.Contains(slash, "/e2e/") && strings.HasSuffix(base, ".spec.ts"):
			t, err := read()
			if err != nil {
				return err
			}
			r.e2eFiles++
			r.e2eTests += len(itRe.FindAllIndex(t, -1)) - len(describeRe.FindAllIndex(t, -1))
		case strings.Contains(slash, "/src/") && (strings.HasSuffix(base, ".ts") || strings.HasSuffix(base, ".tsx")):
			t, err := read()
			if err != nil {
				return err
			}
			r.srcFiles++
			isTest := testFileRe.MatchString(base)
			if isTest {
				r.testFiles++
			}
			r.testDecls += len(itRe.FindAllIndex(t, -1)) - len(describeRe.FindAllIndex(t, -1))
			if strings.Contains(slash, "/pages/") {
				if isTest {
					r.pageTests++
				} else {
					r.pageFiles++
				}
			}
			if base == "App.tsx" {
				for _, m := range routeRe.FindAllStringSubmatch(string(t), -1) {
					r.routes++
					routesSeen[m[1]]++
				}
			}
		}
		return nil
	}
	if err := filepath.WalkDir(dir, walk); err != nil {
		return r, err
	}
	for _, n := range routesSeen {
		if n > 1 {
			r.duplicateRoutes++
		}
	}
	return r, nil
}

var ciJobRe = regexp.MustCompile(`^  ([A-Za-z][\w-]*):\s*$`)

// scanCI counts workflows and jobs. Jobs are two-space-indented keys inside the
// top-level "jobs:" mapping only — section tracking, not a blind grep, so
// trigger keys under "on:" are never miscounted.
func scanCI(dir string) (workflows int, jobPairs []string, err error) {
	yml, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return 0, nil, err
	}
	yaml, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return 0, nil, err
	}
	files := append(yml, yaml...)
	sort.Strings(files)
	for _, f := range files {
		workflows++
		data, err := os.ReadFile(f)
		if err != nil {
			return 0, nil, err
		}
		inJobs := false
		for _, line := range strings.Split(string(data), "\n") {
			if line == "jobs:" {
				inJobs = true
				continue
			}
			if !inJobs {
				continue
			}
			if line != "" && line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(line, "#") {
				inJobs = false
				continue
			}
			if m := ciJobRe.FindStringSubmatch(line); m != nil {
				jobPairs = append(jobPairs, filepath.Base(f)+"/"+m[1])
			}
		}
	}
	sort.Strings(jobPairs)
	return workflows, jobPairs, nil
}

func setEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}
