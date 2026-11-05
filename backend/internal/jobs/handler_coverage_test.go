package jobs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// reservedKinds are kinds this package declares but the worker does not dispatch.
//
// They are not a bug to hide: export and migration runs execute synchronously in
// the request path today, and reconciliation is a manual operation, so no code
// enqueues these kinds. They are kept because the vocabulary is the public
// contract a queue row is written against, and a future asynchronous runner would
// use the same names rather than invent new ones. The point of listing them here
// is that the set is deliberate: TestEveryDeclaredKindIsDispatchedOrReserved fails
// if a kind is declared and neither handled nor named here, so a new kind cannot
// be added and silently never run.
// Keyed by the constant's identifier, matching the names the parser returns.
var reservedKinds = map[string]bool{
	"KindExportExecute": true,
	"KindMigrationRun":  true,
	"KindReconcile":     true,
}

// TestEveryDeclaredKindIsDispatchedOrReserved ties the two halves of the job
// vocabulary together: every Kind constant this package declares must either have
// a case in the worker's dispatch switch or be named in reservedKinds.
//
// The failure this prevents is quiet and expensive. A kind with no handler is a
// job that is accepted at the write, sits queued forever, and is claimed by a
// worker that can only return "no handler is registered" — the same shape of bug
// the outbox had when events were recorded and nothing consumed them. It reads
// as correct in every place a person looks, because the constant and the status
// vocabulary both look fine.
func TestEveryDeclaredKindIsDispatchedOrReserved(t *testing.T) {
	// The dispatch switch lives in the worker binary, which is part of the
	// monolith but not of this package. When the extraction harness copies jobs
	// into a standalone module the worker is absent, and this guard has nothing
	// to read; skip rather than fail, because the extraction test asserts
	// independence, not dispatch coverage.
	workerSrc := filepath.Join("..", "..", "cmd", "forgecrm-worker", "main.go")
	if _, err := os.Stat(workerSrc); err != nil {
		t.Skipf("worker source %s is not present (extracted module); skipping dispatch coverage", workerSrc)
	}

	declared := declaredKindConstants(t)
	if len(declared) < 10 {
		t.Fatalf("found only %d kind constants; the parser is probably not reading jobs.go", len(declared))
	}

	dispatched := dispatchedKinds(t, workerSrc)
	if len(dispatched) < 8 {
		t.Fatalf("found only %d dispatched kinds in the worker; the parser is probably not reading main.go", len(dispatched))
	}

	var missing []string
	for name := range declared {
		if dispatched[name] || reservedKinds[name] {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these job kinds are declared but neither dispatched by the worker nor listed in reservedKinds: %v", missing)
	}

	// The reverse direction matters too: a name in reservedKinds that is no longer
	// declared, or is now dispatched, is stale and should be removed so the list
	// keeps meaning what it says.
	for name := range reservedKinds {
		if !declared[name] {
			t.Errorf("reservedKinds names %q, which is no longer declared; remove it", name)
		} else if dispatched[name] {
			t.Errorf("reservedKinds names %q, which the worker now dispatches; remove it from the list", name)
		}
	}
}

// declaredKindConstants parses jobs.go and returns the identifier of every
// `KindX = "..."` constant.
func declaredKindConstants(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "jobs.go", nil, 0)
	if err != nil {
		t.Fatalf("parse jobs.go: %v", err)
	}
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if len(name.Name) < 4 || name.Name[:4] != "Kind" {
				continue
			}
			if i < len(vs.Values) {
				if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
					out[name.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// dispatchedKinds parses the worker's main.go and returns the selector of every
// `case jobs.KindX:` label in the dispatch switch.
func dispatchedKinds(t *testing.T, path string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range cc.List {
			sel, ok := expr.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "jobs" {
				out[sel.Sel.Name] = true
			}
		}
		return true
	})
	return out
}
