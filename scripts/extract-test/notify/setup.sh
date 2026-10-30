#!/usr/bin/env bash
#
# Extraction test: internal/notify.
#
# notify is the outbound notification transports (email and Telegram). A disabled transport reports off rather than failing.
#
# Usage: scripts/extract-test/notify/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: notify (with its proven dependencies)"
WORK="$(fresh_work notify)"
extract_pkg "$WORK" notify "" "example.com/notify-extracted"
extract_pkg "$WORK" audit "audit" "example.com/notify-extracted/audit"
extract_pkg "$WORK" config "config" "example.com/notify-extracted/config"
extract_pkg "$WORK" database "database" "example.com/notify-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/notify-extracted/httpx"
extract_pkg "$WORK" logging "logging" "example.com/notify-extracted/logging"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/notify-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/notify-extracted/rbac"
extract_pkg "$WORK" retention "retention" "example.com/notify-extracted/retention"
extract_pkg "$WORK" secrets "secrets" "example.com/notify-extracted/secrets"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/notify" "example.com/notify-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/notify-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/config" "example.com/notify-extracted/config"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/notify-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/notify-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/notify-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/notify-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/notify-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/notify-extracted/retention"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/secrets" "example.com/notify-extracted/secrets"

step "verify: notify stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "notify extracts cleanly"
