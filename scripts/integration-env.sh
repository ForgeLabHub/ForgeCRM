#!/usr/bin/env bash
#
# Одноразовое окружение для интеграционных тестов: PostgreSQL и Redis в
# контейнерах на отдельных портах, чтобы локальный прогон был неотличим от CI.
#
#   ./scripts/integration-env.sh up     # поднять (идемпотентно)
#   ./scripts/integration-env.sh down   # снести
#   ./scripts/integration-env.sh wait   # дождаться готовности
#
# Зачем отдельный скрипт, а не `make compose-up`: рабочий стек использует порты
# 5432/6379 и том с данными разработчика. Интеграционные тесты создают и удаляют
# базы, поэтому им нужна пара контейнеров, которую не жалко снести. Порты и
# образы совпадают с сервисами джобы `backend` в .github/workflows/ci.yml —
# расхождение версии образа здесь и там делает локальный прогон бесполезным.
set -uo pipefail

pg_name="forgecrm-it-pg"
redis_name="forgecrm-it-redis"
pg_port="55432"
redis_port="56379"
pg_image="postgres:17-alpine"
redis_image="redis:7-alpine"

ok()   { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad()  { printf '  \033[31mfail\033[0m  %s\n' "$1" >&2; }
head_() { printf '\n%s\n' "$1"; }

need_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    bad "docker не найден; интеграционное окружение требует Docker"
    exit 1
  fi
  if ! docker info >/dev/null 2>&1; then
    bad "демон Docker недоступен (проверьте, что он запущен и сокет доступен)"
    exit 1
  fi
}

wait_pg() {
  for _ in $(seq 1 30); do
    docker exec "$pg_name" pg_isready -U forgecrm >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

wait_redis() {
  for _ in $(seq 1 30); do
    docker exec "$redis_name" redis-cli ping >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

cmd_up() {
  need_docker
  head_ "Интеграционное окружение"

  if docker ps -a --format '{{.Names}}' | grep -qx "$pg_name"; then
    docker start "$pg_name" >/dev/null 2>&1 || true
  else
    docker run -d --name "$pg_name" -p "${pg_port}:5432" \
      -e POSTGRES_USER=forgecrm -e POSTGRES_PASSWORD=forgecrm -e POSTGRES_DB=forgecrm_test \
      "$pg_image" >/dev/null 2>&1
  fi

  if docker ps -a --format '{{.Names}}' | grep -qx "$redis_name"; then
    docker start "$redis_name" >/dev/null 2>&1 || true
  else
    docker run -d --name "$redis_name" -p "${redis_port}:6379" \
      "$redis_image" >/dev/null 2>&1
  fi

  if wait_pg; then ok "PostgreSQL готов на порту ${pg_port}"; else bad "PostgreSQL не поднялся"; exit 1; fi
  if wait_redis; then ok "Redis готов на порту ${redis_port}"; else bad "Redis не поднялся"; exit 1; fi

  head_ "Переменные окружения"
  printf '  export FORGECRM_TEST_DATABASE_URL=%s\n' "postgres://forgecrm:forgecrm@127.0.0.1:${pg_port}/forgecrm_test?sslmode=disable"
  printf '  export FORGECRM_TEST_REDIS_URL=%s\n' "redis://127.0.0.1:${redis_port}/1"
}

cmd_down() {
  need_docker
  docker rm -f "$pg_name" "$redis_name" >/dev/null 2>&1 || true
  ok "интеграционное окружение снесено"
}

cmd_wait() {
  need_docker
  wait_pg && ok "PostgreSQL готов" || { bad "PostgreSQL не поднялся"; exit 1; }
  wait_redis && ok "Redis готов" || { bad "Redis не поднялся"; exit 1; }
}

case "${1:-up}" in
  up)   cmd_up ;;
  down) cmd_down ;;
  wait) cmd_wait ;;
  *)    bad "неизвестная команда: $1 (ожидается up|down|wait)"; exit 2 ;;
esac
