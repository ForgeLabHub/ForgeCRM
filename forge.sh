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
FILE_VERSION="1.1.0"

# Значения по умолчанию. Реальные берутся так: аргумент окружения > .env > это.
PORT_DEFAULT="8080"
FRONTEND_PORT_DEFAULT="5173"
URL_TEMPLATE="http://localhost:@PORT@"
HEALTH_PATH="/health"
READY_PATH="/ready"
FRONTEND_URL_TEMPLATE="http://localhost:@PORT@"

QUIET=0
JSON=0
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

# Цвет отключается флагом --no-color и переменной NO_COLOR (стандарт no-color.org),
# а также автоматически при выводе в файл или в pipe.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_R=$'\033[31m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_B=$'\033[36m'; C_D=$'\033[2m'; C_0=$'\033[0m'
else
  C_R=''; C_G=''; C_Y=''; C_B=''; C_D=''; C_0=''
fi
ok()   { [ "$QUIET" -eq 1 ] && return 0; printf '  %sok%s    %s\n'   "$C_G" "$C_0" "$1"; }
warn() { printf '  %swarn%s  %s\n'   "$C_Y" "$C_0" "$1"; }
bad()  { printf '  %sfail%s  %s\n'   "$C_R" "$C_0" "$1"; }
say()  { [ "$QUIET" -eq 1 ] && return 0; printf '\n%s%s%s\n' "$C_B" "$1" "$C_0"; }
note() { [ "$QUIET" -eq 1 ] && return 0; printf '  %s%s%s\n' "$C_D" "$1" "$C_0"; }
die()  { bad "$1"; exit 1; }

has() { command -v "$1" >/dev/null 2>&1; }

# Порт занят? /dev/tcp есть в bash и не требует утилит.
port_busy() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

# Порт в doctor: занятый порт — проблема только если его держит не наш процесс.
doctor_port() {
  [ -z "${PORT:-}" ] && return 0
  if port_busy "$PORT"; then
    if alive app; then ok "порт $PORT слушает наш процесс (pid $(pid_of app))"
    else warn "порт $PORT занят другим процессом"; fi
  else ok "порт $PORT свободен"; fi
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
  if [ "$HERMETIC" -eq 1 ]; then
    # Тесты и линт не должны зависеть от боевого .env: иначе `forge.sh test`
    # ведёт себя иначе, чем CI, и проверки «без ключа» перестают проверять.
    ( for _k in $ENV_FROM_FILE; do unset "$_k"; done; make "$@" )
  else
    make "$@"
  fi
}

unset_env_from_file() { for _k in $ENV_FROM_FILE; do unset "$_k"; done; }

# --- .env --------------------------------------------------------------------
# Файл читается собственным парсером, а НЕ через `source`: source выполнил бы
# произвольный код из файла, а .env приходит из репозитория и от коллег.
#
# Поддерживается: комментарии `#`, пустые строки, префикс `export`,
# одинарные и двойные кавычки, CRLF, пробелы вокруг `=`, инлайн-комментарий
# после значения (`KEY=value # пояснение`).
# Не поддерживается (сознательно): многострочные значения и подстановки вида
# `${OTHER}` — это уже язык, а не конфигурация.
#
# Приоритет: переменная, уже заданная в окружении, НЕ перетирается файлом,
# поэтому `PORT=9000 ./forge.sh start` работает независимо от .env.

ENV_FILE="${FORGE_ENV_FILE:-$ROOT/.env}"
ENV_LOADED=0
ENV_SKIPPED=0
ENV_LIST=()   # элементы: "имя<TAB>состояние<TAB>значение"
ENV_FROM_FILE=""  # имена, реально загруженные из .env (нужны для герметичных тестов)
HERMETIC=0
# Имена, которые нельзя принимать из файла: они управляют работой самого shell.
ENV_BLOCKED="PATH IFS PS1 PS2 PS4 HOME SHELL PWD OLDPWD LD_PRELOAD LD_LIBRARY_PATH BASH_ENV CDPATH"

