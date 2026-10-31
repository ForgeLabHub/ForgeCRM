package openapi

import (
	"fmt"
	"net/http"
	"strings"
)

// PathParamSchema types a path parameter. Identifiers are UUIDs; the few
// exceptions are named explicitly rather than defaulting everything to string and
// leaving a client to discover the type from a 500.
//
// Exported so the contract test can assert every parameter is typed.
func PathParamSchema(name string) map[string]any { return pathParamSchema(name) }

func pathParamSchema(name string) map[string]any {
	switch name {
	case "id", "roleId", "deliveryId", "userId", "teamId", "orgId", "jobId",
		"sessionId", "importId", "migrationId", "integrationId", "webhookId",
		"eventId", "taskId", "documentId", "ticketId", "dealId", "contactId",
		"accountId", "leadId", "pipelineId", "notificationId":
		return map[string]any{"type": "string", "format": "uuid"}
	default:
		return map[string]any{"type": "string"}
	}
}

// pathParams extracts {name} segments in the order they appear.
func pathParams(path string) []string {
	var out []string
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			break
		}
		out = append(out, path[start+1:start+end])
		path = path[start+end+1:]
	}
	return out
}

// idempotencyParam is the header a creating request may carry to make a retry safe.
//
// It is declared once, like the list parameters, because the middleware enforces the
// same contract everywhere. Documenting it is what turns the behaviour from an
// implementation detail into something a generated client can use.
func idempotencyParam() parameter {
	return parameter{
		Name: "Idempotency-Key", In: "header",
		Schema: map[string]any{"type": "string", "maxLength": 255},
		Description: "Makes a mutating request replayable. A repeat with the same key " +
			"returns the first result (marked with the `Idempotent-Replay` header) instead " +
			"of performing the operation again. The key is scoped to the caller and the " +
			"operation. A concurrent request with the same key is refused with 409.",
	}
}

// listParams is the shared collection query contract.
//
// It is declared once because the middleware enforces the same set everywhere;
// repeating it per route would invite the copies to disagree.
func listParams() []parameter {
	return []parameter{
		{Name: "limit", In: "query",
			Schema:      map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 50},
			Description: "Page size. Values above the server maximum are clamped, not rejected."},
		{Name: "offset", In: "query",
			Schema: map[string]any{"type": "integer", "minimum": 0, "default": 0}},
		{Name: "q", In: "query",
			Schema:      map[string]any{"type": "string"},
			Description: "Free-text filter."},
		{Name: "sort", In: "query",
			Schema:      map[string]any{"type": "string"},
			Description: "Field to sort by. An unknown field is rejected."},
		{Name: "order", In: "query",
			Schema: map[string]any{"type": "string", "enum": []string{"asc", "desc"}, "default": "desc"}},
	}
}

// ref builds a schema reference.
func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// responsesFor assembles the response set for an operation.
//
// The error responses are attached centrally rather than restated per route,
// because every operation can fail the same ways and the copies would drift. The
// success response is described by shape, since the generator does not know which
// entity a given action returns.
func responsesFor(rt Route, isAnonymous bool, c Contract, reg *registry) map[string]response {
	out := map[string]response{}

	success := http.StatusOK
	if rt.Method == http.MethodPost {
		success = http.StatusCreated
	}
	// Actions on an existing resource ("POST /{id}/execute") do not create a
	// resource, so 200 is the honest status for them.
	if rt.Method == http.MethodPost && strings.Contains(rt.Path, "{") {
		success = http.StatusOK
	}
	if rt.Method == http.MethodDelete {
		success = http.StatusNoContent
	}
	// A declared status wins over the convention, for the actions the convention
	// describes wrongly.
	if declared, ok := c.SuccessStatusFor(rt.Key()); ok {
		success = declared
	}

	if success == http.StatusNoContent {
		out["204"] = response{Description: "The operation succeeded. No body is returned."}
	} else if stream, ok := c.StreamingSuccessFor(rt.Key()); ok {
		out[fmt.Sprint(success)] = streamingResponse(stream)
	} else if text, ok := c.TextSuccessFor(rt.Key()); ok {
		out[fmt.Sprint(success)] = textResponse(text)
	} else {
		out[fmt.Sprint(success)] = successResponse(rt, c, reg)
	}

	out["400"] = problemResponse("The request was malformed: bad JSON, an unknown field, or a missing required parameter.")
	if !isAnonymous {
		out["401"] = problemResponse("No valid session, or the session has expired. Sign in again.")
		out["403"] = problemResponse("Authenticated, but the caller lacks the required permission.")
	}
	out["404"] = problemResponse("No such resource, or none visible to the caller's organization.")
	out["409"] = problemResponse("The request conflicts with current state, such as a duplicate external identifier.")
	out["422"] = problemResponse("A field failed validation. `error.details` names each field.")
	out["429"] = problemResponse("Too many requests. Retry after the interval in `Retry-After`.")
	out["500"] = problemResponse("Unexpected failure. The detail is deliberately generic; the cause is in the server log.")

	// A route may declare failure responses the convention cannot infer. Adding them
	// after the inferred set lets a declaration override a generic description.
	for code, description := range c.ExtraResponsesFor(rt.Key()) {
		out[fmt.Sprint(code)] = problemResponse(description)
	}
	return out
}

