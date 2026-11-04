#!/usr/bin/env bash
# Contract tests using fake Docker/curl; no containers, network, or real data.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/project" "$tmp/bin"
cp "$ROOT/start.sh" "$ROOT/docker-compose.yml" "$ROOT/docker-compose.local.yml" "$tmp/project/"
cat > "$tmp/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == info ]]; then exit 0; fi
if [[ "${1:-} ${2:-}" == 'compose version' ]]; then echo "${TEST_COMPOSE_VERSION:-v2.29.7}"; exit 0; fi
if [[ "${1:-} ${2:-}" == 'volume ls' ]]; then
  [[ "${TEST_VOLUME_LIST_FAIL:-0}" != 1 ]] || exit 1
  if [[ "${TEST_VOLUME_EXISTS:-0}" == 1 ]]; then echo forgecrm-local_pgdata; fi
  exit 0
fi
[[ "${1:-}" == compose ]] || exit 91
shift
[[ "${FORGECRM_ENV_FILE:-}" == "$TEST_PROJECT/.env.forgecrm-local" ]] || exit 92
[[ -z "${FORGECRM_DATABASE_URL:-}" && -z "${COMPOSE_PROFILES:-}" && -z "${POSTGRES_USER:-}" ]] || exit 93
printf '%s\n' "$*" >> "$TEST_LOG"
if [[ " $* " == *' up '* && "${TEST_UP_FAIL:-0}" == 1 ]]; then exit 42; fi
if [[ " $* " == *' run '* ]]; then
  [[ "${FORGECRM_BOOTSTRAP_ADMIN_PASSWORD:-}" == 'local-test-password' ]] || exit 94
  [[ " $* " == *' -e FORGECRM_BOOTSTRAP_ADMIN_PASSWORD '* ]] || exit 95
  [[ " $* " != *'local-test-password'* ]] || exit 96
fi
MOCK
cat > "$tmp/bin/curl" <<'MOCK'
#!/usr/bin/env bash
if [[ "$*" == *'/login'* ]]; then printf '<div id="root"></div>\n'; fi
MOCK
chmod +x "$tmp/bin/docker" "$tmp/bin/curl"
export PATH="$tmp/bin:$PATH" TEST_PROJECT="$tmp/project" TEST_LOG="$tmp/docker.log"
export FORGECRM_DATABASE_URL='postgres://production.invalid/never-use' POSTGRES_USER=wrong COMPOSE_PROFILES=proxy
start="$tmp/project/start.sh"
if TEST_COMPOSE_VERSION=v2.24.3 "$start" >/dev/null 2>&1; then echo 'old Compose was accepted' >&2; exit 1; fi
"$start" > /dev/null
config="$tmp/project/.env.forgecrm-local"
[[ -f "$config" && "$(stat -c '%a' "$config")" == 600 ]]
key_before="$(grep '^FORGECRM_SECRET_KEY=' "$config")"
"$start" >/dev/null
[[ "$key_before" == "$(grep '^FORGECRM_SECRET_KEY=' "$config")" ]]
grep -q -- '--project-name forgecrm-local' "$TEST_LOG"
grep -q -- 'up -d --build --wait --wait-timeout 180' "$TEST_LOG"
FORGECRM_LOCAL_ADMIN_EMAIL=ci@example.com FORGECRM_LOCAL_ADMIN_PASSWORD=local-test-password "$start" >/dev/null
FORGECRM_LOCAL_ADMIN_PASSWORD=local-test-password "$start" admin ci@example.com >/dev/null
"$start" stop >/dev/null
if TEST_UP_FAIL=1 "$start" >/dev/null 2>&1; then echo 'failed compose up was accepted' >&2; exit 1; fi
printf 'FORGECRM_DATABASE_URL=postgres://production.invalid/db\n' >> "$config"
if "$start" >/dev/null 2>&1; then echo 'unsafe configuration was accepted' >&2; exit 1; fi
rm "$config"
if TEST_VOLUME_EXISTS=1 "$start" >/dev/null 2>&1; then echo 'lost key was silently replaced' >&2; exit 1; fi
if TEST_VOLUME_LIST_FAIL=1 "$start" >/dev/null 2>&1; then echo 'failed volume check was ignored' >&2; exit 1; fi
[[ ! -e "$config" ]]
echo 'start.sh contract tests passed (mock Docker).'
