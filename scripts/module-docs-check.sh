#!/usr/bin/env bash
#
# Module portability metadata gate.
#
# The extraction harness (scripts/extract-test) proves a module can be lifted out
# of the monolith. This gate proves the *contract* of that portability is written
# down: every module the harness extracts must ship a manifest.yaml that names its
# dependencies, environment, data and public API, plus a MODULE.md with the
# documented sections a person needs to move it.
#
# A module without metadata is portable in principle and undiscoverable in
# practice: the next person cannot tell what it needs or how to detach it without
# reading all of its code. This check makes the metadata a fact, the same way the
# harness makes extraction a fact.
#
# Usage: scripts/module-docs-check.sh
# Exit status is non-zero if any extracted module lacks valid metadata.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
MODULES_FILE="$ROOT/scripts/extract-test/modules.txt"
BACKEND="$ROOT/backend"

ok()   { printf '   ok    %s\n' "$*"; }
bad()  { printf '   FAIL  %s\n' "$*" >&2; }
info() { printf '   %s\n' "$*"; }

[ -f "$MODULES_FILE" ] || { bad "missing $MODULES_FILE"; exit 1; }

# Required keys in manifest.yaml. Kept minimal on purpose: every key here is one
# a person detaching the module actually needs.
required_keys=(
  name version purpose language runtime
  public_api depends_on requires_env database portable extract_status
)

# Required headings in MODULE.md (subset of the 11-section spec that matters for
# portability).
required_headings=(
  "## 1. Назначение и границы"
  "## 3. Контракт"
  "## 6. Зависимости"
  "## 7. Конфигурация"
  "## 8. Сборка и запуск"
  "## 9. Перенос в другой проект"
  "## 11. Ограничения"
)

fail=0
checked=0
missing=0

# Map a modules.txt entry to its internal package path. Leaf modules under
# importx are stored as `safe`/`parsers`.
module_dir() {
  case "$1" in
    safe)    printf 'internal/importx/safe' ;;
    parsers) printf 'internal/importx/parsers' ;;
    *)       printf 'internal/%s' "$1" ;;
  esac
}

while IFS= read -r line; do
  m="${line%%#*}"
  m="$(printf '%s' "$m" | tr -d '[:space:]')"
  [ -z "$m" ] && continue
  checked=$((checked + 1))

  dir="$BACKEND/$(module_dir "$m")"
  manifest="$dir/manifest.yaml"
  moduledoc="$dir/MODULE.md"

  if [ ! -f "$manifest" ]; then
    bad "$m: нет $manifest"
    missing=$((missing + 1)); fail=1; continue
  fi
  if [ ! -f "$moduledoc" ]; then
    bad "$m: нет $moduledoc"
    missing=$((missing + 1)); fail=1; continue
  fi

  local_fail=0
  for k in "${required_keys[@]}"; do
    if ! grep -qE "^${k}:" "$manifest"; then
      bad "$m: manifest.yaml без ключа '${k}:'"
      local_fail=1
    fi
  done
  for h in "${required_headings[@]}"; do
    if ! grep -qF "$h" "$moduledoc"; then
      bad "$m: MODULE.md без раздела «$h»"
      local_fail=1
    fi
  done

  if [ "$local_fail" -eq 0 ]; then
    ok "$m: метаданные переносимости на месте"
  else
    fail=1
  fi
done < "$MODULES_FILE"

printf '\nПроверено модулей: %d; без метаданных: %d\n' "$checked" "$missing"

if [ "$fail" -ne 0 ]; then
  info "Добавьте manifest.yaml и MODULE.md рядом с кодом модуля (см. docs/architecture/PORTABLE_MODULES.md)."
  exit 1
fi

ok "у каждого извлекаемого модуля есть метаданные переносимости"
