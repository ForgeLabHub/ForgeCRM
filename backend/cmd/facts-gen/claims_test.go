package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file is the in-Go twin of scripts/check-doc-claims.sh. The shell gate runs
// in CI's docs job; these tests run on every `go test ./...`, so a drift that the
// shell gate would catch is also covered by the ordinary backend suite, and the
// comparison logic is exercised with a mutation rather than only trusted.

var (
	reReadmeRBAC = regexp.MustCompile(`RBAC:\s*([0-9]+)\s+разрешени`)
	reReadmeAPI  = regexp.MustCompile(`([0-9]+)\s+путей,\s*([0-9]+)\s+операци`)
	// A license claim must name MIT as a whole word in a licensing phrase. A bare
	// "MIT" would also match the "MIT" inside FORGECRM_RATE_LIMIT_STORE, which is
	// how a broken claim slips through — the same trap the shell guard fixes with
	// a word boundary.
	reMITClaim = regexp.MustCompile(`(?i)лиценз[^.]*\bMIT\b`)
)

// checkDocClaims compares the numbers and statuses a document states against the
// values measured from code. It returns one human-readable line per mismatch, so
// the same function serves both the healthy-tree test and the mutation test.
//
// The "real" arguments are the source-of-truth values; the "doc" arguments are
// the document text. Nothing here compares two hardcoded numbers.
func checkDocClaims(readme, security, license string, rbacCount, apiPaths, apiOps int) []string {
	var problems []string

	// RBAC catalogue count.
	if m := reReadmeRBAC.FindStringSubmatch(readme); m == nil {
		problems = append(problems, "README does not state the RBAC permission count")
	} else if got, _ := strconv.Atoi(m[1]); got != rbacCount {
		problems = append(problems, "README says "+m[1]+" permissions, catalogue has "+strconv.Itoa(rbacCount))
	}

	// OpenAPI path/operation counts.
	if m := reReadmeAPI.FindStringSubmatch(readme); m == nil {
		problems = append(problems, "README does not state the OpenAPI path/operation counts")
	} else {
		gotPaths, _ := strconv.Atoi(m[1])
		gotOps, _ := strconv.Atoi(m[2])
		if gotPaths != apiPaths || gotOps != apiOps {
			problems = append(problems, "README says "+m[1]+"/"+m[2]+" paths/operations, spec has "+
				strconv.Itoa(apiPaths)+"/"+strconv.Itoa(apiOps))
		}
	}

	// LICENSE file vs the claims made in prose.
	licMIT := strings.Contains(license, "MIT License")
	if licMIT {
		if !reMITClaim.MatchString(readme) {
			problems = append(problems, "README does not declare the MIT licence although LICENSE is MIT")
		}
		if !reMITClaim.MatchString(security) {
			problems = append(problems, "SECURITY.md does not declare the MIT licence although LICENSE is MIT")
		}
	}
	if !licMIT && (reMITClaim.MatchString(readme) || reMITClaim.MatchString(security)) {
		problems = append(problems, "documents declare MIT but LICENSE is not MIT")
	}

	return problems
}

func TestDocumentationClaimsMatchCode(t *testing.T) {
	repo := findRepoRoot(t)
	rep, err := Compute(repo)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	readme := readFile(t, filepath.Join(repo, "README.md"))
	security := readFile(t, filepath.Join(repo, "SECURITY.md"))
	license := readFile(t, filepath.Join(repo, "LICENSE"))

	problems := checkDocClaims(readme, security, license,
		rep.Facts.RbacCatalog, rep.Facts.OpenAPIPaths, rep.Facts.OpenAPIOperations)
	if len(problems) > 0 {
		t.Errorf("documentation drifted from code:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestDocumentationClaimsDetectDrift proves the check is not vacuous: each
// mutation the guard exists to catch must produce a mismatch.
func TestDocumentationClaimsDetectDrift(t *testing.T) {
	repo := findRepoRoot(t)
	rep, err := Compute(repo)
	if err != nil {
		t.Fatal(err)
	}
	readme := readFile(t, filepath.Join(repo, "README.md"))
	security := readFile(t, filepath.Join(repo, "SECURITY.md"))
	license := readFile(t, filepath.Join(repo, "LICENSE"))
	rbac, paths, ops := rep.Facts.RbacCatalog, rep.Facts.OpenAPIPaths, rep.Facts.OpenAPIOperations

	cases := []struct {
		name             string
		readme, security string
		license          string
		wantSubstr       string
	}{
		{
			name:       "wrong RBAC count",
			readme:     replaceFirst(readme, reReadmeRBAC, "RBAC: 999999 разрешения"),
			security:   security,
			license:    license,
			wantSubstr: "permissions",
		},
		{
			name:       "wrong OpenAPI counts",
			readme:     replaceFirst(readme, reReadmeAPI, "1 путей, 2 операции"),
			security:   security,
			license:    license,
			wantSubstr: "paths/operations",
		},
		{
			name:       "README stops declaring MIT",
			readme:     reMITClaim.ReplaceAllString(readme, "лицензия proprietary"),
			security:   security,
			license:    license,
			wantSubstr: "README does not declare the MIT licence",
		},
		{
			name:       "SECURITY stops declaring MIT",
			readme:     readme,
			security:   reMITClaim.ReplaceAllString(security, "лицензия proprietary"),
			license:    license,
			wantSubstr: "SECURITY.md does not declare the MIT licence",
		},
		{
			name:       "LICENSE removed but docs still claim MIT",
			readme:     readme,
			security:   security,
			license:    "proprietary\n",
			wantSubstr: "declare MIT but LICENSE is not MIT",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := checkDocClaims(tc.readme, tc.security, tc.license, rbac, paths, ops)
			if len(problems) == 0 {
				t.Fatalf("mutation %q was not detected", tc.name)
			}
			if !containsSubstr(problems, tc.wantSubstr) {
				t.Errorf("mutation %q produced %v, want a line containing %q", tc.name, problems, tc.wantSubstr)
			}
		})
	}
}

// TestMITClaimIgnoresLimitStoreName pins the trap the shell guard also fixes: a
// bare "MIT" matches the "MIT" in FORGECRM_RATE_LIMIT_STORE, so a document whose
// real licence statement was removed would still pass if the match were not
// word-bounded and licence-scoped.
func TestMITClaimIgnoresLimitStoreName(t *testing.T) {
	if reMITClaim.MatchString("set FORGECRM_RATE_LIMIT_STORE=memory to keep it in process") {
		t.Error(`bare "MIT" inside FORGECRM_RATE_LIMIT_STORE must not count as a licence claim`)
	}
	if !reMITClaim.MatchString("ForgeCRM распространяется под лицензией MIT (см. LICENSE).") {
		t.Error("a real MIT licence statement must be recognised")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// replaceFirst substitutes the first match of re in s with repl.
func replaceFirst(s string, re *regexp.Regexp, repl string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + repl + s[loc[1]:]
}

func containsSubstr(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
