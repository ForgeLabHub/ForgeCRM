// Command forgecrm is the ForgeCRM constructor CLI.
//
// It is not the API or the worker: it is the tool that turns a recipe from
// internal/constructor into a standalone project on disk. Today it offers
//
// forgecrm recipes              list the recipe catalog
// forgecrm scaffold <recipe>    generate a project from a recipe
//
// The construction itself lives in internal/constructor; this command is only the
// argument parsing and the terminal output.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geff1778/forgecrm/internal/constructor"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "recipes", "list":
		os.Exit(runList(os.Args[2:]))
	case "scaffold", "generate":
		os.Exit(runScaffold(os.Args[2:]))
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "forgecrm: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `forgecrm — build a standalone project from a ForgeCRM recipe.

Usage:
  forgecrm recipes [--json]            list the recipe catalog
  forgecrm scaffold <recipe> --output <dir>
                                       generate a project into an empty directory

A generated project imports no ForgeCRM package and needs no ForgeCRM service.
`)
}

func runList(args []string) int {
	fs := flag.NewFlagSet("recipes", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the catalog as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	recipes, err := constructor.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "forgecrm: %v\n", err)
		return 1
	}
	if *asJSON {
		type row struct {
			Name        string   `json:"name"`
			Version     int      `json:"version"`
			Description string   `json:"description"`
			Modules     []string `json:"modules"`
			Demo        bool     `json:"demo"`
		}
		rows := make([]row, 0, len(recipes))
		for _, r := range recipes {
			rows = append(rows, row{r.Name, r.Version, r.Description, r.Modules, r.Demo.Enabled})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fmt.Fprintf(os.Stderr, "forgecrm: %v\n", err)
			return 1
		}
		return 0
	}
	if len(recipes) == 0 {
		fmt.Println("no recipes are bundled")
		return 0
	}
	for _, r := range recipes {
		demo := ""
		if r.Demo.Enabled {
			demo = "  [demo: " + r.Demo.Command + "]"
		}
		fmt.Printf("%-20s %s%s\n", r.Name, firstLine(r.Description), demo)
	}
	return 0
}

func runScaffold(args []string) int {
	fs := flag.NewFlagSet("scaffold", flag.ContinueOnError)
	output := fs.String("output", "", "directory to generate into (required)")
	// Accept the recipe name before or after the flag. Go's flag package stops at
	// the first positional, so `scaffold <recipe> --output <dir>` would otherwise
	// leave --output unparsed; reorder the flags in front of the names first.
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "forgecrm: scaffold needs exactly one recipe name")
		usage()
		return 2
	}
	if *output == "" {
		fmt.Fprintln(os.Stderr, "forgecrm: --output is required")
		return 2
	}

	recipe, err := constructor.Load(rest[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "forgecrm: %v\n", err)
		return 1
	}

	out, err := filepath.Abs(*output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "forgecrm: resolve output path: %v\n", err)
		return 1
	}
	sourceRoot, err := backendRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "forgecrm: locate backend source: %v\n", err)
		return 1
	}

	res, err := constructor.Generate(recipe, constructor.Options{SourceRoot: sourceRoot, OutputDir: out})
	if err != nil {
		fmt.Fprintf(os.Stderr, "forgecrm: %v\n", err)
		return 1
	}

	fmt.Printf("generated %s into %s\n", res.Recipe, res.Dir)
	fmt.Printf("  modules: %v\n", res.Modules)
	fmt.Printf("  files:   %d\n", len(res.Files))
	if recipe.Demo.Enabled {
		fmt.Printf("\ntry it:\n  cd %s && %s\n", res.Dir, recipe.Demo.Command)
	}
	return 0
}

// flagsFirst reorders args so every "-flag [value]" pair comes before the
// positional arguments, which is what the flag package requires to see all flags.
// It knows --output and --json take a value or are booleans; here the only
// value-taking flag is --output, and --json is a boolean, so the rule below is
// "an unset flag not in '=' form consumes the next token".
func flagsFirst(args []string) []string {
	flags := make([]string, 0, len(args))
	pos := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, pos...)
}

// backendRoot finds the backend directory that holds internal/. The recipe
// modules live there. It walks up from the executable's directory and from the
// working directory, so the command works both from a checkout and next to a
// built binary.
func backendRoot() (string, error) {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	for _, start := range candidates {
		dir := start
		for i := 0; i < 8; i++ {
			if isBackend(dir) {
				return dir, nil
			}
			// One level up may itself be the backend (a binary in backend/bin).
			if isBackend(filepath.Join(dir, "backend")) {
				return filepath.Join(dir, "backend"), nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("cannot find the backend source tree (a directory holding internal/ and go.mod)")
}

func isBackend(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "constructor", "recipes")); err != nil {
		return false
	}
	return true
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	if len(s) > 96 {
		return s[:96] + "…"
	}
	return s
}
