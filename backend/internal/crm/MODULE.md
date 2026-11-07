# Модуль: crm

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package crm implements the CRM domain: accounts, contacts, leads, pipelines, deals, tasks, activities, tickets, tags, comments, custom fields, search and reports.

## 3. Контракт
Публичный Go-API:
- `func ObjectScopeFilter`
- `type Account`
- `type AccountInput`
- `type AccountObservationData`
- `type Activity`
- `type ActivityInput`
- `type BaselineData`
- `type BaselineInput`
- `type BulkArchiveFailure`
- `type BulkArchiveInput`
- `type BulkArchiveResult`
- `type Comment`
- `type Contact`
- `type ContactInput`
- `type CustomFieldDefinition`
- `type Deal`
- `type DealInput`
- `type DealObservationData`
- `type JSONMap`
- `type Lead`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `database`, `httpx`, `outbox`, `rbac`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/crm
go test ./internal/crm/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh crm   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/crm` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
