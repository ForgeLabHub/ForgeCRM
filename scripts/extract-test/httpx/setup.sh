#!/usr/bin/env bash
#
# Extraction test: internal/httpx.
#
# httpx is the HTTP layer every handler is written against: the error envelope,
# JSON writers, pagination and sort parsing, the access-log and recovery
# middleware, the rate-limit middleware and the client-address resolver. It has
# exactly two in-module dependencies — internal/logging and internal/ratelimit —
# both of which are already proven standalone, so the whole layer should lift out
# without reaching into the application. If it ever imports the database, RBAC or
# a domain service, the copy will fail to compile here.
#
# The package's own tests run in the extracted module, including the client-address
# trust tests, so the extraction proves behaviour and not just compilation.
#
# Usage: scripts/extract-test/httpx/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: httpx (with its logging and ratelimit dependencies)"
WORK="$(fresh_work httpx)"
extract_pkg "$WORK" httpx "" "example.com/httpx-extracted"
extract_pkg "$WORK" tracectx "tracectx" "example.com/httpx-extracted/tracectx"
# The two dependencies are copied into subdirectories; extract_pkg rewrites the
# monolith import path across the whole working tree, so the httpx files that
# reference them are re-pointed at the copies too.
extract_pkg "$WORK" logging "logging" "example.com/httpx-extracted/logging"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/httpx-extracted/ratelimit"
install_module_files "$WORK" "$HERE"
# Re-point the smoke program, which imports the packages by their monolith paths.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/httpx-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/httpx-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/httpx-extracted/logging"

step "verify: httpx stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "httpx extracts cleanly"
