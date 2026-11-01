#!/usr/bin/env bash
#
# Проверяет, что числа и утверждения в живой документации совпадают с кодом.
#
# Каждая проверка читает число из источника истины (Go-каталог RBAC, файл
# LICENSE, сгенерированный openapi.json), а не из второго захардкоженного
# значения, и падает с ненулевым кодом, когда документ разошёлся с кодом.
#
#   ./scripts/check-doc-claims.sh
#
# Документы-снимки (PROJECT_AUDIT.md, ROADMAP.md) намеренно не проверяются: их
# числа — запись измерения на конкретный коммит, а не текущее состояние.
set -uo pipefail
# Запускается из корня репозитория (так делают make и CI), но не зависит от cwd.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}" || exit 1

failures=0
ok()  { printf '  \033[32mok\033[0m    %s\n' "$1"; }
bad() { printf '  \033[31mfail\033[0m  %s\n' "$1"; failures=$((failures + 1)); }

rbac_file="backend/internal/rbac/rbac.go"
openapi_file="docs/api/openapi.json"
readme="README.md"

printf '\nRBAC: каталог разрешений\n'
# Источник истины — сам каталог: ключи карты `catalog` в rbac.go. Это тот же
# набор, что возвращает len(rbac.All()), и он не может совпасть с документом
# случайно, потому что извлекается из кода, а не из второго числа в документе.
if [ -s "$rbac_file" ]; then
  catalog=$(sed -n '/^var catalog = map\[Permission\]struct{}{/,/^}/p' "$rbac_file" \
    | grep -oE 'Perm[A-Za-z0-9]+:' | wc -l | tr -d ' ')
  if [ "$catalog" -gt 0 ]; then
    ok "каталог RBAC: $catalog разрешений"
    # README объявляет число в строке статуса «RBAC: N разрешени...».
    readme_rbac=$(grep -oE 'RBAC: [0-9]+ разрешени' "$readme" | grep -oE '[0-9]+' | head -1)
    if [ -z "$readme_rbac" ]; then
      bad "$readme: не найдено утверждение «RBAC: N разрешени…»"
    elif [ "$readme_rbac" = "$catalog" ]; then
      ok "$readme: RBAC $readme_rbac == каталог $catalog"
    else
      bad "$readme говорит «$readme_rbac разрешений», в каталоге $catalog"
    fi
  else
    bad "не удалось извлечь каталог RBAC из $rbac_file"
  fi
else
  bad "нет $rbac_file"
fi

printf '\nOpenAPI: счётчики против спецификации\n'
if [ -s "$openapi_file" ]; then
  # Числа берутся из сгенерированного openapi.json, а не из кода-генератора:
  # документ, который читает человек, обязан совпадать с артефактом, который
  # видит потребитель API.
  json_paths=$(awk '/^  "paths": \{/{f=1; next} f && /^  \}/{exit} f && /^    "/{n++} END{print n+0}' "$openapi_file")
  json_ops=$(awk '/^  "paths": \{/{f=1; next} f && /^  \}/{exit} f && /^      "(get|post|put|patch|delete)": \{/{n++} END{print n+0}' "$openapi_file")
  if [ "$json_paths" -gt 0 ] && [ "$json_ops" -ge "$json_paths" ]; then
    ok "openapi.json: $json_paths путей, $json_ops операций"
    # README объявляет оба числа в строке статуса API.
    readme_paths=$(grep -oE '[0-9]+ путей' "$readme" | grep -oE '[0-9]+' | head -1)
    readme_ops=$(grep -oE '[0-9]+ операции' "$readme" | grep -oE '[0-9]+' | head -1)
    if [ -z "$readme_paths" ] || [ -z "$readme_ops" ]; then
      bad "$readme: не найдено утверждение «N путей, M операции»"
    elif [ "$readme_paths" = "$json_paths" ] && [ "$readme_ops" = "$json_ops" ]; then
      ok "$readme: API $readme_paths путей / $readme_ops операций совпадает со спецификацией"
    else
      bad "$readme говорит «$readme_paths путей / $readme_ops операций», в спецификации $json_paths / $json_ops"
    fi
  else
    bad "не удалось посчитать пути/операции в $openapi_file"
  fi
else
  bad "нет $openapi_file"
fi

printf '\nЛицензия: файл против заявлений в документации\n'
# Источник истины — наличие файла LICENSE и его первая непустая строка.
if [ -s LICENSE ]; then
  lic_kind=$(grep -m1 -oE 'MIT License|Apache License|GNU (GENERAL|LESSER)|BSD' LICENSE)
  if [ -z "$lic_kind" ]; then
    bad "LICENSE есть, но не распознан как MIT/Apache/BSD"
  else
    ok "LICENSE: $lic_kind"
  fi
  # README и SECURITY обязаны называть MIT, раз файл — MIT. Совпадение ищется
  # по границе слова: без \b «MIT» ловится внутри «LI-MIT-» в именах вроде
  # FORGECRM_RATE_LIMIT_STORE, и сломанное утверждение проходит проверку.
  if [ "$lic_kind" = "MIT License" ]; then
    grep -qEi 'лиценз[^.]*\bMIT\b|под лицензией \bMIT\b' "$readme" \
      && ok "$readme: заявляет MIT" \
      || bad "$readme: не заявляет лицензию MIT, хотя LICENSE — MIT"
    grep -qEi 'лиценз[^.]*\bMIT\b|под лицензией \bMIT\b' SECURITY.md \
      && ok "SECURITY.md: заявляет MIT" \
      || bad "SECURITY.md: не заявляет лицензию MIT, хотя LICENSE — MIT"
  fi
else
  # Нет файла — документы не должны утверждать, что лицензия выбрана.
  bad "нет файла LICENSE в корне"
  if grep -qEi 'лиценз[^.]*\bMIT\b' "$readme" SECURITY.md 2>/dev/null; then
    bad "документы заявляют MIT, но файла LICENSE нет"
  fi
fi

printf '\n'
if [ "$failures" -gt 0 ]; then printf '\033[31m%d проблем в утверждениях документации\033[0m\n' "$failures"; exit 1; fi
printf '\033[32mУтверждения документации совпадают с кодом\033[0m\n'
