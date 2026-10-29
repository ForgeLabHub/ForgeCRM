#!/usr/bin/env bash
#
# Extraction test: internal/importx/safe.
#
# safe is the untrusted-archive extractor: it detects a container from its magic
# bytes and refuses a path traversal, a symlink, an oversized entry, a zip bomb
# and excessive nesting. It is the single place in the codebase that writes bytes
# chosen by an uploader to disk, so it is exactly the kind of code that must not
# depend on the application it happens to live inside.
#
# It has no in-module dependency at all — the whole package moves — so the
# extraction is the strongest form of the claim: the security behaviour is proven
# in a clean module, against the package's own tests.
#
# Usage: scripts/extract-test/safe/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: importx/safe"
WORK="$(fresh_work safe)"
extract_pkg "$WORK" importx/safe "" "example.com/safe-extracted"
install_module_files "$WORK" "$HERE"
# Re-point the smoke program, which imports the package by its monolith path.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/importx/safe" "example.com/safe-extracted"

step "verify: safe stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "importx/safe extracts cleanly"
