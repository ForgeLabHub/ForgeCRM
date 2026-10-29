package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The portable document format for a workflow.
//
// A workflow is exported as a self-describing JSON document that can be committed
// to a repository, reviewed in a pull request, and imported into another
// organization. It is deliberately the same definition shape the API stores — the
// `definition` field is the identical document — so an export is not a second
// format that has to be kept in step with the first. What the wrapper adds is the
// trigger and the name, which the definition alone does not carry, plus a format
// version so a document written by a future release is refused with a clear
// message rather than half-read.
const (
	// ExportFormat identifies the document so an unrelated JSON file is rejected by
	// name rather than by a confusing missing-field error.
	ExportFormat = "forgecrm.workflow"
	// ExportFormatVersion versions the wrapper. The definition has its own version
	// (DefinitionSchemaVersion); this one covers the fields around it.
	ExportFormatVersion = 1
)

// ExportDocument is the portable form of a workflow.
type ExportDocument struct {
	Format           string          `json:"format"`
	FormatVersion    int             `json:"format_version"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	TriggerType      string          `json:"trigger_type"`
	EventType        string          `json:"event_type,omitempty"`
	ScheduleCron     string          `json:"schedule_cron,omitempty"`
	ScheduleTimezone string          `json:"schedule_timezone,omitempty"`
	MaxDepth         int             `json:"max_depth,omitempty"`
	Definition       json.RawMessage `json:"definition"`
	// Template records which catalog template this workflow came from, when it came
	// from one. It is informational: importing does not require the template to still
	// exist, and a template that has moved on does not block the import.
	Template *TemplateRef `json:"template,omitempty"`
}

// TemplateRef is the provenance of a workflow that started from a template.
type TemplateRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// ExportWorkflow renders a stored workflow as a portable document.
func ExportWorkflow(w *Workflow, ref *TemplateRef) (*ExportDocument, error) {
	if w == nil {
		return nil, fmt.Errorf("no workflow to export")
	}
	doc := &ExportDocument{
		Format:        ExportFormat,
		FormatVersion: ExportFormatVersion,
		Name:          w.Name,
		Description:   w.Description,
		TriggerType:   w.TriggerType,
		MaxDepth:      w.MaxDepth,
		Definition:    w.Definition,
		Template:      ref,
	}
	if w.EventType != nil {
		doc.EventType = *w.EventType
	}
	if w.ScheduleCron != nil {
		doc.ScheduleCron = *w.ScheduleCron
	}
	if w.ScheduleTimezone != nil {
		doc.ScheduleTimezone = *w.ScheduleTimezone
	}
	return doc, nil
}

// ImportWorkflow parses a portable document into a create input.
//
// The checks are ordered so the first failure a user sees is the most specific
// one: the format name, then the format version, then the definition and the
// trigger. A document whose event the platform does not emit is refused with the
// event named, which is the "понятная ошибка при несовместимом шаблоне" case — an
// import of a document written against a different event vocabulary fails loudly
// rather than creating a workflow that is enabled and never fires.
func ImportWorkflow(raw json.RawMessage) (CreateInput, error) {
	var doc ExportDocument
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return CreateInput{}, fmt.Errorf("the document is not a valid workflow export: %w", err)
	}
	if doc.Format != ExportFormat {
		return CreateInput{}, fmt.Errorf("the document is not a %s export (found format %q)", ExportFormat, doc.Format)
	}
	if doc.FormatVersion > ExportFormatVersion {
		return CreateInput{}, fmt.Errorf("the document uses format version %d, but this server understands at most %d",
			doc.FormatVersion, ExportFormatVersion)
	}
	if strings.TrimSpace(doc.Name) == "" {
		return CreateInput{}, fmt.Errorf("the document has no name")
	}
	if len(doc.Definition) == 0 {
		return CreateInput{}, fmt.Errorf("the document has no definition")
	}
	if _, err := ParseDefinition(doc.Definition); err != nil {
		return CreateInput{}, err
	}

	in := CreateInput{
		Name:        doc.Name,
		Description: doc.Description,
		Definition:  doc.Definition,
		TriggerType: doc.TriggerType,
		MaxDepth:    doc.MaxDepth,
	}
	switch doc.TriggerType {
	case TriggerEvent:
		if doc.EventType == "" {
			return CreateInput{}, fmt.Errorf("an event trigger needs an event type")
		}
		// Only reject a known-bad event when the vocabulary has been loaded. In a unit
		// test that has not registered it, an import must still be parseable.
		if len(knownEvents) > 0 && !knownEvents[doc.EventType] {
			return CreateInput{}, fmt.Errorf("the document triggers on %q, which this platform does not emit", doc.EventType)
		}
		ev := doc.EventType
		in.EventType = &ev
	case TriggerSchedule:
		if doc.ScheduleCron == "" {
			return CreateInput{}, fmt.Errorf("a schedule trigger needs a cron expression")
		}
		if _, err := ParseCron(doc.ScheduleCron); err != nil {
			return CreateInput{}, fmt.Errorf("the schedule cron is invalid: %w", err)
		}
		if _, err := scheduleLocation(strPtrOrNil(doc.ScheduleTimezone)); err != nil {
			return CreateInput{}, err
		}
		cron := doc.ScheduleCron
		in.ScheduleCron = &cron
		if doc.ScheduleTimezone != "" {
			tz := doc.ScheduleTimezone
			in.ScheduleTimezone = &tz
		}
	case TriggerManual:
	default:
		return CreateInput{}, fmt.Errorf("the document has trigger type %q, which must be event, schedule or manual", doc.TriggerType)
	}
	return in, nil
}

// InputFromTemplate turns a catalog template into a create input, with the caller's
// name override. It validates the template again, so a template that was corrupted
// between the catalog check and the call cannot be created.
func InputFromTemplate(t *Template, name string) (CreateInput, error) {
	if t == nil {
		return CreateInput{}, fmt.Errorf("no template was named")
	}
	if strings.TrimSpace(name) == "" {
		name = t.Name
	}
	if _, err := ParseDefinition(t.Definition); err != nil {
		return CreateInput{}, err
	}
	in := CreateInput{
		Name:        name,
		Definition:  t.Definition,
		TriggerType: t.TriggerType,
		MaxDepth:    t.MaxDepth,
	}
	switch t.TriggerType {
	case TriggerEvent:
		ev := t.EventType
		in.EventType = &ev
	case TriggerSchedule:
		cron := t.ScheduleCron
		in.ScheduleCron = &cron
		if t.ScheduleTimezone != "" {
			tz := t.ScheduleTimezone
			in.ScheduleTimezone = &tz
		}
	}
	return in, nil
}
