# Constructor harness

Proves that a recipe generates a project someone can actually use: it builds,
its tests pass, and its demo runs and reports success.

## Why it exists

The extraction harness (`scripts/extract-test`) proves a single package *can*
stand alone. That is a property, not a usable artifact. The constructor
(`internal/constructor`, `cmd/forgecrm`) turns a recipe — a manifest naming a set
of proven modules — into a project on disk. This harness checks the result rather
than the promise: it scaffolds every recipe, then builds, tests and runs what came
out.

A recipe only belongs in the catalog once this passes. A recipe that generates a
project which does not build is a claim nobody tested, which is what the
extraction work exists to avoid.

## What it checks

For each recipe:

1. `forgecrm scaffold <recipe> --output <dir>` succeeds.
2. The generated tree imports no ForgeCRM package. The generator checks this too;
   checking again here means a regression in the check cannot hide behind it.
3. `go build ./...` succeeds in the generated project.
4. `go test ./...` succeeds in the generated project.
5. The demo runs, reports success, and prints the same output twice — so a
   non-deterministic demo is caught.

## Running it

```bash
# every recipe
scripts/constructor-test/run.sh

# one recipe
scripts/constructor-test/run.sh analytics-demo

# keep a generated project for inspection
CONSTRUCTOR_TEST_KEEP=1 scripts/constructor-test/run.sh
```

The generated projects are written to
`${TMPDIR:-/tmp}/forgecrm-constructor-test/<recipe>` and removed afterwards unless
`CONSTRUCTOR_TEST_KEEP=1` is set.

The recipe list is read from `forgecrm recipes --json`, so this script never keeps
a second, hand-maintained list that could drift from the recipes.

## Adding a recipe

1. Add it under `internal/constructor/recipes/<name>/`.
2. Run `scripts/constructor-test/run.sh <name>`; it must scaffold, build, test and
   run.
3. Only then is it part of the catalog. See `docs/CONSTRUCTOR.md`.
