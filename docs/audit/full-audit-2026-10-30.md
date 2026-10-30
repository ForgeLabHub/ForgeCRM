# ForgeCRM — полный аудит состояния проекта

- **Дата:** 2026-10-30
- **Репозиторий:** `https://github.com/ForgeLabHub/ForgeCRM` (origin), ветка `main`
- **Проверяемый коммит:** `49ec175`, обновлён до `cb93db0`
- **Метод:** проверка по фактическому коду и прогонам, а не по прежним отчётам.
  Все числа ниже получены командами на этом коммите; там, где результат —
  утверждение о поведении, указан запущенный тест.

> **Обновление (цикл 14).** После `49ec175` в `main` вошли `c6b10e0` (SSO/OIDC),
> `b23fd44` (потоковая выдача AI по SSE) и `cb93db0` (документация streaming и
> исправление счётчиков схемы). Разделы §4.3 и §6 обновлены под фактическое
> состояние: потоковая выдача реализована. Полный прогон на `cb93db0`: backend
> unit + integration — 31 пакет ok; frontend — 19 файлов / 134 теста; typecheck,
> lint, build — чисто; E2E Chromium — 49 passed, 1 skipped (нужен живой AI-провайдер).

---

## 1. Как получены факты

```bash
# состояние дерева
git rev-parse --abbrev-ref HEAD      # main
git log --oneline -1                 # cb93db0

# backend
cd backend
export PATH=$PATH:/usr/local/go/bin GOTOOLCHAIN=local
go build ./...                       # ok
gofmt -l ./cmd ./internal            # пусто
go vet ./...                         # чисто
go test -race -count=1 ./...         # все пакеты ok, 0 FAIL

# интеграция (реальный PostgreSQL, схема пересоздаётся)
FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable' \
  go test -count=1 -p 1 -tags=integration ./...   # 31 пакет ok, 0 FAIL

# спецификация API генерируется из роутера
go run ./cmd/openapi-gen -check      # "... is current"

# frontend
cd ../frontend
npm run test                         # 19 файлов, 134 теста — ok
npm run typecheck                    # tsc -b --noEmit — чисто
npm run lint                         # 0 ошибок, 7 предупреждений
npm run build                        # ok
npx playwright test                  # 49 passed, 1 skipped (реальный API + PostgreSQL + Chromium)
```

> Примечание по запуску интеграционных тестов: их обязательно нужно запускать с
> `-p 1`. Пакеты интеграционных тестов делят одну схему и пересоздают её, поэтому
> параллельный запуск даёт ложные ошибки `relation "users" does not exist`. В CI
> это уже учтено (`.github/workflows/ci.yml`), и локальный прогон должен повторять
> тот же флаг.

---

## 2. Что сделано в этой сессии

| Коммит | Что |
|---|---|
| `b23fd44` | Потоковая выдача AI по SSE: `internal/ai/streaming.go` (интерфейс `StreamProvider` + реализация для Ollama и OpenAI-совместимого провайдера + `Service.Stream` с откатом на буферизацию), `internal/api/ai_stream_handlers.go` (маршруты `/ai/stream/chat` и `/ai/stream/assist/{id}`), `sseWriter` с флагами против прокси-буферизации; защита от nil-DB в `ai.go`; тесты `internal/ai/streaming_test.go`, `internal/api/ai_stream_test.go`, `internal/testsupport/ai_stream_test.go` (PostgreSQL, изоляция арендаторов); фронт — `parseSSEFrame`/`streamEventError` в `client.ts`, метод `stream` в `assistApi`, потоковая выдача в `AssistPanel` с `AbortController`; тесты `client.test.ts`, `AssistPanel.test.tsx` |
| `cb93db0` | Документация: roadmap 3.5 (streaming реализован), CHANGELOG (Added), исправлены счётчики `docs/database/schema.md` (71 таблица / 22 миграции); новый E2E-сценарий на потоковый маршрут |
| `c6b10e0` | SSO/OIDC (не в этой сессии): вход по OpenID Connect, `internal/oidc`, миграция `0022_sso_oidc` |
| `f13a775` | Экспорт доступен из интерфейса: `ApiClient.download`, привязка `exportsApi` к реальному `POST /api/v1/exports`, кнопка Export в Import Center под правом `exports.execute`; backend-интеграционный тест `internal/api/export_test.go`; frontend-тест `ImportCenterPage.test.tsx`; E2E-сценарий |
| `9335a7d` | Исправлено число frontend-тест-файлов в карте модулей (`docs/MODULES.md`) |
| `49ec175` | Спецификация больше не описывает потоковые ответы как JSON: генератор получил объявление `StreamingSuccess`, экспорт и скачивание документа описаны честно (media types + бинарная схема), экспорт возвращает `200`, а не `201`; добавлен сторожевой тест `TestStreamingResponsesAreNotDescribedAsJSON` |

