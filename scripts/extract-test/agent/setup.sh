#!/usr/bin/env bash
#
# Extraction test: internal/agent.
#
# agent is the human-in-the-loop layer: a proposal queue where a mutating action
# is hashed, shown to an operator, signed, approved or rejected, and only then
# executed. It is the widest dependency set of any module proven so far — audit,
# database and httpx — and every one of those is already proven, so the package
# lifts out.
#
# Its own tests run in the extracted module. They pin the property the whole
# approval flow rests on: the arguments hash is stable across map key order, so
# the hash the operator approved is the hash the server recomputes. None of that
# needs a server.
#
# Usage: scripts/extract-test/agent/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: agent (with its audit, database and httpx dependencies)"
WORK="$(fresh_work agent)"
extract_pkg "$WORK" agent "" "example.com/agent-extracted"
extract_pkg "$WORK" audit "audit" "example.com/agent-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/agent-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/agent-extracted/logging"
extract_pkg "$WORK" httpx "httpx" "example.com/agent-extracted/httpx"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/agent-extracted/ratelimit"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/agent" "example.com/agent-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/agent-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/agent-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/agent-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/agent-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/agent-extracted/ratelimit"

step "verify: agent stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "agent extracts cleanly"
