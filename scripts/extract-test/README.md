# Extraction harness

Proves that a module is independent of the ForgeCRM monolith, by removing the
monolith and checking the module still works.

## Why this exists

A package can build inside the monolith while depending on half of it. The build
system is happy because the dependency is present; nothing fails until someone
tries to take the package out, and then it fails in a way that is expensive to
unpick.

"This module is extraction-ready" is therefore a claim about a state nobody has
tested. The only way to test it is to actually do the extraction: copy the
package into a clean module, without the rest of ForgeCRM, and build, test and run
it there. If it reaches back into the application layer, the copied project no
longer contains the code it reached for and the build fails.

That is what this harness does, and it is why it is a test rather than a
paragraph in a document.

## What counts as a hidden dependency

Anything the package needs that is not:

- the Go standard library,
- a third-party module declared in the module's `go.mod`, or
- another package the harness explicitly copies alongside it.

An import of `github.com/geff1778/forgecrm/internal/...` is a hidden dependency,
even if it looks harmless, because it is exactly the edge that disappears when the
module is taken out.

## Running it

```bash
# every module listed in modules.txt
scripts/extract-test/run.sh

# one module
scripts/extract-test/run.sh objectstore

# keep the extracted project for inspection
EXTRACT_TEST_KEEP=1 scripts/extract-test/run.sh objectstore
```

The extracted project is written to `${TMPDIR:-/tmp}/forgecrm-extract-test/<module>`
and removed afterwards unless `EXTRACT_TEST_KEEP=1` is set.

Each module's extraction must pass before the module is listed in `modules.txt`.
Listing a module that does not extract would make the harness report a property
that is not true, which is worse than not listing it at all.

## Adding a module

1. Create `scripts/extract-test/<module>/`.
2. Write `go.mod` for the extracted module. The simplest way to get it exactly
   right is to do the extraction by hand once, run `go mod tidy`, and copy the
   result. Do not hand-write the indirect requirements.
3. Write `cmd/smoke/main.go`: a small program that exercises the module and exits
   non-zero on failure. It imports the module by its ForgeCRM path; the setup
   script rewrites that to the extracted path.
4. Write `setup.sh`, modelled on an existing one. Use `extract_pkg` for the module
   and for any in-module package it depends on, `install_module_files` for the
   manifest and smoke, and `rewrite_import` to point the smoke at the extracted
   copy.
5. Add the module name to `modules.txt`.

## Relationship to the constructor

This harness proves a module *can* stand alone. `internal/constructor` (with
`cmd/forgecrm` and `scripts/constructor-test/run.sh`) is how a combination of
proven modules becomes a working project in one command. A recipe is only built
from modules this harness has passed; the two are not substitutes.

## Relationship to the archtest

`internal/archtest` enforces the same boundaries cheaply, from the import graph,
on every `go test ./...`. This harness is the expensive, always-true version: it
does not read a graph, it removes the code and builds.

The two are deliberately independent. A stale archtest cannot hide a violation,
because the harness would still fail; and a skipped CI job cannot hide one, because
the archtest would still fail. The archtest pins the *shape* of the boundary; the
harness proves the boundary is real.
