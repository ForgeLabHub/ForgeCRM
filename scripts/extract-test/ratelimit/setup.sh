#!/usr/bin/env bash
#
# Extraction test: internal/ratelimit.
#
# ratelimit owns the fixed-window limiter and its two stores. It is extracted with
# its only third-party dependency, the Redis client, and nothing from ForgeCRM.
# The package's own tests — including the concurrency tests — run in the extracted
# module, so the limiter is proven to work without the rest of the system.
#
# Usage: scripts/extract-test/ratelimit/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: ratelimit"
WORK="$(fresh_work ratelimit)"
extract_pkg "$WORK" ratelimit "" "example.com/ratelimit-extracted"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/ratelimit-extracted"

step "verify: ratelimit stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "ratelimit extracts cleanly"
