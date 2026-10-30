#!/usr/bin/env bash
#
# Extraction test: internal/migrate.
#
# migrate is the runner over the embedded migrations: it hands the embedded
# filesystem to golang-migrate. Its only in-module dependency is the migrations
# leaf, so it lifts out with it.
#
# Usage: scripts/extract-test/migrate/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: migrate (with the embedded migrations it runs)"
WORK="$(fresh_work migrate)"
extract_pkg "$WORK" migrate "" "example.com/migrate-extracted"
extract_pkg "$WORK" migrations "migrations" "example.com/migrate-extracted/migrations"
install_module_files "$WORK" "$HERE"
# The migrations package embeds its SQL, so the copy must carry it.
extract_sql "$WORK" migrations "migrations"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/migrate" "example.com/migrate-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/migrations" "example.com/migrate-extracted/migrations"

step "verify: migrate stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "migrate extracts cleanly"
