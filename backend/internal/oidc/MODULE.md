# Модуль: oidc

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package oidc implements corporate single sign-on over OpenID Connect.

## 3. Контракт
Публичный Go-API:
- `func IsBindingConflict`
- `func IsBindingError`
- `func IsNoAccount`
- `func SafeReturnTo`
- `type BindingError`
- `type Identity`
- `type LinkedIdentity`
- `type Provider`
- `type ProviderInput`
- `type ProviderScope`
- `type PublicProvider`
- `type Service`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `config`, `database`, `secrets`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/oidc
go test ./internal/oidc/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh oidc   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/oidc` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
