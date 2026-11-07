# AI Workspace 2.0 — этап 1: аудит и архитектурный план

Дата: 2026-11-07. Область: существующая AI Platform, Copilot, MCP и фронтенд.

Цель этапа — зафиксировать, что уже есть, что работает частично и чего нет, прежде
чем что-либо менять. Код на этом этапе не меняется; документ описывает план.

## 1. Что уже реализовано

### Провайдер

- `FORGECRM_AI_PROVIDER=openrouter` обслуживается OpenAI-совместимым клиентом
  (`internal/ai/ai.go`, `OpenAICompatibleProvider`). Базовый URL по умолчанию
  `https://openrouter.ai/api/v1`, «голый» хост дополняется путём `/api/v1`.
- Алиасы `OPENROUTER_API_KEY`, `LLM_MODEL`, `LLM_BASE_URL`; один
  `OPENROUTER_API_KEY` включает `provider=openrouter`. Заголовки атрибуции
  `FORGECRM_AI_HTTP_REFERER` (`HTTP-Referer`) и `FORGECRM_AI_TITLE` (`X-Title`).
- Набор значений `FORGECRM_AI_PROVIDER` — закрытое множество
  `config.AIProviderKinds` (`none`, `ollama`, `openai`, `openai_compatible`,
  `openrouter`). `config.Validate` и каталог `GET /ai/providers` строятся из него.
- Ключ хранится зашифрованным (`FORGECRM_SECRET_KEY`), не отдаётся на фронтенд.

### Стриминг

- `POST /ai/stream/chat` и `POST /ai/stream/assist/{id}` — server-sent events
  (`internal/api/ai_stream_handlers.go`). SSE-писатель сбрасывает буфер на каждое
  событие. Обычные (буферизованные) маршруты сохранены для прокси без SSE.

### MCP

- `internal/ai/mcp.go` — реестр из 8 инструментов, каждый проверяет то же
  разрешение и вызывает тот же доменный сервис, что и REST. Привилегированного
  пути нет: агент действует с правами пользователя и ничем больше.
  Инструменты: `search_accounts`, `account_summary` (accounts.read);
  `list_tasks` (tasks.read); `pipeline_overview` (pipelines.read);
  `create_task` (tasks.create, dangerous); `create_ticket` (tickets.create,
  dangerous); `update_deal_stage` (deals.change_stage, dangerous);
  `get_report` (reports.read).
- Транспорт: `POST /mcp` (JSON-RPC 2.0), `POST /mcp/call` (одиночный вызов для
  админ-UI), `GET /mcp/tools` (каталог). Тело ограничено 1 МиБ.
- `POST /ai/tasks` и `GET /ai/tasks` — постановка и список задач ИИ.

### Согласования и агент

- `internal/agent` — `Service` с `StartRun`/`Propose`/`CompleteRun`/`CancelRun` и
  идемпотентностью по ключу (`loadByIdempotencyKey`), бюджетом токенов и
  `SweepExpired`. Таблица `agent_approvals` (миграция 0009), права
  `agent.approve`/`agent.propose`.
- `internal/ai/agent_executor.go` — `RiskTier(toolName)`, `DeniedToolNames()`,
  `ProposeForAgent`. Write-инструменты помечены `Dangerous`.

### Персистентность (частичная)

- `ai_tasks` — журнал запросов ИИ: `organization_id`, `user_id`, `provider`,
  `model`, `kind`, `prompt_summary`, `status`, токены, `tool_calls` (jsonb),
  `correlation_id`. Это **аудит/телеметрия**, а не история диалога.
- `ai_insights` (миграции 0014/0018) — кэш подсказок по сущности.
- `mcp_credentials` (миграция 0025) — учётные данные MCP.

### Фронтенд

- `pages/AiPage.tsx` — чат-консоль `AiAssistantChat` + консоль MCP.
- `components/AiAssistant.tsx` — чат и плавающий запускатель `AiAssistantLauncher`.
- `components/AssistPanel.tsx`, `AttachmentPanel.tsx`, `RecordCollabPanel.tsx`.
- `api/ai.ts`, `api/assist.ts`, `api/assistantCrm.ts`; страница админа
  `pages/admin/MCPCredentialsPage.tsx`.

## 2. Что работает частично

- **История диалога не персистентна.** Нет таблиц `ai_conversations`,
  `ai_messages`, `ai_tool_calls`, `ai_attachments`, `ai_summaries`. История живёт
  в компоненте и теряется при перезагрузке. `ai_tasks` хранит запросы, но не
  восстанавливает диалог.
- **Нет независимых сессий.** Один поток на компонент; нельзя создавать,
  переключать, переименовывать, архивировать, удалять диалоги, искать по истории.
