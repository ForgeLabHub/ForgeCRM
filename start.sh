#!/usr/bin/env bash
# An isolated local Compose installation, never a deployment command.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$ROOT"
CONFIG="$ROOT/.env.forgecrm-local"
PROJECT=forgecrm-local
bootstrap_password="${FORGECRM_LOCAL_ADMIN_PASSWORD:-}"
bootstrap_email="${FORGECRM_LOCAL_ADMIN_EMAIL:-}"
pass_bootstrap=0

fail() { printf 'Ошибка: %s\n' "$*" >&2; exit 1; }
help() {
  cat <<'HELP'
Локальный запуск ForgeCRM (Docker + Compose, openssl и curl):
  ./start.sh                  собрать и запустить изолированный локальный стек
  ./start.sh admin EMAIL      создать первого администратора
  ./start.sh status           состояние контейнеров
  ./start.sh logs             последние логи
  ./start.sh stop             остановить контейнеры, не удаляя данные

При первом запуске конфигурация с ключом шифрования сохраняется в
.env.forgecrm-local. Сохраните этот файл: без ключа данные интеграций не
расшифровать. Для создания администратора пароль запрашивается без отображения;
в автоматизации используйте FORGECRM_LOCAL_ADMIN_EMAIL и
FORGECRM_LOCAL_ADMIN_PASSWORD в окружении при запуске.
Это только локальный development-стек, не production-развёртывание.
HELP
}

