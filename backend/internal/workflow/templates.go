package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A template is a ready-made workflow a user can start from instead of writing a
// definition by hand. It is a workflow definition plus the metadata the picker
// needs, and it is the same JSON contract the API already accepts — a template
// cannot describe a shape the engine cannot run, because it is validated by the
// same ParseDefinition the create path uses.
//
// The catalog is deliberately a fixed, in-code list rather than a database table.
// A template that could be edited would be a second definition format living
// beside the first, which is exactly what the project avoids. The events and step
// types a template names are checked against the real vocabularies below, so a
// template with a misspelled event or an unknown step cannot ship.

// Template is one entry in the catalog.
type Template struct {
	// ID is stable and is what an import/export round-trip carries. It is not the
	// name: a name can be translated or reworded without breaking a saved reference.
	ID string `json:"id"`
	// Version increments when the template's definition changes in a way a user
	// should notice. It is stored on export so a re-import can tell whether the
	// document is a template that has since moved on.
	Version  int    `json:"version"`
	Category string `json:"category"`
	Name     string `json:"name"`
	// Description is shown in the picker.
	Description string `json:"description"`
	// TriggerType and its companions mirror the workflow's own trigger fields, so a
	// user can create from a template without editing the trigger if they accept the
	// default.
	TriggerType      string `json:"trigger_type"`
	EventType        string `json:"event_type,omitempty"`
	ScheduleCron     string `json:"schedule_cron,omitempty"`
	ScheduleTimezone string `json:"schedule_timezone,omitempty"`
	MaxDepth         int    `json:"max_depth,omitempty"`
	// Definition is the step document. It is returned as-is so a client can show it,
	// and it is what a "create from template" call sends after the user edits it.
	Definition json.RawMessage `json:"definition"`
}

// knownStepTypes is the closed set of step types the engine can execute. A
// template naming anything else fails the catalog self-check.
var knownStepTypes = map[string]bool{
	StepCondition:    true,
	StepBranch:       true,
	StepCreateTask:   true,
	StepUpdateDeal:   true,
	StepCreateTicket: true,
	StepNotify:       true,
	StepDelay:        true,
	StepFindDeals:    true,
	StepForeach:      true,
}

// knownEvents is the event vocabulary a template may trigger on. It is filled from
// the outbox constants so it cannot drift from what the dispatcher emits.
var knownEvents = map[string]bool{}

// RegisterKnownEvents records the platform's event vocabulary for template
// validation. It is called once at startup with outbox.EventTypes(), so the
// workflow package does not import the outbox package (which would be a cycle: the
// worker that drains the outbox is the same one that runs the engine).
//
// It replaces the set rather than adding to it: the vocabulary is the platform's
// event list, and a call with a shorter list means the list is shorter, not that
// the previous entries survive. A test that registers a deliberately narrow
// vocabulary therefore actually narrows it.
func RegisterKnownEvents(types []string) {
	next := make(map[string]bool, len(types))
	for _, t := range types {
		next[t] = true
	}
	knownEvents = next
}

