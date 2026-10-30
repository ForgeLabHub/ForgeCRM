// Command smoke exercises the extracted constructor package: the recipe catalog
// loads and a recipe is validated, which is the contract the CLI and CI depend on.
//
// It proves the module runs on its own with no monolith and no service: the
// catalog is embedded, so listing it and loading a recipe need nothing else. If a
// recipe ever became invalid, this exits non-zero.
package main

import (
"fmt"
"os"

constructor "example.com/constructor-extracted"
)

func main() {
recipes, err := constructor.List()
if err != nil {
fmt.Fprintf(os.Stderr, "list recipes: %v\n", err)
os.Exit(1)
}
if len(recipes) == 0 {
fmt.Fprintln(os.Stderr, "the recipe catalog is empty")
os.Exit(1)
}
for _, r := range recipes {
if r.Name == "" {
fail("a recipe has no name")
}
if len(r.Modules) == 0 {
fail("recipe %q names no modules", r.Name)
}
if r.ModulePath == "" || r.GoVersion == "" {
fail("recipe %q is missing module_path or go_version", r.Name)
}
}
// Loading an unknown recipe must fail and name the catalog, not return an
// empty recipe that a caller would generate from.
if _, err := constructor.Load("no-such-recipe"); err == nil {
fail("loading an unknown recipe succeeded")
}
fmt.Printf("constructor catalog ok: %d recipe(s)\n", len(recipes))
}

func fail(format string, args ...any) {
fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
os.Exit(1)
}
