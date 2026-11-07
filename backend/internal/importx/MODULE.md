# Модуль: importx

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package importx implements the Data Import & Migration Center.

## 3. Контракт
Публичный Go-API:
- `func FieldList`
- `type AnalysisResult`
- `type CreateRequest`
- `type Job`
- `type JobPayload`
- `type MappingEntry`
- `type Options`
- `type ReclaimReport`
- `type Report`
- `type RowError`
- `type Service`
- `type ValidationIssue`
- `type ValidationReport`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `config`, `crm`, `database`, `httpx`, `jobs`, `outbox`, `parsers`, `rbac`, `safe`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/importx
go test ./internal/importx/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh importx   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/importx` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
