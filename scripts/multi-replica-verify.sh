#!/usr/bin/env bash
#
# Multi-replica rehearsal: two API containers behind a real nginx, sharing one
# PostgreSQL and one Redis.
#
#   ./scripts/multi-replica-verify.sh [--keep]
#
# Why this exists as a script and not only as Go tests: the Go tests build two
# routers in one process, which proves the *code* shares state through Redis and
# PostgreSQL. This script proves the *deployment* does — two separate containers,
# two separate networks' worth of connection setup, a real proxy terminating the
# connection, and a real container being stopped and restarted. A bug in the
# compose wiring, the trusted-proxy range, or the health ordering would not show
# up in the in-process tests at all.
#
# What it checks:
#   A. A session created through one replica works through the other, and logout
#      and revocation take effect on both.
#   B. The login rate limit is shared: a budget of 2 is 2 attempts across the
#      pair, not 2 per replica.
#   C. Stopping one replica leaves the other serving, with counters intact.
#   D. The proxy's forwarding headers are believed, an untrusted client cannot
#      choose its own bucket by rotating X-Forwarded-For, and a direct connection
#      is attributed to its own address.
#   E. Redis being down fails open for ordinary API traffic and closed for login,
#      reports the store failure rather than a rate-limit refusal, and recovers.
#   F. `docker compose --profile redis up` reaches a healthy state.
#
# Nothing here touches the default stack: a separate compose project name,
# separate ports (18080-18082) and a separate volume.

set -uo pipefail

cd "$(dirname "$0")/.."

COMPOSE="docker compose -f docker-compose.multireplica.yml"
PROXY="http://127.0.0.1:18080"
REPLICA_A="http://127.0.0.1:18081"
REPLICA_B="http://127.0.0.1:18082"

KEEP=0
[[ "${1:-}" == "--keep" ]] && KEEP=1

pass=0
fail=0

check() {
  local name="$1" expected="$2" actual="$3"
  if [[ "${actual}" == "${expected}" ]]; then
    printf '  \033[32mok\033[0m    %s (%s)\n' "${name}" "${actual}"
    pass=$((pass + 1))
  else
    printf '  \033[31mfail\033[0m  %s (got %s, want %s)\n' "${name}" "${actual}" "${expected}"
    fail=$((fail + 1))
  fi
}

contains() {
  local name="$1" needle="$2" haystack="$3"
  if [[ "${haystack}" == *"${needle}"* ]]; then
    printf '  \033[32mok\033[0m    %s\n' "${name}"
    pass=$((pass + 1))
  else
    printf '  \033[31mfail\033[0m  %s (missing %q)\n' "${name}" "${needle}"
    fail=$((fail + 1))
  fi
}

code() { curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$@"; }

# flush_redis clears the shared counter store between sections.
#
# Every request from the test host is attributed to one client address — that is
# the point of the trust model, and it is asserted in section D — so the sections
# would otherwise consume each other's budget. Flushing is how the script starts
# each section from a known count without inventing a new identity, which a client
# is not allowed to do.
flush_redis() {
  ${COMPOSE} exec -T redis redis-cli -n 0 FLUSHDB >/dev/null 2>&1
}

cleanup() {
  if [[ "${KEEP}" == "1" ]]; then
    echo "leaving the stack up (--keep); tear down with: ${COMPOSE} down -v"
    return
  fi
  ${COMPOSE} down -v --remove-orphans >/dev/null 2>&1
}
trap cleanup EXIT

wait_for() {
  local url="$1" name="$2" tries="${3:-60}"
  for _ in $(seq 1 "${tries}"); do
    if [[ "$(code "${url}")" == "200" ]]; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for ${name} at ${url}" >&2
  ${COMPOSE} logs --tail=40 >&2
  return 1
}

echo "== bringing up the two-replica stack =="
${COMPOSE} down -v --remove-orphans >/dev/null 2>&1
if ! ${COMPOSE} up -d --build >/dev/null 2>&1; then
  echo "compose up failed" >&2
  ${COMPOSE} logs --tail=60 >&2
  exit 1
fi

# Wait on readiness, not on a running container. A container that is up but has
# not finished migrations will answer /health before it can serve /api/v1.
wait_for "${PROXY}/health" "the proxy" || exit 1
wait_for "${REPLICA_A}/health" "replica A" || exit 1
wait_for "${REPLICA_B}/health" "replica B" || exit 1

# /ready is the stronger check: it confirms the database is reachable, which
# /health does not.
wait_for "${REPLICA_A}/ready" "replica A readiness" || exit 1
wait_for "${REPLICA_B}/ready" "replica B readiness" || exit 1

echo
echo "== F. compose readiness =="
check "replica A /health" 200 "$(code "${REPLICA_A}/health")"
check "replica B /health" 200 "$(code "${REPLICA_B}/health")"
check "replica A /ready"  200 "$(code "${REPLICA_A}/ready")"
check "replica B /ready"  200 "$(code "${REPLICA_B}/ready")"
check "proxy /health"     200 "$(code "${PROXY}/health")"

# Every container the compose file defines must report healthy, and a running
# container is not the same as a healthy one. The check waits: a container that
# has just started is "starting" during its start_period, which is normal and not
# a finding.
all_healthy=0
for _ in $(seq 1 30); do
  unhealthy="$(${COMPOSE} ps --format '{{.Service}} {{.Health}}' | grep -v 'healthy$' || true)"
  if [[ -z "${unhealthy}" ]]; then
    all_healthy=1
    break
  fi
  sleep 2
done
if [[ "${all_healthy}" == "1" ]]; then
  printf '  \033[32mok\033[0m    all services healthy\n'
  pass=$((pass + 1))
else
  printf '  \033[31mfail\033[0m  all services healthy (still not healthy: %s)\n' "${unhealthy}"
  fail=$((fail + 1))
fi

echo
echo "== creating an administrator =="
# The bootstrap runs inside the network, so the password never appears in the
# process list of the host and the API port is not exposed for it.
${COMPOSE} exec -T -e FORGECRM_BOOTSTRAP_PASSWORD='rehearsal-admin-password-1' api-a \
  /usr/local/bin/forgecrm-api -bootstrap-admin -bootstrap-email admin@rehearsal.test >/dev/null 2>&1
bootstrap_status=$?
check "bootstrap administrator" 0 "${bootstrap_status}"

jar="$(mktemp)"
trap 'rm -f "${jar}"; cleanup' EXIT

echo
echo "== A. shared session =="
# Sign in through replica A directly, then use the cookie against replica B.
login_code=$(code -c "${jar}" -X POST "${REPLICA_A}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@rehearsal.test","password":"rehearsal-admin-password-1"}')
check "login on replica A" 200 "${login_code}"

check "session works on replica A" 200 "$(code -b "${jar}" "${REPLICA_A}/api/v1/auth/me")"
check "session works on replica B" 200 "$(code -b "${jar}" "${REPLICA_B}/api/v1/auth/me")"
check "session works through the proxy" 200 "$(code -b "${jar}" "${PROXY}/api/v1/auth/me")"

csrf="$(curl -s -b "${jar}" "${REPLICA_A}/api/v1/auth/me" | sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p')"

echo
echo "== B. shared login rate limit (budget is 2) =="
# The budget is set to 2 in the compose file. Alternate replicas: with a
# per-process counter each replica would allow 2, so attempt 3 would find replica
# A at its own count of 1 and be allowed — a shared counter refuses it.
#
# Every request from the test host shares one client address, so the section
# starts from a flush. If the counter were per-process, attempt 3 would land on a
# replica with its own count of 1 and be allowed; a shared counter refuses it.
flush_redis
b1=$(code -X POST "${REPLICA_A}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
b2=$(code -X POST "${REPLICA_B}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
b3=$(code -X POST "${REPLICA_A}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
check "attempt 1 (replica A) is not refused" 401 "${b1}"
check "attempt 2 (replica B) is not refused" 401 "${b2}"
check "attempt 3 (replica A) is refused"    429 "${b3}"

echo
echo "== D. reverse proxy and client attribution =="
# A direct connection to a replica must be attributed to its own address, so a
# client cannot pick a bucket by sending a forwarding header from an untrusted
# peer. The published ports connect from an address that is not the trusted
# proxy, so the header is ignored and the request lands in the caller's own
# bucket — the one section B already exhausted. A spoofed header that opened a
# new bucket would answer 401 here instead of 429.
spoof=$(code -X POST "${REPLICA_A}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -H 'X-Forwarded-For: 198.51.100.7' -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
check "a direct client cannot open a bucket with a spoofed header" 429 "${spoof}"

# Through the proxy, nginx appends the real peer to X-Forwarded-For. The API
# reads the right-most untrusted entry, which is the address nginx recorded, so
# a client that rotates the header it sends still lands in one bucket.
flush_redis
p1=$(code -X POST "${PROXY}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -H 'X-Forwarded-For: 203.0.113.201' -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
p2=$(code -X POST "${PROXY}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -H 'X-Forwarded-For: 203.0.113.202' -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
p3=$(code -X POST "${PROXY}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -H 'X-Forwarded-For: 203.0.113.203' -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
check "proxy attempt 1 is not refused" 401 "${p1}"
check "proxy attempt 2 is not refused" 401 "${p2}"
check "proxy attempt 3 is refused despite a rotated header" 429 "${p3}"

echo
echo "== E. Redis failure =="
${COMPOSE} stop redis >/dev/null 2>&1
sleep 2

# Login is fail-closed: the store cannot count, so it must refuse. It must not
# answer 429, which would tell the client to slow down when the truth is that the
# server cannot count; 503 with a distinct code is the honest answer.
redis_down_login=$(code -X POST "${REPLICA_A}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
check "login with Redis down is refused as unavailable, not rate-limited" 503 "${redis_down_login}"

# Ordinary API traffic is fail-open: an authenticated read must still be served.
api_down=$(code -b "${jar}" "${REPLICA_A}/api/v1/system/info")
if [[ "${api_down}" == "429" ]]; then
  printf '  \033[31mfail\033[0m  ordinary API traffic was refused with Redis down; it must fail open\n'
  fail=$((fail + 1))
else
  printf '  \033[32mok\033[0m    ordinary API traffic still served with Redis down (%s)\n' "${api_down}"
  pass=$((pass + 1))
fi

# The degraded decision must be logged, and logged as a store problem.
logs=$(${COMPOSE} logs --tail=80 api-a 2>&1)
contains "the degraded limiter decision is logged" "rate limit store unavailable" "${logs}"

${COMPOSE} start redis >/dev/null 2>&1
sleep 3
flush_redis
recovered=$(code -b "${jar}" "${REPLICA_A}/api/v1/system/info")
check "ordinary traffic recovers after Redis returns" 200 "${recovered}"

echo
echo "== C. replica failure =="
# Stop the replica the session was created on; the other must keep serving.
${COMPOSE} stop api-a >/dev/null 2>&1
sleep 2
check "replica B keeps serving with replica A down" 200 "$(code -b "${jar}" "${REPLICA_B}/api/v1/auth/me")"

# The shared counter must still be enforced while one replica is down. Spend the
# budget on the surviving replica and then confirm it is refused there.
flush_redis
c1=$(code -X POST "${REPLICA_B}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
c2=$(code -X POST "${REPLICA_B}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
c3=$(code -X POST "${REPLICA_B}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')
check "surviving replica serves within budget" 401 "${c1}"
check "surviving replica serves within budget (2)" 401 "${c2}"
check "surviving replica refuses past the shared budget" 429 "${c3}"

${COMPOSE} start api-a >/dev/null 2>&1
wait_for "${REPLICA_A}/ready" "replica A after restart" || exit 1
check "the session still works after replica A restarts" 200 "$(code -b "${jar}" "${REPLICA_A}/api/v1/auth/me")"

# The counters written before the restart must still be in force: a restarted
# replica must not hand back a fresh budget.
check "the shared limit survives the restart" 429 "$(code -X POST "${REPLICA_A}/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"email":"nobody@rehearsal.test","password":"wrong-password"}')"

echo
echo "== A. logout and revocation across replicas =="
logout=$(code -b "${jar}" -X POST "${REPLICA_B}/api/v1/auth/logout" -H "X-CSRF-Token: ${csrf}")
check "logout on replica B" 200 "${logout}"
check "the session is gone on replica A" 401 "$(code -b "${jar}" "${REPLICA_A}/api/v1/auth/me")"
check "the session is gone on replica B" 401 "$(code -b "${jar}" "${REPLICA_B}/api/v1/auth/me")"

echo
if [[ "${fail}" -eq 0 ]]; then
  printf '\033[32m%s checks passed\033[0m\n' "${pass}"
else
  printf '\033[31m%s failed\033[0m, %s passed\n' "${fail}" "${pass}"
fi
exit "${fail}"
