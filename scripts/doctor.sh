#!/usr/bin/env bash
#
# Проверяет, что хост способен собрать и запустить ForgeCRM.
# Ничего не устанавливает и не меняет в системе: только сообщает о расхождениях.
#
#   ./scripts/doctor.sh            # полная проверка
#   ./scripts/doctor.sh --ci       # без docker-проверок (уровень A/B)
set -uo pipefail

expected_go="1.26.10"          # должно совпадать с backend/go.mod и ci.yml
expected_node="22"             # должно совпадать с ci.yml
failures=0
warnings=0

ok()   { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad()  { printf '  \033[31mfail\033[0m  %s\n' "$1"; failures=$((failures + 1)); }
warn() { printf '  \033[33mwarn\033[0m  %s\n' "$1"; warnings=$((warnings + 1)); }
head_() { printf '\n%s\n' "$1"; }

mode="${1:-full}"

head_ "Инструменты"
for tool in git make; do
  if command -v "$tool" >/dev/null 2>&1; then ok "$tool ($(command -v $tool))"; else bad "$tool не найден"; fi
done

if command -v go >/dev/null 2>&1; then
  go_version="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  if [ "$go_version" = "$expected_go" ]; then
    ok "go $go_version (совпадает с go.mod)"
  else
    warn "go $go_version, ожидается $expected_go — сборка воспроизводима только на закреплённой версии"
  fi
else
  bad "go не найден (нужен для уровней A/B)"
fi

if command -v node >/dev/null 2>&1; then
  node_major="$(node -v | sed 's/^v//' | cut -d. -f1)"
  if [ "$node_major" = "$expected_node" ]; then ok "node $(node -v)"; else warn "node $(node -v), ожидается $expected_node.x"; fi
  command -v npm >/dev/null 2>&1 && ok "npm $(npm -v)" || bad "npm не найден"
else
  bad "node не найден (нужен для сборки frontend)"
fi

head_ "Конфигурация"
if [ -f .env ]; then
  ok ".env присутствует"
  grep -q '^FORGECRM_SECRET_KEY=.\+' .env && ok "FORGECRM_SECRET_KEY задан" || bad "FORGECRM_SECRET_KEY пуст — интеграционные секреты не расшифруются"
  perms="$(stat -c '%a' .env 2>/dev/null || stat -f '%A' .env 2>/dev/null)"
  [ "$perms" = "600" ] && ok ".env с правами 600" || warn ".env доступен другим пользователям (права: $perms)"
else
  warn ".env отсутствует — создайте из .env.example (make setup)"
fi

head_ "Сервисы"
if [ "$mode" = "--ci" ]; then
  warn "проверки docker пропущены (--ci)"
else
  if command -v docker >/dev/null 2>&1; then
    ok "docker $(docker --version 2>/dev/null | awk '{print $3}' | tr -d ',')"
    if docker info >/dev/null 2>&1; then
      ok "демон docker доступен"
      docker compose version >/dev/null 2>&1 && ok "docker compose plugin" || bad "docker compose plugin не найден"
      # Здоровье контейнеров, если стек уже поднят. Только чтение.
      if docker compose ps --status running --services 2>/dev/null | grep -qx postgres; then
        ok "postgres запущен"
        docker compose exec -T postgres pg_isready -U forgecrm >/dev/null 2>&1 \
          && ok "postgres принимает соединения" || bad "postgres запущен, но не отвечает"
      else
        warn "стек не запущен — интеграционные уровни недоступны (make compose-up)"
      fi
    else
      bad "демон docker недоступен (нет прав или не запущен)"
    fi
  else
    warn "docker не найден — доступны только уровни A/B"
  fi
fi

head_ "Порты"
for port in 8080 5432; do
  if command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | grep -q ":$port "; then
    warn "порт $port занят"
  else
    ok "порт $port свободен"
  fi
done

printf '\n'
if [ "$failures" -gt 0 ]; then
  printf '\033[31m%d проверок провалено\033[0m, предупреждений: %d\n' "$failures" "$warnings"
  exit 1
fi
printf '\033[32mГотово к работе\033[0m, предупреждений: %d\n' "$warnings"
