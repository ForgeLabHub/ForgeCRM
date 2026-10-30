#!/usr/bin/env bash
#
# Constructor harness: prove that a recipe generates a project that really runs.
#
# What it does
# ------------
# For every recipe in the catalog it runs `forgecrm scaffold` into a throwaway
# directory, then builds, tests and runs the generated project there, checking the
# demo's deterministic output. A recipe that generates a project which does not
# build, or whose demo does not run, fails this check.
#
# Why this is not the same as the extraction harness
# --------------------------------------------------
# scripts/extract-test proves a single package *can* stand alone. This proves the
# constructor actually builds a project someone can use: the recipe manifest, the
# templates, the import rewrite and the demo are all exercised together, and the
# result is checked for the property that makes it trustworthy — no file still
# names ForgeCRM.
#
# Usage
# -----
#   scripts/constructor-test/run.sh                 # every recipe
#   scripts/constructor-test/run.sh analytics-demo  # only the named recipes
#   CONSTRUCTOR_TEST_KEEP=1 scripts/constructor-test/run.sh   # keep the project
#
# Exit status is non-zero if any recipe fails to generate and run.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
BACKEND_DIR="$REPO_ROOT/backend"

if command -v go >/dev/null 2>&1; then
  GO_BIN="$(command -v go)"
elif [ -x /usr/local/go/bin/go ]; then
  GO_BIN="/usr/local/go/bin/go"
else
  GO_BIN="go"
fi
export PATH="$(dirname "$GO_BIN"):$PATH"
export GOTOOLCHAIN=local

WORK_ROOT="${CONSTRUCTOR_TEST_ROOT:-${TMPDIR:-/tmp}/forgecrm-constructor-test}"
KEEP="${CONSTRUCTOR_TEST_KEEP:-0}"

step() { printf '\n== %s ==\n' "$*"; }
info() { printf '   %s\n' "$*"; }
ok()   { printf '   ok    %s\n' "$*"; }
warn() { printf '   warn  %s\n' "$*"; }
die()  { printf '   FAIL  %s\n' "$*" >&2; exit 1; }

# The recipe catalog, taken from the CLI's own JSON output so this script never
# keeps a second, hand-maintained list that could drift from the recipes.
step "constructor harness"
cd "$BACKEND_DIR" || die "no backend directory"
recipes_json="$("$GO_BIN" run ./cmd/forgecrm recipes --json)" || die "forgecrm recipes --json"
if command -v python3 >/dev/null 2>&1; then
  mapfile -t recipes < <(printf '%s' "$recipes_json" | python3 -c 'import json,sys; [print(r["name"]) for r in json.load(sys.stdin)]')
else
  mapfile -t recipes < <(printf '%s' "$recipes_json" | grep -o '"name": *"[^"]*"' | sed 's/.*: *"//; s/"$//')
fi

if [ "$#" -gt 0 ]; then
  recipes=("$@")
fi
if [ "${#recipes[@]}" -eq 0 ]; then
  die "no recipes to test"
fi
info "recipes: ${recipes[*]}"
info "scratch: $WORK_ROOT"

failed=()
for recipe in "${recipes[@]}"; do
  step "recipe: $recipe"
  work="$WORK_ROOT/$recipe"
  rm -rf "$work"
  mkdir -p "$work"

  if ! "$GO_BIN" run ./cmd/forgecrm scaffold "$recipe" --output "$work"; then
    failed+=("$recipe")
    continue
  fi

  # The generated project must not name the monolith anywhere. The constructor
  # already checks this; checking again here means a regression in the check
  # itself cannot hide behind the check.
  if grep -rn "github.com/geff1778/forgecrm" "$work" --include='*.go' >/dev/null 2>&1; then
    warn "$recipe generated a project that still imports ForgeCRM"
    grep -rn "github.com/geff1778/forgecrm" "$work" --include='*.go' >&2
    failed+=("$recipe")
    continue
  fi
  ok "no import of the ForgeCRM monolith"

  if ! ( cd "$work" && "$GO_BIN" build ./... ); then
    warn "$recipe generated a project that does not build"
    failed+=("$recipe")
    continue
  fi
  ok "go build ./..."

  if ! ( cd "$work" && "$GO_BIN" test -count=1 ./... ); then
    warn "$recipe generated a project whose tests fail"
    failed+=("$recipe")
    continue
  fi
  ok "go test ./..."

  # Run the demo twice and require identical output with a success marker. The
  # demo checks its own numbers and exits non-zero on a mismatch; running it
  # twice also confirms the output is deterministic.
  first="$( cd "$work" && "$GO_BIN" run ./cmd/demo 2>&1 )" || { warn "$recipe demo failed:"; printf '%s\n' "$first" >&2; failed+=("$recipe"); continue; }
  printf '%s\n' "$first" | grep -q "all checks passed" || { warn "$recipe demo did not report success"; failed+=("$recipe"); continue; }
  second="$( cd "$work" && "$GO_BIN" run ./cmd/demo 2>&1 )" || true
  if [ "$first" != "$second" ]; then
    warn "$recipe demo output is not deterministic"
    failed+=("$recipe")
    continue
  fi
  ok "demo ran and reported success (deterministic)"

  if [ "$KEEP" = "1" ]; then
    info "kept $work (CONSTRUCTOR_TEST_KEEP=1)"
  else
    rm -rf "$work"
  fi
done

step "result"
if [ "${#failed[@]}" -gt 0 ]; then
  printf '   FAIL  %d recipe(s) did not construct: %s\n' "${#failed[@]}" "${failed[*]}" >&2
  exit 1
fi
ok "every recipe constructed a working project"
