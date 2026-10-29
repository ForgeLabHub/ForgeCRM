#!/usr/bin/env bash
#
# Extraction test: internal/netguard.
#
# netguard is the SSRF guard. It must depend on nothing outside the standard
# library — that is the whole point of it being a leaf, and what lets the object
# store use the guard without importing the Integration Hub.
#
# Usage: scripts/extract-test/netguard/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: netguard"
WORK="$(fresh_work netguard)"
extract_pkg "$WORK" netguard "" "example.com/netguard-extracted"
install_module_files "$WORK" "$HERE"
# The smoke program lives in the harness and imports the package by its monolith
# path; re-point it at the extracted copy.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/netguard" "example.com/netguard-extracted"

step "verify: netguard stands alone"
assert_no_monolith_import "$WORK"
# The go.mod is stdlib-only. A new third-party require would make the file
# disagree with what `go mod tidy` would produce, so check that directly.
if grep -qE '^\s*require' "$WORK/go.mod"; then
  die "netguard has acquired a third-party dependency; it must stay standard-library only"
fi
ok "go.mod declares no third-party dependency"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "netguard extracts cleanly"
