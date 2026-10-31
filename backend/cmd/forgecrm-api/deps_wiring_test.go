package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestEveryWiredServiceIsCopiedIntoAPIDeps guards the defect the Chromium suite
// caught: internal/wiring builds every service, but main.go copies them into
// api.Deps by hand, and a service that is built but not copied leaves its
// handlers dereferencing nil. The failure only showed at request time as an
// opaque 500, because nothing at startup checks that the two lists agree.
//
// This parses the three declarations and fails when wiring.Services has a field
// that the api.Deps literal in main.go never receives. It is a static check, so
// it runs in the ordinary unit suite with no database.
func TestEveryWiredServiceIsCopiedIntoAPIDeps(t *testing.T) {
	// This test lives in cmd/forgecrm-api, so the module root is two levels up.
	root := filepath.Join("..", "..")

	wired := structFields(t, filepath.Join(root, "internal", "wiring", "wiring.go"), "Services")

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	copied := apiDepsCopiedFields(t, file)
	if len(copied) == 0 {
		t.Fatal("found no services.X entries in the api.Deps literal; the parser is wrong, not the code")
	}

	var missing []string
	for _, name := range wired {
		if !copied[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("wiring.Services fields never copied into api.Deps in main.go: %v\n"+
			"A service built but not copied leaves its handlers with a nil dependency. "+
			"Add it to the api.New(&api.Deps{...}) literal.", missing)
	}
}

// structFields returns the field names of a named struct type in a file.
func structFields(t *testing.T, path, typeName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var names []string
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			for _, nm := range f.Names {
				names = append(names, nm.Name)
			}
		}
		return false
	})
	if len(names) == 0 {
		t.Fatalf("no fields found for %s in %s", typeName, path)
	}
	return names
}

// apiDepsCopiedFields returns the set of field names assigned from `services.X`
// in the api.New(&api.Deps{...}) literal.
func apiDepsCopiedFields(t *testing.T, file *ast.File) map[string]bool {
	t.Helper()
	return depsFieldsFrom(t, file, "services")
}

// TestTestHarnessWiresTheSameServicesAsProduction guards the mirror of the defect
// above on the test side. The integration harness builds the same api.Deps by hand
// from wiring.Services, and a service it forgets leaves its routes panicking on a
// nil dependency. The recovery middleware turns that panic into an opaque 500, so
// the surface reads as an untested feature rather than a missing harness field —
// exactly how the automation, simulation, forecast, risk, storage and saved-view
// routes came to have no HTTP coverage.
//
// The check is that every field production copies from a service object is copied in
// the harness too. It is static, so it runs in the ordinary unit suite with no
// database, and it fails on the omission that motivated it.
func TestTestHarnessWiresTheSameServicesAsProduction(t *testing.T) {
	root := filepath.Join("..", "..")

	prod, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	prodFile, err := parser.ParseFile(token.NewFileSet(), "main.go", prod, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	want := apiDepsCopiedFields(t, prodFile)

	harnessPath := filepath.Join(root, "internal", "api", "tenant_isolation_test.go")
	harness, err := os.ReadFile(harnessPath)
	if err != nil {
		t.Fatalf("read %s: %v", harnessPath, err)
	}
	harnessFile, err := parser.ParseFile(token.NewFileSet(), harnessPath, harness, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", harnessPath, err)
	}
	// The harness assigns from `svcs.X` rather than `services.X`.
	got := depsFieldsFrom(t, harnessFile, "svcs")
	if len(got) == 0 {
		t.Fatal("found no svcs.X entries in the harness Deps literal; the parser is wrong, not the code")
	}

	var missing []string
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("services wired in production but not in the integration harness: %v\n"+
			"A service the harness omits makes its routes panic on a nil dependency, "+
			"recovered as an opaque 500 with no coverage. Wire it in newHarnessWithStore.", missing)
	}
}

// depsFieldsFrom returns the set of field names assigned from `<recv>.X` in any
// composite literal whose type is named Deps.
func depsFieldsFrom(t *testing.T, file *ast.File, recv string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// The type is `api.Deps` in main.go and a bare `Deps` in the same-package
		// harness, so both forms are accepted.
		switch t := lit.Type.(type) {
		case *ast.SelectorExpr:
			if t.Sel.Name != "Deps" {
				return true
			}
		case *ast.Ident:
			if t.Name != "Deps" {
				return true
			}
		default:
			return true
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			// Only count a field assigned from the named receiver object.
			if vs, ok := kv.Value.(*ast.SelectorExpr); ok {
				if id, ok := vs.X.(*ast.Ident); ok && id.Name == recv {
					out[key.Name] = true
				}
			}
		}
		return false
	})
	return out
}
