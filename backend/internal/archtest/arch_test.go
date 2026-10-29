// Package archtest enforces the module dependency rules stated in
// docs/ARCHITECTURE.md and docs/adr/0001-hybrid-architecture.md.
//
// Those rules are the reason the monolith is modular rather than a pile of
// packages. Unchecked, they hold until the first convenient import cycle, and a
// cycle added today is discovered later, when it blocks an extraction that has
// already been promised. A test is the cheapest place to notice.
//
// The checks read the real import graph through `go list`, so they cannot drift
// from the code the way a hand-maintained list would.
package archtest

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/geff1778/forgecrm/"

type pkg struct {
	ImportPath string
	Imports    []string
}

// loadGraph returns every package in the module with its direct imports.
func loadGraph(t *testing.T) map[string]pkg {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = "../.." // the module root: backend/, two levels above internal/archtest
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list failed (is the Go toolchain on PATH?): %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	graph := map[string]pkg{}
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		graph[p.ImportPath] = p
	}
	// A guard against a vacuous pass: if go list returned nothing, every
	// assertion below would hold trivially and report success.
	if len(graph) < 20 {
		t.Fatalf("go list returned only %d packages; expected the whole module", len(graph))
	}
	return graph
}

// internalPkg names the internal package a fully-qualified import path refers
// to, or "" when the import is outside this module.
func internalPkg(importPath string) string {
	if !strings.HasPrefix(importPath, modulePath+"internal/") {
		return ""
	}
	rest := strings.TrimPrefix(importPath, modulePath+"internal/")
	return strings.SplitN(rest, "/", 2)[0]
}

// TestNoPackageImportsAPI pins the most important rule: the arrow points one way.
//
// `api` is the composition layer. If a domain or platform package imported it,
// that package could no longer be used by the worker, a future CLI or a test
// harness without dragging in the whole HTTP surface — and the cycle would make
// the module unextractable in exactly the direction extraction is meant to go.
func TestNoPackageImportsAPI(t *testing.T) {
	graph := loadGraph(t)

	for path, p := range graph {
		from := internalPkg(path)
		if from == "" || from == "api" {
			continue
		}
		for _, imp := range p.Imports {
			if internalPkg(imp) == "api" {
				t.Errorf("%s imports %s: only the composition layer may import api", from, imp)
			}
		}
	}
}

// TestProductionPackagesDoNotImportTestsupport keeps test scaffolding out of the
// binaries.
//
// `testsupport` opens a database and applies migrations. If a production package
// imported it, a running service would carry migration code and could be asked to
// migrate a database it does not own.
func TestProductionPackagesDoNotImportTestsupport(t *testing.T) {
	graph := loadGraph(t)

	for path, p := range graph {
		if strings.Contains(path, "/internal/testsupport") {
			continue
		}
		for _, imp := range p.Imports {
			if strings.HasSuffix(imp, "/internal/testsupport") {
				t.Errorf("%s imports testsupport, which is test-only support", path)
			}
		}
	}
}

// TestApplicationServicesDependOnTheCoreAndNotTheReverse pins the layering.
//
// The domain is layered rather than flat:
//
//	crm          the core — entities, rules, permissions. Depends on nothing
//	             domain-level.
//	importx,     application services. They orchestrate the core to perform a
//	migratex,    use case, so they depend on crm and on each other in one
//	ai, events   direction only.
//
// The rule that matters is the arrow into `crm`: the core must not import an
// application service. If it did, the core could no longer be used without that
// service, and a cycle would form the moment the service needed a core change —
// which is the specific cycle that makes a module unextractable.
//
// The check is written against the real graph rather than an assumed one. It was
// first written as "no domain package imports another", which failed on three
// genuine and correct edges (ai -> crm, importx -> crm, migratex -> integrations)
// and so described an architecture the code does not have. Correcting the test
// to match the code, rather than the code to match the test, is the point.
func TestApplicationServicesDependOnTheCoreAndNotTheReverse(t *testing.T) {
	// The core: nothing domain-level may be imported by it.
	core := "crm"

	// Application services: they may import the core and each other.
	services := map[string]bool{
		"importx":      true,
		"migratex":     true,
		"integrations": true,
		"events":       true,
		"notify":       true,
		"ai":           true,
	}

	graph := loadGraph(t)

	// The core must not import any application service.
	for path, p := range graph {
		if internalPkg(path) != core {
			continue
		}
		for _, imp := range p.Imports {
			if to := internalPkg(imp); services[to] {
				t.Errorf("%s (the core) imports %s (an application service); "+
					"the core must not depend on a use case that orchestrates it", core, to)
			}
		}
	}
}

