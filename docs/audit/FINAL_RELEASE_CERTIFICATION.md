# Финальная сертификация релиза ForgeCRM

- **Дата:** 2026-11-04
- **Проверенный SHA:** `0c748e2` (`docs(audit): Phase H final release re-verification`)
- **Ветка:** `main` (единственная)
- **Лицензия:** MIT
- **Версия:** `0.1.0-dev`

Это итоговый документ финализации: он объединяет доказательства, собранные при
публикации пяти ранее непроверенных коммитов (Phase E/G/H), прогоне remote CI на
опубликованном SHA и повторной проверке продукта. Каждое утверждение ниже
проверено командой или job'ом CI; то, что проверить не удалось, помечено
`NOT VERIFIED`, а не выдано за успех.

## A. Состояние проекта

| Параметр | Значение |
| --- | --- |
| Текущий SHA | `0c748e2` |
| Дата | 2026-11-04 |
| Архитектура | Go-монолит API + отдельный воркер + PostgreSQL 17 + Redis (опционально) + React SPA |
| Стек | Go 1.26.10, PostgreSQL 17.11, Redis 7, React 19 + Vite + TypeScript |
| Версия | `0.1.0-dev` |
| Миграции | 31 |
| API | 180 путей / 234 операции (OpenAPI) |
| RBAC | 103 разрешения |
| Страницы SPA | 38 |

**Разворачиваемые компоненты:**

| Компонент | Роль | Запуск |
| --- | --- | --- |
| `api` | HTTP API, миграции, метрики | `forgecrm-api` |
| `worker` | очередь `jobs`, outbox, вебхуки, расписания | `forgecrm-worker` |
| `postgres` | хранилище | внешняя зависимость |
| `redis` | общий лимитер при нескольких репликах | опционально |
| `nginx` | TLS и балансировка (многократная репликация) | опционально |

API и воркер — общий образ, разные бинарники; запускаются независимо.

## B. Верификация

| Область | Результат | Доказательство |
| --- | --- | --- |
| Backend unit | 39 пакетов `ok`, 0 `FAIL` | `go test ./...` |
| Backend integration | 43 пакета `ok`, 0 `FAIL` | `go test -tags=integration -p 1 ./...` против PG 17 + Redis 7 |
| `go build` / `go vet` | чисто | `go build ./...`, `go vet ./...` |
| Frontend | typecheck 0, lint 0 ошибок, **260 тестов** (39 файлов), build ok | `npm run typecheck/lint/test/build` |
| E2E | **85 passed / 1 skipped / 0 failed** | CI job «End-to-end» + локальный прогон |
| Visual regression | **8/8** | CI job «Visual regression (baselines on Linux)» |
| Security | gosec 13 принято, govulncheck 0 уязвимостей (в т.ч. в модулях), gitleaks 284 коммита без утечек | CI job «Security» + локальный govulncheck/gitleaks |
| Миграции | 31 применяются с нуля; 0031 Up/Down/Up; данные целы | изолированные БД |
| Worker liveness | heartbeat, устаревание, `workers_alive` 1→2→0, graceful shutdown | `internal/jobs/heartbeat.go`, интеграционные тесты |
| Backup/restore | dump → restore → совпадение | временные БД `bk_src`/`bk_dst` (см. §F) |
| Производительность | базовая линия воспроизведена | `docs/performance/baseline.md`, бенчмарк |
| Remote CI | **15/15 success** на `0c748e2` | см. §CI |

### CI на опубликованном SHA

| Workflow | Задание | Итог |
| --- | --- | --- |
| CI (run 41747583263) | Backend (format, vet, test, build) | success |
| CI | Frontend (lint, typecheck, test, build) | success |
| CI | End-to-end (real API, real database, real browser) | success |
| CI | Visual regression (baselines on Linux) | success |
| CI | Security (dependencies, secrets, static analysis) | success |
| CI | Docker image builds | success |
| CI | Constructor (each recipe builds a working project) | success |
| CI | Extraction (each module builds without the monolith) | success |
| CI | Blitz Cloud deployment (root image, clean database) | success |
| CI | Required documentation is present | success |
| Portability (run 41747583255) | Upgrade an existing database | success |
| Portability | Backup, restore and smoke test | success |
| Portability | Runs with no optional configuration | success |
| Portability | Documentation is present and linked | success |
| Portability | Full stack (Compose) smoke test | success |

