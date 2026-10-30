#!/usr/bin/env bash
#
# Extraction test: internal/config.
#
# config is the environment loader: it reads every FORGECRM_* variable, applies the defaults and validates the combinations that would otherwise fail silently at run time. It is the last shared piece most modules wait on.
#
# Usage: scripts/extract-test/config/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: config (with its proven dependencies)"
WORK="$(fresh_work config)"
extract_pkg "$WORK" config "" "example.com/config-extracted"
extract_pkg "$WORK" audit "audit" "example.com/config-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/config-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/config-extracted/httpx"
extract_pkg "$WORK" logging "logging" "example.com/config-extracted/logging"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/config-extracted/ratelimit"
extract_pkg "$WORK" retention "retention" "example.com/config-extracted/retention"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/config" "example.com/config-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/config-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/config-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/config-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/config-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/config-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/config-extracted/retention"

step "verify: config stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "config extracts cleanly"
