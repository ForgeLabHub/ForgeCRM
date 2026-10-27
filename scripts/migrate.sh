#!/usr/bin/env bash
#
# Apply database migrations through the API binary.
#
#   ./scripts/migrate.sh          # apply pending migrations
#   ./scripts/migrate.sh status   # report the current version
#
# Run inside the compose network so the container can reach the database.

set -euo pipefail

action="${1:-up}"

case "${action}" in
  up)
    echo "Applying migrations ..."
    docker compose run --rm --no-deps api -migrate
    echo "Migrations applied."
    ;;
  status)
    echo "Migration state:"
    docker compose exec -T postgres \
      psql --username "${POSTGRES_USER:-forgecrm}" --dbname "${POSTGRES_DB:-forgecrm}" \
      -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 5;"
    ;;
  *)
    echo "usage: $0 [up|status]" >&2
    exit 2
    ;;
esac
