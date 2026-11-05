package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAppearanceDiagnostic(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		profile    string
		defaultVal string
		effective  string
		applied    bool
	}{
		{"neutral fallback", nil, "", "minimal", "minimal", false},
		{"priority independent of argument order", []string{"--roles", "sales_representative, executive"}, "executive", "premium", "premium", false},
		{"platform owner", []string{"--roles", "executive", "--platform-owner"}, "platform_admin", "midnight", "midnight", false},
		{"explicit preference", []string{"--roles", "executive", "--preference", "glass"}, "executive", "premium", "glass", true},
		{"unknown role", []string{"--roles", "custom_role"}, "", "minimal", "minimal", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := runAppearance(tt.args, &out, &errOut); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			var result struct {
				DiagnosticOnly    bool   `json:"diagnostic_only"`
				Profile           string `json:"profile"`
				DefaultStyle      string `json:"default_style"`
				EffectiveStyle    string `json:"effective_style"`
				PreferenceApplied bool   `json:"preference_applied"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.DiagnosticOnly || result.Profile != tt.profile || result.DefaultStyle != tt.defaultVal || result.EffectiveStyle != tt.effective || result.PreferenceApplied != tt.applied {
				t.Fatalf("unexpected diagnostic result: %+v", result)
			}
		})
	}
}

func TestAppearanceDiagnosticErrorsAndHelp(t *testing.T) {
	for _, args := range [][]string{{"--preference", "not-a-style"}, {"--roles", "executive,,analyst"}, {"extra"}, {"--unknown"}} {
		var out, errOut bytes.Buffer
		if code := runAppearance(args, &out, &errOut); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
			t.Errorf("args %v: code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if code := runAppearance([]string{"--help"}, &out, &errOut); code != 0 || !strings.Contains(errOut.String(), "Offline diagnostic only") {
		t.Fatalf("help: code=%d stderr=%q", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := runAppearance(nil, failingWriter{}, &errOut); code != 1 || !strings.Contains(errOut.String(), "write result") {
		t.Fatalf("write failure: code=%d stderr=%q", code, errOut.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
