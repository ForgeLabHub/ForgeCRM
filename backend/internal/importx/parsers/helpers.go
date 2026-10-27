package parsers

import (
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// charsetReader passes the document through unchanged.
//
// checkEncoding already rejects anything that is not valid UTF-8, so the only
// declarations that reach here are correct UTF-8 declarations (or harmless
// mis-declarations). Accepting them keeps a valid file importable without
// pulling in a transcoding dependency.
func charsetReader(charsetName string, input io.Reader) (io.Reader, error) {
	return input, nil
}

func sortStrings(s []string) { sort.Strings(s) }

func isInteger(s string) bool {
	_, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
	return err == nil
}

func isNumber(s string) bool {
	cleaned := strings.ReplaceAll(s, ",", "")
	if strings.HasPrefix(cleaned, "$") || strings.HasPrefix(cleaned, "€") ||
		strings.HasPrefix(cleaned, "£") {
		cleaned = cleaned[1:]
	}
	cleaned = strings.ReplaceAll(cleaned, "_", "")
	_, err := strconv.ParseFloat(cleaned, 64)
	return err == nil
}

func isBoolean(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "y", "n", "1", "0":
		return true
	}
	return false
}

// dateLayouts are the formats accepted when inferring a date column.
var dateLayouts = []string{
	"2006-01-02",
	"2006/01/02",
	"02.01.2006",
	"01/02/2006",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"02 Jan 2006",
	"Jan 2, 2006",
}

func isDate(s string) bool {
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// ParseDate attempts to interpret a value as a date or timestamp.
func ParseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ParseBool interprets a boolean-ish value.
func ParseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "y", "1", "on":
		return true, true
	case "false", "no", "n", "0", "off":
		return false, true
	}
	return false, false
}

// ParseNumber interprets a numeric value, tolerating thousands separators and a
// leading currency symbol.
func ParseNumber(s string) (float64, bool) {
	cleaned := strings.TrimSpace(s)
	if cleaned == "" {
		return 0, false
	}
	cleaned = strings.TrimLeft(cleaned, "$€£")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, "_", "")
	cleaned = strings.TrimSpace(cleaned)
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
