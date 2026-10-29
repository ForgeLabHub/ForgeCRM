// Package workflow implements the workflow automation engine.
//
// A workflow is a trigger plus an ordered list of steps. It runs when its trigger
// matches a committed platform event, or on a schedule, or when a user asks for it.
// Steps act on CRM records through the same crm.Service the HTTP API uses, so a
// workflow can never do anything its author could not do: every action re-checks
// permissions as that author, at execution time, not at authoring time.
//
// Design notes that matter:
//
//   - The engine is driven by the existing outbox. It does not introduce a second
//     event mechanism. The worker hands it committed events and it matches them
//     against event-triggered workflows.
//
//   - Delivery is at-least-once, so event-triggered runs are de-duplicated on
//     (workflow_id, event_id) before any step runs. A replayed event cannot cause a
//     second effect.
//
//   - A run that a step causes carries a depth, bounded by the workflow's max_depth.
//     A run at the limit is recorded as skipped rather than executed, which is what
//     stops two workflows that watch each other from looping forever.
//
//   - A delay pauses the run by writing a resume instant and returning; the
//     scheduler resumes it. A worker is never blocked waiting for a delay.
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefinitionSchemaVersion is the shape version of the JSON definition document.
const DefinitionSchemaVersion = 1

// Trigger types.
const (
	TriggerEvent    = "event"
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// Step types.
const (
	StepCondition    = "condition"
	StepBranch       = "branch"
	StepCreateTask   = "create_task"
	StepUpdateDeal   = "update_deal"
	StepCreateTicket = "create_ticket"
	StepNotify       = "notify"
	StepDelay        = "delay"
)

// Limits that bound a definition. They are checked at save time so a workflow that
// could not execute is rejected by the API rather than failing later in the worker.
const (
	maxStepsPerDefinition = 50
	maxBranchDepth        = 5
	maxDelayMinutes       = 60 * 24 * 30 // 30 days
)

// Errors returned while parsing or validating a definition.
var (
	ErrInvalidDefinition = errors.New("workflow definition is invalid")
	ErrUnknownStepType   = errors.New("unknown step type")
)

// Definition is the parsed JSON document that describes a workflow.
type Definition struct {
	Steps []Step `json:"steps"`
}

// Step is one unit of work. Only the fields relevant to its Type are read; the
// rest stay zero. Args is intentionally open (a map) because each step type reads
// a different set of keys, and validation is done per type rather than by a single
// struct that would have to be a union.
type Step struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Args map[string]any `json:"args,omitempty"`

	// For StepCondition and StepBranch: the guard. For a condition step, a false
	// result stops the run and records it as skipped, which is the "only act if …"
	// form.
	Condition *Condition `json:"condition,omitempty"`

	// For StepBranch: the two nested step lists.
	Then []Step `json:"then,omitempty"`
	Else []Step `json:"else,omitempty"`
}

// Condition is a single comparison or a boolean combination of conditions.
type Condition struct {
	Field string `json:"field,omitempty"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`

	// Composite operators ("and", "or", "not") use these instead of Field/Value.
	All []Condition `json:"all,omitempty"`
	Any []Condition `json:"any,omitempty"`
	Not *Condition  `json:"not,omitempty"`
}

// ParseDefinition decodes and validates a definition document.
func ParseDefinition(raw json.RawMessage) (*Definition, error) {
	var def Definition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return &def, nil
}

// Validate checks the structural rules of a definition. It is the single place
// that decides whether a document is executable, so the API, the store and the
// engine all agree.
func (d *Definition) Validate() error {
	if len(d.Steps) == 0 {
		return fmt.Errorf("%w: a workflow needs at least one step", ErrInvalidDefinition)
	}
	if len(d.Steps) > maxStepsPerDefinition {
		return fmt.Errorf("%w: at most %d top-level steps", ErrInvalidDefinition, maxStepsPerDefinition)
	}
	seen := map[string]bool{}
	return validateSteps(d.Steps, 0, seen, false)
}

