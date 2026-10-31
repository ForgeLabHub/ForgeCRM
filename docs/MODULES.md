# Инвентаризация модулей

Это проверенный по коду каталог каждого логического модуля в ForgeCRM, бэкенда и
фронтенда. Он был получен на коммите `7a6d67f` чтением исходников, графа импортов
(`go list`), миграций и тестов — а не доверием существующим документам. Там, где
документ и код расходились, побеждал код, а расхождение записано в соответствующей
строке.

Единица инвентаризации — **пакет Go** (а для фронтенда — слой). Каталог с границей
уровня `go.mod` не становится автоматически модулем: пакет перечислен здесь потому,
что он владеет поведением, а не потому, что у него есть папка.

Две вещи, которые этот документ намеренно не делает:

- Он не выдумывает интерфейсы. Каждый тип и функция, названные ниже, существуют в
  пакете, названном рядом.
- Он не заявляет `READY` о выделении. См. `EXTRACT_READY.md`; ничто не готово к
  выделению, пока не поднято в чистый проект и не собрано там.

## Как читать строку

| Поле | Значение |
| --- | --- |
| **Location** | Реальные пути. Всё под `backend/internal/<pkg>`, если не указано иное. |
| **Public API** | Экспортируемые типы и функции, реально присутствующие в пакете. |
| **Internal deps** | Другие пакеты `internal/…`, которые он импортирует (не-тестовые). |
| **External deps** | Сторонние и значимые для stdlib зависимости. |
| **Fake / test double** | Замена в процессе, поставляемая в не-тестовом коде, или документированная точка внедрения. Помощник `_test.go` перечислен как «только тест». |
| **Tests** | Реальные тестовые файлы. Unit = без базы данных; integration = `//go:build integration`. |
| **Extract** | `no` / `almost` / `yes`, согласовано с `EXTRACT_READY.md`. |
| **Cut lines** | Что переезжает с модулем и что остаётся. |

---

## Бэкенд — точки входа (`backend/cmd`)

### cmd/forgecrm-api
- **Ответственность:** процесс HTTP-сервера. Строит конфигурацию, подключает пул,
  связывает сервисы через `internal/wiring`, монтирует роутер, запускает миграции при
  `-migrate`/`FORGECRM_MIGRATE_ON_START`, создаёт владельца платформы.
- **Public API:** только `main`.
- **Internal deps:** `api, audit, authn, config, database, httpx, logging, migrate,
  rbac, secrets, wiring`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** нет.
- **Tests:** нет (покрыт косвенно интеграционными тестами `testsupport`, которые
  строят тот же `api.New`).
- **Extract:** `no` — корень композиции, не компонент.
- **Cut lines:** остаётся с монолитом; каждое выделение производит *новую* точку
  входа, а не перемещает эту.

### cmd/forgecrm-worker
- **Ответственность:** фоновый процесс. Движет доставку вебхуков, прогоны workflow,
  хранение, очередь заданий, импорты, разбор outbox и уведомления.
- **Public API:** только `main`.
- **Internal deps:** `agent, ai, audit, config, crm, database, events, importx,
  integrations, jobs, logging, notify, outbox, rbac, retention, secrets, workflow`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** нет.
- **Tests:** нет напрямую; компоненты, которыми он движет, тестируются в `testsupport`.
- **Extract:** `no` — корень композиции.
- **Cut lines:** остаётся; рецепт конструктора, которому нужна фоновая работа,
  генерирует собственную точку входа воркера.

### cmd/openapi-gen
- **Ответственность:** перегенерировать `docs/api/openapi.json` из живого роутера;
  `-check` падает, когда закоммиченная спецификация расходится с кодом.
- **Public API:** только `main`.
- **Internal deps:** `api, config, httpx, openapi, ratelimit`.
- **External deps:** нет сверх stdlib.
- **Fake:** нет.
- **Tests:** покрыт контрактными тестами `internal/openapi` и `internal/api`.
- **Extract:** `no`.
- **Cut lines:** остаётся; выделение не перегенерирует спецификацию, которой не владеет.

---

## Бэкенд — доменное ядро

### internal/crm  — ядро домена
- **Ответственность:** сущности и правила для контактов, аккаунтов, лидов, сделок,
  задач, обращений, активностей, комментариев, тегов, кастомных полей, воронок,
  поиска, уведомлений и запросов отчётности/интеллекта только для чтения. С областью
  тенанта; проверяет владение записью; пишет аудит.
- **Public API (типы):** `Service`, `Account`, `Contact`, `Lead`, `Deal`, `Task`,
  `Ticket`, `Activity`, `Comment`, `Tag`, `CustomFieldDefinition`, `Notification`,
  `Pipeline`, `PipelineStage`, `SearchResult` и DTO `*Input`
  (`AccountInput`, `ContactInput`, `DealInput`, `TaskInput`, `TicketInput`,
  `LeadInput`), плюс типы наблюдений/отчётов (`SalesSummaryReport`,
  `PipelineReport`, `SupportSummaryReport`, `RevenueHistory`, `RiskObservationSet`,
  `BaselineData`, `StageBaseline`, `PeriodConversion`).
- **Public API (функции):** `NewService`, `MaxBulkArchiveIDs`; `ErrNotFound`,
  `ErrConflict`.
