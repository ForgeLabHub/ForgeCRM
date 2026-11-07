# Владение данными ForgeCRM

> Описывает **фактическое** положение на `main` @ `3424b3e`: одна схема
> PostgreSQL, логическое владение по модулям. Целевое разделение для
> микросервисов — в `MICROSERVICES.md` (не реализовано).

## 1. Текущее состояние (факт)

- Одна схема PostgreSQL, **83 таблицы**, **39 пар** миграций (симметричны).
- Миграции встроены в бинарник (`//go:embed`), применяются при `-migrate` или
  `FORGECRM_MIGRATE_ON_START=true`.
- Изоляция арендаторов — через обязательный `organization_id` и фильтрацию в `crm`.
- Владение таблицами **логическое**: модуль-владелец — единственный, кто должен
  писать; другие читают через его контракт. Технически межмодульные SQL-запросы к
  «чужим» таблицам не запрещены — это TD-2.

## 2. Владение по модулям (логическое)

| Владелец | Таблицы (пример) |
|---|---|
| `crm` | accounts, contacts, leads, deals, pipelines, pipeline_stages, deal_stage_history, tasks, tickets, comments, activities, contracts, sla_policies, custom_field_definitions, tags, taggables |
| `authn` | users, sessions, mfa_secrets, password_resets, login_attempts |
| `oidc` | oidc_providers, oidc_identities, oidc_states |
| `rbac` | roles, permissions, role_permissions, memberships, teams |
| `audit` | audit_log |
| `jobs` | jobs |
| `outbox` | outbox_events |
| `idempotency` | idempotency_keys |
| `importx` | import_jobs, import_errors, import_uploads |
| `migratex` | migration_jobs, migration_reconciliation |
| `integrations` | integrations, integration_runs |
| `events` | webhook_subscriptions, webhook_deliveries, dead_letters |
| `notify` | notifications, notification_deliveries |
| `consent` | consent_records, consent_policies |
| `storage` | documents, attachments |
| `tenantconfig` | tenant_settings |
| `views` | saved_views |
| `workflow` | workflows, workflow_runs |
| `agent` | agent_proposals, agent_approvals |
| `mcpcred` | mcp_credentials |
| `retention` | retention_policies |

> Точный список таблиц по миграциям — `backend/internal/migrations/*.up.sql`;
> сводка — `docs/verification/project-facts.md` (83 таблицы).

## 3. Правила (действующие)

1. Каждая таблица имеет одного логического владельца-писателя.
2. Кросс-доменные чтения — через публичный API владельца (Go-метод), не сырым SQL.
3. Изменение данных и запись события — одна транзакция (outbox).
4. Изоляция арендатора: каждый запрос фильтруется по `organization_id`.

## 4. Диаграмма владения (Mermaid)

```mermaid
graph TD
  subgraph PG[(PostgreSQL — одна схема)]
    T1[crm.*]
    T2[identity: users/sessions/oidc]
    T3[rbac.*]
    T4[platform: jobs/outbox/audit/idempotency]
    T5[import/migration]
    T6[integrations/events]
    T7[notify/consent]
    T8[storage/documents]
  end
  CRM[crm] --- T1
  ID[authn/oidc/rbac] --- T2
  ID --- T3
  PLAT[jobs/outbox/audit/idempotency] --- T4
  IMP[importx/migratex] --- T5
  INT[integrations/events] --- T6
  NT[notify/consent] --- T7
  ST[storage] --- T8
```

## 5. Целевое (для микросервисов, не реализовано)

- Схема на контекст: `identity`, `crm`, `workflow`, `notifications`, `analytics`.
- Запрет межсхемных запросов, обеспеченный тестом-хранителем.
- Межсервисные данные — через API/события/read models.

## 6. Проверка

| Утверждение | Команда |
|---|---|
| Миграции симметричны | `go run ./cmd/facts-gen -repo .. -check` |
| Миграции применяются на чистой БД | `go test -tags=integration ./internal/migrate/...` |
| Изоляция арендаторов | `go test -tags=integration ./internal/api/ -run Isolation` |
