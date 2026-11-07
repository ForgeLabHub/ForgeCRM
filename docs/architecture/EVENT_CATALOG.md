# Каталог событий ForgeCRM

> Источник: `backend/internal/outbox/outbox.go`. Версия схемы события несётся в
> поле `schema_version` (`outbox.Event`). Это **реальный** список, а не план.

## Модель

Событие пишется в `outbox_events` **в той же транзакции**, что и изменение данных.
Worker забирает строки через `SKIP LOCKED`, доставляет и помечает обработанными.
При ошибке — retry с backoff, при исчерпании бюджета — dead-letter
(`internal/events`).

Поля события: `id`, `organization_id`, `event_type`, `schema_version`, `source`,
`payload`, `correlation_id`, `occurred_at`.

- **Версионирование:** `schema_version` (целое). Правило совместимости: добавлять
  поля можно, удалять/переименовывать — только с новой версией схемы.
- **Correlation ID:** `correlation_id` протягивается из запроса (`tracectx`),
  поэтому событие связано с породившим его HTTP-вызовом.
- **Идемпотентность консьюмера:** доставка at-least-once; консьюмер обязан быть
  идемпотентным (`internal/idempotency`).

## События (28, по коду)

| Тип | Область | Смысл |
|---|---|---|
| `crm.account.created` | CRM | создан аккаунт |
| `crm.account.updated` | CRM | изменён аккаунт |
| `crm.account.archived` | CRM | архивирован аккаунт |
| `crm.contact.created` | CRM | создан контакт |
| `crm.contact.updated` | CRM | изменён контакт |
| `crm.contact.archived` | CRM | архивирован контакт |
| `crm.lead.created` | CRM | создан лид |
| `crm.lead.updated` | CRM | изменён лид |
| `crm.lead.converted` | CRM | лид конвертирован |
| `crm.deal.created` | CRM | создана сделка |
| `crm.deal.updated` | CRM | изменена сделка |
| `crm.deal.stage_changed` | CRM | сделка переведена по стадии |
| `crm.deal.won` | CRM | сделка выиграна |
| `crm.deal.lost` | CRM | сделка проиграна |
| `crm.task.created` | CRM | создана задача |
| `crm.task.assigned` | CRM | задача назначена |
| `crm.task.due_changed` | CRM | изменён срок задачи |
| `crm.task.completed` | CRM | задача завершена |
| `support.ticket.created` | Support | создано обращение |
| `support.ticket.updated` | Support | изменено обращение |
| `support.ticket.closed` | Support | обращение закрыто |
| `data.import.completed` | Import | импорт завершён |
| `data.import.failed` | Import | импорт провалился |
| `data.migration.completed` | Migration | миграция завершена |
| `integration.run.completed` | Integration | прогон интеграции завершён |
| `platform.user.invited` | Platform | пользователь приглашён |
| `platform.user.blocked` | Platform | пользователь заблокирован |

> Список держится синхронно с кодом: `outbox.EventTypes()` возвращает ровно эти
> типы, и `outbox_test.go` падает при расхождении.

## Поток события (Mermaid)

```mermaid
sequenceDiagram
  participant H as handler
  participant DB as PostgreSQL
  participant W as worker
  participant C as consumer (webhook/notify)
  H->>DB: BEGIN; изменение данных
  H->>DB: INSERT outbox_events (event_type, schema_version, correlation_id)
  H->>DB: COMMIT
  W->>DB: SELECT ... FOR UPDATE SKIP LOCKED
  W->>C: доставка
  alt успех
    W->>DB: пометить обработанным
  else ошибка
    W->>DB: retry с backoff
    W->>DB: dead-letter при исчерпании бюджета
  end
```

## Использование в микросервисной ветке

Каталог — готовый контракт межсервисных событий: те же типы можно вынести в
топик брокера без изменения эмитентов. Версионирование и correlation ID уже
на месте.