- **Internal deps:** `audit, database, httpx, outbox, rbac`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** нет в процессе. Тесты движут реальный PostgreSQL.
- **Tests:** `crm_test.go` (валидаторы и кодек столбца `JSONMap`; первые тесты внутри
  пакета) плюс интеграционные тесты `internal/testsupport` и тесты обработчиков
  `internal/api`, которые движут реальный PostgreSQL.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/crm`, с `audit`,
  `database`, `httpx`, `outbox` и `rbac` как зависимостями, все доказаны. Утверждение
  «`httpx` и `outbox` — настоящее трение» было неверным: оба — доказанные листья/
  модули с областью, поэтому путешествуют с ним.
- **Cut lines:** пакет переезжает целиком. `database` (пул + `WithTx`), `rbac`
  (Principal + каталог разрешений), `httpx` и `outbox` путешествуют как объявленные
  зависимости.

### internal/analytics  — чистые вычисления
- **Ответственность:** чистая, детерминированная арифметика над деньгами, долями,
  процентами, моделированием воронки/выручки/удержания, методами прогнозирования и
  обнаружением риска. **Без базы данных, без часов, без I/O.**
- **Public API:** `Money`, `Ratio`, `Unit`, `Comparison`, `Evidence`,
  `RiskSignal`, `RiskSeverity`, `RiskSignalType`, `RiskThresholds`,
  `Forecast`, `ForecastMethod`, `ForecastPoint`, `ForecastParams`,
  `ForecastQuality`, `FunnelParams`, `RetentionParams`, `RevenueParams`,
  `StageSnapshot`, `Baseline`, `PeriodPoint`, `DealObservation`,
  `TaskObservation`, `AccountObservation`, `ConversionPeriodObservation`;
  функции `NewMoney`, `ParseMoney`, `NewRatioFrom*`, `FloatPercent`,
  `PercentString`, `RunForecast`, `RunFunnel`, `RunRetention`, `RunRevenue`,
  `DetectStalledDeals`, `DetectOverdueTasks`, `DetectAccountsWithoutContact`,
  `DetectAmountSpikes`, `DetectConversionDrops`, `DetectStageConcentration`,
  `SortSignals`, `SortPeriods`, `DefaultRiskThresholds` и константы версий.
- **Internal deps:** нет.
- **External deps:** только stdlib (`math/big`, `math`, `sort`, …).
- **Fake:** неприменимо — он уже чистый и детерминированный.
- **Tests:** `analytics_test.go` и др. (4 файла), без базы данных.
- **Extract:** `yes` — **ноль внутренних зависимостей**, и он действительно выделен,
  собран, протестирован и запущен в чистом модуле. См. `EXTRACT_READY.md` (Кандидат 1)
  и `EXTRACTION_TESTING.md`.
- **Cut lines:** весь пакет переезжает ни с чем больше. Потребитель должен поставить
  наблюдения и прочитать обратно результаты.

---

## Бэкенд — прикладные сервисы

### internal/importx
- **Ответственность:** Центр импорта: определение формата, разбор (CSV/XLSX/JSON/
  NDJSON/архивы), сопоставление, валидация, пробный прогон и исполнение против CRM.
- **Public API:** `Service`, `Job`, `JobPayload`, `CreateRequest`, `Options`,
  `MappingEntry`, `AnalysisResult`, `ValidationReport`, `ValidationIssue`,
  `Report`, `RowError`, `FieldList`, `NewService`.
- **Internal deps:** `audit, config, crm, database, httpx, importx/parsers,
  importx/safe, jobs, outbox, rbac`. **10 внутренних зависимостей** — см. примечание
  о 8+ в `DEPENDENCY_MAP.md`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`;
  подпакет `parsers` добавляет `github.com/xuri/excelize/v2`, `safe` добавляет
  `github.com/ulikunitz/xz`.
- **Fake:** `safe` работает с локальными временными каталогами (без внешнего сервиса).
  Фейка пути записи в CRM в процессе нет.
- **Tests:** модульные тесты `importx` (2 файла, включая определение и очистку
  временных); интеграция через `testsupport`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/importx`, с
  `audit`, `config`, `crm`, `database`, `httpx`, `importx/parsers`, `importx/safe`,
  `jobs`, `outbox` и `rbac` как зависимостями, все доказаны. Прежнее `no` его
  недооценивало: путь записи в `crm` — это зависимость, а не блокер, и каждая
  зависимость доказана.
- **Cut lines:** `parsers/` и `safe/` — отделяемые подпакеты; сам сервис — нет.

### internal/importx/safe  — распаковка недоверенных архивов
- **Ответственность:** единственное место, которое пишет на диск байты, выбранные
  загружающим. Он определяет контейнер по сигнатуре и отклоняет обход пути,
  символическую ссылку, слишком большую запись, zip-бомбу и чрезмерную вложенность,
  записывая в приватный корень `0700`, которым владеет.
- **Public API:** `Limits`, `DefaultLimits`, `Extractor`, `NewExtractor`,
  `Format`, `DetectFormat`, `ExtractedFile`, `ExtractResult`, `IsArchive`,
  `IsSupported` и отказы `Err*` (`ErrTooManyEntries`, `ErrTooLarge`,
  `ErrEntryTooLarge`, `ErrTooDeep`, `ErrUnsafePath`, `ErrUnsupportedLink`,
  `ErrRatioExceeded`, `ErrUnsupportedType`).
- **Internal deps:** нет — это лист.
- **External deps:** `github.com/ulikunitz/xz` (стандартная библиотека для zip/tar/gzip/bzip2).
- **Fake:** работает с локальными временными каталогами; без внешнего сервиса.
- **Tests:** 2 модульных файла (~24 теста), покрывающих обход, символические ссылки,
  zip-бомбы, глубокую вложенность, повреждённые данные и независимое декодирование
  фикстур.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/safe`. Поскольку
  это код, обрабатывающий контролируемый атакующим ввод, «его можно поднять и его
  тесты безопасности идут изолированно» здесь стоит дороже, чем где-либо ещё.
- **Cut lines:** переезжает сам; ничто внутри `importx` не должно переезжать с ним.

### internal/importx/parsers  — разбор табличных источников
- **Ответственность:** превращать CSV/TSV/JSON/JSONL/XLSX/XML в общую модель строки,
  сообщая выведенный набор столбцов, определённые типы и любые проблемы уровня строки,
  а не угадывая молча.
- **Public API:** `Parse`, `Stream`, `Detect`, `ListSheets`, `Format`, `Row`,
  `Result`, `RowError`, `Options`, `StreamOptions`, `Batch`, `ParseDate`,
  `ParseBool`, `ParseNumber`.
- **Internal deps:** нет — это лист.
- **External deps:** `github.com/xuri/excelize/v2`.
- **Tests:** добавлены в этом срезе; закрепляют нормализацию заголовка, восстановление
  строк, вывод типов, обработку листов XLSX, записи XML и отклонение кодировки. См.
  примечание о багах ниже.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/parsers`. Написание
  его тестов нашло два реальных бага, оба исправлены: XLSX отклонялся проверкой
  UTF-8, применённой ко всем форматам, а XML-парсер разбивал каждую запись на строку
  атрибутов и строку полей.
- **Cut lines:** переезжает сам; ничто внутри `importx` не должно переезжать с ним.

### internal/migratex  — миграция внешних данных
- **Ответственность:** переносить данные из внешнего источника (PostgreSQL, SQLite,
  REST) в ForgeCRM, с записями сверки.
- **Public API:** `Service`, `Source`, `SourceKind`, `TableSchema`, `ColumnInfo`,
  `ConnectionConfig`, `CreateRequest`, `MigrationJob`, `Reconciliation`,
  `JSONMap`, `NewService`, `NewPostgresSource`, `NewSQLiteSource`,
  `NewRESTSource`, `Implemented`.
- **Internal deps:** `audit, database, httpx, integrations, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`,
  `modernc.org/sqlite`.
- **Fake:** `Source` — интерфейс (документированный шов); адаптер источника REST включён.
- **Tests:** интеграция через `testsupport`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/migratex`, с
  `audit`, `database`, `httpx`, `integrations`, `rbac` и `secrets` как зависимостями,
  все доказаны. Интерфейс `Source` — это граница; источники остаются подключаемыми, а
  `Implemented` называет три, у которых есть реальный адаптер.
- **Cut lines:** интерфейс `Source` — граница; источники подключаемы.

