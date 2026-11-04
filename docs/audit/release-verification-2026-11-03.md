# Финальная релизная перепроверка — Phase H

- **Дата:** 2026-11-03
- **Аудитор:** инженерный агент
- **Область:** `main` @ `07d5dcf` (локально), `origin/main` @ `6484a50`
- **Цель:** независимо, сквозным прогоном переподтвердить безопасность, миграции,
  живость воркера, критические сценарии и готовность фронтенда перед релизом.
- **Вердикт:** **GO с условиями** — 0 блокеров P0; одно условие перед публикацией
  (см. §6, риск P1).

Этот документ — **записанное доказательство**, а не обещание. Каждая строка ниже
воспроизводима командой; там, где проверка требует живого сервиса, указано, как он
поднимался. Ничего из продуктового кода в ходе проверки не менялось.

## 1. Начальное состояние (проверено, не предположено)

| Факт | Доказательство |
|---|---|
| Локальный HEAD | `07d5dcf76ed3ec9ae5d76fe6b5054fe2a9cec61f` |
| Ветка | `main` |
| `origin/main` | `6484a50a742f3a1a1e4cadf02e8ba2f05beceda5` |
| Расхождение | локально **5 коммитов впереди** `origin/main`, `0` позади |
| Рабочее дерево | чистое (`git status --porcelain` пуст) |
| Лицензия | MIT (совпадает в `LICENSE`, `README.md`, `SECURITY.md`) |
| Видимость | public, default branch `main` |

Пять неотправленных коммитов — это и есть содержание Phase E/G/H
(`4178cac` bump `golang.org/x/net`, `ed0b5d6` тест триграммных индексов,
`abc57d2` heartbeat-таблица и метрика `workers_alive`, `91ee991` перф-базовая
линия, `07d5dcf` заметка в AGENTS.md). **CI на этих коммитах ещё не проходил** —
см. §6, риск P1.

## 2. CI на `origin/main`

Последний прогон на `origin/main` (`6484a50`) — **15/15 заданий `success`**,
прочитано через GitHub REST API, а не взято из предыдущего отчёта:

| Workflow | Задания |
|---|---|
| `ci.yml` | Frontend, E2E, Blitz Cloud deployment, Backend, Security, Visual regression, Constructor, Extraction, Docs, Docker image |
| `portability.yml` | Upgrade an existing database, Backup/restore/smoke, Documentation, Runs with no optional configuration, Full stack (Compose) smoke |

## 3. Безопасность

| Проверка | Команда | Результат |
|---|---|---|
| Статический анализ | `gosec` | 13 находок, все 13 приняты (0 новых) → гейт зелёный |
| Уязвимые зависимости | `govulncheck` | 0 в вызываемом коде; 2 в импортированных/required модулях, **не вызываются** |
| Утечки секретов | `gitleaks` | 282 коммита, ~10.62 МБ, утечек нет |

Известные, но не вызываемые уязвимости зафиксированы честно:

- `GO-2026-4503` — `filippo.io/edwards25519@v1.1.0`, исправлено в `v1.1.1`;
  путь вызова недостижим из продукта.
- `GO-2026-5932` — `golang.org/x/crypto/openpgp` (пакет объявлен
  неподдерживаемым); продукт его не вызывает.

`.gitleaks.toml` просмотрен: правила по умолчанию плюс allowlist только для
очевидно фиктивного placeholder-ключа из CI — реальных секретов в истории нет.

## 4. Миграции

### 4.1 Симметрия 0031 (`worker_heartbeats`) на изолированной БД

На `forgecrm_phaseh`:

| Шаг | Результат |
|---|---|
| Up | версия 31 |
| Down 1 | версия 30, **данные целы** (права 103, таблиц 78→77, trgm-индексов 16) |
| Up повторно | версия 31, без dirty-состояния |

### 4.2 Ограничения `worker_heartbeats`

