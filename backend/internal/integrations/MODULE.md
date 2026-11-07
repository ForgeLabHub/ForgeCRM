# Модуль: integrations

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Модуль ForgeCRM (документация пакета не задана в коде).

## 3. Контракт
Публичный Go-API:
- `func IsBlockedIP`
- `func ValidateOutboundURL`
- `type Adapter`
- `type Column`
- `type CreateRequest`
- `type GuardedClient`
- `type Hub`
- `type Integration`
- `type ReadRequest`
- `type Record`
- `type Run`
- `type Schema`
- `type Status`
- `type Table`
- `type WriteResult`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `database`, `httpx`, `netguard`, `rbac`, `secrets`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/integrations
go test ./internal/integrations/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh integrations   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/integrations` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
