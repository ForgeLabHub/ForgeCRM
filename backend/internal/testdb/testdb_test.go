package testdb

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestSanitizeProducesAValidIdentifier(t *testing.T) {
	cases := map[string]string{
		"api":             "api",
		"forgecrm-worker": "forgecrm_worker",
		"internal/api":    "internal_api",
		"":                "pkg",
		"UPPER":           "upper",
		"weird!@#$%^&*()": "weird__________",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeTruncatesLongNames(t *testing.T) {
	long := strings.Repeat("a", 200)
	if got := sanitize(long); len(got) != 40 {
		t.Fatalf("sanitize produced %d characters, want 40", len(got))
	}
}

func TestPackageLabelIsUniqueAndPrefixed(t *testing.T) {
	label := packageLabel()
	if !strings.HasPrefix(label, "forgecrm_test_") {
		t.Fatalf("label %q lacks the forgecrm_test_ prefix", label)
	}
	// The PID suffix is what keeps two runs of the same package from sharing a
	// database, so it must be present.
	if !strings.HasSuffix(label, strconv.Itoa(os.Getpid())) {
		t.Fatalf("label %q does not end with the process id %d", label, os.Getpid())
	}
}

func TestDatabaseURLSwapsThePath(t *testing.T) {
	base, _ := url.Parse("postgres://u:p@h:5432/forgecrm_test?sslmode=disable")
	got := databaseURL(base, "forgecrm_test_api_42")
	want := "postgres://u:p@h:5432/forgecrm_test_api_42?sslmode=disable"
	if got != want {
		t.Fatalf("databaseURL = %q, want %q", got, want)
	}
}

func TestAdminURLPointsAtPostgres(t *testing.T) {
	base, _ := url.Parse("postgres://u:p@h:5432/forgecrm_test?sslmode=disable")
	got := adminURL(base)
	if !strings.HasSuffix(got, "/postgres?sslmode=disable") {
		t.Fatalf("adminURL = %q, want the /postgres maintenance database", got)
	}
}