is_secret_name() {
  case "$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')" in
    # длительности, размеры и адреса секретами не считаем: маскировать число
    # минут или имя хоста — только путать
    *_MINUTES|*_SECONDS|*_HOURS|*_DAYS|*_TTL|*_SIZE|*_COUNT|*_LIMIT|*_PORT|*_URL|*_URI|*_HOST) return 1 ;;
    *SECRET*|*PASSWORD*|*PASSWD*|*TOKEN*|*CREDENTIAL*|*PRIVATE*|*API_KEY*|*APIKEY*|*ADMIN_CODE*|*DSN*) return 0 ;;
  esac
  return 1
}

is_placeholder() { # значение-заглушка из примера?
  # tr/lowercase не понижает регистр кириллицы в C-локали, поэтому варианты
  # написания перечислены явно — проверка не зависит от локали машины.
  case "$1" in
    ''|'<'*|todo*|TODO*|Todo*) return 0 ;;
    change-me*|change_me*|changeme*|Change-Me*|CHANGE-ME*|CHANGE_ME*|CHANGEME*|example*|Example*|EXAMPLE*|your-*|your_*|YOUR-*|YOUR_*|xxx*|XXX*|placeholder*|Placeholder*|PLACEHOLDER*|admin123|demo123) return 0 ;;
    замени*|Замени*|ЗАМЕНИ*|сгенерируй*|Сгенерируй*|СГЕНЕРИРУЙ*|поменяй*|Поменяй*|ПОМЕНЯЙ*|измени*|Измени*|ИЗМЕНИ*|впиши*|Впиши*|ВПИШИ*|укажи*|Укажи*|УКАЖИ*|пароль*|Пароль*|ПАРОЛЬ*|секрет*|Секрет*|СЕКРЕТ*) return 0 ;;
  esac
  return 1
}

mask_value() { # mask_value <имя> <значение>
  local name="$1" value="$2"
  [ -z "$value" ] && { printf '(пусто)'; return 0; }
  if is_secret_name "$name"; then
    if [ "${#value}" -le 6 ]; then
      printf '****'
    else
      printf '%s…%s (%s симв.)' "${value:0:2}" "${value: -2}" "${#value}"
    fi
    return 0
  fi
  # Пароль внутри строки подключения: postgres://user:pass@host -> user:***@host
  case "$value" in
    *://*:*@*) printf '%s' "$value" | sed -E 's#(://[^:/@]+:)[^@]*@#\1***@#'; return 0 ;;
  esac
  printf '%s' "$value"
}

# printf %-28s считает байты, а кириллица в UTF-8 занимает по два — поэтому
# колонки выравниваем вручную, по числу символов.
pad_to() { # pad_to <строка> <ширина>
  local str="$1" width="$2" n="${#1}"
  printf '%s' "$str"
  while [ "$n" -lt "$width" ]; do printf ' '; n=$((n + 1)); done
}

