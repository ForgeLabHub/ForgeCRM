#!/usr/bin/env bash
#
# Extraction test: internal/audit.
#
# audit is the append-only action trail and the security-event stream. It has two
# in-module dependencies — internal/database and internal/logging — both already
# proven standalone, so the package lifts out.
#
# Its schema (audit_log, security_events) lives in migration 0001 and is not part
# of the package, which is the one thing a real extraction would have to carry
# with it. That is noted in the docs rather than hidden; what this proves is that
# the Go package itself has no coupling to the application.
#
# Its own tests run in the extracted module. They cover the pure half of the
# package — the action vocabulary, the validation ordering that keeps an
# unattributable row out of the trail, and the marshalling helpers — none of which
# needs a database.
#
# Usage: scripts/extract-test/audit/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: audit (with its database and logging dependencies)"
WORK="$(fresh_work audit)"
extract_pkg "$WORK" audit "" "example.com/audit-extracted"
extract_pkg "$WORK" database "database" "example.com/audit-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/audit-extracted/logging"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/audit-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/audit-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/audit-extracted/logging"

step "verify: audit stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "audit extracts cleanly"
