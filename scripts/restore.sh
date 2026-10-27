#!/usr/bin/env bash
#
# Restore a ForgeCRM backup produced by scripts/backup.sh.
#
#   ./scripts/restore.sh backups/forgecrm-db-20260101T000000Z.dump
#
# This DESTROYS the current database contents. It refuses to run unless
# --yes-really is passed, and verifies the checksum first when one is present.

set -euo pipefail

dump="${1:-}"
shift || true
confirmed=false
for arg in "$@"; do
  [[ "${arg}" == "--yes-really" ]] && confirmed=true
done

if [[ -z "${dump}" || ! -f "${dump}" ]]; then
  echo "usage: $0 <path-to-.dump> [--yes-really]" >&2
  exit 2
fi

if [[ "${confirmed}" != true ]]; then
  echo "error: this overwrites the current database." >&2
  echo "       re-run with --yes-really once you are sure." >&2
  exit 1
fi

checksum_file="$(dirname "${dump}")/$(basename "${dump}" .dump).sha256"
if [[ -f "${checksum_file}" ]]; then
  echo "Verifying checksum ..."
  ( cd "$(dirname "${dump}")" && sha256sum -c "$(basename "${checksum_file}")" )
fi

db_name="$(grep -E '^POSTGRES_DB=' .env 2>/dev/null | cut -d= -f2- || true)"
db_user="$(grep -E '^POSTGRES_USER=' .env 2>/dev/null | cut -d= -f2- || true)"
db_name="${db_name:-forgecrm}"
db_user="${db_user:-forgecrm}"

echo "Stopping api and worker so nothing writes during the restore ..."
docker compose stop api worker >/dev/null

echo "Recreating database ${db_name} ..."
docker compose exec -T postgres psql --username "${db_user}" --dbname postgres \
  -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS ${db_name} WITH (FORCE);" \
  -c "CREATE DATABASE ${db_name} OWNER ${db_user};"

echo "Restoring ..."
docker compose exec -T postgres pg_restore \
  --username "${db_user}" \
  --dbname "${db_name}" \
  --no-owner \
  --exit-on-error \
  < "${dump}"

echo "Restarting services ..."
docker compose start api worker >/dev/null

echo "Restore complete. Check GET /ready before serving traffic."