### internal/ai  — абстракция провайдера, контекстный assist, MCP, инструменты агента
- **Ответственность:** абстракция провайдера модели (Ollama, совместимые с OpenAI),
  сборка контекста с проверкой прав + маскированием PII, генерация инсайтов с
  вычисляемым сервером баллом обоснованности, транспорт MCP JSON-RPC, реестр
  инструментов и исполнитель агента на базе ИИ.
- **Public API:** `Provider` (интерфейс), `Service`, `OllamaProvider`,
  `OpenAICompatibleProvider`, `CompletionRequest`, `CompletionResponse`,
  `AssistRequest`, `Insight`, `Fact`, `Disclosure`, `Draft`, `Confidence`,
  `ContextBlock`, `EntityType`, `Kind`, `Trust`, `MCPServer`, `ToolRegistry`,
  `Tool`, `ToolDescriptor`, `AgentExecutor`, `PromptInput`, `Message`,
  `RPCError`, `Request`, `Response`; функции `NewService`, `NewMCPServer`,
  `NewToolRegistry`, `NewAgentExecutor`, `AssembleSystemPrompt`,
  `DeniedToolNames`, `HTTPErrorFrom`, `RiskTier`; ошибки `ErrDisabled`,
  `ErrRateLimited`, `ErrProviderUnavailable`, `ErrUnknownEntity`,
  `ErrInsightNotCached`.
- **Internal deps:** `agent, audit, config, crm, database, httpx, logging,
  ratelimit, rbac`. **9 внутренних зависимостей.**
- **External deps:** `github.com/google/uuid`, stdlib `net/http`.
- **Fake:** `Provider` — интерфейс с **двумя реальными реализациями**, но **без
  фейка в процессе**; тесты используют серверы `httptest` или недостижимый адрес.
- **Tests:** 7 модульных файлов (сборка промпта, уверенность/обоснованность,
  маскирование, протокол MCP, классификация ошибок провайдера) + интеграция
  (`ai_assist_test.go`, `ai_assist_cache_test.go`, `ai_provider_outage_test.go`).
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/ai`, с
  `agent`, `audit`, `config`, `crm`, `database`, `httpx`, `logging`, `ratelimit`,
  `rbac` и `retention` как зависимостями, все доказаны. Smoke доказывает, что
  ограждение контекста нейтрализует подделанную запись, а уровни риска — это явное
  отображение. Прежнее `no` его недооценивало: привязка к `crm` — не блокер, когда
  `crm` доказан.
- **Cut lines:** интерфейс `Provider` и логика промпта/уверенности — переносимая
  часть; сервис привязывается к `crm`.

### internal/agent  — исполнение с участием человека
- **Ответственность:** конечный автомат согласования (ADR 0005). Предложенное опасное
  действие записывается, одобряется/отклоняется, затем исполняется через тот же путь
  инструментов с проверкой прав.
- **Public API:** `Service`, `Executor` (интерфейс), `ExecutionFunc`, `Config`,
  `Action`, `Run`, `Proposal`, `Approval`, `Decision`, `Decider`, `RiskTier`;
  функции `NewService`, `ArgumentsHash`, `CanonicalArguments`.
- **Internal deps:** `audit, database, httpx`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `Executor` — шов внедрения.
- **Tests:** `agent_test.go` (модуль) + `testsupport/agent_approvals_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/agent`, с `audit`,
  `database` и `httpx` (и собственными листьями `httpx`) как зависимостями. Его тест
  хеша аргументов идёт в выделенном модуле. `Executor` поставляет хост-приложение,
  поэтому шов путешествует с ним.
- **Cut lines:** переезжает с `audit`/`database`; `Executor` поставляет хост-приложение.

### internal/workflow  — движок автоматизации
- **Ответственность:** декларативные workflow: триггеры (event/schedule/manual),
  шаги (condition, branch, foreach, delay, действия), вычисление выражений, история
  прогонов/шагов, каталог шаблонов, импорт/экспорт JSON.
- **Public API:** `Engine`, `Store`, `Config`, `Workflow`, `Definition`,
  `Step`, `Condition`, `Template`, `TemplateRef`, `CreateInput`, `Run`,
  `RunResult`, `StepRun`, `EvalContext`, `ExportDocument`; функции `NewEngine`,
  `NewStore`, `NewEvalContext`, `ParseDefinition`, `ExportWorkflow`,
  `ImportWorkflow`, `ValidateCatalog`, `ParseCron`, `RegisterKnownEvents`,
  `Templates`, `TemplateByID`, `InputFromTemplate`; константа
  `DefinitionSchemaVersion`; виды триггеров `TriggerEvent`, `TriggerEventID`,
  `TriggerManual`, `TriggerSchedule`.
- **Internal deps:** `audit, crm, database, httpx, logging, outbox, rbac`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** нет в процессе.
- **Tests:** модульные тесты `workflow` (2 файла: cron, определение) +
  `testsupport/workflow_engine_test.go`, `workflow_templates_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/workflow`, с
  `audit`, `crm`, `database`, `httpx`, `logging`, `outbox` и `rbac` как
  зависимостями, все доказаны. Smoke регистрирует реальный словарь событий и
  валидирует каталог. Прямой вызов `crm.Service` — не блокер, когда `crm` доказан.
- **Cut lines:** слой определения/разбора/шаблонов отделяем от движка исполнения.

### internal/simulate  — Simulation Lab
- **Ответственность:** сценарии «что если» против базовой линии CRM. **Только
  чтение:** предпросмотр и прогон никогда не пишут данные CRM, не ставят задания, не
  отправляют вебхуки.
- **Public API:** `Service`, `Scenario`, `Run`, `CatalogEntry`, `ParamSpec`,
  `CreateScenarioInput`, `RunInput`, `PreviewInput`, `PreviewResult`, `Catalog`,
  `NewService`.
- **Internal deps:** `analytics, audit, crm, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** нет; читает через `crm.Service`.
- **Tests:** `testsupport/simulation_test.go` (интеграция) + тесты страницы фронтенда.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/simulate`, с
  `analytics`, `audit`, `crm`, `database` и `rbac` как зависимостями, все доказаны.
  Чтение `crm` — не блокер, когда `crm` доказан.
- **Cut lines:** `analytics` — переносимое ядро; `simulate` — обёртка сохранения +
  сервиса.

### internal/forecast
- **Ответственность:** прогнозирование выручки по истории организации; замораживает
  параметры, историю и версию алгоритма в строку прогона.
- **Public API:** `Service`, `Run`, `RunInput`, `CatalogEntry`, `Catalog`,
  `NewService`.
- **Internal deps:** `analytics, audit, crm, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Tests:** `testsupport/intelligence_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/forecast`, с
  `analytics`, `audit`, `crm`, `database` и `rbac` как зависимостями.
- **Cut lines:** как у `simulate`.

### internal/risk
- **Ответственность:** сканировать CRM на объяснимые условия риска; держать живой
  сигнал на (организацию, тип) с цитируемыми доказательствами; управлять порогами.
