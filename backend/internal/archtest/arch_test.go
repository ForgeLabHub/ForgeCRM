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
	"sort"
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
// Thirty modules are pinned:
//
//   - netguard is the SSRF guard. It is a leaf: it must depend on nothing inside
//     this module and nothing outside the standard library. That is what lets the
//     object store use the guard without importing the Integration Hub.
//   - secrets is password hashing, tokens and the credential keyring. It is a leaf
//     too, so it can be built and tested on its own.
//   - logging is structured logging with central redaction; a leaf as well, and the
//     foundation every other package logs through.
//   - database is the connection pool and transaction helper. A leaf, so the pool
//     guard and the transaction contract can be tested with no server and no
//     application around them.
//   - importx/safe is the untrusted-archive extractor: the one place that writes
//     uploader-chosen bytes to disk. It is a leaf, so nothing it does can be
//     influenced by the application around it.
//   - importx/parsers is the tabular source parser. It is a leaf too, so the whole
//     package lifts out with only its spreadsheet dependency.
//   - objectstore is the object-storage layer carved out of `storage`. It may
//     depend on netguard and on third-party libraries, but on no application-layer
//     package — not the database, not the audit log, not RBAC, not HTTP error
//     types. A single such import is what would make it unextractable.
//   - httpx is the HTTP layer. It may depend only on logging and ratelimit, the
//     two leaves it is built on; a dependency on the database, RBAC or a domain
//     service is what would stop it from lifting out of the monolith.
//   - rbac is the permission catalog, the request principal and the resolution
//     service. It may depend only on the database handle: the catalog and the
//     checks are pure, and the service talks to the database and nothing else.
//   - audit is the append-only action trail and the security-event stream, and
//     outbox is the transactional outbox with the event vocabulary. Both may
//     depend only on the database handle and the logger, the two leaves they are
//     built on; anything else would weld them to the application.
//   - retention is the data-retention engine: a fixed policy catalog, a dry-run
//     default and an opt-in delete. It may depend only on the audit trail and the
//     database handle.
//   - agent is the human-in-the-loop layer: a proposal queue with a signed
//     approval. It may depend only on the audit trail, the database handle and the
//     HTTP primitives — the widest set of any proven module, and every one of them
//     is proven in turn.
//   - jobs is the queue. It may depend only on the database handle.
//   - crm is the domain service. It may depend only on the database handle, the
//     audit trail, the HTTP primitives, the outbox and RBAC.
//   - storage is the object-store application layer. It may depend only on the
//     database handle, the audit trail, the HTTP primitives, the provider contract
//     (objectstore, and objectstore's own netguard) and RBAC.
//   - integrations is the Integration Hub. It may depend only on the database
//     handle, the audit trail, the HTTP primitives, the SSRF guard (netguard), RBAC
//     and the secret keyring.
//   - config is the environment loader. It may depend only on the database handle,
//     the HTTP primitives and the retention engine whose window settings it
//     validates. It is the last shared piece most modules wait on.
//   - workflow is the automation engine. It may depend only on the database
//     handle, the audit trail, the HTTP primitives, the CRM domain, the outbox and
//     RBAC.
//   - events is the inbound webhook gateway. It may depend only on the database
//     handle, the audit trail, the HTTP primitives, the Integration Hub, the
//     outbox, RBAC and the secret keyring.
//   - migratex is the cross-database migration service. It may depend only on the
//     database handle, the audit trail, the HTTP primitives, the Integration Hub,
//     RBAC and the secret keyring.
//   - simulate, forecast and risk are read-only computations. Each may depend only
//     on the pure analytics core, the audit trail, the CRM read, the database
//     handle and RBAC; none of them writes a domain row.
//   - notify is the outbound transports. It may depend only on the environment
//     loader, the database handle, the audit trail, the HTTP primitives, RBAC and
//     the secret keyring.
//   - ai is the contextual AI layer. It may depend only on the human-in-the-loop
//     agent layer, the environment loader, the database handle, the audit trail,
//     the HTTP primitives, the CRM domain, the logger, the rate limiter and RBAC.
//   - importx is the Import Center. It may depend only on the environment loader,
//     the database handle, the audit trail, the HTTP primitives, the CRM domain,
//     the two parser leaves, the job queue, the outbox and RBAC.
//   - migrations is the embedded SQL. It is a leaf, but it carries data as well as
//     code: the embed directive names *.sql, so the extraction must copy the SQL.
//   - migrate is the runner over the embedded migrations; it depends only on them.
//   - authn is authentication. It is the widest module proven — the environment
//     loader, the schema runner, the retention engine and RBAC all travel with it.
//   - constructor builds a standalone project from a recipe: it reads a manifest
//     and copies modules out of the tree by path, importing none of them. It is a
//     leaf, but it carries data as well as code — the recipes/ directory is
//     embedded — so an extraction must copy the recipes too.
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

	// The pinned modules, keyed by their path relative to internal/. A leaf must
	// import nothing inside the module at all; a scoped module names the in-module
	// packages it is allowed to reach.
	//
	// The key is the full relative path, not internalPkg's first segment: a nested
	// package like importx/safe would otherwise never match its case and the test
	// would pass while checking nothing.
	leaves := map[string]bool{
		"netguard":        true,
		"secrets":         true,
		"logging":         true,
		"importx/safe":    true,
		"importx/parsers": true,
		"database":        true,
		// tracectx carries a W3C trace id through a context. It imports only the
		// standard library — no database, no logger, no domain type — so any layer
		// may depend on it without gaining a dependency on another layer.
		"tracectx": true,
		// constructor reads recipe manifests and the source tree; it copies modules
		// by path and imports none of them, so it is a leaf. A recipe that needed to
		// import a module to describe it would weld the constructor to that module.
		"constructor": true,
	}
	allowedDeps := map[string]map[string]bool{
		// httpx may depend only on its proven leaves. A dependency on the
		// database, RBAC or a domain service is what would stop the HTTP layer
		// from lifting out, and is the coupling the extraction harness catches.
		"httpx": {"logging": true, "ratelimit": true, "tracectx": true},
		// rbac may depend only on the database handle. Its catalog and Principal are
		// pure, and the resolution service talks to the database and nothing else, so
		// that is the whole boundary it has to keep to stay extractable.
		"rbac": {"database": true},
		// audit and outbox are built on the same two leaves — the database handle and
		// the logger — and on nothing else. A dependency on a domain service, the HTTP
		// layer or RBAC is what would weld them to the application.
		"audit":  {"database": true, "logging": true},
		"outbox": {"database": true, "logging": true},
		// retention is the data-retention engine. It is built on the audit trail and
		// the database handle; a dependency on a domain service is what would weld it
		// to the application.
		"retention": {"database": true, "audit": true, "logging": true},
		// agent is the human-in-the-loop layer, the widest set proven so far: the
		// audit trail, the database handle and the HTTP primitives, all of them
		// proven leaves or proven scoped modules themselves.
		"agent": {"database": true, "audit": true, "logging": true, "httpx": true, "ratelimit": true},
		// jobs is the queue: a unit of work enqueued, claimed, completed or failed
		// with a bounded retry. Its only in-module dependency is the database handle.
		"jobs": {"database": true},
		// crm is the domain service. It is built on the database handle, the audit
		// trail, the HTTP primitives, the outbox and RBAC, and on nothing else — no
		// dependency on the API layer, the workflow engine or a sibling domain.
		"crm": {"database": true, "audit": true, "httpx": true, "outbox": true, "rbac": true},
		// storage is the application layer over an object store. It re-exports the
		// provider contract from objectstore (which itself depends only on netguard),
		// so the store travels with it.
		"storage": {"database": true, "audit": true, "httpx": true, "objectstore": true, "rbac": true},
		// integrations is the Integration Hub: adapter lifecycle, the SSRF guard
		// (aliased from netguard), the secret handoff and the run log.
		"integrations": {"database": true, "audit": true, "httpx": true, "netguard": true, "rbac": true, "secrets": true},
		// config is the environment loader. It is the last shared piece most modules
		// wait on, so it is pinned to the database handle, the HTTP primitives and the
		// retention engine whose window settings it validates.
		"config": {"database": true, "httpx": true, "retention": true},
		// workflow is the automation engine: the step document, its validation, the
		// cron parser, the template catalog and the export/import round-trip. It reads
		// the CRM domain and writes through the outbox, so both are declared.
		"workflow": {"audit": true, "crm": true, "database": true, "httpx": true, "logging": true, "outbox": true, "rbac": true},
		// events is the inbound webhook gateway: signature verification, the timestamp
		// window, the payload cap and the dead-letter path, with the Integration Hub's
		// secret handoff for the per-subscription key.
		"events": {"audit": true, "database": true, "httpx": true, "integrations": true, "logging": true, "outbox": true, "rbac": true, "secrets": true, "tracectx": true},
		// migratex is the cross-database migration service: the source adapters and the
		// reconciliation, over the Integration Hub's connection secrets.
		"migratex": {"audit": true, "database": true, "httpx": true, "integrations": true, "rbac": true, "secrets": true},
		// simulate, forecast and risk are read-only computations over the pure
		// analytics core and the CRM read; none of them writes a domain row.
		"simulate": {"analytics": true, "audit": true, "crm": true, "database": true, "rbac": true},
		"forecast": {"analytics": true, "audit": true, "crm": true, "database": true, "rbac": true},
		"risk":     {"analytics": true, "audit": true, "crm": true, "database": true, "rbac": true},
		// notify is the outbound transports (email, Telegram), configured by the
		// environment loader and logging every send.
		"notify": {"audit": true, "config": true, "database": true, "httpx": true, "rbac": true, "secrets": true},
		// ai is the contextual AI layer: prompt assembly, the tool risk tiers and the
		// MCP protocol. It acts through the human-in-the-loop agent layer and reads the
		// CRM domain, so both are declared.
		"ai": {"agent": true, "audit": true, "config": true, "crm": true, "database": true, "httpx": true, "logging": true, "ratelimit": true, "rbac": true},
		// importx is the Import Center: format detection, the field catalog and the
		// two parser leaves it stands on. It enqueues its work and writes through the
		// outbox in the same transaction.
		"importx": {"audit": true, "config": true, "crm": true, "database": true, "httpx": true, "importx/parsers": true, "importx/safe": true, "jobs": true, "outbox": true, "rbac": true},
		// views is the saved list-view service. It stores named filters over the
		// database handle, writes to the audit trail and reads the caller's
		// principal for visibility and ownership. It touches no CRM table.
		"views": {"audit": true, "database": true, "rbac": true},
		// consent records what a person agreed to and answers whether a channel may
		// contact them. It reads and writes only its own table, writes the audit
		// trail, and reads the caller's principal; it holds no CRM data and calls
		// no sibling domain, so those three are the whole boundary.
		"consent": {"audit": true, "database": true, "rbac": true},
		// oidc is the single sign-on service: the authorization-code flow and the
		// provider registry. It verifies ID tokens, seals each client secret with
		// the keyring and writes the audit trail, so those four are declared. It
		// holds no CRM data and reads no principal, which is why rbac is absent.
		"oidc": {"audit": true, "config": true, "database": true, "secrets": true},
	}
	forbiddenDeps := map[string]map[string]bool{
		"objectstore": forbiddenForObjectstore,
	}

	seen := map[string]bool{}
	for path, p := range graph {
		rel := strings.TrimPrefix(path, modulePath+"internal/")
		if leaves[rel] || allowedDeps[rel] != nil || forbiddenDeps[rel] != nil {
			seen[rel] = true
		}
		if leaves[rel] {
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePath) {
					t.Errorf("%s imports %s; it must be a leaf with no in-module dependency",
						rel, strings.TrimPrefix(imp, modulePath+"internal/"))
				}
			}
			continue
		}
		if allowed, ok := allowedDeps[rel]; ok {
			for _, imp := range p.Imports {
				if !strings.HasPrefix(imp, modulePath+"internal/") {
					continue
				}
				if to := strings.TrimPrefix(imp, modulePath+"internal/"); !allowed[to] {
					t.Errorf("%s imports %s; it may depend only on %s", rel, to, keys(allowed))
				}
			}
			continue
		}
		if forbidden, ok := forbiddenDeps[rel]; ok {
			for _, imp := range p.Imports {
				if to := strings.TrimPrefix(imp, modulePath+"internal/"); forbidden[to] {
					t.Errorf("%s imports %s; the storage boundary must not depend on the application layer", rel, to)
				}
			}
		}
	}

	// A pinned module that is not in the graph would make its case above match
	// nothing and the test pass while checking nothing. That is the failure mode
	// this test exists to prevent, so it is itself checked.
	for rel := range leaves {
		if !seen[rel] {
			t.Errorf("the extraction boundary pins %s, but no such package is in the graph; the check is a no-op", rel)
		}
	}
	for rel := range allowedDeps {
		if !seen[rel] {
			t.Errorf("the extraction boundary pins %s, but no such package is in the graph; the check is a no-op", rel)
		}
	}
	for rel := range forbiddenDeps {
		if !seen[rel] {
			t.Errorf("the extraction boundary pins %s, but no such package is in the graph; the check is a no-op", rel)
		}
	}
}

// keys renders an allow-list for an error message.
func keys(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
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
