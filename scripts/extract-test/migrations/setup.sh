#!/usr/bin/env bash
#
# Extraction test: internal/migrations.
#
# migrations is the embedded SQL: a leaf, but one that carries data as well as
# code. The //go:embed directive names *.sql, so the extraction must copy the SQL
# files too — a Go-only copy fails to build. That is exactly the kind of hidden
# coupling the harness exists to catch.
#
# Usage: scripts/extract-test/migrations/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: migrations"
WORK="$(fresh_work migrations)"
extract_pkg "$WORK" migrations "" "example.com/migrations-extracted"
install_module_files "$WORK" "$HERE"
# The embedded SQL travels with the package; without it the build fails.
extract_sql "$WORK" migrations ""
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/migrations" "example.com/migrations-extracted"

step "verify: migrations stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "migrations extracts cleanly"
