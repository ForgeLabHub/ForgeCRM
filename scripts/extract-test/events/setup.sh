#!/usr/bin/env bash
#
# Extraction test: internal/events.
#
# events is the inbound webhook gateway: signature verification, the timestamp window, the payload cap and the dead-letter path.
#
# Usage: scripts/extract-test/events/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: events (with its proven dependencies)"
WORK="$(fresh_work events)"
extract_pkg "$WORK" events "" "example.com/events-extracted"
extract_pkg "$WORK" audit "audit" "example.com/events-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/events-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/events-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/events-extracted/tracectx"
extract_pkg "$WORK" integrations "integrations" "example.com/events-extracted/integrations"
extract_pkg "$WORK" logging "logging" "example.com/events-extracted/logging"
extract_pkg "$WORK" netguard "netguard" "example.com/events-extracted/netguard"
extract_pkg "$WORK" outbox "outbox" "example.com/events-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/events-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/events-extracted/rbac"
extract_pkg "$WORK" secrets "secrets" "example.com/events-extracted/secrets"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/events" "example.com/events-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/events-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/events-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/events-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/integrations" "example.com/events-extracted/integrations"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/events-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/netguard" "example.com/events-extracted/netguard"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/events-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/events-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/events-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/secrets" "example.com/events-extracted/secrets"

step "verify: events stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "events extracts cleanly"