trim_spaces() {
  local s="$1"
  s="${s#"${s%%[! ]*}"}"
  s="${s%"${s##*[! ]}"}"
  printf '%s' "$s"
}

rand_secret() { LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom 2>/dev/null | head -c "${1:-32}"; }

load_env() { # load_env [файл]
  local file="${1:-$ENV_FILE}"
  ENV_FILE="$file"
  ENV_LOADED=0
  ENV_SKIPPED=0
  ENV_LIST=()
  ENV_FROM_FILE=""
  [ -f "$file" ] || return 0
  local line key value state rest
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%$'\r'}"
    line="$(trim_spaces "$line")"
    case "$line" in ''|\#*) continue ;; esac
    case "$line" in export[[:space:]]*) line="$(trim_spaces "${line#export}")" ;; esac
    case "$line" in *=*) ;; *) continue ;; esac
    key="$(trim_spaces "${line%%=*}")"
    value="$(trim_spaces "${line#*=}")"
    case "$key" in
      ''|*[!A-Za-z0-9_]*|[0-9]*) ENV_LIST+=("$key"$'\t'"invalid"$'\t'"$value"); continue ;;
    esac
    case " $ENV_BLOCKED " in
      *" $key "*)
        ENV_LIST+=("$key"$'\t'"ignored"$'\t'"$value")
        ENV_SKIPPED=$((ENV_SKIPPED + 1))
        continue ;;
    esac
    case "$value" in
      \"*) value="${value#\"}"; value="${value%%\"*}" ;;
      \'*) value="${value#\'}"; value="${value%%\'*}" ;;
      *)   case "$value" in *' #'*) value="$(trim_spaces "${value%% #*}")" ;; esac ;;
    esac
    if [ -n "${!key+set}" ]; then
      state="env"
      ENV_SKIPPED=$((ENV_SKIPPED + 1))
    else
      export "$key=$value"
      state="loaded"
      ENV_FROM_FILE="$ENV_FROM_FILE $key"
      ENV_LOADED=$((ENV_LOADED + 1))
    fi
    ENV_LIST+=("$key"$'\t'"$state"$'\t'"$value")
  done < "$file"
  return 0
}

_env_show() {
  if [ ! -f "$ENV_FILE" ]; then
    warn "файла $ENV_FILE нет"
    note "создать из примера: ./forge.sh env init"
    return 1
  fi
  say "Переменные: $ENV_FILE"
  printf '  %s%s %s %s%s\n' "$C_D" "$(pad_to "ИМЯ" 28)" "$(pad_to "ИСТОЧНИК" 11)" "ЗНАЧЕНИЕ" "$C_0"
  local entry key state value rest
  for entry in ${ENV_LIST[@]+"${ENV_LIST[@]}"}; do
    key="${entry%%$'\t'*}"
    rest="${entry#*$'\t'}"
    state="${rest%%$'\t'*}"
    value="${rest#*$'\t'}"
    case "$state" in
      loaded)  state="файл" ;;
      env)     state="окружение" ;;
      ignored) state="игнор" ;;
      invalid) state="ошибка" ;;
    esac
    printf '  %s %s %s\n' "$(pad_to "$key" 28)" "$(pad_to "$state" 11)" "$(mask_value "$key" "$value")"
  done
  note "всего: ${#ENV_LIST[@]} · загружено $ENV_LOADED · не перетёрто (уже в окружении) $ENV_SKIPPED"
}

_env_keys() { # имена переменных из файла, по одному в строке
  [ -f "$1" ] || return 0
  grep -E '^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*=' "$1" 2>/dev/null \
    | sed -E 's/^[[:space:]]*(export[[:space:]]+)?//; s/=.*//' | sort -u
}

_env_check() {
  [ -f "$ENV_FILE" ] || { bad "$ENV_FILE нет — ./forge.sh env init"; return 1; }
  local example="" c fails=0
  for c in .env.example env.example .env.sample; do [ -f "$c" ] && { example="$c"; break; }; done
  say "Сверка $ENV_FILE"
  if [ -n "$example" ]; then
    local missing extra
    missing="$(comm -23 <(_env_keys "$example") <(_env_keys "$ENV_FILE") 2>/dev/null)"
    extra="$(comm -13 <(_env_keys "$example") <(_env_keys "$ENV_FILE") 2>/dev/null)"
    if [ -z "$missing" ]; then
      ok "все переменные из $example на месте"
    else
      warn "нет в .env (возьмутся значения по умолчанию): $(printf '%s ' $missing)"
    fi
    [ -n "$extra" ] && note "только в .env: $(printf '%s ' $extra)"
  fi
  local entry key state value rest placeholders=""
  for entry in ${ENV_LIST[@]+"${ENV_LIST[@]}"}; do
    key="${entry%%$'\t'*}"
    rest="${entry#*$'\t'}"
    state="${rest%%$'\t'*}"
    value="${rest#*$'\t'}"
    case "$state" in ignored|invalid) continue ;; esac
    is_secret_name "$key" || continue
    if [ -z "$value" ]; then
      bad "$key пуст"
      fails=$((fails + 1))
    elif is_placeholder "$value"; then
      placeholders="$placeholders $key"
    fi
  done
  if [ -n "$placeholders" ]; then
    bad "остались значения из примера:$placeholders"
    fails=$((fails + 1))
  fi
  if [ -f .gitignore ] && ! grep -qxF '.env' .gitignore 2>/dev/null; then
    warn ".env не указан в .gitignore"
  fi
  if [ "$fails" -eq 0 ]; then ok "конфигурация готова к запуску"; return 0; fi
  bad "замечаний: $fails"
  return 1
}

