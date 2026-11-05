# FUNCTION_INVENTORY — ForgeCRM

> Phase 1, Full System Inventory. Baseline HEAD `cc65d3fe63a8b7fb5bb0730467ef0574445ef32f` (`main`).
> Источник истины — исходный код (`backend/internal/**`, `frontend/src/**`), а не README.
> Числа согласованы с `docs/verification/project-facts.md` (генератор `cmd/facts-gen`).

## 0. Сводка по проекту (измерено)

Числа ниже измерены на **замороженном baseline** `cc65d3f` (`main`) — это точка
отсчёта Phase 1, относительно которой сверяются все дальнейшие изменения.

| Метрика | Значение (baseline `cc65d3f`) |
|---|---|
| Go-пакеты | 53 (48 в `internal`) |
| Go-файлы (без тестов) | 151 |
| Go-тестовые файлы | 143 |
| Go-тестовые функции | 890 |
| Бенчмарки / Fuzz | 3 / 0 |
| Миграции up / down | 33 / 33 |
| Таблиц БД | 80 |
| RBAC-права (каталог = сид) | 103 |
| Системные роли | 14 |
| OpenAPI operations | 236 (182 paths) |
| Frontend: файлов src / тестовых / it() | 125 / 40 / 261 |
| Frontend: страниц / тестов страниц / маршрутов | 38 / 20 / 46 |
| E2E: spec-файлов / тестов | 4 / 67 |
| Integration-файлы (`//go:build integration`) | 62 |

> **Дельта к текущему `main`.** После baseline Phase 2 добавила test-only пакеты
> `internal/testdb` и `internal/testfixture` (уже в `main`). Текущие счётчики на
> `HEAD`: integration-файлы **70** (на baseline `cc65d3f` — 62). Эти пакеты не
> линкуются в бинарь и не меняют production-поверхность; дельта показана, чтобы
> baseline и текущее состояние не смешивались.

## 1. Архитектура в двух словах

Модульный монолит на Go (chi), PostgreSQL 17, Redis (rate limit), опциональные
адаптеры (S3/object store, SMTP, Telegram, OIDC, AI-провайдеры). Две роли одного
образа: `forgecrm-api` (HTTP) и `forgecrm-worker` (фоновые задания). Всё, что
пишет данные и порождает событие, делает это в одной транзакции через **outbox**.
Права — данные (RBAC), проверяются на сервере; tenant scope — по `organization_id`.

## 2. Реестр функций по модулям

Легенда статуса тестов:
`E` — есть unit-тест (не требует БД) · `I` — есть integration-тест (`//go:build integration`) ·
`E2E` — покрыто Playwright · `—` — тестов не найдено.
**Risk** — Critical / High / Medium / Low по влиянию на данные и деньги.

### 2.1 Authentication & Identity

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Login (password) | `POST /auth/login` | `api/auth_handlers.go`, `authn` | users, sessions, login_attempts | — | no | E,I | — | Critical |
| Logout | `POST /auth/logout` | auth_handlers | sessions | — | no | E | — | Medium |
| Session list / revoke | `GET /auth/sessions`, `DELETE /auth/sessions/{id}` | auth_handlers | sessions | — | no | E,I | — | High |
| MFA enroll/confirm/disable | `/auth/mfa/*` | auth_handlers, `authn` | mfa_factors, recovery_codes | — | no | E | — | High |
| MFA verify (login step) | `POST /auth/mfa/verify` | auth_handlers | mfa_factors | — | no | E | — | Critical |
| Password change / reset | `/auth/password/*` | auth_handlers | users, password_reset_tokens | — | no | E | — | High |
| Account lockout | (login) | `authn/lockout_integration_test.go` | login_attempts | — | no | I | — | High |
| OIDC SSO start/callback | `/auth/oidc/*` | `api/oidc_handlers.go`, `oidc` | oidc_providers, oidc_login_states, user_identities | — | no | E,I | — | High |
| Step-up auth | `POST /auth/step-up` | `api/step_up_handlers.go` | sessions | — | no | E | — | High |
| CSRF | (middleware) | `api/server.go` | — | — | no | E | — | Critical |

### 2.2 Organizations, Users, RBAC

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Organizations CRUD | `/organizations*` | `api/admin_handlers.go` | organizations | organizations.read/manage | no | I | — | High |
| Users CRUD | `/users*` | admin_handlers | users, memberships, user_roles | users.read/manage | no | I | — | High |
| Roles & permissions | `/roles*`, `/permissions` | admin_handlers, `rbac` | roles, role_permissions, permissions | roles.read/manage | no | E,I | — | Critical |
| Teams | `/teams*` | admin_handlers | teams, team_members | teams.manage | no | E | — | Medium |
| Tenant isolation | (все домены) | `crm`, `rbac` | все | — | no | I | — | Critical |
| Platform-owner bootstrap | CLI `-bootstrap-admin` | `bootstrap` | users, memberships | — | no | E | — | High |

