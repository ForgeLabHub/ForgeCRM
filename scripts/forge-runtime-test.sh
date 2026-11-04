#!/usr/bin/env bash
# Exercise failure propagation and credential forwarding without a real stack.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/project/frontend/node_modules" "$tmp/project/backend" "$tmp/bin"
cp "$root/forge.sh" "$tmp/project/forge.sh"
: > "$tmp/project/.env"
cat > "$tmp/bin/make" <<'MOCK'
#!/usr/bin/env bash
exit 42
MOCK
cat > "$tmp/bin/go" <<'MOCK'
#!/usr/bin/env bash
exit 43
MOCK
cat > "$tmp/bin/docker" <<'MOCK'
#!/usr/bin/env bash
if [[ " $* " == *' up '* ]]; then exit 44; fi
if [[ " $* " == *' -bootstrap-admin '* ]]; then
  [[ "${FORGECRM_BOOTSTRAP_PASSWORD:-}" == local-test-password ]]
  [[ " $* " == *' -e FORGECRM_BOOTSTRAP_PASSWORD '* ]]
  [[ " $* " != *local-test-password* ]]
  exit
fi
exit 0
MOCK
chmod +x "$tmp/bin/"*
export PATH="$tmp/bin:$PATH" FORGE_ENV_FILE="$tmp/project/missing.env"
forge="$tmp/project/forge.sh"
for command in test lint verify install prod; do
  if "$forge" "$command" >"$tmp/output" 2>&1; then
    echo "forge.sh $command reported success after a failed command" >&2; exit 1
  fi
  if [[ "$command" == prod ]] && grep -q 'стек поднят' "$tmp/output"; then
    echo 'forge.sh prod printed success after failure' >&2; exit 1
  fi
done
FORGECRM_BOOTSTRAP_PASSWORD=local-test-password "$forge" admin ci@example.com >/dev/null
echo 'forge.sh runtime contract tests passed (mock commands).'
