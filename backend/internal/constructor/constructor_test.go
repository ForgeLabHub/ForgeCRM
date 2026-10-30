package constructor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureSource builds a throwaway "backend" tree that Generate can copy from, so
// the tests do not depend on the real monolith's package list. That independence
// is the point: the constructor imports no ForgeCRM package, and its tests should
// not need one either, or the package could not be extracted with them.
//
// The fixture module has two packages. `demo` imports `helper` through the
// monolith prefix, so a generation that rewrites imports is exercised, and a
// generation that does not is caught.
func fixtureSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module github.com/geff1778/forgecrm\n\ngo 1.26.10\n")
	write(t, filepath.Join(root, "internal", "demo", "demo.go"), `package demo

import "github.com/geff1778/forgecrm/internal/helper"

// Value returns the helper's value.
func Value() int { return helper.Value() }
`)
	write(t, filepath.Join(root, "internal", "demo", "demo_test.go"), `package demo

import "testing"

func TestValue(t *testing.T) {
if Value() != 7 {
t.Fatal("value")
}
}
`)
	write(t, filepath.Join(root, "internal", "helper", "helper.go"), `package helper

// Value is a constant used by the fixture.
func Value() int { return 7 }
`)
	return root
}

// fixtureRecipe returns a recipe that names fixtureSource's packages and enables
// no demo or env, so the shape under test is the manifest handling, not a
// specific checked-in recipe.
func fixtureRecipe(t *testing.T) *Recipe {
	t.Helper()
	r, err := Load("analytics-demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Copy the bundled recipe's structure but point it at the fixture packages.
	r.Modules = []string{"demo"}
	r.Env = nil
	r.Demo.Enabled = false
	r.Demo.Command = ""
	return r
}

func TestLoadBundledRecipeIsValid(t *testing.T) {
	r, err := Load("analytics-demo")
	if err != nil {
		t.Fatalf("Load(analytics-demo): %v", err)
	}
	if r.Name != "analytics-demo" {
		t.Errorf("name = %q", r.Name)
	}
	if len(r.Modules) == 0 || r.Modules[0] != "analytics" {
		t.Errorf("modules = %v", r.Modules)
	}
	if !r.Demo.Enabled || r.Demo.Command == "" {
		t.Errorf("demo not configured: %+v", r.Demo)
	}
}

func TestListReturnsTheBundledRecipes(t *testing.T) {
	recipes, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recipes) == 0 {
		t.Fatal("the catalog is empty; a construction with no recipe is not useful")
	}
	found := false
	for _, r := range recipes {
		if r.Name == "analytics-demo" {
			found = true
		}
	}
	if !found {
		t.Errorf("analytics-demo is not in the catalog: %v", recipes)
	}
}

func TestLoadUnknownRecipeNamesTheCatalog(t *testing.T) {
	_, err := Load("does-not-exist")
	if err == nil {
		t.Fatal("loading an unknown recipe succeeded")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error does not list the catalog: %v", err)
	}
}

func TestGenerateProducesAStandaloneProject(t *testing.T) {
	out := t.TempDir()
	res, err := Generate(fixtureRecipe(t), Options{SourceRoot: fixtureSource(t), OutputDir: out})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Modules) != 1 || res.Modules[0] != "demo" {
		t.Errorf("modules = %v", res.Modules)
	}

	// The generated go.mod must name the recipe's module, not ForgeCRM's.
	gomod := readFile(t, filepath.Join(out, "go.mod"))
	if !strings.Contains(gomod, "module "+res.Recipe) && !strings.Contains(gomod, "module example.com/analytics-demo") {
		t.Errorf("go.mod does not name the recipe module:\n%s", gomod)
	}
	if strings.Contains(gomod, "geff1778/forgecrm") {
		t.Errorf("go.mod still names the monolith:\n%s", gomod)
	}

	// The cross-package import must be rewritten to the new module path.
	demo := readFile(t, filepath.Join(out, "demo", "demo.go"))
	if strings.Contains(demo, "github.com/geff1778/forgecrm") {
		t.Errorf("the copy still imports the monolith:\n%s", demo)
	}
	if !strings.Contains(demo, "example.com/analytics-demo/helper") {
		t.Errorf("the import was not re-pointed at the recipe module:\n%s", demo)
	}

	// A recipe with no env must not write a configuration file.
	if _, err := os.Stat(filepath.Join(out, ".env.example")); err == nil {
		t.Error(".env.example was written for a recipe that reads no configuration")
	}
}