// successResponse describes a successful body.
//
// When the contract names the type, the schema is derived from it, so a client
// knows the shape. When it does not, the response is documented as a free-form
// object and the description says so, rather than inventing fields. Saying "an
// object" is honest; listing fields nobody verified is not.
func successResponse(rt Route, c Contract, reg *registry) response {
	if t, ok := c.ResponseType(rt.Key()); ok {
		return response{
			Description: "Success.",
			Content: map[string]mediaType{
				"application/json": {Schema: c.schemaForResponse(t, reg)},
			},
		}
	}
	return response{
		Description: "Success. This operation's response body is not yet described by a schema; " +
			"the shape is whatever the handler returns.",
		Content: map[string]mediaType{
			"application/json": {Schema: map[string]any{"type": "object"}},
		},
	}
}

// streamingResponse describes a success body that is not JSON.
//
// The bytes are documented by media type with a binary schema, and the description
// says what they are. A JSON schema here would be a lie a client would act on: a
// download whose body is a PNG is not an object.
func streamingResponse(s StreamingResponse) response {
	content := map[string]mediaType{}
	for _, mt := range s.MediaTypes {
		content[mt] = mediaType{Schema: map[string]any{"type": "string", "format": "binary"}}
	}
	return response{Description: s.Description, Content: content}
}

// textResponse describes a success body that is text in a non-JSON media type.
//
// The schema is a string, not an object and not binary: the body is readable text
// with a documented media type. The description says what the text is, because a
// bare string schema cannot.
func textResponse(s TextResponse) response {
	content := map[string]mediaType{}
	for _, mt := range s.MediaTypes {
		content[mt] = mediaType{Schema: map[string]any{"type": "string"}}
	}
	return response{Description: s.Description, Content: content}
}

func problemResponse(description string) response {
	return response{
		Description: description,
		Content: map[string]mediaType{
			"application/problem+json": {Schema: ref("Problem")},
			"application/json":         {Schema: ref("Problem")},
		},
	}
}

// schemas returns the shared component schemas.
//
// The error envelope is the important one: it is the only shape every client
// must handle, and it is specified from the code in internal/httpx rather than
// from memory.
func schemas() map[string]any {
	return map[string]any{
		"Problem": map[string]any{
			"type":        "object",
			"description": "RFC 9457 Problem Details, as emitted by internal/httpx.",
			"required":    []string{"type", "title", "status", "error"},
			"properties": map[string]any{
				"type":   map[string]any{"type": "string", "format": "uri-reference", "description": "A stable identifier for the problem class."},
				"title":  map[string]any{"type": "string", "description": "A short human-readable summary."},
				"status": map[string]any{"type": "integer", "description": "The HTTP status code, repeated for convenience."},
				"detail": map[string]any{"type": "string", "description": "An explanation specific to this occurrence. Never contains a secret."},
				"instance": map[string]any{
					"type":        "string",
					"description": "The request path, or its request id when a path would be misleading.",
				},
				"retryable": map[string]any{
					"type":        "boolean",
					"description": "Whether retrying the same request may succeed.",
				},
				"request_id": map[string]any{
					"type":        "string",
					"description": "Correlates this response with the server log. Quote it when reporting a problem.",
				},
				"error": map[string]any{
					"type":        "object",
					"description": "The machine-readable code. Branch on this, not on `title`.",
					"required":    []string{"code", "message"},
					"properties": map[string]any{
						"code": map[string]any{
							"type": "string",
							"enum": []string{
								"bad_request", "unauthorized", "forbidden", "not_found",
								"conflict", "validation_failed", "rate_limited",
								"internal_error", "unavailable",
							},
						},
						"message": map[string]any{"type": "string"},
						"details": map[string]any{
							"type":                 "object",
							"additionalProperties": map[string]any{"type": "string"},
							"description":          "Field-level messages, present on validation failures.",
						},
					},
				},
			},
		},
		"Pagination": map[string]any{
			"type":        "object",
			"description": "Paging metadata returned with every collection.",
			"required":    []string{"total", "limit", "offset"},
			"properties": map[string]any{
				"total":  map[string]any{"type": "integer", "description": "Total matching rows, ignoring limit and offset."},
				"limit":  map[string]any{"type": "integer"},
				"offset": map[string]any{"type": "integer"},
			},
		},
		"Error": map[string]any{
			"type":        "object",
			"description": "Deprecated standalone error body. Prefer the Problem schema.",
			"deprecated":  true,
			"required":    []string{"code", "message"},
			"properties": map[string]any{
				"code":    map[string]any{"type": "string"},
				"message": map[string]any{"type": "string"},
			},
		},
	}
}
