# Продолжение финальной проверки ForgeCRM — CI, deployment, Telegram, очереди

- **Тип:** read-only аудит. Production environment не менялся, деплой не выполнялся,
  код не менялся, секреты не раскрывались, отчёт не закоммичен.
- **Дата/время:** 2026-11-04, ~18:25–18:47 UTC.
- **Репозиторий:** `ForgeLabHub/ForgeCRM`.
- **Связанный документ:** `docs/audit/FINAL_RELEASE_VERIFICATION.md` (эта миссия
  проверяет и уточняет его).

---

## 0. Краткий итог

| # | Задача | Результат |
| --- | --- | --- |
| 1 | Актуальность `FINAL_RELEASE_VERIFICATION.md` | ⚠️ Частично устарел: `main` ушёл с `623ac94` на `cc65d3f`; уточнён механизм stale-commit (нет `.git`, а не build-arg) |
| 2 | Инструкция по удалению `FORGECRM_COMMIT` + redeploy | ✅ Подготовлена (§2) |
| 3 | Перезапуск Portability #285 | ✅ Выполнен: attempt 2 — **success 5/5** |
| 4 | CI и повторный запуск | ✅ `#285` success; `#382/#282` (76a5298) 15/15; `#388/#288` (cc65d3f) success |
| 5 | Telegram smoke-test | ✅ Подготовлен (§5) |
| 6 | API/Worker/Outbox/очереди/ошибки | ✅ Проверено по коду (§6) |
| 7 | Безопасность Telegram Mini App | ✅ `initData`/Mini App **в коде отсутствует** — нечего эксплуатировать (§7) |
| 8 | Release blockers | ✅ Список в §8 |

---

## 1. Актуальность `FINAL_RELEASE_VERIFICATION.md`

Проверено повторно. Документ **в основном верен для своего SHA (`623ac94`)**,
но к моменту этой проверки устарел в двух местах:

