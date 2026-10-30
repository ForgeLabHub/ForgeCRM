package openapi

import (
	"strings"
	"testing"
)

// The site is a view of the committed document. These tests assert the
// properties that make it trustworthy: it renders every operation the document
// declares, it refuses a document that is not OpenAPI, and it escapes text rather
// than emitting it as markup.

func TestSiteRendersEveryOperation(t *testing.T) {
	doc, err := Build(
		[]Route{
			{Method: "GET", Path: "/api/v1/accounts"},
			{Method: "POST", Path: "/api/v1/accounts"},
			{Method: "GET", Path: "/api/v1/accounts/{id}"},
		},
		map[string]string{
			"GET /api/v1/accounts":      "accounts.read",
			"POST /api/v1/accounts":     "accounts.create",
			"GET /api/v1/accounts/{id}": "accounts.read",
		},
		Contract{},
		Info{Title: "Test API", Version: "1.2.3", Description: "A test document."},
		nil,
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	html, err := Site(doc)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	out := string(html)

	for _, want := range []string{
		"Test API",
		"1.2.3",
		"/api/v1/accounts",
		"/api/v1/accounts/{id}",
		`id="op-get-api-v1-accounts"`,
		`id="op-post-api-v1-accounts"`,
		`id="op-get-api-v1-accounts-id"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered site does not contain %q", want)
		}
	}

	// Each operation must appear as a body article, not only in the sidebar, or
	// the filter and the anchors would point at nothing.
	if n := strings.Count(out, `<article class="op" id="op-`); n != 3 {
		t.Errorf("rendered %d operation articles, want 3", n)
	}
}

func TestSiteRefusesANonOpenAPIDocument(t *testing.T) {
	if _, err := Site([]byte(`{"hello":"world"}`)); err == nil {
		t.Fatal("a JSON object with no openapi version must be refused")
	}
	if _, err := Site([]byte(`not json`)); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
}

// TestSiteEscapesText proves a description is shown as text, not markup. A
// description carrying a tag must not be able to inject one into the page.
func TestSiteEscapesText(t *testing.T) {
	doc, err := Build(
		[]Route{{Method: "GET", Path: "/api/v1/thing"}},
		map[string]string{"GET /api/v1/thing": "thing.read"},
		Contract{},
		Info{Title: "T", Version: "1", Description: "Danger: <script>alert(1)</script> and <b>x</b>."},
		nil,
	)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	html, err := Site(doc)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	out := string(html)
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatal("a description was emitted as markup")
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("the escaped form of the description is missing")
	}
}

func TestSlugIsIDStable(t *testing.T) {
	cases := map[string]string{
		"/api/v1/accounts/{id}/contacts": "api-v1-accounts-id-contacts",
		"DealInput":                      "dealinput",
		"users-and-permissions":          "users-and-permissions",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
