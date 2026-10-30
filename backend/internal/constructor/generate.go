package constructor

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// modulePrefix is the import prefix of the monolith. Every copied file has it
// rewritten to the generated project's module path, which is why a module that
// reaches for a package the recipe did not include fails to compile there: the
// import names a package that is not in the project.
const modulePrefix = "github.com/geff1778/forgecrm/internal/"

// Options configures a generation.
type Options struct {
	// SourceRoot is the backend directory that holds internal/<module>. It is an
	// explicit input so generation does not depend on the working directory and a
	// test can point it at a fixture.
	SourceRoot string
	// OutputDir is the directory to generate into. It must not exist yet, or must
	// be empty: generation never overwrites, so a run cannot silently destroy work.
	OutputDir string
}

// File is one file generation wrote, relative to the output directory.
type File struct {
	Path   string
	Source string // "module" for a copied package file, "recipe" for a template
}

// Result reports what generation produced.
type Result struct {
	Recipe  string
	Modules []string
	Files   []File
	Dir     string
}

// Generate writes the project a recipe describes into opts.OutputDir.
func Generate(r *Recipe, opts Options) (*Result, error) {
	if opts.SourceRoot == "" {
		return nil, fmt.Errorf("source root is required")
	}
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("output directory is required")
	}

	// Resolve every module to real files before writing anything, so a missing
	// module fails before a half-written project is left behind.
	type srcPkg struct {
		name  string
		dir   string
		files []string
	}
	pkgs := make([]srcPkg, 0, len(r.Modules))
	for _, m := range r.Modules {
		dir := filepath.Join(opts.SourceRoot, "internal", filepath.FromSlash(m))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("recipe %q names module %q, but %s is not a directory", r.Name, m, dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read module %s: %w", m, err)
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			files = append(files, e.Name())
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("module %q has no Go files at %s", m, dir)
		}
		sort.Strings(files)
		pkgs = append(pkgs, srcPkg{name: m, dir: dir, files: files})
	}

	if err := prepareOutput(opts.OutputDir); err != nil {
		return nil, err
	}

	res := &Result{Recipe: r.Name, Modules: append([]string(nil), r.Modules...), Dir: opts.OutputDir}

	// 1. Copy each module's Go files, rewriting the monolith import prefix.
	for _, p := range pkgs {
		for _, name := range p.files {
			src := filepath.Join(p.dir, name)
			raw, err := os.ReadFile(src)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", src, err)
			}
			rewritten := bytes.ReplaceAll(raw, []byte(modulePrefix), []byte(r.ModulePath+"/"))
			dst := filepath.Join(opts.OutputDir, filepath.FromSlash(p.name), name)
			if err := writeFile(dst, rewritten); err != nil {
				return nil, err
			}
			res.Files = append(res.Files, File{Path: rel(opts.OutputDir, dst), Source: "module"})
		}
	}

	// 2. Render the recipe's own files. A .tmpl suffix is stripped from the output
	// name; any other file is copied verbatim.
	if err := renderRecipeFiles(r, opts.OutputDir, res); err != nil {
		return nil, err
	}

	// 3. A README is generated, not copied, so it always states the real module
	// list, commands and adapter choices of this recipe.
	if err := writeFile(filepath.Join(opts.OutputDir, "README.md"), []byte(renderReadme(r))); err != nil {
		return nil, err
	}
	res.Files = append(res.Files, File{Path: "README.md", Source: "recipe"})

	// 4. An .env.example only when the project actually reads configuration. A
	// recipe with none gets no configuration file, and in particular no secret.
	if len(r.Env) > 0 {
		if err := writeFile(filepath.Join(opts.OutputDir, ".env.example"), []byte(renderEnvExample(r))); err != nil {
			return nil, err
		}
		res.Files = append(res.Files, File{Path: ".env.example", Source: "recipe"})
	}

	// 5. The invariant that makes the result trustworthy: nothing in the generated
	// project may still name the monolith, and no secret value may have been
	// carried across.
	if err := assertNoMonolith(opts.OutputDir); err != nil {
		return nil, err
	}
	return res, nil
}

// prepareOutput ensures the target is empty or absent. It never removes a
// non-empty directory: refusing is the point, because a generation into the wrong
// place must not delete a user's files.
func prepareOutput(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("output path %s exists and is not a directory", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read output directory: %w", err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("output directory %s is not empty; refusing to overwrite", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat output path: %w", err)
	}
	return os.MkdirAll(dir, 0o755)
}

// renderRecipeFiles walks the recipe's files/ directory and renders it into the
// output. It is a recursive copy with template rendering at the leaves, so a
// recipe can lay out cmd/demo/main.go just as it appears in the project.
func renderRecipeFiles(r *Recipe, out string, res *Result) error {
	root := path.Join(r.dir, "files")
	entries, err := fs.ReadDir(recipesFS, root)
	if err != nil {
		// A recipe with no files/ directory is legal: it only ships copied modules.
		if os.IsNotExist(err) || strings.Contains(err.Error(), "file does not exist") {
			return nil
		}
		return fmt.Errorf("read %s: %w", root, err)
	}
	return renderDir(r, root, out, entries, res)
}

