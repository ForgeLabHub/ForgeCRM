#!/usr/bin/env bash
#
# Extraction test: internal/ai.
#
# ai is the contextual AI layer: the prompt assembly, the tool risk tiers and the MCP protocol. The context is labelled as data so a record cannot forge its way into the instructions.
#
# Usage: scripts/extract-test/ai/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: ai (with its proven dependencies)"
WORK="$(fresh_work ai)"
extract_pkg "$WORK" ai "" "example.com/ai-extracted"
extract_pkg "$WORK" agent "agent" "example.com/ai-extracted/agent"
extract_pkg "$WORK" audit "audit" "example.com/ai-extracted/audit"
extract_pkg "$WORK" config "config" "example.com/ai-extracted/config"
extract_pkg "$WORK" crm "crm" "example.com/ai-extracted/crm"
extract_pkg "$WORK" database "database" "example.com/ai-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/ai-extracted/httpx"
extract_pkg "$WORK" logging "logging" "example.com/ai-extracted/logging"
extract_pkg "$WORK" outbox "outbox" "example.com/ai-extracted/outbox"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/ai-extracted/ratelimit"
extract_pkg "$WORK" rbac "rbac" "example.com/ai-extracted/rbac"
extract_pkg "$WORK" retention "retention" "example.com/ai-extracted/retention"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ai" "example.com/ai-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/agent" "example.com/ai-extracted/agent"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/ai-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/config" "example.com/ai-extracted/config"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/ai-extracted/crm"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/ai-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/ai-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/ai-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/ai-extracted/outbox"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/ai-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/ai-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/ai-extracted/retention"

step "verify: ai stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "ai extracts cleanly"