Найденная и исправленная ошибка спецификации была настоящей: `POST /api/v1/exports`
и `GET /api/v1/documents/{id}/download` пишут байты (CSV/JSONL/файл) прямо в ответ,
но документ описывал оба как `application/json`-объект, а экспорт — как `201`.
Клиент, сгенерированный из документа, пытался бы разобрать CSV как JSON.

---

## 3. Состояние проекта

### 3.1 Масштаб

| Метрика | Значение |
|---|---|
| Backend, Go (без тестов) | ~44 136 строк |
| Backend, всего | ~66 180 строк |
| Backend, тест-файлы | 87 файлов, 606 функций `Test*`/`Benchmark*` |
| Пакеты backend | 42 пакета |
| Миграции | 22 пары `NNNN_name.{up,down}.sql` (встроены в бинарник) |
| Frontend, TS/TSX | 85 файлов |
| Frontend, unit-тесты | 19 файлов, 134 теста |
| E2E | 50 сценариев (Chromium, реальный стек), 1 условно пропускаемый |
| Спецификация API | 164 пути, 174 схемы, генерируется из роутера |
| RBAC | 129 прав в каталоге |
| ADR | 13 записей |

### 3.2 Топология

```
ForgeCRM/
├── backend/            Go-модуль github.com/geff1778/forgecrm (Go 1.26.10)
│   ├── cmd/            forgecrm-api, forgecrm-worker, forgecrm, openapi-gen
│   └── internal/       36 пакетов (см. §4)
├── frontend/           React + Vite + TypeScript + TanStack Query (SPA)
├── docs/               архитектура, ADR, roadmap, api, audit, operations
├── scripts/            check-env, setup, migrate, backup, restore, smoke-test, e2e-bootstrap
├── deploy/             конфигурация развёртывания
└── docker-compose*.yml
```

---

## 4. Архитектура и логика

### 4.1 Общая модель

Модульный монолит на Go с двумя процессами (API и worker), общей PostgreSQL и
единственным frontend-SPA. Внутренние пакеты разделены по границам, которые
проверяет `internal/archtest` — это не декларация, а тест: пакет, который тянет
запрещённую зависимость, роняет сборку.

Основные пакеты:

- **api** — HTTP-слой: маршрутизация (chi), обработчики по областям, middleware
  (сессия, CSRF, права, лимиты, аудит).
- **openapi** — генератор спецификации из живого роутера + таблица контракта.
- **rbac** — каталог прав; синхронизируется с миграцией `0004`.
- **authn / authz** — вход, второй фактор, step-up, сессии, API-ключи.
- **crm** — домен: accounts, contacts, deals, leads, tasks, tickets, pipelines,
  теги, комментарии, кастомные поля, поиск.
- **workflow** — движок автоматизации: определение, парсер, выражение, движок,
  хранилище, шаблоны, cron, export/import.
- **ai** — абстракция провайдера, contextual assist, confidence, MCP-протокол.
- **agent** — HITL-исполнение (ADR 0005): предложение → одобрение → выполнение.
- **retention** — политики хранения данных (opt-in).
- **storage / objectstore** — S3-совместимое хранилище и его абстракция.
- **importx** — Import Center: парсеры, определение формата, безопасное
  распаковывание архивов (`importx/safe`), исполнение.
