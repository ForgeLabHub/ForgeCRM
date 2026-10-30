# The constructor

The extraction harness answers one question: can this module stand alone? The
constructor answers the next one: can someone actually build a project from a
chosen set of modules, without doing the extraction by hand?

It is the operational half of the same idea. The harness proves a property in CI;
the constructor is the tool that acts on it.

## What it does

```bash
cd backend
go run ./cmd/forgecrm recipes             # list the catalog
go run ./cmd/forgecrm scaffold analytics-demo --output /tmp/demo
cd /tmp/demo && go run ./cmd/demo
```

`forgecrm scaffold`:

1. reads a **recipe** — a manifest naming the modules to copy, the adapters they
   use and a demo program;
2. copies exactly those modules out of `backend/internal`, rewriting every
   `github.com/geff1778/forgecrm/internal/...` import to the recipe's module path;
3. renders the recipe's templates (a `.tmpl` file becomes a real file);
4. writes a README generated from the recipe, so it cannot drift from what was
   built;
5. refuses to finish if any generated file still names ForgeCRM, or if the output
   directory already holds files.

The result is a project that builds, tests and runs with no dependency on the
monolith and no ForgeCRM service.

## Why it is trustworthy

The claim a constructor makes is "the project it produced is standalone". Three
things make that true rather than aspirational:

- **It refuses to overwrite.** Generating into a non-empty directory is an error,
  not a warning, so a mistyped path cannot delete a user's work. A failed
  generation also writes nothing: the module list is resolved before the first
  file is written.
- **It checks its own output.** After writing, it walks the tree and fails if any
  Go file still names the monolith. The name check would be worthless if the code
  could skip it, so `scripts/constructor-test/run.sh` repeats it independently.
- **The demo asserts numbers.** The generated `cmd/demo` does not print "success".
  It computes a fixed scenario and checks the results against the values the module
  produces inside ForgeCRM, exiting non-zero on a mismatch. A module that compiles
  but behaves differently is caught.

## A recipe

A recipe lives in `internal/constructor/recipes/<name>/`:

```
recipe.yaml          the manifest
files/               templates rendered into the project
  go.mod.tmpl
  cmd/demo/main.go.tmpl
```

`recipe.yaml` names the modules, the module path of the generated project, its Go
version, any adapters and environment variables, and the demo:

```yaml
name: analytics-demo
version: 1
description: >-
  A standalone analytics demonstration.
module_path: example.com/analytics-demo
go_version: "1.26.10"
modules:
  - analytics
adapters: {}
demo:
  enabled: true
  command: go run ./cmd/demo
commands:
  test: go test ./...
  run: go run ./cmd/demo
```

`modules` lists package paths under `backend/internal`. A recipe that needs
dependencies names them too; the generator still fails if a copied file reaches
for a package the recipe did not include, because the import would name a package
that is not in the project.

`env` is optional. When it is present, an `.env.example` is written and any entry
marked `secret` is left blank — the generator never invents or copies a
credential.

## The first recipe: `analytics-demo`

It lifts the pure `analytics` core — the money, ratio and scenario maths, the
forecast and the risk rules — into a project with no database, no external
service and no secrets. `analytics` has zero in-module and zero third-party
dependencies, so the recipe's module list is one line and the generated project
has no `require` block at all.

The demo reads a frozen snapshot of one organization's pipeline and its last
thirty days, then:

- runs the **funnel** scenario with 10% more leads;
- runs the **revenue** scenario with a higher ticket, a better win rate and more
  deals;
- runs the **retention** scenario with 20% less churn;
- runs a **trend forecast** over five observed months, scored against the naive
  baseline;
- scans for a **stalled deal**;
- checks every headline number against the value the module produces inside
  ForgeCRM.

## Verifying it

```bash
# each recipe generates a project that builds, tests and runs
make test-constructor

# keep a generated project for inspection
CONSTRUCTOR_TEST_KEEP=1 scripts/constructor-test/run.sh
```

The script reads the recipe list from `forgecrm recipes --json`, so it never keeps
a second, hand-maintained list that could drift from the recipes. It runs in CI as
the `constructor` job.

The constructor package is itself extraction-proven: it is a leaf that depends
only on the standard library and a YAML parser, and its tests use a throwaway
fixture tree rather than the real monolith. It is listed in
`scripts/extract-test/modules.txt` and pinned in `internal/archtest`.

## Adding a recipe

1. Create `internal/constructor/recipes/<name>/recipe.yaml`.
2. Add `files/` with the templates, or omit it for a project that only ships
   copied modules.
3. Run `go test ./internal/constructor/` — the loader validates the manifest.
4. Run `scripts/constructor-test/run.sh <name>` — it must generate a project that
   builds, tests and runs.
5. Only then does the recipe belong in the catalog. A recipe that does not
   produce a working project is a promise, not a feature.

## Limits

- The constructor copies whole packages, not selected files. A recipe is a
  package-level cut, which is the granularity the extraction work established.
- It does not fetch third-party modules: a generated project that needs one must
  run `go mod tidy` itself, with network access. The analytics-demo recipe needs
  none, so generation works offline.
- Adapters are declared and validated but not code-generated; a recipe that needs
  a fake implementation ships it under `files/`.
