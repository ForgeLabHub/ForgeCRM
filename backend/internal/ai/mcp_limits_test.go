package ai

import (
	"encoding/json"
	"testing"
)

func TestBoundedLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want int
		bad  bool
	}{
		{"default", nil, 20, false},
		{"minimum", map[string]any{"limit": float64(1)}, 1, false},
		{"maximum", map[string]any{"limit": json.Number("50")}, 50, false},
		{"too large", map[string]any{"limit": float64(500000)}, 0, true},
		{"negative", map[string]any{"limit": -1}, 0, true},
		{"fraction", map[string]any{"limit": 1.5}, 0, true},
		{"string", map[string]any{"limit": "10"}, 0, true},
		{"null", map[string]any{"limit": nil}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := boundedLimit(tc.args, 20)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got (%d, %v), want (%d, error=%v)", got, err, tc.want, tc.bad)
			}
		})
	}
}
