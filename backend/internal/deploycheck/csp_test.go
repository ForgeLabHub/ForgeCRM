package deploycheck

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The policy pins the pre-paint theme/layout bootstrap in frontend/index.html by
// hash. A hash is a claim about bytes, and nothing in the language connects it
// to the file it describes: editing the script leaves a policy that still
// parses, still deploys, and silently blocks the bootstrap it was meant to
// allow. The visible effect is a page that paints in the system theme and then
// recolours once React mounts, plus a `script-src-elem` violation reported on
// every navigation. Under the Report-Only delivery the app does not break, so
// the regression is invisible unless something recomputes the hash.
//
// These checks live in deploycheck rather than httpx for the same reason the
// other checks here do: they read repository files. deploycheck is not part of
// the extraction harness, which copies httpx out of the tree into a standalone
// module to prove httpx has no dependency on the application — a test inside
// httpx that reached for frontend/ would be that dependency, and would fail
// there for reasons that have nothing to do with CSP.
//
// The policy is read from the source file rather than imported so that this
// package keeps no build-time edge to httpx.

var (
	// inlineScriptRe extracts the body of every inline <script> in index.html.
	// The pre-paint bootstrap is the only one the application ships today, but a
	// second one would need its own hash, so the checks reason about the set.
	inlineScriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

	// cspScriptSrcRe pulls the script-src directive out of the policy literal.
	// Scanning the whole policy for a hash would also match one parked in
	// another directive, and a pin only means something where the browser reads
	// it.
	cspScriptSrcRe = regexp.MustCompile(`script-src[^;]*`)

	// cspScriptHashRe finds a sha256 source expression inside a directive.
	cspScriptHashRe = regexp.MustCompile(`sha256-[A-Za-z0-9+/]+=*`)
)

// TestCSPInlineScriptHashMatchesIndexHTML keeps the Content Security Policy's
// script-src hash in step with the one inline script it allows, in both
// directions: a script the policy does not pin, and a pinned hash that matches
// no script.
func TestCSPInlineScriptHashMatchesIndexHTML(t *testing.T) {
	root := repoRoot(t)

	policy, err := os.ReadFile(filepath.Join(root, "backend", "internal", "httpx", "httpx.go"))
	if err != nil {
		t.Fatalf("read httpx.go: %v", err)
	}
	directive := cspScriptSrcRe.FindString(string(policy))
	if directive == "" {
		t.Fatal("httpx.go declares no script-src directive; the policy no longer pins the inline script")
	}

	pinned := map[string]bool{}
	for _, h := range cspScriptHashRe.FindAllString(directive, -1) {
		pinned[h] = true
	}
	if len(pinned) == 0 {
		t.Fatalf("script-src pins no sha256 hash, so the inline bootstrap is blocked: %s", directive)
	}

	html, err := os.ReadFile(filepath.Join(root, "frontend", "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	bodies := inlineScriptRe.FindAllStringSubmatch(string(html), -1)
	if len(bodies) == 0 {
		// Not a vacuous pass: the policy pins an inline script, so a file with
		// none means the two sides disagree about what ships.
		t.Fatal("index.html declares no inline <script>, but the CSP pins one")
	}

	actual := map[string]bool{}
	for i, m := range bodies {
		sum := sha256.Sum256([]byte(m[1]))
		want := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
		actual[want] = true
		if !pinned[want] {
			t.Errorf(
				"inline script #%d hashes to %q but script-src does not pin it (pinned: %s); "+
					"recompute the hash in httpx.CSP after editing frontend/index.html",
				i, want, strings.Join(sortedKeys(pinned), ", "),
			)
		}
	}

	// A pinned hash that matches no inline script is dead weight, and it hides
	// the real one from anyone reading the policy.
	for h := range pinned {
		if !actual[h] {
			t.Errorf("script-src pins %q, which matches no inline script in index.html", h)
		}
	}
}

// TestCSPDeclaresTheDirectivesTheAppReliesOn holds the rest of the policy's
// shape. These are not style preferences: each is a guarantee the application
// depends on, and dropping one would be invisible in review because nothing
// fails to compile.
func TestCSPDeclaresTheDirectivesTheAppReliesOn(t *testing.T) {
	root := repoRoot(t)

	src, err := os.ReadFile(filepath.Join(root, "backend", "internal", "httpx", "httpx.go"))
	if err != nil {
		t.Fatalf("read httpx.go: %v", err)
	}
	policy := string(src)

	required := []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"object-src 'none'",
		"report-uri /csp-report",
		"report-to csp-endpoint",
	}
	for _, want := range required {
		if !strings.Contains(policy, want) {
			t.Errorf("CSP is missing %q", want)
		}
	}

	// The application has no worker, no manifest and no third-party frame. Each
	// is denied explicitly so that adding one becomes a decision rather than a
	// default.
	for _, denied := range []string{"worker-src 'none'", "manifest-src 'none'", "frame-src 'none'"} {
		if !strings.Contains(policy, denied) {
			t.Errorf("CSP is missing %q", denied)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