Проверено на живом сервисе: PK по `worker_id`; дубликат вставки падает;
три различных id дают три строки; upsert сохраняет одну строку на id.

### 4.3 Полный прогон миграций

`forgecrm-api -migrate` на чистой БД: `count:31`, `schema ready version:31`.

## 5. Живость воркера и наблюдаемость

| Проверка | Результат |
|---|---|
| Строка heartbeat | присутствует (`role=worker`, `version=0.1.0-dev`) |
| Возраст heartbeat | растёт во времени |
| Устаревание | после остановки воркера через ~35 с `alive30=0` |
| `started_at` | сохраняется при рестарте |
| `/metrics` → `workers_alive` | 1 → 2 → 0 (согласовано с устареванием) |
| `/metrics` без токена | 401 |
| `/health`, `/ready` | 200 с корректным JSON |

Graceful shutdown подтверждён в логе воркера: `shutdown signal received; waiting
for in-flight jobs` → `worker shutdown complete; all in-flight jobs finished`.

## 6. Критические сценарии

Интеграционные и модульные тесты (verbose, против реального PostgreSQL 17 и
Redis 7 на `127.0.0.1:6379`) — **все проходят**, в том числе:

- аутентификация: `TestFullSignInLifecycle`,
  `TestLoginRateLimitIsSharedAcrossReplicas`,
  `TestSessionCreatedOnOneReplicaWorksOnTheOther`;
- метрики: `TestMetricsEndpointRequiresTheToken`,
  `TestMetricsSurfacesAgree`, `TestMetricsReportWorkerLiveness`;
- изоляция и права: `TestCrossOrganizationIsolation`,
  `TestAccountsAreScopedToTheActingOrganization`,
  `TestMCPCredentialIsolationBetweenOrganizations`, RBAC-юнит-тесты;
- целостность: `TestIdempotencyKeysAreScopedToTheCaller`, outbox-тесты,
  `TestExportIsAudited`, `TestSignPayloadAndVerifySignature`,
  `TestWebhookLifecycleOverHTTP`.

Четыре теста репликации/Redis при первом прогоне были **SKIP** (переменная
окружения не задана). Redis подтверждён (`PONG`), тесты перезапущены с
переменной — все **PASS**. Skip в отчёте означает «не запускался», а не
«прошёл»; здесь это устранено.

**Свежие агрегаты на этой машине:**

| Набор | Результат |
|---|---|
| Backend unit (без БД) | 39 пакетов `ok`, 0 `FAIL` |
| Backend integration | 43 пакета `ok`, 0 `FAIL` |
| `go vet ./...` | чисто |
| `archtest` | ok |
| `deploycheck` | ok (6 тестов) |

## 7. Фронтенд

| Проверка | Команда | Результат |
|---|---|---|
| Типы | `npm run typecheck` | 0 ошибок |
| Линт | `npm run lint` | 0 ошибок, 20 предупреждений (exit 0, как в CI) |
| Юнит-тесты | `npm run test` | **39 файлов / 260 тестов passed** |
| Сборка | `npm run build` | `dist` собран |
| E2E (Chromium, реальный API и БД) | `npm run e2e` | **85 passed, 1 skipped, 0 failed** |
| Визуальная регрессия | `npx playwright test --project=visual` | **8 passed** |

E2E-стек поднимался как в CI: изолированная БД `forgecrm_phaseh_e2e`, миграции,
`scripts/e2e-bootstrap.sh`, реальный API на `127.0.0.1:8080`, Vite — силами
Playwright. Единственный skip — `platform.spec.ts` про провайдера ИИ: тест
пропускается, когда провайдер не сконфигурирован (`test.skip` по условию), это
ожидаемое поведение, а не сбой.