command="${1:-start}"
case "$command" in help|-h|--help) help; exit 0 ;; start|stop|status|logs|admin) ;; *) help >&2; exit 2 ;; esac
if [[ "$command" == admin ]]; then
  [[ $# == 2 && -n "$2" ]] || fail 'Укажите email: ./start.sh admin you@example.com'
else
  [[ $# -le 1 ]] || fail 'Лишние аргументы; см. ./start.sh --help'
fi
if [[ "$command" == start ]] && { [[ -n "$bootstrap_email" && -z "$bootstrap_password" ]] || [[ -z "$bootstrap_email" && -n "$bootstrap_password" ]]; }; then
  fail 'Для автоматического создания администратора задайте и FORGECRM_LOCAL_ADMIN_EMAIL, и FORGECRM_LOCAL_ADMIN_PASSWORD.'
fi
command -v docker >/dev/null 2>&1 || fail 'Docker не найден. Установите Docker с плагином Compose.'
compose_version="$(docker compose version --short 2>/dev/null)" || fail 'Плагин Docker Compose не найден.'
if [[ ! "$compose_version" =~ ^v?([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
  fail 'Не удалось определить версию Docker Compose; нужна версия 2.24.4 или новее.'
fi
major=$((10#${BASH_REMATCH[1]})); minor=$((10#${BASH_REMATCH[2]})); patch=$((10#${BASH_REMATCH[3]}))
if (( major < 2 || (major == 2 && (minor < 24 || (minor == 24 && patch < 4))) )); then
  fail 'Нужен Docker Compose 2.24.4 или новее.'
fi
docker info >/dev/null 2>&1 || fail 'Docker daemon недоступен.'

# Compose normally prefers the caller's environment, silently overriding the
# env file. Remove only application/Compose variables in a subshell so a host
# production DSN, project name or profile cannot leak into the local stack.
compose() (
  local var
  while IFS= read -r var; do
    case "$var" in FORGECRM_*|POSTGRES_*|COMPOSE_*|DATABASE_URL) unset "$var" ;; esac
  done < <(compgen -e)
  export FORGECRM_ENV_FILE="$CONFIG"
  if [[ "$pass_bootstrap" == 1 ]]; then export FORGECRM_BOOTSTRAP_ADMIN_PASSWORD="$bootstrap_password"; fi
  docker compose --project-name "$PROJECT" --env-file "$CONFIG" -f "$ROOT/docker-compose.yml" -f "$ROOT/docker-compose.local.yml" "$@"
)

# Never source the file and never silently connect a modified file to an
# external database. Reject extra keys, duplicates and anything other than the
# known local settings. The key is intentionally never printed.
validate_config() {
  awk '
    BEGIN { n=0 }
    /^FORGECRM_ENV=development$/ { env++; next }
    /^FORGECRM_DATABASE_URL=postgres:\/\/forgecrm:forgecrm@postgres:5432\/forgecrm\?sslmode=disable$/ { db++; next }
    /^FORGECRM_MIGRATE_ON_START=true$/ { migrate++; next }
    /^FORGECRM_BASE_URL=http:\/\/localhost:8080$/ { base++; next }
    /^FORGECRM_SECRET_KEY=[0-9a-f]+$/ { if (length(substr($0, 21)) == 64) key++; else n++; next }
    { n++ }
    END { exit !(n==0 && env==1 && db==1 && migrate==1 && base==1 && key==1) }
  ' "$CONFIG" || fail 'Локальный конфиг изменён или повреждён. Остановлено без изменений; проверьте .env.forgecrm-local.'
}

if [[ "$command" == start && ! -e "$CONFIG" ]]; then
  command -v openssl >/dev/null 2>&1 || fail 'Для генерации ключа нужен openssl.'
  # Losing the key must never cause a new, unrelated key to be generated for
  # an existing database volume: that would make encrypted credentials unreadable.
  volumes="$(docker volume ls --format '{{.Name}}')" || fail 'Не удалось проверить существующие тома Docker; ключ не создан.'
  if grep -Fxq "${PROJECT}_pgdata" <<< "$volumes"; then
    fail 'Локальный том уже существует, а файл ключа отсутствует. Восстановите .env.forgecrm-local из резервной копии.'
  fi
  umask 077
  tmp="$(mktemp "$ROOT/.env.forgecrm-local.XXXXXX")"
  trap 'rm -f "$tmp"' EXIT
  key="$(openssl rand -hex 32)"
  printf '%s\n' 'FORGECRM_ENV=development' \
    'FORGECRM_DATABASE_URL=postgres://forgecrm:forgecrm@postgres:5432/forgecrm?sslmode=disable' \
    'FORGECRM_MIGRATE_ON_START=true' 'FORGECRM_BASE_URL=http://localhost:8080' \
    "FORGECRM_SECRET_KEY=$key" > "$tmp"
  unset key
  # A concurrent launch must not overwrite a different key.
  ln "$tmp" "$CONFIG" 2>/dev/null || fail 'Локальный конфиг уже создан другим процессом; повторите запуск.'
  rm -f "$tmp"
  trap - EXIT
  echo 'Создан .env.forgecrm-local с отдельным ключом. Сохраните его в надёжном месте.'
fi
[[ -f "$CONFIG" ]] || fail 'Локальный конфиг отсутствует. Сначала выполните ./start.sh'
validate_config

create_admin() {
  local email="$1"
  if [[ -z "$bootstrap_password" ]]; then
    [[ -t 0 ]] || fail 'Задайте FORGECRM_LOCAL_ADMIN_PASSWORD в окружении или запустите команду в терминале.'
    read -r -s -p 'Пароль администратора: ' bootstrap_password
    echo
    [[ -n "$bootstrap_password" ]] || fail 'Пароль не может быть пустым.'
  fi
  pass_bootstrap=1
  compose run --rm --no-deps -e FORGECRM_BOOTSTRAP_ADMIN_PASSWORD api -bootstrap-admin -bootstrap-email "$email"
  pass_bootstrap=0
}

case "$command" in
  start)
    command -v curl >/dev/null 2>&1 || fail 'Для проверки готовности нужен curl.'
    echo 'Сборка и запуск локальной базы, API, интерфейса и воркера...'
    compose up -d --build --wait --wait-timeout 180
    curl --noproxy '*' -fsS --max-time 10 http://127.0.0.1:8080/ready >/dev/null || fail 'API не прошёл проверку /ready.'
    curl --noproxy '*' -fsS --max-time 10 http://127.0.0.1:8080/login | grep -q '<div id="root"></div>' || fail 'Интерфейс не отвечает на /login.'
    echo 'Готово: http://localhost:8080'
    if [[ -z "$bootstrap_email" && -t 0 ]]; then
      read -r -p 'Email первого администратора (Enter — пропустить): ' bootstrap_email
    fi
    if [[ -n "$bootstrap_email" ]]; then
      create_admin "$bootstrap_email"
    else
      echo 'Первый вход: ./start.sh admin you@example.com (если администратор ещё не создан).'
    fi
    ;;
  admin) create_admin "$2" ;;
  stop) compose stop ;;
  status) compose ps ;;
  logs) compose logs --tail=100 ;;
esac