Итого **15/15 success**. Это первый CI-прогон, покрывающий Phase E/G/H.

## C. Полнота продукта

| Функция | Реализована | Проверена | Ограничения |
| --- | --- | --- | --- |
| Аутентификация (пароль, сессии, TOTP) | да | интеграционные + E2E | — |
| RBAC (103 разрешения) | да | unit + integration | — |
| Изоляция организаций | да | `TestCrossOrganizationIsolation` | — |
| Контакты: CRUD, поиск, фильтр | да | integration + E2E | поиск на 50k деградирует (см. §F) |
| Аккаунты, лиды, сделки, задачи, тикеты | да | integration + E2E | — |
| Карточка клиента, история, связи | да | E2E | — |
| Архив и восстановление | да | integration | — |
| Import Center (CSV, XLSX, mapping, отчёт) | да | integration + E2E | очень большие книги требуют поднятия лимитов |
| Вебхуки (подпись, ретраи, dead letter) | да | `TestWebhookLifecycleOverHTTP` | — |
| Telegram / email-уведомления | да | integration (без реальных внешних отправок) | внешняя доставка `NOT VERIFIED` без credentials |
| Workflows, симуляции, прогнозы, риск | да | unit + E2E | AI-провайдер опционален |
| Админ: пользователи, роли, API-ключи | да | integration | — |
| Аудит и `security_events` | да | integration | — |
| Метрики и `workers_alive` | да | integration | — |
| Экспорт с аудитом | да | `TestExportIsAudited` | — |

Функция отмечена «реализована» только там, где путь исполняется на реальном
коде и покрыт тестом; заглушек и UI-без-бэкенда в списке нет.

## D. Безопасность

| Область | Состояние |
| --- | --- |
| Аутентификация | пароль + сессии + TOTP; сессии в БД, общие между репликами |
| Авторизация | RBAC по 103 разрешениям; проверяется на каждом маршруте |
| Изоляция организаций | каждый запрос ограничен `organization_id`; тест на кросс-тенант |
| Сессии | хэш токена, отзыв, TTL; вход лимитирован |
| CSRF | double-submit токен на мутирующих маршрутах |
| Секреты | нет хардкода; `gitleaks` 284 коммита без утечек; конфиг через env |
| Зависимости | govulncheck: 0 уязвимостей (оба объявления исправлены обновлением зависимостей) |
| Security headers | `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Permissions-Policy`; CSP в Report-Only; HSTS — при настройке |
| Аудит | `audit_log` + `security_events`, экспорт аудируется |
| Вебхуки | подпись HMAC, проверка на приёмнике, ретраи, dead letter |

## E. Эксплуатационная готовность

| Область | Состояние |
| --- | --- |
| Развёртывание | `docker-compose.yml`, `docker-compose.multireplica.yml`, `docs/deployment/` |
| Backup | `scripts/backup.sh`: custom-format dump + том импортов + sha256 |
| Restore | `scripts/restore.sh`; проверен на временных БД |
| Мониторинг | `/metrics` за токеном; 8 алертов в `docs/operations/monitoring.md` |
| SLO/SLI | **предложены** в `docs/operations/slo-and-runbooks.md` (не подтверждены данными) |
| Runbooks | 9 сценариев в `docs/operations/slo-and-runbooks.md` |
| Восстановление | шаги в runbooks + `docs/operations/upgrades.md` |
| Graceful shutdown | воркер дожидается in-flight заданий; подтверждено логом |

## F. Известные ограничения

