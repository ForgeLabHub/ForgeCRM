#!/usr/bin/env bash
#
# Extraction test: internal/rbac.
#
# rbac is the permission catalog and the request principal: the vocabulary of
# what a user may do, the checks every handler makes, and the DB-backed service
# that resolves a user's effective permissions and roles. It has one in-module
# dependency — internal/database — which is extracted alongside it, and its own
# tests run in the extracted module.
#
# The package is extracted whole rather than split, because the split the docs
# once proposed (a pure catalog and Principal apart from the service) is not what
# the code does: the catalog, the principal and the service share the file and the
# permission type, and the whole package already extracts with only its one
# dependency. A split would be a change to the code, not to the boundary, and is
# not needed to prove the boundary holds.
#
# Usage: scripts/extract-test/rbac/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: rbac (with its database dependency)"
WORK="$(fresh_work rbac)"
extract_pkg "$WORK" rbac "" "example.com/rbac-extracted"
extract_pkg "$WORK" database "database" "example.com/rbac-extracted/database"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/rbac-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/rbac-extracted/database"

step "verify: rbac stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "rbac extracts cleanly"
