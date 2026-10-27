#!/usr/bin/env bash
#
# Verify that a host can run ForgeCRM before anything is deployed.
# Exits non-zero on the first missing requirement so CI can gate on it.

set -uo pipefail

failures=0

ok()   { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad()  { printf '  \033[31mfail\033[0m  %s\n' "$1"; failures=$((failures + 1)); }
warn() { printf '  \033[33mwarn\033[0m  %s\n' "$1"; }

echo "ForgeCRM environment check"

# --- Required tools ---------------------------------------------------------
if command -v docker >/dev/null 2>&1; then
  ok "docker $(docker --version 2>/dev/null | awk '{print $3}' | tr -d ',')"
  if docker info >/dev/null 2>&1; then
    ok "docker daemon reachable"
  else
    bad "docker daemon is not reachable (start Docker, or run with sufficient privileges)"
  fi
else
  bad "docker is not installed"
fi

if docker compose version >/dev/null 2>&1; then
  ok "docker compose $(docker compose version --short 2>/dev/null)"
else
  bad "the docker compose plugin is not available"
fi

if command -v openssl >/dev/null 2>&1; then
  ok "openssl (for generating FORGECRM_SECRET_KEY)"
else
  warn "openssl is missing; generate FORGECRM_SECRET_KEY another way"
fi

# --- Configuration ----------------------------------------------------------
if [[ -f .env ]]; then
  ok ".env is present"

  # shellcheck disable=SC1091
  key="$(grep -E '^FORGECRM_SECRET_KEY=' .env | cut -d= -f2- || true)"
  if [[ -z "${key}" ]]; then
    bad "FORGECRM_SECRET_KEY is empty; integration credentials cannot be encrypted"
  elif [[ ${#key} -lt 32 ]]; then
    bad "FORGECRM_SECRET_KEY is shorter than 32 characters"
  else
    ok "FORGECRM_SECRET_KEY is set (${#key} characters)"
  fi

  env_value="$(grep -E '^FORGECRM_ENV=' .env | cut -d= -f2- || true)"
  if [[ "${env_value}" == "production" ]]; then
    if grep -qE '^FORGECRM_COOKIE_SECURE=(true|1)$' .env; then
      ok "session cookies are marked Secure"
    else
      bad "FORGECRM_ENV=production but FORGECRM_COOKIE_SECURE is not true"
    fi
    if grep -qE '^FORGECRM_BASE_URL=https://' .env; then
      ok "FORGECRM_BASE_URL uses https"
    else
      bad "FORGECRM_ENV=production but FORGECRM_BASE_URL is not https"
    fi
  else
    ok "FORGECRM_ENV=${env_value:-unset} (development settings apply)"
  fi
else
  bad ".env is missing; copy .env.example and fill it in"
fi

# --- Ports ------------------------------------------------------------------
port="$(grep -E '^FORGECRM_HTTP_PORT=' .env 2>/dev/null | cut -d= -f2- || true)"
port="${port:-8080}"
if command -v ss >/dev/null 2>&1; then
  if ss -ltn "sport = :${port}" 2>/dev/null | grep -q LISTEN; then
    warn "port ${port} is already in use"
  else
    ok "port ${port} is free"
  fi
fi

# --- Result -----------------------------------------------------------------
echo
if [[ ${failures} -eq 0 ]]; then
  echo "All checks passed."
  exit 0
fi
echo "${failures} check(s) failed."
exit 1
