# Переносимые модули ForgeCRM

> Статус: **механизм реализован**, метаданные — добавляются в этом цикле (Этап E).
> Механизм доказан харнессом `scripts/extract-test` (36/36 модулей, `EXTRACT_RC=0`).

## 1. Что значит «переносимый модуль»

Модуль переносим, если его можно скопировать в **другой** Go-проект и там
собрать, протестировать и запустить, не притаскивая весь ForgeCRM. Это
**измеряемое** свойство: его доказывает харнесс, а не аккуратная папка.

## 2. Уже существующий механизм (факт)

| Артефакт | Что делает |
|---|---|
| `scripts/extract-test/modules.txt` | источник истины: 36 модулей |
| `scripts/extract-test/<module>/setup.sh` | копирует пакет в чистый Go-модуль, переписывает импорты, собирает, тестирует, запускает smoke |
| `scripts/extract-test/lib.sh` | примитивы: `extract_pkg`, `rewrite_import`, `assert_no_monolith_import` |
| `scripts/extract-test/run.sh` | прогоняет все (или указанные) модули |
| CI job `extraction` | тот же прогон в CI |
| `internal/archtest` | дешёвая постоянная проверка тех же границ через `go list` |
| `internal/constructor` + `cmd/forgecrm` | генерация standalone-проекта из рецепта |

**Доказательство:** `scripts/extract-test/run.sh` → `EXTRACT_RC=0`, 36/36 ok.

## 3. Чего не хватало (и что добавляет Этап E)

У модулей не было **машинно-читаемых метаданных** и **инструкции по переносу**.
Добавлено:

- `manifest.yaml` — контракт переноса (входы/выходы, env, миграции, события, API).
- `MODULE.md` — человекочитаемое описание и инструкция переноса.
- `scripts/module-docs-check.sh` — гейт: каждый модуль из `modules.txt` обязан
  иметь `manifest.yaml` с обязательными полями.
- Пример минимального запуска и пример встраивания (в `MODULE.md`).

## 4. Расположение артефактов

Метаданные живут рядом с кодом модуля, чтобы не разъезжаться:

```
backend/internal/<module>/
  manifest.yaml     # контракт переноса (Этап E)
  MODULE.md         # описание + инструкция переноса (Этап E)
  *.go              # код (существует)
  *_test.go         # тесты (существуют)
```

> Примечание: программа предлагала `modules/<name>/` с `go.mod` на модуль. В
> ForgeCRM модули живут в одном Go-модуле по ADR 0001; вводить 36 отдельных
> `go.mod` значило бы сломать текущую сборку. Метаданные кладутся рядом с кодом —
> это даёт переносимость **без** разрыва монолита. Харнесс извлечения и есть
> доказательство переносимости.

## 5. Схема `manifest.yaml`

```yaml
name: crm                 # имя модуля (= имя пакета)
version: 1.0.0            # версия публичного интерфейса
purpose: ...              # назначение
language: go
runtime: library          # library | process
entry_points: []          # для process: команды; для library: пусто
public_api: ...           # ключевые экспортируемые типы/функции
depends_on: [audit, ...]  # внутренние зависимости (доказаны харнессом)
external_deps: [pgx, ...] # сторонние зависимости
requires_env: [...]       # переменные окружения
database:                 # требования к БД
  tables: [...]
  migrations: [...]
events:                   # события (для тех, кто их эмитит)
  emits: [...]
  consumes: [...]
api: []                   # HTTP-маршруты (для сервисов с транспортом)
portable: true            # доказано харнессом
extract_status: verified  # verified | almost | no
```

## 6. Как перенести модуль (пошагово)

```bash
# 1. Убедиться, что модуль извлекается
./scripts/extract-test/run.sh <module>

# 2. Скопировать пакет и его зависимости (список — в manifest.yaml)
# 3. Переписать импорты github.com/geff1778/forgecrm/internal/<m> → <ваш путь>
# 4. Применить миграции из database.migrations (если модуль владеет таблицами)
# 5. Задать requires_env (см. manifest.yaml)
# 6. Собрать и протестировать:
go build ./... && go test ./...
```

Готовый пример для `analytics` (нулевые зависимости):

```bash
./scripts/extract-test/run.sh analytics
# analytics — чистая арифметика, переносится одним пакетом без зависимостей
```

## 7. Список модулей и статус

Источник истины — `scripts/extract-test/modules.txt`. Текущий статус:
**36 модулей, все `extract_status: verified`** (доказано харнессом).

Полный список (в порядке файла):

```
netguard analytics ratelimit objectstore secrets logging httpx safe parsers
database rbac audit outbox retention agent jobs crm storage integrations config
workflow events migratex simulate forecast risk notify ai importx migrations
migrate authn oidc views constructor tenantconfig
```

## 8. Проверка (гейты)

| Утверждение | Команда |
|---|---|
| Модуль извлекается | `./scripts/extract-test/run.sh <module>` |
| Все модули извлекаются | `./scripts/extract-test/run.sh` (CI `extraction`) |
| Границы держатся в графе | `go test ./internal/archtest/...` |
| У каждого модуля есть манифест | `./scripts/module-docs-check.sh` |

## 9. Ограничения (честно)

- Метаданные добавлены для **приоритетных** модулей в этом цикле; гейт
  `module-docs-check.sh` перечислит оставшиеся. Цель — покрыть все 36.
- Один автономный файл: для `analytics` и `netguard` это возможно и показано в их
  `MODULE.md`; для крупных модулей искусственно упаковывать в один файл не нужно.
- Модули остаются в одном Go-модуле (ADR 0001); переносимость доказывается
  харнессом, а не отдельным `go.mod`.
