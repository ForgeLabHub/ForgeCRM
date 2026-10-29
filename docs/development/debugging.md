# Отладка

## Инструменты

| Задача | Как |
| --- | --- |
| Посмотреть конфигурацию | `make doctor`, логи старта API |
| Проверить живость | `make smoke-test SMOKE_URL=http://localhost:8080` |
| Логи сервисов | `make compose-logs` |
| Версия схемы | `make migrate-status` |
| Разобрать запрос | найти `request_id` в JSON-логах |
| Состояние фоновой обработки | SQL по `jobs`, `webhook_deliveries`, `workflow_runs` |
| Debug-сборка API | `cd backend && go run ./cmd/forgecrm-api` (с `FORGECRM_LOG_LEVEL=debug`) |

## Частые отказы

| Симптом | Причина | Что делать |
| --- | --- | --- |
| API не стартует, `FORGECRM_SECRET_KEY is required` | секрет не задан | сгенерировать ключ в `.env` (см. [setup.md](setup.md)) |
| API не стартует, ошибка подключения к БД | неверный DSN/база не поднята | `docker compose ps`, `FORGECRM_DATABASE_URL` |
| `FORGECRM_REDIS_URL is set but ... memory` | противоречивая конфигурация | выставить `FORGECRM_RATE_LIMIT_STORE=redis` или убрать URL |
| Вход не работает, cookie не приходит | `FORGECRM_COOKIE_SECURE=true` без TLS | выключить локально |
| 401 после входа | сессия не ротируется / домен cookie | проверить `FORGECRM_COOKIE_DOMAIN` |
| 403 на всех страницах | у роли нет прав | проверить `user_roles` и миграции-seed |
| Вебхуки не доставляются | worker не запущен или SSRF-блок | `jobs`, `webhook_deliveries`, лог воркера |
| Импорт падает на архиве | лимиты `FORGECRM_IMPORT_*` | увеличить лимит осознанно, проверив размер |
| Фронтенд пустой при `npm run dev` | нет прокси на API | `vite.config.ts`, var окружения API |
| `openapi-gen -check` падает | маршрут изменён без регенерации | `make openapi` и закоммитить |

## Практика

- Воспроизводите в минимальном масштабе: один запрос, одна таблица, один сервис.
- Сначала проверьте БД, потом код: большинство «багов API» — состояние данных.
- Инцидент, выявивший архитектурную проблему, фиксируется в `docs/audit/`.
