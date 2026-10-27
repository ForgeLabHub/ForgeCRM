#!/usr/bin/env bash
#
# End-to-end smoke test against a running ForgeCRM instance.
#
#   ./scripts/smoke-test.sh [base-url]
#
# Verifies that the process serves traffic, that the database is migrated, that
# unauthenticated access is refused and that authentication works. It never
# mutates data beyond signing in and out.

set -uo pipefail

base="${1:-http://localhost:8080}"
cookie_jar="$(mktemp)"
trap 'rm -f "${cookie_jar}"' EXIT

pass=0
fail=0

check() {
  local name="$1" expected="$2" actual="$3"
  if [[ "${actual}" == "${expected}" ]]; then
    printf '  \033[32mok\033[0m    %s (%s)\n' "${name}" "${actual}"
    pass=$((pass + 1))
  else
    printf '  \033[31mfail\033[0m  %s (got %s, want %s)\n' "${name}" "${actual}" "${expected}"
    fail=$((fail + 1))
  fi
}

code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

echo "ForgeCRM smoke test against ${base}"

check "GET /health" 200 "$(code "${base}/health")"
check "GET /ready"  200 "$(code "${base}/ready")"

# An anonymous caller must not reach CRM data. Authentication is checked before
# anything else, so an unauthenticated mutating request is 401, not 403.
check "GET /api/v1/contacts without a session" 401 "$(code "${base}/api/v1/contacts")"
check "POST /api/v1/contacts without a session" 401 \
  "$(code -X POST "${base}/api/v1/contacts" -H 'Content-Type: application/json' -d '{}')"

# The login endpoint must reject an empty body with a validation error.
check "POST /api/v1/auth/login with an empty body" 422 \
  "$(code -X POST "${base}/api/v1/auth/login" -H 'Content-Type: application/json' -d '{}')"

if [[ -n "${FORGECRM_SMOKE_EMAIL:-}" && -n "${FORGECRM_SMOKE_PASSWORD:-}" ]]; then
  login_body="$(curl -s -c "${cookie_jar}" -X POST "${base}/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"${FORGECRM_SMOKE_EMAIL}\",\"password\":\"${FORGECRM_SMOKE_PASSWORD}\"}")"
  csrf="$(printf '%s' "${login_body}" | sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p')"

  if [[ -n "${csrf}" ]]; then
    check "GET /api/v1/auth/me after login" 200 "$(code -b "${cookie_jar}" "${base}/api/v1/auth/me")"

    # A session without a CSRF token must not be able to mutate data.
    check "POST /api/v1/contacts without CSRF" 403 \
      "$(code -b "${cookie_jar}" -X POST "${base}/api/v1/contacts" \
        -H 'Content-Type: application/json' -d '{}')"

    check "POST /api/v1/auth/logout" 200 \
      "$(code -b "${cookie_jar}" -X POST "${base}/api/v1/auth/logout" -H "X-CSRF-Token: ${csrf}")"
    check "GET /api/v1/auth/me after logout" 401 "$(code -b "${cookie_jar}" "${base}/api/v1/auth/me")"
  else
    printf '  \033[31mfail\033[0m  login did not return a csrf_token\n'
    fail=$((fail + 1))
  fi
else
  printf '  \033[33mskip\033[0m  authenticated checks (set FORGECRM_SMOKE_EMAIL and FORGECRM_SMOKE_PASSWORD)\n'
fi

echo
if [[ ${fail} -eq 0 ]]; then
  echo "Smoke test passed (${pass} checks)."
  exit 0
fi
echo "Smoke test failed (${pass} passed, ${fail} failed)."
exit 1
