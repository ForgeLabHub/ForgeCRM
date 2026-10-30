#!/usr/bin/env bash
#
# Extraction test: internal/constructor.
#
# constructor reads recipe manifests and copies modules out of the source tree by
# path; it imports none of them, so the whole package moves. It also carries data
# as well as code — the recipes/ directory is embedded with //go:embed — so the
# copy must carry the recipe files or the build fails with "pattern recipes: no
# matching files found".
#
# Its own tests use a throwaway fixture tree rather than the real monolith, so the
# package extracts with them and proves a generation works without the monolith
# present.
#
# Usage: scripts/extract-test/constructor/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: constructor"
WORK="$(fresh_work constructor)"
extract_pkg "$WORK" constructor "" "example.com/constructor-extracted"
# constructor embeds recipes/, so the copy must carry them.
cp -r "$BACKEND_DIR/internal/constructor/recipes" "$WORK/recipes"
info "copied internal/constructor/recipes ($(find "$WORK/recipes" -type f | wc -l | tr -d ' ') files)"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/constructor" "example.com/constructor-extracted"

step "verify: constructor stands alone"
# constructor names the monolith prefix as the string it rewrites, so the textual
# check would flag its own constant and its test fixtures. The real invariant is
# that it does not *depend* on the monolith, which this checks; the subsequent
# build in a module with no replace directive proves it.
assert_no_monolith_dependency "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "constructor extracts cleanly"
