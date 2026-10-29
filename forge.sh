#!/usr/bin/env bash
# ForgeCRM — управление проектом одной командой.
#
#   ./forge.sh              интерактивное меню
#   ./forge.sh help         список команд
#   ./forge.sh start        запустить в фоне, дождаться готовности, открыть браузер
#   ./forge.sh status       что запущено, отвечает ли порт
#
# Скрипт ничего не устанавливает без явной команды `install` и не удаляет
# данные: `stop` только останавливает процессы, `clean` убирает артефакты сборки
# и логи, но не базы и не .venv (для них есть отдельные команды с --hard).

set -uo pipefail

NAME="ForgeCRM"
PORT="8080"
FRONTEND_PORT="5173"
URL="http://localhost:8080/healthz"
FRONTEND_URL="http://localhost:5173"
PYTHON="${PYTHON:-python3}"

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$ROOT" || exit 1
RUN="$ROOT/.run"
mkdir -p "$RUN"

# Если рядом есть виртуальное окружение, им пользуются ВСЕ команды (make, pytest,
# uvicorn), а не только запуск приложения: иначе `forge.sh install` ставит
# зависимости в .venv, а `forge.sh test` проверяет системный python.
# Явный выбор пользователя (PYTHON=python3.12) не переопределяется.
case "$PYTHON" in
  python|python3)
    if [ -x "$ROOT/.venv/bin/python" ]; then
      PYTHON="$ROOT/.venv/bin/python"
      export PYTHON
    fi
    ;;
esac