- **Public API:** `Service`, `Signal`, `ListFilter`, `ScanResult`, `Thresholds`,
  `NewService`.
- **Internal deps:** `analytics, audit, crm, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Tests:** `testsupport/intelligence_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/risk`, с
  `analytics`, `audit`, `crm`, `database` и `rbac` как зависимостями.
- **Cut lines:** как у `simulate`.

### internal/integrations  — жизненный цикл адаптера
- **Ответственность:** адаптеры внешних интеграций (REST, webhook-out, проба SMTP,
  дескриптор базы). Исходящая защита SSRF, которой он раньше владел, теперь живёт в
  `internal/netguard`; имена ниже реэкспортируются для существующих вызывающих.
- **Public API:** `Hub`, `Adapter` (интерфейс), `GuardedClient`, `Integration`,
  `Run`, `Schema`, `Table`, `Column`, `Record`, `ReadRequest`, `WriteResult`,
  `Status`, `CreateRequest`; функции `NewHub`, `NewGuardedClient`,
  `ValidateOutboundURL`, `IsBlockedIP` (последние четыре реэкспортируют `netguard`);
  `ErrWriteNotSupported`.
- **Internal deps:** `audit, database, httpx, netguard, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `Adapter` — шов; `webhookOutAdapter`/`restAdapter` — реальные реализации.
- **Tests:** модульные тесты `integrations` (валидация URL) + интеграция. Тесты SSRF
  переехали в `netguard`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/integrations`, с
  `audit`, `database`, `httpx`, `netguard`, `rbac` и `secrets` как зависимостями, все
  доказаны. Защита SSRF уже вынесена в `netguard`; остальное поднимается с кольцом
  ключей. **было:** `almost` — защита SSRF уже вынесена; остальному нужны `secrets` и
  `audit`.
- **Cut lines:** защита переехала в `netguard`; сохранение — нет.

### internal/events  — входящие события + доставка вебхуков
- **Ответственность:** принимать входящие события, проверять подписи/время и
  доставлять исходящие вебхуки с бюджетом повторов, dead-letter и повтором.
- **Public API:** `Gateway`, `Dispatcher`, `DeliverySender`, `Poster` (интерфейс),
  `Subscription`, `Delivery`, `InboundEvent`, `CreateSubscriptionRequest`;
  функции `NewGateway`, `NewDispatcher`, `NewDeliverySender`,
  `NewDeliverySenderWithPoster`, `NewPlainPoster`, `SignInboundForTest`,
  `CorrelationIDFrom`; константы `MaxInboundPayloadBytes`, `TimestampTolerance`.
- **Internal deps:** `audit, database, httpx, integrations, logging, outbox,
  rbac, secrets`. **8 внутренних зависимостей.**
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `Poster` — документированный транспортный шов; `NewPlainPoster` —
  неохраняемый постер только для теста.
- **Tests:** `testsupport/webhook_sink_test.go`, `webhook_deadletter_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/events`, с
  `audit`, `database`, `httpx`, `integrations`, `logging`, `outbox`, `rbac` и
  `secrets` как зависимостями, все доказаны. Smoke закрепляет контракт подписи,
  допуска и предела payload.
- **Cut lines:** интерфейс `Poster` — граница.

### internal/notify  — почта + Telegram
- **Ответственность:** почта SMTP и доставка Telegram; коды связи; обработка команд.
- **Public API:** `EmailService`, `TelegramService`, `Message`, `LinkRequest`,
  `Update`; функции `NewEmailService`, `NewTelegramService`; переменные `ErrDisabled`,
  `ErrTelegramDisabled`, `AllowedCommands`; константа `LinkCodeTTL`.
- **Internal deps:** `audit, config, database, httpx, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, stdlib `net/smtp`.
- **Fake:** `Enabled()` терпит nil; когда выключено, это no-op. **Реалистичного
  фейкового транспорта нет.**
- **Tests:** нет в пакете (нет `_test.go`). Это пробел.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/notify`, с
  `audit`, `config`, `database`, `httpx`, `rbac` и `secrets` как зависимостями, все
  доказаны. Smoke доказывает, что выключенный транспорт сообщает off и отказывается
  отправлять. Прежнее «понадобится интерфейс `Mailer`» было неверным: транспорты уже
  терпят nil-получатель и возвращают `ErrDisabled`.
- **Cut lines:** для извлекаемости понадобился бы интерфейс `Mailer`.

### internal/oidc  — единый вход
- **Ответственность:** поток authorization-code OpenID Connect с PKCE и nonce и
  реестр провайдеров идентичности. Он начинает вход (сохраняя состояние, nonce и
  PKCE-верификатор на сервере), завершает его (проверяя подпись, издателя, аудиторию,
  срок и nonce ID-токена, прежде чем читать утверждение), разрешает идентичность в
  локальный аккаунт и опционально провижинит его. Локальный аккаунт имеет ключ
  `(provider, subject)`, никогда email. Клиентский секрет запечатывается кольцом
  ключей платформы и никогда не возвращается API.
- **Public API:** `Service`, `Provider`, `ProviderInput`, `PublicProvider`,
  `Identity`; функции `NewService`, `Enabled`, `List`, `EnabledProviders`,
  `Create`, `Update`, `Delete`, `Begin`, `Complete`, `ListIdentities`,
  `RedirectURL`, `LoginURL`; переменные `ErrUnknownProvider`, `ErrInvalidProvider`,
  `ErrTokenInvalid`, `ErrDisabled`.
- **Internal deps:** `audit, config, database, secrets`.
- **External deps:** `github.com/coreos/go-oidc/v3/oidc`,
  `golang.org/x/oauth2`, `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** нет. Тесты используют провайдер в процессе, публикующий реальные документы
  обнаружения и JWKS и подписывающий реальные ID-токены, а не мок.
- **Tests:** `oidc_test.go` (валидация ввода провайдера) плюс
  `api/sso_integration_test.go` — весь поток через реальный стек HTTP против
  PostgreSQL, включая неверного издателя, аудиторию, nonce и неподтверждённый email.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/oidc` (с `audit`,
  `config`, `database`, `secrets`, `logging`, `httpx`, `ratelimit` и `retention`,
  каждый доказан по очереди); программа smoke движет `Public`, `Enabled`,
  `RedirectURL` и `LoginURL` в чистом модуле. Он не держит данных CRM и не читает
  принципала, поэтому не несёт зависимости прикладного слоя.
- **Cut lines:** переезжает с `secrets`, `database`, `config`, `migrate`.

### internal/authn  — аутентификация
- **Ответственность:** пароли (Argon2), сессии, TOTP MFA, коды восстановления,
  попытки входа и блокировка, поверхность конфигурации OIDC.
- **Public API:** `Service`, `LoginRequest`, `LoginResult`, `Session`, `User`;
  функции `NewService`, `ValidatePassword`, `CorrelationContext`; константы
  `MFATicketTTL`, `MinPasswordLength`, `ResetTokenTTL`, `StepUpWindow`.
- **Internal deps:** `audit, config, database, logging, rbac, secrets`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`,
  `github.com/pquerna/otp`, `.../totp`.
