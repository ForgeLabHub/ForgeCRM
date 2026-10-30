#!/usr/bin/env bash
#
# Extraction test: internal/integrations.
#
# integrations is the Integration Hub: adapter lifecycle, the SSRF guard (aliased
# from netguard), the encrypted secret handoff and the run log. Its in-module
# dependencies — audit, database, httpx, netguard, ratelimit, rbac, secrets — are
# all proven, so the package lifts out.
#
# The extracted module has no in-package test file, so the harness drives the
# public surface through a smoke program: the SSRF guard's decisions on addresses
# and URLs, and the constructor's refusal to build an incomplete hub. A connection
# to a real endpoint is not attempted, which is the point — the guard's job is to
# refuse before the dial.
#
# Usage: scripts/extract-test/integrations/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: integrations (with its audit, database, httpx, netguard, ratelimit, rbac and secrets dependencies)"
WORK="$(fresh_work integrations)"
extract_pkg "$WORK" integrations "" "example.com/integrations-extracted"
extract_pkg "$WORK" audit "audit" "example.com/integrations-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/integrations-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/integrations-extracted/logging"
extract_pkg "$WORK" httpx "httpx" "example.com/integrations-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/integrations-extracted/tracectx"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/integrations-extracted/ratelimit"
extract_pkg "$WORK" netguard "netguard" "example.com/integrations-extracted/netguard"
extract_pkg "$WORK" rbac "rbac" "example.com/integrations-extracted/rbac"
extract_pkg "$WORK" secrets "secrets" "example.com/integrations-extracted/secrets"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/integrations" "example.com/integrations-extracted"
for m in audit database logging httpx ratelimit netguard rbac secrets; do
  rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/$m" "example.com/integrations-extracted/$m"
done

step "verify: integrations stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "integrations extracts cleanly"
