#!/usr/bin/env bash
#
# Extraction test: internal/objectstore.
#
# objectstore is the layer this slice carved out of the storage package. It has
# exactly one in-module dependency, internal/netguard, which is copied into a
# subdirectory and re-pointed to the extracted module. If objectstore ever reaches
# back into the application layer, the copy will fail to compile here — which is
# the property the slice exists to guarantee.
#
# The package's own contract tests run in the extracted module, including the S3
# contract against an in-process S3 wire-protocol server. The extraction is proven
# to store and retrieve objects on its own.
#
# Usage: scripts/extract-test/objectstore/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: objectstore (with its netguard dependency)"
WORK="$(fresh_work objectstore)"
extract_pkg "$WORK" objectstore "" "example.com/objectstore-extracted"
# netguard is copied second; extract_pkg rewrites its monolith import path across
# the whole working tree, so the objectstore files that reference it are re-pointed
# at the copy too.
extract_pkg "$WORK" netguard "netguard" "example.com/objectstore-extracted/netguard"
install_module_files "$WORK" "$HERE"
# Re-point the smoke program, which imports the package by its monolith path.
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/objectstore" "example.com/objectstore-extracted"

step "verify: objectstore stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "objectstore extracts cleanly"
