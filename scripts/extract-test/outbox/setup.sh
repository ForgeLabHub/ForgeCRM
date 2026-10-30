#!/usr/bin/env bash
#
# Extraction test: internal/outbox.
#
# outbox is the transactional outbox: events are written in the same transaction
# as the change that caused them, then dispatched by the worker. It has two
# in-module dependencies — internal/database and internal/logging — both already
# proven, so it lifts out.
#
# Its own test runs in the extracted module. The test pins the event vocabulary,
# which is the contract a webhook subscription is built from and the exact shape
# of the historical bug where bare names were accepted and then matched nothing.
#
# Usage: scripts/extract-test/outbox/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: outbox (with its database and logging dependencies)"
WORK="$(fresh_work outbox)"
extract_pkg "$WORK" outbox "" "example.com/outbox-extracted"
extract_pkg "$WORK" database "database" "example.com/outbox-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/outbox-extracted/logging"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/outbox" "example.com/outbox-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/outbox-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/outbox-extracted/logging"

step "verify: outbox stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "outbox extracts cleanly"
