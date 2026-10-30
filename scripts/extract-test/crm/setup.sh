#!/usr/bin/env bash
#
# Extraction test: internal/crm.
#
# crm is the domain service: accounts, contacts, leads, deals, tasks, tickets,
# activities, the pipeline read, the search and the baseline analytics. It is the
# core of the product. Its in-module dependencies — audit, database, httpx,
# outbox, ratelimit, rbac — are all proven, so the package lifts out.
#
# It had no in-package tests before this slice; it now has the first, covering the
# validators that run before any database access and the JSONMap column codec.
# Those tests run in the extracted module.
#
# Usage: scripts/extract-test/crm/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: crm (with its audit, database, httpx, outbox, ratelimit and rbac dependencies)"
WORK="$(fresh_work crm)"
extract_pkg "$WORK" crm "" "example.com/crm-extracted"
extract_pkg "$WORK" audit "audit" "example.com/crm-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/crm-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/crm-extracted/logging"
extract_pkg "$WORK" httpx "httpx" "example.com/crm-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/crm-extracted/tracectx"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/crm-extracted/ratelimit"
extract_pkg "$WORK" outbox "outbox" "example.com/crm-extracted/outbox"
extract_pkg "$WORK" rbac "rbac" "example.com/crm-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/crm" "example.com/crm-extracted"
for m in audit database logging httpx ratelimit outbox rbac; do
  rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/$m" "example.com/crm-extracted/$m"
done

step "verify: crm stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "crm extracts cleanly"