- **integrations** — адаптеры и SSRF-guard.
- **outbox / events / jobs / notify** — события, доставка, очередь, уведомления.
- **analytics / forecast / risk / simulate** — аналитика и прогнозы.
- **audit** — append-only журнал.
- **ratelimit / secrets / netguard / logging / httpx / config / database / migratex / wiring**.

### 4.2 Контракт спецификации

Спецификация не пишется руками. Генератор идёт по роутеру и соединяет три
источника по одному ключу `METHOD /path`: маршрут (какие пути есть), таблицу прав
(какое право нужно) и таблицу контракта `internal/api/contract.go` (какой реальный
DTO у запроса/ответа). Схемы выводятся из Go-типов, поэтому документ не может
описать форму, которую код не принимает. `openapi-gen -check` роняет CI при
расхождении. Правила доступа: право, `openapi.Anonymous` (закрытый список из
шести) или `openapi.SessionOnly`; маршрут без объявленного уровня роняет генерацию.

В этой сессии контракт расширен объявлением `StreamingSuccess` (§2) — это
единственное место, где ответ не JSON, и теперь оно названо явно.

### 4.3 Contextual AI (приоритет 1) — реализовано

`internal/ai` (≈4 400 строк) реализует абстракцию провайдера без привязки к
одному поставщику (`FORGECRM_AI_PROVIDER`: `ollama`, `openai`, `openai_compatible`).

- `assist.go` (1 178 строк) собирает ограниченный, проверенный по правам контекст
  для сущностей **account, contact, deal, ticket, task**. Действия: `summary`,
  `risks`, `next_actions`, `draft`, `recap`, `ask`.
- **Правила, зашитые в код и проверяемые тестами:**
  - право на чтение сущности проверяется на сервере *до* любого запроса — чужую
    сделку не резюмирует даже тот, кто может вызывать AI
    (`TestAssistRefusesToReadAnotherOrganizationsAccount`);
  - модель получает данные, но не полномочия — на этом пути нет вызова инструментов;
  - факты CRM, вывод модели и рекомендации возвращаются в **разных полях**, чтобы
    UI не подал догадку как записанный факт; факты возвращаются дословно
    (`TestAssembleOmitsDataTheCallerCannotRead`);
  - персональные данные (email/телефон) маскируются перед отправкой
    (`TestAssistMasksContactPersonalData`);
  - ничего не пишется в CRM: черновик — это текст, пользователь сохраняет его сам,
    что сохраняет HITL-модель ADR 0005;
  - **grounded confidence** (`confidence.go`) — серверная оценка из проверяемых
    сигналов (структурированность ответа, совпадение цифр с данными, полнота
    контекста), а не самодоклад модели; возвращается вместе с обоснованием.
- **Кэш ответов** — таблица `ai_insights` с `context_hash` (починена миграцией
  `0018`); `refresh` обходит кэш («спросить заново»).
- **Лимиты** — общий `ratelimit.Limiter`, ключ по организации, лимит в минуту.
- **Аудит** — запись `ActionAIInsightGenerated` на каждый реальный вызов.
- **Обработка сбоев** — разные состояния: нет провайдера → `503 disabled`, лимит →
  `429 rate_limited`, недоступен/таймаут → retryable `503 unavailable`; панель
  предлагает повтор только для retryable-случая.
- **Frontend** — `AssistPanel` на экранах account, contact, deal, ticket, task.
- **Потоковая выдача — реализована (цикл 14).** `POST /api/v1/ai/stream/chat` и
  `POST /api/v1/ai/stream/assist/{id}` отвечают server-sent events: `delta`-события
  несут текст по мере генерации, завершающее `done` — тот же результат, что и
  буферизованный маршрут, а `error`-событие — тот же код, что вернул бы буферный
  маршрут (`disabled` / `rate_limited` / `unavailable`). Оба маршрута вызывают
  один и тот же сервис, поэтому проверки прав, лимит и запись в `ai_tasks`
  идентичны; отличается только транспорт. Буферизованные маршруты не изменены, и
  панель откатывается на буферный запрос, если поток закрылся без результата
  (так ведёт себя прокси, проглотивший тело). Провайдер, реализующий только
  базовый интерфейс, буферизуется и отдаётся одним `delta`.

