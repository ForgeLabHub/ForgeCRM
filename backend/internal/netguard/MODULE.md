# Модуль: netguard

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package netguard holds the outbound-destination guard shared by every part of ForgeCRM that opens a connection to an operator- or user-supplied address: the Integration Hub, the outbound webhook sender, the migration connectors and the S3-compatible object store.

## 3. Контракт
Публичный Go-API:
- `func IsBlockedIP`
- `func ValidateOutboundURL`
- `type GuardedClient`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: нет (лист).

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/netguard
go test ./internal/netguard/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh netguard   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/netguard` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
