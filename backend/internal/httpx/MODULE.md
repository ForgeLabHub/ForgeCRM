# Модуль: httpx

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package httpx provides HTTP plumbing shared by every handler: a uniform error envelope, JSON helpers, pagination, correlation IDs, rate limiting and security headers.

## 3. Контракт
Публичный Go-API:
- `func AccessLogMiddleware`
- `func ClientIP`
- `func ClientIPFrom`
- `func ClientIPMiddleware`
- `func DecodeJSON`
- `func OutboundTraceHeader`
- `func PathParam`
- `func RateLimitMiddleware`
- `func RecoverMiddleware`
- `func RequestID`
- `func RequestIDMiddleware`
- `func SecurityHeadersMiddleware`
- `func SecurityHeadersMiddlewareWithOptions`
- `func TraceMiddleware`
- `func WriteError`
- `func WriteJSON`
- `func WriteJSONList`
- `func WriteJSONStatus`
- `func WriteRateLimitHeaders`
- `type APIError`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `logging`, `ratelimit`, `tracectx`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/httpx
go test ./internal/httpx/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh httpx   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/httpx` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