Визуальные эталоны — Linux-only и генерируются в образе
`mcr.microsoft.com/playwright:v1.49.1-jammy`. Docker-демон в этой среде не
поднимается (stale `containerd`), поэтому сравнение выполнено локальным
Playwright **той же версии** (`1.49.1`, `chromium-1148`) — тем же браузерным
билдом, что и CI. Все 8 эталонов совпали. Абсолютную воспроизводимость
растеризации гарантирует задание `visual` в CI; здесь подтверждено отсутствие
структурной регрессии макета.

## 8. Документация и контракты

| Гейт | Результат |
|---|---|
| `make openapi-check` | спецификация и `index.html` актуальны |
| `make facts-check` | `project-facts.md` соответствует коду |
| `make docs-check` | документы на месте, ссылки живые, 180 путей / 234 операции, RBAC 103 |
| `make docs-claims` | числа и статусы совпадают с кодом |
| `make forge-check` | 37 команд, справка и диспетчер согласованы |
| `make fmt-check` | `gofmt` чист |

## 9. Производительность — перепроверка записанного finding

`docs/performance/baseline.md` утверждает: поиск по трём `ILIKE` через `OR` не
использует триграммный индекс и деградирует до полного скана. Это
**воспроизведено независимо** на изолированной БД `forgecrm_perf` (50 000
контактов, `ANALYZE`, `EXPLAIN (ANALYZE, BUFFERS)`):

| Запрос | План | Время |
|---|---|---|
| Только ветка `email ILIKE` | Bitmap Index Scan по `contacts_email_trgm_idx` | **8.7 мс** |
| Полное условие (три ветки `OR`) | **Seq Scan**, `Rows Removed by Filter: 49999` | **68.8 мс** |
| Полное условие, `enable_seqscan=off` | Index Scan по `contacts_active_idx`, `Rows Removed: 49999` | 72.6 мс (не быстрее) |

Вывод документа подтверждён: индекс по одиночной ветке работает, но
планировщик не складывает три ветки `OR` в дешёвый план, и принуждение не
помогает. Это **записанный факт базовой линии, а не регрессия**; следующий шаг,
если поиск станет приоритетом, — выражение `tsvector` с GIN-индексом.

## 10. Готовность к эксплуатации

| Проверка | Результат |
|---|---|
| `docker-compose.yml` | API и worker — общий образ, раздельные бинарники через entrypoint-диспетчер |
| Worker при SIGTERM | `stop_grace_period: 45s`, `FORGECRM_WORKER_SHUTDOWN_GRACE` 30 с |
| Зависимости запуска | worker стартует только после `service_healthy` API (миграции применяет API) |
| Healthcheck API | busybox `wget` на `/health` |
| Многократная репликация | `docker-compose.multireplica.yml` (2 API за nginx, общие PG и Redis) |
| Мониторинг | `docs/operations/monitoring.md`: 8 алертов, включая `workers_alive = 0` |
| Хост-предпроверка | `make doctor` |

## 11. Риски

| Уровень | Риск | Обоснование | Что делать |
|---|---|---|---|
| **P0** | — | блокеров нет | — |
| **P1** | 5 коммитов на локальном `main` не проходили CI на remote | CI зелёный только на `6484a50`; Phase E/G/H добавлены после | до публикации прогнать CI на этих коммитах (push и дождаться зелёного) |
| **P2** | визуальная регрессия проверена не в том же образе, что CI | Docker-демон недоступен; сравнение локальным Chromium той же версии | перепроверить заданием `visual` в CI; при расхождении — блокирует |
| **P3** | 2 неисправленные CVE в невызываемых путях | недостижимы из продукта | обновить при следующем плановом bump зависимостей |
| **P3** | ветка `main` без обязательной защиты | API вернул `403 Resource not accessible by integration` — токен без прав на чтение защиты; **состояние защиты не подтверждено, а не «отсутствует»** | владельцу проверить/включить required checks вручную |
| **P3** | 20 предупреждений линта фронтенда | 0 ошибок, CI зелёный | почистить инкрементально |
| **P3** | отсутствуют SLO/SLI и дашборды | заявлено в `monitoring.md` как сознательно не сделанное | задача эксплуатации |

