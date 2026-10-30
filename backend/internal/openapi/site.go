package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
)

// Site renders a single self-contained HTML reference from an OpenAPI document.
//
// It is a second view of the same document the generator already produces, not a
// second source of truth: the page is built by parsing the document and laying out
// what is there, so it cannot describe an endpoint the API does not serve. Nothing
// is fetched at view time — the stylesheet is inlined and the only script is a few
// lines of filter — so the page opens from disk, over a file share, or behind an
// air-gapped proxy, with no network and no CDN.
//
// The document is the authority for structure; this function only chooses how to
// present it. Where the document is silent (a tag it does not declare, a schema
// with no description) the page says so rather than inventing a value.
func Site(document []byte) ([]byte, error) {
	var d siteDoc
	dec := json.NewDecoder(bytes.NewReader(document))
	dec.UseNumber()
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("parse OpenAPI document: %w", err)
	}
	if d.OpenAPI == "" {
		return nil, fmt.Errorf("parse OpenAPI document: no `openapi` version; this is not an OpenAPI document")
	}
	return renderSite(&d)
}

type siteDoc struct {
	OpenAPI    string                        `json:"openapi"`
	Info       Info                          `json:"info"`
	Servers    []Server                      `json:"servers"`
	Tags       []tag                         `json:"tags"`
	Paths      map[string]map[string]*siteOp `json:"paths"`
	Components struct {
		Schemas map[string]*siteSchema `json:"schemas"`
	} `json:"components"`
	Security []map[string][]string `json:"security"`
}

type siteOp struct {
	OperationID string                  `json:"operationId"`
	Tags        []string                `json:"tags"`
	Summary     string                  `json:"summary"`
	Description string                  `json:"description"`
	Parameters  []siteParam             `json:"parameters"`
	RequestBody *siteRequestBody        `json:"requestBody"`
	Responses   map[string]siteResponse `json:"responses"`
	Security    *[]map[string][]string  `json:"security"`
}

type siteParam struct {
	Name        string      `json:"name"`
	In          string      `json:"in"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Schema      *siteSchema `json:"schema"`
}

type siteRequestBody struct {
	Required    bool                 `json:"required"`
	Description string               `json:"description"`
	Content     map[string]siteMedia `json:"content"`
}

type siteResponse struct {
	Description string               `json:"description"`
	Content     map[string]siteMedia `json:"content"`
}

type siteMedia struct {
	Schema *siteSchema `json:"schema"`
}

// typeNames is a JSON Schema `type`, which may be a single name or a list of
// names (e.g. ["string","null"] for a nullable field). Both spellings decode to a
// slice so the renderer has one shape to reason about.
type typeNames []string

func (t *typeNames) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*t = nil
		return nil
	}
	if trimmed[0] == '[' {
		var list []string
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		*t = list
		return nil
	}
	var one string
	if err := json.Unmarshal(trimmed, &one); err != nil {
		return err
	}
	*t = []string{one}
	return nil
}

// first returns the primary type name, ignoring "null", which is rendered
// separately as nullability.
func (t typeNames) first() string {
	for _, name := range t {
		if name != "null" {
			return name
		}
	}
	return ""
}

func (t typeNames) nullable() bool {
	for _, name := range t {
		if name == "null" {
			return true
		}
	}
	return false
}

type siteSchema struct {
	Ref         string                 `json:"$ref"`
	Type        typeNames              `json:"type"`
	Format      string                 `json:"format"`
	Description string                 `json:"description"`
	Enum        []any                  `json:"enum"`
	Properties  map[string]*siteSchema `json:"properties"`
	Required    []string               `json:"required"`
	Items       *siteSchema            `json:"items"`
	Nullable    bool                   `json:"nullable"`
	Default     any                    `json:"default"`
	Minimum     *float64               `json:"minimum"`
	Maximum     *float64               `json:"maximum"`
	OneOf       []*siteSchema          `json:"oneOf"`
	AnyOf       []*siteSchema          `json:"anyOf"`
	AllOf       []*siteSchema          `json:"allOf"`
}

// httpMethods is the fixed set, in the order operations are listed within a path.
var httpMethods = []string{"get", "post", "put", "patch", "delete"}

// siteEntry is one resolved operation: its method, path and body, ready to render.
type siteEntry struct {
	Method string
	Path   string
	Spec   *siteOp
	Anchor string
	Tag    string
}

func renderSite(d *siteDoc) ([]byte, error) {
	entries := collectOperations(d)
	byTag := groupByTag(d, entries)

	var b strings.Builder
	b.WriteString("<!doctype html>\n")
	b.WriteString(`<html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	fmt.Fprintf(&b, "<title>%s — API reference</title>\n", html.EscapeString(d.Info.Title))
	b.WriteString("<style>\n")
	b.WriteString(siteCSS)
	b.WriteString("</style>\n")
	b.WriteString("</head><body>\n")

	renderSidebar(&b, d, byTag)
	b.WriteString(`<main class="content">` + "\n")
	renderHeader(&b, d)
	renderTagSections(&b, byTag)
	renderSchemas(&b, d)
	b.WriteString("</main>\n")
	b.WriteString("<script>\n")
	b.WriteString(siteJS)
	b.WriteString("</script>\n")
	b.WriteString("</body></html>\n")

	return []byte(b.String()), nil
}

