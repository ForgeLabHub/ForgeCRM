# Модуль: authn

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package authn implements authentication: password login, opaque sessions, CSRF, TOTP MFA, account lockout and password reset.

## 3. Контракт
Публичный Go-API:
- `func CorrelationContext`
- `func ValidatePassword`
- `type LoginRequest`
- `type LoginResult`
- `type Service`
- `type Session`
- `type User`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `config`, `database`, `logging`, `rbac`, `secrets`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/authn
go test ./internal/authn/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh authn   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/authn` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
