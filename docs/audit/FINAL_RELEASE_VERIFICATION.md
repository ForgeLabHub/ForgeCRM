# FINAL RELEASE IDENTITY & DEPLOYMENT VERIFICATION

- **Тип проверки:** read-only аудит (код, ENV, deployment и git history не менялись;
  `git fetch` только обновил remote-tracking ссылки).
- **Дата и время:** 2026-11-04, ~17:43–18:10 UTC.
- **Репозиторий:** `ForgeLabHub/ForgeCRM` (публичный, MIT, default `main`).
- **Проверял:** OpenHands (агент), по запросу владельца.

> **ВАЖНО — отчёт исправлен.** Первая версия этого документа (по проверке 17:44–17:52)
> утверждала, что production собран «не из актуального кода». Повторная, более
> глубокая проверка показала обратное: **production собран из актуального `main`
> (`623ac94`), код D-1…D-5 в production присутствует**, а расходится только
> **отображаемое** значение `commit` в `/ready`. Причина расхождения установлена
> точно (см. §3–4). Ниже — скорректированные выводы; ошибочный тезис убран.

---

## 0. Главное в двух словах

| Вопрос | Ответ |
| --- | --- |
| Развёрнут ли актуальный код? | **Да.** Production = `main` `623ac94` (доказано побайтово). |
| Присутствуют ли D-1…D-5 в production? | **Да** (доказано по бандлу и маршрутам). |
| Почему `/ready` показывает `8d350b0`? | Это значение env-переменной `FORGECRM_COMMIT`, а не собранный коммит. |
| Что делать владельцу? | **Удалить `FORGECRM_COMMIT`** на вкладке Environment и выполнить redeploy. |

---

## 1. Git identity

| Команда | Результат |
| --- | --- |
| `git rev-parse HEAD` | `76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` |
| `git rev-parse origin/main` | `623ac949e664149d13a8639502dfb807354deceb` |
| `git status -sb` | `## main...origin/main [behind 10]` |

`git log --oneline -10` (локальный `HEAD` = `76a5298`):

```
76a5298 (HEAD -> main, origin/HEAD, fix/post-production-qa-stabilization) fix: close the five post-production QA defects (D-1…D-5)
80ae26e docs(audit): complete authenticated live production QA
00e83f3 docs(audit): live production browser QA of published ForgeCRM
30d0446 docs(audit): final repository audit and branch reconciliation
8cceccf docs(audit): note the certified code SHA and the docs-only tip
2e4fd9e docs(audit): certify final SHA 2d87ee1 with 15/15 CI
2d87ee1 docs(audit): mark both dependency advisories fixed in certification
41f78b5 fix(deps): update two modules with known vulnerabilities
026fd17 docs(audit): final release certification and push/CI addendum
4bae6b1 docs(ops): propose SLI/SLO targets and incident runbooks
```

`git show --stat --oneline HEAD`:

```
76a5298 fix: close the five post-production QA defects (D-1…D-5)
 33 files changed, 938 insertions(+), 84 deletions(-)
```

`git show HEAD:Dockerfile` — стадия `buildmeta` (штамп SHA из чекаута):

```dockerfile
FROM alpine:3.20 AS buildmeta
RUN apk add --no-cache git
WORKDIR /meta
COPY . /meta
RUN set -eu; \
    if [ -d /meta/.git ]; then \
        git config --global --add safe.directory /meta; \
        git -C /meta rev-parse HEAD > /revision; \
    else \
        printf 'unknown' > /revision; \
    fi
...
ARG VERSION=dev
ARG COMMIT=
RUN set -eu; \
    revision="${COMMIT:-$(cat /revision)}"; \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${revision}" \
        -o /out/forgecrm-api ./cmd/forgecrm-api && ...
```

### Точный SHA исправлений D-1…D-5

**`76a5298e4d636e22ec9fa3c53c17930aaac0e5d3`** (короткий `76a5298`) — единственный
коммит с исправлениями. Он есть и в `main`, и в `fix/post-production-qa-stabilization`.
Тега, содержащего этот SHA, нет.

### Сверка с предыдущим аудитом — почему «не совпадает»

Здесь два независимых расхождения, и оба объяснены:

