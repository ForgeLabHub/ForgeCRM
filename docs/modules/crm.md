# Модуль: CRM-ядро

## 1. Назначение и границы
Ядро продукта: контакты, компании, сделки, воронки и стадии, задачи, тикеты, лиды,
активности, комментарии, теги, пользовательские поля, SLA, поиск. Модуль содержит
сущности и правила, но **не знает**, кто его вызывает: HTTP, MCP-инструменты, worker
и AI-слой используют один и тот же контракт.

Не делает: не отправляет письма, не ходит в сеть, не знает про транспорт и формат
ответа HTTP.

## 2. Сценарии использования
- Создание/редактирование сделки, перевод по стадиям с записью истории.
- Массовая архивация и восстановление записей.
- Глобальный поиск по сущностям арендатора.
- Расчёт сводок: сумма по стадиям, конверсия, сроки.
- Отказ: попытка доступа к записи другого арендатора → «не найдено», а не «запрещено»
  (не подтверждаем существование чужой записи).

## 3. Контракт
HTTP-маршруты `/api/v1/{contacts,accounts,deals,tasks,tickets,leads,pipelines}` — в
`docs/api/openapi.json`. Публичный Go-контракт — экспортируемые типы и методы пакета
`internal/crm`. События: `deal.created`, `deal.updated`, `deal.stage_changed`,
`deal.won`, `deal.lost` через `outbox`.

## 4. Данные
Владеет: `contacts`, `accounts`, `deals`, `pipelines`, `pipeline_stages`,
`deal_stage_history`, `leads`, `tasks`, `tickets`, `comments`, `activities`,
`contracts`, `sla_policies`, `custom_field_definitions`, `tags`, `taggables`.
Деньги — целые минорные единицы; арифметика вынесена в `internal/analytics/money.go`.
Миграции: `0002_crm` и последующие расширения.

## 5. Права
Домены прав: `deals.*` (7), `contacts.*` (5), `accounts.*` (5), `tickets.*` (5),
`leads.*` (5), `tasks.*` (4), `pipelines.*` (2), `reports.*` (2).
Изоляция: каждый запрос фильтруется по `organization_id` из tenant-контекста;
значение из тела запроса не принимается.

## 6. Зависимости
Исходящие: `database`, `audit`, `outbox`, `rbac` (только идентификаторы прав).
Входящие: `api`, `agent`, `importx`, `workflow`, `ai`, `simulate`.
Ядро не импортирует прикладные сервисы и транспорт — проверяется
`TestApplicationServicesDependOnTheCoreAndNotTheReverse`,
`TestCoreDoesNotImportCompositionOrHTTP`.

## 7. Конфигурация
Собственных переменных нет: поведение задаётся пайплайнами, стадиями и SLA в данных.
Косвенно зависит от `FORGECRM_DATABASE_URL`, `FORGECRM_DATABASE_MAX_CONNS`.

## 8. Сборка и отладка
Собирается в составе бэкенда (`make build-api`). Отдельный запуск не предусмотрен:
это библиотека, а не процесс. Точечная отладка — unit-тесты пакета:

```bash
cd backend && go test ./internal/crm/ -run Deal -v
```

## 9. Тесты
Unit: `internal/crm/*_test.go`, `internal/analytics/money_test.go` (правила денег),
`internal/analytics/scenario_test.go`. Integration: `internal/api/tenant_isolation_test.go`,
`isolation_cases_test.go`, `bulk_archive_test.go`. E2E: `frontend/e2e/platform.spec.ts`
(создание и поиск записи).

## 10. Наблюдаемость и ошибки
Аудит пишется в `audit_log` на каждое изменение сущности; события — в `outbox_events`
в той же транзакции. Ошибки: `not_found`, `conflict` (нарушение уникальности),
`validation_failed`. Метрики: задержка запросов по маршрутам, количество 5xx.

## 11. Ограничения и критерии готовности
- Мультивалютность без конвертации по курсу: валюта хранится, пересчёт — нет.
- Кастомные поля не участвуют в индексах сложных отчётов.
- Готовность: миграции применяются с нуля и повторно; unit-тесты покрывают правила
  денег и переходов стадий; тесты изоляции зелёные.
