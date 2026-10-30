#!/usr/bin/env bash
#
# Extraction test: internal/importx.
#
# importx is the Import Center: format detection, the field catalog and the parser leaves it stands on.
#
# Usage: scripts/extract-test/importx/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: importx (with its proven dependencies)"
WORK="$(fresh_work importx)"
extract_pkg "$WORK" importx "" "example.com/importx-extracted"
extract_pkg "$WORK" audit "audit" "example.com/importx-extracted/audit"
extract_pkg "$WORK" config "config" "example.com/importx-extracted/config"
extract_pkg "$WORK" crm "crm" "example.com/importx-extracted/crm"
extract_pkg "$WORK" database "database" "example.com/importx-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/importx-extracted/httpx"
extract_pkg "$WORK" jobs "jobs" "example.com/importx-extracted/jobs"
extract_pkg "$WORK" logging "logging" "example.com/importx-extracted/logging"
extract_pkg "$WORK" outbox "outbox" "example.com/importx-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/importx-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/importx-extracted/rbac"
extract_pkg "$WORK" retention "retention" "example.com/importx-extracted/retention"
extract_pkg "$WORK" "importx/parsers" "parsers" "example.com/importx-extracted/parsers"
extract_pkg "$WORK" "importx/safe" "safe" "example.com/importx-extracted/safe"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/importx" "example.com/importx-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/importx-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/config" "example.com/importx-extracted/config"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/importx-extracted/crm"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/importx-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/importx-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/jobs" "example.com/importx-extracted/jobs"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/importx-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/importx-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/importx-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/importx-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/importx-extracted/retention"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/importx/parsers" "example.com/importx-extracted/parsers"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/importx/safe" "example.com/importx-extracted/safe"

step "verify: importx stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "importx extracts cleanly"