1. **`76a5298` не был upstream-HEAD в прошлом аудите.** Прошлый аудит
   (`docs/audit/FINAL_REPOSITORY_AUDIT.md`, `LIVE_PRODUCTION_BROWSER_QA.md`)
   закончился на `80ae26e` (ранее сертифицировался код `2d87ee1`). `76a5298` —
   это **новый** коммит, добавленный поверх `80ae26e` миссией POST-PRODUCTION QA
   STABILIZATION. Его родитель — `80ae26e`; `8cceccf`, `2d87ee1`, `41f78b5` из
   прошлых отчётов являются его предками. История **не переписана**.

2. **`main` с тех пор ушёл вперёд ещё на 10 коммитов.** Пока локальный чекаут
   оставался на `76a5298`, удалённый `main` ушёл на `623ac94`:

```
623ac94 (origin/main, origin/HEAD) Merge pull request #49 from ForgeLabHub/arena/5fd3022f-forgecrm
8ecb7a3 Merge main and reconcile Telegram integration
bce68dd Accept Russian inflections in API documentation guard
9de5036 Format Go module requirements
710ddf1 Implement opt-in Telegram webhook and task notifications
ca13274 Prepare scoped Telegram task commands through CRM
820ff61 Record PostgreSQL CI and E2E dependency download failure
028f2fa Keep Telegram integration tests outside extracted module
0ab799a Harden Telegram link challenges and audit Authentik SSO
6ac40e7 Harden Telegram recipient lookup and clarify setup status
```

`git merge-base --is-ancestor 76a5298 623ac94` → да: `76a5298` — предок
актуального `main`, а не альтернативная ветка. **История не изменялась;
исправлять нечего.**

### Актуальный `main` (для справки)

