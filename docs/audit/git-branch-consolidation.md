# Консолидация Git-веток ForgeCRM в `main`

Дата: 2026-11-07. Консолидация кода завершена на `b0393c0`; финальный tip
`main` (с этим отчётом) — `23709a1`. Обе точки синхронизированы с `origin/main`.

Статусы: VERIFIED · INTEGRATED · PARTIAL · NOT INTEGRATED · DELETED.

## 0. Резервная копия

- SHA всех локальных и удалённых веток до начала: `/workspace/audit/backups/branch-shas-20261107-164103.txt`.
- Полный bundle всей истории: `/workspace/audit/backups/forgecrm-all-20261107-164103.bundle`
  (`git bundle verify` → «records a complete history»).
- Локальные refs `refs/backup/20261107-164103/*` (18 шт.) на все ветки.
- Тег `archive/arena-7bd19000-frontend-redesign-20261107` → `5a905a0` (запушен на GitHub).
- `git reset --hard` и `git clean -fd` не использовались; force push не применялся.

## 1. Исходное состояние

`origin/main` ушёл вперёд на 19 коммитов (PR #63–#65) и **разошёлся** с локальным
`main` (локально 18 своих коммитов). Ни одна из сторон не была предком другой.

## 2. Таблица веток

| Ветка | Исходный SHA | Общий предок | Уникальные коммиты | Статус |
|-------|--------------|--------------|--------------------|--------|
| `main` (локальный) | `b63e773` | — | 18 | INTEGRATED |
| `origin/main` | `2e8685e` | `4bc3cd5` | 19 | INTEGRATED |
| `feat/oidc-explicit-binding` | `a03c33e` | `a03c33e` (предок main) | 0 | INTEGRATED |
| `feat/telegram-standalone-service` | `6dc7c3f` | — | 1 | INTEGRATED |
| `merge/oidc-explicit-binding` | `3844cdd` | — | лок. merge-коммит | INTEGRATED |
| `merge/telegram-standalone` | `7ca4dd2` | — | лок. merge-коммит | INTEGRATED |
| `fix/serve-crm-at-root` | `68ef993` | `022f78b` | 0 | INTEGRATED |
| `fix/visual-accounts-order-independence` | `145efd2` | `145efd2` | 0 | INTEGRATED |
| `groq/ai-provider-catalog` | `370edcd` | `370edcd` | 0 | INTEGRATED |
| `arena/144f1b8b-forgecrm` | `11c3502` | `11c3502` | 0 | INTEGRATED |
| `arena/61498510-forgecrm` | `d547534` | `d547534` | 0 | INTEGRATED |
| `arena/c036360c-forgecrm` | `2e49b9b` | `2e49b9b` | 0 | INTEGRATED |
| `arena/f3b0bd45-forgecrm` | `32c5357` | `32c5357` | 0 | INTEGRATED |
| `arena/58b35bf6-forgecrm` | `fb31d7a` | — | 0 (через origin/main) | INTEGRATED |
| `arena/7a43a2ba-forgecrm` | `5661b90` | — | 0 (через origin/main) | INTEGRATED |
| `arena/7bd19000-forgecrm` | `5a905a0` | **НЕТ (несвязанная история)** | 306 | NOT INTEGRATED (архив) |

«Уникальные коммиты = 0» означает, что все коммиты ветки уже достижимы из `main`
(`git merge-base --is-ancestor`).

## 3. Слияние `origin/main` → `main`

Коммит `f7ae013`. Разрешено объединением обеих сторон, без массового «ours/theirs»:

| Файл | Конфликт | Решение |
|------|----------|---------|
| `backend/internal/config/config.go` | AI-настройки (мои vs origin) | Взяты AI-изменения origin (нормализация провайдера, алиасы, `groq`, таблица base-URL) + сохранены поля Telegram-standalone из локальной ветки |
| `backend/internal/api/ai_handlers.go` | каталог провайдеров | Версия origin (включая запись `groq`) |
| `backend/internal/api/ai_providers_test.go` | add/add | Ассерты origin (на основе `config.AIProviderKinds()`) + мой сквозной `probeProviderAccepted` через `config.Validate` |
| `backend/internal/config/config_test.go` | тесты провайдеров | Тесты origin (`groq`, нормализация) + мой тест дефолтного эндпоинта OpenRouter |
| `docs/verification/project-facts.md` | генерируемый | Перегенерён |
| `backend/internal/ai/provider_kind_test.go` | дубликат | Удалён: `provider_test.go` origin — надмножество |

## 4. `arena/7bd19000-forgecrm` — детальный анализ

Несвязанная история (`git merge-base` → пусто; 306 уникальных коммитов). Обычный
merge дал бы 162 конфликта и переписал бы проект. Проведён разбор по существу.

Проверено по коду, что **все** содержательные изменения ветки уже присутствуют в
`main` (пришли другими путями — через PR #63–#65, `feat/oidc-explicit-binding`,
`fix/serve-crm-at-root` и ранние интеграции):

| Изменение ветки | Коммит(ы) | В `main` |
|---|---|---|
| CVE: `filippo.io/edwards25519` v1.2.0, `golang.org/x/crypto` фикс | `41f78b5` | VERIFIED (в `go.mod`) |
| `golang.org/x/net` v0.62.0 | `4178cac` | VERIFIED |
| Триграммные индексы для поиска | `746d5e9` | VERIFIED (`0030_perf_trgm_indexes`) |
| Worker heartbeat + `workers_alive` | `abc57d2` | VERIFIED (`0031_worker_heartbeats`, `jobs/heartbeat.go`) |
| MCP batch + cookie attributes | `36e74d8` | VERIFIED (`mcp_protocol.go`, `config.go`) |
| CSP Report-Only, HSTS opt-in, Referrer-Policy | `e40206c` | VERIFIED (`api/server.go`, `httpx`) |
| Закрытие backslash open-redirect в OIDC `return_to` | `e2e0887` | VERIFIED (`oidc.SafeReturnTo` + allow-list) |
| Идемпотентность: лизинг брошенного claim | `e5376a2` | VERIFIED (`internal/idempotency`) |
| tenantconfig (конфигурация организации) | `762882b` | VERIFIED (`internal/tenantconfig`) |
| Принудительный consent на исходящую почту | `413bcf6`, `00bbd50` | VERIFIED (`internal/consent`) |
| create_task уважает default priority организации | `f1d5e21` | VERIFIED (регресс-тест в `workflow_engine_test.go`) |
| gosec как gate в CI | `cb533d0` | VERIFIED (`.github/workflows/ci.yml`) |
| Экспорт uuid канонично в CSV/JSONL | `d04765a` | VERIFIED |
| Страницы detail для lead/task/ticket, i18n, breadcrumbs | `dd8bc30` и др. | VERIFIED (`LeadsPage` и пр.) |

Единственная уникальная дельта ветки — **альтернативная дизайн-система**:

- `frontend/src/components/ds/` (`CommandPalette`, `DataTable`, `ListLayout`,
  `controls`, `overlays` — ~1250 строк),
- `frontend/src/styles/design-system.css` (1020 строк),
- command palette по Ctrl/Cmd+K.

`main` уже покрывает эти задачи своими компонентами (`GlobalSearch`,
`ResourceList`, `ui.tsx`). Согласно ТЗ, альтернативная дизайн-система **не
переносится без доказанной необходимости**. Реализация ветки: **NOT INTEGRATED**;
её история и SHA сохранены тегом `archive/arena-7bd19000-frontend-redesign-20261107`.

## 5. Миграции

- Всего 39 миграций; номера уникальны; у каждой `*.up.sql` есть парный `*.down.sql`.
- Старшая: `0039_oidc_state_browser_binding`. Коллизия номеров при вливании OIDC
  разрешена переносом на `0038/0039` (в локальной ветке против `origin/main`).
- Данные не терялись: конфликтов с удалением/переименованием таблиц не было.

## 6. Сохранённые функции

OpenRouter (провайдер + верифицированная модель `nvidia/nemotron-3-ultra-550b-a55b:free`),
SSE-стриминг, MCP-реестр (8 инструментов), сервис агента/согласований, telegram
standalone service, OIDC explicit binding + browser-bound state, RBAC/tenant
isolation, idempotency, consent, tenantconfig, retention, триграммный поиск,
worker heartbeat, CSP/HSTS, audit hash-chain, gosec gate.

## 7. Проверки на итоговом `main` (`b0393c0`)

| Проверка | Результат | Статус |
|----------|-----------|--------|
| `go build ./...` | exit 0 | VERIFIED |
| `go vet ./...` | exit 0 | VERIFIED |
| `gofmt -l ./cmd ./internal` | пусто | VERIFIED |
| `go test -race -count=1 ./...` | exit 0, 46 пакетов `ok`, 0 FAIL | VERIFIED |
| `tsc -b` | exit 0 | VERIFIED |
| Frontend tests | 50 файлов / 382 теста — pass | VERIFIED |
| `vite build` | exit 0 | VERIFIED |
| `scripts/docs-check.sh` | exit 0 | VERIFIED |
| `openapi-gen -check` | exit 0 | VERIFIED |
| `facts-gen -check` | exit 0 | VERIFIED |
| Миграции (парность/уникальность) | 39, все парные | VERIFIED |
| Docker/PostgreSQL | недоступны в среде | NOT RUN |

## 8. Публикация и удаление

- `main` запушен fast-forward: `2e8685e..b0393c0`. `git ls-remote --heads` → только `refs/heads/main`.
- Локальный `main` == `origin/main` == `b0393c0`.
- Тег архива запушен.
- Удалены 11 удалённых веток и 4 локальные; remote-tracking prune → только `origin/main`.
- Открытых PR нет (0). Теги, релизы и история коммитов не удалялись.

## 9. Итог

- Локальные ветки: только `main`. Удалённые: только `origin/main`. Дерево чистое.
- `main` содержит всю полезную функциональность всех веток.
- `arena/7bd19000`: полезные изменения — INTEGRATED (через другие пути);
  альтернативная дизайн-система — NOT INTEGRATED (архивный тег).
- Резервная копия: `/workspace/audit/backups/` + `refs/backup/20261107-164103/*`.