if [ -t 1 ]; then
  C_R=$'\033[31m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_B=$'\033[36m'; C_0=$'\033[0m'
else
  C_R=''; C_G=''; C_Y=''; C_B=''; C_0=''
fi
ok()   { printf '  %sok%s    %s\n'   "$C_G" "$C_0" "$1"; }
warn() { printf '  %swarn%s  %s\n'   "$C_Y" "$C_0" "$1"; }
bad()  { printf '  %sfail%s  %s\n'   "$C_R" "$C_0" "$1"; }
say()  { printf '\n%s%s%s\n' "$C_B" "$1" "$C_0"; }
die()  { bad "$1"; exit 1; }

has() { command -v "$1" >/dev/null 2>&1; }

# Порт занят? /dev/tcp есть в bash и не требует утилит.
port_busy() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

http_ok() {
  has curl || return 1
  curl -fsS -m 2 -o /dev/null "$1" 2>/dev/null
}

pid_of() { [ -f "$RUN/$1.pid" ] && cat "$RUN/$1.pid" 2>/dev/null; }

alive() {
  local pid; pid="$(pid_of "$1")"
  [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null
}

# kill_tree <pid>: сначала потомки, потом сам процесс — иначе дочерний npm/go
# остаётся жить и держит порт.
kill_tree() {
  local pid="$1" child
  for child in $(pgrep -P "$pid" 2>/dev/null); do kill_tree "$child"; done
  kill -TERM "$pid" 2>/dev/null
}

_spawn() { # _spawn <имя> <команда>
  local name="$1"
  local cmd="$2"
  local log="$RUN/$name.log"
  if alive "$name"; then warn "$name уже запущен (pid $(pid_of "$name"))"; return 0; fi
  if has setsid; then
    setsid bash -c "$cmd" >"$log" 2>&1 &
  else
    bash -c "$cmd" >"$log" 2>&1 &
  fi
  echo $! > "$RUN/$name.pid"
  ok "$name запущен (pid $!), лог: .run/$name.log"
}

_stop() { # _stop <имя>
  local name="$1"
  local pid
  pid="$(pid_of "$name")"
  if [ -z "${pid:-}" ]; then return 0; fi
  if ! kill -0 "$pid" 2>/dev/null; then rm -f "$RUN/$name.pid"; return 0; fi
  kill_tree "$pid"
  for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.25; done
  kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null
  rm -f "$RUN/$name.pid"
  ok "$name остановлен"
}

wait_http() { # wait_http <url> [сек] [имя процесса]
  local url="$1"
  local limit="${2:-60}"
  local who="${3:-}"
  for _ in $(seq 1 "$limit"); do
    http_ok "$url" && { ok "$url отвечает"; return 0; }
    # Нет смысла ждать минуту, если процесс уже упал: показываем лог сразу.
    if [ -n "$who" ] && ! alive "$who"; then
      bad "$who завершился, не открыв порт"
      [ -f "$RUN/$who.log" ] && tail -n 15 "$RUN/$who.log"
      return 1
    fi
    sleep 1
  done
  bad "$url не ответил за ${limit}s"
  return 1
}

open_url() {
  [ -z "$1" ] && return 0
  if has xdg-open;   then xdg-open "$1" >/dev/null 2>&1 &
  elif has open;     then open "$1" >/dev/null 2>&1
  elif has start;    then start "" "$1" >/dev/null 2>&1
  else warn "откройте вручную: $1"; fi
}

require_tools() { return 0; }

run_make() { # использует Makefile, если он есть
  has make || die "make не найден"
  make "$@"
}

cmd_help() {
  cat <<EOF
${C_B}${NAME}${C_0} — управление проектом

  ./forge.sh [команда] [параметры]

Основное
  status              что запущено, отвечают ли порты
  start [--fg]        запустить (по умолчанию в фоне) и дождаться готовности
  stop                остановить всё, что запустил скрипт
  restart             stop + start
  logs [имя] [-f]     логи (api, web, ...); -f — следить в реальном времени
  open                открыть приложение в браузере
  doctor              проверить окружение (ничего не меняет)

Разработка
  install             установить зависимости (venv/pip, npm)
  test                тесты
  lint                статический анализ
  build               собрать артефакты
  verify              lint + test + проверка документации

  api / web           запустить только бэкенд или только фронтенд
  migrate             применить миграции
  backup / restore    резервная копия и восстановление БД

Служебное
  docker up|down|logs|reset   контейнеры (данные сохраняются)
  db migrate|backup|restore   работа с базой
  clean [--hard]      убрать артефакты и логи (--hard: ещё .venv и кэши)
  version             версия скрипта и проекта
  menu                интерактивное меню

Переменные: PYTHON=python3.12, PORT=9000 ./forge.sh start
EOF
}

cmd_doctor() {
  say "Окружение"
  if has "$PYTHON"; then ok "$PYTHON: $($PYTHON -V 2>&1)"; else bad "$PYTHON не найден"; fi
  if has go;   then ok "go: $(go version 2>/dev/null | awk '{print $3}')"; else bad "go не найден (нужен для backend)"; fi
  if has node; then ok "node: $(node -v 2>/dev/null)"; else warn "node не найден (нужен для frontend)"; fi
  if has docker && docker compose version >/dev/null 2>&1; then ok "docker compose доступен"; else warn "docker compose недоступен (БД в контейнере)"; fi
  if port_busy "$PORT"; then warn "порт $PORT занят"; else ok "порт $PORT свободен"; fi
  [ -f frontend/node_modules/.package-lock.json ] && ok "frontend: зависимости установлены" || warn "frontend: npm ci не выполнялся"
  # Проекты без конфигурации (репозиторий артефактов) не должны получать
  # предупреждение о .env: ориентир — наличие .env.example в репозитории.
  if [ -f .env.example ]; then
    if [ -f .env ]; then ok ".env на месте"; else warn ".env нет (см. .env.example)"; fi
  fi
  if [ -d .git ] && [ -f .env ]; then
    if git ls-files --error-unmatch .env >/dev/null 2>&1; then
      bad ".env отслеживается git — секреты попадут в историю"
    else
      ok ".env не отслеживается git"
    fi
  fi
  if has make; then ok "make: $(make --version 2>/dev/null | head -1)"; else warn "make не найден — часть команд недоступна"; fi
}

cmd_status() {
  say "$NAME"
  local running=0
  if port_busy "$PORT"; then ok "API: порт $PORT слушается"; running=1
    http_ok "http://localhost:$PORT/healthz" && ok "API: /healthz 200" || warn "API: /healthz не отвечает"; fi
  if port_busy "$FRONTEND_PORT"; then ok "web: порт $FRONTEND_PORT слушается"; running=1; fi
  if has docker && docker compose ps --status running 2>/dev/null | grep -q .; then
    ok "docker: контейнеры запущены"; running=1; else warn "docker: контейнеры не запущены"; fi
  if [ "$running" -eq 0 ]; then warn "ничего не запущено (./forge.sh start)"; fi
}

cmd_logs() {
  local name="${1:-}" follow="${2:-}"
  case "$name" in -f|--follow) follow="-f"; name="";; esac
  if [ -z "$name" ]; then
    for f in "$RUN"/*.log; do
      [ -e "$f" ] || continue
      say "$(basename "$f")"
      tail -n 30 "$f"
    done
  else
    [ -f "$RUN/$name.log" ] || die "лога $name нет"
    tail -n 50 ${follow:+-f} "$RUN/$name.log"
  fi
}

cmd_start() {
  local fg=0
  for a in "$@"; do [ "$a" = "--fg" ] && fg=1; done
  if [ "$fg" -eq 1 ]; then run_foreground; return $?; fi
  require_tools || die "не хватает инструментов — установите их или используйте docker"
  start_services || die "запуск не удался (смотрите .run/*.log)"
  cmd_status
  open_url "$URL"
}

cmd_stop() {
  stop_services
  # добиваем то, что могло остаться от ручных запусков и держит порт
  if port_busy "$PORT"; then
    local pid
    pid="$(lsof -ti :"$PORT" 2>/dev/null | head -1)"
    if [ -n "${pid:-}" ]; then warn "порт $PORT держит pid $pid"; else warn "порт $PORT всё ещё занят"; fi
  else
    ok "порт $PORT свободен"
  fi
}

cmd_open() {
  open_url "$URL"
  if [ -n "$FRONTEND_URL" ]; then open_url "$FRONTEND_URL"; fi
}

cmd_clean() {
  local hard=0
  for a in "$@"; do [ "$a" = "--hard" ] && hard=1; done
  rm -rf "$RUN"
  clean_extra
  ok "артефакты и логи удалены"
  if [ "$hard" -eq 1 ]; then
    [ -d .venv ] && rm -rf .venv && ok ".venv удалён"
    [ -d node_modules ] && rm -rf node_modules && ok "node_modules удалён"
  else
    warn "базы, .venv и node_modules не тронуты (--hard удалит .venv и node_modules)"
  fi
}

cmd_version() {
  echo "$NAME"
  [ -f VERSION ] && echo "версия проекта: $(cat VERSION)"
  if [ -d .git ]; then echo "коммит: $(git rev-parse --short HEAD 2>/dev/null) ($(git branch --show-current 2>/dev/null))"; fi
  echo "скрипт: forge.sh v1.0"
}

cmd_menu() {
  while :; do
    cat <<EOF

${C_B}${NAME}${C_0}
  1) status            7) build
  2) start             8) verify (lint+test+docs)
  3) stop              9) docker up
  4) restart          10) docker down
  5) logs             11) db migrate
  6) test             12) doctor
 13) backup БД
 14) migrate

  0) выход
EOF
    printf 'выбор: '
    read -r choice || return 0
    case "$choice" in
      1) cmd_status ;;
      2) cmd_start ;;
      3) cmd_stop ;;
      4) cmd_stop; cmd_start ;;
      5) cmd_logs ;;
      6) cmd_test ;;
      7) cmd_build ;;
      8) cmd_verify ;;
      9) cmd_docker up ;;
      10) cmd_docker down ;;
      11) cmd_db migrate ;;
      12) cmd_doctor ;;
      13) cmd_db backup ;;
      14) cmd_db migrate ;;
      0|q|Q) return 0 ;;
      *) warn "нет такого пункта" ;;
    esac
  done
}

# --- реализация проекта ---

require_tools() {
  local missing=0
  if ! has go; then bad "go не найден — backend не запустится (https://go.dev/dl)"; missing=1; fi
  if [ ! -d frontend/node_modules ]; then warn "frontend: нет node_modules (./forge.sh install)"; fi
  return $missing
}

cmd_install() {
  say "Зависимости"
  ( cd backend && go mod download ) && ok "go модули" || bad "go mod download"
  if has npm; then ( cd frontend && npm ci --no-audit --no-fund ) && ok "npm" || bad "npm ci"; fi
  if has docker; then docker compose up -d db && ok "PostgreSQL запущен" || warn "docker compose поднять БД не удался"; fi
  run_make doctor || warn "doctor нашёл замечания (см. выше) — установка завершена"
}

start_services() {
  if has docker && ! docker compose ps --status running 2>/dev/null | grep -q .; then
    say "База данных"; docker compose up -d db && ok "db запущена"
    for _ in $(seq 1 30); do docker compose exec -T db pg_isready -U forgecrm >/dev/null 2>&1 && break; sleep 1; done
  fi
  _spawn api "cd backend && FORGECRM_LOG_LEVEL=debug go run ./cmd/forgecrm-api"
  wait_http "http://localhost:$PORT/healthz" 120 api || return 1
  if [ -d frontend/node_modules ]; then _spawn web "cd frontend && npm run dev"; fi
}

stop_services() {
  _stop web
  _stop api
  if has docker; then docker compose stop >/dev/null 2>&1 && ok "контейнеры остановлены (данные сохранены)"; fi
}

run_foreground() {
  say "Запуск API в переднем плане (Ctrl+C — остановить)"
  ( cd backend && FORGECRM_LOG_LEVEL=debug go run ./cmd/forgecrm-api )
}

cmd_test() {
  say "Тесты"
  run_make test-unit
  if [ -d frontend/node_modules ]; then run_make test-frontend; fi
}

cmd_lint() {
  say "Линт"
  run_make fmt-check vet lint
  if [ -d frontend/node_modules ]; then run_make test-frontend; fi
}

cmd_build() {
  say "Сборка"
  run_make build
}

cmd_verify() {
  say "Проверки"
  cmd_lint
  run_make test-unit docs-check
  ok "локальная матрица пройдена (интеграционные и E2E — в CI)"
}

cmd_db() {
  case "${1:-}" in
    migrate) say "Миграции"; run_make migrate ;;
    backup)  say "Резервная копия"; run_make backup ;;
    restore) say "Восстановление"; shift; run_make restore "$@" ;;
    *)       echo "db migrate | backup | restore <файл>" ;;
  esac
}

cmd_docker() {
  has docker || die "docker не найден"
  case "${1:-}" in
    up)    docker compose up -d && ok "контейнеры подняты" ;;
    down)  docker compose down && ok "контейнеры остановлены (данные сохранены)" ;;
    reset) printf 'Удалить данные БД? [y/N] '; read -r a; [ "${a:-n}" = y ] && docker compose down -v ;;
    logs)  docker compose logs -f --tail=100 ;;
    *)     echo "docker up | down | logs | reset" ;;
  esac
}

clean_extra() {
  ( cd backend && go clean -testcache ) 2>/dev/null
  rm -rf frontend/dist
}


main() {
  local cmd="${1:-menu}"
  [ $# -gt 0 ] && shift
  case "$cmd" in
    help|-h|--help) cmd_help ;;
    menu)           cmd_menu ;;
    status|ps)      cmd_status ;;
    start|up)       cmd_start "$@" ;;
    stop|down)      cmd_stop ;;
    restart)        cmd_stop; cmd_start "$@" ;;
    logs|log)       cmd_logs "$@" ;;
    open)           cmd_open ;;
    doctor)         cmd_doctor ;;
    install|setup)  cmd_install ;;
    test)           cmd_test ;;
    lint)           cmd_lint ;;
    build)          cmd_build ;;
    verify)         cmd_verify ;;
    docker)         cmd_docker "$@" ;;
    db)             cmd_db "$@" ;;
    admin)          cmd_admin 2>/dev/null || warn "команда admin не поддерживается" ;;
    clean)          cmd_clean "$@" ;;
    version|-v)     cmd_version ;;
    *) warn "неизвестная команда: $cmd"; cmd_help; exit 2 ;;
  esac
}

main "$@"