- **Актуальный upstream HEAD: `623ac949e664149d13a8639502dfb807354deceb`**
  (2026-11-04T16:30:32Z, «Merge pull request #49 …»).
- Он содержит `76a5298` (все исправления D-1…D-5) плюс интеграцию Telegram.

---

## 2. CI

### 2.1 Прогоны на коммите исправлений `76a5298`

| Workflow | Run | SHA | Статус |
| --- | --- | --- | --- |
| **CI** | [#382](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41860210003) | `76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` | ✅ success |
| **Portability** | [#282](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41860209869) | `76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` | ✅ success |

15 jobs = 10 (CI #382) + 5 (Portability #282), все `success`:

CI #382: Required documentation is present · Docker image builds · Backend
(format, vet, test, build) · Constructor (each recipe builds a working project) ·
Frontend (lint, typecheck, test, build) · Security (dependencies, secrets, static
analysis) · Blitz Cloud deployment (root image, clean database) · Extraction
(each module builds without the monolith) · End-to-end (real API, real database,
real browser) · Visual regression (baselines on Linux).

Portability #282: Full stack (Compose) smoke test · Runs with no optional
configuration · Upgrade an existing database · Documentation is present and
linked · Backup, restore and smoke test.

> Запрос просил убедиться, что CI относится к коммиту исправлений, а не к
> предыдущему. Уточнение: коммит исправлений — это **`76a5298`** (не «предыдущий»);
> CI на нём зелёный, 15/15. Ниже — статус CI на актуальном `main`.

### 2.2 Прогоны на актуальном `main` (`623ac94`)

| Workflow | Run | SHA | Статус |
| --- | --- | --- | --- |
| **CI** | [#385](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867924938) | `623ac949e664149d13a8639502dfb807354deceb` | ✅ success (10/10) |
| **Portability** | [#285](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867925002) | `623ac949e664149d13a8639502dfb807354deceb` | ❌ failure (3/5) |

**Разбор падения Portability #285** (не дефект кода — сбой сети при скачивании
Go-модулей):

- job `Full stack (Compose) smoke test`, шаг `Build and start the stack`:
  `#25 ... go: github.com/redis/go-redis/v9@v9.23.0: read "https://proxy.golang.org/.../@v/v9.23.0.zip": stream error: stream ID 285; INTERNAL_ERROR; received from peer` → `go mod download` exit code 1.
- job `Runs with no optional configuration`, шаг `Build without secrets`:
  тот же `stream error` при скачивании `github.com/redis/go-redis/v9@v9.23.0`.

Оба — транзиентные ошибки прокси `proxy.golang.org`; **`CI #385` на том же коммите
полностью зелёный (10/10)**. Рекомендация: перезапустить Portability #285
(Re-run failed jobs); при повторении — закрепить GOPROXY/ретраи.

> Инфраструктурное замечание: `GITHUB_TOKEN` в момент аудита отвечал
> `401 Bad credentials`; API-запросы выполнены с `GITHUB_PERSONAL_ACCESS_TOKEN`.
> Секрет `GITHUB_TOKEN` стоит обновить, если он используется в автоматизациях.

---

## 3. Docker build metadata

### Как SHA попадает в бинарник

1. Стадия `buildmeta` копирует контекст (`COPY . /meta`) и, если `.git` есть,
   пишет `git rev-parse HEAD` в `/revision`; иначе — `unknown`.
2. Стадия сборки: `revision="${COMMIT:-$(cat /revision)}"` и
   `-ldflags "-X main.commit=${revision}"`.
3. `buildInfo()` подставляет env (`FORGECRM_COMMIT`, затем `RENDER_GIT_COMMIT`)
   **только** когда штамп пуст или `unknown`/`dev`.

Ключевая деталь: **build-arg `COMMIT` имеет приоритет над штампом**
(`${COMMIT:-…}`). Он приходит из `docker-compose.yml`:

```yaml
args:
  VERSION: ${FORGECRM_VERSION:-dev}
  COMMIT: ${FORGECRM_COMMIT:-}
```

### Проверено на этом коммите

| Сценарий | Как проверено | `/revision` (штамп) |
| --- | --- | --- |
| Full checkout с `.git` | `docker build --target buildmeta` | `76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` |
| Shallow-чекаут (`--depth 1`) | `git clone --depth 1 … && docker build --target buildmeta` | `76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` |
| Tarball без `.git` | `git archive … && docker build --target buildmeta` | `unknown` (сборка не падает) |

- **Какой SHA встраивается:** SHA чекаута (linker-флаг `main.commit`) —
  при условии, что build-arg `COMMIT` не задан.
- **Shallow / full checkout:** результат одинаковый (`git rev-parse HEAD` в
  shallow-клоне возвращает тот же SHA).
- **Если `.git` недоступен:** `/revision` = `unknown`, сборка штатная.
- **Сборка из tarball:** то же — `unknown`.
- **Может ли production случайно получить устаревший commit:** механизм штампа
  корректен, **но** `docker-compose.yml` передаёт `COMMIT` из `FORGECRM_COMMIT`,
  и непустой build-arg перекрывает штамп. Это ровно то, что наблюдается на
  production (см. §4). Доказательство: сборка образа с
  `--build-arg COMMIT=8d350b0…` даёт `forgecrm-api main (8d350b0…)`, хотя чекаут —
  `76a5298`. Вывод: **устаревший commit может попасть в `/ready`, если
  `FORGECRM_COMMIT` задана**, даже при корректном коде.

---

## 4. Production domains

Проверка 2026-11-04, ~17:44–18:08 UTC. Приложение **не будилось интерактивно** и
production **не менялся**. `forgecrm.forgecrm.blitz.cloud` отвечает `308` на
`forgecrm.forgecrm.prod.page` (`x-blitz-gate: redirect`) — это один сервис под
двумя именами.

### 4.1 `https://forgecrm.forgecrm.blitz.cloud`

| Проверка | Ответ |
| --- | --- |
| `/` | `308` → `https://forgecrm.forgecrm.prod.page/` |
| `/health` | `308` → `…/health` |
| `/ready` | `308` → `…/ready` |
| commit SHA | не отдаёт (перенаправляет) |
| CSP | нет собственного CSP (только `report-to` группы `cf-nel` от Cloudflare) |
| Время | `date: Wed, 04 Nov 2026 17:45:15–16 GMT` |

### 4.2 `https://forgecrm.forgecrm.prod.page`

| Проверка | Ответ | Время (UTC) |
| --- | --- | --- |
| `/` | `200`, `text/html`; ссылается на `/assets/index-TUBd0gck.js`, `/assets/index-DkcWs5hq.css` | 17:45–18:05 |
| `/health` | `200` → `{"service":"forgecrm","status":"ok","version":"main"}` | 17:59 |
| `/ready` | `200` → `{"commit":"8d350b0cbdbbc815cce3253a083b30cfd706fa15","status":"ready","version":"main"}` | 17:59 |
| `/metrics` | `503` → `{"type":"urn:forgecrm:error:disabled",…,"detail":"metrics scraping is not enabled; set FORGECRM_METRICS_TOKEN to enable it"}` | 17:44 |
| `POST /csp-report` | `204` | 17:59 |
| `GET /csp-report` | `405` → `{"type":"urn:forgecrm:error:bad_request","title":"Malformed request","status":405,"detail":"method not allowed","instance":"/csp-report"}` | 17:59 |
| `POST /api/v1/telegram/webhook` | `401` (маршрут смонтирован; отказ по секрету Telegram) | 18:07 |
| `POST /api/v1/does-not-exist` | `404` (контроль) | 18:07 |
| `GET /api/v1/admin/diagnostics` | `401` (требуется аутентификация) | 18:05 |

CSP-заголовки (на `prod.page`, режим **report-only**):

```
content-security-policy-report-only: default-src 'self';
  script-src 'self' 'sha256-vnv5pucUkxrn/0vQPxiGLGnKRB8NToF/9rfKNCzOx8E=';
  style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self';
  connect-src 'self'; frame-src 'none'; frame-ancestors 'none'; base-uri 'none';
  form-action 'self'; object-src 'none'; worker-src 'none'; manifest-src 'none';
  report-uri /csp-report; report-to csp-endpoint
reporting-endpoints: csp-endpoint="/csp-report"
```

### 4.3 Какая ревизия реально развёрнута — доказательства

`/ready` показывает `commit=8d350b0`, но это **не** собранный коммит. Что
развёрнут именно `623ac94`, доказано четырьмя независимыми способами:

1. **Побайтовое совпадение `index.html`.** Сборка frontend из `623ac94` даёт
   `index.html`, идентичный отдаваемому production (`diff` пуст).
2. **Совпадение content-hash имён бандлов.** Сборка frontend из `623ac94`
   (в том же образе `node:22-alpine`, что использует Dockerfile) даёт **ровно**
   `/assets/index-TUBd0gck.js` и `/assets/index-DkcWs5hq.css` — те же имена, что
   отдаёт production. (Сборка из `76a5298` даёт другие имена — `index-DygSuyFQ.js`
   — что и подтверждает: production **новее** `76a5298`.)
3. **Маршрут `POST /csp-report`** существует только в `76a5298` и позже; на
   production он смонтирован (`GET` → `405`).
4. **Маршрут `POST /api/v1/telegram/webhook`** добавлен только в диапазоне
   `76a5298..623ac94`; на production он смонтирован (`POST` → `401`, тогда как
   несуществующий путь → `404`).

Вывод: **production развёрнут из актуального `main` (`623ac94`); исправления
D-1…D-5 в production присутствуют.** Расхождение — только в *отображаемом*
`commit`.

### 4.4 Причина расхождения `commit=8d350b0`

На вкладке **Environment** приложения задана (устаревшая) переменная
`FORGECRM_COMMIT=8d350b0…`. blitz.cloud собирает проект по `docker-compose.yml`,
который передаёт `COMMIT: ${FORGECRM_COMMIT:-}` как build-arg; непустой build-arg
перекрывает штамп `buildmeta` (`revision="${COMMIT:-$(cat /revision)}"`). Поэтому
в бинарник попадает `8d350b0`, хотя собран код `623ac94`.

Почему `8d350b0`, а не что-то ещё: `8d350b0` — это «Merge pull request #44 …»
(2026-11-03) и **предок** актуального `main`; его, по-видимому, вписали вручную
на более раннем деплое, и с тех пор он не обновлялся — ровно дефект D-1.

Важно: это **косметическая** проблема (неверная метка), а не «старый код».
`FORGECRM_VERSION=main` (тоже env) штамп не перекрывает, потому что `VERSION` —
отдельный build-arg, а `version` в `buildInfo()` намеренно остаётся
env-overridable.

---

## 5. Итог

### Точный SHA исправлений
`76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` — все D-1…D-5; актуальный `main` —
`623ac949e664149d13a8639502dfb807354deceb` (содержит `76a5298` + Telegram).

### CI run
Исправления: [CI #382](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41860210003) +
[Portability #282](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41860209869) — 15/15 ✅.
Актуальный `main`: [CI #385](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867924938) 10/10 ✅;
[Portability #285](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867925002) 3/5 ❌
(транзиентные ошибки `proxy.golang.org`, не код).

### Фактический статус доменов
- `forgecrm.forgecrm.blitz.cloud` — `308` → `prod.page` (алиас).
- `forgecrm.forgecrm.prod.page` — **работает**; код = `623ac94`;
  `/ready` показывает устаревший `commit=8d350b0`; CSP — report-only.

### Какие проверки подтверждены
- ✅ Git: `76a5298` — предок актуального `main`; история не переписана.
- ✅ CI: 15/15 на `76a5298`; 10/10 (CI) на `623ac94`.
- ✅ Docker metadata: штамп чекаута работает (full/shallow/tarball); доказано,
  что build-arg `COMMIT` перекрывает штамп — это и даёт устаревшую метку.
- ✅ Production: код актуален (`623ac94`) — доказано бандлом, `index.html` и
  маршрутами; D-1…D-5 в production присутствуют.
- ✅ Причина stale-`commit` установлена: env `FORGECRM_COMMIT` → compose build-arg.

### Готовность проекта

| Компонент | Состояние |
| --- | --- |
| GitHub main | ✅ `623ac94` (локальный чекаут отставал на 10 коммитов) |
| CI | ✅ CI 10/10 на `623ac94`; ⚠️ Portability #285 — транзиентный сбой (перезапустить) |
| Docker revision | ✅ штамп корректен; ⚠️ перекрывается env `FORGECRM_COMMIT` |
| Production API | ✅ `health` 200, `ready` 200, webhook-маршруты смонтированы |
| Production revision | ⚠️ `/ready` = `8d350b0` (env-метка); фактический код = `623ac94` |
| Исправления D-1…D-5 в production | ✅ присутствуют (код), но D-1-метка неверна |
| Telegram production | ⚠️ код + маршрут есть; требует проверки владельцем (см. ниже) |

### Что требует ручного действия владельца
1. **Удалить `FORGECRM_COMMIT`** на вкладке Environment (не заменять другим SHA).
   `FORGECRM_VERSION=main` можно оставить.
2. **Перезапустить Portability #285** (или дождаться нового прогона) — падение
   из-за `proxy.golang.org`, не из-за кода.
3. **Redeploy.** После него `/ready` должен показать `623ac94…` (или его
   корректное сокращение). Если покажет `unknown` — значит, сборка идёт без `.git`
   в контексте (тогда нужен отдельный разбор; признак — `unknown`, а не SHA).
4. **Проверить вживую после redeploy:** `/health`, `/ready`, вход, основные
   CRM-функции.
5. **Telegram:** проверить переменные `FORGECRM_TELEGRAM_ENABLED`,
   `FORGECRM_TELEGRAM_BOT_TOKEN`, `FORGECRM_TELEGRAM_WEBHOOK_SECRET`,
   `FORGECRM_TELEGRAM_WEBHOOK_ENABLED` и сквозной сценарий бота (сейчас `POST`
   на webhook отвечает `401` — это ожидаемое поведение без секрета; признак, что
   маршрут смонтирован).
6. **Миграции:** `/ready` = `200` означает, что применённая версия схемы не ниже
   ожидаемой бинарником (`migrate.Check` + `ExpectedVersion`). Для `623ac94`
   `ExpectedVersion = 33`, то есть миграции `0032_telegram_link_codes` и
   `0033_telegram_updates` применены. Точную версию и `schema_dirty` можно
   увидеть в `GET /api/v1/admin/diagnostics` (`schema_version`) под админом.
7. **Worker:** отдельного health-эндпоинта нет; признак живости —
   `worker_heartbeats` (метрика `workers_alive` в `/metrics`, но `/metrics` сейчас
   `503`, т.к. `FORGECRM_METRICS_TOKEN` не задан). Проверить через
   `/admin/diagnostics` (поле `jobs_by_status`) или логи части **worker** в
   панели blitz.
8. Опционально — защита ветки `main` обязательными проверками CI.

> Аудит read-only: код, ENV, deployment и git history не изменялись (единственная
> операция записи в репозитории — этот файл; `git fetch` обновил только
> remote-tracking ссылки).
