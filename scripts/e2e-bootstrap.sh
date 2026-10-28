#!/usr/bin/env bash
# Provision a platform owner for the end-to-end suite.
#
# The product has no default password by design, so the E2E run cannot assume an
# account exists. This script creates the same bootstrap owner an operator would
# create by hand, using the same binary and the same code path, so the suite
# exercises the real provisioning route rather than a test-only back door.
#
# It is idempotent: bootstrap-admin is a no-op once a platform owner exists, so
# running it before every suite is safe.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_BIN="${FORGECRM_API_BIN:-${ROOT_DIR}/backend/forgecrm-api}"
EMAIL="${FORGECRM_E2E_EMAIL:-e2e-owner@forgecrm.test}"
PASSWORD="${FORGECRM_E2E_PASSWORD:-E2E-Owner-Passw0rd-2026!}"

if [[ ! -x "${API_BIN}" ]]; then
  echo "error: ${API_BIN} is not executable." >&2
  echo "Build it first: (cd backend && go build -o forgecrm-api ./cmd/forgecrm-api)" >&2
  exit 1
fi

echo "Provisioning the E2E platform owner ${EMAIL}…"
FORGECRM_BOOTSTRAP_PASSWORD="${PASSWORD}" \
  "${API_BIN}" --bootstrap-admin --bootstrap-email "${EMAIL}"
echo "Done. The E2E suite can now sign in as ${EMAIL}."
