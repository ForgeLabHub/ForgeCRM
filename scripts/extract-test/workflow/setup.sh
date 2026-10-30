#!/usr/bin/env bash
#
# Extraction test: internal/workflow.
#
# workflow is the automation engine: the step document, its validation, the cron parser, the template catalog and the export/import round-trip. Its trigger events and its RBAC mapping are checked, not assumed.
#
# Usage: scripts/extract-test/workflow/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: workflow (with its proven dependencies)"
WORK="$(fresh_work workflow)"
extract_pkg "$WORK" workflow "" "example.com/workflow-extracted"
extract_pkg "$WORK" audit "audit" "example.com/workflow-extracted/audit"
extract_pkg "$WORK" crm "crm" "example.com/workflow-extracted/crm"
extract_pkg "$WORK" database "database" "example.com/workflow-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/workflow-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/workflow-extracted/tracectx"
extract_pkg "$WORK" logging "logging" "example.com/workflow-extracted/logging"
extract_pkg "$WORK" outbox "outbox" "example.com/workflow-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/workflow-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/workflow-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/workflow" "example.com/workflow-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/workflow-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/workflow-extracted/crm"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/workflow-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/workflow-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/workflow-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/workflow-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/workflow-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/workflow-extracted/rbac"

step "verify: workflow stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "workflow extracts cleanly"