### 2.3 CRM Domain

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Accounts CRUD+archive | `/accounts*` | `api/crm_handlers.go`, `crm` | accounts | accounts.* | no | E,I | — | High |
| Contacts CRUD+archive | `/contacts*` | crm_handlers | contacts | contacts.* | no | E,I | — | High |
| Leads CRUD + convert | `/leads*`, `/leads/{id}/convert` | crm_handlers | leads, accounts, contacts | leads.*, leads.convert | no | E | Конкурентная конвертация | High |
| Deals CRUD + stage | `/deals*`, `/deals/{id}/stage` | crm_handlers | deals, deal_stage_history | deals.*, deals.change_stage | no | E | Lost-update при параллельном edit | Critical |
| Pipelines & stages | `/pipelines*` | crm_handlers | pipelines, pipeline_stages | pipelines.* | no | E | — | Medium |
| Tasks CRUD + assign | `/tasks*` | crm_handlers | tasks | tasks.* | no | E,I | — | Medium |
| Tickets CRUD + close | `/tickets*` | crm_handlers | tickets | tickets.* | no | E | — | Medium |
| Activities | `/activities*` | crm_handlers | activities | activities.read/create | no | E | — | Low |
| Comments | `/comments*` | crm_handlers | comments | comments.create | no | E | — | Low |
| Tags + attach | `/tags*`, `/tags/{id}/attach` | `api/view_handlers.go` | tags, taggables | tags.manage | no | E | — | Low |
| Custom fields | `/custom-fields*` | admin_handlers | custom_field_definitions | custom_fields.manage | no | E | — | Medium |
| Saved views | `/views*` | `api/view_handlers.go`, `views` | saved_views | views.* | no | E,I | — | Low |
| Customer 360 (read model) | `/accounts/{id}/...` | `crm` | many | *.read | no | E | N+1 под нагрузкой | High |
| Global search | `/search` | `crm` | many (trgm) | *.read | no | E,I | — | Medium |

### 2.4 Sales & Reporting

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Sales summary | `/reports/sales-summary` | `analytics` | deals | reports.read | no | E | — | Medium |
| Support summary | `/reports/support-summary` | `analytics` | tickets | reports.read | no | E | — | Medium |
| Pipeline report | `/pipelines/{id}/report` | analytics | deals, pipeline_stages | reports.read | no | E | — | Medium |
| Forecast | `/forecast*` | `forecast`, analytics | deals, forecast_runs | forecast.read/run | no | E | — | Medium |
| Risk scan | `/risk*` | `risk` | risk_signals, risk_thresholds | risk.read/manage | no | E | — | Medium |
| Reports export | `/reports/export` | `api/export_handlers.go` | export_jobs | reports.export | yes | E | — | Medium |

### 2.5 Import Center & Export

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Import create/upload | `/imports*` | `api/import_handlers.go`, `importx` | import_jobs | imports.create | yes | E,I | — | High |
| Import dry-run | (job) | `importx`, worker | import_jobs | imports.execute | yes | E | E2E в браузере | High |
| Import execute | (job `import.execute`) | `importx`, worker | import_jobs, import_row_errors | imports.execute | yes | E | Полный E2E + crash recovery | Critical |
| Import approve | `/imports/{id}/approve` | import_handlers | import_jobs | imports.approve | no | E | — | High |
| Parsers (CSV/TSV/JSON/JSONL/XLSX/XML/zip) | — | `importx/parsers` | — | — | yes | E | Fuzz | High |
| Export create/execute | `/exports*` | `api/export_handlers.go`, worker | export_jobs | exports.execute/read | yes | E | E2E + retention | High |
| Temp-file sweep / reclaim | (periodic jobs) | worker | import_jobs | — | yes | I | — | Medium |