### 4.4 Retention и rotation (приоритет 2) — реализовано

`internal/retention` (566 строк): реестр политик, **по умолчанию выключен**,
**dry-run по умолчанию**, пакетное удаление, защита append-only аудита, счётчики по
каждой политике и одна запись аудита на реальный прогон.

Покрытые таблицы (22 политики): `audit_log` (защищён), `ai_insights`, `ai_tasks`,
`agent_runs`, `workflow_runs`, `webhook_deliveries` (в т.ч. `_dead`, `_delivered`),
`outbox_events`, `events_inbox`, `jobs`, `export_jobs`, `import_jobs`,
`notifications`, `sessions`, `security_events` и др.

Проверяется 13 интеграционными тестами: окно, dry-run, идемпотентность, защита
таблиц, отказ чистить `audit_log`, минимальное окно, пакетная обработка,
tenant-agnostic, запись аудита, наличие индексов (`TestRetentionSweepIndexesExist`).
Фоновая задача запускается из worker (`runMaintenanceLoop` → `retention.Run`),
метрики отдаются в `/api/v1/system/metrics` (`retention_runs_total`,
`retention_deleted_total`).

### 4.5 S3-совместимое хранилище (приоритет 3) — реализовано

- `internal/objectstore` — абстракция `ObjectStore` + реализации `memory` (тесты)
  и `s3` (MinIO-совместимая, 355 строк).
- `internal/storage` — сервис поверх объектного хранилища: метаданные в таблице
  `documents` (миграция `0016`), серверно-построенные ключи объектов, проверки
  прав на маршруте, SSRF-guard на endpoint, реконсиляция «объект есть, записи нет»
  и наоборот (`reconcile.go`).
- Frontend: `AttachmentPanel` — загрузка, скачивание (по `download_url` от
  сервера), архивация. `GET /api/v1/system/capabilities` отдаёт `storage` и
  `max_upload_bytes`.
- Конфигурация через окружение: `FORGECRM_STORAGE_*` (endpoint, bucket, region,
  ключи, path-style, TTL presign, max bytes, таймаут, `ALLOW_PRIVATE_ENDPOINT`).
- Тесты: `storage_test.go`, `storage_s3_test.go`, `objectstore` contract-тесты
  (`TestS3RoundTrip`, `TestS3MissingObjectIsNamed`, `TestS3PresignServesTheObject`,
  `TestS3ServiceOverRealStore`).

### 4.6 Workflow templates (приоритет 4) — реализовано

`internal/workflow/templates.go` (337 строк) — каталог из **шести** CRM-сценариев,
каждый называет реальный триггер и шаги, которые движок уже умеет:

| ID | Сценарий |
|---|---|
| `deal-overdue-notify-owner` | уведомление владельцу о просроченной сделке |
| `deal-stage-change-create-task` | задача при смене стадии сделки |
| `deal-no-activity-reminder` | напоминание об отсутствии активности |
| `ticket-created-acknowledge` | подтверждение нового обращения |
| `deal-won-onboarding-task` | задача онбординга при выигрыше сделки |
| `ticket-priority-triage` | триаж приоритета обращения |

Конструирование падает, если событие шаблона отсутствует в `outbox.EventTypes()`,
поэтому шаблон нельзя создать, включить и никогда не сработать. Создание из
шаблона — `POST /api/v1/workflow-templates/{id}/create`; определение строится и
валидируется на сервере, клиент его не присылает. Импорт/экспорт JSON, версии,
валидация — `export.go`, `definition.go`. Тесты: `TestEveryCatalogTemplateCanBeCreatedAndRun`,
`TestTemplateCreateFromAPI`, `TestExportImportRoundTripsThroughTheStore`.

Новые шаги движка, которые понадобились шаблонам, — `find_deals` и `foreach`
(с привязанными фильтрами дат). **У каждого нового типа шага есть запись в карте
прав**, иначе workflow стал бы способом превысить права автора.

