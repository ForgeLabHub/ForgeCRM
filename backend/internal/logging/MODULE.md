# Модуль: logging

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package logging configures structured logging with correlation IDs and field-level redaction of secrets.

## 3. Контракт
Публичный Go-API:
- `func Actor`
- `func CorrelationID`
- `func IsSensitiveKey`
- `func New`
- `func Organization`
- `func Redact`
- `func WithActor`
- `func WithCorrelationID`
- `func WithOrganization`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: нет (лист).

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/logging
go test ./internal/logging/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh logging   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/logging` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
