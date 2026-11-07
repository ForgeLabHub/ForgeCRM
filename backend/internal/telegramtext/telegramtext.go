// Package telegramtext holds the Telegram assistant's localized copy.
//
// It is deliberately separate from the API and transport layers so that adding a
// language is a matter of adding a map entry, not editing handlers. Handlers ask
// for a message by key and a locale; they never spell out user-facing text, so
// there is one place to look for what the bot says and one place to translate.
package telegramtext

import "strings"

// Locale is a supported language tag. Only the primary subtag matters: "ru-RU"
// and "ru" are the same language here.
type Locale string

const (
	// LocaleEnglish is the fallback for an unknown or unsupported tag.
	LocaleEnglish Locale = "en"
	// LocaleRussian is the second supported language.
	LocaleRussian Locale = "ru"
)

// Supported lists the locales the assistant can answer in, for validation and for
// rendering the language picker.
var Supported = []Locale{LocaleEnglish, LocaleRussian}

// Normalize maps an arbitrary language tag to a supported locale. Anything
// unknown becomes English, so a new client's tag can never produce a blank reply.
func Normalize(tag string) Locale {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		tag = tag[:i]
	}
	switch tag {
	case string(LocaleRussian):
		return LocaleRussian
	default:
		return LocaleEnglish
	}
}

// IsSupported reports whether a tag names a language the assistant speaks. It
// inspects the primary subtag directly rather than via Normalize, because
// Normalize maps anything unknown to English and would then report every tag as
// supported.
func IsSupported(tag string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(tag))
	if trimmed == "" {
		return false
	}
	if i := strings.IndexAny(trimmed, "-_"); i > 0 {
		trimmed = trimmed[:i]
	}
	switch Locale(trimmed) {
	case LocaleEnglish, LocaleRussian:
		return true
	default:
		return false
	}
}

// Key identifies a message. A missing translation falls back to English and then
// to the key itself, so a half-finished dictionary degrades to a visible token
// rather than an empty string.
type Key string

const (
	KeyWelcome            Key = "welcome"
	KeyChooseLanguage     Key = "choose_language"
	KeyLanguageSet        Key = "language_set"
	KeyHelp               Key = "help"
	KeyMenuTitle          Key = "menu_title"
	KeyUnknownCommand     Key = "unknown_command"
	KeyNotLinked          Key = "not_linked"
	KeyLinkUsage          Key = "link_usage"
	KeyLinkSuccess        Key = "link_success"
	KeyLinkFailed         Key = "link_failed"
	KeyUnlinked           Key = "unlinked"
	KeyNoOpenTasks        Key = "no_open_tasks"
	KeyOpenTasksHeader    Key = "open_tasks_header"
	KeyTodayTasksHeader   Key = "today_tasks_header"
	KeyOverdueHeader      Key = "overdue_header"
	KeyNoOverdueTasks     Key = "no_overdue_tasks"
	KeyTaskDetail         Key = "task_detail"
	KeyTaskCompleted      Key = "task_completed"
	KeyTaskDueUpdated     Key = "task_due_updated"
	KeyTaskNotFound       Key = "task_not_found"
	KeyTaskConflict       Key = "task_conflict"
	KeyInvalidTaskID      Key = "invalid_task_id"
	KeyInvalidTimestamp   Key = "invalid_timestamp"
	KeyNoPermission       Key = "no_permission"
	KeyProfile            Key = "profile"
	KeyNotificationsHdr   Key = "notifications_header"
	KeyNoNotifications    Key = "no_notifications"
	KeyServiceUnavailable Key = "service_unavailable"
	KeyGenericError       Key = "generic_error"
	// Menu button labels.
	BtnTasks    Key = "btn_tasks"
	BtnOverdue  Key = "btn_overdue"
	BtnProfile  Key = "btn_profile"
	BtnLanguage Key = "btn_language"
	BtnHelp     Key = "btn_help"
	BtnMenu     Key = "btn_menu"
	// Usage strings with the command name appended by the caller.
	KeyUsageTasks     Key = "usage_tasks"
	KeyUsageTask      Key = "usage_task"
	KeyUsageDone      Key = "usage_done"
	KeyUsageDue       Key = "usage_due"
	KeyDueNotSet      Key = "due_not_set"
	KeyDuePrefix      Key = "due_prefix"
	KeyOpenTaskWithID Key = "open_task_with_id"
	KeyTaskLine       Key = "task_line"
	// Notification settings and unlink.
	KeyNotifSettings   Key = "notif_settings"
	KeyNotifOn         Key = "notif_on"
	KeyNotifOff        Key = "notif_off"
	KeyNotifAssignment Key = "notif_assignment"
	KeyNotifDueChanged Key = "notif_due_changed"
	KeyNotifOverdue    Key = "notif_overdue"
	KeyNotifCompleted  Key = "notif_completed"
	KeyUnlinkConfirm   Key = "unlink_confirm"
	KeyUnlinkDone      Key = "unlink_done"
	BtnNotifications   Key = "btn_notifications"
	BtnSettings        Key = "btn_settings"
	BtnUnlink          Key = "btn_unlink"
	BtnUnlinkConfirm   Key = "btn_unlink_confirm"
	BtnUnlinkCancel    Key = "btn_unlink_cancel"
	BtnNotifAssignment Key = "btn_notif_assignment"
	BtnNotifDueChanged Key = "btn_notif_due_changed"
	BtnNotifOverdue    Key = "btn_notif_overdue"
	BtnNotifCompleted  Key = "btn_notif_completed"
	// CRM lookup commands. These make the bot useful for reading the CRM, not
	// only for managing the caller's own tasks.
	KeySearchUsage    Key = "search_usage"
	KeySearchHeader   Key = "search_header"
	KeySearchEmpty    Key = "search_empty"
	KeyDealsHeader    Key = "deals_header"
	KeyDealsEmpty     Key = "deals_empty"
	KeyDealLine       Key = "deal_line"
	KeyPipelineHeader Key = "pipeline_header"
	KeyPipelineEmpty  Key = "pipeline_empty"
	KeyPipelineLine   Key = "pipeline_line"
	KeyUsageSearch    Key = "usage_search"
	KeyUsageDeals     Key = "usage_deals"
	KeyUsagePipeline  Key = "usage_pipeline"
	// Menu button labels for the CRM commands.
	BtnSearch   Key = "btn_search"
	BtnDeals    Key = "btn_deals"
	BtnPipeline Key = "btn_pipeline"
)

// Catalog maps a key to its translation per locale.
var Catalog = map[Locale]map[Key]string{
	LocaleEnglish: english,
	LocaleRussian: russian,
}

// T returns the message for a key in a locale. It never returns an empty string:
// an unsupported locale falls back to English, a missing key to the key text.
func T(locale Locale, key Key) string {
	if msg, ok := Catalog[locale][key]; ok {
		return msg
	}
	if msg, ok := Catalog[LocaleEnglish][key]; ok {
		return msg
	}
	return string(key)
}
