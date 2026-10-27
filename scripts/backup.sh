#!/usr/bin/env bash
#
# Back up the ForgeCRM database and the import data volume.
#
#   ./scripts/backup.sh [output-dir]
#
# Produces a consistent custom-format dump (restorable with pg_restore) plus a
# tar of uploaded files. The dump is taken with --serializable-deferrable so it
# reflects a single point in time even while the API keeps serving.

set -euo pipefail

out_dir="${1:-./backups}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p "${out_dir}"

compose() { docker compose "$@"; }

if ! compose ps --status running --services 2>/dev/null | grep -qx postgres; then
  echo "error: the postgres service is not running" >&2
  exit 1
fi

db_name="$(grep -E '^POSTGRES_DB=' .env 2>/dev/null | cut -d= -f2- || true)"
db_user="$(grep -E '^POSTGRES_USER=' .env 2>/dev/null | cut -d= -f2- || true)"
db_name="${db_name:-forgecrm}"
db_user="${db_user:-forgecrm}"

echo "Backing up database ${db_name} ..."
compose exec -T postgres pg_dump \
  --username "${db_user}" \
  --dbname "${db_name}" \
  --format custom \
  --serializable-deferrable \
  > "${out_dir}/forgecrm-db-${stamp}.dump"

echo "Backing up import data volume ..."
compose run --rm --no-deps -T --entrypoint sh api -c \
  'tar -czf - -C /var/lib/forgecrm .' \
  > "${out_dir}/forgecrm-imports-${stamp}.tar.gz"

# Checksums let a restore verify the archives before touching live data.
( cd "${out_dir}" && sha256sum "forgecrm-db-${stamp}.dump" "forgecrm-imports-${stamp}.tar.gz" \
    > "forgecrm-${stamp}.sha256" )

echo
echo "Backup complete:"
ls -lh "${out_dir}/forgecrm-"*"${stamp}"*
echo
echo "Verify with:  (cd ${out_dir} && sha256sum -c forgecrm-${stamp}.sha256)"