1. **Поиск на 50 000 контактов деградирует до полного скана.** Подтверждено на
   наборе бенчмарка: `count(*)` с условием из трёх `ILIKE` через `OR` даёт
   **Seq Scan, 78 мс, 49 999 строк отфильтровано**; одна ветка `email ILIKE`
   даёт Bitmap Index Scan; `enable_seqscan=off` выбирает `contacts_active_idx` и
   не быстрее. Причина — триграммы термина совпадают со всеми строками, а
   планировщик не складывает три ветки `OR` в дешёвый план. Поиск работает
   корректно, но при 50k строк один запрос стоит ~150 мс (бенчмарк
   `contacts_list_search_selective`). Это **не блокер**, а записанное ограничение;
   следующий шаг — `tsvector` + GIN вместо трёх независимых триграммных индексов.
2. **Лимитер частоты на процесс по умолчанию.** `FORGECRM_RATE_LIMIT_STORE`
   по умолчанию `memory`, то есть при нескольких репликах лимит умножается на их
   число. Для многократной репликации есть `RedisStore` (`docker-compose.multireplica.yml`
   включает `redis`), но это осознанный выбор конфигурации, а не автоопределение.
   Ограничение задокументировано.
3. **CSP в режиме Report-Only.** Политика не блокирует, пока SPA не проверена в
   реальном браузере против неё; включение enforcing — отдельное решение.
4. **HSTS выключен по умолчанию** (`max-age=0`) — включается только за TLS.
5. ~~Два CVE в невызываемых путях.~~ **Устранено.** `GO-2026-4503`
   (`filippo.io/edwards25519@v1.1.0`) обновлён до `v1.2.0`, `GO-2026-5932`
   (`golang.org/x/crypto`) — до исправленной версии `v0.58.1-0.2026...`. После
   обновления `govulncheck` сообщает `No vulnerabilities found` — ни в
   вызываемом коде, ни в импортах, ни в модулях.
6. **Защита ветки `main` не включена.** С admin-токеном API вернул
   `Branch not protected`. Изменение настроек GitHub не входит в эту миссию —
   это действие владельца (см. §G).
7. **Нет дашбордов и формальных SLO**, подтверждённых данными; метрики отдаются.
8. **20 предупреждений линта фронтенда** (0 ошибок).
9. **Внешняя доставка** (реальные Telegram/email/вебхуки к чужим сервисам) не
   проверялась — `NOT VERIFIED` без credentials владельца.

## G. Вердикт

**CONTROLLED PILOT READY.**

| Оценка | Итог |
| --- | --- |
| Техническая готовность | высокая: CI 15/15, 260+ тестов фронтенда, 39+43 пакета backend, миграции и backup/restore проверены |
| Продуктовая готовность | основная CRM-функциональность реализована и проверена сквозными сценариями |
| Безопасность | сильная: RBAC, изоляция тенантов, CSRF, подпись вебхуков, аудит; ограничения — CSP Report-Only, невызываемые CVE |
| Эксплуатационная готовность | высокая: health/ready, метрики, алерты, runbooks, backup/restore; нет подтверждённых SLO и дашбордов |

**Почему именно этот статус, а не PUBLIC RELEASE READY:**

- техническая и эксплуатационная база готова, но у продукта **нет
  эксплуатационной истории**: SLO предложены, но не подтверждены данными, а
  пилот нужен ровно чтобы их получить;
- поиск на целевом масштабе (50k контактов) имеет записанную деградацию;
- защита ветки `main` не включена — для публичного релиза это стоит закрыть.

**Почему не BLOCKED и не INTERNAL DEMO READY:** нет ни одного P0; сквозные
сценарии реально работают; CI зелёный на опубликованном SHA; backup/restore
восстанавливает данные с совпадением счётчиков; обе невызываемые уязвимости
закрыты обновлением зависимостей. Это выше демо.

### Условия перехода к публичному релизу

1. Включить защиту `main` с обязательными проверками CI (действие владельца).
2. Прогнать контролируемый пилот, собрать SLI и подтвердить/пересмотреть SLO.
3. Опционально: реализовать `tsvector`+GIN для поиска при росте данных.

## H. Что осталось NOT VERIFIED

- Внешняя доставка вебхуков/Telegram/email к реальным сервисам (нужны
  credentials владельца).
- Производительность под конкурентной нагрузкой и реальным профилем трафика.
- Поведение production-развёртывания (публичный deployment недоступен).
- Дашборды и подтверждённые SLO — предложены, но не подтверждены данными.