func TestGenerateRefusesANonEmptyDirectory(t *testing.T) {
	r := fixtureRecipe(t)
	out := t.TempDir()
	// Put a file there first: it stands in for a user's work.
	keep := filepath.Join(out, "do-not-delete.txt")
	if err := os.WriteFile(keep, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Generate(r, Options{SourceRoot: fixtureSource(t), OutputDir: out})
	if err == nil {
		t.Fatal("generation into a non-empty directory succeeded")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("error does not explain the refusal: %v", err)
	}
	// The existing file must be untouched.
	if got := readFile(t, keep); got != "precious" {
		t.Errorf("existing file was modified: %q", got)
	}
}

func TestGenerateReportsAMissingModule(t *testing.T) {
	r := fixtureRecipe(t)
	r.Modules = []string{"no-such-module"}
	out := t.TempDir()
	_, err := Generate(r, Options{SourceRoot: fixtureSource(t), OutputDir: out})
	if err == nil {
		t.Fatal("generation with a missing module succeeded")
	}
	if !strings.Contains(err.Error(), "no-such-module") {
		t.Errorf("error does not name the missing module: %v", err)
	}
	// Nothing must be written when the module list cannot be resolved.
	entries, _ := os.ReadDir(out)
	if len(entries) != 0 {
		t.Errorf("a failed generation left %d files behind", len(entries))
	}
}

func TestGenerateIsReproducible(t *testing.T) {
	r := fixtureRecipe(t)
	root := fixtureSource(t)
	first := t.TempDir()
	second := t.TempDir()
	if _, err := Generate(r, Options{SourceRoot: root, OutputDir: first}); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(r, Options{SourceRoot: root, OutputDir: second}); err != nil {
		t.Fatal(err)
	}

	// The same recipe generated twice must produce byte-identical trees. A
	// scrambled map order or a timestamp in a template would break this, and it is
	// the property that makes a generated project reviewable.
	firstFiles := walk(t, first)
	secondFiles := walk(t, second)
	if len(firstFiles) != len(secondFiles) {
		t.Fatalf("file counts differ: %d vs %d", len(firstFiles), len(secondFiles))
	}
	for i := range firstFiles {
		a, b := firstFiles[i], secondFiles[i]
		if a != b {
			t.Fatalf("file lists differ at %d: %q vs %q", i, a, b)
		}
		if ga, gb := readFile(t, filepath.Join(first, a)), readFile(t, filepath.Join(second, b)); ga != gb {
			t.Errorf("%s differs between two runs of the same recipe", a)
		}
	}
}

func TestGenerateNeverCarriesASecret(t *testing.T) {
	// A recipe that does read configuration must still not carry a value for a
	// secret. The generator leaves secret entries blank on purpose.
	r := fixtureRecipe(t)
	r.Env = []EnvVar{
		{Name: "DEMO_TOKEN", Description: "a token", Secret: true},
		{Name: "DEMO_MODE", Description: "a mode", Default: "test"},
	}
	out := t.TempDir()
	if _, err := Generate(r, Options{SourceRoot: fixtureSource(t), OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	env := readFile(t, filepath.Join(out, ".env.example"))
	if !strings.Contains(env, "DEMO_TOKEN=\n") {
		t.Errorf("secret entry was not left blank:\n%s", env)
	}
	if !strings.Contains(env, "DEMO_MODE=test") {
		t.Errorf("non-secret default was not written:\n%s", env)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// walk returns every file under dir as a sorted, slash-separated relative path.
func walk(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		r, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(r))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}
