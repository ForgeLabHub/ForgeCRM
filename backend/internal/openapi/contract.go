package openapi

import (
	"reflect"
	"sort"
)

// Contract is what the generator cannot learn from the router alone.
//
// The router says which paths exist and which permission each needs. It does not
// say what a handler decodes or what it returns, because that is a runtime fact
// about a value, not a property of the route table. The contract supplies it by
// naming the real Go type, so the schema is derived from the type the handler
// actually uses and cannot describe a different one.
//
// Every entry is a reflect.Type rather than a schema literal. A literal would be a
// second description of the same thing, and the second description is the one that
// goes stale. Naming the type means a field added to a DTO appears in the
// specification on the next generation, with no edit here.
//
// The maps are keyed by "METHOD /path", the same key the router and the permission
// table use, so all three are joined on one identifier.
type Contract struct {
	// RequestBodies names the type each handler decodes into.
	RequestBodies map[string]reflect.Type

	// Responses names the type a successful response carries.
	Responses map[string]reflect.Type

	// RequiredBody lists the request fields validation actually rejects when
	// absent. It is declared rather than inferred: a field being a non-pointer
	// says nothing about whether a client must send it. DealInput.Amount is a
	// non-pointer float that may be omitted, and AccountInput.Name is a
	// non-pointer string that may not. Only the validation code knows which.
	RequiredBody map[string][]string

	// BodyNames overrides the component name used for a request body.
	//
	// It exists for the case where one Go type serves as both a request and a
	// response. The two directions need different `required` sets — a client
	// must send a name but must not send the id the server assigns — and one
	// component can carry only one set. Naming the request separately gives each
	// direction its own schema, which is the honest description, instead of one
	// component whose required list is wrong for at least one caller.
	BodyNames map[string]string

	// BodyDescriptions adds prose to a request body, keyed by route.
	//
	// It carries the rules a schema cannot state: which methods a JSON-RPC
	// endpoint accepts, that a field is shorthand for another, that an
	// acknowledgement flag is required. Keying by route rather than by type means
	// two routes using one type can each describe themselves correctly.
	BodyDescriptions map[string]string

	// TypeNames overrides the component name for a Go type everywhere it appears.
	//
	// It exists for two distinct Go types that share a bare name — importx.Job and
	// jobs.Job are both "Job" — which would otherwise collide in the components map.
	// Generation stops on a collision rather than letting one type's fields describe
	// the other, and this names them apart. It is keyed by type rather than by route
	// because a type is often reached as a nested field of a generic response, where
	// a per-route override would not apply.
	TypeNames map[reflect.Type]string

	// QueryParams declares the filters a collection endpoint accepts. The shared
	// pagination parameters are added automatically; this is for the ones that
	// differ per resource.
	QueryParams map[string][]QueryParam

	// SortColumns lists the fields a collection can be sorted by. It is declared
	// because the allowlist lives in an unexported map inside the handler package
	// that this package cannot import; the generator checks the declaration against
	// the handler at generation time so the two cannot drift.
	SortColumns map[string][]string

	// SuccessStatus overrides the status code a successful call returns.
	//
	// The generator otherwise assumes the convention: 201 for a POST that creates,
	// 200 for a POST that acts on an existing resource, 204 for a DELETE. The
	// convention is right most of the time and wrong for actions that create nothing
	// and return a result — a bulk operation reporting per-record outcomes, a logout
	// returning an acknowledgement. Declaring the code here keeps the specification
	// honest about those instead of forcing the handler to return a status that
	// misdescribes what it did.
	SuccessStatus map[string]int

	// StreamingSuccess names the routes whose success response is a byte stream
	// rather than JSON: an export, a file download. The generator would otherwise
	// describe the body as an application/json object, which is wrong for every
	// caller — a client that reads the document as JSON finds it is CSV. Naming the
	// route lets the response be documented as the media types it really carries, and
	// the description says what the bytes are.
	StreamingSuccess map[string]StreamingResponse

	// TextSuccess names the routes whose success response is text in a non-JSON
	// media type, such as the Prometheus exposition format. It is separate from
	// StreamingSuccess because the body is readable text, not opaque bytes:
	// documenting `format: binary` on a `text/plain` body would tell a client to
	// treat text as a blob. Without this, the generator would describe an
	// application/json object, which is worse — a scraper that parsed it as JSON
	// would fail.
	TextSuccess map[string]TextResponse

	// ExtraResponses declares failure responses a route can return that the
	// generator would not infer from its method and access level. The convention
	// covers the common cases (400, 404, 429, 500, and 401/403 for a session route);
	// it cannot know that an anonymous route is closed without a shared token, which
	// is a 401, or that an opt-in subsystem answers 503 when it is switched off.
	// Declaring those keeps the document from under-promising: a caller that only
	// sees the inferred codes would not know a valid request can be refused.
	ExtraResponses map[string]map[int]string
}

