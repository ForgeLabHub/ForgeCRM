// Package openapi derives the API specification from the running router.
//
// The paths and methods are not written by hand. They are read from the same chi
// router the server serves, so the document cannot list a route that does not
// exist or miss one that does. A hand-written specification drifts on the first
// route addition, and a specification that describes a system that no longer
// exists is worse than none, because clients trust it.
//
// Two things the router cannot report are merged in:
//
//   - The permission each route requires. chi.Walk exposes only the innermost
//     handler, so the requirement is not recoverable at runtime. It is parsed
//     from the route registration in source instead, which is the same statement
//     that installs the middleware, so the two cannot disagree.
//   - Request and response schemas. These are declared below, next to the routes
//     they describe.
//
// A route with no permission annotation and no public marker is a test failure,
// not a silent gap.
package openapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Route is one method on one path, as the router actually serves it.
type Route struct {
	Method string
	Path   string
}

// Key identifies a route in the annotation tables.
func (r Route) Key() string { return r.Method + " " + r.Path }

// Routes walks a chi router and returns every registered route.
//
// chi.Walk reports the full path including any mounted prefix, so the result is
// what a client calls rather than what a handler declares. It also reports paths
// with a trailing slash, which chi treats as equivalent to the bare path; the
// slash is stripped so the document shows one canonical form.
func Routes(r chi.Routes) ([]Route, error) {
	var routes []Route

	err := chi.Walk(r, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi registers a catch-all for its own 404 and 405 handling. Those are
		// not API operations; the document describes them once as shared
		// responses.
		if strings.Contains(path, "/*") {
			return nil
		}
		if len(path) > 1 {
			path = strings.TrimSuffix(path, "/")
		}
		routes = append(routes, Route{Method: strings.ToUpper(method), Path: path})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk router: %w", err)
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes, nil
}

// Group names the logical area a path belongs to. It is derived from the path, so
// a new route is grouped without anyone remembering to say so. A route that lands
// in "Other" is a signal that its path is surprising.
func Group(path string) string {
	switch {
	case path == "/health", path == "/ready", path == "/metrics", strings.HasPrefix(path, "/api/v1/system"):
		return "System"
	case strings.HasPrefix(path, "/api/v1/auth"):
		return "Authentication"
	case strings.HasPrefix(path, "/api/v1/users"),
		strings.HasPrefix(path, "/api/v1/roles"),
		strings.HasPrefix(path, "/api/v1/permissions"),
		strings.HasPrefix(path, "/api/v1/teams"),
		strings.HasPrefix(path, "/api/v1/organizations"):
		return "Users and permissions"
	case strings.HasPrefix(path, "/api/v1/imports"), strings.HasPrefix(path, "/api/v1/exports"):
		return "Import and export"
	case strings.HasPrefix(path, "/api/v1/migrations"):
		return "Migrations"
	case strings.HasPrefix(path, "/api/v1/integrations"):
		return "Integrations"
	case strings.HasPrefix(path, "/api/v1/events"), strings.HasPrefix(path, "/api/v1/webhooks"):
		return "Events"
	case strings.HasPrefix(path, "/api/v1/jobs"):
		return "Jobs"
	case strings.HasPrefix(path, "/api/v1/audit"), strings.HasPrefix(path, "/api/v1/admin"):
		return "Audit and diagnostics"
	case strings.HasPrefix(path, "/api/v1/ai"), path == "/api/v1/mcp", strings.HasPrefix(path, "/api/v1/mcp/"):
		return "AI and MCP"
	case strings.HasPrefix(path, "/api/v1/notifications"):
		return "Notifications"
	case strings.HasPrefix(path, "/api/v1/"):
		return "CRM"
	default:
		return "Other"
	}
}

// TagFor turns a group name into an OpenAPI tag.
func TagFor(group string) string {
	return strings.ReplaceAll(strings.ToLower(group), " ", "-")
}

// Info describes the document itself.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// DefaultDescription states the conventions a client needs before reading any
// single endpoint, so they are not repeated in seventy operations.
//
// It lives here rather than in the generator so the command and the contract test
// produce byte-identical documents. If the two used different text, the drift test
// would fail for a reason that has nothing to do with the API.
const DefaultDescription = "ForgeCRM REST API.\n\n" +
	"## Authentication\n\n" +
	"Sign in with POST /api/v1/auth/login, which sets the session cookie\n" +
	"`forgecrm_session`. State-changing requests must also send the CSRF token\n" +
	"returned by GET /api/v1/auth/me in the `X-CSRF-Token` header.\n\n" +
	"## Errors\n\n" +
	"Every failure returns RFC 9457 Problem Details with `type`, `title`,\n" +
	"`status`, `detail`, `instance` and `retryable`, and repeats the\n" +
	"machine-readable code under `error`. Branch on `error.code`; `title` is prose\n" +
	"and may change. See docs/api/errors.md.\n\n" +
	"## Collections\n\n" +
	"List operations accept `limit`, `offset`, `q`, `sort` and `order`, and return\n" +
	"`{items, total, limit, offset}`. `limit` is clamped by the server, so a large\n" +
	"value is not an error.\n\n" +
	"## Long operations\n\n" +
	"Imports, migrations and exports return a job identifier instead of blocking\n" +
	"the request. Poll GET /api/v1/jobs/{id}.\n\n" +
	"## Permissions\n\n" +
	"Each operation states the permission it requires. A caller without it receives\n" +
	"403. Authorisation is enforced on the server; the interface only decides what\n" +
	"to display."

// Server is an OpenAPI server entry.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Anonymous lists routes that require no session at all.
//
// It is a small, closed list. A route that is in none of Anonymous, SessionOnly or
// the parsed permission table fails the contract test, so an unauthenticated
// endpoint cannot appear by accident.
//
// These routes carry an explicit empty `security` list in the document, because in
// OpenAPI an absent `security` inherits the document-level requirement. Omitting it
// would tell a generated client to send a session cookie to the sign-in endpoint.
var Anonymous = map[string]string{
	"GET /health":                       "Liveness probe. Returns 200 while the process is running.",
	"GET /ready":                        "Readiness probe. Returns 200 only when the database is reachable.",
	"GET /metrics":                      "Prometheus metrics in the text exposition format. Reachable without a session, so it is closed unless `FORGECRM_METRICS_TOKEN` is set; a scrape must present that token in the `X-Metrics-Token` header. With no token configured it answers 503 `disabled`.",
	"POST /api/v1/auth/login":           "Sign in and receive a session cookie.",
	"POST /api/v1/auth/password/forgot": "Request a password reset. The response is identical whether or not the account exists.",
	"POST /api/v1/auth/password/reset":  "Complete a password reset with a single-use token.",
	"POST /api/v1/auth/mfa/verify":      "Complete sign-in by presenting a second factor.",
	"GET /api/v1/auth/oidc/providers":   "The single sign-on providers a signed-out visitor may choose from. Returns an empty list when SSO is not configured.",
	"GET /api/v1/auth/oidc/start":       "Begin single sign-on: redirects the browser to the configured identity provider.",
	"GET /api/v1/auth/oidc/callback":    "Complete single sign-on: verifies the provider's response and establishes a session, then redirects back into the application.",
}

// SessionOnly lists routes that require a signed-in session but no specific
// permission, because they act on the caller's own account.
//
// These are distinct from Anonymous: they inherit the document-level security
// requirement, so a client knows to send a session. Conflating the two, as an
// earlier revision did, produced a document that told clients these endpoints were
// callable anonymously while the server answered 401.
var SessionOnly = map[string]string{
	"GET /api/v1/auth/me":               "The current caller and their CSRF token.",
	"POST /api/v1/auth/logout":          "Sign out and revoke the current session.",
	"GET /api/v1/auth/sessions":         "The caller's own active sessions.",
	"GET /api/v1/auth/identities":       "The caller's own linked single sign-on identities.",
	"DELETE /api/v1/auth/sessions/{id}": "Revoke one of the caller's own sessions.",
	"POST /api/v1/auth/password/change": "Change the caller's own password.",
	"POST /api/v1/auth/mfa/enroll":      "Begin enrolling a second factor for the caller.",
	"POST /api/v1/auth/mfa/confirm":     "Confirm a second factor enrolment.",
	"POST /api/v1/auth/mfa/disable":     "Disable the caller's second factor.",
	"POST /api/v1/auth/step-up":         "Re-present a factor to refresh the second-factor window a destructive agent decision requires.",
	"GET /api/v1/system/info":           "Build and version information.",
	"GET /api/v1/system/capabilities":   "Advertised capabilities of the running instance.",
	"GET /api/v1/simulation/catalog":    "The scenario catalog the Simulation Lab can compute. Static metadata about the engine, exposing no tenant data.",
	"GET /api/v1/forecast/catalog":      "The forecasting methods the engine can compute. Static metadata about the engine, exposing no tenant data.",
}

// Public is the union of Anonymous and SessionOnly, for callers that only need to
// know whether a permission is required.
//
// Deprecated: use Anonymous or SessionOnly, which distinguish whether a session is
// needed. This exists so the distinction can be introduced without rewriting every
// caller at once.
func Public(key string) (string, bool) {
	if d, ok := Anonymous[key]; ok {
		return d, true
	}
	d, ok := SessionOnly[key]
	return d, ok
}

// document is the OpenAPI document root.
type document struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Tags       []tag                 `json:"tags"`
	Paths      map[string]pathItem   `json:"paths"`
	Components components            `json:"components"`
	Security   []map[string][]string `json:"security"`
}

