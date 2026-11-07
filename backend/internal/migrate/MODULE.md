# Модуль: migrate

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package migrate runs embedded SQL migrations and reports schema readiness.

## 3. Контракт
Публичный Go-API:
- `func Check`
- `func Down`
- `func ExpectedVersion`
- `func IsSchemaNotReady`
- `func Up`
- `func WaitForSchema`
- `type Queryer`
- `type SchemaBehindError`
- `type SchemaDirtyError`
- `type SchemaNotReadyError`
- `type State`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `migrations`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/migrate
go test ./internal/migrate/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh migrate   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/migrate` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