// collectOperations walks the paths into a flat, deterministically ordered list.
func collectOperations(d *siteDoc) []siteEntry {
	var entries []siteEntry
	for path, methods := range d.Paths {
		for _, method := range httpMethods {
			spec, ok := methods[method]
			if !ok || spec == nil {
				continue
			}
			tagName := "untagged"
			if len(spec.Tags) > 0 {
				tagName = spec.Tags[0]
			}
			entries = append(entries, siteEntry{
				Method: method,
				Path:   path,
				Spec:   spec,
				Anchor: opAnchor(method, path),
				Tag:    tagName,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return methodRank(entries[i].Method) < methodRank(entries[j].Method)
	})
	return entries
}

func methodRank(m string) int {
	for i, v := range httpMethods {
		if v == m {
			return i
		}
	}
	return len(httpMethods)
}

// groupByTag orders operations under the tags the document declares, so the page
// follows the document's own grouping. A tag an operation names but the document
// does not declare still gets a section, placed after the declared ones, rather
// than the operation being dropped.
func groupByTag(d *siteDoc, entries []siteEntry) []tagGroup {
	byTag := map[string][]siteEntry{}
	for _, e := range entries {
		byTag[e.Tag] = append(byTag[e.Tag], e)
	}

	var groups []tagGroup
	seen := map[string]bool{}
	for _, t := range d.Tags {
		seen[t.Name] = true
		if ops, ok := byTag[t.Name]; ok {
			groups = append(groups, tagGroup{Name: t.Name, Description: t.Description, Entries: ops})
		} else {
			groups = append(groups, tagGroup{Name: t.Name, Description: t.Description})
		}
	}
	var extra []string
	for name := range byTag {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		groups = append(groups, tagGroup{Name: name, Entries: byTag[name]})
	}
	return groups
}

type tagGroup struct {
	Name        string
	Description string
	Entries     []siteEntry
}

func renderSidebar(b *strings.Builder, d *siteDoc, groups []tagGroup) {
	b.WriteString(`<nav class="sidebar">` + "\n")
	fmt.Fprintf(b, `<div class="brand"><a href="#top">%s</a><span class="ver">%s</span></div>`+"\n",
		html.EscapeString(d.Info.Title), html.EscapeString(d.Info.Version))
	b.WriteString(`<input id="filter" type="search" placeholder="Filter operations…" aria-label="Filter operations">` + "\n")
	for _, g := range groups {
		if len(g.Entries) == 0 {
			continue
		}
		fmt.Fprintf(b, `<div class="nav-group" data-group>%s</div>`+"\n", html.EscapeString(g.Name))
		b.WriteString(`<ul class="nav-list">` + "\n")
		for _, e := range g.Entries {
			fmt.Fprintf(b, `<li data-op><a href="#%s"><span class="m m-%s">%s</span><span class="p">%s</span></a></li>`+"\n",
				e.Anchor, e.Method, strings.ToUpper(e.Method), html.EscapeString(e.Path))
		}
		b.WriteString("</ul>\n")
	}
	b.WriteString(`<div class="nav-group">Schemas</div>` + "\n")
	b.WriteString(`<ul class="nav-list">` + "\n")
	for _, name := range sortedKeys(d.Components.Schemas) {
		fmt.Fprintf(b, `<li data-op><a href="#%s">%s</a></li>`+"\n", schemaAnchor(name), html.EscapeString(name))
	}
	b.WriteString("</ul>\n")
	b.WriteString("</nav>\n")
}

func renderHeader(b *strings.Builder, d *siteDoc) {
	b.WriteString(`<header id="top">` + "\n")
	fmt.Fprintf(b, "<h1>%s</h1>\n", html.EscapeString(d.Info.Title))
	fmt.Fprintf(b, `<p class="version">Version %s · OpenAPI %s</p>`+"\n",
		html.EscapeString(d.Info.Version), html.EscapeString(d.OpenAPI))
	if len(d.Servers) > 0 {
		b.WriteString(`<p class="servers">`)
		for i, s := range d.Servers {
			if i > 0 {
				b.WriteString(" · ")
			}
			fmt.Fprintf(b, "<code>%s</code>", html.EscapeString(s.URL))
			if s.Description != "" {
				fmt.Fprintf(b, " — %s", html.EscapeString(s.Description))
			}
		}
		b.WriteString("</p>\n")
	}
	if d.Info.Description != "" {
		b.WriteString(`<div class="doc-desc">`)
		b.WriteString(renderProse(d.Info.Description))
		b.WriteString("</div>\n")
	}
	b.WriteString("</header>\n")
}

func renderTagSections(b *strings.Builder, groups []tagGroup) {
	for _, g := range groups {
		if len(g.Entries) == 0 {
			continue
		}
		fmt.Fprintf(b, `<section class="tag" id="tag-%s">`+"\n", slug(g.Name))
		fmt.Fprintf(b, "<h2>%s</h2>\n", html.EscapeString(g.Name))
		if g.Description != "" {
			fmt.Fprintf(b, `<p class="tag-desc">%s</p>`+"\n", html.EscapeString(g.Description))
		}
		for _, e := range g.Entries {
			renderOperation(b, e)
		}
		b.WriteString("</section>\n")
	}
}

func renderOperation(b *strings.Builder, e siteEntry) {
	fmt.Fprintf(b, `<article class="op" id="%s" data-op>`+"\n", e.Anchor)
	fmt.Fprintf(b, `<h3><span class="m m-%s">%s</span> <code class="path">%s</code></h3>`+"\n",
		e.Method, strings.ToUpper(e.Method), html.EscapeString(e.Path))
	if e.Spec.Summary != "" {
		fmt.Fprintf(b, `<p class="summary">%s</p>`+"\n", html.EscapeString(e.Spec.Summary))
	}
	if e.Spec.Description != "" {
		b.WriteString(`<div class="op-desc">`)
		b.WriteString(renderProse(e.Spec.Description))
		b.WriteString("</div>\n")
	}
	renderAuth(b, e)

	if len(e.Spec.Parameters) > 0 {
		b.WriteString("<h4>Parameters</h4>\n")
		b.WriteString(`<table class="params"><thead><tr><th>Name</th><th>In</th><th>Type</th><th>Required</th><th>Description</th></tr></thead><tbody>` + "\n")
		params := append([]siteParam(nil), e.Spec.Parameters...)
		sort.SliceStable(params, func(i, j int) bool {
			return paramRank(params[i].In) < paramRank(params[j].In)
		})
		for _, p := range params {
			req := "no"
			if p.Required {
				req = "yes"
			}
			fmt.Fprintf(b, "<tr><td><code>%s</code></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
				html.EscapeString(p.Name), html.EscapeString(p.In), schemaType(p.Schema),
				req, html.EscapeString(p.Description))
		}
		b.WriteString("</tbody></table>\n")
	}

	if e.Spec.RequestBody != nil {
		b.WriteString("<h4>Request body")
		if e.Spec.RequestBody.Required {
			b.WriteString(` <span class="req">required</span>`)
		}
		b.WriteString("</h4>\n")
		if e.Spec.RequestBody.Description != "" {
			fmt.Fprintf(b, "<p>%s</p>\n", html.EscapeString(e.Spec.RequestBody.Description))
		}
		if s := firstSchema(e.Spec.RequestBody.Content); s != nil {
			b.WriteString(renderSchemaBlock(s, 0))
		}
	}

	if len(e.Spec.Responses) > 0 {
		b.WriteString("<h4>Responses</h4>\n")
		for _, code := range sortedCodes(e.Spec.Responses) {
			r := e.Spec.Responses[code]
			fmt.Fprintf(b, `<div class="resp"><div class="resp-head"><span class="code c-%s">%s</span> %s</div>`,
				statusClass(code), html.EscapeString(code), html.EscapeString(r.Description))
			if s := firstSchema(r.Content); s != nil {
				b.WriteString(renderSchemaBlock(s, 0))
			}
			b.WriteString("</div>\n")
		}
	}
	b.WriteString("</article>\n")
}

func renderAuth(b *strings.Builder, e siteEntry) {
	// An operation with an explicit empty `security` list is callable without a
	// session. Absent `security` inherits the document default, which is a session.
	if e.Spec.Security != nil && len(*e.Spec.Security) == 0 {
		b.WriteString(`<p class="auth anonymous">No authentication required.</p>` + "\n")
	}
}

func renderSchemas(b *strings.Builder, d *siteDoc) {
	if len(d.Components.Schemas) == 0 {
		return
	}
	b.WriteString(`<section class="tag" id="schemas"><h2>Schemas</h2>` + "\n")
	b.WriteString(`<p class="tag-desc">The request and response types the operations above use, as the handlers define them.</p>` + "\n")
	for _, name := range sortedKeys(d.Components.Schemas) {
		s := d.Components.Schemas[name]
		fmt.Fprintf(b, `<article class="op schema" id="%s" data-op>`+"\n", schemaAnchor(name))
		fmt.Fprintf(b, "<h3><code>%s</code></h3>\n", html.EscapeString(name))
		if s.Description != "" {
			fmt.Fprintf(b, "<p>%s</p>\n", html.EscapeString(s.Description))
		}
		b.WriteString(renderSchemaBlock(s, 0))
		b.WriteString("</article>\n")
	}
	b.WriteString("</section>\n")
}

// renderSchemaBlock renders a schema as a property table, or as a compact type
// line when it is a scalar, an enum, or a reference to another schema.
func renderSchemaBlock(s *siteSchema, depth int) string {
	if s == nil {
		return ""
	}
	if depth > 6 {
		return `<p class="muted">… (nested deeper than this page renders; see the schema)</p>`
	}
	var b strings.Builder
	if s.Ref != "" {
		name := refName(s.Ref)
		fmt.Fprintf(&b, `<p class="ref">See <a href="#%s"><code>%s</code></a></p>`+"\n", schemaAnchor(name), html.EscapeString(name))
		return b.String()
	}
	if len(s.Properties) > 0 || s.Type.first() == "object" {
		required := map[string]bool{}
		for _, r := range s.Required {
			required[r] = true
		}
		b.WriteString(`<table class="params schema-props"><thead><tr><th>Field</th><th>Type</th><th>Description</th></tr></thead><tbody>` + "\n")
		for _, name := range sortedKeys(s.Properties) {
			prop := s.Properties[name]
			mark := ""
			if required[name] {
				mark = ` <span class="req">required</span>`
			}
			desc := prop.Description
			if len(prop.Enum) > 0 {
				desc = strings.TrimSpace("One of: " + inlineEnum(prop.Enum) + ". " + desc)
			}
			if prop.Default != nil {
				desc = strings.TrimSpace(desc + fmt.Sprintf(" Default: %v.", prop.Default))
			}
			fmt.Fprintf(&b, "<tr><td><code>%s</code>%s</td><td>%s</td><td>%s</td></tr>\n",
				html.EscapeString(name), mark, schemaType(prop), html.EscapeString(desc))
		}
		b.WriteString("</tbody></table>\n")
		if len(s.Properties) == 0 {
			b.WriteString(`<p class="muted">No declared fields.</p>` + "\n")
		}
		return b.String()
	}
	if len(s.OneOf) > 0 || len(s.AnyOf) > 0 || len(s.AllOf) > 0 {
		label, parts := "One of", s.OneOf
		if len(s.AnyOf) > 0 {
			label, parts = "Any of", s.AnyOf
		}
		if len(s.AllOf) > 0 {
			label, parts = "All of", s.AllOf
		}
		var refs []string
		for _, p := range parts {
			if p.Ref != "" {
				name := refName(p.Ref)
				refs = append(refs, fmt.Sprintf(`<a href="#%s"><code>%s</code></a>`, schemaAnchor(name), html.EscapeString(name)))
			} else {
				refs = append(refs, html.EscapeString(schemaType(p)))
			}
		}
		fmt.Fprintf(&b, `<p class="ref">%s: %s</p>`+"\n", label, strings.Join(refs, ", "))
		return b.String()
	}
	fmt.Fprintf(&b, `<p class="ref">Type: %s</p>`+"\n", schemaType(s))
	if s.Description != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(s.Description))
	}
	return b.String()
}

