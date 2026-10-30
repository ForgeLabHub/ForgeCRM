#!/usr/bin/env bash
#
# Extraction test: internal/forecast.
#
# forecast is the revenue forecast: the method catalog and the accuracy that says whether a method fits the series.
#
# Usage: scripts/extract-test/forecast/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: forecast (with its proven dependencies)"
WORK="$(fresh_work forecast)"
extract_pkg "$WORK" forecast "" "example.com/forecast-extracted"
extract_pkg "$WORK" analytics "analytics" "example.com/forecast-extracted/analytics"
extract_pkg "$WORK" audit "audit" "example.com/forecast-extracted/audit"
extract_pkg "$WORK" crm "crm" "example.com/forecast-extracted/crm"
extract_pkg "$WORK" database "database" "example.com/forecast-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/forecast-extracted/httpx"
extract_pkg "$WORK" logging "logging" "example.com/forecast-extracted/logging"
extract_pkg "$WORK" outbox "outbox" "example.com/forecast-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/forecast-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/forecast-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/forecast" "example.com/forecast-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/analytics" "example.com/forecast-extracted/analytics"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/forecast-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/forecast-extracted/crm"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/forecast-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/forecast-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/forecast-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/forecast-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/forecast-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/forecast-extracted/rbac"

step "verify: forecast stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "forecast extracts cleanly"
