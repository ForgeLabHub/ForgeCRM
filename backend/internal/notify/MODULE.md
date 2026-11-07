# Модуль: notify

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package notify implements outbound notifications: SMTP email and the optional Telegram bot.

## 3. Контракт
Публичный Go-API:
- `func IsNotificationKind`
- `func RenderTaskCard`
- `type ConsentGate`
- `type EmailService`
- `type InlineButton`
- `type LinkRequest`
- `type Message`
- `type NotificationKind`
- `type TaskCard`
- `type TaskDeliveryResult`
- `type TelegramService`
- `type Update`
- `type UserSettings`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `config`, `database`, `httpx`, `rbac`, `secrets`, `telegramtext`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/notify
go test ./internal/notify/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh notify   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/notify` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