- **Fake:** нет.
- **Tests:** `authn_test.go` (политика паролей) + `testsupport/concurrency_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/authn`, с `audit`,
  `config`, `database`, `httpx`, `logging`, `migrate`, `migrations`, `ratelimit`,
  `rbac`, `retention` и `secrets` как зависимостями, все доказаны. Прежнее `no`
  говорило, что выделение неразумно «пока те не переедут первыми»; они все переехали,
  поэтому теперь переезжает и он. Smoke закрепляет политику паролей и окно step-up.
- **Cut lines:** переезжает с `rbac`, `secrets`, `database`, `config`, `migrate`.

---

## Бэкенд — платформа / инфраструктура

### internal/rbac  — каталог разрешений
- **Ответственность:** каталог разрешений и `Principal` с `Has`/`HasAll`/`HasAny`/
  `Require`. Должен оставаться в шаге с миграцией 0004.
- **Public API:** `Permission`, `Principal`, `Role`, `Service`; функции
  `NewService`, `NewPrincipal`, `All`, `Known`; `ErrForbidden`.
- **Internal deps:** `database`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** `NewPrincipal` строит принципала из явного набора разрешений — годится
  как фейковый принципал в тестах.
- **Tests:** `principal_test.go` + `testsupport` (совпадение сида/каталога).
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/rbac`, с
  `database` как единственной зависимостью. Пакет выделяется целиком, а не раздельно:
  документы однажды предлагали отделить чистый каталог и `Principal` от сервиса на
  базе, но каталог, принципал и сервис делят тип разрешения и уже поднимаются вместе,
  поэтому разделение было бы изменением кода, а не границы. Граница закреплена в
  `archtest`: `rbac` может импортировать `database` и ничего больше в модуле.
- **Cut lines:** переезжает с `database`.

### internal/audit
- **Ответственность:** журнал действий «только добавление» (`audit_log`,
  обеспечиваемый триггером БД) и поток событий безопасности (`security_events`).
- **Public API:** `Logger`, `Entry`, `Action`, `SecurityEvent`; функции `New`,
  `StrPtr`, `UUIDPtr`.
- **Internal deps:** `database, logging`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** нет (пишет в БД). Использует окружающую транзакцию.
- **Tests:** `audit_test.go` (словарь действий, порядок валидации, который держит
  неприписываемую строку вне журнала, и помощники маршалинга) плюс интеграция через
  `testsupport`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/audit`, с
  `database` и `logging` как двумя зависимостями. Его схема живёт в миграции 0001,
  поэтому реальное выделение понесло бы миграцию с собой; это указано, а не скрыто.
  Набор доказывает, что у Go-пакета нет связи с приложением.
- **Cut lines:** переезжает с `database` и `logging`.

### internal/database
- **Ответственность:** пул соединений, `WithTx`, `TxFromContext`, классификация
  ошибок (`IsNoRows`, `IsUniqueViolation`, …), `Querier`.
- **Public API:** `DB`, `Tx`, `Querier` (интерфейс), `Connect`, `ConnectWithPool`,
  `WithTx`, `TxFromContext`, помощники `Is*`, `DefaultMaxConns`, `MaxAllowedConns`.
- **Internal deps:** нет.
- **External deps:** `github.com/jackc/pgx/v5`, `.../pgconn`, `.../pgxpool`.
- **Fake:** `Querier` — интерфейс (шов). БД в памяти нет.
- **Tests:** `pool_test.go` (сервер не нужен: граница пула проверяется до попытки
  соединения).
- **Extract:** **`yes`** — лист без внутренних зависимостей; выделен и доказан
  `scripts/extract-test/database`. Его защита пула и контракт транзакции тестируемы
  без сервера, что и делает выделение дешёвым. То, что это общий субстрат, — факт о
  желательности, а не о выполнимости, и гарантия, которую стоит иметь, в том, что он
  *может* подняться.
- **Cut lines:** это общий компонент инфраструктуры.

### internal/httpx  — примитивы HTTP
- **Ответственность:** проблемные ответы RFC 9457, список/пагинация/сортировка,
  middleware request ID + журнала доступа + восстановления + заголовков безопасности,
  определение адреса клиента за доверенным прокси, middleware ограничения частоты.
- **Public API:** `APIError`, `ErrorCode`, `ListResponse`, `Pagination`,
  `SortSpec`, `RateLimitOptions`, `SubjectFunc`, `TrustedProxies`; функции
  `WriteError`, `WriteJSON`, `WriteJSONList`, `WriteJSONStatus`, `DecodeJSON`,
  `ParsePagination`, `ParseSort`, `NewError`, `NewListResponse`, `PathParam`,
  `RequestID`, `ClientIP`, `ClientIPFrom`, `TrustedProxiesFrom` и конструкторы
  middleware.
