#!/usr/bin/env bash
#
# Extraction test: internal/simulate.
#
# simulate is the Simulation Lab: the scenario catalog and the preview, computed on the pure analytics core.
#
# Usage: scripts/extract-test/simulate/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: simulate (with its proven dependencies)"
WORK="$(fresh_work simulate)"
extract_pkg "$WORK" simulate "" "example.com/simulate-extracted"
extract_pkg "$WORK" analytics "analytics" "example.com/simulate-extracted/analytics"
extract_pkg "$WORK" audit "audit" "example.com/simulate-extracted/audit"
extract_pkg "$WORK" crm "crm" "example.com/simulate-extracted/crm"
extract_pkg "$WORK" database "database" "example.com/simulate-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/simulate-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/simulate-extracted/tracectx"
extract_pkg "$WORK" logging "logging" "example.com/simulate-extracted/logging"
extract_pkg "$WORK" outbox "outbox" "example.com/simulate-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/simulate-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/simulate-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/simulate" "example.com/simulate-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/analytics" "example.com/simulate-extracted/analytics"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/simulate-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/simulate-extracted/crm"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/simulate-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/simulate-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/simulate-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/simulate-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/simulate-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/simulate-extracted/rbac"

step "verify: simulate stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "simulate extracts cleanly"
