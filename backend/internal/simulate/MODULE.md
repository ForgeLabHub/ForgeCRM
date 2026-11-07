# Модуль: simulate

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package simulate implements the Simulation Lab: saved "what if" scenarios and the runs that compute them.

## 3. Контракт
Публичный Go-API:
- `type CatalogEntry`
- `type CreateScenarioInput`
- `type ParamSpec`
- `type PreviewInput`
- `type PreviewResult`
- `type Run`
- `type RunInput`
- `type Scenario`
- `type Service`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `analytics`, `audit`, `crm`, `database`, `rbac`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/simulate
go test ./internal/simulate/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh simulate   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/simulate` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
