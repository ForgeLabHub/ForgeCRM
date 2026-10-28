package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/openapi"
)

// These tests check the contract against reality rather than against itself.
//
// The drift guard in internal/openapi compares two generated documents, so it
// passes even if the contract table is wrong in the same way both times. What is
// checked here is the table itself: that a field called required really is rejected
// when absent, that every component named exists, and that nothing is referenced
// that was never defined.

// buildDocument generates the specification the way the command does.
func buildDocument(t *testing.T) map[string]any {
	t.Helper()

	srv := New(&Deps{
		Config: &config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	routes, err := openapi.Routes(srv.Router())
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	perms, err := openapi.Permissions(".", "../rbac")
	if err != nil {
		t.Fatalf("read permissions: %v", err)
	}
	perms = openapi.PrefixAPI(perms)

	doc, err := openapi.Build(routes, perms, APIContract(), openapi.Info{
		Title: "ForgeCRM API", Version: "test", Description: "test",
	}, []openapi.Server{{URL: "/"}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(doc, &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return out
}

// schemasOf returns the components.schemas section.
func schemasOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatal("the document has no components section")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("the document has no components.schemas section")
	}
	return schemas
}

// validationDTOs lists every request type whose required fields are declared and
// whose rules live in a Validate method.
//
// A type appears here only if it has Validate, because that is what makes the
// declaration checkable. Types validated inline in a handler are covered by
// TestDeclaredRequiredFieldsExist, which checks the names but cannot check the
// rules; that gap is stated rather than hidden.
var validationDTOs = []struct {
	name string
	zero func() any
}{
	{"AccountInput", func() any { return &crm.AccountInput{} }},
	{"ContactInput", func() any { return &crm.ContactInput{} }},
	{"LeadInput", func() any { return &crm.LeadInput{} }},
	{"DealInput", func() any { return &crm.DealInput{} }},
	{"TaskInput", func() any { return &crm.TaskInput{} }},
	{"ActivityInput", func() any { return &crm.ActivityInput{} }},
	{"TicketInput", func() any { return &crm.TicketInput{} }},
	{"BulkArchiveInput", func() any { return &crm.BulkArchiveInput{} }},
}

// conditionalRequired names the validation error keys that are not plain "this
// field must be present" rules, with the reason each is not in the required list.
//
// An entry here is a claim that the rule is genuinely conditional and is stated in
// the request body description instead. Adding one without a description would hide
// a rule a client must satisfy, so TestConditionalRulesAreDocumented checks that a
// description exists.
var conditionalRequired = map[string]map[string]string{
	"ContactInput": {
		"name": "at least one of first_name, last_name or email is required",
	},
	"ActivityInput": {
		"type":   "the type must be one of a fixed set; an empty value is not a member",
		"target": "at least one of account_id, contact_id, deal_id or ticket_id is required",
	},
}

// TestDeclaredRequiredFieldsAreActuallyRequired is the core check.
//
// For every request type with a Validate method, it calls Validate on a zero value
// and reads which fields the rules reject. Every field the contract calls required
// must appear there: if it does not, the specification tells a client to send
// something the server would accept without. The converse is checked too, against a
// declared list of rules that are genuinely conditional, so a required rule cannot
// be dropped from the table unnoticed.
func TestDeclaredRequiredFieldsAreActuallyRequired(t *testing.T) {
	doc := buildDocument(t)
	schemas := schemasOf(t, doc)

	for _, tc := range validationDTOs {
		t.Run(tc.name, func(t *testing.T) {
			schema, ok := schemas[tc.name].(map[string]any)
			if !ok {
				t.Fatalf("the contract declares %s but the document has no such component", tc.name)
			}
			declared := stringSet(schema["required"])

			rejected := rejectedFields(t, tc.zero())

			for field := range declared {
				if !rejected[field] {
					t.Errorf("%s declares %q required, but omitting it produces no validation error",
						tc.name, field)
				}
			}

			allowed := conditionalRequired[tc.name]
			for field := range rejected {
				if declared[field] {
					continue
				}
				if _, ok := allowed[field]; ok {
					continue
				}
				t.Errorf("%s rejects the absent field %q, but the contract neither lists it as "+
					"required nor records it as conditional: add it to RequiredBody or to "+
					"conditionalRequired with the reason", tc.name, field)
			}
		})
	}
}

// TestConditionalRulesAreDocumented checks that every rule recorded as conditional
// is actually stated where a client will read it.
//
// The required list cannot express "one of these", so the rule lives in the request
// body description. Recording the exemption without writing the description would
// mean the rule is enforced but undocumented.
func TestConditionalRulesAreDocumented(t *testing.T) {
	doc := buildDocument(t)
	paths, _ := doc["paths"].(map[string]any)

	for _, tc := range validationDTOs {
		allowed := conditionalRequired[tc.name]
		if len(allowed) == 0 {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			desc := requestBodyDescription(t, paths, tc.name)
			if desc == "" {
				t.Fatalf("%s has conditional rules but no request body description explaining them",
					tc.name)
			}
		})
	}
}

// TestDeclaredRequiredFieldsExist checks every name in the contract against the
// generated schema.
//
// This covers the types whose validation is inline in a handler and so cannot be
// exercised here. A rename that misses the table is caught, because a required name
// that is not a property of the component is a typo.
func TestDeclaredRequiredFieldsExist(t *testing.T) {
	contract := APIContract()
	doc := buildDocument(t)
	schemas := schemasOf(t, doc)

	for name, required := range contract.RequiredBody {
		schema, ok := schemas[name].(map[string]any)
		if !ok {
			t.Errorf("RequiredBody names the component %q, which the document does not define", name)
			continue
		}
		props, _ := schema["properties"].(map[string]any)
		for _, field := range required {
			if _, ok := props[field]; !ok {
				t.Errorf("%s declares %q required, but the component has no such property", name, field)
			}
		}
	}
}

// TestNoDanglingReferences checks that every $ref points at a component that exists.
//
// A reference to a missing component makes the document unusable: a code generator
// either fails or emits a type with no fields.
func TestNoDanglingReferences(t *testing.T) {
	doc := buildDocument(t)
	schemas := schemasOf(t, doc)

	for _, ref := range collectRefs(doc) {
		const prefix = "#/components/schemas/"
		if !strings.HasPrefix(ref, prefix) {
			t.Errorf("unexpected reference form %q", ref)
			continue
		}
		name := strings.TrimPrefix(ref, prefix)
		if _, ok := schemas[name]; !ok {
			t.Errorf("the document references %q, which is not defined in components.schemas", name)
		}
	}
}

// TestNoOrphanComponents checks the converse: a component nothing references is
// documentation for a shape no endpoint uses.
//
// The registry only registers reachable types, so an orphan means the walk and the
// emit disagree — a bug worth failing on rather than shipping dead schemas.
func TestNoOrphanComponents(t *testing.T) {
	doc := buildDocument(t)
	schemas := schemasOf(t, doc)

	referenced := map[string]bool{}
	for _, ref := range collectRefs(doc) {
		referenced[strings.TrimPrefix(ref, "#/components/schemas/")] = true
	}

	// The shared schemas are referenced by name from the error responses; Problem is
	// referenced by problemResponse, and the rest are part of that family.
	shared := map[string]bool{"Problem": true, "Pagination": true, "Error": true}

	for name := range schemas {
		if referenced[name] || shared[name] {
			continue
		}
		t.Errorf("components.schemas defines %q, which no operation references", name)
	}
}

// TestEveryOperationNamesAComponentOrSaysWhyNot checks that a success response
// either carries a real schema or admits it does not.
//
// A success response described as a bare object is acceptable only when the
// description says so. This stops the placeholder from becoming permanent by
// accident: the operation is either given a type or explicitly left free-form.
func TestEveryOperationNamesAComponentOrSaysWhyNot(t *testing.T) {
	doc := buildDocument(t)
	paths, _ := doc["paths"].(map[string]any)

	for path, item := range paths {
		methods, _ := item.(map[string]any)
		for method, raw := range methods {
			op, _ := raw.(map[string]any)
			responses, _ := op["responses"].(map[string]any)
			for status, rawResp := range responses {
				if status == "204" || strings.HasPrefix(status, "4") || strings.HasPrefix(status, "5") {
					continue
				}
				resp, _ := rawResp.(map[string]any)
				content, _ := resp["content"].(map[string]any)
				mt, _ := content["application/json"].(map[string]any)
				schema, _ := mt["schema"].(map[string]any)
				_, hasRef := schema["$ref"]
				desc, _ := resp["description"].(string)
				if !hasRef && !strings.Contains(desc, "not yet described") {
					t.Errorf("%s %s: the success response has no schema and does not say so",
						strings.ToUpper(method), path)
				}
			}
		}
	}
}

// rejectedFields calls Validate on the value and returns the field names the error
// names. A value with no error contributes nothing, which is correct: a rule that
// does not fire on a zero value is not an unconditional requirement.
func rejectedFields(t *testing.T, v any) map[string]bool {
	t.Helper()

	validator, ok := v.(interface{ Validate() error })
	if !ok {
		t.Fatalf("%T has no Validate method; it should not be in validationDTOs", v)
	}
	err := validator.Validate()
	if err == nil {
		return map[string]bool{}
	}

	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("%T.Validate returned %v, which is not an httpx.APIError", v, err)
	}
	return stringSet(apiErr.Details)
}

// stringSet turns a JSON array or a string map's keys into a set.
func stringSet(v any) map[string]bool {
	out := map[string]bool{}
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if s, ok := item.(string); ok {
				out[s] = true
			}
		}
	case map[string]string:
		for k := range x {
			out[k] = true
		}
	}
	return out
}

// collectRefs walks any decoded JSON value and returns every $ref string.
func collectRefs(v any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if k == "$ref" {
					if s, ok := x[k].(string); ok {
						out = append(out, s)
						continue
					}
				}
				walk(x[k])
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(v)
	return out
}

// requestBodyDescription finds the request body description for the operation that
// declares the given component, so the conditional-rule test can read it.
func requestBodyDescription(t *testing.T, paths map[string]any, component string) string {
	t.Helper()

	target := "#/components/schemas/" + component
	for _, item := range paths {
		methods, _ := item.(map[string]any)
		for _, raw := range methods {
			op, _ := raw.(map[string]any)
			body, _ := op["requestBody"].(map[string]any)
			content, _ := body["content"].(map[string]any)
			mt, _ := content["application/json"].(map[string]any)
			schema, _ := mt["schema"].(map[string]any)
			if schema["$ref"] == target {
				desc, _ := body["description"].(string)
				return desc
			}
		}
	}
	return ""
}
