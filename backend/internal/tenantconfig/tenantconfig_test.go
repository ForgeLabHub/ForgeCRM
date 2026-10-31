package tenantconfig

import (
	"errors"
	"testing"
)

// TestCatalogIsClosedAndWellFormed pins the invariants the catalog must hold for
// the surface to be honest: every key has a default of its own kind, an enum key
// declares its set, and no two keys collide.
func TestCatalogIsClosedAndWellFormed(t *testing.T) {
	defs := Catalog()
	if len(defs) == 0 {
		t.Fatal("the catalog is empty; the settings surface would have nothing to read")
	}
	for _, d := range defs {
		if d.Key == "" {
			t.Error("a catalog entry has an empty key")
		}
		if d.Title == "" {
			t.Errorf("%s: no title; the editor would show a raw key", d.Key)
		}
		if d.Description == "" {
			t.Errorf("%s: no description; the operator cannot tell what it affects", d.Key)
		}
		if d.Since < 1 {
			t.Errorf("%s: Since = %d, must be at least 1", d.Key, d.Since)
		}
		if d.Default == nil {
			t.Errorf("%s: no default; an absent row could not resolve", d.Key)
		}
		// The declared default must itself pass the key's own validation, or the
		// platform would ship a value it would refuse from a caller.
		if _, err := validate(d, d.Default); err != nil {
			t.Errorf("%s: the declared default %v does not validate: %v", d.Key, d.Default, err)
		}
		if d.Kind == KindEnum && len(d.Enum) == 0 {
			t.Errorf("%s: an enum with no permitted values", d.Key)
		}
	}
}

// TestValidateEnforcesEachKind is the boundary an operator's input hits first.
func TestValidateEnforcesEachKind(t *testing.T) {
	strDef := Definition{Key: "k.string", Kind: KindString, MaxLength: 5}
	enumDef := Definition{Key: "k.enum", Kind: KindEnum, Enum: []string{"a", "b"}}
	intDef := Definition{Key: "k.int", Kind: KindInteger, Min: int64Ptr(1), Max: int64Ptr(10)}
	boolDef := Definition{Key: "k.bool", Kind: KindBoolean}

	cases := []struct {
		name    string
		def     Definition
		raw     any
		wantErr bool
	}{
		{"string accepted", strDef, "abc", false},
		{"string trimmed", strDef, "  ab  ", false},
		{"string not a string", strDef, 5, true},
		{"string empty", strDef, "   ", true},
		{"string too long", strDef, "abcdef", true},
		{"enum member", enumDef, "a", false},
		{"enum outside the set", enumDef, "c", true},
		{"enum wrong type", enumDef, 1, true},
		{"int in range", intDef, float64(5), false},
		{"int below the minimum", intDef, float64(0), true},
		{"int above the maximum", intDef, float64(11), true},
		{"int not whole", intDef, 5.5, true},
		{"int wrong type", intDef, "5", true},
		{"bool true", boolDef, true, false},
		{"bool wrong type", boolDef, "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validate(tc.def, tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("validate(%v) = nil, want an error", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validate(%v) = %v, want nil", tc.raw, err)
			}
		})
	}
}

// TestValidationErrorNamesTheKey is the contract with the API: a refused value
// carries the offending key, so the caller sees which field was wrong rather than
// a bare 422.
func TestValidationErrorNamesTheKey(t *testing.T) {
	_, err := validate(Definition{Key: "k.int", Kind: KindInteger}, "not a number")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a *ValidationError, got %T", err)
	}
	if _, ok := ve.Fields["k.int"]; !ok {
		t.Fatalf("the error does not name the key: %v", ve.Fields)
	}
}

// TestLookupIsCaseSensitive pins that a key is matched exactly. A case-insensitive
// match would let two spellings reach the same setting, which is how a rename
// starts to look like it worked.
func TestLookupIsCaseSensitive(t *testing.T) {
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("no catalog keys")
	}
	if _, ok := Lookup(keys[0]); !ok {
		t.Fatalf("Lookup(%q) = false for a key the catalog lists", keys[0])
	}
	upper := ""
	for _, r := range keys[0] {
		if r >= 'a' && r <= 'z' {
			upper += string(r - 32)
			continue
		}
		upper += string(r)
	}
	if upper == keys[0] {
		t.Skip("the first key has no lower-case letter to vary")
	}
	if _, ok := Lookup(upper); ok {
		t.Errorf("Lookup(%q) = true; a key must match exactly", upper)
	}
}
