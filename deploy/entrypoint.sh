#!/bin/sh
# Entrypoint dispatcher for the ForgeCRM image.
#
# One image carries both binaries (forgecrm-api and forgecrm-worker), so the
# entrypoint has to choose between them. The default is the API, which keeps the
# common case — a plain `docker run <image>` or a platform that only knows about
# the image — correct.
#
# The worker is selected in any of three ways, so a platform whose "docker
# command" semantics differ (replace the entrypoint, or append to it) still runs
# the worker rather than a second API:
#
#   1. FORGECRM_ROLE=worker — an explicit hint that wins outright.
#   2. The first argument names the worker binary (dockerCommand appended to the
#      entrypoint).
#   3. The worker binary is run directly (dockerCommand replaces the entrypoint).
#
# Every other argument is passed through to the API, so `-migrate`,
# `-bootstrap-admin` and `-version` behave exactly as before.
set -e

if [ "${FORGECRM_ROLE:-}" = "worker" ]; then
  exec /usr/local/bin/forgecrm-worker "$@"
fi

case "${1:-}" in
  worker | forgecrm-worker | /usr/local/bin/forgecrm-worker)
    shift
    exec /usr/local/bin/forgecrm-worker "$@"
    ;;
  api | forgecrm-api | /usr/local/bin/forgecrm-api)
    shift
    exec /usr/local/bin/forgecrm-api "$@"
    ;;
esac

exec /usr/local/bin/forgecrm-api "$@"
