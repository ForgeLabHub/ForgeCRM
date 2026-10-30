#!/usr/bin/env bash
#
# Extraction test: internal/database.
#
# database owns the PostgreSQL connection pool and the transaction helper every
# repository is written against. It has no in-module dependency at all, so the
# whole package moves; the only external dependency is the pgx driver.
#
# Its own tests run in the extracted module. They deliberately need no server:
# the pool-size guard is enforced before a connection is attempted, so the check
# that matters here — that an over-large pool is refused at startup — is proven
# without a database.
#
# Usage: scripts/extract-test/database/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: database"
WORK="$(fresh_work database)"
extract_pkg "$WORK" database "" "example.com/database-extracted"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/database-extracted"

step "verify: database stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "database extracts cleanly"
