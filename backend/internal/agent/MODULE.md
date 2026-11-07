# Модуль: agent

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package agent implements the human-in-the-loop execution model described in ADR 0005.

## 3. Контракт
Публичный Go-API:
- `func ArgumentsHash`
- `func CanonicalArguments`
- `type Action`
- `type Approval`
- `type Config`
- `type Decider`
- `type Decision`
- `type ExecutionFunc`
- `type Executor`
- `type Proposal`
- `type RiskTier`
- `type Run`
- `type Service`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `audit`, `database`, `httpx`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/agent
go test ./internal/agent/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh agent   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/agent` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