type info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type tag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type pathItem map[string]operation

type operation struct {
	OperationID string                 `json:"operationId"`
	Tags        []string               `json:"tags"`
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Parameters  []parameter            `json:"parameters,omitempty"`
	RequestBody *requestBody           `json:"requestBody,omitempty"`
	Responses   map[string]response    `json:"responses"`
	Security    *[]map[string][]string `json:"security,omitempty"`
	Deprecated  bool                   `json:"deprecated,omitempty"`
}

type parameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Required    bool           `json:"required,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
}

type requestBody struct {
	Required    bool                 `json:"required"`
	Description string               `json:"description,omitempty"`
	Content     map[string]mediaType `json:"content"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type mediaType struct {
	Schema map[string]any `json:"schema"`
}

type components struct {
	Schemas         map[string]any `json:"schemas"`
	SecuritySchemes map[string]any `json:"securitySchemes"`
}

// Build assembles the document.
//
// perms maps a route key to the permission it requires; it comes from parsing the
// route registrations in source. A route missing from both perms and Public makes
// Build return an error rather than emitting an undocumented operation.
//
// c supplies the request and response types. It is checked against the route table
// in both directions: a key it names that no route serves, or a route it names with
// a type the router cannot reach, stops generation rather than producing a document
// that describes an API other than the one that exists.
func Build(routes []Route, perms map[string]string, c Contract, in Info, servers []Server) ([]byte, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes: refusing to emit an empty specification")
	}

	served := map[string]bool{}
	for _, rt := range routes {
		served[rt.Key()] = true
	}
	for _, key := range sortedKeys(c.keys()) {
		if !served[key] {
			return nil, fmt.Errorf(
				"the API contract names %s, which the router does not serve: "+
					"remove the entry or fix the route", key)
		}
	}

	reg := newRegistry(c.TypeNames, c.discover())
	paths := map[string]pathItem{}
	tagsSeen := map[string]bool{}

	for _, rt := range routes {
		group := Group(rt.Path)
		tagsSeen[group] = true

		op, err := operationFor(rt, group, perms, c, reg)
		if err != nil {
			return nil, err
		}
		if paths[rt.Path] == nil {
			paths[rt.Path] = pathItem{}
		}
		method := strings.ToLower(rt.Method)
		if _, dup := paths[rt.Path][method]; dup {
			return nil, fmt.Errorf("duplicate route %s", rt.Key())
		}
		paths[rt.Path][method] = op
	}

	tags := make([]tag, 0, len(tagsSeen))
	for name := range tagsSeen {
		tags = append(tags, tag{Name: TagFor(name), Description: tagDescription(name)})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })

	// The registry is closed only after every operation has declared its types, so a
	// component referenced by any endpoint is present.
	componentSchemas := schemas()
	for name, s := range reg.build() {
		componentSchemas[name] = s
	}

	doc := document{
		OpenAPI: "3.1.0",
		Info:    in,
		Servers: servers,
		Tags:    tags,
		Paths:   paths,
		Components: components{
			Schemas: componentSchemas,
			SecuritySchemes: map[string]any{
				"sessionCookie": map[string]any{
					"type": "apiKey",
					"in":   "cookie",
					"name": "forgecrm_session",
					"description": "Opaque session cookie set by POST /api/v1/auth/login. " +
						"State-changing requests must also send the X-CSRF-Token header.",
				},
			},
		},
		Security: []map[string][]string{{"sessionCookie": {}}},
	}

	return marshalDeterministic(doc)
}

