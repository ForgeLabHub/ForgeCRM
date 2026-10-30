#!/usr/bin/env bash
#
# Extraction test: internal/storage.
#
# storage is the application layer over an object store: it owns the document rows,
# the entity attachment rules, the size and content-type limits, and the object
# key. It re-exports the provider contract from internal/objectstore, so the store
# itself (which depends only on netguard) travels with it. Its in-module
# dependencies — audit, database, httpx, objectstore (and objectstore's netguard),
# ratelimit, rbac — are all proven, so the package lifts out.
#
# Its own tests run in the extracted module. They pin the tenant-scoping of an
# object key, the safe-extension allow-list, the default refusal of executables,
# the memory-store round trip and the S3 config guards — none of which needs a
# server or a real bucket.
#
# Usage: scripts/extract-test/storage/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: storage (with its audit, database, httpx, objectstore, ratelimit and rbac dependencies)"
WORK="$(fresh_work storage)"
extract_pkg "$WORK" storage "" "example.com/storage-extracted"
extract_pkg "$WORK" audit "audit" "example.com/storage-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/storage-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/storage-extracted/logging"
extract_pkg "$WORK" httpx "httpx" "example.com/storage-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/storage-extracted/tracectx"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/storage-extracted/ratelimit"
extract_pkg "$WORK" objectstore "objectstore" "example.com/storage-extracted/objectstore"
extract_pkg "$WORK" netguard "netguard" "example.com/storage-extracted/netguard"
extract_pkg "$WORK" rbac "rbac" "example.com/storage-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/storage" "example.com/storage-extracted"
for m in audit database logging httpx ratelimit objectstore netguard rbac; do
  rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/$m" "example.com/storage-extracted/$m"
done

step "verify: storage stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "storage extracts cleanly"
