package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// The template catalog is checked against the real vocabularies, not against
// itself. A template that names an event the platform does not emit, or a step the
// engine does not run, would be accepted by the API and then never fire or fail at
// run time — the exact silent failure the event-vocabulary note warns about.

func TestCatalogIsValid(t *testing.T) {
	RegisterKnownEvents([]string{
		"crm.deal.created", "crm.deal.updated", "crm.deal.stage_changed", "crm.deal.won", "crm.deal.lost",
		"crm.task.created", "crm.task.completed", "support.ticket.created", "support.ticket.updated",
	})
	if err := ValidateCatalog(); err != nil {
		t.Fatalf("the built-in catalog is invalid: %v", err)
	}
}

func TestCatalogRejectsAnUnknownEvent(t *testing.T) {
	// Register a vocabulary that does not include the event a template names. The
	// catalog must fail rather than ship a workflow that can never be triggered.
	RegisterKnownEvents([]string{"crm.account.created"})
	defer RegisterKnownEvents([]string{"crm.deal.stage_changed", "crm.deal.won", "support.ticket.created"})
	if err := ValidateCatalog(); err == nil {
		t.Fatal("the catalog accepted a template whose event the platform does not emit")
	}
}

func TestEveryTemplateRoundTripsThroughExport(t *testing.T) {
	RegisterKnownEvents([]string{
		"crm.deal.stage_changed", "crm.deal.won", "support.ticket.created",
	})
	for _, tmpl := range Templates() {
		t.Run(tmpl.ID, func(t *testing.T) {
			in, err := InputFromTemplate(&tmpl, "")
			if err != nil {
				t.Fatalf("InputFromTemplate: %v", err)
			}
			// Build the workflow a create would store, then export and re-import it.
			wf := &Workflow{
				Name:             in.Name,
				Description:      in.Description,
				Definition:       in.Definition,
				TriggerType:      in.TriggerType,
				EventType:        in.EventType,
				ScheduleCron:     in.ScheduleCron,
				ScheduleTimezone: in.ScheduleTimezone,
				MaxDepth:         in.MaxDepth,
			}
			doc, err := ExportWorkflow(wf, &TemplateRef{ID: tmpl.ID, Version: tmpl.Version})
			if err != nil {
				t.Fatalf("ExportWorkflow: %v", err)
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("marshal export: %v", err)
			}
			reimported, err := ImportWorkflow(raw)
			if err != nil {
				t.Fatalf("ImportWorkflow: %v", err)
			}
			if reimported.Name != in.Name {
				t.Errorf("name changed: %q -> %q", in.Name, reimported.Name)
			}
			if reimported.TriggerType != in.TriggerType {
				t.Errorf("trigger type changed: %q -> %q", in.TriggerType, reimported.TriggerType)
			}
			if string(reimported.Definition) != string(in.Definition) {
				// Compare semantically: an export writes the definition verbatim and a
				// re-import preserves it, but a byte compare is too strict for a document
				// that may be re-indented in between.
				var a, b any
				if err := json.Unmarshal(in.Definition, &a); err != nil {
					t.Fatalf("unmarshal original: %v", err)
				}
				if err := json.Unmarshal(reimported.Definition, &b); err != nil {
					t.Fatalf("unmarshal reimported: %v", err)
				}
				na, _ := json.Marshal(a)
				nb, _ := json.Marshal(b)
				if string(na) != string(nb) {
					t.Errorf("definition changed across a round trip:\n %s\n %s", na, nb)
				}
			}
		})
	}
}

func TestImportRejectsAnotherFormat(t *testing.T) {
	_, err := ImportWorkflow(json.RawMessage(`{"format":"something.else","format_version":1,"name":"x","definition":{"steps":[{"id":"a","type":"create_task","args":{"title":"t"}}]}}`))
	if err == nil {
		t.Fatal("an import of a foreign document was accepted")
	}
	if !strings.Contains(err.Error(), "not a") {
		t.Fatalf("the error did not name the format problem: %v", err)
	}
}

func TestImportRejectsAFutureFormatVersion(t *testing.T) {
	doc := `{"format":"forgecrm.workflow","format_version":99,"name":"x","trigger_type":"manual","definition":{"steps":[{"id":"a","type":"create_task","args":{"title":"t"}}]}}`
	_, err := ImportWorkflow(json.RawMessage(doc))
	if err == nil {
		t.Fatal("a document with a future format version was accepted")
	}
	if !strings.Contains(err.Error(), "format version") {
		t.Fatalf("the error did not name the version problem: %v", err)
	}
}

func TestImportRejectsAnUnknownEvent(t *testing.T) {
	RegisterKnownEvents([]string{"crm.deal.won"})
	doc := `{"format":"forgecrm.workflow","format_version":1,"name":"x","trigger_type":"event","event_type":"crm.nope.happened","definition":{"steps":[{"id":"a","type":"create_task","args":{"title":"t"}}]}}`
	_, err := ImportWorkflow(json.RawMessage(doc))
	if err == nil {
		t.Fatal("a document triggering on an event the platform does not emit was accepted")
	}
	if !strings.Contains(err.Error(), "does not emit") {
		t.Fatalf("the error did not name the event problem: %v", err)
	}
}

func TestImportRejectsAnInvalidDefinition(t *testing.T) {
	doc := `{"format":"forgecrm.workflow","format_version":1,"name":"x","trigger_type":"manual","definition":{"steps":[{"id":"a","type":"nonexistent_step","args":{"title":"t"}}]}}`
	if _, err := ImportWorkflow(json.RawMessage(doc)); err == nil {
		t.Fatal("a document with an unknown step type was accepted")
	}
}

// TestForeachAcceptsOnlyActionSteps pins the validation rule: a condition or a
// delay inside a foreach would make the resume point of a paused run ambiguous.
func TestForeachAcceptsOnlyActionSteps(t *testing.T) {
	doc := `{"steps":[{"id":"f","type":"foreach","do":[{"id":"c","type":"condition","condition":{"field":"x","op":"eq","value":1}}]}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err == nil {
		t.Fatal("a foreach containing a condition was accepted")
	}
	doc = `{"steps":[{"id":"f","type":"foreach","do":[{"id":"w","type":"delay","args":{"minutes":5}}]}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err == nil {
		t.Fatal("a foreach containing a delay was accepted")
	}
	doc = `{"steps":[{"id":"f","type":"foreach","do":[{"id":"t","type":"create_task","args":{"title":"x"}}]}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err != nil {
		t.Fatalf("a foreach containing only an action step was rejected: %v", err)
	}
}

func TestFindDealsRejectsAnUnknownEntity(t *testing.T) {
	doc := `{"steps":[{"id":"f","type":"find_deals","args":{"entity":"invoices"}}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err == nil {
		t.Fatal("a find_deals step for an unsupported entity was accepted")
	}
	doc = `{"steps":[{"id":"f","type":"find_deals","args":{"entity":"deals","status":"open"}}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err != nil {
		t.Fatalf("a valid find_deals step was rejected: %v", err)
	}
}

func TestFindDealsRejectsAnOversizedLimit(t *testing.T) {
	doc := `{"steps":[{"id":"f","type":"find_deals","args":{"limit":100000}}]}`
	if _, err := ParseDefinition(json.RawMessage(doc)); err == nil {
		t.Fatal("a find_deals step asking for more than the cap was accepted")
	}
}