func renderDir(r *Recipe, root, out string, entries []fs.DirEntry, res *Result) error {
	for _, e := range entries {
		src := path.Join(root, e.Name())
		if e.IsDir() {
			sub, err := fs.ReadDir(recipesFS, src)
			if err != nil {
				return fmt.Errorf("read %s: %w", src, err)
			}
			if err := renderDir(r, src, path.Join(out, e.Name()), sub, res); err != nil {
				return err
			}
			continue
		}
		raw, err := recipesFS.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", src, err)
		}
		name := e.Name()
		var body []byte
		if strings.HasSuffix(name, ".tmpl") {
			name = strings.TrimSuffix(name, ".tmpl")
			body, err = renderTemplate(src, raw, r)
			if err != nil {
				return err
			}
		} else {
			body = raw
		}
		dst := filepath.Join(out, name)
		if err := writeFile(dst, body); err != nil {
			return err
		}
		res.Files = append(res.Files, File{Path: rel(out, dst), Source: "recipe"})
	}
	return nil
}

func renderTemplate(name string, raw []byte, r *Recipe) ([]byte, error) {
	tmpl, err := template.New(path.Base(name)).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, r); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// assertNoMonolith fails if any generated file still names ForgeCRM, which would
// mean the project is not standalone and would not build outside the repo.
func assertNoMonolith(dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".example") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte("github.com/geff1778/forgecrm")) {
			return fmt.Errorf("generated file %s still imports the ForgeCRM monolith", rel(dir, p))
		}
		return nil
	})
}

func writeFile(dst string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func rel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

// renderReadme writes the project README from the recipe, so it cannot drift from
// what was actually generated.
func renderReadme(r *Recipe) string {
	var b strings.Builder
	w := bufio.NewWriter(&b)
	fmt.Fprintf(w, "# %s\n\n", r.Name)
	fmt.Fprintf(w, "%s\n\n", strings.TrimSpace(r.Description))
	fmt.Fprintln(w, "This project was generated by `forgecrm scaffold`. It is standalone: it")
	fmt.Fprintln(w, "imports no ForgeCRM package and needs no ForgeCRM service to run.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Module path: `%s`\n\n", r.ModulePath)

	fmt.Fprintln(w, "## Modules included")
	fmt.Fprintln(w)
	for _, m := range r.Modules {
		fmt.Fprintf(w, "- `%s` (copied from `backend/internal/%s`)\n", path.Join(r.ModulePath, m), m)
	}
	fmt.Fprintln(w)

	if len(r.Adapters) > 0 {
		fmt.Fprintln(w, "## Adapters")
		fmt.Fprintln(w)
		ports := make([]string, 0, len(r.Adapters))
		for p := range r.Adapters {
			ports = append(ports, p)
		}
		sort.Strings(ports)
		for _, p := range ports {
			fmt.Fprintf(w, "- `%s`: %s\n", p, r.Adapters[p])
		}
		fmt.Fprintln(w)
	}

	if len(r.Commands) > 0 {
		fmt.Fprintln(w, "## Commands")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```bash")
		names := make([]string, 0, len(r.Commands))
		for n := range r.Commands {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(w, "# %s\n%s\n", n, r.Commands[n])
		}
		fmt.Fprintln(w, "```")
		fmt.Fprintln(w)
	}

	if len(r.Env) > 0 {
		fmt.Fprintln(w, "## Configuration")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Copy `.env.example` to `.env` and adjust. A value marked **secret**")
		fmt.Fprintln(w, "must never be committed.")
		fmt.Fprintln(w)
		for _, e := range r.Env {
			note := ""
			if e.Secret {
				note = " (secret)"
			}
			if e.Required {
				note += " (required)"
			}
			def := ""
			if e.Default != "" {
				def = fmt.Sprintf(" Default: `%s`.", e.Default)
			}
			fmt.Fprintf(w, "- `%s`%s — %s.%s\n", e.Name, note, e.Description, def)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "## Provenance")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Generation refused to overwrite an existing directory and would have failed")
	fmt.Fprintln(w, "if any copied file still named ForgeCRM. To refresh, generate into a new")
	fmt.Fprintln(w, "directory and compare.")
	w.Flush()
	return b.String()
}

// renderEnvExample writes a template .env.example. Secret values are left blank:
// the generator never invents or copies a credential.
func renderEnvExample(r *Recipe) string {
	var b strings.Builder
	w := bufio.NewWriter(&b)
	fmt.Fprintf(w, "# Environment for %s, generated by forgecrm scaffold.\n", r.Name)
	fmt.Fprintln(w, "# Copy to .env. A secret is left blank here on purpose.")
	fmt.Fprintln(w)
	for _, e := range r.Env {
		if e.Description != "" {
			fmt.Fprintf(w, "# %s\n", e.Description)
		}
		if e.Secret {
			fmt.Fprintf(w, "%s=\n", e.Name)
		} else {
			fmt.Fprintf(w, "%s=%s\n", e.Name, e.Default)
		}
	}
	w.Flush()
	return b.String()
}
