package openapi

import (
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// This file turns a Go type into a JSON Schema.
//
// The type is the single source of truth: the struct a handler decodes into is the
// struct that describes the request body, so the two cannot disagree. A hand-written
// schema would be a second description of the same thing, and the second description
// is the one that goes stale.
//
// What reflection cannot know is stated explicitly instead of guessed. Whether a
// field is *required* is a fact about the validation rules, not about the struct, so
// required fields are declared in contract.go and checked at generation time.
// Guessing "non-pointer means required" would be wrong: `amount float64` is a
// non-pointer a client may omit, and `name string` is a non-pointer it may not.

// schemaFor describes a Go type as a JSON Schema fragment.
//
// Named structs are emitted as a $ref into components.schemas and registered in reg.
// Anonymous structs, slices, maps and scalars are inlined.
func schemaFor(t reflect.Type, reg *registry, m mode) map[string]any {
	if t == nil {
		// An untyped value (map[string]any, any) is genuinely free-form.
		return map[string]any{}
	}

	// A pointer means the field may be absent or null.
	if t.Kind() == reflect.Pointer {
		return nullable(schemaFor(t.Elem(), reg, m))
	}

	if t == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t == uuidType {
		return map[string]any{"type": "string", "format": "uuid"}
	}
	if t == rawMessageType {
		return map[string]any{"description": "Arbitrary JSON value."}
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			// A byte slice is base64 in JSON, not an array of numbers.
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": schemaFor(t.Elem(), reg, m)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaFor(t.Elem(), reg, m)}
	case reflect.Interface:
		return map[string]any{"description": "Arbitrary JSON value."}
	case reflect.Struct:
		name := componentName(t, reg, m)
		reg.add(name, t, m)
		return ref(name)
	default:
		return map[string]any{}
	}
}

// objectSchema describes a struct's fields.
func objectSchema(t reflect.Type, reg *registry, required []string, m mode) map[string]any {
	props := map[string]any{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, ok := jsonName(f)
		if !ok {
			continue
		}

		s := schemaFor(f.Type, reg, m)
		if desc := f.Tag.Get("jsonschema"); desc != "" {
			s["description"] = desc
		}
		props[name] = s
	}

	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sorted := append([]string(nil), required...)
		sort.Strings(sorted)
		out["required"] = sorted
	}
	return out
}

// jsonName returns the JSON key for a field, its tag options, and whether the field
// is part of the wire format at all.
func jsonName(f reflect.StructField) (string, map[string]bool, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", nil, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = f.Name
	}
	opts := map[string]bool{}
	for _, o := range parts[1:] {
		opts[o] = true
	}
	return name, opts, true
}

// nullable wraps a schema so it also accepts null.
//
// OpenAPI 3.1 uses JSON Schema 2020-12, where `type` may be a list. Emitting
// ["string","null"] is the standard spelling and every current generator understands
// it; the 3.0 `nullable: true` keyword is not valid in 3.1.
func nullable(s map[string]any) map[string]any {
	if t, ok := s["type"].(string); ok {
		return map[string]any{"type": []string{t, "null"}}
	}
	// A $ref cannot carry a sibling keyword in JSON Schema 2020-12, so a nullable
	// reference has to be rephrased. This is the one place a reference is not reused
	// verbatim.
	if ref, ok := s["$ref"].(string); ok {
		return map[string]any{
			"anyOf": []any{
				map[string]any{"$ref": ref},
				map[string]any{"type": "null"},
			},
		}
	}
	// A free-form schema already accepts null.
	return s
}

var (
	timeType       = reflect.TypeOf(time.Time{})
	uuidType       = reflect.TypeOf(uuid.UUID{})
	rawMessageType = reflect.TypeOf([]byte(nil))
)

// componentName derives a stable component name for a struct type.
//
// A generic instantiation reports a name that embeds full package paths
// ("ListResponse[github.com/.../crm.Account]"), which is unusable as a key and
// unstable across module renames. Names are reduced to their last path segment:
// "ListResponseAccount".
func componentName(t reflect.Type, reg *registry, m mode) string {
	if override, ok := reg.names[t]; ok {
		return override
	}
	name := t.Name()
	if name == "" {
		// An anonymous struct has no name to key on.
		return reg.anonymousName(t)
	}
	if !strings.Contains(name, "[") {
		if m == modeRequest && reg.shared[t] {
			// A type used in both directions gets a distinct request component, so
			// its request `required` set cannot contradict its response one.
			return exportName(name+"Input", t)
		}
		return exportName(name, t)
	}

	base, params, _ := strings.Cut(name, "[")
	params = strings.TrimSuffix(params, "]")
	var out strings.Builder
	out.WriteString(base)
	for _, p := range strings.Split(params, ",") {
		out.WriteString(genericArgName(p))
	}
	return out.String()
}

// exportName gives an unexported Go type an exported component name.
//
// A request DTO in package api is unexported ("loginRequest") because nothing
// outside the package names it. A specification key is a public identifier, and
// "loginRequest" next to "Account" reads as an accident of Go visibility rather than
// a deliberate name. The first letter is capitalised so the components section is
// consistently named; the Go type is untouched, so the schema still comes from the
// type the handler decodes.
func exportName(name string, t reflect.Type) string {
	if t.PkgPath() == "" || name == "" {
		return name
	}
	if r := name[0]; r >= 'a' && r <= 'z' {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

// genericArgName turns a type parameter's printed name into a readable suffix.
//
// A parameter prints as "github.com/org/repo/internal/importx.Job". Taking only the
// type name would produce "Job", which collides with an unrelated jobs.Job used
// elsewhere in the document; keeping the package too gives "ImportxJob", which is
// unique without needing an override for every instantiation.
func genericArgName(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "*")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	parts := strings.Split(p, ".")
	start := 0
	if len(parts) > 1 {
		// Keep the package segment as well as the type name.
		start = len(parts) - 2
	}
	var out strings.Builder
	for _, part := range parts[start:] {
		if part == "" {
			continue
		}
		out.WriteString(strings.ToUpper(part[:1]))
		out.WriteString(part[1:])
	}
	return out.String()
}