_env_init() {
  local force=0 a c example=""
  for a in "$@"; do [ "$a" = "--force" ] && force=1; done
  if [ -f "$ENV_FILE" ] && [ "$force" -eq 0 ]; then
    bad "$ENV_FILE уже существует"
    note "перезаписать: ./forge.sh env init --force (прежний файл останется в .env.bak)"
    return 1
  fi
  for c in .env.example env.example .env.sample; do [ -f "$c" ] && { example="$c"; break; }; done
  [ -z "$example" ] && { bad "нет .env.example — нечего копировать"; return 1; }
  if [ -f "$ENV_FILE" ]; then
    cp "$ENV_FILE" "$ENV_FILE.bak" && ok "прежний файл сохранён: $(basename "$ENV_FILE").bak"
  fi
  cp "$example" "$ENV_FILE" || { bad "не удалось создать $ENV_FILE"; return 1; }
  chmod 600 "$ENV_FILE" 2>/dev/null
  ok "создан из $example"
  # Пустые и примерные значения секретов заменяются случайными: файл сразу
  # рабочий, а не «поменяйте change-me на что-нибудь».
  local tmp line key value bare changed=""
  tmp="$(mktemp)"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in ''|\#*) printf '%s\n' "$line" >> "$tmp"; continue ;; esac
    case "$line" in
      export[[:space:]]*[A-Za-z_]*=*|[A-Za-z_]*=*) ;;
      *) printf '%s\n' "$line" >> "$tmp"; continue ;;
    esac
    key="${line%%=*}"
    key="${key#export }"
    key="$(trim_spaces "$key")"
    value="${line#*=}"
    bare="$value"
    bare="${bare%\"}"; bare="${bare#\"}"
    bare="${bare%\'}"; bare="${bare#\'}"
    if is_secret_name "$key" && is_placeholder "$bare"; then
      value="$(rand_secret 32)"
      changed="$changed $key"
    fi
    printf '%s=%s\n' "$key" "$value" >> "$tmp"
  done < "$ENV_FILE"
  mv "$tmp" "$ENV_FILE"
  chmod 600 "$ENV_FILE" 2>/dev/null
  if [ -n "$changed" ]; then ok "сгенерированы случайные значения:$changed"; else
    warn "секретных значений для генерации не нашлось — проверьте файл вручную"
  fi
  load_env "$ENV_FILE"
  note "дальше: ./forge.sh env check && ./forge.sh doctor"
}

_env_set() {
  case "${1:-}" in *=*) ;; *) bad "формат: ./forge.sh env set KEY=VALUE"; return 2 ;; esac
  local key="${1%%=*}" value="${1#*=}" tmp line found=0
  case "$key" in ''|*[!A-Za-z0-9_]*|[0-9]*) bad "недопустимое имя переменной: $key"; return 2 ;; esac
  [ -f "$ENV_FILE" ] || { : > "$ENV_FILE"; }
  chmod 600 "$ENV_FILE" 2>/dev/null
  tmp="$(mktemp)"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "$key="*) printf '%s=%s\n' "$key" "$value" >> "$tmp"; found=1 ;;
      *) printf '%s\n' "$line" >> "$tmp" ;;
    esac
  done < "$ENV_FILE"
  [ "$found" -eq 0 ] && printf '%s=%s\n' "$key" "$value" >> "$tmp"
  cp "$ENV_FILE" "$ENV_FILE.bak" 2>/dev/null
  mv "$tmp" "$ENV_FILE"
  chmod 600 "$ENV_FILE" 2>/dev/null
  ok "записано: $key=$(mask_value "$key" "$value")"
  note "применить: ./forge.sh restart"
}