### 2.6 Jobs, Outbox, Workflows

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Job queue (enqueue/claim/complete/fail) | `/jobs*` | `jobs` | jobs | jobs.read/manage | yes | E,I | Нагрузка очереди | Critical |
| Worker heartbeat | `/health`, metrics | `jobs/heartbeat.go` | worker_heartbeats | — | yes | E | — | High |
| Outbox drain | (job `outbox.drain`) | `outbox`, worker | outbox_events | — | yes | E | E2E crash recovery | Critical |
| Webhook subscribe / deliver | `/webhooks*` | `api/event_handlers.go`, worker | webhook_subscriptions, webhook_deliveries | webhooks.manage | yes | E,I | Mock E2E доставки | High |
| Webhook replay / dead-letter | `/webhooks/{id}/deliveries/*` | event_handlers | webhook_deliveries | webhooks.manage | yes | E,I | — | Medium |
| Workflows CRUD + run | `/workflows*` | `api/workflow_handlers.go`, `workflow` | workflows, workflow_runs, workflow_step_runs | workflows.* | yes | E,I | — | High |
| Scheduled workflows | (cron trigger) | `workflow` | workflows | — | yes | E | — | High |
| Retention sweep | (job `maintenance.retention`) | `retention` | many | — | yes | E,I | Safety (не тронуть live) | High |
| Migrations (external data) | `/migrations*` | `api/migration_handlers.go`, `migratex` | migration_jobs, migration_reconciliations | migrations.* | yes | E,I | — | High |

### 2.7 Integrations & Notifications

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Integrations CRUD | `/integrations*` | `api/integration_handlers.go`, `integrations` | integrations, integration_runs | integrations.* | yes | E | — | High |
| Telegram webhook (inbound) | `POST /telegram/webhook` | `api/telegram_webhook.go` | telegram_update_receipts, telegram_links | — (secret) | no | E,I | — | High |
| Telegram notify (outbound) | (job `telegram.task_notification`) | worker, `notify` | telegram_task_delivery_receipts | — | yes | I | — | Medium |
| Email send | (job `email.send`) | worker, `notify` | email_log | email.manage | yes | I | Mock SMTP E2E | Medium |
| Consent enforcement | `/consent*` | `api/consent_handlers.go`, `consent` | consent_records | consent.* | no | E,I | — | High |
| Notifications | `/notifications*` | `api/notification_handlers.go` | notifications | notifications.read | no | E | — | Low |

### 2.8 AI, Agent, MCP

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| AI providers config | `/ai/providers*` | `api/ai_handlers.go`, `ai` | ai_providers | ai.configure | no | E | — | High |
| AI insights | `/ai/insights*` | ai_handlers | ai_insights | ai.read | no | E | — | Medium |
| AI assist (sync + stream) | `/ai/assist*` | `api/ai_assist_handlers.go`, `ai_stream_handlers.go` | ai_tasks | ai.read | no | E,I | — | High |
| Agent runs & approvals | `/agent*`, `/approvals*` | `api/agent_handlers.go`, `agent` | agent_runs, agent_actions, agent_approvals | agent.*, approvals.* | yes | E,I | — | Critical |
| MCP protocol (JSON-RPC) | `POST /mcp` | `api/mcp_*`, `mcpcred` | mcp_credentials | (credential) | no | E,I | — | Critical |
| Intelligence (assemble) | `/intelligence*` | `api/intelligence_handlers.go`, `crm` | many | *.read | no | E,I | — | High |

### 2.9 Security Center, Audit, Admin

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Audit log + verify (chain) | `/audit*` | `api/admin_handlers.go`, `audit` | audit_log, security_events | audit.read | no | E,I | — | Critical |
| Security events | `/security*` | admin_handlers | security_events | security.read | no | E | — | High |
| API keys / MCP credentials | `/mcp-credentials*` | `api/mcp_credential_handlers.go`, `mcpcred` | mcp_credentials | security.configure | no | E,I | — | High |
| Tenant settings | `/tenant-settings*` | `api/tenantconfig_handlers.go`, `tenantconfig` | tenant_settings, tenant_setting_revisions | system.settings | no | E,I | — | High |
| System settings / diagnostics | `/system*` | `api/system_handlers.go` | app_settings | system.settings | no | E | — | High |
| Metrics (`/metrics`) | `GET /metrics` | `metrics`, server | — | token | no | E | — | Medium |
| Health (`/healthz`,`/ready`) | GET | server | — | — | no | E | Worker readiness | Critical |

### 2.10 Documents, Storage, Simulation

| Feature | API | Backend | DB | Perm | Async | Tests | Missing | Risk |
|---|---|---|---|---|---|---|---|---|
| Documents CRUD + upload | `/documents*` | `api/document_handlers.go`, `storage` | documents | documents.* | no | E | Malware/path traversal | High |
| Object store (Memory/S3) | — | `objectstore` | — | — | no | E | — | High |
| Storage reconcile | (job) | worker | documents | — | yes | I | — | Medium |
| Simulation lab | `/simulation*` | `api/simulation_handlers.go`, `simulate` | simulation_runs, simulation_scenarios | simulation.* | no | E,I | — | Low |