// schemaType renders a schema's type as safe HTML, linking a reference.
func schemaType(s *siteSchema) string {
	if s == nil {
		return `<span class="muted">unspecified</span>`
	}
	if s.Ref != "" {
		name := refName(s.Ref)
		return fmt.Sprintf(`<a href="#%s"><code>%s</code></a>`, schemaAnchor(name), html.EscapeString(name))
	}
	t := s.Type.first()
	switch t {
	case "array":
		return "array of " + schemaType(s.Items)
	case "":
		if len(s.Enum) > 0 {
			return "string (enum)"
		}
		return "unspecified"
	}
	if s.Format != "" {
		t += " (" + s.Format + ")"
	}
	if s.Nullable || s.Type.nullable() {
		t += ", nullable"
	}
	if len(s.Enum) > 0 {
		t += " — one of " + inlineEnum(s.Enum)
	}
	if s.Minimum != nil || s.Maximum != nil {
		t += " — " + bounds(s)
	}
	return html.EscapeString(t)
}

func bounds(s *siteSchema) string {
	switch {
	case s.Minimum != nil && s.Maximum != nil:
		return fmt.Sprintf("%v..%v", *s.Minimum, *s.Maximum)
	case s.Minimum != nil:
		return fmt.Sprintf("≥ %v", *s.Minimum)
	default:
		return fmt.Sprintf("≤ %v", *s.Maximum)
	}
}