### 4.7 Визуальный редактор workflow (приоритет 5) — реализовано

`frontend/src/components/WorkflowEditor.tsx` читает и пишет **тот же** JSON-документ,
который парсит движок (сырой вид — тот же документ в сериализации), поэтому второго
формата определения нет. `WorkflowsPage` содержит выбор шаблона, диалоги
импорта/экспорта и историю запусков. 11 тестов в `WorkflowEditor.test.tsx`.

---

## 5. Что реализовано (сводка по приоритетам)

| Приоритет | Статус | Ключевые файлы |
|---|---|---|
| 1. Contextual AI | **Реализовано** | `internal/ai/assist.go`, `confidence.go`, `prompt.go`, `streaming.go`, `api/ai_assist_handlers.go`, `api/ai_stream_handlers.go`, `frontend/src/components/AssistPanel.tsx` |
| 2. Retention / rotation | **Реализовано** | `internal/retention/retention.go`, `cmd/forgecrm-worker/main.go` (maintenance loop), `config.RetentionConfig` |
| 3. S3-совместимое хранилище | **Реализовано** | `internal/storage/{storage.go,reconcile.go}`, `internal/objectstore/{s3.go,memory.go}`, `frontend/src/components/AttachmentPanel.tsx` |
| 4. Workflow templates | **Реализовано** | `internal/workflow/templates.go`, `export.go`, `api/workflow_handlers.go` |
| 5. Визуальный редактор workflow | **Реализовано** | `frontend/src/components/WorkflowEditor.tsx`, `pages/WorkflowsPage.tsx` |
| 8. MySQL/MariaDB adapter | **Отложено, блокеры измерены** | `docs/adr/0008-mysql-mariadb-datastore-blockers.md` |

---

## 6. Что осталось незавершённым и почему

1. **MySQL/MariaDB adapter (приоритет 8) — отложен осознанно.** ADR 0008 фиксирует
   измеренные блокеры, а не намерение: 19 нетестовых файлов импортируют `jackc/pgx`,
   64 `RETURNING`, 21 `ON CONFLICT`, 8 `SKIP LOCKED`, 26 `ILIKE`, 16 PostgreSQL-миграций
   с `gen_random_uuid`, `plpgsql`-триггерами, частичными индексами, `jsonb` и массивами.
   `internal/database` оборачивает `*pgxpool.Pool`, а `Tx` — это `pgx.Tx`; `jobs`,
   `events`, `importx`, `authn`, `crm` проверяют `pgx.ErrNoRows`. Поэтому адаптер
   потребовал бы переписать *места вызова*, а не только обёртку. Компиляция под
   MySQL-драйвер здесь **не** считается поддержкой. Это отдельный большой этап.

2. **Потоковая выдача AI (streaming) — реализована (цикл 14).** Все требования
   приоритета 1 выполнены: контекст, права, маскирование, лимиты, кэш, аудит,
   состояния загрузки/ошибки/повтора и потоковая выдача по SSE. Остаются
   осознанно незакрытые улучшения — не дефекты.

3. **OIDC Authorization Code Flow — не реализован** (finding A в `docs/PROJECT_AUDIT.md`).
   `capabilities.oidc_configured` теперь честно сообщает только о наличии настроек.

4. **Распределённый rate limiting** — общий `ratelimit.Limiter`; для нескольких
   реплик он опирается на общее хранилище. Проверяется в multireplica-композе.

5. **Полный трейсинг (trace propagation)** — сейчас `request_id`/`correlation_id`
   прокидываются, но без распространения трассировки (PARTIAL в roadmap).

Прочие PARTIAL-пункты roadmap (8 инструментов MCP, генератор клиентской
конфигурации с copy-to-clipboard) — это осознанно незакрытые улучшения, не дефекты.

---

## 7. Архитектура и безопасность — проверено

- **Tenant isolation:** все tenant-запросы фильтруют `organization_id`; проверено
  интеграционными тестами (cross-org отказы для счетов, тегов, кастомных полей,
  bulk-archive, экспорта, AI-assist).