- **Internal deps:** `logging, ratelimit`.
- **External deps:** `github.com/go-chi/chi/v5`, `github.com/google/uuid`.
- **Fake:** `SubjectFunc` внедряет субъект ограничения частоты.
- **Tests:** 4 модульных файла, включая тесты доверия адресу клиента.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/httpx` вместе с
  `logging` и `ratelimit`. Это общий HTTP-субстрат, поэтому выделять его сам по себе
  обычно не *желательно*, но теперь это *возможно*, и именно это не даёт обработчику
  дотянуться в него и затащить прикладной слой обратно.
- **Cut lines:** переезжает с `logging` и `ratelimit`.

### internal/ratelimit  — подключаемый лимитер
- **Ответственность:** ограничение с фиксированным окном через абстракцию `Store`.
  `MemoryStore` (один процесс, с очисткой, ограниченный) и `RedisStore` (атомарный
  Lua, несколько реплик), плюс политика fail-open/fail-closed.
- **Public API:** `Store` (интерфейс), `Limiter`, `Policy`, `Decision`, `FailMode`,
  `MemoryStore`, `RedisStore`, `RedisOptions`; функции `New`, `NewMemoryStore`,
  `NewRedisStore`; `ErrUnavailable`.
- **Internal deps:** нет.
- **External deps:** `github.com/redis/go-redis/v9`.
- **Fake:** `MemoryStore` — реальная, годная реализация в процессе — самый сильный
  существующий «фейк» в кодовой базе. Явного `NoopStore` нет.
- **Tests:** `ratelimit_test.go` (память, политики) + `redis_integration_test.go`.
- **Extract:** `yes` — **ноль внутренних зависимостей**; Redis опционален. Путь в
  памяти был выделен, собран и модульно протестирован в чистом модуле (путь Redis
  скомпилировался, но не был там упражнён). См. `EXTRACT_READY.md` (Кандидат 2).
- **Cut lines:** весь пакет переезжает; Redis — зависимость времени сборки только у
  файла, который строит `RedisStore`.

### internal/retention  — жизненный цикл данных
- **Ответственность:** окна хранения по таблицам, пробный прогон, пакетное удаление,
  защищённые таблицы, предикаты статуса, политики, движимые истечением.
- **Public API:** `Runner`, `Policy`, `Options`, `Result`; функции `New`,
  `Policies`, `ValidateWindows`; `ErrDisabled`.
- **Internal deps:** `audit, database`.
- **External deps:** нет сверх stdlib.
- **Fake:** `DryRun` — поза по умолчанию; `Runner` nil-безопасен.
- **Tests:** `retention_test.go` + `testsupport/retention_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/retention`, с
  `audit` и `database` как зависимостями. Его тесты безопасности идут в выделенном
  модуле.
- **Cut lines:** переезжает с `audit`/`database`.

### internal/views  — сохранённые представления списков
- **Ответственность:** именованные, переиспользуемые выборы фильтра/сортировки/
  столбцов для одного экрана списка CRM. Сохранённый документ — это ровно набор
  параметров запроса, которые принимают эндпоинты списков, поэтому представление не
  может назвать фильтр, которого не соблюдает ни один эндпоинт.
- **Public API:** `Service`, `NewService`, `Input`, `View`, `FilterSet`,
  `EntityType`, `ValidationError`, `ErrNotFound`, `ErrForbidden`; методы
  `Create`, `List`, `Get`, `Update`, `Delete`.
- **Internal deps:** `audit, database, rbac`.
- **External deps:** `github.com/google/uuid`.
- **Fake:** не нужен; видимость и владение — предикаты SQL.
- **Tests:** `views_test.go` (модуль) + `testsupport/views_test.go` (PostgreSQL).
- **Extract:** `almost` — у него нет зависимости от CRM, но он читает принципала
  вызывающего из `rbac` и пишет журнал аудита, поэтому переезжает с ними. Доказан
  `scripts/extract-test/views`, который выделяет его с `audit`, `database`, `rbac` и
  `logging` и движет набор типов сущностей и круговой рейс фильтра в чистом модуле.
- **Cut lines:** переезжает с `audit`/`database`/`rbac`.

### internal/netguard  — исходящая защита SSRF
- **Ответственность:** единственное место, которое решает, безопасно ли назначение
  исходящего запроса: отклоняет loopback, приватные, link-local, multicast и
  неопределённые адреса и схемы, отличные от `http(s)`, для самого URL и для каждого
  разрешённого IP. Используется Хабом интеграций и объектным хранилищем S3.
- **Public API:** `GuardedClient`, `NewGuardedClient`, `ValidateOutboundURL`,
  `IsBlockedIP`; `ErrBlocked`.
- **Internal deps:** нет — **только стандартная библиотека.**
- **External deps:** нет сверх stdlib.
- **Fake:** не нужен; решение чистое.
- **Tests:** `netguard_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/netguard`; setup
  также утверждает, что `go.mod` остаётся свободным от любого `require`.
- **Cut lines:** уже отрезан. Это лист, поэтому что угодно может зависеть от него, а
  он не зависит ни от чего.

### internal/objectstore  — слой провайдера объектного хранилища
- **Ответственность:** *механизм* хранения: контракт `ObjectStore`, провайдер
  `Memory` в процессе, провайдер `S3`, совместимый с S3, и чистые помощники для
  ключей объектов и типов содержимого. Он **не** решает, кто может читать или писать
  объект, и не трогает базу данных — это `storage.Service`.
- **Public API:** `ObjectStore` (интерфейс), `Memory`, `S3`, `ObjectInfo`,
  `ObjectMeta`, `NormaliseContentType`, `IsAllowedContentType`; функции `NewMemory`,
  `NewS3`; `ErrObjectMissing`.
- **Internal deps:** `netguard`.
- **External deps:** `github.com/google/uuid`, `github.com/minio/minio-go/v7`,
  `.../pkg/credentials`.
- **Fake:** `Memory` (`NewMemory`) — **реальный `ObjectStore` в процессе**,
  используемый и тестами, и профилем локальной разработки.
- **Tests:** `contract_test.go` (общий контракт поведения, идущий против `Memory`),
  `s3_contract_test.go` (тот же контракт против реального протокола S3 через
  `gofakes3`), `helpers_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/objectstore`.
- **Cut lines:** уже отрезан, из `internal/storage`. `storage.Service` зависит от
  него; зависимость не указывает обратно.

### internal/storage  — сервис вложений (авторизация + метаданные)
- **Ответственность:** вложения: оркестрация загрузки/скачивания, строки метаданных,
  привязка к сущности, проверки тенанта/прав, лимиты размера/типа, сверка осиротевших.
  Механизм делегирует `internal/objectstore`.
- **Public API:** `Service`, `Config`, `Document`, `UploadInput`,
  `ReconcileReport`; функции `New`; `ErrNotConfigured`. Имена `ObjectStore`, `Memory`,
  `S3`, `ObjectInfo` и `ObjectMeta` реэкспортируются как алиасы, поэтому существующие
  вызывающие неизменны.
- **Internal deps:** `audit, database, httpx, objectstore, rbac`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** `objectstore.Memory` через реэкспортированный `NewMemory`.
- **Tests:** `storage_test.go` (правила ключа/расширения/сущности) +
  `testsupport/storage_s3_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/storage`, с
  `audit`, `database`, `httpx`, `objectstore` (и его `netguard`), `ratelimit` и
  `rbac` как зависимостями, все доказаны. Привязка к `rbac`/`audit`/`database` по
  дизайну — не блокер, когда все три доказаны. **было:** `no` — `Service` по дизайну
  привязывается к `rbac`/`audit`/`database`; переносимая половина (`objectstore`) уже
  вынесена.
- **Cut lines:** слой провайдера переехал в `internal/objectstore`; `Service` остаётся.

### internal/jobs
- **Ответственность:** очередь фоновых заданий: постановка, заявка (`SKIP LOCKED`),
  повтор, dead-letter.
- **Public API:** `Queue`, `Job`, `EnqueueRequest`, `NewQueue`, `ErrNotRetryable`.
- **Internal deps:** `database`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** нет.
- **Tests:** `jobs_test.go` (словари статуса/вида, валидация до записи и задержка
  повтора; первые тесты внутри пакета).
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/jobs`, с
  `database` как зависимостью. Написание теста задержки нашло реальный скрытый баг:
  сдвиг был ограничен 10, поэтому задержка упиралась в 1024с, а документированный
  потолок в 30 минут был недостижимым мёртвым кодом; ограничение теперь 11.
- **Cut lines:** переезжает с `database`.

### internal/outbox  — транзакционный outbox
- **Ответственность:** писать события в той же транзакции, что и изменение; воркер их
  разбирает.
