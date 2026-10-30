#!/usr/bin/env bash
#
# Extraction test: internal/views.
#
# views is the saved list-view service: named filters stored over the database
# handle. It depends on audit, database and rbac; audit reaches logging, so those
# four travel together and each is proven in its own extraction test.
#
# Its own tests run in the extracted module. They pin the properties that need no
# server: a view's entity type must be one a list endpoint honours, the filter
# document round-trips unchanged, a missing view is reported as not-found rather
# than as forbidden, and a nil service refuses to list rather than panicking.
#
# Usage: scripts/extract-test/views/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: views (with its proven dependencies)"
WORK="$(fresh_work views)"
extract_pkg "$WORK" views "" "example.com/views-extracted"
extract_pkg "$WORK" audit "audit" "example.com/views-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/views-extracted/database"
extract_pkg "$WORK" rbac "rbac" "example.com/views-extracted/rbac"
extract_pkg "$WORK" logging "logging" "example.com/views-extracted/logging"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/views" "example.com/views-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/views-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/views-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/views-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/views-extracted/logging"

step "verify: views stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "views extracts cleanly"
