package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/rbac"
)

type taskFake struct {
	items        []crm.Task
	from, before *time.Time
	id           uuid.UUID
	due          time.Time
	called       string
	err          error
}

func (f *taskFake) ListMyOpenTasks(_ context.Context, _ *rbac.Principal, from, before *time.Time, limit int) ([]crm.Task, error) {
	f.called = "list"
	f.from = from
	f.before = before
	if limit != 10 {
		panic("unexpected limit")
	}
	return f.items, f.err
}
func (f *taskFake) GetMyTask(_ context.Context, _ *rbac.Principal, id uuid.UUID) (*crm.Task, error) {
	f.called = "get"
	f.id = id
	if f.err != nil {
		return nil, f.err
	}
	return &f.items[0], nil
}
func (f *taskFake) CompleteMyTask(_ context.Context, _ *rbac.Principal, id uuid.UUID) (*crm.Task, error) {
	f.called = "complete"
	f.id = id
	return nil, f.err
}
func (f *taskFake) RescheduleMyTask(_ context.Context, _ *rbac.Principal, id uuid.UUID, due time.Time) (*crm.Task, error) {
	f.called = "due"
	f.id = id
	f.due = due
	return nil, f.err
}

func TestTaskCommands(t *testing.T) {
	id := uuid.New()
	f := &taskFake{items: []crm.Task{{ID: id, Title: "Пример\n/fake", Status: "open"}}}
	p := rbac.NewPrincipal(uuid.New(), "", "", uuid.New(), nil)
	ctx := context.Background()
	now := time.Date(2026, 11, 4, 3, 0, 0, 0, time.FixedZone("+3", 3*60*60))
	text, err := handleTelegramTaskCommand(ctx, f, p, "/today@bot", now)
	if err != nil || f.called != "list" || f.from == nil || !f.from.Equal(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)) || f.before.Sub(*f.from) != 24*time.Hour {
		t.Fatalf("today: %q %v %+v", text, err, f)
	}
	if strings.Contains(text, "\n/fake") || !strings.Contains(text, "Пример /fake") {
		t.Fatalf("unsafe title: %q", text)
	}
	text, err = handleTelegramTaskCommand(ctx, f, p, "/tasks", now)
	if err != nil || f.from != nil || f.before != nil || !strings.Contains(text, id.String()) {
		t.Fatalf("list: %q %v", text, err)
	}
	text, err = handleTelegramTaskCommand(ctx, f, p, "/task "+id.String(), now)
	if err != nil || f.called != "get" || f.id != id || !strings.Contains(text, "Status: open") {
		t.Fatalf("task: %q %v", text, err)
	}
	_, err = handleTelegramTaskCommand(ctx, f, p, "/done "+id.String(), now)
	if err != nil || f.called != "complete" || f.id != id {
		t.Fatalf("done: %v", err)
	}
	_, err = handleTelegramTaskCommand(ctx, f, p, "/due "+id.String()+" 2026-11-04T12:00:00+03:00", now)
	if err != nil || f.called != "due" || !f.due.Equal(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("due: %v", err)
	}
}

func TestTaskCommandsRejectMalformedAndDoNotEchoErrors(t *testing.T) {
	f := &taskFake{}
	for _, input := range []string{"/done no", "/due invalid secret", "/due " + uuid.NewString() + " tomorrow", "/tasks extra", "/unknown"} {
		out, err := handleTelegramTaskCommand(context.Background(), f, nil, input, time.Now())
		if err != nil || out == "" || f.called != "" || strings.Contains(out, "secret") {
			t.Fatalf("%s: %q %v %s", input, out, err, f.called)
		}
	}
	f.err = errors.New("sensitive database error")
	out, err := handleTelegramTaskCommand(context.Background(), f, nil, "/tasks", time.Now())
	if err == nil || out != "" {
		t.Fatalf("error exposed: %q %v", out, err)
	}
}
