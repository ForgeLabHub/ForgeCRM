#!/usr/bin/env python3
"""Generate portability metadata (manifest.yaml + MODULE.md) for extracted modules.

The metadata is derived from the code, not written from memory: the dependency
list comes from `go list`, the public API from `go doc`, the purpose from the
package doc comment, and the environment keys from the FORGECRM_* literals the
package actually reads. A generator is used because 36 hand-written manifests
would drift from the code the moment a module gains an import; running this
again is the way to keep them honest.

It is idempotent: it rewrites both files for every module in modules.txt. Review
the diff before committing.

Usage:
  scripts/gen-module-docs.py            # write metadata for every module
  scripts/gen-module-docs.py crm rbac   # only the named modules
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BACKEND = os.path.join(ROOT, "backend")
MODULES_FILE = os.path.join(ROOT, "scripts", "extract-test", "modules.txt")
MODULE_PATH = "github.com/geff1778/forgecrm/"


def module_dir(name: str) -> str:
    if name == "safe":
        return "internal/importx/safe"
    if name == "parsers":
        return "internal/importx/parsers"
    return f"internal/{name}"


def read_modules() -> list[str]:
    out = []
    with open(MODULES_FILE, encoding="utf-8") as fh:
        for line in fh:
            line = line.split("#", 1)[0].strip()
            if line:
                out.append(line)
    return out


def go_list_graph() -> dict[str, dict]:
    """Import path -> {imports, gofiles, doc, name} for the whole module."""
    raw = subprocess.check_output(
        ["go", "list", "-json", "./..."],
        cwd=BACKEND,
        text=True,
        env={**os.environ, "GOTOOLCHAIN": "local"},
    )
    dec = json.JSONDecoder()
    graph = {}
    idx = 0
    while idx < len(raw):
        while idx < len(raw) and raw[idx] in " \t\r\n":
            idx += 1
        if idx >= len(raw):
            break
        obj, end = dec.raw_decode(raw, idx)
        idx = end
        graph[obj["ImportPath"]] = obj
    return graph


def internal_deps(pkg: dict) -> list[str]:
    deps = []
    for imp in pkg.get("Imports", []):
        if imp.startswith(MODULE_PATH + "internal/"):
            rest = imp[len(MODULE_PATH + "internal/"):]
            top = rest.split("/", 1)[0]
            if imp.endswith("/safe"):
                top = "safe"
            elif imp.endswith("/parsers"):
                top = "parsers"
            deps.append(top)
    return sorted(set(deps))


def external_deps(pkg: dict) -> list[str]:
    """Third-party (non-stdlib, non-internal) direct imports."""
    deps = []
    for imp in pkg.get("Imports", []):
        if imp.startswith(MODULE_PATH):
            continue
        head = imp.split("/", 1)[0]
        if "." in head:  # a domain name means a third-party module
            deps.append(imp)
    return sorted(set(deps))


def package_doc(pkg: dict) -> str:
    doc = (pkg.get("Doc") or "").strip()
    if not doc:
        return "Модуль ForgeCRM (документация пакета не задана в коде)."
    # First paragraph only, collapsed to one line.
    first = re.split(r"\n\s*\n", doc)[0]
    return " ".join(first.split())


def exported_api(name: str) -> list[str]:
    """Exported types and top-level funcs, via go doc."""
    path = f"./{module_dir(name)}"
    try:
        out = subprocess.check_output(
            ["go", "doc", path],
            cwd=BACKEND,
            text=True,
            stderr=subprocess.DEVNULL,
            env={**os.environ, "GOTOOLCHAIN": "local"},
        )
    except subprocess.CalledProcessError:
        return []
    items = []
    for line in out.splitlines():
        m = re.match(r"^func (\w+)", line)
        if m and m.group(1)[0].isupper():
            items.append(f"func {m.group(1)}")
            continue
        m = re.match(r"^type (\w+)", line)
        if m and m.group(1)[0].isupper():
            items.append(f"type {m.group(1)}")
    return items


def env_keys(name: str) -> list[str]:
    """FORGECRM_* literals the package reads, from its source."""
    keys = set()
    d = os.path.join(BACKEND, module_dir(name))
    for dirpath, _dirs, files in os.walk(d):
        for fn in files:
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            with open(os.path.join(dirpath, fn), encoding="utf-8", errors="replace") as fh:
                for m in re.finditer(r'"(FORGECRM_[A-Z0-9_]+)"', fh.read()):
                    keys.add(m.group(1))
    return sorted(keys)


def emit_manifest(name: str, pkg: dict) -> str:
    purpose = package_doc(pkg)
    deps = internal_deps(pkg)
    ext = external_deps(pkg)
    envs = env_keys(name)
    api = exported_api(name)

    def ylist(items: list[str]) -> str:
        if not items:
            return " []"
        return "\n" + "\n".join(f"  - {i}" for i in items)

    lines = [
        f"name: {name}",
        "version: 1.0.0",
        f"purpose: >-\n  {purpose}",
        "language: go",
        "runtime: library",
        "entry_points: []",
        "public_api:" + ylist(api),
        "depends_on:" + ylist(deps),
        "external_deps:" + ylist(ext),
        "requires_env:" + ylist(envs),
        "database:",
        "  tables: []            # владение данными — логическое, см. audit/DEPENDENCY_MAP.md",
        "  migrations: []        # общие встроенные миграции internal/migrations",
        "events:",
        "  emits: []",
        "  consumes: []",
        "api: []                 # HTTP-маршруты объявлены в internal/api (композиция)",
        "portable: true          # доказано scripts/extract-test",
        "extract_status: verified",
    ]
    return "\n".join(lines) + "\n"


def emit_module_md(name: str, pkg: dict) -> str:
    purpose = package_doc(pkg)
    deps = internal_deps(pkg)
    envs = env_keys(name)
    api = exported_api(name)
    dep_txt = ", ".join(f"`{d}`" for d in deps) if deps else "нет (лист)"
    env_txt = "\n".join(f"- `{e}`" for e in envs) if envs else "- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)"
    api_txt = "\n".join(f"- `{a}`" for a in api[:20]) if api else "- публичный API не объявлен"

    return f"""# Модуль: {name}

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
{purpose}

## 3. Контракт
Публичный Go-API:
{api_txt}

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: {dep_txt}.

## 7. Конфигурация
{env_txt}

## 8. Сборка и запуск
```bash
cd backend
go build ./{module_dir(name)}
go test ./{module_dir(name)}/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh {name}   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`{MODULE_PATH}internal/{name}` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
"""


def main() -> int:
    wanted = sys.argv[1:]
    modules = read_modules()
    if wanted:
        modules = [m for m in modules if m in wanted]
    graph = go_list_graph()

    written = 0
    for name in modules:
        pkg = graph.get(MODULE_PATH + module_dir(name))
        if pkg is None:
            print(f"skip {name}: package not found", file=sys.stderr)
            continue
        d = os.path.join(BACKEND, module_dir(name))
        with open(os.path.join(d, "manifest.yaml"), "w", encoding="utf-8") as fh:
            fh.write(emit_manifest(name, pkg))
        with open(os.path.join(d, "MODULE.md"), "w", encoding="utf-8") as fh:
            fh.write(emit_module_md(name, pkg))
        written += 1
    print(f"wrote metadata for {written} module(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
