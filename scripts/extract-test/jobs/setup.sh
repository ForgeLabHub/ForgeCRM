#!/usr/bin/env bash
#
# Extraction test: internal/jobs.
#
# jobs is the queue: a unit of work is enqueued (optionally inside the caller's
# transaction), claimed by a worker, completed or failed with a bounded retry
# backoff, and swept for staleness. Its only in-module dependency is
# internal/database, so it lifts out with one proven leaf.
#
# It had no in-package tests before this slice; it now has tests for the status
# and kind vocabularies, the validation ordering that rejects a job with no kind
# before the database is touched, and the retry backoff. Writing the backoff test
# found a real latent bug: the shift was clamped at 10, so the delay topped out at
# 1024s and the documented 30-minute ceiling was unreachable dead code.
#
# Usage: scripts/extract-test/jobs/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: jobs (with its database dependency)"
WORK="$(fresh_work jobs)"
extract_pkg "$WORK" jobs "" "example.com/jobs-extracted"
extract_pkg "$WORK" database "database" "example.com/jobs-extracted/database"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/jobs" "example.com/jobs-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/jobs-extracted/database"

step "verify: jobs stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "jobs extracts cleanly"
