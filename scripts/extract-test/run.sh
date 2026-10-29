#!/usr/bin/env bash
#
# Extraction harness: prove that a module is independent of the ForgeCRM monolith.
#
# What it does
# ------------
# For each module in modules.txt it runs scripts/extract-test/<module>/setup.sh,
# which copies the package into a clean Go module and builds, tests and runs it
# there. A module that extracts passes; one that reaches back into the application
# layer fails, because the copied project no longer contains the code it reached
# for.
#
# Why this is a real check and not documentation
# ----------------------------------------------
# A package can build inside the monolith while depending on half of it. "This
# module is extraction-ready" is only true if removing the rest of the monolith
# still leaves it working, and the only way to know that is to actually remove it.
# That is what this runs.
#
# Usage
# -----
#   scripts/extract-test/run.sh                 # every module in modules.txt
#   scripts/extract-test/run.sh objectstore     # only the named modules
#   EXTRACT_TEST_KEEP=1 scripts/extract-test/run.sh objectstore   # keep the copy
#
# Exit status is non-zero if any module fails to extract.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$HERE/lib.sh"

MODULES_FILE="$HERE/modules.txt"
[ -f "$MODULES_FILE" ] || die "missing $MODULES_FILE"

# Named modules on the command line, or every module listed in modules.txt.
if [ "$#" -gt 0 ]; then
  MODULES=("$@")
else
  mapfile -t MODULES < <(grep -vE '^\s*(#|$)' "$MODULES_FILE")
fi

if [ "${#MODULES[@]}" -eq 0 ]; then
  die "no modules to test"
fi

step "extraction harness"
info "modules: ${MODULES[*]}"
info "scratch: $EXTRACT_TEST_ROOT"

failed=()
for module in "${MODULES[@]}"; do
  setup="$HERE/$module/setup.sh"
  if [ ! -x "$setup" ]; then
    warn "$module has no runnable setup.sh; skipping"
    failed+=("$module")
    continue
  fi
  if ! "$setup"; then
    failed+=("$module")
  fi
done

step "result"
if [ "${#failed[@]}" -gt 0 ]; then
  printf '   FAIL  %d module(s) did not extract: %s\n' "${#failed[@]}" "${failed[*]}" >&2
  exit 1
fi
ok "every module extracted cleanly"
