package api

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

// telegramTaskOperations is the narrow, ownership-enforcing CRM interface. Callers must resolve
// the principal from the active Telegram link for each update, not trust an ID
// or a principal supplied in the message.
type telegramTaskOperations interface {
	ListMyOpenTasks(context.Context, *rbac.Principal, *time.Time, *time.Time, int) ([]crm.Task, error)
	ListMyOverdueTasks(context.Context, *rbac.Principal, time.Time, int) ([]crm.Task, error)
	GetMyTask(context.Context, *rbac.Principal, uuid.UUID) (*crm.Task, error)
	CompleteMyTask(context.Context, *rbac.Principal, uuid.UUID) (*crm.Task, error)
	RescheduleMyTask(context.Context, *rbac.Principal, uuid.UUID, time.Time) (*crm.Task, error)
}

// handleTelegramTaskCommand returns a localized reply and its inline keyboard, or
// an error. The transport must translate errors into generic responses and never
// expose or log the incoming text.
// /today uses UTC explicitly; /due requires an RFC3339 timestamp with timezone.
func handleTelegramTaskCommand(ctx context.Context, tasks telegramTaskOperations, p *rbac.Principal, text string, now time.Time, loc telegramtext.Locale) (string, [][]notify.InlineButton, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return telegramtext.T(loc, telegramtext.KeyUsageTask), nil, nil
	}
	cmd := strings.ToLower(parts[0])
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	switch cmd {
	case "/tasks", "/today", "/overdue":
		if len(parts) != 1 {
			return fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyUsageTasks), cmd), nil, nil
		}
		var (
			items   []crm.Task
			header  telegramtext.Key
			empty   telegramtext.Key
			listErr error
		)
		switch cmd {
		case "/today":
			t := now.UTC()
			start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			header, empty = telegramtext.KeyTodayTasksHeader, telegramtext.KeyNoOpenTasks
			items, listErr = tasks.ListMyOpenTasks(ctx, p, &start, &end, 10)
		case "/overdue":
			header, empty = telegramtext.KeyOverdueHeader, telegramtext.KeyNoOverdueTasks
			items, listErr = tasks.ListMyOverdueTasks(ctx, p, now, 10)
		default:
			header, empty = telegramtext.KeyOpenTasksHeader, telegramtext.KeyNoOpenTasks
			items, listErr = tasks.ListMyOpenTasks(ctx, p, nil, nil, 10)
		}
		if listErr != nil {
			return "", nil, listErr
		}
		if len(items) == 0 {
			return telegramtext.T(loc, empty), nil, nil
		}
		var b strings.Builder
		b.WriteString(telegramtext.T(loc, header))
		rows := make([][]notify.InlineButton, 0, len(items))
		for _, task := range items {
			fmt.Fprintf(&b, "\n%s — %s", task.ID, safeTitle(task.Title))
			if task.DueAt != nil {
				fmt.Fprintf(&b, " (%s)", fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyDuePrefix), task.DueAt.UTC().Format(time.RFC3339)))
			}
			id := task.ID.String()
			rows = append(rows, []notify.InlineButton{
				{Text: "📄 " + id[:8], Data: "task:" + id},
				{Text: "✅", Data: "done:" + id},
			})
		}
		b.WriteByte('\n')
		b.WriteString(telegramtext.T(loc, telegramtext.KeyOpenTaskWithID))
		return b.String(), rows, nil
	case "/task", "/done", "/due":
		want := 2
		if cmd == "/due" {
			want = 3
		}
		if len(parts) != want {
			switch cmd {
			case "/task":
				return telegramtext.T(loc, telegramtext.KeyUsageTask), nil, nil
			case "/done":
				return telegramtext.T(loc, telegramtext.KeyUsageDone), nil, nil
			default:
				return telegramtext.T(loc, telegramtext.KeyUsageDue), nil, nil
			}
		}
		id, err := uuid.Parse(parts[1])
		if err != nil || id == uuid.Nil {
			return telegramtext.T(loc, telegramtext.KeyInvalidTaskID), nil, nil
		}
		switch cmd {
		case "/task":
			task, err := tasks.GetMyTask(ctx, p, id)
			if err != nil {
				return "", nil, err
			}
			due := telegramtext.T(loc, telegramtext.KeyDueNotSet)
			if task.DueAt != nil {
				due = task.DueAt.UTC().Format(time.RFC3339)
			}
			body := fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyTaskDetail), safeTitle(task.Title), task.Status, due)
			rows := [][]notify.InlineButton{{{Text: "✅", Data: "done:" + id.String()}}}
			return body, rows, nil
		case "/done":
			if _, err := tasks.CompleteMyTask(ctx, p, id); err != nil {
				return "", nil, err
			}
			return telegramtext.T(loc, telegramtext.KeyTaskCompleted), nil, nil
		default:
			due, err := time.Parse(time.RFC3339, parts[2])
			if err != nil {
				return telegramtext.T(loc, telegramtext.KeyInvalidTimestamp), nil, nil
			}
			if _, err := tasks.RescheduleMyTask(ctx, p, id, due); err != nil {
				return "", nil, err
			}
			return telegramtext.T(loc, telegramtext.KeyTaskDueUpdated), nil, nil
		}
	default:
		return telegramtext.T(loc, telegramtext.KeyUnknownCommand), nil, nil
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
