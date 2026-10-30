#!/usr/bin/env bash
#
# Extraction test: internal/risk.
#
# risk is the Risk scan: the thresholds, the scan over the CRM read and the signal reconciliation.
#
# Usage: scripts/extract-test/risk/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: risk (with its proven dependencies)"
WORK="$(fresh_work risk)"
extract_pkg "$WORK" risk "" "example.com/risk-extracted"
extract_pkg "$WORK" analytics "analytics" "example.com/risk-extracted/analytics"
extract_pkg "$WORK" audit "audit" "example.com/risk-extracted/audit"
extract_pkg "$WORK" crm "crm" "example.com/risk-extracted/crm"
extract_pkg "$WORK" database "database" "example.com/risk-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/risk-extracted/httpx"
extract_pkg "$WORK" logging "logging" "example.com/risk-extracted/logging"
extract_pkg "$WORK" outbox "outbox" "example.com/risk-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/risk-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/risk-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/risk" "example.com/risk-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/analytics" "example.com/risk-extracted/analytics"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/risk-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/risk-extracted/crm"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/risk-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/risk-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/risk-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/risk-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/risk-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/risk-extracted/rbac"

step "verify: risk stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "risk extracts cleanly"
