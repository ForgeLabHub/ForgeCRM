# Модуль: secrets

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package secrets provides password hashing, token generation and authenticated encryption for credentials stored in the database.

## 3. Контракт
Публичный Go-API:
- `func ConstantTimeEqual`
- `func HashPassword`
- `func HashPasswordWithParams`
- `func HashToken`
- `func IsSensitiveKey`
- `func RandomNumericCode`
- `func RandomToken`
- `func SignPayload`
- `func VerifyPassword`
- `func VerifySignature`
- `type Argon2Params`
- `type Keyring`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: нет (лист).

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/secrets
go test ./internal/secrets/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh secrets   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/secrets` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
