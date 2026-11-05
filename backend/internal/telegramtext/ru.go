package telegramtext

// russian mirrors english. The two are kept in step by TestCatalogsAreComplete,
// which fails when a key exists in one dictionary and not the other.
var russian = map[Key]string{
	KeyWelcome: "Привет! Я ForgeCRM Assistant — помощник для твоих задач и CRM.\n\n" +
		"Я могу:\n" +
		"• показывать твои задачи\n" +
		"• напоминать о сроках\n" +
		"• сообщать об изменениях\n" +
		"• помогать получать информацию из CRM\n" +
		"• выполнять доступные тебе действия\n\n" +
		"Сначала выбери язык:",
	KeyChooseLanguage: "Выбери язык:",
	KeyLanguageSet:    "Готово — я буду писать по-русски.",
	KeyHelp: "ForgeCRM Assistant\n\n" +
		"Что я умею:\n" +
		"• /tasks — твои активные задачи\n" +
		"• /overdue — твои просроченные задачи\n" +
		"• /profile — твой привязанный аккаунт\n" +
		"• /notifications — последние уведомления\n" +
		"• /settings — выбрать, какие уведомления получать\n" +
		"• /language — сменить язык\n" +
		"• /unlink — отвязать аккаунт\n" +
		"• /help — эта справка\n\n" +
		"Как привязать аккаунт: открой ForgeCRM → Настройки → Telegram, сгенерируй код и отправь /link <код>.\n" +
		"Как получать уведомления: привяжи аккаунт и оставь уведомления включёнными в ForgeCRM.\n" +
		"Если команда не работает, отправь /start, чтобы начать заново.",
	KeyMenuTitle:          "Главное меню — выбери действие:",
	KeyUnknownCommand:     "Не знаю такую команду. Отправь /help, чтобы увидеть, что я умею.",
	KeyNotLinked:          "Этот Telegram-аккаунт пока не привязан к ForgeCRM. Открой ForgeCRM → Настройки → Telegram, сгенерируй код и отправь /link <код>.",
	KeyLinkUsage:          "Отправь /link <код>, где код берётся в ForgeCRM → Настройки → Telegram.",
	KeyLinkSuccess:        "Аккаунт привязан к ForgeCRM. Отправь /tasks, чтобы увидеть активные задачи.",
	KeyLinkFailed:         "Не удалось привязать аккаунт. Проверь, что код актуален, или сначала отвяжи текущий аккаунт.",
	KeyUnlinked:           "Этот Telegram-аккаунт больше не привязан к ForgeCRM.",
	KeyNoOpenTasks:        "У тебя нет активных задач.",
	KeyOpenTasksHeader:    "Твои активные задачи:",
	KeyTodayTasksHeader:   "Задачи со сроком сегодня (UTC):",
	KeyOverdueHeader:      "Твои просроченные задачи:",
	KeyNoOverdueTasks:     "У тебя нет просроченных задач.",
	KeyTaskDetail:         "%s\nСтатус: %s\nСрок: %s",
	KeyTaskCompleted:      "Задача выполнена.",
	KeyTaskDueUpdated:     "Срок задачи обновлён.",
	KeyTaskNotFound:       "Не нашёл такую задачу, либо она не твоя.",
	KeyTaskConflict:       "Эту задачу нельзя изменить в её текущем состоянии.",
	KeyInvalidTaskID:      "Это не похоже на идентификатор задачи.",
	KeyInvalidTimestamp:   "Это не похоже на дату. Используй RFC3339 с часовым поясом, например 2026-11-04T12:00:00Z.",
	KeyNoPermission:       "У тебя нет прав на это действие.",
	KeyProfile:            "Аккаунт: %s\nОрганизация: %s\nЯзык: %s",
	KeyNotificationsHdr:   "Твои последние уведомления:",
	KeyNoNotifications:    "У тебя нет новых уведомлений.",
	KeyServiceUnavailable: "Сейчас уведомления не доставляются — webhook бота отключён. Обратись к администратору.",
	KeyGenericError:       "Что-то пошло не так на моей стороне. Попробуй ещё раз через минуту.",
	BtnTasks:              "📋 Мои задачи",
	BtnOverdue:            "⏰ Просроченные",
	BtnProfile:            "👤 Профиль",
	BtnLanguage:           "🌐 Язык",
	BtnHelp:               "❓ Помощь",
	BtnMenu:               "⬅️ Меню",
	KeyUsageTasks:         "Использование: %s",
	KeyUsageTask:          "Использование: /task <id>",
	KeyUsageDone:          "Использование: /done <id>",
	KeyUsageDue:           "Использование: /due <id> <время RFC3339>",
	KeyDueNotSet:          "не указан",
	KeyDuePrefix:          "срок %s",
	KeyOpenTaskWithID:     "Открой задачу командой /task и её ID.",
	KeyTaskLine:           "%s — %s",
	KeyNotifSettings:      "Настройки уведомлений — нажмите на тип, чтобы включить или выключить:",
	KeyNotifOn:            "Вкл",
	KeyNotifOff:           "Выкл",
	KeyNotifAssignment:    "Назначение",
	KeyNotifDueChanged:    "Срок изменён",
	KeyNotifOverdue:       "Просрочка",
	KeyNotifCompleted:     "Завершение",
	KeyUnlinkConfirm:      "Отвязать этот Telegram от ForgeCRM? Вы перестанете получать уведомления здесь.",
	KeyUnlinkDone:         "Отвязано. Привязать снова можно командой /link <код>.",
	BtnNotifications:      "🔔 Уведомления",
	BtnSettings:           "⚙️ Настройки",
	BtnUnlink:             "🔗 Отвязать",
	BtnUnlinkConfirm:      "✅ Отвязать",
	BtnUnlinkCancel:       "⬅️ Отмена",
	BtnNotifAssignment:    "Назначение",
	BtnNotifDueChanged:    "Срок изменён",
	BtnNotifOverdue:       "Просрочка",
	BtnNotifCompleted:     "Завершение",
}
