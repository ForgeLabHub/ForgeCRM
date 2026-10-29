# Права доступа и изоляция арендаторов

## Модель

- **Каталог прав** — `internal/rbac/rbac.go`: **87 прав** вида `<домен>.<действие>`
  (`deals.read`, `deals.write`, `imports.run`, `webhooks.manage`, …).
- **Системные роли** — 14: `platform_owner`, `platform_admin`, `tenant_admin`,
  `sales_manager`, `sales_representative`, `support_manager`, `support_agent`,
  `analyst`, `data_steward`, `employee`, `executive`, `integration_admin`,
  `security_auditor`, `readonly_auditor`.
- **Права назначаются ролям** (`role_permissions`), роли — пользователям
  (`user_roles`) внутри организации.
- Каталог синхронизирован с миграциями: `0004_rbac_seed`, `0006_seed_webhooks_manage`,
  `0010_seed_agent_permissions`, `0013_seed_workflow_permissions`. Расхождение ловится
  unit-тестами.

## Где проверяется

1. **На маршруте.** Каждый маршрут объявляет требуемое право; маршрут без права —
   дефект, а не «открытая ручка». Проверка: `internal/api/contract_test.go`,
   `capabilities_test.go`.
2. **В домене — только как утверждение о данных**, не как замена проверке маршрута.
3. **В UI** — `RequirePermission` прячет элементы, но это удобство, а не защита.

## Изоляция арендаторов

- Каждый запрос получает tenant-контекст до входа в handler.
- Запросы к данным всегда содержат `organization_id` из контекста; значение из тела
  запроса игнорируется.
- Платформенные администраторы работают через отдельные маршруты с явным
  намерением, а не через «выключение фильтра».
- Доказательство: `internal/api/tenant_isolation_test.go`,
  `internal/api/isolation_cases_test.go` — включая попытки доступа по прямому UUID
  чужой записи.

## Как добавить право

1. Добавить код в `rbac.go`.
2. Добавить миграцию-seed с назначением ролям (нумерованная, вперёд).
3. Объявить право на маршруте.
4. Добавить тест: «пользователь без права получает 403».
5. Обновить эту страницу и, при необходимости, `docs/api/openapi.json` (`make openapi`).

## Проверка после изменений

```bash
make test-unit          # каталог прав и согласованность
make test-integration   # изоляция арендаторов против реальной БД
```
