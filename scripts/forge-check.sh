#!/usr/bin/env bash
#
# Проверяет, что `forge.sh help` и диспетчер команд не разошлись.
#
# Мотивация. Раньше `help` перечислял `migrate`, `backup`, `restore`, `api`,
# `web`, а диспетчер их не знал: пользователь читал про команду, вводил её и
# получал «неизвестная команда». Это не косметика — скрипт обещал работу,
# которой не было. Проверка читает список команд из самого скрипта (`commands`)
# и сверяет его с текстом справки, поэтому расхождение ловится в CI, а не у
# пользователя.
#
#   ./scripts/forge-check.sh

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
FORGE="$REPO_ROOT/forge.sh"

failures=0
ok()  { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad() { printf '  \033[31mfail\033[0m  %s\n' "$1"; failures=$((failures + 1)); }

printf '\nforge.sh\n'

[ -f "$FORGE" ] || { bad "forge.sh не найден"; exit 1; }

# Синтаксис: скрипт должен разбираться оболочкой.
if bash -n "$FORGE" 2>/dev/null; then
  ok "bash -n: синтаксис корректен"
else
  bad "bash -n: синтаксическая ошибка"
fi

# Список команд из скрипта. Пустой вывод — тоже ошибка: значит, интроспекция
# сломана, и проверка ниже стала бы вакуумной.
commands="$(bash "$FORGE" commands 2>/dev/null || true)"
if [ -z "$commands" ]; then
  bad "forge.sh commands не вернул список команд"
  printf '\nforge-check: %d замечаний\n' "$failures"
  exit 1
fi
ok "forge.sh commands: $(printf '%s\n' "$commands" | wc -l | tr -d ' ') команд"

# Каждая команда, упомянутая в справке как первое слово строки, должна
# существовать. Строки справки с несколькими командами через «|» тоже
# разбираются: проверяется каждое слово из такой строки.
help="$(bash "$FORGE" help 2>/dev/null || true)"
if [ -z "$help" ]; then
  bad "forge.sh help не вернул текст справки"
else
  missing=0
  while IFS= read -r word; do
    [ -n "$word" ] || continue
    # Флаги и служебные слова справки команд не образуют.
    case "$word" in -*|env|path|init|set|show|check|edit|reload|status) continue ;; esac
    if ! printf '%s\n' "$commands" | grep -qx "$word"; then
      bad "help упоминает «$word», но диспетчер её не знает"
      missing=1
    fi
  done < <(
    printf '%s\n' "$help" \
      | grep -oE '^  [a-z][a-z-]*( \| [a-z][a-z-]*)*' \
      | sed 's/^  //; s/ | /\n/g'
  )
  [ "$missing" -eq 0 ] && ok "help и диспетчер согласованы"
fi

# Каждая команда из списка должна присутствовать в блоке диспетчеризации
# (`case "$cmd" in`). Список команд и `case` — два места, которые легко
# расходятся; проверка читает сам блок и сверяет его со списком. Команды не
# запускаются: часть из них меняет состояние, и проверка не должна ничего
# поднимать или останавливать.
dispatch_block="$(sed -n '/case "\$cmd" in/,/^  esac/p' "$FORGE")"
if [ -z "$dispatch_block" ]; then
  bad "не найден блок диспетчеризации в forge.sh"
else
  not_dispatched=0
  while IFS= read -r c; do
    [ -n "$c" ] || continue
    # В блоке команды перечислены как `name)` или `a|b)`.
    if ! printf '%s' "$dispatch_block" | grep -qE "(^|[ (|])${c}[|)]"; then
      bad "«$c» числится в списке, но отсутствует в диспетчере"
      not_dispatched=1
    fi
  done <<< "$commands"
  [ "$not_dispatched" -eq 0 ] && ok "каждая команда есть в диспетчере"
fi

# Автодополнение должно знать те же команды, что и диспетчер: иначе новые
# команды не подсказываются, хотя работают.
comp="$(bash "$FORGE" completion bash 2>/dev/null || true)"
if printf '%s\n' "$comp" | grep -q 'local cmds='; then
  cmds_line="$(printf '%s\n' "$comp" | grep 'local cmds=' | head -1)"
  missing_comp=0
  for c in migrate backup restore api web dev prod scale smoke admin; do
    printf '%s' "$cmds_line" | grep -qw "$c" || { bad "completion не знает «$c»"; missing_comp=1; }
  done
  [ "$missing_comp" -eq 0 ] && ok "completion покрывает команды"
else
  bad "completion bash не содержит списка команд"
fi

printf '\n'
if [ "$failures" -eq 0 ]; then
  printf '\033[32mforge-check: ok\033[0m\n'
  exit 0
fi
printf '\033[31mforge-check: %d замечаний\033[0m\n' "$failures"
exit 1