- **Public API:** `Store`, `Envelope`, `Event`, `NewStore`, `EventTypes`.
- **Internal deps:** `database, logging`.
- **External deps:** `github.com/google/uuid`, `github.com/jackc/pgx/v5`.
- **Fake:** нет.
- **Tests:** `outbox_test.go` (словарь событий).
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/outbox`, с
  `database` и `logging` как двумя зависимостями. Его собственный тест идёт в
  выделенном модуле.
- **Cut lines:** переезжает с `database`/`logging`.

### internal/secrets
- **Ответственность:** хеширование паролей Argon2, хеширование токенов, подпись HMAC,
  шифрование секретов интеграций (кольцо ключей).
- **Public API:** `Keyring`, `Argon2Params`; функции `HashPassword`,
  `HashPasswordWithParams`, `VerifyPassword`, `HashToken`, `SignPayload`,
  `VerifySignature`, `RandomToken`, `RandomNumericCode`, `ConstantTimeEqual`,
  `DefaultArgon2Params`, `NewKeyring`, `IsSensitiveKey`; `ErrInvalidHash`,
  `ErrPasswordMismatch`.
- **Internal deps:** нет.
- **External deps:** `golang.org/x/crypto/argon2`.
- **Fake:** неприменимо (чистая криптография).
- **Tests:** `secrets_test.go` (20 тестов: хеширование, путь искажённого хеша, кольцо
  ключей, подписи CSRF и вебхука).
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/secrets`.
- **Cut lines:** весь пакет переезжает.

### internal/config
- **Ответственность:** разбор и валидация окружения для каждой подсистемы.
- **Public API:** `Config`, `AIConfig`, `EmailConfig`, `OIDCConfig`,
  `RetentionConfig`, `StorageConfig`, `TelegramConfig`, `Load`.
- **Internal deps:** `database, httpx, retention`.
- **External deps:** нет сверх stdlib.
- **Fake:** неприменимо.
- **Tests:** `config_test.go`.
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/config`, с
  `database`, `httpx` и `retention` как зависимостями, все доказаны. Именование
  конфигурации каждой подсистемы — не блокер, когда пакеты, которые он называет,
  доказаны.
- **Cut lines:** рецепт генерирует собственную урезанную конфигурацию.

### internal/logging
- **Ответственность:** структурная настройка `slog` и сокрытие чувствительных ключей;
  действующее лицо/организация/корреляция в области запроса.
- **Public API:** `New`, `Redact`, `IsSensitiveKey`, `WithActor`,
  `WithCorrelationID`, `WithOrganization`, `Actor`, `CorrelationID`,
  `Organization`.
- **Internal deps:** нет.
- **External deps:** stdlib `log/slog`.
- **Fake:** неприменимо.
- **Tests:** `logging_test.go` (сокрытие через `New`, `With` и вложенные группы;
  помощники контекста; связывание уровня/формата).
- **Extract:** **`yes`** — выделен и доказан `scripts/extract-test/logging`.
- **Cut lines:** переезжает один.

### internal/migrate / internal/migrations
- **Ответственность:** встроенные SQL-миграции и раннер миграций (`Up`/`Down`) над
  `golang-migrate`; `migrations.FS` — встроенная ФС.
- **Public API:** `migrate.Up`, `migrate.Down`, `migrations.FS`.
- **Internal deps:** `migrate -> migrations`.
- **External deps:** `github.com/golang-migrate/migrate/v4` (+ postgres, iofs).
- **Fake:** неприменимо.
- **Tests:** нет в пакете; применяются в CI и интеграции.
- **Extract:** **`yes`** — `migrations` и `migrate` каждый выделены и доказаны
  `scripts/extract-test/migrations` и `scripts/extract-test/migrate`. `migrations` —
  лист в коде, но несёт и данные: директива `//go:embed` называет `*.sql`, поэтому
  выделение должно скопировать SQL-файлы — копия только Go не собирается, и набор их
  копирует. Прежнее `no` («схема общеприкладная») было неверным: схема — это данные,
  которые бинарник встраивает, а не зависимость от приложения.
- **Cut lines:** переезжает с SQL, который встраивает.

### internal/openapi  — генерация спецификации
- **Ответственность:** строить `docs/api/openapi.json` из живого роутера и таблицы
  `api.APIContract`.
- **Public API:** `Build`, `Report`, `Routes`, `Route`, `Contract`, `Info`,
  `Server`, `Group`, `QueryParam`, `PathParamSchema`, `Permissions`, `PrefixAPI`,
  `TagFor`, `DefaultDescription`, `Anonymous`, `SessionOnly`.
- **Internal deps:** нет (его импортируют *его* `api` и `openapi-gen`).
- **External deps:** `github.com/go-chi/chi/v5`, `github.com/google/uuid`.
- **Fake:** неприменимо.
- **Tests:** `openapi_test.go` (дрейф, объявления доступа).
- **Extract:** `no` — он описывает маршруты этого приложения.
- **Cut lines:** рецепт получает собственный генератор или ни одного.

### internal/wiring  — корень композиции
- **Ответственность:** единственное место сборки графа зависимостей; возвращает
  структуру `Services`.
- **Public API:** `Build`, `Services`.
- **Internal deps:** 22 пакета.
- **Tests:** нет; упражняется каждым интеграционным тестом.
- **Extract:** `no` — он противоположность модулю.
- **Cut lines:** рецепт его заменяет.

### internal/archtest  — правила зависимостей
- **Ответственность:** обеспечение правил слоистости только тестами (ни один пакет не
  импортирует `api`; production не импортирует `testsupport`; ядро не импортирует
  композицию/HTTP; каждый внутренний пакет достижим из команды).
- **Public API:** только тестовые функции.
- **Internal deps:** нет.
- **Tests:** 5 тестов, все читают реальный граф через `go list`.
- **Extract:** `almost` (как переиспользуемый инструмент управления).
- **Cut lines:** переносим как шаблон; путь модуля зашит.

### internal/testsupport  — тестовый набор
- **Ответственность:** каркас интеграционных тестов: открывает черновую базу,
  удаляет/пересобирает `public`, применяет миграции.
- **Public API:** только тестовые помощники.
- **Internal deps:** много (он собирает приложение).
- **Tests:** 15 интеграционных файлов (`//go:build integration`).
- **Extract:** `no` — никогда не поставляется в бинарнике.

---

## Фронтенд (`frontend/src`)

