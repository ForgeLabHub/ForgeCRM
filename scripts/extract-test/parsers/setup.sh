#!/usr/bin/env bash
#
# Extraction test: internal/importx/parsers.
#
# parsers turns CSV/TSV/JSON/JSONL/XLSX/XML into the common row model the Import
# Center maps to CRM fields. It has no in-module dependency, so the whole package
# moves; the only external dependency is the spreadsheet library.
#
# This module was extracted because it had no tests of its own and the extraction
# exercise is what found two real bugs in it: XLSX parsing failed the UTF-8 check
# applied to every format, and the XML parser emitted a record's attributes and
# its nested elements as separate rows. Both are fixed, and the package's tests
# now pin them.
#
# Usage: scripts/extract-test/parsers/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: importx/parsers"
WORK="$(fresh_work parsers)"
extract_pkg "$WORK" importx/parsers "" "example.com/parsers-extracted"
install_module_files "$WORK" "$HERE"
# Re-point the smoke program, which imports the package by its monolith path.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/importx/parsers" "example.com/parsers-extracted"

step "verify: parsers stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "importx/parsers extracts cleanly"
