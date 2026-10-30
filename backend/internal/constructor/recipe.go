// Package constructor turns a recipe into a standalone Go project.
//
// A recipe names a set of ForgeCRM modules, the adapters they use and a runnable
// demo. Generating it copies exactly those modules out of the monolith, rewrites
// their imports to the new module path, renders the recipe's templates and writes
// a README. The result is a project that builds, tests and runs with no
// dependency on the rest of ForgeCRM.
//
// It is the operational half of the extraction harness in scripts/extract-test:
// the harness proves a module *can* stand alone in CI, and the constructor is how
// someone actually does it, for a chosen combination rather than a single package.
package constructor

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// recipesFS holds the recipe catalog: one directory per recipe, each with a
// recipe.yaml and the files to render.
//
//go:embed recipes
var recipesFS embed.FS

// Recipe is a parsed recipe.yaml. The yaml tags are lower_snake_case to match the
// manifest; the JSON tags are only used by the CLI's `--json` listing.
type Recipe struct {
	Name        string `yaml:"name"`
	Version     int    `yaml:"version"`
	Description string `yaml:"description"`

	// ModulePath is the Go module path of the generated project, and GoVersion is
	// its toolchain. Both are declared so the generated go.mod does not depend on
	// the monolith's own module path.
	ModulePath string `yaml:"module_path"`
	GoVersion  string `yaml:"go_version"`

	// Modules are the packages to copy, by their path under backend/internal.
	Modules []string `yaml:"modules"`

	// Adapters names the implementation each port uses in the generated project.
	// It is documentation and validation, not code generation: a recipe that names
	// an adapter that does not exist is rejected at load time.
	Adapters map[string]string `yaml:"adapters"`

	// Env lists the environment variables the generated project reads. When it is
	// empty no .env.example is written, because a project with no configuration
	// needs no configuration file.
	Env []EnvVar `yaml:"env"`

	Demo struct {
		Enabled bool   `yaml:"enabled"`
		Command string `yaml:"command"`
	} `yaml:"demo"`

	// Commands are the commands the README and CI tell the reader to run.
	Commands map[string]string `yaml:"commands"`

	// dir is the recipe's directory inside recipesFS, set by Load.
	dir string
}

// EnvVar is one environment variable a generated project may read.
type EnvVar struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Default     string `yaml:"default"`
	Secret      bool   `yaml:"secret"`
	Required    bool   `yaml:"required"`
}

// Load reads and validates one recipe by name.
func Load(name string) (*Recipe, error) {
	if name == "" {
		return nil, fmt.Errorf("recipe name is empty")
	}
	dir := path.Join("recipes", name)
	data, err := recipesFS.ReadFile(path.Join(dir, "recipe.yaml"))
	if err != nil {
		available, _ := List()
		names := make([]string, 0, len(available))
		for _, r := range available {
			names = append(names, r.Name)
		}
		return nil, fmt.Errorf("unknown recipe %q (available: %s)", name, strings.Join(names, ", "))
	}
	var r Recipe
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s/recipe.yaml: %w", dir, err)
	}
	r.dir = dir
	if err := r.validate(); err != nil {
		return nil, fmt.Errorf("recipe %q is invalid: %w", name, err)
	}
	return &r, nil
}

// List returns every recipe in the catalog, sorted by name.
func List() ([]*Recipe, error) {
	entries, err := fs.ReadDir(recipesFS, "recipes")
	if err != nil {
		return nil, fmt.Errorf("read recipe catalog: %w", err)
	}
	var out []*Recipe
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := Load(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// validate checks the manifest against itself. It cannot check that a module
// exists — that needs the source tree — and Generate does that separately, so a
// recipe whose module has been renamed fails at generation rather than here.
func (r *Recipe) validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if r.ModulePath == "" {
		return fmt.Errorf("module_path is required")
	}
	if r.GoVersion == "" {
		return fmt.Errorf("go_version is required")
	}
	if len(r.Modules) == 0 {
		return fmt.Errorf("at least one module is required")
	}
	for _, m := range r.Modules {
		if m == "" || strings.HasPrefix(m, "/") || strings.Contains(m, "..") {
			return fmt.Errorf("module %q is not a valid path under internal/", m)
		}
	}
	for port, adapter := range r.Adapters {
		if adapter == "" {
			return fmt.Errorf("adapter for %q is empty", port)
		}
	}
	for _, e := range r.Env {
		if e.Name == "" {
			return fmt.Errorf("an env entry has no name")
		}
	}
	if r.Demo.Enabled && r.Demo.Command == "" {
		return fmt.Errorf("demo is enabled but no command is set")
	}
	return nil
}