// validateSteps walks the step tree. inBranch is true once inside a branch, which
// is where a delay is refused: a delay pauses the whole run, so nesting it inside a
// conditional path would make the resume point ambiguous.
func validateSteps(steps []Step, depth int, seen map[string]bool, inBranch bool) error {
	if depth > maxBranchDepth {
		return fmt.Errorf("%w: branches nested more than %d deep", ErrInvalidDefinition, maxBranchDepth)
	}
	for i := range steps {
		s := &steps[i]
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("%w: step %d has no id", ErrInvalidDefinition, i)
		}
		if seen[s.ID] {
			return fmt.Errorf("%w: duplicate step id %q", ErrInvalidDefinition, s.ID)
		}
		seen[s.ID] = true

		switch s.Type {
		case StepCondition:
			if s.Condition == nil {
				return fmt.Errorf("%w: step %q is a condition with no condition", ErrInvalidDefinition, s.ID)
			}
			if err := s.Condition.validate(); err != nil {
				return fmt.Errorf("step %q: %w", s.ID, err)
			}
		case StepBranch:
			if s.Condition == nil {
				return fmt.Errorf("%w: step %q is a branch with no condition", ErrInvalidDefinition, s.ID)
			}
			if err := s.Condition.validate(); err != nil {
				return fmt.Errorf("step %q: %w", s.ID, err)
			}
			if len(s.Then) == 0 && len(s.Else) == 0 {
				return fmt.Errorf("%w: step %q has neither a then nor an else path", ErrInvalidDefinition, s.ID)
			}
			if err := validateSteps(s.Then, depth+1, seen, true); err != nil {
				return err
			}
			if err := validateSteps(s.Else, depth+1, seen, true); err != nil {
				return err
			}
		case StepCreateTask, StepCreateTicket, StepNotify, StepUpdateDeal:
			if len(s.Args) == 0 {
				return fmt.Errorf("%w: step %q has no args", ErrInvalidDefinition, s.ID)
			}
		case StepDelay:
			if inBranch {
				return fmt.Errorf("%w: step %q is a delay inside a branch; a delay must be top-level", ErrInvalidDefinition, s.ID)
			}
			if _, err := delayFromArgs(s.Args); err != nil {
				return fmt.Errorf("step %q: %w", s.ID, err)
			}
		case "":
			return fmt.Errorf("%w: step %q has no type", ErrInvalidDefinition, s.ID)
		default:
			return fmt.Errorf("%w: %q", ErrUnknownStepType, s.Type)
		}
	}
	return nil
}

func (c *Condition) validate() error {
	switch c.Op {
	case "and":
		if len(c.All) == 0 {
			return fmt.Errorf("%w: \"and\" needs at least one condition", ErrInvalidDefinition)
		}
		for i := range c.All {
			if err := c.All[i].validate(); err != nil {
				return err
			}
		}
	case "or":
		if len(c.Any) == 0 {
			return fmt.Errorf("%w: \"or\" needs at least one condition", ErrInvalidDefinition)
		}
		for i := range c.Any {
			if err := c.Any[i].validate(); err != nil {
				return err
			}
		}
	case "not":
		if c.Not == nil {
			return fmt.Errorf("%w: \"not\" needs a condition", ErrInvalidDefinition)
		}
		return c.Not.validate()
	case "eq", "ne", "gt", "gte", "lt", "lte", "contains", "starts_with", "ends_with", "in", "exists":
		if strings.TrimSpace(c.Field) == "" {
			return fmt.Errorf("%w: %q needs a field", ErrInvalidDefinition, c.Op)
		}
		if c.Op == "exists" {
			return nil
		}
		if c.Value == nil {
			return fmt.Errorf("%w: %q needs a value", ErrInvalidDefinition, c.Op)
		}
	default:
		return fmt.Errorf("%w: unknown operator %q", ErrInvalidDefinition, c.Op)
	}
	return nil
}

// delayFromArgs reads a delay duration from a delay step's args. Exactly one of
// "minutes" or "hours" is expected; both are bounded so a definition cannot pause a
// run for a year by accident.
func delayFromArgs(args map[string]any) (time.Duration, error) {
	get := func(key string) (float64, bool) {
		v, ok := args[key]
		if !ok {
			return 0, false
		}
		switch n := v.(type) {
		case float64:
			return n, true
		case int:
			return float64(n), true
		case json.Number:
			f, err := n.Float64()
			return f, err == nil
		default:
			return 0, false
		}
	}
	if m, ok := get("minutes"); ok {
		if m <= 0 || m > maxDelayMinutes {
			return 0, fmt.Errorf("%w: delay minutes must be between 1 and %d", ErrInvalidDefinition, maxDelayMinutes)
		}
		return time.Duration(m * float64(time.Minute)), nil
	}
	if h, ok := get("hours"); ok {
		if h <= 0 || h*60 > maxDelayMinutes {
			return 0, fmt.Errorf("%w: delay hours must be between 1 and %d", ErrInvalidDefinition, maxDelayMinutes/60)
		}
		return time.Duration(h * float64(time.Hour)), nil
	}
	return 0, fmt.Errorf("%w: a delay step needs \"minutes\" or \"hours\"", ErrInvalidDefinition)
}