// TextResponse describes a success response that is text in a non-JSON media type.
type TextResponse struct {
	// MediaTypes are the content types the handler can set.
	MediaTypes []string
	// Description says what the text is, since a bare string schema cannot.
	Description string
}

// StreamingResponse describes a success response that is not JSON.
type StreamingResponse struct {
	// MediaTypes are the content types the handler can set. For a download whose type
	// is the stored file's own type, application/octet-stream is the fallback.
	MediaTypes []string
	// Description says what the bytes are and how the type is decided, since a schema
	// cannot.
	Description string
}

// StreamingSuccessFor returns the streaming description for a route, if any.
func (c Contract) StreamingSuccessFor(key string) (StreamingResponse, bool) {
	s, ok := c.StreamingSuccess[key]
	return s, ok
}

// TextSuccessFor returns the text-stream description for a route, if any.
func (c Contract) TextSuccessFor(key string) (TextResponse, bool) {
	s, ok := c.TextSuccess[key]
	return s, ok
}

// ExtraResponsesFor returns the declared extra failure responses for a route.
func (c Contract) ExtraResponsesFor(key string) map[int]string {
	return c.ExtraResponses[key]
}

// QueryParam describes one query parameter.
type QueryParam struct {
	Name        string
	Description string
	Schema      map[string]any
	Required    bool
}

// SuccessStatusFor returns the declared success status for a route, if any.
func (c Contract) SuccessStatusFor(key string) (int, bool) {
	code, ok := c.SuccessStatus[key]
	return code, ok
}

// ResponseType returns the declared response type for a route, if any.
func (c Contract) ResponseType(key string) (reflect.Type, bool) {
	t, ok := c.Responses[key]
	return t, ok
}

// BodyType returns the declared request type for a route, if any.
func (c Contract) BodyType(key string) (reflect.Type, bool) {
	t, ok := c.RequestBodies[key]
	return t, ok
}

// requiredFor returns the declared required field names for a request type.
func (c Contract) requiredForBody(name string) []string {
	return c.RequiredBody[name]
}

// keys returns every route key the contract mentions, for the orphan check.
func (c Contract) keys() map[string]bool {
	out := map[string]bool{}
	for k := range c.RequestBodies {
		out[k] = true
	}
	for k := range c.Responses {
		out[k] = true
	}
	for k := range c.QueryParams {
		out[k] = true
	}
	for k := range c.SortColumns {
		out[k] = true
	}
	for k := range c.ExtraResponses {
		out[k] = true
	}
	return out
}

// schemaForResponse describes a successful response. Required fields are derived
// from the struct tags, because on a response the tags are authoritative: a field
// without omitempty is always serialised, so it is always present.
func (c Contract) schemaForResponse(t reflect.Type, reg *registry) map[string]any {
	if t.Kind() != reflect.Struct {
		return schemaFor(t, reg, modeResponse)
	}
	name := componentName(t, reg, modeResponse)
	reg.addResponse(name, t)
	return ref(name)
}

// sortedKeys returns a sorted key list, so iteration order cannot vary between runs.
func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
