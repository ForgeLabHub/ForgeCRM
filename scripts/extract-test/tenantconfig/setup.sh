#!/usr/bin/env bash
#
# Extraction test: internal/tenantconfig.
#
# tenantconfig holds an organization's own configuration: a closed, versioned
# catalog of settings and the overrides stored against it. It depends on audit,
# database and rbac; audit reaches logging, so those four travel together and each
# is proven in its own extraction test.
#
# The arrow of dependency is the thing this proves. The domain packages that read a
# default — crm and workflow — spell the setting key as their own constant and
# declare a narrow OrgDefaults port, so they import nothing from this package. That
# direction is what lets tenantconfig lift out without dragging the CRM domain with
# it, and it is why the cross-module key-spelling check lives in internal/archtest
# rather than in this package's tests.
#
# Its own tests run in the extracted module. They pin the properties that need no
# server: the catalog is closed and well-formed, each kind's validation accepts and
# refuses the right shapes, a refused value names its key, and key lookup is
# case-sensitive so a rename cannot half-apply.
#
# Usage: scripts/extract-test/tenantconfig/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: tenantconfig (with its proven dependencies)"
WORK="$(fresh_work tenantconfig)"
extract_pkg "$WORK" tenantconfig "" "example.com/tenantconfig-extracted"
extract_pkg "$WORK" audit "audit" "example.com/tenantconfig-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/tenantconfig-extracted/database"
extract_pkg "$WORK" logging "logging" "example.com/tenantconfig-extracted/logging"
extract_pkg "$WORK" rbac "rbac" "example.com/tenantconfig-extracted/rbac"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/tenantconfig" "example.com/tenantconfig-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/tenantconfig-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/tenantconfig-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/tenantconfig-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/tenantconfig-extracted/rbac"

step "verify: tenantconfig stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "tenantconfig extracts cleanly"
