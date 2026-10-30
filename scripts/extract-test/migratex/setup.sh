#!/usr/bin/env bash
#
# Extraction test: internal/migratex.
#
# migratex is the cross-database migration service: the source adapters and the reconciliation, with the implemented/unimplemented source kinds named explicitly rather than guessed.
#
# Usage: scripts/extract-test/migratex/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: migratex (with its proven dependencies)"
WORK="$(fresh_work migratex)"
extract_pkg "$WORK" migratex "" "example.com/migratex-extracted"
extract_pkg "$WORK" audit "audit" "example.com/migratex-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/migratex-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/migratex-extracted/httpx"
extract_pkg "$WORK" integrations "integrations" "example.com/migratex-extracted/integrations"
extract_pkg "$WORK" logging "logging" "example.com/migratex-extracted/logging"
extract_pkg "$WORK" netguard "netguard" "example.com/migratex-extracted/netguard"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/migratex-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/migratex-extracted/rbac"
extract_pkg "$WORK" secrets "secrets" "example.com/migratex-extracted/secrets"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/migratex" "example.com/migratex-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/migratex-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/migratex-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/migratex-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/integrations" "example.com/migratex-extracted/integrations"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/migratex-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/netguard" "example.com/migratex-extracted/netguard"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/migratex-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/migratex-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/secrets" "example.com/migratex-extracted/secrets"

step "verify: migratex stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "migratex extracts cleanly"
