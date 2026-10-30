//go:build integration

// Tags and comments through the real HTTP stack.
//
// The point of these tests is the read path the interface needs and the tenant
// boundary around it. A tag is scoped to an organization and a comment is scoped
// to the record it names, so a caller in one tenant must not see a tag attached
// in another, and a caller holding only the read permission must not be able to
// attach one.
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

type tagItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type commentItem struct {
	ID         string `json:"id"`
	Body       string `json:"body"`
	AuthorName string `json:"author_name"`
}

// listTags decodes the {items:[...]} envelope the read routes return.
func decodeList[T any](t *testing.T, body []byte) []T {
	t.Helper()
	var env struct {
		Items []T `json:"items"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode items: %v (body %s)", err, body)
	}
	return env.Items
}

func TestTagsAreAttachedAndListedThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Tag Tenant")
	admin := h.seedUser("tag-admin@example.com", "Tag-Admin-Passw0rd-2026!", "Admin")
	h.joinOrganization(admin, org, "tenant_admin")
	a := h.login("tag-admin@example.com", "Tag-Admin-Passw0rd-2026!")

	account := h.seedAccount(org, "Acme")

	// Create a tag.
	rec := h.do(a, http.MethodPost, "/api/v1/tags", `{"name":"Enterprise","color":"#0f0"}`)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create tag: status %d, body %s", rec.Code, rec.Body.String())
	}
	var created tagItem
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode tag: %v (body %s)", err, rec.Body.String())
	}

	// Attach it to the account.
	body, _ := json.Marshal(map[string]string{"entity_type": "account", "entity_id": account.String()})
	rec = h.do(a, http.MethodPost, "/api/v1/tags/"+created.ID+"/attach", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("attach tag: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Read the tags attached to that one record.
	rec = h.do(a, http.MethodGet, "/api/v1/tags/attached?entity_type=account&entity_id="+account.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list attached: status %d, body %s", rec.Code, rec.Body.String())
	}
	items := decodeList[tagItem](t, rec.Body.Bytes())
	if len(items) != 1 || items[0].Name != "Enterprise" {
		t.Fatalf("expected the attached tag, got %+v", items)
	}

	// A different, untagged record must show nothing.
	other := h.seedAccount(org, "Beta")
	rec = h.do(a, http.MethodGet, "/api/v1/tags/attached?entity_type=account&entity_id="+other.String(), "")
	if got := decodeList[tagItem](t, rec.Body.Bytes()); len(got) != 0 {
		t.Fatalf("untagged record returned %+v", got)
	}
}

func TestAttachedTagsDoNotCrossOrganizations(t *testing.T) {
	h := newHarness(t)
	orgA := h.seedOrganization("Org A")
	orgB := h.seedOrganization("Org B")
	adminA := h.seedUser("a@example.com", "Org-A-Admin-Passw0rd-2026!", "A")
	adminB := h.seedUser("b@example.com", "Org-B-Admin-Passw0rd-2026!", "B")
	h.joinOrganization(adminA, orgA, "tenant_admin")
	h.joinOrganization(adminB, orgB, "tenant_admin")
	a1 := h.login("a@example.com", "Org-A-Admin-Passw0rd-2026!")
	a2 := h.login("b@example.com", "Org-B-Admin-Passw0rd-2026!")

	accountA := h.seedAccount(orgA, "Shared Name")

	rec := h.do(a1, http.MethodPost, "/api/v1/tags", `{"name":"Secret"}`)
	var tag tagItem
	if err := json.Unmarshal(rec.Body.Bytes(), &tag); err != nil {
		t.Fatalf("decode tag: %v (body %s)", err, rec.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"entity_type": "account", "entity_id": accountA.String()})
	if rec := h.do(a1, http.MethodPost, "/api/v1/tags/"+tag.ID+"/attach", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("attach: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Tenant B asks for the same entity id. It must not see tenant A's tag: the
	// query joins on the tag's organization, not only the link.
	rec = h.do(a2, http.MethodGet, "/api/v1/tags/attached?entity_type=account&entity_id="+accountA.String(), "")
	if got := decodeList[tagItem](t, rec.Body.Bytes()); len(got) != 0 {
		t.Fatalf("cross-tenant tag leaked: %+v", got)
	}

	// Tenant B cannot attach tenant A's tag even knowing its id.
	rec = h.do(a2, http.MethodPost, "/api/v1/tags/"+tag.ID+"/attach", string(body))
	if rec.Code == http.StatusOK {
		t.Fatalf("cross-tenant attach was accepted: %s", rec.Body.String())
	}
}

func TestTagAttachRequiresTheManagePermission(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Perm Tenant")
	admin := h.seedUser("perm-admin@example.com", "Perm-Admin-Passw0rd-2026!", "Admin")
	rep := h.seedUser("rep@example.com", "Perm-Rep-Passw0rd-2026!", "Rep")
	h.joinOrganization(admin, org, "tenant_admin")
	h.joinOrganization(rep, org, "sales_representative")
	aAdmin := h.login("perm-admin@example.com", "Perm-Admin-Passw0rd-2026!")
	aRep := h.login("rep@example.com", "Perm-Rep-Passw0rd-2026!")

	account := h.seedAccount(org, "Acme")
	rec := h.do(aAdmin, http.MethodPost, "/api/v1/tags", `{"name":"Prospect"}`)
	var tag tagItem
	if err := json.Unmarshal(rec.Body.Bytes(), &tag); err != nil {
		t.Fatalf("decode tag: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"entity_type": "account", "entity_id": account.String()})

	// A sales representative holds contacts.read but not tags.manage.
	rec = h.do(aRep, http.MethodPost, "/api/v1/tags/"+tag.ID+"/attach", string(body))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for attach without tags.manage, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCommentsRoundTripAndRequirePermission(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Comment Tenant")
	admin := h.seedUser("c-admin@example.com", "C-Admin-Passw0rd-2026!", "Admin")
	viewer := h.seedUser("c-viewer@example.com", "C-Viewer-Passw0rd-2026!", "Viewer")
	h.joinOrganization(admin, org, "tenant_admin")
	h.joinOrganization(viewer, org, "readonly_auditor")
	aAdmin := h.login("c-admin@example.com", "C-Admin-Passw0rd-2026!")
	aViewer := h.login("c-viewer@example.com", "C-Viewer-Passw0rd-2026!")

	account := h.seedAccount(org, "Acme")

	body, _ := json.Marshal(map[string]string{
		"entity_type": "account", "entity_id": account.String(), "body": "Called the buyer",
	})
	rec := h.do(aAdmin, http.MethodPost, "/api/v1/comments", string(body))
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create comment: status %d, body %s", rec.Code, rec.Body.String())
	}

	rec = h.do(aAdmin, http.MethodGet, "/api/v1/comments?entity_type=account&entity_id="+account.String(), "")
	items := decodeList[commentItem](t, rec.Body.Bytes())
	if len(items) != 1 || items[0].Body != "Called the buyer" {
		t.Fatalf("expected the comment, got %+v", items)
	}
	// The author's display name is resolved server-side, which is what lets the
	// interface attribute a comment without a second request.
	if items[0].AuthorName != "Admin" {
		t.Fatalf("expected author name Admin, got %q", items[0].AuthorName)
	}

	// A read-only auditor may read but not comment.
	if rec := h.do(aViewer, http.MethodGet,
		"/api/v1/comments?entity_type=account&entity_id="+account.String(), ""); rec.Code != http.StatusOK {
		t.Fatalf("viewer list: status %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(aViewer, http.MethodPost, "/api/v1/comments", string(body)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer comment, got %d: %s", rec.Code, rec.Body.String())
	}
}
