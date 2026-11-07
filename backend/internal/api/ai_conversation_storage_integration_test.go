//go:build integration

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/google/uuid"
)

// This tests the storage service, not an HTTP API or a live AI provider.
func TestConversationStorageIsPrivateAndCascades(t *testing.T) {
	h := newHarness(t)
	org := h.seedOrganization("Conversation tenant")
	otherOrg := h.seedOrganization("Other tenant")
	owner := h.seedUser("conv-owner@example.test", "Secret-2026!", "Owner")
	other := h.seedUser("conv-other@example.test", "Secret-2026!", "Other")
	h.joinOrganization(owner, org, "sales_manager")
	// joinOrganization creates an org-scoped role, so add the second member
	// without trying to create the same role a second time.
	if _, err := h.db.Pool().Exec(context.Background(), `INSERT INTO memberships (user_id, organization_id, status) VALUES ($1,$2,'active')`, other, org); err != nil {
		t.Fatal(err)
	}
	h.joinOrganization(owner, otherOrg, "sales_manager")
	principal := func(user, organization uuid.UUID, perms ...rbac.Permission) *rbac.Principal {
		return rbac.NewPrincipal(user, "", "", organization, perms)
	}
	p := principal(owner, org, rbac.PermAIToolsExecute)
	svc := ai.NewService(&config.AIConfig{}, h.db, nil, nil, nil, nil)
	ctx := context.Background()
	c, err := svc.CreateConversation(ctx, p, "  First session  ")
	if err != nil || c.Title != "First session" {
		t.Fatalf("create: %+v %v", c, err)
	}
	if _, err = svc.AppendConversationMessage(ctx, p, c.ID, "user", "private message"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendConversationMessage(ctx, p, c.ID, "assistant", "reply"); err != nil {
		t.Fatal(err)
	}
	msgs, err := svc.ListConversationMessages(ctx, p, c.ID, 100, 0)
	if err != nil || len(msgs) != 2 || msgs[0].Content != "private message" || msgs[1].Content != "reply" {
		t.Fatalf("messages: %+v %v", msgs, err)
	}
	for _, attacker := range []*rbac.Principal{principal(other, org, rbac.PermAIToolsExecute), principal(owner, otherOrg, rbac.PermAIToolsExecute)} {
		if _, err := svc.GetConversation(ctx, attacker, c.ID); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("foreign get: %v", err)
		}
		if _, err := svc.ListConversationMessages(ctx, attacker, c.ID, 20, 0); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("foreign messages: %v", err)
		}
		if _, err := svc.UpdateConversation(ctx, attacker, c.ID, "changed", false, false); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("foreign update: %v", err)
		}
		if _, err := svc.AppendConversationMessage(ctx, attacker, c.ID, "user", "bad"); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("foreign append: %v", err)
		}
		if err := svc.DeleteConversation(ctx, attacker, c.ID); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("foreign delete: %v", err)
		}
		list, err := svc.ListConversations(ctx, attacker, false, 20, 0)
		if err != nil || len(list) != 0 {
			t.Fatalf("foreign list: %+v %v", list, err)
		}
	}
	if _, err := svc.GetConversation(ctx, principal(owner, org), c.ID); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("missing permission: %v", err)
	}
	// Enforce the composite owner constraint independently of the Go service.
	if _, err := h.db.Pool().Exec(ctx, `INSERT INTO ai_messages (conversation_id,organization_id,user_id,role,content) VALUES ($1,$2,$3,'user','bad')`, c.ID, org, other); err == nil {
		t.Fatal("cross-owner message accepted by database")
	}
	c, err = svc.UpdateConversation(ctx, p, c.ID, "Renamed", true, true)
	if err != nil || c.ArchivedAt == nil || !c.Pinned {
		t.Fatalf("archive: %+v %v", c, err)
	}
	if _, err := svc.AppendConversationMessage(ctx, p, c.ID, "user", "bad"); !errors.Is(err, httpx.ErrNotFound) {
		t.Fatalf("append to archive: %v", err)
	}
	archived, err := svc.ListConversations(ctx, p, true, 20, 0)
	if err != nil || len(archived) != 1 {
		t.Fatalf("archive list: %+v %v", archived, err)
	}
	if err := svc.DeleteConversation(ctx, p, c.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := h.db.Pool().QueryRow(ctx, `SELECT count(*) FROM ai_messages WHERE conversation_id=$1`, c.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade: %d %v", count, err)
	}
}
