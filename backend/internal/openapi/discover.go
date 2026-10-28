package openapi

import (
	"reflect"
)

// discover returns the types that are reachable in both directions.
//
// Some types are genuinely both a request and a response: a pipeline stage is sent
// by a client creating a pipeline and returned by the server with the id it
// assigned. The two directions need different `required` sets, and one component
// carries only one, so a shared type gets two components rather than one that is
// wrong for a caller. Which types are shared is a fact about the whole contract, not
// about a single route, so it is computed once by walking every declared type
// before any schema is built.
func (c Contract) discover() map[reflect.Type]bool {
	asRequest := map[reflect.Type]bool{}
	asResponse := map[reflect.Type]bool{}

	for _, t := range c.RequestBodies {
		walk(t, asRequest)
	}
	for _, t := range c.Responses {
		walk(t, asResponse)
	}

	shared := map[reflect.Type]bool{}
	for t := range asRequest {
		if asResponse[t] {
			shared[t] = true
		}
	}
	return shared
}

// walk records every named struct reachable from t, following fields.
//
// It does not follow map values or interface types: a map[string]any has no shape to
// describe, so descending into it would invent types that are not in the contract.
func walk(t reflect.Type, seen map[reflect.Type]bool) {
	if t == nil {
		return
	}
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	if seen[t] {
		return
	}
	seen[t] = true

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if _, _, ok := jsonName(f); !ok {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			walk(ft, seen)
		}
	}
}
