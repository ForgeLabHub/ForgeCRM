#!/usr/bin/env bash
#
# Extraction test: internal/secrets.
#
# secrets is password hashing, token generation and the AES-GCM keyring for stored
# integration credentials. Its only dependency is golang.org/x/crypto, so it should
# extract on its own. Before this slice it shipped no tests at all, which is why
# the module inventory marked it ALMOST; the tests now live with the package and
# run here in the extracted copy.
#
# Usage: scripts/extract-test/secrets/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: secrets"
WORK="$(fresh_work secrets)"
extract_pkg "$WORK" secrets "" "example.com/secrets-extracted"
install_module_files "$WORK" "$HERE"
# The smoke program imports the package by its monolith path; re-point it at the
# extracted copy.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/secrets" "example.com/secrets-extracted"

step "verify: secrets stands alone"
assert_no_monolith_import "$WORK"
# The package's only third-party dependency is x/crypto. Assert it did not grow a
# dependency on the application layer without anyone noticing.
if grep -qE 'geff1778/forgecrm' "$WORK/go.mod"; then
  die "secrets has acquired a dependency on the ForgeCRM monolith"
fi
ok "go.mod declares no ForgeCRM dependency"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "secrets extracts cleanly"
