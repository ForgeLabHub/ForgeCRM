# Модуль: audit

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package audit records user actions and security events.

## 3. Контракт
Публичный Go-API:
- `func StrPtr`
- `func UUIDPtr`
- `type Action`
- `type Entry`
- `type Logger`
- `type SecurityEvent`
- `type VerificationResult`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `database`, `logging`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/audit
go test ./internal/audit/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh audit   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/audit` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
