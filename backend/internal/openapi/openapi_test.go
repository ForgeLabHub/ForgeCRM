package openapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geff1778/forgecrm/internal/api"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/openapi"
	"github.com/geff1778/forgecrm/internal/ratelimit"
)

// These tests are the contract. They are the reason the specification can be
// trusted: without them it is a document generated once and stale thereafter,
// which is worse than no document, because clients believe it.

// buildRouter returns the real router with no services attached.
//
// The route table is static, so a zero configuration is enough to enumerate it.
// If that ever stops being true these tests fail loudly rather than silently
// producing a partial document.
func buildRouter(t *testing.T) chi.Router {
	t.Helper()
	srv, err := api.New(&api.Deps{
		Config: &config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, ratelimit.NewMemoryStore(), httpx.TrustedProxies{})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	return srv.Router()
}

// generate produces the document in memory, exactly as the command does.
func generate(t *testing.T) []byte {
	t.Helper()

	routes, err := openapi.Routes(buildRouter(t))
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	perms, err := openapi.Permissions(
		filepath.Join("..", "api"),
		filepath.Join("..", "rbac"),
	)
	if err != nil {
		t.Fatalf("read permissions: %v", err)
	}
	perms = openapi.PrefixAPI(perms)

	doc, err := openapi.Build(routes, perms, api.APIContract(), openapi.Info{
		Title:       "ForgeCRM API",
		Version:     "0.1.0",
		Description: openapi.DefaultDescription,
	}, []openapi.Server{
		{URL: "/", Description: "The server this document was generated from."},
	})
	if err != nil {
		t.Fatalf("build document: %v", err)
	}
	return doc
}

// TestCommittedSpecMatchesTheRouter is the drift guard.
//
// If a route is added, renamed or removed without regenerating
// docs/api/openapi.json, this fails and says how to fix it. That is the whole
// point of generating the document: a client can trust it, because a mismatch
// cannot be merged.
func TestCommittedSpecMatchesTheRouter(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "api", "openapi.json"))
	if err != nil {
		t.Fatalf("read committed specification: %v\n"+
			"If the file does not exist, run `go run ./cmd/openapi-gen` and commit it.", err)
	}

	generated := generate(t)

	if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(generated)) {
		t.Errorf("docs/api/openapi.json is out of date: the API and the specification disagree.\n"+
			"Run `go run ./cmd/openapi-gen` and commit the result.\n"+
			"committed %d bytes, generated %d bytes", len(committed), len(generated))
	}
}

// TestEveryRouteIsDocumented is a direct assertion, independent of the file.
//
// The previous test compares two generated values, so it would still pass if the
// generator itself dropped routes. This one compares the document against the
// router directly.
func TestEveryRouteIsDocumented(t *testing.T) {
	routes, err := openapi.Routes(buildRouter(t))
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(generate(t), &doc); err != nil {
		t.Fatal(err)
	}

	for _, rt := range routes {
		item, ok := doc.Paths[rt.Path]
		if !ok {
			t.Errorf("%s is served but absent from the specification", rt.Key())
			continue
		}
		if _, ok := item[strings.ToLower(rt.Method)]; !ok {
			t.Errorf("%s is served but absent from the specification", rt.Key())
		}
	}
}

// TestNoRouteIsInvented is the converse: the document must not describe an
// operation the server does not serve.
//
// A specification that over-promises is as damaging as one that under-promises,
// because a generated client will call an endpoint that returns 404.
func TestNoRouteIsInvented(t *testing.T) {
	routes, err := openapi.Routes(buildRouter(t))
	if err != nil {
		t.Fatal(err)
	}
	served := map[string]bool{}
	for _, rt := range routes {
		served[rt.Key()] = true
	}

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(generate(t), &doc); err != nil {
		t.Fatal(err)
	}

	for path, item := range doc.Paths {
		for method := range item {
			key := strings.ToUpper(method) + " " + path
			if !served[key] {
				t.Errorf("the specification describes %s, which the router does not serve", key)
			}
		}
	}
}

// TestEveryRouteDeclaresItsAccessRequirement enforces the rule that makes the
// permission section trustworthy.
//
// A route must either name the permission it requires or be listed in
// openapi.Public with a reason. The alternative — defaulting a missing annotation
// to "no permission" — would let a new authenticated endpoint appear in the
// document with no access statement at all, and a reader would have to guess.
func TestEveryRouteDeclaresItsAccessRequirement(t *testing.T) {
	routes, err := openapi.Routes(buildRouter(t))
	if err != nil {
		t.Fatal(err)
	}
	perms, err := openapi.Permissions(filepath.Join("..", "api"), filepath.Join("..", "rbac"))
	if err != nil {
		t.Fatal(err)
	}
	perms = openapi.PrefixAPI(perms)

	for _, rt := range routes {
		key := rt.Key()
		perm := perms[key]
		_, isAnonymous := openapi.Anonymous[key]
		_, isSessionOnly := openapi.SessionOnly[key]

		if perm == "" && !isAnonymous && !isSessionOnly {
			t.Errorf("%s declares no access requirement: give it a permission in its route "+
				"registration, or list it in openapi.Anonymous or openapi.SessionOnly "+
				"with a reason", key)
		}
		if perm != "" && (isAnonymous || isSessionOnly) {
			t.Errorf("%s requires %q and is also listed as needing no permission; "+
				"one of the two is wrong", key, perm)
		}
		if isAnonymous && isSessionOnly {
			t.Errorf("%s is listed as both anonymous and session-only", key)
		}
	}
}