## 12. Вердикт

**GO с условиями.** Секреты, миграции, живость воркера, критические сценарии,
фронтенд, документация и контракты переподтверждены на текущем состоянии.
Продуктовых дефектов не найдено; 0 блокеров P0.

Единственное условие перед публикацией — **P1**: прогнать CI на пяти
неотправленных коммитах и дождаться зелёного результата. P2 проверяется
заданием `visual` в CI. P3 — плановые улучшения, не блокирующие релиз.

## 13. Как воспроизвести

```bash
# статические гейты (без сервисов)
make lint openapi-check facts-check docs-check docs-claims forge-check

# backend
make test-unit
FORGECRM_TEST_DATABASE_URL=postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable \
FORGECRM_TEST_REDIS_URL=redis://127.0.0.1:6379/1 \
  go test -tags=integration -count=1 -p 1 ./...

# frontend
cd frontend && npm run typecheck && npm run lint && npm run test && npm run build

# e2e и visual (нужны миграции, bootstrap-владелец и запущенный API)
cd frontend && FORGECRM_API_URL=http://127.0.0.1:8080 npm run e2e
cd frontend && FORGECRM_API_URL=http://127.0.0.1:8080 npx playwright test --project=visual
```

## 14. Изменения в ходе проверки

Продуктовый код не менялся. Созданы и удалены: черновые БД
`forgecrm_phaseh`, `forgecrm_phaseh_live`, `forgecrm_phaseh_e2e`,
`forgecrm_perf`; временный помощник миграций; собранный бинарник API.
Рабочее дерево чистое. Коммитов в ходе Phase H не делалось.

## 15. Дополнение: публикация и подтверждение remote CI (2026-11-04)

Закрывает P1 из §11.

| Параметр | Значение |
| --- | --- |
| Исходный SHA | `6484a50` (`origin/main` до публикации) |
| Опубликованный SHA | `0c748e2` |
| Тип отправки | обычный push, fast-forward `6484a50..0c748e2`, без force |
| Отправленные коммиты | `4178cac`, `ed0b5d6`, `abc57d2`, `91ee991`, `07d5dcf`, `0c748e2` |
| Секрет-аудит | `gitleaks 8.28.0` по диапазону `origin/main..HEAD`: утечек нет |
| Remote CI | CI run `41747583263` + Portability run `41747583255`, оба на `0c748e2` |

**Результат каждого обязательного job:**

| Workflow | Задание | Итог |
| --- | --- | --- |
| CI | Backend (format, vet, test, build) | success |
| CI | Frontend (lint, typecheck, test, build) | success |
| CI | End-to-end (real API, real database, real browser) | success |
| CI | Visual regression (baselines on Linux) | success |
| CI | Security (dependencies, secrets, static analysis) | success |
| CI | Docker image builds | success |
| CI | Constructor (each recipe builds a working project) | success |
| CI | Extraction (each module builds without the monolith) | success |
| CI | Blitz Cloud deployment (root image, clean database) | success |
| CI | Required documentation is present | success |
| Portability | Upgrade an existing database | success |
| Portability | Backup, restore and smoke test | success |
| Portability | Runs with no optional configuration | success |
| Portability | Documentation is present and linked | success |
| Portability | Full stack (Compose) smoke test | success |

**15/15 success.** Visual regression на опубликованном SHA подтверждена
заданием CI (закрывает P2). Ветка `main` защищена не была — API с admin-токеном
вернул `Branch not protected` (остаётся P3, действие владельца).

**Статус P1: ЗАКРЫТ.** Phase E/G/H подтверждены remote CI на опубликованном SHA.

Итоговый статус релиза — в
[FINAL_RELEASE_CERTIFICATION.md](FINAL_RELEASE_CERTIFICATION.md).