// catalog is the built-in set. Every entry is a real CRM scenario that uses only
// events the platform emits and steps the engine runs.
var catalog = []Template{
	{
		ID:               "deal-overdue-notify-owner",
		Version:          1,
		Category:         "deals",
		Name:             "Notify the owner about an overdue deal",
		Description:      "Every weekday morning, find open deals whose expected close date has passed and notify the owner.",
		TriggerType:      TriggerSchedule,
		ScheduleCron:     "0 9 * * 1-5",
		ScheduleTimezone: "UTC",
		Definition: json.RawMessage(`{
  "steps": [
    {"id": "overdue", "type": "find_deals", "args": {"status": "open", "close_date_within_days": 0, "limit": 100}},
    {"id": "each", "type": "foreach", "do": [
      {"id": "notify_owner", "type": "notify", "args": {
        "user_id": "{{item.owner_user_id}}",
        "title": "Overdue deal: {{item.title}}",
        "body": "The expected close date ({{item.expected_close_date}}) has passed. Update the deal or move the stage."
      }}
    ]}
  ]
}`),
	},
	{
		ID:          "deal-stage-change-create-task",
		Version:     1,
		Category:    "deals",
		Name:        "Create a follow-up task when a deal changes stage",
		Description: "When a deal moves to a new stage, open a task to follow up so a stage change is never the last thing that happened.",
		TriggerType: TriggerEvent,
		EventType:   "crm.deal.stage_changed",
		Definition: json.RawMessage(`{
  "steps": [
    {"id": "follow_up", "type": "create_task", "args": {
      "title": "Follow up on the deal after its stage change",
      "description": "The deal moved stage. Review it and decide the next action.",
      "priority": "normal",
      "deal_id": "{{payload.deal_id}}",
      "due_in_hours": 24
    }}
  ]
}`),
	},
	{
		ID:               "deal-no-activity-reminder",
		Version:          1,
		Category:         "deals",
		Name:             "Remind the owner about a deal with no recent activity",
		Description:      "Every Monday, find open deals with no recorded activity in the last 14 days and notify the owner.",
		TriggerType:      TriggerSchedule,
		ScheduleCron:     "0 8 * * 1",
		ScheduleTimezone: "UTC",
		Definition: json.RawMessage(`{
  "steps": [
    {"id": "stale", "type": "find_deals", "args": {"status": "open", "stale_days": 14, "limit": 100}},
    {"id": "each", "type": "foreach", "do": [
      {"id": "remind_owner", "type": "notify", "args": {
        "user_id": "{{item.owner_user_id}}",
        "title": "No recent activity: {{item.title}}",
        "body": "This deal has had no activity for at least 14 days. Log a call or a note, or move it forward."
      }}
    ]}
  ]
}`),
	},
	{
		ID:          "ticket-created-acknowledge",
		Version:     1,
		Category:    "support",
		Name:        "Acknowledge a new support ticket",
		Description: "When a ticket is created, open a first-response task so a new ticket is picked up rather than sitting unassigned.",
		TriggerType: TriggerEvent,
		EventType:   "support.ticket.created",
		Definition: json.RawMessage(`{
  "steps": [
    {"id": "first_response", "type": "create_task", "args": {
      "title": "Respond to the new support ticket",
      "description": "A new ticket was created. Send a first response and triage it.",
      "priority": "high",
      "due_in_hours": 4
    }}
  ]
}`),
	},
	{
		ID:          "deal-won-onboarding-task",
		Version:     1,
		Category:    "deals",
		Name:        "Start onboarding when a deal is won",
		Description: "When a deal is won, create an onboarding task so the handover happens while the win is fresh.",
		TriggerType: TriggerEvent,
		EventType:   "crm.deal.won",
		Definition: json.RawMessage(`{
  "steps": [
    {"id": "onboarding", "type": "create_task", "args": {
      "title": "Kick off onboarding for the won deal",
      "description": "The deal was won. Hand it to delivery and confirm the next step with the customer.",
      "priority": "high",
      "deal_id": "{{payload.deal_id}}",
      "due_in_hours": 48
    }}
  ]
}`),
	},
	{
		ID:          "ticket-priority-triage",
		Version:     1,
		Category:    "support",
		Name:        "Triage a new ticket by priority",
		Description: "When a ticket is created, open a higher-priority first-response task when it is urgent and a normal one otherwise.",
		TriggerType: TriggerEvent,
		EventType:   "support.ticket.created",
		Definition: json.RawMessage(`{
  "steps": [
    {"id": "by_priority", "type": "branch",
     "condition": {"field": "payload.priority", "op": "in", "value": ["high", "urgent"]},
     "then": [
       {"id": "urgent_task", "type": "create_task", "args": {
         "title": "Respond urgently to the new ticket",
         "description": "A high or urgent ticket was created. Respond within the SLA.",
         "priority": "urgent",
         "due_in_hours": 1
       }}
     ],
     "else": [
       {"id": "normal_task", "type": "create_task", "args": {
         "title": "Respond to the new ticket",
         "description": "A new ticket was created. Send a first response and triage it.",
         "priority": "normal",
         "due_in_hours": 8
       }}
     ]}
  ]
}`),
	},
}