// TestPermissionsAreResolvedToStringValues checks that the document states a
// permission a client can act on, not a Go identifier.
//
// "PermContactsRead" tells a client nothing. "contacts.read" is the value the
// RBAC layer checks. If the parser fails to resolve a constant it returns the
// identifier unchanged, which this catches.
func TestPermissionsAreResolvedToStringValues(t *testing.T) {
	perms, err := openapi.Permissions(filepath.Join("..", "api"), filepath.Join("..", "rbac"))
	if err != nil {
		t.Fatal(err)
	}
	perms = openapi.PrefixAPI(perms)

	resolved := 0
	for key, perm := range perms {
		if perm == "" {
			continue
		}
		if strings.HasPrefix(perm, "Perm") {
			t.Errorf("%s reports the identifier %q instead of its string value; "+
				"the rbac constant parser is not resolving it", key, perm)
			continue
		}
		if !strings.Contains(perm, ".") {
			t.Errorf("%s reports %q, which is not a dotted permission name", key, perm)
			continue
		}
		resolved++
	}
	if resolved < 100 {
		t.Errorf("only %d routes declare a permission; expected well over 100, "+
			"so the parser is probably reading the wrong package or the wrong functions", resolved)
	}
}

// TestAnonymousRoutesDoNotInheritSessionSecurity guards a subtle OpenAPI rule.
//
// An operation without a `security` member inherits the document-level
// requirement. If a public route omitted it, a generated client would send a
// session cookie to the sign-in endpoint, and a reader would conclude that sign-in
// requires a session. The route must carry an explicit empty list.
func TestAnonymousRoutesDoNotInheritSessionSecurity(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Security *[]map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(generate(t), &doc); err != nil {
		t.Fatal(err)
	}

	for key := range openapi.Anonymous {
		method, path, ok := strings.Cut(key, " ")
		if !ok {
			t.Fatalf("malformed Anonymous key %q", key)
		}
		item, ok := doc.Paths[path]
		if !ok {
			t.Errorf("%s is listed anonymous but absent from the specification", key)
			continue
		}
		op, ok := item[strings.ToLower(method)]
		if !ok {
			t.Errorf("%s is listed anonymous but absent from the specification", key)
			continue
		}
		if op.Security == nil {
			t.Errorf("%s is anonymous but omits `security`, so it inherits the session "+
				"requirement; set an explicit empty list", key)
			continue
		}
		if len(*op.Security) != 0 {
			t.Errorf("%s is anonymous but declares security requirements %v", key, *op.Security)
		}
	}
}

// TestSessionRoutesInheritSecurity is the converse: a protected operation must
// not silently become anonymous by carrying an empty list.
func TestSessionRoutesInheritSecurity(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Security *[]map[string][]string `json:"security"`
		} `json:"paths"`
		Security []map[string][]string `json:"security"`
	}
	if err := json.Unmarshal(generate(t), &doc); err != nil {
		t.Fatal(err)
	}

	if len(doc.Security) == 0 {
		t.Fatal("the document declares no global security requirement")
	}

	for path, item := range doc.Paths {
		for method, op := range item {
			key := strings.ToUpper(method) + " " + path
			if _, isAnonymous := openapi.Anonymous[key]; isAnonymous {
				continue
			}
			if op.Security != nil && len(*op.Security) == 0 {
				t.Errorf("%s is not anonymous but declares an empty security list, "+
					"which makes it callable without a session", key)
			}
		}
	}
}

// TestPathParametersAreTyped checks that identifiers are declared as UUIDs.
//
// A client generating types from a document that calls every id a string loses
// the ability to validate one before sending it, and discovers the real type from
// a 500 instead of a 422.
func TestPathParametersAreTyped(t *testing.T) {
	routes, err := openapi.Routes(buildRouter(t))
	if err != nil {
		t.Fatal(err)
	}

	untyped := 0
	for _, rt := range routes {
		for _, name := range pathParamNames(rt.Path) {
			schema := openapi.PathParamSchema(name)
			if _, ok := schema["format"]; !ok {
				untyped++
				t.Logf("%s: path parameter %q has no format", rt.Key(), name)
			}
		}
	}
	if untyped > 0 {
		t.Errorf("%d path parameters are untyped; declare a format so clients can validate them", untyped)
	}
}

