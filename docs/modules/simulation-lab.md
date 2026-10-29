# Модуль: Simulation Lab

## 1. Назначение и границы
Сценарии «что если»: сохранить гипотезу (изменение стадии, скидка, срок), увидеть
предпросмотр результата и историю запусков. Считает поверх реальных данных CRM.
Не делает: **не изменяет данные CRM**. Это утверждение — не пожелание, а проверяемый
контракт: `TestSimulationNeverWritesCRMRows` и `TestSimulationPreviewPersistsNothing`
(`backend/internal/testsupport/simulation_test.go`).

## 2. Сценарии использования
- Создать сценарий с параметрами и сохранить.
- Предпросмотр: расчёт без записи в базу.
- Запуск: расчёт с сохранением прогона и снимком входных данных.
- Недостаточно данных → честный отчёт «недостаточно данных», а не выдуманное число.
- Воспроизводимость: повторный прогон с тем же снимком даёт тот же результат.

## 3. Контракт
`/api/v1/simulations/*`: каталог параметров, CRUD сценария, preview, run, история.
Чистые расчёты — `internal/simulate/params.go`, `run.go`; ничего не знают о HTTP.

## 4. Данные
Владеет: `simulation_scenarios`, `simulation_runs` (миграция `0019_simulation_lab`).
Читает CRM-данные **через контракт `crm`**, только чтение. Прогон сохраняет снимок
входных данных, поэтому результат воспроизводим при изменении CRM.

## 5. Права
Права домена `simulations.*`; доступ к данным CRM дополнительно ограничен правами
чтения соответствующих сущностей. Изоляция: `TestSimulationTenantIsolation`,
проверка прав: `TestSimulationPermissionIsEnforced`.

## 6. Зависимости
Исходящие: `crm` (чтение), `analytics` (арифметика), `database`, `audit`.
Входящие: `api`, frontend `SimulationPage`.

## 7. Конфигурация
Собственных переменных нет. Ограничения параметров заданы каталогом (`Catalog()`).
Косвенно зависит от лимитов БД (`FORGECRM_DATABASE_MAX_CONNS`).

## 8. Сборка и отладка
В составе API. Проверки модуля отдельно:

```bash
cd backend && go test -tags=integration ./internal/testsupport/ -run Simulation -v
```

## 9. Тесты
`TestSimulationScenarioLifecycle` (CRUD), `TestSimulationScenarioValidation`,
`TestSimulationRunAgainstRealData`, `TestSimulationPreviewPersistsNothing`,
`TestSimulationNeverWritesCRMRows` (**главная гарантия модуля**),
`TestSimulationInsufficientDataIsReported`, `TestSimulationTenantIsolation`,
`TestSimulationPermissionIsEnforced`, `TestSimulationRunIsReproducible`,
`TestSimulationMigrationDownRemovesTheTables`. Frontend:
`frontend/src/pages/SimulationPage.test.tsx`.

## 10. Наблюдаемость и ошибки
Запуск пишется в `simulation_runs` (входной снимок, результат, время) и в аудит.
Ошибки: `insufficient_data`, `invalid_parameters`, `not_found`. Метрики: число
прогонов, длительность.

## 11. Ограничения и готовность
- Модель — детерминированные правила, не прогноз на ML.
- Нет сравнения двух сценариев бок о бок в одном запросе.
- Готовность: гарантия «не пишет в CRM» покрыта тестом; preview ничего не сохраняет;
  недостаток данных сообщается, а не маскируется.