1. **`main` ушёл вперёд.** `origin/main` = **`cc65d3fe63a8b7fb5bb0730467ef0574445ef32f`**
   («Merge pull request #50 …», 2026-11-04T18:23:54Z). Это +2 коммита к `623ac94`:
   `e43afa2 docs: audit post-merge portability and deployment` → `cc65d3f` (merge).
   Изменён **только** `docs/audit/POST_MERGE_PORTABILITY_DEPLOYMENT_AUDIT.md` (+98 строк).
   Кода, миграций, Dockerfile, compose — не тронуто. Значит `ExpectedVersion` и
   поведение приложения не изменились.
2. **Уточнён механизм stale-commit.** Прежний вывод «build-arg `COMMIT` из
   `docker-compose.yml` перекрывает штамп» **не подтверждается** для Blitz:
   руководство `docs/deployment/blitz-cloud.md` прямо утверждает, что blitz.cloud
   собирает корневой `Dockerfile` сам и **build args не передаёт**. Точный механизм —
   ниже (§1.1).

### 1.1 Точный механизм `main (8d350b0)` — доказано экспериментом

Собран корневой образ из контекста **без `.git`** (что и делает Blitz, когда `.git`
недоступен сборщику) и проверен `--version`:

| Сборка | env | `--version` |
| --- | --- | --- |
| нет `.git`, build args нет | — | `forgecrm-api dev (unknown)` |
| нет `.git`, build args нет | `FORGECRM_COMMIT=8d350b0…` | `forgecrm-api dev (8d350b0…)` |
| нет `.git`, build args нет | `FORGECRM_VERSION=main`, `FORGECRM_COMMIT=8d350b0…` | `forgecrm-api main (8d350b0…)` |

**Вывод:** ровно наблюдаемое в production `main (8d350b0…)` получается при
**(a)** отсутствии `.git` в контексте сборки (штамп `buildmeta` = `unknown`),
**(b)** env `FORGECRM_VERSION=main` и **(c)** env `FORGECRM_COMMIT=8d350b0…`.
Оба env-значения — запасной путь, который срабатывает только при пустом штампе
(`buildInfo()` в `cmd/forgecrm-api/main.go`). Дефект D-1 (метка) при этом остаётся:
ручная `FORGECRM_COMMIT` не обновляется вместе с кодом.

> Важно: этот вывод **не меняет** главного факта — **развёрнут актуальный код**.
> Доказано в `FINAL_RELEASE_VERIFICATION.md` §4.3 (побайтовое совпадение
> `index.html`, совпадение content-hash имён бандлов, наличие маршрутов
> `/csp-report` и `/api/v1/telegram/webhook`, которых нет в старых ревизиях).

---

## 2. Удаление `FORGECRM_COMMIT` и повторный деплой (Blitz Cloud)

Руководство: `docs/deployment/blitz-cloud.md` (раздел «Переменные окружения»).
Оба env — **не секреты**, показывать их значения безопасно.

### 2.1 Удалить `FORGECRM_COMMIT`
1. Открыть `dashboard.blitz.cloud` → приложение ForgeCRM → вкладка **Environment**.
2. Найти строку **`FORGECRM_COMMIT`** (текущее значение `8d350b0…`).
3. **Удалить переменную** (не «заменить на другой SHA»).
4. Оставить `FORGECRM_VERSION=main` — она задаёт осмысленную метку версии и штамп
   не перекрывает (отдельный build-arg).

### 2.2 (Опционально) сделать штамп надёжнее
Если после redeploy `/ready` покажет `commit=unknown` (а не SHA), значит сборщик
Blitz не получает `.git` в контексте. Тогда временная мера — задать
`FORGECRM_COMMIT` корректным SHA **на момент релиза** и обновлять её при каждом
деплое; постоянное решение — обеспечить `.git` в build-контексте (см. §8, blocker B-1).

### 2.3 Redeploy
1. Вкладка **Deployments** (или **Settings → Deploy**) → **Redeploy** / **Deploy latest**.
2. Дождаться сборки корневого `Dockerfile` (образ содержит API + worker + SPA).
3. Проверить:
   - `GET /health` → `200 {"service":"forgecrm","status":"ok",…}`
   - `GET /ready` → `200` и **`commit` = актуальный SHA** (например `cc65d3f…`),
     а **не** `8d350b0…` и **не** `unknown`
   - вход в UI и базовая CRM-операция
4. Проверить, что часть **worker** действительно запущена: в логах части `worker`
   должна быть строка `starting forgecrm-worker … commit=…`.

> **Не** менять `FORGECRM_SECRET_KEY`, `DATABASE_URL`, `FORGECRM_BASE_URL` —
> это сломает существующие сессии/шифрование. Меняется только `FORGECRM_COMMIT`.

---

## 3. Перезапуск Portability #285 — результат

- Команда: `POST /repos/ForgeLabHub/ForgeCRM/actions/runs/41867925002/rerun-failed-jobs`
  → **HTTP 201** (принято).
- Итог **attempt 2**: `status=completed`, **`conclusion=success`**, SHA `623ac949…`.
- Все 5 jobs attempt 2 — **success**: `Runs with no optional configuration`,
  `Full stack (Compose) smoke test`, `Backup, restore and smoke test`,
  `Documentation is present and linked`, `Upgrade an existing database`.

Это подтверждает: падение attempt 1 было **транзиентным** (`proxy.golang.org`,
`stream error … INTERNAL_ERROR` при скачивании `github.com/redis/go-redis/v9@v9.23.0`),
а не дефектом кода или конфигурации.

---

## 4. CI — сводка

| Коммит | Workflow | Run | Результат |
| --- | --- | --- | --- |
| `76a5298` (D-1…D-5) | CI | #382 | ✅ 10/10 |
| `76a5298` | Portability | #282 | ✅ 5/5 |
| `623ac94` | CI | #385 | ✅ 10/10 |
| `623ac94` | Portability | #285 (attempt 1) | ❌ 3/5 (транзиент) |
| `623ac94` | Portability | #285 (attempt 2) | ✅ **5/5** |
| `cc65d3f` (текущий main) | CI | #388 | ✅ success |
| `cc65d3f` | Portability | #288 | ✅ success |

Все актуальные проверки зелёные. Открытых падений CI нет.

---

## 5. Telegram smoke-test (безопасные сценарии)

Выполнять **только** после отдельного согласования включения Telegram и в тестовом
аккаунте. Никакие токены/секреты не вводить в чат, Git, URL или логи.

**Предусловия:** `FORGECRM_TELEGRAM_ENABLED=true`, валидный
`FORGECRM_TELEGRAM_BOT_TOKEN`, `FORGECRM_TELEGRAM_WEBHOOK_ENABLED=true`,
`FORGECRM_TELEGRAM_WEBHOOK_SECRET` (32–256 символов `A–Z a–z 0–9 _ -`);
webhook зарегистрирован в Bot API на `https://<host>/api/v1/telegram/webhook`
с тем же secret; у тест-пользователя есть `tasks.read`/`tasks.update`.

| # | Шаг | Ожидаемо |
| --- | --- | --- |
| S1 | UI: Settings → Telegram → «Generate code» | код (43 симв.), `expires_in_seconds=900`, заголовок `Cache-Control: no-store` |
| S2 | В личном чате боту: `/start` | справка с командами, без данных |
| S3 | `/link <код>` | «Telegram is linked…»; повтор того же кода → «link_failed» (одноразовый) |
| S4 | Просрочить код (>15 мин) и `/link` | «link_failed» |
| S5 | `/link` кодом другого сотрудника | «link_failed» (привязка к чужому аккаунту невозможна) |
| S6 | `/tasks` | только задачи тест-пользователя (до 10), без чужих |
| S7 | `/today` | только задачи со сроком на текущие UTC-сутки |
| S8 | `/task <id>` чужой задачи | «Task not found.» |
| S9 | `/due <id> 2026-11-10T12:00:00Z` | «Task due date updated.» |
| S10 | `/done <id>` | «Task completed.»; повтор `/done` → «This task cannot be changed.» |
| S11 | `/done` без прав `tasks.update` | «You do not have permission for this action.» |
| S12 | `/unlink` | «no longer linked»; затем `/tasks` → «not linked… /link <code>» |
| S13 | Написать в **группе** боту | команды не исполняются (обрабатывается только личный чат) |
| S14 | Повтор доставки того же `update_id` | CRM-операция **не** повторяется (receipt) |
| S15 | Worker: назначить задачу на тест-пользователя | приходит PNG-карточка назначения (или текст при ошибке картинки) |
| S16 | Изменить срок / приблизить просрочку | карточка «due changed» / «overdue» |
| S17 | Отозвать согласие (consent) | отправка блокируется (`outcome=blocked`), сообщение не уходит |

**Негативные проверки (безопасные):**
- `POST /api/v1/telegram/webhook` без `X-Telegram-Bot-Api-Secret-Token` → `401`
  (уже подтверждено на live, §7).
- С неверным секретом → `401`; с телом > 1 МиБ → `413`; с телом не-объектом /
  `update_id < 0` / двумя событиями → `400`.
- Отправитель-бот (`is_bot=true`) → игнорируется.

---

## 6. API, Worker, Outbox, очереди, обработка ошибок

### 6.1 API
- Маршруты Telegram: анонимный `POST /api/v1/telegram/webhook` (выделенный
  rate-limiter 1200/мин, `FailClosed`) + админские `GET /api/v1/admin/telegram/status`,
  `POST /link-code`, `DELETE /link` под `requireSession` + CSRF.
- `/ready` (`auth_handlers.go` → `migrate.Check` → `checkVersion`) требует
  `applied >= ExpectedVersion`. Для `623ac94`/`cc65d3f` `ExpectedVersion = 33`,
  значит миграции `0032_telegram_link_codes` и `0033_telegram_updates` **применены**
  (это доказательство сильнее, чем статус `200` сам по себе).

### 6.2 Worker
- `cmd/forgecrm-worker`: не применяет миграции, а **ждёт** схему
  (`FORGECRM_WORKER_SCHEMA_WAIT`, дефолт 5 мин) — корректно.
- Циклы: outbox-drain, requeue устаревших job, maintenance (retention),
  delivery вебхуков, workflow-планировщик, очистка/просрочка Telegram.
- Liveness: heartbeat в `worker_heartbeats` (интервал 10 c, окно 30 c), метрика
  `workers_alive` — но она видна только в `/metrics`, который на production `503`
  (нет `FORGECRM_METRICS_TOKEN`), либо в `/admin/diagnostics` под админом.

### 6.3 Outbox (transactional outbox)
- `ClaimPending`: `FOR UPDATE SKIP LOCKED`, повторный claim не раньше 5 мин,
  `attempts < maxPublishAttempts`, исключая dead-lettered. At-least-once.
- `MarkFailed`: `attempts+1`, `last_error` (≤2000 симв.), снятие claim; по
  исчерпании — `dead_lettered_at` (видно оператору, не ретраится бесконечно).
- `ReleaseStale(10m)` при старте возвращает события «мёртвого» воркера.

### 6.4 Очередь уведомлений (jobs)
- События назначения/срока/завершения проецируются в **существующие** `jobs`
  (`telegram.task_notification`), kind'ы: `assignment`, `due_changed`, `completed`,
  `overdue`.
- Дедуп: `INSERT INTO telegram_task_delivery_receipts(dedupe_key) ON CONFLICT DO
  NOTHING` + уникальный индекс `jobs_telegram_dedupe_idx` по
  `payload->>'dedupe_key'` — повторное событие не создаёт вторую отправку.
- Просрочка: `telegram_due_revision` (UUID, trigger при смене assignee/due/status),
  ключ `overdue:<schedule_id>`; возврат к прежнему сроку создаёт новую ревизию.

### 6.5 Обработка ошибок
- `jobs.Fail`: если `attempts < max_attempts` → requeue с **экспоненциальным
  backoff** (1s·2^n, потолок 30 мин); иначе `status='failed'`.
- Webhook: `503` если transport/keyring/DB/CRM недоступны; `401` при неверном
  секрете; `413` тело > 1 МиБ; `400` при невалидном Update; иначе `200`.
- Ответы: исходное сообщение и код **не логируются**; отложенный ответ хранится
  зашифрованным (`reply_ciphertext`) и стирается после принятия Telegram.
- **Утечка токена исключена**: ошибки `net/http`/парсинга URL заменены на
  generic-сообщения (`send telegram message: request failed` / `invalid request`),
  потому что URL содержит bot token. Проверено в `notify/telegram.go` (актуальный
  main).

---

## 7. Безопасность Telegram (webhook + Mini App)

### 7.1 Что реально развёрнуто и безопасно
- **Секрет webhook.** `VerifyWebhookSecret` — constant-time сравнение
  (`secrets.ConstantTimeEqual`), требуется длина 32–256 и допустимый алфавит.
  При неверном/пустом секрете — `401` с **пустым** телом.
- **Live-факт:** `POST /api/v1/telegram/webhook` → **`401`** (не `503`) и заголовок
  `ratelimit-limit: 1200`. Это доказывает, что на production
  `WebhookEnabled()==true` → **Telegram включён и webhook-secret настроен корректно**.
  (Ветка `503` срабатывает только при выключенном/некорректном транспорте.)
- **Привязка пользователя.** Код — 32 случайных байта, в БД только SHA-256,
  TTL 15 мин, одноразовое транзакционное потребление под advisory-lock;
  `INSERT … ON CONFLICT (telegram_user_id)` не даёт «украсть» чужую активную
  привязку даже между организациями; `ResolveUser` каждый раз проверяет
  `linked_at/revoked_at`, статусы user/org/membership и пересчитывает RBAC.
- **Права.** `/tasks` требует `tasks.read`; `/done`/`/due` — `tasks.update`;
  задачи ограничены организацией и `assignee_user_id` (только «свои»).
  Перед повторной отправкой отложенного ответа проверяются привязка, права и
  актуальность/владелец задач (row-locks).
- **Consent.** Отправка проходит через `ConsentGate` (`AllowSend`), отказ →
  `ErrConsentBlocked`, `outcome=blocked`; сообщение не уходит.
- **Анти-replay.** `telegram_update_receipts(update_id PK)` — повторный update не
  повторяет CRM-операцию.

### 7.2 Telegram Mini App (`initData`) — **в коде отсутствует**
- Поиск по всему дереву `origin/main`: `initData`, `initDataUnsafe`,
  `window.Telegram`, `telegram-web-app`, `data_check_string` — **нет ни одного
  совпадения** в коде (только упоминание в `docs/telegram/PRODUCTION_ENVIRONMENT.md`).
- Frontend не подключает Telegram Web App SDK; авторизация — обычная сессия+CSRF.
- `X-Frame-Options: DENY` (live-подтверждено) и CSP `frame-ancestors 'none'`
  (Report-Only) **блокируют** встраивание.
- **Следствие:** нет `initData`, который можно было бы подделать, — класс
  уязвимостей «доверие к `initDataUnsafe`/Telegram ID» **отсутствует**, потому что
  функциональность не реализована. Если Mini App будет добавляться, потребуется
  серверная проверка подписи `initData` (HMAC-SHA256 c ключом
  `HMAC_SHA256(bot_token, "WebAppData")`), проверка `auth_date` и явная связка с
  CRM-пользователем и организацией (см. §8, N-1).

---

## 8. Release blockers и некритичные замечания

### Блокеры (важные, но не блокируют работу сайта)
- **B-1 (stale commit).** `/ready` показывает `8d350b0`, хотя развёрнут `cc65d3f`.
  Причина — env `FORGECRM_COMMIT` (и, вероятно, отсутствие `.git` в контексте
  сборки Blitz). Это **косметика**, код актуален. Действие владельца: §2.
- **B-2 (doc drift).** `docs/telegram/PHASE2_STATUS.md` утверждает «миграции `0032`
  и `0033` в production не применялись», а `FINAL_RELEASE_VERIFICATION.md` писал про
  механизм build-arg. Оба тезиса устарели (миграции применены — доказано `/ready`
  200 при `ExpectedVersion=33`; механизм — §1.1). Рекомендация: обновить доки
  (требует согласования, т.к. это правка кода/репо).

### Некритичные замечания
- **N-1 (Mini App не реализован).** Если планируется — сначала спроектировать
  серверную проверку `initData`, иначе небезопасно (см. §7.2).
- **N-2 (наблюдаемость worker).** Liveness воркера (`workers_alive`) недоступен
  без `FORGECRM_METRICS_TOKEN`; `/admin/diagnostics` — только под админом. Рекомендация:
  задать `FORGECRM_METRICS_TOKEN` и мониторить `workers_alive` + глубину очереди.
- **N-3 (token из env, не из зашифрованной записи).** Bot token читается из
  `FORGECRM_TELEGRAM_BOT_TOKEN`; требование ТЗ о шифрованном хранении не выполнено.
- **N-4 (retention ledger/update-receipts) — ЗАКРЫТО.** Таблицы
  `telegram_update_receipts` и `telegram_task_delivery_receipts` росли: worker
  чистит коды >1 сут и тела ответов >7 сут, но сами receipt-строки не удалялись.
  Теперь в `internal/retention` есть по политике на каждую таблицу (миграция
  `0037` добавляет частичные индексы, ведущие с временного столбца), обметающие
  **только завершённые** строки (`finished_at IS NOT NULL`), окно по умолчанию 90
  дней. In-flight строка никогда не удаляется, поэтому anti-replay и дедупликация
  не теряются. Покрыто `TestRetentionSweepsSettledTelegramUpdateReceipts`,
  `TestRetentionSweepsFinishedTelegramTaskDeliveryReceipts` и
  `TestRetentionSweepIndexesExist`.
- **N-5 (неатомарность сеть↔БД).** Сбой после принятия Telegram, но до commit →
  возможна повторная доставка ответа (описано в PHASE2_STATUS). At-least-once.
- **N-6 (SSO).** `docs/security/SSO_AUTHENTIK.md` — SSO не включать (вне Telegram).
- **N-7 (GITHUB_TOKEN).** В ходе аудита `GITHUB_TOKEN` давал `401 Bad credentials`;
  API-запросы шли через `GITHUB_PERSONAL_ACCESS_TOKEN`. Обновить секрет, если
  используется в автоматизациях.
- **N-8 (защита ветки).** Рекомендуется включить обязательные CI-проверки на `main`.

---

## 9. Доказательства (сводка)

- Git: `origin/main` = `cc65d3fe63a8b7fb5bb0730467ef0574445ef32f`; `76a5298` —
  предок; история не переписана.
- CI: #285 attempt 2 → success 5/5; #388/#288 (`cc65d3f`) → success; #382/#282
  (`76a5298`) → 15/15.
- Сборка: контекст без `.git` + `FORGECRM_VERSION=main` + `FORGECRM_COMMIT=8d350b0…`
  → `forgecrm-api main (8d350b0…)` (воспроизводит production ровно).
- Production: `/health` 200; `/ready` 200 (`commit=8d350b0`, `version=main`);
  бандлы `index-TUBd0gck.js` / `index-DkcWs5hq.css` совпадают со сборкой из
  актуального main; `POST /telegram/webhook` → 401 (пустое тело, лимит 1200);
  `X-Frame-Options: DENY`; CSP Report-Only.
- Код: `initData` в дереве **отсутствует**; `VerifyWebhookSecret` — constant-time;
  RBAC/consent/дедуп/receipt — присутствуют.

---

## 10. Конкретный список действий владельца

1. **Environment:** удалить `FORGECRM_COMMIT` (оставить `FORGECRM_VERSION=main`).
2. **Redeploy** приложения в Blitz; дождаться сборки корневого `Dockerfile`.
3. **Проверить после деплоя:** `/health` → 200; `/ready` → 200 и `commit` = актуальный
   SHA (не `8d350b0`, не `unknown`); вход в UI; базовая CRM-операция.
   Если `commit=unknown` — временно задать `FORGECRM_COMMIT` корректным SHA (§2.2).
4. **Worker:** убедиться, что часть `worker` запущена (логи: `starting forgecrm-worker`),
   и что задания обрабатываются (нет роста очереди).
5. **(Опционально) Наблюдаемость:** задать `FORGECRM_METRICS_TOKEN`, мониторить
   `workers_alive`, глубину очереди `jobs`, dead-letter outbox.
6. **Telegram:** после согласования прогнать smoke-test (§5); убедиться, что webhook
   зарегистрирован в Bot API на `/api/v1/telegram/webhook` с secret token; проверить,
   что секрет и токен не попадают в логи.
7. **Mini App:** не включать до реализации серверной проверки `initData` (§7.2, N-1).
8. **Docs:** обновить `PHASE2_STATUS.md` и `FINAL_RELEASE_VERIFICATION.md` (drift, B-2)
   — только с отдельного согласования.
9. **GitHub:** обновить `GITHUB_TOKEN`, если используется; включить защиту ветки `main`.

> Отчёт не закоммичен (по требованию). Все проверки — read-only; production
> environment, код и история Git не изменялись.

---

## 11. Аддендум: разделение состояний `623ac94` / `cc65d3f` и `FACTS_COMMIT`

Добавлено после уточнений владельца. **Два состояния проекта нельзя смешивать:**

| Событие | Коммит |
| --- | --- |
| Production проверен аудитом | `623ac94` |
| PR #50 объединён | `cc65d3f` |
| Последняя известная сборка Blitz | `cc65d3f` |
| Локализация Telegram | `5e72fc59` — **не опубликована** |

### 11.1 Проверка `5e72fc59`

Коммит `5e72fc59` **отсутствует** во всех ссылках этого репозитория: `git cat-file`
→ `Not a valid object name`, `git branch -a --contains` → ошибка, во всех
`refs/remotes/` его нет (`origin/main` = `cc65d3f`, прочие — `arena/*`). Это
подтверждает: он ещё **не запушен** и не входит ни в один аудит. Все выводы этого
отчёта относятся только к `623ac94` и `cc65d3f`.

### 11.2 `FACTS_COMMIT` — назначение в `cc65d3f` (проверено)

`FACTS_COMMIT` — **НЕ** то же самое, что `FORGECRM_COMMIT`. Это отдельная
переменная для инструмента генерации документации.

| Свойство | `FORGECRM_COMMIT` | `FACTS_COMMIT` |
| --- | --- | --- |
| Читатель | рантайм API/worker (`buildInfo()`/`buildCommit()`) | **только** `backend/cmd/facts-gen` (`commitFromEnv()`) |
| Назначение | запасной источник SHA для `/ready` и строки запуска | штамп SHA в блоке `FACTS:AUTO` файла `docs/verification/project-facts.md` |
| Кто задаёт | оператор (не следует — перекрывает штамп) | **CI** (`.github/workflows/ci.yml:95` → `${{ github.sha }}`) |
| Дефолт | `unknown` | нет (fallback: `git rev-parse HEAD`, затем «(SHA недоступен)») |
| В Dockerfile/entrypoint/compose | да (build-arg/fallback) | **нет** (отсутствует полностью) |
| Обязательна для запуска | нет | нет |

Доказательства (`cc65d3f`):
- `backend/cmd/facts-gen/main.go:98` — `if c := os.Getenv("FACTS_COMMIT"); c != ""`.
- `.github/workflows/ci.yml:95` — `FACTS_COMMIT: ${{ github.sha }}` (шаг «Project
  facts are current»).
- `docs/deployment/environment-variables.md:234` — раздел **Tooling**, не runtime.
- Grep по `backend/cmd/forgecrm-api/`, `backend/cmd/forgecrm-worker/`,
  `backend/internal/`, `Dockerfile`, `deploy/entrypoint.sh`, `docker-compose.yml`
  → **ноль** совпадений.

**Вывод:** в коде `cc65d3f` **ничто не требует** `FACTS_COMMIT` при запуске API
или worker; переменная влияет только на генерацию документации в CI. Если панель
Blitz выставляет `FACTS_COMMIT` как обязательную, это требование **платформы**, а
не приложения; безопасно задать её равной SHA деплоя (значение не секретное), но
на работу сайта она не влияет. Не смешивать с `FORGECRM_COMMIT`.

### 11.3 Текущее состояние production (проверено повторно, read-only)

- `/ready` → `{"commit":"8d350b0…","status":"ready","version":"main"}` — **без
  изменений**; production ещё не переразвёрнут.
- Бандлы `/assets/index-TUBd0gck.js` и `/assets/index-DkcWs5hq.css` — те же, что
  собираются из `cc65d3f` (документационный merge ассеты не меняет), т.е. код на
  production соответствует `cc65d3f`.
- `origin/main` = `cc65d3f`; ничего нового не запушено.

### 11.4 Рекомендация по переменным

- `FORGECRM_COMMIT` — **оставить удалённой** (согласовано).
- `FORGECRM_VERSION=main` — **не менять** (согласовано).
- `FACTS_COMMIT` — **не обязательна** для приложения; если Blitz требует её на
  уровне платформы, задать SHA деплоя; на `/ready`/CRM/Telegram не влияет.

### 11.5 Проверки после redeploy (план приёмки)

1. `GET /ready` → `200`, `commit` = SHA деплоя (не `8d350b0`, не `unknown`).
2. Авторизация: вход существующим пользователем; сессия и CSRF работают.
3. CRM: создать/открыть контакт, сделку, задачу; список и детальная карточка.
4. Telegram: `POST /api/v1/telegram/webhook` без секрета → `401`; `/link` → `/tasks`
   по smoke-test §5; назначение задачи присылает карточку.
5. Worker: лог `starting forgecrm-worker … commit=<SHA>`; очередь не растёт.

> Перезапуск Portability (Go proxy) больше не требуется — attempt 2 прошёл успешно
> (см. §3); зависимости не менять.