## 3. Ключевые пробелы покрытия (baseline)

1. **Конкурентность (race / lost update).** Есть отдельные тесты (`internal/testsupport/concurrency_test.go`,
   `TestAgentConcurrentExecutionIsExactlyOnce`), но **нет** систематических сценариев
   «два пользователя правят одну сделку/аккаунт» и «двойной клик по stage-change».
2. **Import/Export/Webhook/Outbox E2E.** Есть unit и часть integration, но нет полного
   прогона через реальный worker с crash-recovery.
3. **Worker runtime.** Нет CI-гейта, доказывающего, что `forgecrm-worker` реально
   стартует и выполняет job в контейнере деплоя (исторический сбой: worker не запускался).
4. **Load / stress / spike / soak.** Полностью отсутствуют. Есть только
   `internal/api/bench_test.go` (Go-бенч, требует `FORGECRM_TEST_DATABASE_URL`).
5. **Fault injection.** Частично (AI provider outage, webhook sink, telegram), но нет
   сценариев «PostgreSQL недоступен», «Redis недоступен», «pool исчерпан».
6. **Tenant isolation.** Есть сильные тесты (`tenant_isolation_test.go`, `isolation_cases_test.go`),
   их надо расширить на новые модули (agent, MCP, documents).
7. **Наблюдаемость.** `/metrics` отдаёт 503 без токена (это правильно), но нет метрик
   состояния worker/очереди, по которым можно обнаружить отказ worker.

## 4. Самые рискованные компоненты (для приоритизации)

| # | Компонент | Почему | Риск |
|---|---|---|---|
| 1 | `forgecrm-worker` + `jobs` + `outbox` | Исторический дефект: worker не запускался в деплое; без него импорт/экспорт/webhook/уведомления не работают | Critical |
| 2 | Deals / pipeline (деньги) | Lost update при параллельном редактировании → неверные суммы/прогноз | Critical |
| 3 | Import Center | Массовая запись в БД, дубликаты, частичные результаты, crash recovery | Critical |
| 4 | Audit chain | Целостность журнала; подделка/разрыв = комплаенс-риск | Critical |
| 5 | RBAC + tenant isolation | Утечка данных между организациями | Critical |
| 6 | MCP / Agent / AI tool-calling | Обход RBAC через не-REST путь | Critical |
| 7 | Auth / sessions / MFA | Компрометация аккаунта | Critical |
| 8 | Health/metrics | Отказ worker остаётся невидимым | High |

## 5. Рекомендуемая очередность дальнейшей работы

1. **Phase 2** — тестовая архитектура: изолированные БД на пакет (устранить
   shared-schema дефект, из-за которого `-p 1` обязателен), фикстуры tenant/user,
   фабрики данных, mock HTTP/SMTP/Telegram, harness очереди и worker.
2. **Phase 8 (поднято в приоритет)** — расследование и восстановление worker
   (это же и главный блокер: без него фоновые функции недоказуемы).
3. **Phase 3** — функциональное покрытие критических доменов (deals, leads convert,
   import/export, audit).
4. **Phase 5 + 6** — безопасность и конкурентность (tenant isolation, race, idempotency).
5. **Phase 7** — нагрузка (load/stress/spike/soak) в отдельном окружении.
6. **Phase 4** — негативные тесты.
7. **Phase 9** — Supabase assessment (архитектурная оценка, без подключения).
8. **Phase 10** — производительность БД на больших наборах.
9. **Phase 11–13** — CI Quality Gate, дашборд, финальный отчёт.

## 6. Примечание о среде

Локально установлены: Go 1.26.10 (`/workspace/tools/go`), Docker (daemon поднят),
тестовые `postgres:16-alpine` (`127.0.0.1:55432`) и `redis:7-alpine` (`127.0.0.1:56379`),
Node 24 + зависимости frontend. Полный integration-прогон (`go test -tags=integration
./...`) идёт **параллельно, без `-p 1`**: пакет `internal/testdb` заводит каждому
тест-бинарнику приватную базу `forgecrm_test_<пакет>_<pid>` из
`FORGECRM_TEST_DATABASE_URL`. Прежний запрет на параллельность снят (был следствием
общей схемы `public`). Проверено: `ok 44 / FAIL 0`.

## 7. Как проверялись числа (Phase 1 grounding)

