package workflow

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a parsed 5-field cron expression: minute hour day-of-month month
// day-of-week.
//
// This is a deliberately small implementation rather than a dependency. It supports
// the forms an operator actually writes — `*`, `5`, `1-5`, `1,3,5`, `*/15`, `1-30/5`
// — and nothing else. It rejects the field counts and macros it does not implement
// instead of silently misreading them, so a workflow never runs at an unintended
// time because a `@daily` was quietly treated as something else.
type cronSpec struct {
	minute  fieldSet
	hour    fieldSet
	dom     fieldSet
	month   fieldSet
	dow     fieldSet
	domStar bool
	dowStar bool
	raw     string
}

type fieldSet struct {
	bits [64]bool
}

func (f fieldSet) has(v int) bool {
	if v < 0 || v >= 64 {
		return false
	}
	return f.bits[v]
}

type fieldBound struct {
	name     string
	min, max int
}

var (
	minuteBound = fieldBound{"minute", 0, 59}
	hourBound   = fieldBound{"hour", 0, 23}
	domBound    = fieldBound{"day-of-month", 1, 31}
	monthBound  = fieldBound{"month", 1, 12}
	dowBound    = fieldBound{"day-of-week", 0, 6}
)

// ParseCron parses a 5-field cron expression.
func ParseCron(expr string) (*cronSpec, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: cron expression %q needs exactly 5 fields", ErrInvalidDefinition, expr)
	}
	spec := &cronSpec{raw: expr}
	var err error
	if spec.minute, err = parseField(fields[0], minuteBound); err != nil {
		return nil, err
	}
	if spec.hour, err = parseField(fields[1], hourBound); err != nil {
		return nil, err
	}
	if spec.dom, err = parseField(fields[2], domBound); err != nil {
		return nil, err
	}
	if spec.month, err = parseField(fields[3], monthBound); err != nil {
		return nil, err
	}
	if spec.dow, err = parseField(fields[4], dowBound); err != nil {
		return nil, err
	}
	spec.domStar = fields[2] == "*"
	spec.dowStar = fields[4] == "*"
	return spec, nil
}

func parseField(raw string, b fieldBound) (fieldSet, error) {
	var out fieldSet
	for _, part := range strings.Split(raw, ",") {
		if part == "" {
			return out, fmt.Errorf("%w: empty %s field", ErrInvalidDefinition, b.name)
		}
		step := 1
		if slash := strings.Index(part, "/"); slash >= 0 {
			stepStr := part[slash+1:]
			part = part[:slash]
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return out, fmt.Errorf("%w: bad step %q in %s field", ErrInvalidDefinition, stepStr, b.name)
			}
			step = n
		}
		lo, hi := b.min, b.max
		switch {
		case part == "*":
			// full range
		case strings.Contains(part, "-"):
			bounds := strings.SplitN(part, "-", 2)
			var err error
			if lo, err = strconv.Atoi(bounds[0]); err != nil {
				return out, fmt.Errorf("%w: bad range %q in %s field", ErrInvalidDefinition, part, b.name)
			}
			if hi, err = strconv.Atoi(bounds[1]); err != nil {
				return out, fmt.Errorf("%w: bad range %q in %s field", ErrInvalidDefinition, part, b.name)
			}
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return out, fmt.Errorf("%w: bad value %q in %s field", ErrInvalidDefinition, part, b.name)
			}
			lo, hi = n, n
		}
		if lo < b.min || hi > b.max || lo > hi {
			return out, fmt.Errorf("%w: %s value out of range %d-%d in %q", ErrInvalidDefinition, b.name, b.min, b.max, raw)
		}
		for v := lo; v <= hi; v += step {
			out.bits[v] = true
		}
	}
	return out, nil
}

// Next returns the first instant strictly after `after` that matches the
// expression, in the given location. It returns ok=false when no match is found
// within a five-year horizon, which for a valid 5-field expression cannot happen.
func (c *cronSpec) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	t := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !c.month.has(int(t.Month())) {
			// Jump to the first minute of the next month.
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
			continue
		}
		if !c.hour.has(t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc).Add(time.Hour)
			continue
		}
		if !c.minute.has(t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

// dayMatches applies the standard day rule: when both day-of-month and
// day-of-week are restricted, a day matches if either matches (the Vixie cron
// behaviour operators expect). When one is `*`, only the other constrains the day.
func (c *cronSpec) dayMatches(t time.Time) bool {
	dom := c.dom.has(t.Day())
	dow := c.dow.has(int(t.Weekday()))
	switch {
	case c.domStar && c.dowStar:
		return true
	case c.domStar:
		return dow
	case c.dowStar:
		return dom
	default:
		return dom || dow
	}
}

// String returns the original expression.
func (c *cronSpec) String() string { return c.raw }
