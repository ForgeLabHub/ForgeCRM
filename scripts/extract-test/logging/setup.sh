#!/usr/bin/env bash
#
# Extraction test: internal/logging.
#
# logging is structured logging with central redaction of secrets. It depends on
# nothing beyond the standard library, so it should extract on its own. Before this
# slice it shipped no tests at all, which is why the module inventory marked it
# ALMOST; the tests now live with the package and run here in the extracted copy.
#
# Usage: scripts/extract-test/logging/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: logging"
WORK="$(fresh_work logging)"
extract_pkg "$WORK" logging "" "example.com/logging-extracted"
install_module_files "$WORK" "$HERE"
# The smoke program imports the package by its monolith path; re-point it at the
# extracted copy.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/logging-extracted"

step "verify: logging stands alone"
assert_no_monolith_import "$WORK"
# logging is a leaf: a third-party require would mean it is no longer the
# dependency-free foundation the rest of the code logs through.
if grep -qE '^\s*require' "$WORK/go.mod"; then
  die "logging has acquired a third-party dependency; it must stay standard-library only"
fi
ok "go.mod declares no third-party dependency"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "logging extracts cleanly"
