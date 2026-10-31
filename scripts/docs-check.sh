#!/usr/bin/env bash
#
# Проверяет, что документация существует, не содержит битых ссылок и покрывает
# обязательные разделы. Документация — часть контракта, поэтому проверяется в CI.
#
#   ./scripts/docs-check.sh
set -uo pipefail
failures=0
ok()  { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad() { printf '  \033[31mfail\033[0m  %s\n' "$1"; failures=$((failures + 1)); }

printf '\nОбязательные документы\n'
required=(
  README.md BUILD.md SECURITY.md
  docs/index.md
  docs/ARCHITECTURE.md
  docs/architecture/system-context.md
  docs/architecture/components.md
  docs/architecture/dependency-rules.md
  docs/architecture/data-flow.md
  docs/development/setup.md docs/development/testing.md
  docs/development/code-style.md docs/development/debugging.md
  docs/database/schema.md docs/database/migrations.md docs/database/backup-restore.md
  docs/api/conventions.md
  docs/operations/deployment.md docs/operations/monitoring.md docs/operations/upgrades.md
  docs/deployment/environment-variables.md
  docs/security/threat-model.md docs/security/permissions.md
  docs/modules/module-template.md docs/modules/crm.md
  docs/verification/matrix.md
  docs/adr/README.md
)
for f in "${required[@]}"; do
  [ -s "$f" ] && ok "$f" || bad "$f отсутствует или пуст"
done

printf '\nСпецификации модулей\n'
shopt -s nullglob
modules=(docs/modules/*.md)
headings=(
  "Назначение" "Сценарии" "Контракт" "Данные" "Права"
  "Зависимости" "Конфигурация" "Сборка" "Тесты" "Наблюдаемость" "Ограничения"
)
module_fail=0
for m in "${modules[@]}"; do
  case "$m" in *module-template.md) continue ;; esac
  for h in "${headings[@]}"; do
    if ! grep -qi "$h" "$m"; then
      bad "$m: нет раздела «$h»"
      module_fail=1
    fi
  done
done
[ "$module_fail" -eq 0 ] && ok "все спецификации модулей содержат 11 обязательных разделов"

printf '\nСсылки\n'
# Относительные ссылки во всей документации должны вести на существующие файлы.
broken=0
while IFS= read -r line; do
  file="${line%%:*}"
  target="$(printf '%s' "$line" | sed -n 's/.*](\([^)#]*\).*/\1/p')"
  [ -z "$target" ] && continue
  case "$target" in http*|mailto:*|\#*|"") continue ;; esac
  base="$(dirname "$file")"
  if [ ! -e "$base/$target" ] && [ ! -e "$target" ]; then
    bad "$file -> $target (битая ссылка)"
    broken=$((broken + 1))
  fi
done < <(grep -rnoE '\[[^]]*\]\([^)]+\)' --include='*.md' \
  --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist \
  --exclude-dir=coverage . 2>/dev/null)
[ "$broken" -eq 0 ] && ok "битых относительных ссылок нет"

printf '\nСчётчики схемы\n'
# Числа в docs/database/schema.md обязаны совпадать с кодом: миграции — файлы в
# backend/internal/migrations, таблицы — из CREATE TABLE в них. Без этой проверки
# документ тихо отстаёт от схемы, и «65 таблиц, 19 миграций» переживает три
# миграции, как это уже было.
schema=docs/database/schema.md
if [ -s "$schema" ]; then
  doc_migrations=$(grep -oE '\*\*[0-9]+ миграц' "$schema" | grep -oE '[0-9]+' | head -1)
  doc_tables=$(grep -oE '\*\*[0-9]+ таблиц' "$schema" | grep -oE '[0-9]+' | head -1)
  real_migrations=$(ls backend/internal/migrations/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
  real_tables=$(grep -rhoE 'CREATE TABLE [a-z_]+' backend/internal/migrations/*.up.sql 2>/dev/null | awk '{print $3}' | sort -u | wc -l | tr -d ' ')
  [ "$doc_migrations" = "$real_migrations" ] \
    && ok "schema.md: миграций $real_migrations" \
    || bad "schema.md говорит «$doc_migrations миграций», в коде $real_migrations"
  [ "$doc_tables" = "$real_tables" ] \
    && ok "schema.md: таблиц $real_tables" \
    || bad "schema.md говорит «$doc_tables таблиц», в коде $real_tables"
fi

printf '\nМодули выделения\n'
# Число «READY»-модулей повторяется в нескольких документах. Оно уже отставало от
# набора на несколько модулей, поэтому проверяется здесь: набор
# `scripts/extract-test/modules.txt` — источник истины, а каждый список в
# документации обязан с ним совпадать. Пропущенный или лишний модуль ловится в CI,
# а не живёт до следующего ревью.
extract_harness="scripts/extract-test/modules.txt"
if [ -s "$extract_harness" ]; then
  real_modules=$(grep -vcE '^\s*#|^\s*$' "$extract_harness")
  # modules.txt хранит листья importx как `safe`/`parsers`; документы называют их
  # полным путём `importx/safe`/`importx/parsers`, поэтому нормализуем для сравнения.
  norm='s/^safe$/importx\/safe/;s/^parsers$/importx\/parsers/'
  listed=$(grep -vE '^\s*#|^\s*$' "$extract_harness" | sed "$norm" | sort -u)

  # check_module_list <doc> <label> — сравнивает список модулей, извлечённый из
  # stdin, с набором и печатает результат. Пустой ввод означает, что список не
  # найден: это тоже расхождение, потому что документ обещает его содержать.
  check_module_list() {
    local doc="$1" label="$2" doc_list
    doc_list=$(sed "$norm" | sort -u)
    if [ -z "$doc_list" ]; then
      bad "$doc: список модулей «$label» не найден"
      return
    fi
    local missing extra
    missing=$(comm -23 <(printf '%s\n' "$listed") <(printf '%s\n' "$doc_list") | tr '\n' ' ')
    extra=$(comm -13 <(printf '%s\n' "$listed") <(printf '%s\n' "$doc_list") | tr '\n' ' ')
    if [ -z "$missing" ] && [ -z "$extra" ]; then
      ok "$doc: список модулей совпадает с набором ($real_modules)"
    else
      bad "$doc: расхождение с набором (нет: ${missing:-—}; лишние: ${extra:-—})"
    fi
  }

  # MODULES.md: прозаический список между «Статус Extract» и ссылкой на EXTRACT_READY.
  check_module_list docs/MODULES.md "Статус Extract" < <(
    sed -n '/Статус Extract/,/EXTRACT_READY\.md/p' docs/MODULES.md 2>/dev/null \
      | grep -oE '`[a-z][a-z/-]*`' | tr -d '`' | grep -vxE 'yes|scripts/.*')

  # EXTRACT_READY.md: строки сводной таблицы, помеченные READY.
  check_module_list docs/EXTRACT_READY.md "сводка READY" < <(
    awk '/^## Сводка/,0' docs/EXTRACT_READY.md 2>/dev/null \
      | grep -E '^\| `[^`]+` \| \*\*READY' | sed -E 's/^\| `([^`]+)`.*/\1/')

  # EXTRACTION_TESTING.md: прозаический список после «перечисляет сегодня».
  check_module_list docs/EXTRACTION_TESTING.md "перечисляет сегодня" < <(
    sed -n '/перечисляет сегодня/,/extract-test\/README/p' docs/EXTRACTION_TESTING.md 2>/dev/null \
      | grep -oE '`[a-z][a-z/-]*`' | tr -d '`' | grep -vxE 'yes|scripts/.*')
fi

printf '\nМатрица проверок\n'
matrix=docs/verification/matrix.md
if [ -s "$matrix" ]; then
  # Каждая строка матрицы обязана ссылаться на существующий скрипт или цель make.
  missing=0
  while IFS= read -r cmd; do
    # Из `make target` / `./scripts/x.sh` вырезаем саму команду без кавычек.
    cmd="${cmd//\`/}"
    case "$cmd" in
      make\ *)
        target="${cmd#make }"; target="${target%% *}"
        if ! grep -qE "^${target}:" Makefile; then
          bad "матрица ссылается на отсутствующую цель: make $target"
          missing=$((missing+1))
        fi
        ;;
      ./*)
        if [ ! -x "$cmd" ]; then
          bad "матрица ссылается на отсутствующий скрипт: $cmd"
          missing=$((missing+1))
        fi
        ;;
    esac
  done < <(grep -oE '`(make [a-z-]+|\./scripts/[a-z-]+\.sh)`' "$matrix" | sort -u)
  [ "$missing" -eq 0 ] && ok "все команды из матрицы существуют"
fi

printf '\n'
if [ "$failures" -gt 0 ]; then printf '\033[31m%d проблем в документации\033[0m\n' "$failures"; exit 1; fi
printf '\033[32mДокументация согласована\033[0m\n'