// TestIdempotencyKeyIsDocumentedOnMutations checks the specification describes the
// header the middleware actually accepts, and only where it is accepted.
//
// Documenting it on a read would tell a client to send a header the server ignores;
// omitting it from a mutation would hide a real capability. Both are contract bugs,
// so both directions are asserted.
func TestIdempotencyKeyIsDocumentedOnMutations(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(generate(t), &doc); err != nil {
		t.Fatal(err)
	}

	hasKey := func(params []struct {
		Name string `json:"name"`
		In   string `json:"in"`
	}) bool {
		for _, p := range params {
			if p.Name == "Idempotency-Key" && p.In == "header" {
				return true
			}
		}
		return false
	}

	// The middleware is mounted on every authenticated group, so the header belongs on
	// every mutation that has a caller to scope it to. An anonymous route has no
	// principal, so the server cannot honour a key there and must not document one.
	anonymous := map[string]bool{}
	for key := range openapi.Anonymous {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) == 2 {
			anonymous[strings.ToLower(parts[1])] = true
		}
	}

	mutating := map[string]bool{"post": true, "put": true, "patch": true, "delete": true}
	for path, ops := range doc.Paths {
		// A path is anonymous only when every operation on it is; the auth group mixes
		// an anonymous login with session-only siblings under one prefix.
		pathIsAnonymous := anonymous[path]
		for method, op := range ops {
			switch {
			case mutating[method] && !pathIsAnonymous && !hasKey(op.Parameters):
				t.Errorf("%s %s changes state but does not document Idempotency-Key", method, path)
			case mutating[method] && pathIsAnonymous && hasKey(op.Parameters):
				t.Errorf("%s %s is anonymous but documents Idempotency-Key", method, path)
			case !mutating[method] && hasKey(op.Parameters):
				t.Errorf("%s %s is a read but documents Idempotency-Key", method, path)
			}
		}
	}
}

// TestOperationIDsAreUnique protects the generated SDK.
//
// Most code generators name a method after the operationId. Duplicates silently
// produce two methods with the same name, and one overwrites the other, so a whole
// endpoint disappears from the client with no error.
func TestOperationIDsAreUnique(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(generate(t), &doc); err != nil {
		t.Fatal(err)
	}

	seen := map[string]string{}
	for path, item := range doc.Paths {
		for method, op := range item {
			if op.OperationID == "" {
				t.Errorf("%s %s has no operationId", strings.ToUpper(method), path)
				continue
			}
			if prev, dup := seen[op.OperationID]; dup {
				t.Errorf("operationId %q is used by both %s and %s %s",
					op.OperationID, prev, strings.ToUpper(method), path)
			}
			seen[op.OperationID] = strings.ToUpper(method) + " " + path
		}
	}
}

// TestGenerationIsDeterministic is what makes the committed file diffable.
//
// A document that changes on every run cannot be committed, so it would be
// generated at build time and never reviewed. Two generations in one process must
// be byte-identical.
func TestGenerationIsDeterministic(t *testing.T) {
	first := generate(t)
	for i := 0; i < 3; i++ {
		if again := generate(t); !bytes.Equal(first, again) {
			t.Fatalf("generation %d differs from the first; the output is not stable", i+1)
		}
	}
}

// TestSpecIsValidJSON catches an encoding failure that would break every client.
func TestSpecIsValidJSON(t *testing.T) {
	doc := generate(t)
	if !json.Valid(doc) {
		t.Fatal("the generated document is not valid JSON")
	}

	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"openapi", "info", "paths", "components", "tags"} {
		if _, ok := root[field]; !ok {
			t.Errorf("the document is missing the required top-level field %q", field)
		}
	}
	if v, _ := root["openapi"].(string); !strings.HasPrefix(v, "3.1") {
		t.Errorf("openapi version is %q, expected 3.1.x", v)
	}
}

// TestRoutesAreSorted keeps the document readable and the diff small.
//
// The order is by path, then by method within a path — not by the "METHOD path"
// key, which would group all GETs together and scatter the methods of one
// resource across the file.
func TestRoutesAreSorted(t *testing.T) {
	routes, err := openapi.Routes(buildRouter(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) < 2 {
		t.Fatal("too few routes to test ordering")
	}
	for i := 1; i < len(routes); i++ {
		prev, cur := routes[i-1], routes[i]
		switch {
		case prev.Path > cur.Path:
			t.Fatalf("routes are not sorted by path: %s appears before %s", prev.Path, cur.Path)
		case prev.Path == cur.Path && prev.Method > cur.Method:
			t.Fatalf("methods within %s are not sorted: %s appears before %s",
				prev.Path, prev.Method, cur.Method)
		}
	}
}

// pathParamNames extracts {name} segments.
func pathParamNames(path string) []string {
	var out []string
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			return out
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			return out
		}
		out = append(out, path[start+1:start+end])
		path = path[start+end+1:]
	}
}
