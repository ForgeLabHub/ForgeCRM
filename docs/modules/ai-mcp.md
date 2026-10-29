# Модуль: AI и MCP

## 1. Назначение и границы
Контекстная помощь внутри CRM: подсказки по записи, суммаризация, инсайты, вызов
инструментов через MCP (JSON-RPC). Провайдер модели — заменяемый адаптер.
Не делает: не пишет в базу в обход прикладных сервисов; не выполняет деструктивные
действия без подтверждения человеком.

## 2. Сценарии использования
- Запрос подсказки по сделке/тикету (контекст собирается сервером, не клиентом).
- Генерация инсайта и его сохранение.
- MCP-клиент вызывает инструмент; инструмент проходит RBAC; деструктивное действие
  уходит в очередь подтверждений.
- Отказ: провайдер не настроен → внятное «не настроено», функции выключены, продукт
  работает.

## 3. Контракт
`/api/v1/ai/*`, `/api/v1/agent/*`, MCP JSON-RPC (`internal/ai/mcp.go`,
`mcp_protocol.go`). Порт провайдера: `ai.Provider`. Порт исполнения: `agent.Executor`.
События аудита: `agent.run`, `agent.action`, `approval.requested`.

## 4. Данные
Владеет: `ai_providers`, `ai_tasks`, `ai_insights`, `agent_runs`, `agent_actions`,
`agent_approvals`, `approvals`. Читает CRM-данные через контракт `crm`.

## 5. Права
`ai.*`, `approvals.*`, `agent.*`; инструменты MCP объявляют требуемое право и
проверяются теми же правилами RBAC, что и HTTP-маршруты. Деструктивные действия
требуют step-up.

## 6. Зависимости
Исходящие: `crm` (чтение/запись через контракт), `audit`, `secrets` (шифрование
ключей провайдера), `outbox`. Внешняя система — провайдер модели (Ollama,
OpenAI-совместимый) через `ai.Provider`.
Входящие: `api`, `agent`.

## 7. Конфигурация
`FORGECRM_AI_ENABLED`, `FORGECRM_AI_PROVIDER`, `FORGECRM_AI_BASE_URL`,
`FORGECRM_AI_API_KEY`, `FORGECRM_AI_MODEL`, `FORGECRM_AI_TIMEOUT`,
`FORGECRM_AI_MAX_REQUESTS_PER_MIN`. Ключ хранится зашифрованным
(`FORGECRM_SECRET_KEY`); при выключенном флаге модуль не делает сетевых вызовов.

## 8. Сборка и отладка
В составе API/worker. Локальная проверка протокола:

```bash
cd backend && go test ./internal/ai/... -v
```

## 9. Тесты
Unit: `ai/assist_test.go`, `mcp_protocol_test.go`, `prompt_test.go`,
`prompt_descriptors_test.go`, `agent/agent_test.go`. Integration:
`internal/api/mcp_integration_test.go`, `ai_assist_handlers`-тесты,
`internal/testsupport/ai_assist_test.go`, `agent_approvals_test.go`,
`ai_assist_cache_test.go`.

## 10. Наблюдаемость и ошибки
Каждый вызов пишется в `ai_tasks`; действия — в `agent_runs`/`agent_actions`; отказы
и подтверждения — в `audit_log`. Ошибки: `not_configured`, `rate_limited`,
`provider_error`, `approval_required`. Метрики: число вызовов, задержка, доля отказов.

## 11. Ограничения и готовность
- Качество ответа не гарантируется; журнал и подтверждения — компенсирующий механизм.
- Нет автоматической оценки качества модели.
- Prompt-injection снижается тегированием данных, но не устраняется (ADR 0006).
- Готовность: при выключенном AI система полностью работоспособна; деструктивные
  действия невозможны без подтверждения; всё залогировано.
