#!/bin/sh
# Entrypoint dispatcher for the ForgeCRM image.
#
# One image carries all three binaries (forgecrm-api, forgecrm-worker and
# forgecrm-telegram), so the entrypoint has to choose between them. The default
# is the API, which keeps the common case — a plain `docker run <image>` or a
# platform that only knows about the image — correct.
#
# The worker or the bot is selected in any of three ways, so a platform whose
# "docker command" semantics differ (replace the entrypoint, or append to it)
# still runs the intended process rather than a second API:
#
#   1. FORGECRM_ROLE=worker|telegram — an explicit hint that wins outright.
#   2. The first argument names the binary (dockerCommand appended to the
#      entrypoint).
#   3. The binary is run directly (dockerCommand replaces the entrypoint).
#
# Every other argument is passed through to the API, so `-migrate`,
# `-bootstrap-admin` and `-version` behave exactly as before.
set -e

case "${FORGECRM_ROLE:-}" in
  worker)
    exec /usr/local/bin/forgecrm-worker "$@"
    ;;
  telegram | bot)
    exec /usr/local/bin/forgecrm-telegram "$@"
    ;;
esac

case "${1:-}" in
  worker | forgecrm-worker | /usr/local/bin/forgecrm-worker)
    shift
    exec /usr/local/bin/forgecrm-worker "$@"
    ;;
  telegram | bot | forgecrm-telegram | /usr/local/bin/forgecrm-telegram)
    shift
    exec /usr/local/bin/forgecrm-telegram "$@"
    ;;
  api | forgecrm-api | /usr/local/bin/forgecrm-api)
    shift
    exec /usr/local/bin/forgecrm-api "$@"
    ;;
esac

exec /usr/local/bin/forgecrm-api "$@"
