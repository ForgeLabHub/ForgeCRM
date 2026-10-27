package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteJSONListSerialisesEmptyAsArray guards a bug that reached every
// non-paginated collection endpoint: a nil slice marshals to `null`, not `[]`,
// so `{"items": null}` was returned whenever the collection was empty. A client
// that iterates the array crashes on null, and the shape differed from the
// paginated endpoints for no reason.
func TestWriteJSONListSerialisesEmptyAsArray(t *testing.T) {
	t.Run("nil slice becomes an empty array", func(t *testing.T) {
		var items []string
		rec := httptest.NewRecorder()

		WriteJSONList(rec, items)

		body := rec.Body.String()
		if strings.Contains(body, "null") {
			t.Fatalf("body contains null: %s", body)
		}
		var decoded struct {
			Items []string `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("decode: %v (body %s)", err, body)
		}
		if decoded.Items == nil {
			t.Fatalf("items decoded as nil, want an empty slice (body %s)", body)
		}
		if len(decoded.Items) != 0 {
			t.Fatalf("items has %d entries, want 0", len(decoded.Items))
		}
	})

	t.Run("populated slice is preserved", func(t *testing.T) {
		rec := httptest.NewRecorder()

		WriteJSONList(rec, []string{"a", "b"})

		var decoded struct {
			Items []string `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(decoded.Items) != 2 || decoded.Items[0] != "a" || decoded.Items[1] != "b" {
			t.Fatalf("items = %v, want [a b]", decoded.Items)
		}
	})
}

// TestNewListResponseSerialisesEmptyAsArray covers the same guarantee on the
// paginated path, which callers depend on for `total` and `limit` as well.
func TestNewListResponseSerialisesEmptyAsArray(t *testing.T) {
	var items []int
	resp := NewListResponse(items, 0, Pagination{Limit: 50, Offset: 0})

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), `"items":null`) {
		t.Fatalf("items serialised as null: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"items":[]`) {
		t.Fatalf("items did not serialise as an empty array: %s", encoded)
	}
	if resp.Total != 0 || resp.Limit != 50 {
		t.Fatalf("metadata lost: total=%d limit=%d", resp.Total, resp.Limit)
	}
}
