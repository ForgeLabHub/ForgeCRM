package workflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// EvalContext is the data a condition is evaluated against. `Fields` is the
// flattened view of the trigger event: the event envelope plus its payload, with
// nested objects reachable by dotted path (for example "payload.amount").
type EvalContext struct {
	Fields map[string]any
}

// NewEvalContext flattens an event envelope and payload into a field map.
//
// The envelope keys are exposed unprefixed (event_type, event_id, source, …) and
// the payload under "payload.", so a condition reads the same way the webhook
// payload does. Flattening rather than storing a tree means a condition is a flat
// lookup, and an unknown path is simply absent — which "exists" can test for.
func NewEvalContext(envelope map[string]any, payload json.RawMessage) *EvalContext {
	fields := map[string]any{}
	for k, v := range envelope {
		fields[k] = v
	}
	if len(payload) > 0 {
		var decoded any
		if err := json.Unmarshal(payload, &decoded); err == nil {
			flatten(fields, "payload", decoded)
		}
	}
	return &EvalContext{Fields: fields}
}

// flatten walks a decoded JSON value and records every leaf under a dotted path.
// Objects and arrays are both recorded as leaves as well, so a condition can test
// "payload.tags contains x" against an array without a special case.
func flatten(out map[string]any, prefix string, v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			flatten(out, prefix+"."+k, child)
		}
	case []any:
		out[prefix] = t
		for i, child := range t {
			flatten(out, fmt.Sprintf("%s.%d", prefix, i), child)
		}
	default:
		out[prefix] = t
	}
}

// Evaluate reports whether the condition holds in the given context.
func (c *Condition) Evaluate(ctx *EvalContext) bool {
	if c == nil {
		return true
	}
	switch c.Op {
	case "and":
		for i := range c.All {
			if !c.All[i].Evaluate(ctx) {
				return false
			}
		}
		return true
	case "or":
		for i := range c.Any {
			if c.Any[i].Evaluate(ctx) {
				return true
			}
		}
		return false
	case "not":
		return !c.Not.Evaluate(ctx)
	}

	actual, present := ctx.Fields[c.Field]
	switch c.Op {
	case "exists":
		return present && actual != nil
	case "eq":
		return compareEqual(actual, c.Value)
	case "ne":
		return !compareEqual(actual, c.Value)
	case "gt", "gte", "lt", "lte":
		return compareOrdered(actual, c.Value, c.Op)
	case "contains":
		return containsValue(actual, c.Value)
	case "starts_with":
		s, ok := actual.(string)
		p, ok2 := c.Value.(string)
		return ok && ok2 && strings.HasPrefix(s, p)
	case "ends_with":
		s, ok := actual.(string)
		p, ok2 := c.Value.(string)
		return ok && ok2 && strings.HasSuffix(s, p)
	case "in":
		return inList(actual, c.Value)
	}
	return false
}

// compareEqual compares two JSON-decoded values for equality, treating numbers by
// their numeric value rather than their Go type so 1 (int in a literal) equals 1
// (float64 from JSON).
func compareEqual(a, b any) bool {
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
	}
	if ab, ok := a.(bool); ok {
		if bb, ok := b.(bool); ok {
			return ab == bb
		}
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return as == bs
	}
	return reflect.DeepEqual(a, b)
}

func compareOrdered(a, b any, op string) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch op {
		case "gt":
			return af > bf
		case "gte":
			return af >= bf
		case "lt":
			return af < bf
		case "lte":
			return af <= bf
		}
		return false
	}
	// Fall back to string ordering for timestamps and other lexical types.
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		switch op {
		case "gt":
			return as > bs
		case "gte":
			return as >= bs
		case "lt":
			return as < bs
		case "lte":
			return as <= bs
		}
	}
	return false
}

func containsValue(haystack, needle any) bool {
	switch h := haystack.(type) {
	case []any:
		for _, item := range h {
			if compareEqual(item, needle) {
				return true
			}
		}
		return false
	case string:
		s, ok := needle.(string)
		return ok && strings.Contains(h, s)
	default:
		return false
	}
}

func inList(actual, list any) bool {
	items, ok := list.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if compareEqual(actual, item) {
			return true
		}
	}
	return false
}

// toFloat converts a numeric value of any JSON-decoded type to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
