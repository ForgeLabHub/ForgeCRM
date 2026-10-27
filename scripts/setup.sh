#!/usr/bin/env bash
#
# Prepare a clean checkout for development:
#   - create .env from .env.example when missing
#   - generate a FORGECRM_SECRET_KEY
#   - start the database, apply migrations, create the first administrator
#
#   ./scripts/setup.sh you@example.com
#
# Safe to re-run: it never overwrites an existing .env or an existing admin.

set -euo pipefail

admin_email="${1:-}"

if [[ ! -f .env ]]; then
  if [[ ! -f .env.example ]]; then
    echo "error: .env.example is missing" >&2
    exit 1
  fi
  cp .env.example .env

  # A per-installation key. This is the value that protects stored integration
  # credentials, so it must never be committed or reused across environments.
  key="$(openssl rand -base64 48 | tr -d '\n')"
  if grep -q '^FORGECRM_SECRET_KEY=$' .env; then
    # Use a literal replacement to avoid sed interpreting base64 characters.
    awk -v k="${key}" '{ if ($0 == "FORGECRM_SECRET_KEY=") print "FORGECRM_SECRET_KEY=" k; else print }' \
      .env > .env.tmp && mv .env.tmp .env
  fi
  chmod 600 .env
  echo "Created .env with a generated FORGECRM_SECRET_KEY."
else
  echo ".env already exists; leaving it untouched."
fi

echo "Starting PostgreSQL ..."
docker compose up -d postgres

echo "Waiting for PostgreSQL to accept connections ..."
for _ in $(seq 1 30); do
  if docker compose exec -T postgres pg_isready -q; then break; fi
  sleep 1
done

echo "Applying migrations ..."
docker compose run --rm --no-deps api -migrate

echo "Starting the API and worker ..."
docker compose up -d api worker

if [[ -n "${admin_email}" ]]; then
  # The bootstrap command takes the password from the environment so it never
  # appears in the process list or shell history. It is idempotent: if a platform
  # owner already exists it reports that and changes nothing.
  if [[ -z "${FORGECRM_BOOTSTRAP_PASSWORD:-}" ]]; then
    echo
    echo "Set FORGECRM_BOOTSTRAP_PASSWORD before creating the administrator, e.g.:"
    echo "  FORGECRM_BOOTSTRAP_PASSWORD='<a strong password>' ./scripts/setup.sh ${admin_email}"
    echo "Skipping administrator creation."
  else
    echo "Creating the platform administrator for ${admin_email} ..."
    docker compose run --rm --no-deps \
      -e FORGECRM_BOOTSTRAP_PASSWORD="${FORGECRM_BOOTSTRAP_PASSWORD}" \
      api -bootstrap-admin -bootstrap-email "${admin_email}"
  fi
else
  echo "No admin email given. Create one later with:"
  echo "  FORGECRM_BOOTSTRAP_PASSWORD='<a strong password>' \\"
  echo "    docker compose run --rm --no-deps -e FORGECRM_BOOTSTRAP_PASSWORD \\"
  echo "      api -bootstrap-admin -bootstrap-email you@example.com"
fi

echo
echo "ForgeCRM should be reachable at http://localhost:${FORGECRM_HTTP_PORT:-8080}"
echo "Verify with: ./scripts/smoke-test.sh"
