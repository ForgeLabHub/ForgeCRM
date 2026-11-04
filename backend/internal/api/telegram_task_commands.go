package api

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// telegramTaskOperations is the narrow, ownership-enforcing CRM interface. Callers must resolve
// the principal from the active Telegram link for each update, not trust an ID
// or a principal supplied in the message.
type telegramTaskOperations interface {
	ListMyOpenTasks(context.Context, *rbac.Principal, *time.Time, *time.Time, int) ([]crm.Task, error)
	GetMyTask(context.Context, *rbac.Principal, uuid.UUID) (*crm.Task, error)
	CompleteMyTask(context.Context, *rbac.Principal, uuid.UUID) (*crm.Task, error)
	RescheduleMyTask(context.Context, *rbac.Principal, uuid.UUID, time.Time) (*crm.Task, error)
}

// handleTelegramTaskCommand returns a reply or an error. The transport must translate
// errors into generic responses and never expose or log the incoming text.
// /today uses UTC explicitly; /due requires an RFC3339 timestamp with timezone.
func handleTelegramTaskCommand(ctx context.Context, tasks telegramTaskOperations, p *rbac.Principal, text string, now time.Time) (string, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "Send /tasks or /help.", nil
	}
	cmd := strings.ToLower(parts[0])
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	switch cmd {
	case "/tasks", "/today":
		if len(parts) != 1 {
			return "Usage: " + cmd, nil
		}
		var from, before *time.Time
		if cmd == "/today" {
			t := now.UTC()
			start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			from, before = &start, &end
		}
		items, err := tasks.ListMyOpenTasks(ctx, p, from, before, 10)
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "No open tasks found.", nil
		}
		var b strings.Builder
		if cmd == "/today" {
			b.WriteString("Tasks due today (UTC):\n")
		} else {
			b.WriteString("Your open tasks:\n")
		}
		for _, task := range items {
			fmt.Fprintf(&b, "%s — %s", task.ID, safeTitle(task.Title))
			if task.DueAt != nil {
				fmt.Fprintf(&b, " (due %s)", task.DueAt.UTC().Format(time.RFC3339))
			}
			b.WriteByte('\n')
		}
		b.WriteString("Open a task with /task followed by its ID.")
		return b.String(), nil
	case "/task", "/done", "/due":
		want := 2
		if cmd == "/due" {
			want = 3
		}
		if len(parts) != want {
			return "Usage: /task <id>, /done <id>, or /due <id> <RFC3339 timestamp>", nil
		}
		id, err := uuid.Parse(parts[1])
		if err != nil || id == uuid.Nil {
			return "Invalid task ID.", nil
		}
		switch cmd {
		case "/task":
			task, err := tasks.GetMyTask(ctx, p, id)
			if err != nil {
				return "", err
			}
			due := "not set"
			if task.DueAt != nil {
				due = task.DueAt.UTC().Format(time.RFC3339)
			}
			return fmt.Sprintf("%s\n%s\nStatus: %s\nDue: %s\nUse /done %s or /due %s <RFC3339 timestamp>.", task.ID, safeTitle(task.Title), task.Status, due, task.ID, task.ID), nil
		case "/done":
			if _, err := tasks.CompleteMyTask(ctx, p, id); err != nil {
				return "", err
			}
			return "Task completed.", nil
		default:
			due, err := time.Parse(time.RFC3339, parts[2])
			if err != nil {
				return "Invalid timestamp. Use RFC3339 with timezone, for example 2026-11-04T12:00:00Z.", nil
			}
			if _, err := tasks.RescheduleMyTask(ctx, p, id, due); err != nil {
				return "", err
			}
			return "Task due date updated.", nil
		}
	default:
		return "Unknown command. Send /help for available commands.", nil
	}
}

func safeTitle(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == 100 {
			b.WriteRune('…')
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
