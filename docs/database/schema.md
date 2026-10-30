# Схема данных

PostgreSQL 17, схема `forgecrm`, **73 таблицы**, **25 миграций**. Полное описание
столбцов — в SQL (`backend/internal/migrations/*.up.sql`); здесь — карта владения и
связей, чтобы понять, кто за что отвечает.

## Домены и владельцы

| Домен | Таблицы | Владелец (пакет) |
| --- | --- | --- |
| Арендаторы и доступ | `organizations`, `users`, `memberships`, `teams`, `team_members`, `roles`, `permissions`, `role_permissions`, `user_roles` | `rbac`, `authn` |
| Аутентификация | `sessions`, `mfa_factors`, `recovery_codes`, `login_attempts`, `password_reset_tokens` | `authn` |
| CRM-ядро | `contacts`, `accounts`, `deals`, `pipelines`, `pipeline_stages`, `deal_stage_history`, `leads`, `tasks`, `tickets`, `comments`, `activities`, `contracts`, `sla_policies`, `custom_field_definitions`, `tags`, `taggables` | `crm` |
| Импорт и перенос | `import_jobs`, `import_templates`, `import_row_errors`, `migration_jobs`, `migration_reconciliations`, `export_jobs` | `importx`, `migratex` |
| Автоматизация | `workflows`, `workflow_runs`, `workflow_step_runs`, `workflow_run_events`, `approvals` | `workflow` |
| События и вебхуки | `events_inbox`, `outbox_events`, `webhook_subscriptions`, `webhook_deliveries` | `events`, `outbox` |
| AI и агенты | `ai_providers`, `ai_tasks`, `ai_insights`, `agent_runs`, `agent_actions`, `agent_approvals` | `ai`, `agent` |
| Аналитика и симуляция | `simulation_scenarios`, `simulation_runs` | `simulate`; расчёты — `analytics` (без БД) |
| Прогнозы и риски | `forecast_runs`, `risk_signals`, `risk_thresholds` | `analytics`; расчёты — `analytics/forecast.go`, `analytics/risk.go` |
| Интеграции | `integrations`, `integration_runs`, `documents`, `telegram_links`, `email_log` | `integrations`, `storage`, `notify` |
| Платформа | `jobs`, `audit_log`, `security_events`, `app_logs`, `app_settings`, `notifications`, `idempotency_keys` | `jobs`, `audit`, `logging`, `secrets` |

Правило: **писать в таблицы домена может только его владелец.** Чужой модуль
получает данные через контракт владельца, а не прямым `INSERT`.

## Ключевые связи

```
organizations 1─┬─* users (через memberships)
                ├─* contacts ──* deals ──* deal_stage_history
                ├─* accounts  (deals.account_id)
                ├─* tasks / tickets / activities / comments
                ├─* pipelines ──* pipeline_stages ──* deals.stage_id
                └─* workflows / webhook_subscriptions / import_jobs
users 1─* sessions, mfa_factors, user_roles
```

## Правила, которые нельзя нарушать

1. **Мультиарендность.** Практически у каждой бизнес-таблицы есть `organization_id`.
   Запрос без него — дефект безопасности, а не оптимизация.
2. **Деньги — в целых числах (минорные единицы), валюта отдельной колонкой.**
   Никаких `float`; арифметика — в `internal/analytics/money.go` с тестами.
3. **Аудит неизменяем.** `audit_log` и `security_events` только дополняются.
4. **Идемпотентность.** Повторный запрос с тем же ключом не создаёт вторую запись.
5. **Мягкое удаление там, где есть история.** `deals`/`contacts` архивируются,
   чтобы не рвать аудит и отчёты.

## Индексы

Индексы добавляются миграцией, а не вручную на проде. Отдельные миграции
`0015_retention_indexes`, `0017_retention_sweep_indexes`,
`0021_intelligence_retention_indexes` и `0024_transient_retention_indexes`
существуют потому, что retention-выборки без них деградируют на больших объёмах.
`risk_signals` не нужен отдельный индекс в 0021: миграция 0020 уже создала
`risk_signals_terminal_idx` — частичный индекс по `status_changed_at` для
терминальных статусов, ровно под этот sweep. `oidc_login_states` тоже не нужен
отдельный индекс в 0024: миграция 0022 уже создала `oidc_login_states_expires_idx`
под эту же выборку.

## Проверка

- Создание схемы с нуля: `make migrate` на пустой БД, затем повторный запуск —
  обязан быть no-op (проверяется в CI).
- Изоляция арендаторов: `go test -tags=integration ./internal/api/`.
- Резервное копирование и восстановление: [backup-restore.md](backup-restore.md).