Реестр — не пересказ README, а результат прямого измерения на замороженном
baseline `cc65d3f`. Каждое число воспроизводимо командой; расхождение с
`docs/verification/project-facts.md` означало бы, что код изменился, а документ — нет.

| Метрика | Команда / источник | Результат |
|---|---|---|
| Go-пакеты (весь backend) | `git ls-files 'backend/**/*.go'` → уникальные каталоги | 53 |
| Go-пакеты (`internal`) | то же, `internal/**` | 48 |
| Go-файлы без тестов | `git ls-files ... \| grep -v '_test.go$'` | 151 |
| Go-тестовые файлы | `git ls-files ... \| grep '_test.go$'` | 143 |
| Тестовые функции Go | `git grep -hoE '^func Test[A-Za-z0-9_]+' HEAD -- '*_test.go'` | 890 |
| Integration-файлы | `git grep -l '^//go:build integration' HEAD -- '*.go'` | 62 (baseline `cc65d3f`) / **70** (текущий `main`) |
| Миграции up/down | `ls internal/migrations/*.up.sql` / `*.down.sql` | 33 / 33 |
| Таблиц БД | уникальные `CREATE TABLE` во всех up-миграциях | 80 |
| RBAC-права | `len(rbac.All())` (совпадает с каталогом и сидом) | 103 |
| Системные роли | кортежи `INSERT INTO roles … true` в `0004_rbac_seed` | 14 |
| OpenAPI paths / ops | `paths` и методы в `docs/api/openapi.json` | 182 / 236 |
| Frontend src / тестовые | `find frontend/src -name '*.ts*'` / `*.test.ts*` | 125 / 40 |
| Frontend it()/test() | `grep -rhoE '\bit(\.\w+)?\(' src --include='*.test.ts*'` | 259 |
| Frontend страниц / тестов | `frontend/src/pages` (без `admin/`) | 38 / 20 |
| Маршрутов `<Route path>` | объявления в `frontend/src/App.tsx` | 46 |
| E2E spec / тестов | `frontend/e2e/*.spec.ts` / `test(...)` | 4 / 67 |

**Оговорки (важно для честности реестра).**

- `go list ./...` без тегов не видит `internal/testsupport`: он состоит только из
  integration-файлов (тег `//go:build integration`), поэтому возвращает 52, а не 53.
  Facts-gen считает каталоги с `.go` независимо от тега и даёт 53. Обе цифры верны,
  определение разное. (Phase 2 добавляет `internal/testdb` и `internal/testfixture` —
  у них есть файлы без тега, поэтому в рабочем дереве `go list` их видит.)
- Число `it()`/`test()` в `src` равно 259 при простом подсчёте и 261 по определению
  facts-gen (оно учитывает обёртки и вычитает `test.describe`). Реестр использует
  канонические 261 из `project-facts.md`.
- E2E-тестов 62 при подсчёте `test(` и 67 по facts-gen (учитывает параметризацию).
- «26 файлов» в старом тексте `docs/development/testing.md` было ошибочным; на
  baseline Phase 1 (`cc65d3f`) число файлов с тегом — 62, документ исправлен.
  К текущему `main` набор вырос до **70** (Phase 2 добавила `testdb`/`testfixture`,
  Phase 1 — новые integration-тесты); см. §0.

**Проверенные утверждения о высокорисковых компонентах.**

- `forgecrm-worker` собирается (`go build ./cmd/forgecrm-worker` → OK) и выбирается
  двумя сигналами (`FORGECRM_ROLE=worker` и `command: ["worker"]`), что охраняет
  `TestComposeWorkerSelectsTheWorkerBinary` в `internal/deploycheck`. Диспетчер —
  `deploy/entrypoint.sh`. Реальное исполнение job в изолированном окружении — задача
  Phase 8, а не Phase 1.
- Cross-cutting защита в `internal/api/server.go` подтверждена: rate limit на
  `/api/v1` (300/мин, login 60/мин), CSRF на изменяющих маршрутах, idempotency-middleware
  на session- и MCP-группах, `s.perm(...)` на защищённых маршрутах.
- Ограничения размера запроса подтверждены: MCP-тело 1 МиБ, import-upload 256 МиБ
  (`FORGECRM_IMPORT_MAX_UPLOAD_BYTES`), telegram-webhook 1 МиБ, documents — свой лимит.
- Конкурентные и tenant-isolation тесты существуют поимённо (см. §3); отсутствие
  теста на конвертацию лида подтверждено (`grep` по `Convert` в `*_test.go` — пусто).