_env_edit() {
  [ -f "$ENV_FILE" ] || { warn "файла нет — создаю из примера"; _env_init || return 1; }
  local ed="${EDITOR:-${VISUAL:-}}" c
  if [ -z "$ed" ]; then
    for c in nano vim vi; do has "$c" && { ed="$c"; break; }; done
  fi
  [ -z "$ed" ] && { bad "редактор не найден — задайте EDITOR"; return 1; }
  "$ed" "$ENV_FILE"
  ok "сохранено"
  note "проверить: ./forge.sh env check · применить: ./forge.sh restart"
}

cmd_env() {
  case "${1:-show}" in
    show|list)      _env_show ;;
    path)           echo "$ENV_FILE" ;;
    init)           shift; _env_init "$@" ;;
    set)            shift; _env_set "$@" ;;
    check|validate) _env_check ;;
    edit)           _env_edit ;;
    reload)         load_env; ok "перечитано: загружено $ENV_LOADED, пропущено $ENV_SKIPPED" ;;
    *)              echo "env show | init [--force] | set KEY=VALUE | check | edit | path | reload" ;;
  esac
}

cmd_completion() {
  case "${1:-bash}" in
    bash)
      cat <<'COMP'
# Дополнение для bash: eval "$(./forge.sh completion bash)"
_forge() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local cmds="status start stop restart logs open doctor install test lint build verify docker db env admin clean version help menu completion migrate backup restore api web dev prod scale smoke"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "$cmds" -- "$cur") )
  fi
}
complete -F _forge forge.sh ./forge.sh
COMP
      ;;
    zsh)
      cat <<'COMP'
# Дополнение для zsh: ./forge.sh completion zsh > ~/.zfunc/_forge
_arguments '1:command:(status start stop restart logs open doctor install test lint build verify docker db env admin clean version help menu completion migrate backup restore api web dev prod scale smoke)'
COMP
      ;;
    *) echo "bash | zsh" ;;
  esac
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
  dev                 поднять полный стек разработки (db + API + web) и проверить
  migrate             применить миграции
  backup / restore    резервная копия и восстановление БД
  admin <email>       создать первого администратора (пароль из FORGECRM_BOOTSTRAP_PASSWORD)

Развёртывание
  prod                поднять стек в контейнерах (production Compose)
  scale <N>           запустить N реплик API за балансировщиком (N >= 2)
  smoke               проверить живой инстанс по HTTP

Служебное
  docker up|down|logs|reset   контейнеры (данные сохраняются)
  db migrate|backup|restore   работа с базой
  clean [--hard]      убрать артефакты и логи (--hard: ещё .venv и кэши)
  version             версия скрипта и проекта
  menu                интерактивное меню
  completion [bash]   скрипт автодополнения

Конфигурация (.env)
  env                 показать переменные (секреты маскируются)
  env init [--force]  создать .env из .env.example, сгенерировать секреты
  env set KEY=VALUE   записать/обновить переменную
  env check           сверить .env с примером и найти заглушки
  env edit            открыть .env в редакторе
  env path            путь к используемому файлу
  env reload          перечитать файл

Флаги
  -q, --quiet         только предупреждения и ошибки
      --no-color      без цвета (то же даёт NO_COLOR=1)
      --json          машинный вывод для status
  -h, --help          эта справка
  -V, --version       версия