func inlineEnum(vals []any) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, html.EscapeString(fmt.Sprint(v)))
	}
	return strings.Join(parts, ", ")
}

// renderProse turns a document description into paragraphs. The descriptions the
// generator emits are plain text with blank-line paragraphs and simple `##`
// headings and `-` bullets; that is all this handles, and anything else is shown
// as escaped text rather than dropped.
func renderProse(s string) string {
	var b strings.Builder
	for _, block := range strings.Split(strings.TrimSpace(s), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if strings.HasPrefix(block, "## ") {
			fmt.Fprintf(&b, "<h4>%s</h4>\n", html.EscapeString(strings.TrimPrefix(block, "## ")))
			continue
		}
		if strings.HasPrefix(block, "- ") {
			b.WriteString("<ul>\n")
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
				if line != "" {
					fmt.Fprintf(&b, "<li>%s</li>\n", html.EscapeString(line))
				}
			}
			b.WriteString("</ul>\n")
			continue
		}
		fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(strings.ReplaceAll(block, "\n", " ")))
	}
	return b.String()
}

func firstSchema(content map[string]siteMedia) *siteSchema {
	if content == nil {
		return nil
	}
	// Prefer application/json for a stable choice when more than one media type
	// is declared for the same status.
	if v, ok := content["application/json"]; ok && v.Schema != nil {
		return v.Schema
	}
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if content[k].Schema != nil {
			return content[k].Schema
		}
	}
	return nil
}