| Модуль | Ответственность | Ключевые файлы | Зависит от | Тесты | Extract |
| --- | --- | --- | --- | --- | --- |
| **API clients** | по модулю на каждую область бэкенда над общим `client.ts` | `api/client.ts`, `api/{auth,crm,imports,integrations,workflows,simulation,intelligence,ai,assist,documents,notifications,agent,admin}.ts` | формы OpenAPI бэкенда; `types/api.ts` | `api/client.test.ts` | `almost` (ядро клиента общее) |
| **Types** | написанные руками типы ответа/запроса | `types/api.ts` | — | проверяются типами | `almost` |
| **Auth hook** | сессия, шлюз по правам | `hooks/useAuth.tsx`, `components/RequirePermission.tsx` | `api/auth.ts` | `RequirePermission.test.tsx` | `no` |
| **Search hook** | глобальный поиск с задержкой | `hooks/useGlobalSearch.ts` | `api/*` | `GlobalSearch.test.tsx` | `almost` |
| **UI kit** | общие примитивы (Card, Alert, Table, …) | `components/ui.tsx` | — | `ui.test.ts` | `almost` |
| **Shell/nav** | каркас приложения и обвязка маршрутизации | `components/AppShell.tsx`, `pages/ErrorPages.tsx` | auth | через E2E | `no` |
| **ResourceList / BulkArchive** | общий список + массовые действия | `components/ResourceList.tsx`, `BulkArchiveButton.tsx` | `api/crm.ts` | `.test.tsx` (2) | `almost` |
| **CRM pages** | аккаунты/контакты/лиды/сделки/задачи/обращения | `pages/{Accounts,Account,Contacts,Leads,Deals,Tasks,Tickets}Page.tsx` | `api/crm.ts` | E2E | `no` |
| **Record panels** | кастомные поля + теги/комментарии на странице детали | `components/{CustomFieldsPanel,RecordCollabPanel}.tsx` | `api/crm.ts` | `.test.tsx` (2) | `almost` |
| **AssistPanel** | поверхность контекстного ИИ | `components/AssistPanel.tsx`, `api/assist.ts` | `api/assist.ts` | `AssistPanel.test.tsx` | `almost` |
| **Workflow editor** | редактор шагов + валидация | `components/WorkflowEditor.tsx`, `pages/WorkflowsPage.tsx`, `api/workflows.ts` | `api/workflows.ts` | `WorkflowEditor.test.tsx` | `almost` |
| **Simulation page** | UI Simulation Lab | `pages/SimulationPage.tsx`, `api/simulation.ts` | `api/simulation.ts` | `SimulationPage.test.tsx` | `almost` |
| **Intelligence pages** | UI прогноза + риска | `pages/{Forecast,Risk}Page.tsx`, `api/intelligence.ts` | `api/intelligence.ts` | `.test.tsx` (2) | `almost` |
| **AI/MCP page** | статус провайдера, каталог инструментов | `pages/AiPage.tsx`, `api/ai.ts` | `api/ai.ts` | E2E | `almost` |
| **Import pages** | UI Центра импорта, экспорт CSV/JSONL | `pages/{ImportCenter,ImportDetail}Page.tsx`, `api/imports.ts` | `api/imports.ts` | `ImportCenterPage.test.tsx`, E2E | `no` |
| **Integrations/Webhooks/Events/Jobs pages** | поверхности администрирования | `pages/{Integrations,Webhooks,Events,Jobs,Migrations}Page.tsx` + соответствующие клиенты api | их клиенты api | E2E | `no` |
| **Notifications page** | входящие уведомления workflow | `pages/NotificationsPage.tsx` | `api/crm.ts` | `NotificationsPage.test.tsx` | `almost` |
| **Admin pages** | аудит, пользователи, роли, организации, команды, безопасность, диагностика (+ настройки платформы), почта, telegram, SSO, согласие | `pages/admin/*.tsx`, `api/admin.ts` | `api/admin.ts` | `SecurityPage.test.tsx`, `DiagnosticsPage.test.tsx`, `SSOPage.test.tsx`, `ConsentPanel.test.tsx` | `no` |
| **Attachments** | загрузка/скачивание документов | `components/AttachmentPanel.tsx`, `api/documents.ts` | `api/documents.ts` | `AttachmentPanel.test.tsx` | `almost` |
| **Saved views** | именованные фильтры списка на экран | `components/SavedViewsBar.tsx`, `api/views.ts` | `api/views.ts` | `SavedViewsBar.test.tsx`, E2E | `almost` |

**Итоги фронтенда:** 98 файлов `.ts`/`.tsx`; 24 файла модульных тестов (159 тестов);
E2E в `frontend/e2e/platform.spec.ts` — 59 тестов, из них часть пропускается по
конфигурации окружения (например, ветки, зависящие от настроенного AI-провайдера).

---

## Сводка

- **Пакетов бэкенда в инвентаризации:** 38 (каталоги `internal/*`; `importx` несёт два
  подпакета, поэтому `go list` сообщает 39), плюс 3 команды.
- **Слоёв фронтенда в инвентаризации:** 20.
- **Циклы:** нет (проверено `go list` + DFS; см. `DEPENDENCY_MAP.md`).
- **Пакеты с ≥8 внутренними зависимостями:** `api` (26), `wiring` (22), `importx` (10),
  `ai` (9), `events` (8); `cmd/forgecrm-worker` (17) и `cmd/forgecrm-api` (11) —
  корни композиции, не модули.
- **Реальные фейки в процессе, существующие сегодня:** `objectstore.Memory`
  (`NewMemory`, реэкспортированный как `storage.NewMemory`), `ratelimit.MemoryStore`.
  Документированные швы внедрения без поставляемого фейка: `ai.Provider`,
  `events.Poster`, `integrations.Adapter`, `migratex.Source`, `agent.Executor`,
  `database.Querier`, `objectstore.ObjectStore`, `ratelimit.Store`.
- **Статус Extract:** тридцать три модуля — `yes` — `analytics`, `ratelimit`,
  `netguard`, `objectstore`, `secrets`, `logging`, `httpx`, `importx/safe`,
  `importx/parsers`, `database`, `rbac`, `audit`, `outbox`, `retention`, `agent`,
  `jobs`, `crm`, `storage`, `integrations`, `config`, `workflow`, `events`,
  `migratex`, `simulate`, `forecast`, `risk`, `notify`, `ai`, `importx`,
  `migrations`, `migrate`, `authn` и `constructor` —
  потому что они были
  действительно выделены, собраны, протестированы и запущены в чистом модуле, теперь
  автоматизированным набором в `scripts/extract-test/`. Статусы здесь в точности
  совпадают с `EXTRACT_READY.md`.
- **Найденные пробелы:** пакеты **без единого `_test.go`** — `audit`, `crm`,
  `events`, `forecast`, `jobs`, `migrate`, `migratex`, `migrations`, `notify`,
  `risk`, `simulate`, `wiring`. Из них `crm`, `events`, `forecast`, `risk` и
  `simulate` имеют интеграционное покрытие через `internal/testsupport`; `audit`,
  `jobs`, `notify` и `wiring` покрыты только косвенно. `logging` и `secrets` получили
  тесты внутри пакета в срезе выделения, что и позволило пометить их `READY`. У `ai`
  две реальные реализации `Provider`, но нет фейка в процессе; в `ratelimit` нет
  `NoopStore`, и нигде нет авторизатора `AllowAll` (авторизация обеспечивается
  встроенно в middleware `api` плюс проверки владения в `crm`).