// marshalDeterministic encodes with sorted map keys and a trailing newline, so the
// committed file is byte-stable across runs and machines. A generated file that
// changes on every run cannot be committed and diffed, and a specification nobody
// diffs is a specification nobody maintains. encoding/json sorts map keys, which
// is what makes this work.
func marshalDeterministic(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// operationFor builds one operation.
func operationFor(rt Route, group string, perms map[string]string, c Contract, reg *registry) (operation, error) {
	perm := perms[rt.Key()]
	_, isAnonymous := Anonymous[rt.Key()]
	_, isSessionOnly := SessionOnly[rt.Key()]

	if perm == "" && !isAnonymous && !isSessionOnly {
		return operation{}, fmt.Errorf(
			"%s is neither permission-annotated nor listed in openapi.Anonymous or "+
				"openapi.SessionOnly: add the permission in its route registration, or "+
				"declare its access level with a reason", rt.Key())
	}
	if perm != "" && (isAnonymous || isSessionOnly) {
		return operation{}, fmt.Errorf(
			"%s declares the permission %q and is also listed as needing no permission; "+
				"one of the two is wrong", rt.Key(), perm)
	}
	if isAnonymous && isSessionOnly {
		return operation{}, fmt.Errorf(
			"%s is listed as both anonymous and session-only; these are mutually exclusive", rt.Key())
	}

	desc := accessDescription(rt, perm)

	summary := summaryFor(rt)
	if perm == "" {
		// The access tables already state what the operation does in one line,
		// which reads better than the mechanical verb phrase ("Create a login").
		summary = accessDescription(rt, perm)
	}

	op := operation{
		OperationID: operationID(rt),
		Tags:        []string{TagFor(group)},
		Summary:     summary,
		Description: desc,
		Responses:   responsesFor(rt, isAnonymous, c, reg),
	}

	if bodyType, ok := c.BodyType(rt.Key()); ok {
		op.RequestBody = requestBodyFor(rt.Key(), bodyType, c, reg)
	}

	for _, name := range pathParams(rt.Path) {
		op.Parameters = append(op.Parameters, parameter{
			Name: name, In: "path", Required: true, Schema: pathParamSchema(name),
		})
	}

	// Collection query parameters: the shared pagination contract plus anything the
	// resource declares.
	//
	// A route whose success body is not JSON is not a collection: a metrics scrape or
	// a file download has no pages to skip. Adding `limit` and `offset` to one would
	// document a filter the server ignores, which is the kind of promise a generated
	// client acts on.
	_, isStreaming := c.StreamingSuccessFor(rt.Key())
	_, isText := c.TextSuccessFor(rt.Key())
	if rt.Method == http.MethodGet && !strings.Contains(rt.Path, "{") && !isStreaming && !isText {
		op.Parameters = append(op.Parameters, listParams()...)
	}
	// A mutating request may carry an idempotency key. It is documented on every
	// authenticated mutation because the middleware accepts it on each of them, and
	// deliberately omitted from an anonymous one: the key is scoped to the resolved
	// caller, and a route with no session has no caller to scope it to. Documenting it
	// there would promise a guarantee the server does not make.
	if isMutationMethod(rt.Method) && !isAnonymous {
		op.Parameters = append(op.Parameters, idempotencyParam())
	}
	for _, q := range c.QueryParams[rt.Key()] {
		op.Parameters = append(op.Parameters, parameter{
			Name: q.Name, In: "query", Required: q.Required,
			Schema: q.Schema, Description: q.Description,
		})
	}
	// A declared sort allowlist becomes an enum, so a client cannot send a column the
	// server will reject, and the accepted set is visible rather than discovered.
	if cols := c.SortColumns[rt.Key()]; len(cols) > 0 {
		op.Parameters = addSortEnum(op.Parameters, cols)
	}

	// An anonymous operation must override the document-level security with an
	// explicit empty list: in OpenAPI an absent `security` inherits the global
	// requirement, so omitting it would tell a generated client to send a session
	// cookie to the sign-in endpoint. The field is a pointer because a plain slice
	// cannot distinguish "empty" from "absent". A session-only operation inherits
	// the requirement, which is what we want.
	if isAnonymous {
		none := []map[string][]string{}
		op.Security = &none
	}
	return op, nil
}

// addSortEnum narrows the shared `sort` parameter to the columns the resource allows.
func addSortEnum(params []parameter, cols []string) []parameter {
	sorted := append([]string(nil), cols...)
	sort.Strings(sorted)
	for i := range params {
		if params[i].Name == "sort" && params[i].In == "query" {
			params[i].Schema = map[string]any{"type": "string", "enum": sorted}
			params[i].Description = "Field to sort by. Any other value is rejected."
			return params
		}
	}
	return params
}

// isMutationMethod reports whether a method changes state and so may carry an
// idempotency key. It matches the middleware's own set: the two must agree, or the
// specification would document a header the server ignores.
func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// requestBodyFor builds a request body from the real type the handler decodes.
func requestBodyFor(key string, t reflect.Type, c Contract, reg *registry) *requestBody {
	name, ok := c.BodyNames[key]
	if !ok {
		name = componentName(t, reg, modeRequest)
	}
	reg.addRequest(name, t, c.requiredForBody(name))

	desc := requiredDescription(t)
	if d, ok := c.BodyDescriptions[key]; ok {
		// A route-level description adds to, rather than replaces, the rule derived
		// from the type: one route may need both.
		if desc != "" {
			desc = desc + "\n\n" + d
		} else {
			desc = d
		}
	}
	return &requestBody{
		Required:    true,
		Description: desc,
		Content: map[string]mediaType{
			"application/json": {Schema: ref(name)},
		},
	}
}

// requiredDescription states any rule that a required-field list cannot express.
//
// Some validations are not "this field must be present" but "one of these must be",
// or "these two go together". Encoding that as a required list would be wrong, and
// leaving it out would hide a rule a client must satisfy, so it is stated in prose.
func requiredDescription(t reflect.Type) string {
	switch t.Name() {
	case "ContactInput":
		return "At least one of `first_name`, `last_name` or `email` is required. " +
			"`external_source` and `external_id` must be supplied together or not at all."
	case "ActivityInput":
		return "The activity must reference at least one of `account_id`, `contact_id`, " +
			"`deal_id` or `ticket_id`."
	default:
		return ""
	}
}

// accessDescription returns the description text: the hand-written line from the
// access table, or the permission requirement, or both.
func accessDescription(rt Route, perm string) string {
	text := ""
	if d, ok := Anonymous[rt.Key()]; ok {
		text = d
	} else if d, ok := SessionOnly[rt.Key()]; ok {
		text = d
	}

	switch {
	case perm == "":
		return text
	case text == "":
		return "Requires the `" + perm + "` permission."
	default:
		return strings.TrimSpace(text + "\n\nRequires the `" + perm + "` permission.")
	}
}

// operationID is derived from the method and path, so it is stable and unique
// without a hand-maintained registry that can fall out of step.
func operationID(rt Route) string {
	parts := strings.Split(strings.Trim(rt.Path, "/"), "/")
	var b strings.Builder
	b.WriteString(strings.ToLower(rt.Method))
	for _, p := range parts {
		if p == "" || p == "api" || p == "v1" {
			continue
		}
		if strings.HasPrefix(p, "{") {
			name := strings.Trim(p, "{}")
			b.WriteString("By" + strings.ToUpper(name[:1]) + name[1:])
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// summaryFor describes the operation in one line, from its shape. It is
// deliberately mechanical: a hand-written summary per route drifts, and a wrong
// summary is worse than a plain one.
func summaryFor(rt Route) string {
	seg := lastStaticSegment(rt.Path)
	switch {
	case rt.Method == http.MethodGet && strings.Contains(rt.Path, "{"):
		return "Get one " + seg + " by id"
	case rt.Method == http.MethodGet:
		return "List " + plural(seg)
	case rt.Method == http.MethodPost && strings.Contains(rt.Path, "{"):
		return "Perform an action on " + withArticle(seg)
	case rt.Method == http.MethodPost:
		return "Create " + withArticle(seg)
	case rt.Method == http.MethodPut:
		return "Replace " + withArticle(seg)
	case rt.Method == http.MethodPatch:
		return "Update " + withArticle(seg)
	case rt.Method == http.MethodDelete:
		return "Archive or delete " + withArticle(seg)
	default:
		return rt.Method + " " + rt.Path
	}
}

// withArticle prefixes the indefinite article, so a generated summary reads as
// English rather than "Create a account". The vowel test approximates the sound
// rule and is deliberately narrow: it covers the words this API uses and does not
// attempt general English, where the choice depends on pronunciation.
func withArticle(word string) string {
	if word == "" {
		return "a resource"
	}
	switch word[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an " + word
	default:
		return "a " + word
	}
}

// plural returns the plural form for list operations. Irregular forms are listed
// rather than guessed, because a wrong summary is worse than a plain one.
func plural(word string) string {
	switch word {
	case "":
		return "records"
	case "activity":
		return "activities"
	case "status":
		return "statuses"
	}
	if strings.HasSuffix(word, "s") || strings.HasSuffix(word, "x") ||
		strings.HasSuffix(word, "ch") || strings.HasSuffix(word, "sh") {
		return word + "es"
	}
	if strings.HasSuffix(word, "y") && len(word) > 1 {
		switch word[len(word)-2] {
		case 'a', 'e', 'i', 'o', 'u':
			return word + "s"
		default:
			return word[:len(word)-1] + "ies"
		}
	}
	return word + "s"
}

// lastStaticSegment returns the trailing non-parameter path segment, singularised
// so "accounts" reads as "account".
func lastStaticSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p == "" || strings.HasPrefix(p, "{") {
			continue
		}
		switch p {
		case "api", "v1", "system", "admin":
			continue
		}
		if strings.HasSuffix(p, "ies") {
			return strings.TrimSuffix(p, "ies") + "y"
		}
		if strings.HasSuffix(p, "sses") || strings.HasSuffix(p, "shes") || strings.HasSuffix(p, "ches") {
			return strings.TrimSuffix(p, "es")
		}
		if strings.HasSuffix(p, "s") && !strings.HasSuffix(p, "ss") {
			return strings.TrimSuffix(p, "s")
		}
		return p
	}
	return "resource"
}

func tagDescription(name string) string {
	switch name {
	case "System":
		return "Health, readiness, build information and advertised capabilities."
	case "Authentication":
		return "Sign-in, session management, password recovery and second factors."
	case "Users and permissions":
		return "Users, roles, permissions, teams and organizations."
	case "CRM":
		return "Contacts, accounts, leads, deals, pipelines, tasks, activities, tickets, documents, tags, custom fields, search and reports."
	case "Import and export":
		return "File and archive import, field mapping, dry runs, reports and exports."
	case "Migrations":
		return "Source database schema discovery, mapping, execution and reconciliation."
	case "Integrations":
		return "Integration hub: connections, configuration checks and synchronisation runs."
	case "Events":
		return "Inbound events, webhook subscriptions and delivery history."
	case "Jobs":
		return "Background jobs: status, cancellation and retry."
	case "Audit and diagnostics":
		return "Audit trail, application logs, metrics and diagnostics."
	case "AI and MCP":
		return "AI providers, AI tasks and the tool surface."
	case "Notifications":
		return "Email and Telegram notification channels."
	default:
		return "Operations not yet classified."
	}
}
