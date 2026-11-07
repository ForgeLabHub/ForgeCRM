# Модуль: storage

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package storage implements CRM file attachments on an S3-compatible object store.

## 3. Контракт
Публичный Go-API:
- `type Config`
- `type Document`
- `type Memory`
- `type ObjectInfo`
- `type ObjectMeta`
- `type ObjectStore`
- `type ReconcileReport`
- `type S3`
- `type S3Config`
- `type Service`
- `type UploadInput`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `database`, `httpx`, `objectstore`, `rbac`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/storage
go test ./internal/storage/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh storage   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/storage` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
