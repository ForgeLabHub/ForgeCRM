#!/usr/bin/env bash
#
# Extraction test: internal/oidc.
#
# oidc is single sign-on: the authorization-code flow and the provider registry.
# It depends on audit, config, database and secrets; config in turn reaches httpx,
# logging, ratelimit and retention, so those travel too. Each of them is proven in
# its own extraction test.
#
# Its own tests run in the extracted module. They pin the safety properties that
# need no server: a provider's public shape never carries the client secret, an
# open-redirect attempt in `return_to` collapses to the root, only an allowed email
# domain passes, the default scope set always includes `openid`, and a disabled
# service reports off rather than panicking.
#
# Usage: scripts/extract-test/oidc/setup.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib.sh
. "$HERE/../lib.sh"

step "extract: oidc (with its proven dependencies)"
WORK="$(fresh_work oidc)"
extract_pkg "$WORK" oidc "" "example.com/oidc-extracted"
extract_pkg "$WORK" audit "audit" "example.com/oidc-extracted/audit"
extract_pkg "$WORK" config "config" "example.com/oidc-extracted/config"
extract_pkg "$WORK" database "database" "example.com/oidc-extracted/database"
extract_pkg "$WORK" secrets "secrets" "example.com/oidc-extracted/secrets"
extract_pkg "$WORK" logging "logging" "example.com/oidc-extracted/logging"
extract_pkg "$WORK" httpx "httpx" "example.com/oidc-extracted/httpx"
extract_pkg "$WORK" tracectx "tracectx" "example.com/oidc-extracted/tracectx"
extract_pkg "$WORK" ratelimit "ratelimit" "example.com/oidc-extracted/ratelimit"
extract_pkg "$WORK" retention "retention" "example.com/oidc-extracted/retention"
install_module_files "$WORK" "$HERE"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/oidc" "example.com/oidc-extracted"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/audit" "example.com/oidc-extracted/audit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/config" "example.com/oidc-extracted/config"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/database" "example.com/oidc-extracted/database"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/secrets" "example.com/oidc-extracted/secrets"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/logging" "example.com/oidc-extracted/logging"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/httpx" "example.com/oidc-extracted/httpx"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/ratelimit" "example.com/oidc-extracted/ratelimit"
rewrite_import "$WORK" "github.com/geff1778/forgecrm/internal/retention" "example.com/oidc-extracted/retention"

step "verify: oidc stands alone"
assert_no_monolith_import "$WORK"
run_build "$WORK"
run_tests "$WORK"
run_smoke "$WORK"

cleanup_work "$WORK"
ok "oidc extracts cleanly"
