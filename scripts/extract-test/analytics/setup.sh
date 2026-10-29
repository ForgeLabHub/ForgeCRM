#!/usr/bin/env bash
#
# Extraction test: internal/analytics.
#
# analytics is a pure computation package — it holds the money, ratio and scenario
# maths and imports nothing from ForgeCRM. The extraction should therefore need no
# third-party module at all. If it starts to, this test fails and the new
# dependency is visible.
#
# Usage: scripts/extract-test/analytics/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: analytics"
WORK="$(fresh_work analytics)"
extract_pkg "$WORK" analytics "" "example.com/analytics-extracted"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/analytics" "example.com/analytics-extracted"

step "verify: analytics stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "analytics extracts cleanly"
