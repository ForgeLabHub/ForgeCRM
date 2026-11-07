# Модуль: workflow

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package workflow implements the workflow automation engine.

## 3. Контракт
Публичный Go-API:
- `func RegisterKnownEvents`
- `func ValidateCatalog`
- `func ParseCron`
- `type Condition`
- `type Config`
- `type CreateInput`
- `type Definition`
- `type Engine`
- `type EvalContext`
- `type ExportDocument`
- `type OrgDefaults`
- `type Run`
- `type RunResult`
- `type Step`
- `type StepRun`
- `type Store`
- `type Template`
- `type TemplateRef`
- `type Workflow`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `crm`, `database`, `httpx`, `logging`, `outbox`, `rbac`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/workflow
go test ./internal/workflow/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh workflow   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/workflow` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
