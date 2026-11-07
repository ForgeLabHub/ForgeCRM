# Модуль: events

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package events implements the inbound event gateway and outbound webhooks.

## 3. Контракт
Публичный Go-API:
- `func CorrelationIDFrom`
- `func SignInboundForTest`
- `type CreateSubscriptionRequest`
- `type Delivery`
- `type DeliverySender`
- `type Dispatcher`
- `type Gateway`
- `type InboundEvent`
- `type Poster`
- `type Subscription`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `database`, `httpx`, `integrations`, `logging`, `outbox`, `rbac`, `secrets`, `tracectx`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/events
go test ./internal/events/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh events   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/events` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