Переменные окружения
  PYTHON=python3.12   интерпретатор вместо python3
  PORT=9000           переопределить порт (приоритет выше .env)
  FORGE_ENV_FILE=...  другой файл конфигурации

Файл .env читается сам, без source: комментарии, кавычки, export, CRLF.
Значение, уже заданное в окружении, файлом не перетирается.
EOF
}

cmd_doctor() {
  say "Окружение"
  if has "$PYTHON"; then ok "$PYTHON: $($PYTHON -V 2>&1)"; else bad "$PYTHON не найден"; fi
  if has go;   then ok "go: $(go version 2>/dev/null | awk '{print $3}')"; else bad "go не найден (нужен для backend)"; fi
  if has node; then ok "node: $(node -v 2>/dev/null)"; else warn "node не найден (нужен для frontend)"; fi
  if has docker && docker compose version >/dev/null 2>&1; then ok "docker compose доступен"; else warn "docker compose недоступен (БД в контейнере)"; fi
  doctor_port
  [ -f frontend/node_modules/.package-lock.json ] && ok "frontend: зависимости установлены" || warn "frontend: npm ci не выполнялся"
  # Проекты без конфигурации (репозиторий артефактов) не должны получать
  # предупреждение о .env: ориентир — наличие .env.example в репозитории.
  if [ -f .env.example ]; then
    if [ -f "$ENV_FILE" ]; then
      ok "$(basename "$ENV_FILE"): загружено $ENV_LOADED переменных (не перетёрто: $ENV_SKIPPED)"
    else
      warn "$(basename "$ENV_FILE") нет — ./forge.sh env init"
    fi
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

_status_json() {
  local api=false web=false
  if [ -n "${PORT:-}" ] && port_busy "$PORT"; then api=true; fi
  if [ -n "${FRONTEND_PORT:-}" ] && port_busy "$FRONTEND_PORT"; then web=true; fi
  local port_json="null"
  [ -n "${PORT:-}" ] && port_json="$PORT"
  printf '{"name":"%s","port":%s,"url":"%s","api_running":%s,"web_running":%s}\n' \
    "$NAME" "$port_json" "${URL:-}" "$api" "$web"
}

cmd_status() {
  if [ "$JSON" -eq 1 ]; then _status_json; return 0; fi
  say "$NAME"
  local running=0
  if port_busy "$PORT"; then ok "API: порт $PORT слушается"; running=1
    http_ok "http://localhost:$PORT$HEALTH_PATH" && ok "API: $HEALTH_PATH 200" || warn "API: $HEALTH_PATH не отвечает"; fi
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
  start_services "$@" || die "запуск не удался (смотрите .run/*.log)"
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
  echo "конфигурация: $ENV_FILE ($([ -f "$ENV_FILE" ] && echo "${#ENV_LIST[@]} переменных" || echo "файла нет"))"
  echo "скрипт: forge.sh v$FILE_VERSION"
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
  start_db
  _spawn api "cd backend && FORGECRM_LOG_LEVEL=debug go run ./cmd/forgecrm-api"
  wait_http "http://localhost:$PORT$HEALTH_PATH" 120 api || return 1
  if [ -d frontend/node_modules ]; then _spawn web "cd frontend && npm run dev"; fi
}

# start_db brings the database container up and waits for it to accept
# connections. It is shared by `start`, `dev` and `api` so the readiness wait is
# not duplicated with a different timeout in each.
start_db() {
  if has docker && ! docker compose ps --status running 2>/dev/null | grep -q .; then
    say "База данных"; docker compose up -d db && ok "db запущена"
    for _ in $(seq 1 30); do docker compose exec -T db pg_isready -U forgecrm >/dev/null 2>&1 && break; sleep 1; done
  fi
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


# cmd_migrate applies pending migrations through the API binary. It goes through
# `make migrate` so the Compose project, the database name and the migration
# runner stay in one place; a second copy of that command here could drift.
cmd_migrate() {
  say "Миграции"
  run_make migrate
}

# cmd_backup writes a database dump and a tar of uploaded files. It delegates to
# scripts/backup.sh, which is the implementation the operations docs describe.
cmd_backup() {
  say "Резервная копия"
  [ -f scripts/backup.sh ] || die "scripts/backup.sh не найден"
  bash scripts/backup.sh "$@"
}

# cmd_restore restores a dump. scripts/restore.sh refuses to run without an
# explicit confirmation flag, and this wrapper does not add one: restoring over a
# live database destroys the current data, so the human decision stays the
# caller's, exactly as the script intends.
cmd_restore() {
  say "Восстановление"
  [ -f scripts/restore.sh ] || die "scripts/restore.sh не найден"
  bash scripts/restore.sh "$@"
}

# cmd_admin creates the first platform administrator. It mirrors scripts/setup.sh:
# the password comes from the environment (FORGECRM_BOOTSTRAP_PASSWORD) so it is
# never passed on a command line where it would land in the shell history, and the
# command is idempotent — creating an administrator that already exists is refused
# by the binary, not silently overwritten.
cmd_admin() {
  local email="${1:-}"
  if [ -z "$email" ]; then
    die "укажите адрес администратора: ./forge.sh admin <email>"
  fi
  if [ -z "${FORGECRM_BOOTSTRAP_PASSWORD:-}" ]; then
    bad "FORGECRM_BOOTSTRAP_PASSWORD не задан"
    note "пароль берётся из окружения, чтобы не попасть в историю команд:"
    note "  FORGECRM_BOOTSTRAP_PASSWORD='<сильный пароль>' ./forge.sh admin $email"
    return 2
  fi
  say "Администратор"
  has docker || die "docker не найден (создание администратора идёт через контейнер api)"
  docker compose run --rm --no-deps api -bootstrap-admin -bootstrap-email "$email"
}

# cmd_api / cmd_web run one side of the stack on its own. They exist because the
# development loop often needs the API without the frontend (or the reverse), and
# starting both to then stop one is wasted work.
cmd_api() {
  require_tools || die "не хватает инструментов"
  start_db
  say "API"
  _spawn api "cd backend && FORGECRM_LOG_LEVEL=debug go run ./cmd/forgecrm-api"
  wait_http "http://localhost:$PORT$HEALTH_PATH" 120 api || return 1
  ok "API отвечает на http://localhost:$PORT$HEALTH_PATH"
}

cmd_web() {
  if [ ! -d frontend/node_modules ]; then die "frontend: нет node_modules (./forge.sh install)"; fi
  say "Frontend"
  _spawn web "cd frontend && npm run dev"
  ok "frontend запускается на http://localhost:$FRONTEND_PORT"
}

# cmd_smoke checks a running instance over HTTP, without touching the database.
cmd_smoke() {
  say "Smoke"
  local okall=0
  if http_ok "http://localhost:$PORT$HEALTH_PATH"; then ok "GET $HEALTH_PATH 200"; else bad "GET $HEALTH_PATH не отвечает"; okall=1; fi
  if http_ok "http://localhost:$PORT$READY_PATH"; then ok "GET $READY_PATH 200"; else warn "GET $READY_PATH не отвечает (нет готовности)"; fi
  if [ -n "${FRONTEND_PORT:-}" ] && port_busy "$FRONTEND_PORT"; then
    ok "web: порт $FRONTEND_PORT слушается"
  else
    warn "web не запущен на порту $FRONTEND_PORT"
  fi
  return $okall
}

# cmd_dev brings up the full development stack (database + API + frontend).
cmd_dev() {
  require_tools || die "не хватает инструментов"
  start_db
  start_services "$@" || die "запуск не удался (смотрите .run/*.log)"
  cmd_status
  cmd_smoke || warn "smoke нашёл замечания"
}

# cmd_prod runs the containers the way a deployment does: the production Compose
# file, detached. It refuses to invent a password, so a missing .env is an error
# rather than a silently insecure stack.
cmd_prod() {
  has docker || die "docker не найден"
  if [ ! -f .env ]; then die ".env не найден — ./forge.sh env init"; fi
  say "Production"
  docker compose -f docker-compose.yml up -d --build
  ok "стек поднят; проверьте ./forge.sh status"
}

# cmd_scale runs several stateless API replicas behind the reverse proxy. It uses
# the multi-replica Compose file, because a single-file Compose project cannot
# scale a service that publishes a fixed host port.
cmd_scale() {
  local n="${1:-2}"
  has docker || die "docker не найден"
  case "$n" in ''|*[!0-9]*) die "укажите число реплик: ./forge.sh scale 3";; esac
  if [ "$n" -lt 2 ]; then die "scale имеет смысл от 2 реплик; для одной используйте ./forge.sh start"; fi
  [ -f docker-compose.multireplica.yml ] || die "docker-compose.multireplica.yml не найден"
  say "Масштабирование API до $n реплик"
  docker compose -f docker-compose.yml -f docker-compose.multireplica.yml up -d --scale api="$n"
  ok "запущено реплик: $n"
}


# cmd_commands prints every command the dispatcher accepts, one per line. It
# exists so a check can compare the help text with the dispatcher: the two used to
# drift (`migrate`, `backup`, `restore`, `api`, `web` were documented but not
# dispatched), and a command a user can read about but not run is a defect.
cmd_commands() {
  cat <<'CMDS'
help
menu
status
ps
start
up
stop
down
restart
logs
log
open
doctor
install
setup
test
lint
build
verify
docker
db
migrate
backup
restore
api
web
dev
prod
scale
smoke
admin
env
config
completion
clean
version
commands
CMDS
}


main() {
  local cmd="" a
  while [ $# -gt 0 ]; do
    a="$1"
    case "$a" in
      -h|--help)    cmd=help; shift; break ;;
      -V|--version) cmd=version; shift; break ;;
      -q|--quiet)   QUIET=1; shift ;;
      --no-color)   C_R=''; C_G=''; C_Y=''; C_B=''; C_D=''; C_0=''; shift ;;
      --json)       JSON=1; shift ;;
      --)           shift; break ;;
      -*)           bad "неизвестный флаг: $a"; note "справка: ./forge.sh help"; exit 2 ;;
      *)            cmd="$a"; shift; break ;;
    esac
  done
  [ -z "$cmd" ] && cmd="menu"

  load_env   # .env влияет на порт, адреса и на все команды ниже

  # Порт и адреса: аргумент окружения важнее .env, .env важнее встроенного.
  case "$cmd" in test|lint|verify|build|checksums) HERMETIC=1 ;; esac

  PORT="${PORT:-$PORT_DEFAULT}"
  FRONTEND_PORT="${FRONTEND_PORT:-$FRONTEND_PORT_DEFAULT}"
  URL="${URL:-${URL_TEMPLATE//@PORT@/$PORT}}"
  FRONTEND_URL="${FRONTEND_URL:-${FRONTEND_URL_TEMPLATE//@PORT@/$FRONTEND_PORT}}"
  export PORT URL

  case "$cmd" in
    help)           cmd_help ;;
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
    migrate)        cmd_migrate ;;
    backup)         cmd_backup "$@" ;;
    restore)        cmd_restore "$@" ;;
    api)            cmd_api ;;
    web)            cmd_web ;;
    smoke)          cmd_smoke ;;
    dev)            cmd_dev "$@" ;;
    prod)           cmd_prod ;;
    scale)          cmd_scale "$@" ;;
    admin)          cmd_admin "$@" ;;
    env|config)     cmd_env "$@" ;;
    completion)     cmd_completion "$@" ;;
    commands)       cmd_commands ;;
    clean)          cmd_clean "$@" ;;
    version)        cmd_version ;;
    *) bad "неизвестная команда: $cmd"; note "справка: ./forge.sh help"; exit 2 ;;
  esac
}

main "$@"
