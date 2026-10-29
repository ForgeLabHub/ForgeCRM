# Компоненты

Каждый компонент: ответственность, чем владеет, от чего зависит. Владелец данных
означает «единственный, кто пишет в эти таблицы».

## Точки входа

| Компонент | Путь | Ответственность |
| --- | --- | --- |
| API | `backend/cmd/forgecrm-api` | HTTP-сервер, миграции по флагу `-migrate`, bootstrap администратора |
| Worker | `backend/cmd/forgecrm-worker` | доставка вебхуков, workflow, retention, фоновые задания, импорт |
| OpenAPI-генератор | `backend/cmd/openapi-gen` | генерация `docs/api/openapi.json` из живого роутера; `-check` для CI |

## Домен и прикладные сервисы (`backend/internal`)

| Пакет | Роль | Владеет данными |
| --- | --- | --- |
| `crm` | ядро: контакты, компании, сделки, задачи, тикеты, поиск, деньги | `contacts`, `accounts`, `deals`, `tasks`, `tickets`, `pipelines`, `custom_field_definitions` |
| `analytics` | чистые расчёты: деньги, доли, сценарии | — (без БД) |
| `simulate` | Simulation Lab: сценарии «что если» | `simulation_scenarios`, `simulation_runs` |
| `importx` | импорт: определение формата, парсеры, выполнение | `import_jobs`, `import_templates`, `import_row_errors` |
| `importx/safe` | безопасная распаковка недоверенных архивов | — |
| `workflow` | движок автоматизации: триггеры, шаги, выражения | `workflows`, `workflow_runs`, `workflow_step_runs` |
| `agent` | human-in-the-loop исполнение действий AI | `agent_runs`, `agent_actions`, `agent_approvals` |
| `ai` | абстракция провайдера модели, MCP-транспорт, промпты | `ai_providers`, `ai_tasks`, `ai_insights` |
| `events` | приём событий и доставка вебхуков | `events_inbox`, `webhook_subscriptions`, `webhook_deliveries` |
| `outbox` | транзакционный outbox | `outbox_events` |
| `jobs` | очередь фоновых заданий | `jobs` |
| `retention` | окна хранения и ротация | — (удаляет по политике) |
| `integrations` | жизненный цикл адаптеров, SSRF-защита исходящих запросов | `integrations`, `integration_runs` |
| `notify` | email (SMTP) и Telegram | `email_log`, `telegram_links` |
| `authn` | пароли, сессии, TOTP, OIDC | `sessions`, `mfa_factors`, `recovery_codes`, `login_attempts` |
| `rbac` | каталог прав и их проверка | `permissions`, `roles`, `role_permissions`, `user_roles` |
| `audit` | аудит и события безопасности | `audit_log`, `security_events` |
| `database` | пул соединений, транзакции, tenant-контекст | — |
| `migrate` / `migratex` | встроенные миграции и перенос данных | `migration_jobs`, `migration_reconciliations` |
| `config` | конфигурация из окружения, валидация | — |
| `httpx` | общие HTTP-примитивы: списки, rate limit, trusted proxy | — |
| `storage` | объектное хранилище (S3-совместимое) | `documents` |
| `secrets` | шифрование секретов интеграций | `app_settings` |
| `logging`, `telemetry` | структурные логи, метрики | `app_logs` |
| `wiring` | композиция зависимостей (единственное место сборки графа) | — |
| `archtest` | проверка правил зависимостей | — |
| `testsupport` | тестовая обвязка (никогда не входит в бинарники) | — |

## Frontend (`frontend/src`)

| Слой | Путь | Ответственность |
| --- | --- | --- |
| API-клиенты | `src/api/*` | по одному модулю на область backend, общий `client.ts` |
| Компоненты | `src/components/*` | оболочка, списки, гейт прав, примитивы |
| Страницы | `src/pages/*`, `src/pages/admin/*` | по файлу на маршрут |
| Типы | `src/types/api.ts` | отражение контрактов API |

## Инфраструктура

| Компонент | Путь | Ответственность |
| --- | --- | --- |
| Compose | `docker-compose.yml` | postgres, redis, api, worker, nginx |
| Multi-replica | `docker-compose.multireplica.yml`, `deploy/nginx-multireplica.conf` | несколько реплик API |
| Миграции | `backend/internal/migrations` | 19 пар `NNNN_name.{up,down}.sql`, встроены в бинарник |
| Скрипты | `scripts/` | doctor, setup, migrate, backup, restore, smoke-test, e2e, docs-check |
| CI | `.github/workflows/ci.yml`, `portability.yml` | матрица проверок |
