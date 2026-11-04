package telegramtext

// english is the reference dictionary. A key added here must be added to every
// other locale; TestCatalogsAreComplete enforces that.
var english = map[Key]string{
	KeyWelcome: "Hi! I'm the ForgeCRM Assistant — a helper for your tasks and CRM.\n\n" +
		"I can:\n" +
		"• show your tasks\n" +
		"• remind you about deadlines\n" +
		"• tell you about changes\n" +
		"• help you get information from the CRM\n" +
		"• perform the actions available to you\n\n" +
		"First, choose a language:",
	KeyChooseLanguage: "Choose your language:",
	KeyLanguageSet:    "Done — I'll write to you in English.",
	KeyHelp: "ForgeCRM Assistant\n\n" +
		"What I can do:\n" +
		"• /tasks — your open tasks\n" +
		"• /overdue — your overdue tasks\n" +
		"• /profile — your linked account\n" +
		"• /language — change the language\n" +
		"• /help — this help\n\n" +
		"To link your account: open ForgeCRM → Settings → Telegram, generate a code and send /link <code>.\n" +
		"To get notifications: link your account and keep notifications on in ForgeCRM.\n" +
		"If a command does not work, send /start to begin again.",
	KeyMenuTitle:          "Main menu — pick an action:",
	KeyUnknownCommand:     "I don't know that command. Send /help to see what I can do.",
	KeyNotLinked:          "This Telegram account is not linked to ForgeCRM yet. Open ForgeCRM → Settings → Telegram, generate a code and send /link <code>.",
	KeyLinkUsage:          "Send /link <code>, where the code comes from ForgeCRM → Settings → Telegram.",
	KeyLinkSuccess:        "Linked to your ForgeCRM account. Send /tasks to see your open tasks.",
	KeyLinkFailed:         "I couldn't link this account. Check that the code is current, or unlink the existing account first.",
	KeyUnlinked:           "This Telegram account is no longer linked to ForgeCRM.",
	KeyNoOpenTasks:        "You have no open tasks.",
	KeyOpenTasksHeader:    "Your open tasks:",
	KeyTodayTasksHeader:   "Tasks due today (UTC):",
	KeyOverdueHeader:      "Your overdue tasks:",
	KeyNoOverdueTasks:     "You have no overdue tasks.",
	KeyTaskDetail:         "%s\nStatus: %s\nDue: %s",
	KeyTaskCompleted:      "Task completed.",
	KeyTaskDueUpdated:     "Task due date updated.",
	KeyTaskNotFound:       "I couldn't find that task, or it isn't yours.",
	KeyTaskConflict:       "That task can't be changed in its current state.",
	KeyInvalidTaskID:      "That isn't a valid task ID.",
	KeyInvalidTimestamp:   "That isn't a valid date. Use RFC3339 with a timezone, for example 2026-11-04T12:00:00Z.",
	KeyNoPermission:       "You don't have permission for this action.",
	KeyProfile:            "Account: %s\nOrganization: %s\nLanguage: %s",
	KeyNotificationsHdr:   "Your recent notifications:",
	KeyNoNotifications:    "You have no new notifications.",
	KeyServiceUnavailable: "Notifications can't be delivered right now — the bot's webhook is off. Please contact your administrator.",
	KeyGenericError:       "Something went wrong on my side. Please try again in a moment.",
	BtnTasks:              "📋 My tasks",
	BtnOverdue:            "⏰ Overdue",
	BtnProfile:            "👤 Profile",
	BtnLanguage:           "🌐 Language",
	BtnHelp:               "❓ Help",
	BtnMenu:               "⬅️ Menu",
	KeyUsageTasks:         "Usage: %s",
	KeyUsageTask:          "Usage: /task <id>",
	KeyUsageDone:          "Usage: /done <id>",
	KeyUsageDue:           "Usage: /due <id> <RFC3339 timestamp>",
	KeyDueNotSet:          "not set",
	KeyDuePrefix:          "due %s",
	KeyOpenTaskWithID:     "Open a task with /task followed by its ID.",
	KeyTaskLine:           "%s — %s",
}
