# Модуль: ai

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package ai implements the AI layer and the MCP tool surface.

## 3. Контракт
Публичный Go-API:
- `func AssembleSystemPrompt`
- `func DeniedToolNames`
- `func HTTPErrorFrom`
- `func RiskTier`
- `type AgentExecutor`
- `type AssistRequest`
- `type CompletionRequest`
- `type CompletionResponse`
- `type Confidence`
- `type ContextBlock`
- `type DeltaFunc`
- `type Disclosure`
- `type Draft`
- `type EntityType`
- `type Fact`
- `type Insight`
- `type Kind`
- `type MCPServer`
- `type Message`
- `type OllamaProvider`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `agent`, `audit`, `config`, `crm`, `database`, `httpx`, `logging`, `ratelimit`, `rbac`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/ai
go test ./internal/ai/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh ai   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/ai` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