- **Context engine отсутствует.** Нет сжатия длинных диалогов, summary,
  сохранённой памяти; в модель уходит только текущий запрос и контекст сущности.
- **MCP-инструментов 8 из целевого каталога (~50).** Нет аналитических,
  дубль-поиска, data-quality, единого поиска, file intelligence.
- **Управление API-ключами MCP неполное.** `mcp_credentials` есть, но нет
  UI-жизненного цикла (создание/срок/отзыв/ротация/журнал вызовов) и явных scopes.
- **Агент оркестрации ограничен.** Есть `Run`/`Action`/подтверждение, но нет
  многошагового планировщика с промежуточным состоянием и продолжением.

## 3. Чего нет

- AI Workspace (три области: сессии / чат / контекст+инструменты), Elitеа-подобный.
- Постоянные AI-сессии и память в PostgreSQL.
- Smart CRM context (карточка клиента → связанные сделки, активность, задачи,
  тикеты, аналитика) с показом использованных данных.
- Analytics/Business Intelligence естественным языком с числами и источниками.
- File Intelligence (PDF/DOCX/XLSX/CSV/TXT).
- Расширенный аудит ИИ (корреляционные id цепочек tool calls, отдельно от истории).
- Единый mock-провайдер для детерминированных тестов.

## 4. Архитектурные ограничения и правила

1. Не создавать вторую CRM, вторую AI Platform или дублирующие сервисы. Каждый
   инструмент идёт через существующий доменный сервис `internal/crm`.
2. Модель не является доверенной стороной. Любой ответ модели не даёт новых
   полномочий; права проверяются на сервере перед каждым изменением.
3. RBAC и tenant isolation на каждом endpoint и каждом вызове инструмента.
4. Прямой произвольный SQL модели не даётся.
5. Секреты не в Git и не на фронтенде.
6. История — в PostgreSQL, не в localStorage.
7. Работа ведётся в `main`; новые ветки не создаются; рабочее дерево проверяется
   перед изменениями; существующие тесты и security checks не ослабляются.

## 5. Карта файлов, которые предстоит изменить (по этапам)

| Этап | Область | Файлы |
|------|---------|-------|
| 2 | Модель данных сессий | `backend/internal/migrations/00NN_ai_conversations*.sql` (+ messages, tool_calls, attachments, summaries), `backend/internal/ai/` (сервис сессий), `backend/internal/api/ai_*` |
| 3 | AI Workspace | `frontend/src/pages/AiPage.tsx`, `frontend/src/components/AiAssistant.tsx`, новые компоненты сессий/контекста, `frontend/src/api/ai.ts` |
| 4 | OpenRouter + streaming | `backend/internal/ai/ai.go`, `streaming.go`, `ai_stream_handlers.go` (модель задаётся ENV; уже готово) |
| 5 | Context & Memory | `backend/internal/ai/` (сжатие, summary), новые таблицы памяти |
| 6 | MCP tools | `backend/internal/ai/mcp.go` (+ аналитика, поиск, дубли, data quality) |
| 7 | Agent orchestration | `backend/internal/ai/agent_executor.go`, `backend/internal/agent/*` |
| 8 | Approval workflow | `backend/internal/agent/approval.go`, `backend/internal/api/agent_handlers.go` |
| 9 | CRM context & analytics | `backend/internal/ai/assist.go`, `internal/crm` (чтение), отчёты |
| 10 | File intelligence | новый пакет `backend/internal/ai/files` + хранилище вложений |
| 11 | AI admin & keys | `backend/internal/api/ai_handlers.go`, `mcp_credentials`, `frontend/src/pages/admin/` |
| 12 | Security/тесты/CI | `backend/internal/testsupport/`, `.github/workflows/` |

## 6. Проверенные факты на момент аудита

- `go build ./...`, `go vet ./...`, `gofmt -l .` — чисто; `go test ./...` — pass.
- Frontend: `tsc -b` — чисто; 375 тестов (48 файлов) — pass; `vite build` — ok.
- `scripts/docs-check.sh` и `openapi-gen -check` — согласованы.
- Модель `nvidia/nemotron-3-ultra-550b-a55b:free` сверена с живым каталогом
  OpenRouter: контекст 1 000 000, поддержка `tools`/`tool_choice`, цена 0.

## 7. Известные ограничения этапа 1

- Аудит — по коду и документации; поведение под нагрузкой не измерялось.
- Размер «целевого каталога MCP (~50 инструментов)» — заявка из ТЗ, а не
  подтверждённый перечень реализуемого; каждый инструмент должен опираться на
  существующий доменный сервис, поэтому часть имён может не иметь опоры.
- Этапы 2–12 не начаты; это план, а не выполненная работа.
