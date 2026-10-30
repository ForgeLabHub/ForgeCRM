#!/usr/bin/env bash
#
# Extraction test: internal/retention.
#
# retention is the data-retention engine: a fixed catalog of policies, each with a
# window and a protection flag, run either as a dry run (the default) or as a
# delete. It has two in-module dependencies — internal/audit and internal/database
# — both already proven, so the package lifts out.
#
# Its own tests run in the extracted module. They pin the safety properties that
# matter most: every policy is well formed, protected policies keep a long minimum
# window, the default posture is dry-run, and a nil or disabled runner refuses to
# run rather than deleting. None of that needs a server.
#
# Usage: scripts/extract-test/retention/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: retention (with its audit and database dependencies)"
WORK="$(fresh_work retention)"
extract_pkg "$WORK" retention "" "example.com/retention-extracted"
extract_pkg "$WORK" audit "audit" "example.com/retention-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/retention-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/retention-extracted/logging"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/retention-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/retention-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/retention-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/retention-extracted/logging"

step "verify: retention stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "retention extracts cleanly"
