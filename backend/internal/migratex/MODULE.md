# Модуль: migratex

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package migratex implements data migration from external systems.

## 3. Контракт
Публичный Go-API:
- `func Implemented`
- `func ImplementedSources`
- `type ColumnInfo`
- `type ConnectionConfig`
- `type CreateRequest`
- `type JSONMap`
- `type MigrationJob`
- `type Reconciliation`
- `type Service`
- `type Source`
- `type SourceKind`
- `type TableSchema`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `database`, `httpx`, `integrations`, `rbac`, `secrets`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/migratex
go test ./internal/migratex/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh migratex   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/migratex` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
