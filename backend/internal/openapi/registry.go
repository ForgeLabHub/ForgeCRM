package openapi

import (
	"reflect"
	"sort"
)

// mode records how a type was reached, because `required` depends on it.
//
// On a response, the struct tags are authoritative: a field without omitempty is
// always serialised, so it is always present, so it is required. On a request, the
// tags say nothing — the validation rules decide, and they are declared in the
// contract. Propagating the mode through nested types keeps a request's nested
// object from inheriting response-style required fields.
type mode int

const (
	modeRequest mode = iota
	modeResponse
)

// registry accumulates the component schemas reachable from the types the API
// actually uses.
//
// It walks types on demand: describing a type registers any named struct it
// references, and the referenced struct is described in turn. This means the
// components section contains exactly the types the endpoints need, and a type that
// no endpoint uses cannot appear. Listing every type in the domain package instead
// produces a large section of schemas that no operation references, which is
// documentation for an API that does not exist.
type registry struct {
	types    map[string]reflect.Type
	required map[string][]string
	modes    map[string]mode
	anon     map[reflect.Type]string
	names    map[reflect.Type]string
	shared   map[reflect.Type]bool
	n        int
}

func newRegistry(names map[reflect.Type]string, shared map[reflect.Type]bool) *registry {
	if names == nil {
		names = map[reflect.Type]string{}
	}
	if shared == nil {
		shared = map[reflect.Type]bool{}
	}
	return &registry{
		types:    map[string]reflect.Type{},
		required: map[string][]string{},
		modes:    map[string]mode{},
		anon:     map[reflect.Type]string{},
		names:    names,
		shared:   shared,
	}
}

// addRequest registers a request component with its declared required fields.
func (r *registry) addRequest(name string, t reflect.Type, required []string) {
	r.register(name, t, required, modeRequest)
}

// addResponse registers a response component, deriving required fields from tags.
func (r *registry) addResponse(name string, t reflect.Type) {
	r.register(name, t, tagRequired(t), modeResponse)
}

// add registers a type reached while describing another type. Its required fields
// are unknown, so none are declared.
func (r *registry) add(name string, t reflect.Type, m mode) {
	r.register(name, t, nil, m)
}

func (r *registry) register(name string, t reflect.Type, required []string, m mode) {
	if existing, ok := r.types[name]; ok {
		if existing != t {
			// Two different types claiming one name would describe one type with the
			// other's fields, which is silently wrong in a way a client cannot detect.
			panic("openapi: component name " + name + " is claimed by two types: " +
				existing.String() + " and " + t.String())
		}
		if prev, ok := r.modes[name]; ok && prev != m {
			// The same type reached as both a request and a response would need two
			// different required sets. That is a real modelling problem, not something
			// to paper over, so it stops generation.
			panic("openapi: type " + t.String() + " is used as both a request and a response, " +
				"so its required fields are ambiguous; give it a distinct type for one direction")
		}
		// A later registration may know the required set when an earlier one did not
		// (a type reached as a nested object, then declared as a body).
		if len(required) > 0 {
			r.required[name] = required
		}
		return
	}
	r.types[name] = t
	r.modes[name] = m
	if len(required) > 0 {
		r.required[name] = required
	}
}

// anonymousName assigns a stable name to a struct with no type name.
func (r *registry) anonymousName(t reflect.Type) string {
	if n, ok := r.anon[t]; ok {
		return n
	}
	r.n++
	name := "AnonymousObject"
	if r.n > 1 {
		name = name + itoa(r.n)
	}
	r.anon[t] = name
	return name
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// build resolves every registered type into a schema, following references until
// the set is closed.
//
// Types are recorded before their fields are described, so a self-referential type
// terminates instead of recursing forever.
func (r *registry) build() map[string]any {
	out := map[string]any{}

	for {
		names := make([]string, 0, len(r.types))
		for name := range r.types {
			if _, done := out[name]; !done {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			break
		}
		sort.Strings(names)

		for _, name := range names {
			t := r.types[name]
			// Reserve the slot first: describing the fields may register more types,
			// including this one.
			out[name] = map[string]any{}
			out[name] = objectSchema(t, r, r.required[name], r.modes[name])
		}
	}

	return out
}

// tagRequired derives the required field set for a response from its struct tags.
//
// A field with omitempty may be absent; a field without it is always serialised. A
// pointer without omitempty serialises as null rather than being absent, so it is
// still present and counts as required.
func tagRequired(t reflect.Type) []string {
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, ok := jsonName(f)
		if !ok {
			continue
		}
		if opts["omitempty"] {
			continue
		}
		required = append(required, name)
	}
	return required
}