func sortedCodes(responses map[string]siteResponse) []string {
	codes := make([]string, 0, len(responses))
	for c := range responses {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool {
		ni, nj := numeric(codes[i]), numeric(codes[j])
		if ni != nj {
			return ni < nj
		}
		return codes[i] < codes[j]
	})
	return codes
}

// numeric returns the value of a status code, or a large sentinel for a
// non-numeric code such as "default", which then sorts last.
func numeric(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 1 << 30
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func statusClass(code string) string {
	if code == "" {
		return "other"
	}
	switch code[0] {
	case '2':
		return "ok"
	case '3':
		return "redirect"
	case '4':
		return "client"
	case '5':
		return "server"
	}
	return "other"
}

func paramRank(in string) int {
	switch in {
	case "path":
		return 0
	case "query":
		return 1
	case "header":
		return 2
	}
	return 3
}

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func opAnchor(method, path string) string { return "op-" + method + "-" + slug(path) }

func schemaAnchor(name string) string { return "schema-" + slug(name) }

// slug makes an HTML-id-safe token from a path or name.
func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '{' || r == '}':
			// drop the braces; the name inside stays
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

const siteCSS = `
:root{--bg:#f7f7f5;--fg:#1c1c1a;--muted:#6b6b66;--line:#e2e2dd;--card:#fff;
--get:#1f6feb;--post:#1a7f37;--put:#9a6700;--patch:#8250df;--delete:#cf222e;
--code:#f2f2ee}
*{box-sizing:border-box}
body{margin:0;font:15px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
color:var(--fg);background:var(--bg)}
a{color:var(--get);text-decoration:none}
a:hover{text-decoration:underline}
code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9em;
background:var(--code);padding:.1em .35em;border-radius:4px}
.sidebar{position:fixed;top:0;left:0;bottom:0;width:300px;overflow:auto;background:var(--card);
border-right:1px solid var(--line);padding:16px}
.brand{font-weight:600;font-size:16px;margin-bottom:2px;display:flex;justify-content:space-between;align-items:baseline}
.brand .ver{color:var(--muted);font-weight:400;font-size:12px}
#filter{width:100%;margin:12px 0;padding:7px 10px;border:1px solid var(--line);border-radius:6px;background:var(--bg)}
.nav-group{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);margin:14px 0 4px}
.nav-list{list-style:none;margin:0;padding:0}
.nav-list li{margin:1px 0}
.nav-list a{display:flex;gap:8px;align-items:baseline;padding:3px 6px;border-radius:5px;color:var(--fg)}
.nav-list a:hover{background:var(--bg);text-decoration:none}
.nav-list .p{font-family:ui-monospace,monospace;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.m{font-family:ui-monospace,monospace;font-size:11px;font-weight:700;text-transform:uppercase}
.m-get{color:var(--get)}.m-post{color:var(--post)}.m-put{color:var(--put)}
.m-patch{color:var(--patch)}.m-delete{color:var(--delete)}
.content{margin-left:300px;padding:32px 40px;max-width:1000px}
header h1{margin:0 0 4px;font-size:30px}
.version{color:var(--muted);margin:0 0 8px}
.servers{color:var(--muted);font-size:13px}
.doc-desc{border-left:3px solid var(--line);padding-left:16px;margin:16px 0}
.tag{margin-top:40px}
.tag>h2{border-bottom:2px solid var(--line);padding-bottom:6px;font-size:22px}
.tag-desc{color:var(--muted)}
.op{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:18px 20px;margin:16px 0}
.op h3{margin:0 0 8px;font-size:16px;display:flex;gap:10px;align-items:baseline;flex-wrap:wrap}
.path{background:transparent;font-size:14px;padding:0}
.op h4{margin:18px 0 6px;font-size:13px;text-transform:uppercase;letter-spacing:.05em;color:var(--muted)}
.summary{margin:0 0 6px;font-weight:500}
.op-desc{color:#333}
table.params{border-collapse:collapse;width:100%;font-size:13px}
table.params th{text-align:left;color:var(--muted);font-weight:600;border-bottom:1px solid var(--line);padding:6px 8px}
table.params td{border-bottom:1px solid var(--line);padding:6px 8px;vertical-align:top}
.req{color:var(--delete);font-size:11px;text-transform:uppercase;letter-spacing:.04em}
.resp{border-top:1px solid var(--line);padding-top:10px;margin-top:10px}
.resp-head{font-weight:500}
.code{font-family:ui-monospace,monospace;font-weight:700;margin-right:6px}
.c-ok{color:var(--post)}.c-client{color:var(--put)}.c-server{color:var(--delete)}
.c-redirect{color:var(--get)}
.auth{margin:6px 0;font-size:13px}
.auth.anonymous{color:var(--post)}
.ref{color:var(--muted);font-size:13px}
.muted{color:var(--muted)}
.schema .schema-props{margin-top:8px}
.hidden{display:none!important}
@media(max-width:820px){
.sidebar{position:static;width:auto;border-right:0;border-bottom:1px solid var(--line)}
.content{margin-left:0;padding:20px}
}
`

const siteJS = `
(function(){
  var box=document.getElementById('filter');
  if(!box)return;
  box.addEventListener('input',function(){
    var q=box.value.trim().toLowerCase();
    var ops=document.querySelectorAll('article[data-op]');
    for(var i=0;i<ops.length;i++){
      var el=ops[i];
      el.classList.toggle('hidden',q!==''&&el.textContent.toLowerCase().indexOf(q)===-1);
    }
    var groups=document.querySelectorAll('.nav-group[data-group]');
    for(var g=0;g<groups.length;g++){
      var list=groups[g].nextElementSibling;
      if(!list)continue;
      var items=list.querySelectorAll('li');
      var show=0;
      for(var j=0;j<items.length;j++){
        var hit=items[j].textContent.toLowerCase().indexOf(q)!==-1;
        items[j].classList.toggle('hidden',q!==''&&!hit);
        if(hit)show++;
      }
      groups[g].classList.toggle('hidden',q!==''&&show===0);
      list.classList.toggle('hidden',q!==''&&show===0);
    }
  });
})();
`
