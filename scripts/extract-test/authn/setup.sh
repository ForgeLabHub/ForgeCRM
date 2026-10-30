#!/usr/bin/env bash
#
# Extraction test: internal/authn.
#
# authn is authentication: login, lockout, sessions, MFA, password reset. It is
# the widest module proven so far — the environment loader, the schema runner, the
# retention engine and RBAC all travel with it — and every one of those is proven
# in turn.
#
# Usage: scripts/extract-test/authn/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: authn (with its proven dependencies)"
WORK="$(fresh_work authn)"
extract_pkg "$WORK" authn "" "example.com/authn-extracted"
extract_pkg "$WORK" config "config" "example.com/authn-extracted/config"
extract_pkg "$WORK" migrate "migrate" "example.com/authn-extracted/migrate"
extract_pkg "$WORK" migrations "migrations" "example.com/authn-extracted/migrations"
extract_pkg "$WORK" retention "retention" "example.com/authn-extracted/retention"
extract_pkg "$WORK" rbac "rbac" "example.com/authn-extracted/rbac"
extract_pkg "$WORK" secrets "secrets" "example.com/authn-extracted/secrets"
extract_pkg "$WORK" audit "audit" "example.com/authn-extracted/audit"
extract_pkg "$WORK" database "database" "example.com/authn-extracted/database"
extract_pkg "$WORK" httpx "httpx" "example.com/authn-extracted/httpx"
extract_pkg "$WORK" logging "logging" "example.com/authn-extracted/logging"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/authn-extracted/ratelimit"
install_module_files "$WORK" "$HERE"
# The migrations package embeds its SQL, so the copy must carry it.
extract_sql "$WORK" migrations "migrations"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/authn" "example.com/authn-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/config" "example.com/authn-extracted/config"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/migrate" "example.com/authn-extracted/migrate"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/migrations" "example.com/authn-extracted/migrations"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/authn-extracted/retention"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/rbac" "example.com/authn-extracted/rbac"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/secrets" "example.com/authn-extracted/secrets"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/authn-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/authn-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/authn-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/authn-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/authn-extracted/ratelimit"

step "verify: authn stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "authn extracts cleanly"