// TestCoreDoesNotImportCompositionOrHTTP keeps the core free of transport.
//
// `crm` is called by the HTTP API, the MCP tools and the worker. If it imported
// `api` it could only be called from a request, and the worker and the AI layer
// would each need their own copy of the rules.
//
// The `httpx` edge is a known, recorded piece of debt rather than a rule: the
// core returns `httpx.APIError` and uses `httpx.Pagination`/`httpx.SortSpec`, so
// it does depend on that package's types. The dependency is one-directional and
// therefore not a cycle, so the module builds and the rules are still written
// once. It is wrong in principle — a domain error type does not belong in a
// package named for HTTP — and it is tracked in docs/PROJECT_AUDIT.md.
//
// This test does not pretend the debt is absent. It pins what must stay true
// while the debt exists:
//
//  1. `api` is never imported (hard rule).
//  2. `httpx` never imports `crm`, so the pair cannot become a cycle.
//
// When the error types are moved to a neutral package, tighten this test to
// forbid `httpx` entirely.
func TestCoreDoesNotImportCompositionOrHTTP(t *testing.T) {
	graph := loadGraph(t)

	// Rule 1: the core never imports the composition layer.
	for path, p := range graph {
		if internalPkg(path) != "crm" {
			continue
		}
		for _, imp := range p.Imports {
			if internalPkg(imp) == "api" {
				t.Errorf("crm imports api; the core must not depend on the composition layer")
			}
		}
	}

	// Rule 2: the httpx debt must stay one-directional. If httpx ever imports
	// crm, the pair becomes a cycle and the debt turns into a structural defect.
	for path, p := range graph {
		if internalPkg(path) != "httpx" {
			continue
		}
		for _, imp := range p.Imports {
			if internalPkg(imp) == "crm" {
				t.Errorf("httpx imports crm: combined with crm -> httpx this is an import cycle")
			}
		}
	}
}

// TestExtractionBoundaryHolds pins the extraction-ready property in the import
// graph, so a regression is caught by `go test ./...` and not only by the heavier
// extraction harness in scripts/extract-test.
//
// Five modules are pinned:
//
//   - netguard is the SSRF guard. It is a leaf: it must depend on nothing inside
//     this module and nothing outside the standard library. That is what lets the
//     object store use the guard without importing the Integration Hub.
//   - secrets is password hashing, tokens and the credential keyring. It is a leaf
//     too, so it can be built and tested on its own.
//   - logging is structured logging with central redaction; a leaf as well, and the
//     foundation every other package logs through.
//   - objectstore is the object-storage layer carved out of `storage`. It may
//     depend on netguard and on third-party libraries, but on no application-layer
//     package — not the database, not the audit log, not RBAC, not HTTP error
//     types. A single such import is what would make it unextractable.
//   - httpx is the HTTP layer. It may depend only on logging and ratelimit, the
//     two leaves it is built on; a dependency on the database, RBAC or a domain
//     service is what would stop it from lifting out of the monolith.
//
// This is the cheap, always-on half of the check. The extraction harness is the
// expensive half that actually compiles the module on its own; the two are
// deliberately independent, so neither a stale archtest nor a skipped CI job can
// hide a violation.
func TestExtractionBoundaryHolds(t *testing.T) {
	graph := loadGraph(t)

	// The application layer, as named in TestApplicationServicesDependOnTheCore...
	// plus the infrastructure a storage provider must not know about.
	forbiddenForObjectstore := map[string]bool{
		"api":          true,
		"crm":          true,
		"database":     true,
		"audit":        true,
		"rbac":         true,
		"httpx":        true,
		"storage":      true,
		"integrations": true,
		"wiring":       true,
		"testsupport":  true,
	}

	// netguard, secrets and logging are the modules the extraction harness proves
	// standalone as leaves: each must have no import whose target begins with
	// modulePath.
	for path, p := range graph {
		name := internalPkg(path)
		switch name {
		case "netguard", "secrets", "logging":
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePath) {
					t.Errorf("%s imports %s; it must be a leaf with no in-module dependency",
						name, strings.TrimPrefix(imp, modulePath))
				}
			}
		case "objectstore":
			for _, imp := range p.Imports {
				if to := internalPkg(imp); forbiddenForObjectstore[to] {
					t.Errorf("objectstore imports %s; the storage boundary must not depend on the application layer",
						to)
				}
			}
		case "httpx":
			// httpx may depend only on its two proven leaves. A dependency on the
			// database, RBAC or a domain service is what would stop the HTTP layer
			// from lifting out, and is the coupling the extraction harness catches.
			allowed := map[string]bool{"logging": true, "ratelimit": true}
			for _, imp := range p.Imports {
				if to := internalPkg(imp); to != "" && !allowed[to] {
					t.Errorf("httpx imports %s; the HTTP layer may depend only on logging and ratelimit", to)
				}
			}
		}
	}
}

// TestEveryInternalPackageIsReachableFromACommand catches dead packages.
//
// A package no binary can reach is either an unfinished extraction or a leftover.
// Both matter: a leftover is still compiled, reviewed and secured as though it
// were live.
func TestEveryInternalPackageIsReachableFromACommand(t *testing.T) {
	graph := loadGraph(t)

	reachable := map[string]bool{}
	var visit func(string)
	visit = func(path string) {
		if reachable[path] {
			return
		}
		reachable[path] = true
		for _, dep := range graph[path].Imports {
			visit(dep)
		}
	}

	roots := 0
	for path := range graph {
		if strings.Contains(path, "/cmd/") {
			roots++
			visit(path)
		}
	}
	if roots == 0 {
		t.Fatal("no command packages found; the reachability check would be vacuous")
	}

	// migrations is embedded SQL read through a directory, not an imported Go
	// package, so it has no importer by construction.
	//
	// archtest is this package: it imports nothing from the module and is run by
	// `go test` rather than linked into a binary, so it is unreachable by design.
	skip := map[string]bool{"migrations": true, "archtest": true}

	for path := range graph {
		internal := internalPkg(path)
		if internal == "" || skip[internal] {
			continue
		}
		if !reachable[path] {
			t.Errorf("%s is unreachable from any command: dead code or an unfinished extraction",
				strings.TrimPrefix(path, modulePath))
		}
	}
}
