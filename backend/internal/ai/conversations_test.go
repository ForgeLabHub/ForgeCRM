package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/google/uuid"
)

func TestConversationTitleValidation(t *testing.T) {
	if got, err := conversationTitle("  Test  "); err != nil || got != "Test" {
		t.Fatalf("title: %q %v", got, err)
	}
	for _, title := range []string{"", "  ", strings.Repeat("я", 201), string([]byte{0xff})} {
		if _, err := conversationTitle(title); err == nil {
			t.Fatalf("accepted title %q", title)
		}
	}
}

func TestConversationValidationBeforeDatabase(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	p := rbac.NewPrincipal(uuid.New(), "", "", uuid.New(), []rbac.Permission{rbac.PermAIToolsExecute})
	if _, err := s.CreateConversation(ctx, nil, "title"); !errors.Is(err, rbac.ErrForbidden) {
		t.Fatalf("nil principal: %v", err)
	}
	if _, err := s.CreateConversation(ctx, p, ""); err == nil {
		t.Fatal("accepted blank title")
	}
	for _, test := range []struct{ role, content string }{{"system", "text"}, {"user", " "}, {"assistant", strings.Repeat("x", 16001)}} {
		if _, err := s.AppendConversationMessage(ctx, p, uuid.New(), test.role, test.content); err == nil {
			t.Fatalf("accepted invalid message: %q", test.role)
		}
	}
	if _, err := s.ListConversations(ctx, p, false, 101, 0); err == nil {
		t.Fatal("accepted excessive page size")
	}
	if _, err := s.ListConversationMessages(ctx, p, uuid.New(), 1, -1); err == nil {
		t.Fatal("accepted negative offset")
	}
}
