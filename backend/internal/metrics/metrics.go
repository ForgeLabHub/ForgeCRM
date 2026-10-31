// Package metrics renders samples in the Prometheus text exposition format
// (version 0.0.4).
//
// It is deliberately small and dependency-free. The platform exports a handful of
// scalar counters and gauges, and adopting a client library for that would add a
// dependency, a process-global registry, and a second place the values are
// defined. The values themselves are read from the database, so they are already
// correct when several replicas run; this package only formats them.
//
// Formatting is deterministic: metrics are ordered by name and their samples by
// label set, so the same state always renders to the same bytes. A scrape body
// that reorders between requests is impossible to diff and impossible to test.
package metrics

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Kind is the Prometheus metric type.
type Kind string

const (
	// Counter is a value that only rises. It is the type to compute rate() over.
	Counter Kind = "counter"
	// Gauge is a value that can rise and fall.
	Gauge Kind = "gauge"
)

// Sample is one measurement of one metric.
//
// Several samples may share a Name with different Labels; they are rendered under
// a single HELP and TYPE, which is what makes them one metric rather than several.
type Sample struct {
	Name   string
	Help   string
	Kind   Kind
	Labels map[string]string
	Value  float64
}

// Render writes the samples in the text exposition format.
//
// It returns an error rather than writing a malformed body: a metric name that a
// Prometheus scraper would reject, a metric declared with two different help
// texts, or an unknown type. A body the scraper silently drops is worse than a
// failed request, because the failure is invisible.
func Render(w io.Writer, samples []Sample) error {
	byName := map[string][]Sample{}
	help := map[string]string{}
	kind := map[string]Kind{}
	var order []string

	for _, s := range samples {
		if !validName(s.Name) {
			return fmt.Errorf("metric name %q is not a valid Prometheus name", s.Name)
		}
		switch s.Kind {
		case Counter, Gauge:
		default:
			return fmt.Errorf("metric %s has unsupported type %q", s.Name, s.Kind)
		}
		for label := range s.Labels {
			if !validLabelName(label) {
				return fmt.Errorf("metric %s has an invalid label name %q", s.Name, label)
			}
		}

		if prev, seen := help[s.Name]; seen {
			if prev != s.Help {
				return fmt.Errorf("metric %s is declared with two different help texts", s.Name)
			}
			if kind[s.Name] != s.Kind {
				return fmt.Errorf("metric %s is declared as both %s and %s", s.Name, kind[s.Name], s.Kind)
			}
		} else {
			help[s.Name] = s.Help
			kind[s.Name] = s.Kind
			order = append(order, s.Name)
		}
		byName[s.Name] = append(byName[s.Name], s)
	}

	sort.Strings(order)

	var b bytes.Buffer
	for _, name := range order {
		fmt.Fprintf(&b, "# HELP %s %s\n", name, escapeHelp(help[name]))
		fmt.Fprintf(&b, "# TYPE %s %s\n", name, kind[name])

		rows := byName[name]
		sort.Slice(rows, func(i, j int) bool {
			return canonicalLabels(rows[i].Labels) < canonicalLabels(rows[j].Labels)
		})
		for _, s := range rows {
			b.WriteString(name)
			b.WriteString(formatLabels(s.Labels))
			b.WriteByte(' ')
			b.WriteString(formatValue(s.Value))
			b.WriteByte('\n')
		}
	}

	_, err := w.Write(b.Bytes())
	return err
}

// validName reports whether s matches [a-zA-Z_:][a-zA-Z0-9_:]*.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == ':' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// validLabelName reports whether s matches [a-zA-Z_][a-zA-Z0-9_]*.
func validLabelName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// escapeHelp escapes the characters the exposition format reserves in help text.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// escapeLabel escapes the characters reserved in a label value.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// formatLabels renders a label set, sorted by name so the output is stable.
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(name)
		b.WriteString(`="`)
		b.WriteString(escapeLabel(labels[name]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// canonicalLabels is the sort key for a label set.
func canonicalLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(labels[name])
	}
	return b.String()
}

// formatValue renders a float the way the exposition format expects, including
// the named forms for the values strconv would otherwise spell differently.
func formatValue(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	default:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
}