// Templates returns the catalog, sorted by category then name so the picker's
// order is stable across calls.
func Templates() []Template {
	out := make([]Template, len(catalog))
	copy(out, catalog)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// TemplateByID returns one template from the catalog.
func TemplateByID(id string) (*Template, bool) {
	for i := range catalog {
		if catalog[i].ID == id {
			t := catalog[i]
			return &t, true
		}
	}
	return nil, false
}

// ValidateCatalog checks every built-in template against the real vocabularies.
//
// It is called at startup and from a test, so a template that names an event the
// platform does not emit, or a step the engine does not run, is caught before it
// can be offered to a user. The failure mode this prevents is the one the webhook
// form once had: a subscription that is accepted and enabled and then never fires,
// which looks like a network fault and is invisible in the interface.
func ValidateCatalog() error {
	if len(knownEvents) == 0 {
		return fmt.Errorf("the event vocabulary is empty; RegisterKnownEvents was not called")
	}
	seen := map[string]bool{}
	for i := range catalog {
		t := &catalog[i]
		if t.ID == "" {
			return fmt.Errorf("template %d has no id", i)
		}
		if seen[t.ID] {
			return fmt.Errorf("duplicate template id %q", t.ID)
		}
		seen[t.ID] = true
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("template %q has no name", t.ID)
		}
		if t.Version < 1 {
			return fmt.Errorf("template %q has no version", t.ID)
		}
		switch t.TriggerType {
		case TriggerEvent:
			if t.EventType == "" {
				return fmt.Errorf("template %q is event-triggered but names no event", t.ID)
			}
			if !knownEvents[t.EventType] {
				return fmt.Errorf("template %q triggers on %q, which the platform does not emit", t.ID, t.EventType)
			}
		case TriggerSchedule:
			if t.ScheduleCron == "" {
				return fmt.Errorf("template %q is scheduled but has no cron", t.ID)
			}
			if _, err := ParseCron(t.ScheduleCron); err != nil {
				return fmt.Errorf("template %q has an invalid cron: %w", t.ID, err)
			}
			if _, err := scheduleLocation(strPtrOrNil(t.ScheduleTimezone)); err != nil {
				return fmt.Errorf("template %q has an invalid timezone: %w", t.ID, err)
			}
		case TriggerManual:
		default:
			return fmt.Errorf("template %q has trigger type %q", t.ID, t.TriggerType)
		}

		def, err := ParseDefinition(t.Definition)
		if err != nil {
			return fmt.Errorf("template %q has an invalid definition: %w", t.ID, err)
		}
		if err := checkStepTypes(def.Steps); err != nil {
			return fmt.Errorf("template %q: %w", t.ID, err)
		}
	}
	return nil
}

// checkStepTypes walks a definition and rejects any step type the engine does not
// know. ParseDefinition already rejects an unknown type, so this is a second check
// against the catalog's own list; it exists so adding a step type to the engine
// and forgetting the catalog (or the reverse) is a test failure rather than a
// runtime surprise.
func checkStepTypes(steps []Step) error {
	for i := range steps {
		s := &steps[i]
		if !knownStepTypes[s.Type] {
			return fmt.Errorf("step %q has type %q, which the catalog does not know", s.ID, s.Type)
		}
		if err := checkStepTypes(s.Then); err != nil {
			return err
		}
		if err := checkStepTypes(s.Else); err != nil {
			return err
		}
		if err := checkStepTypes(s.Do); err != nil {
			return err
		}
	}
	return nil
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