- **RBAC и object-level authorization:** 93 права; право проверяется на сервере;
  движок workflow проверяет право на каждый тип шага по свежему principal автора.
- **Транзакции и индексы:** `database.WithTx` откатывает на любой ошибке; retention
  имеет отдельный тест на наличие индексов.
- **Конкурентный доступ:** интеграционные тесты (`TestLockAppliesAcrossReplicas`,
  `TestConcurrentFailuresAcrossReplicasAreAllCounted`).
- **Повторные запросы / идемпотентность:** retention идемпотентен; дедупликация
  event-triggered workflow — первичным ключом `(workflow_id, event_id)` в одной
  транзакции с созданием запуска.
- **Таймауты и graceful shutdown:** у worker есть graceful-остановка; у AI и storage
  отдельные таймауты.
- **Секреты в логах:** ключи конфигурации с признаком credential отклоняются при
  записи настроек (тест `TestASecretLookingSettingKeyIsRefused`).
- **Миграции:** вперёд/назад, встроены в бинарник; повторный `-migrate` — no-op.
- **Совместимость API и frontend:** спецификация проверяется на актуальность
  (`openapi-gen -check`); фронтовые типы ответов выверены по реальному JSON.
- **Безопасность фоновых задач:** worker не наследует пользовательский контекст,
  retention-политики по умолчанию выключены.

---

## 8. Как запустить и настроить новые функции

### Contextual AI

```bash
FORGECRM_AI_ENABLED=true
FORGECRM_AI_PROVIDER=openai_compatible     # или ollama, openai
FORGECRM_AI_BASE_URL=https://your-endpoint/v1
FORGECRM_AI_MODEL=your-model
FORGECRM_AI_API_KEY=...                     # секрет, не логируется
FORGECRM_AI_TIMEOUT=60s
FORGECRM_AI_MAX_REQUESTS_PER_MIN=60
```

Панель `AssistPanel` появляется на карточках account/contact/deal/ticket/task.
Без провайдера она сообщает, что ассистент недоступен (503 `disabled`).

### Retention

```bash
FORGECRM_RETENTION_ENABLED=true             # по умолчанию false
FORGECRM_RETENTION_DRY_RUN=true             # по умолчанию true
FORGECRM_RETENTION_WINDOWS='audit_log=8760h,webhook_deliveries=720h'   # пример
FORGECRM_RETENTION_ALLOW_AUDIT=false        # защита аудита по умолчанию
```

Свип выполняет worker; метрики — в `/api/v1/system/metrics`.

### S3-совместимое хранилище

```bash
FORGECRM_STORAGE_ENABLED=true
FORGECRM_STORAGE_ENDPOINT=https://s3.example.com
FORGECRM_STORAGE_BUCKET=forgecrm
FORGECRM_STORAGE_REGION=us-east-1
FORGECRM_STORAGE_ACCESS_KEY_ID=...
FORGECRM_STORAGE_SECRET_ACCESS_KEY=...
FORGECRM_STORAGE_USE_PATH_STYLE=true
FORGECRM_STORAGE_PRESIGN_TTL=15m
FORGECRM_STORAGE_MAX_OBJECT_BYTES=...
FORGECRM_STORAGE_TIMEOUT=30s
# FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT=true   # только для локального теста (SSRF-guard)
```

### Workflow templates и редактор

Каталог: `GET /api/v1/workflow-templates`. Создание: `POST
/api/v1/workflow-templates/{id}/create`. Импорт/экспорт JSON и редактор — на
странице `/workflows`. Спецификация API: `docs/api/openapi.json`.

---

## 9. Итог

Пять направлений приоритетов 1–5 реализованы и проверены фактическими прогонами.
Приоритет 8 (MySQL/MariaDB) отложен с измеренным обоснованием (ADR 0008), а не
заявлен как «поддержано». В этой сессии исправлены три реальных дефекта:
недоступность экспорта из интерфейса, неверное число тест-файлов в документации и
нечестное описание потоковых ответов в спецификации. Backend (unit + интеграция),
frontend (unit + typecheck + lint + build) и E2E проходят на реальном стеке.
