# Stage 0 — Аудит репозитория: честный baseline и оставшиеся пробелы

Дата: 2026-11-05
HEAD: `2f84e24` (`main`) — плюс незакоммиченная правка генератора OpenAPI, см. §1.3.
Метод: команды запускались заново в этой копии; статусы присваивались по записанному
результату, а не по заявлениям предыдущих отчётов.

Словарь статусов: `NOT_STARTED` · `IN_PROGRESS` · `IMPLEMENTED` · `TESTED` · `VERIFIED` ·
`BLOCKED`. `VERIFIED` требует записанного доказательства (команда + результат);
`IMPLEMENTED` — код есть и компилируется, но не покрыт проходящими тестами.

---

## 1. Baseline — что реально прогнано

### 1.1 Backend

| Проверка | Команда | Результат |
|---|---|---|
| Сборка | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Формат | `gofmt -l ./cmd ./internal` | пусто |
| Модульные тесты | `go test -race -count=1 ./...` | все пакеты `ok`, ни одной строки `FAIL` |
| Пакеты | `go list ./...` | 53 пакета, 41 с тестовыми файлами |
| Актуальность OpenAPI | `go run ./cmd/openapi-gen -check` | спецификация актуальна |
| Факты проекта | `go run ./cmd/facts-gen -repo .. -check` | `project-facts.md соответствует коду` |
| Документация | `./scripts/docs-check.sh` | `Документация согласована` |
| Интеграционные тесты | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration …` | проходят против живого PostgreSQL |

Интеграционный прогон подтверждён живьём, а не по документу: против PostgreSQL на
`127.0.0.1:5432` прошли `TestAgentApprovalExecutesOnce` (1.6с) и набор
`TestSimulation|TestMCP|TestRisk|TestForecast` (46с).

### 1.2 Frontend

| Проверка | Команда | Результат |
|---|---|---|
| Типы | `npm run typecheck` | чисто |
| Тесты | `npm run test` | 43 файла, 339 тестов — все прошли |
| Линт | `npm run lint` | 0 ошибок, 31 предупреждение |
| Сборка | `npm run build` | успешно |

### 1.3 Незакоммиченная правка генератора OpenAPI

Рабочее дерево этой копии **не чистое**: на момент аудита поверх `2f84e24` лежит
незакоммиченная правка генератора спецификации (7 файлов). Она сделана в этой же сессии,
покрыта тестами и зелёная, но не отправлена — то есть `main` её не содержит.

| Файл | Что изменено |
|---|---|
| `internal/openapi/openapi.go` | `setQueryParam` — параметр запроса из объявления маршрута **заменяет** одноимённый из общего контракта, а не добавляется рядом (иначе `q` печатался бы дважды) |
| `internal/openapi/schema.go` | `exportSegment` — имя компонента, выведенное из generic-типа, очищается до допустимых символов (`map[string]interface {}` → `MapStringInterface`) |
| `internal/api/contract.go` | у `contact_id`/`email` в `/consent/*` появилась схема (`uuid`/`email`) вместо `null` |
| `internal/openapi/openapi_test.go` | +4 теста: `TestQueryParametersAreTyped`, `TestComponentNamesAreLegal`, `TestEveryComponentRefResolves`, `TestNoDuplicateParameters` |
| `docs/api/openapi.json`, `docs/api/index.html` | перегенерированы генератором |
| `docs/verification/project-facts.md` | перегенерирован `facts-gen` |

Проверка на этом дереве: `go build`, `go vet`, `gofmt -l`, `go test ./...` — всё зелёное;
`openapi-gen -check` и `facts-gen -check` сообщают, что артефакты актуальны. То есть правка
консистентна и готова к отправке; незакрытым остаётся только её незакоммиченное состояние.

### 1.4 CI на опубликованном SHA

`gh run view` для HEAD-коммита `2f84e24`:

- `CI` (run 41939339225): **10/10 job'ов success** — Backend, Extraction, Constructor,
  Frontend, End-to-end, Visual regression, Security, Docker image, Blitz Cloud, Docs.
- `Portability` (run 41939339217): **6/6 job'ов success** — upgrade-path, backup-restore,
  compose-smoke, local-start-smoke, config-without-secrets, docs.

Итого **16/16 зелёных**. Примечание о дрейфе: `FINAL_RELEASE_CERTIFICATION.md` §G говорит
«CI 15/15». На текущем HEAD job'ов 16, потому что в `portability.yml` появилось задание
`local-start-smoke` («Isolated one-script local start»). Число устарело, утверждение
«CI зелёный на опубликованном SHA» — верно.

---

## 2. Сверка статусов дорожной карты

`docs/ROADMAP.md` — фактический счёт статусов (grep):

| Статус | Кол-во |
|---|---|
| VERIFIED | 130 |
| VERIFIED FIXED | 6 |
| IMPLEMENTED | 2 |
| IN_PROGRESS | 3 |
| NOT_STARTED | 10 |
| PARTIAL | 2 |
| DEFERRED | 1 |

Итого 154 пункта. `VERIFIED FIXED` — регрессионные исправления (7.2, 7.3, 7.4, 8.1, 8.5,
17.8), тоже закрытые. Ниже — то, что я проверил в коде, а не по надписи.

### 2.1 Подтверждено в коде

| Пункт | Заявление | Что найдено |
|---|---|---|
| 4.1 | «API диагностики» = VERIFIED | маршрут есть: `admin_handlers.go:79` `GET /admin/diagnostics` за `rbac.PermMetricsRead`; присутствует в `docs/api/openapi.json:1881` |
| 14.1 | «манифест рецепта» = VERIFIED | `internal/constructor/recipes/analytics-demo/recipe.yaml` существует |
| CSP | Report-Only | `config.go:443` `CSPEnforce` дефолт `false`; `httpx/headers_test.go` утверждает, что `Content-Security-Policy` не выставляется, а только `…-Report-Only` |
| HSTS | выключен по умолчанию | `config.go:439` `FORGECRM_HSTS_MAX_AGE` дефолт `0`; тест требует отсутствия заголовка при `max-age=0` |
| Лимитер | memory по умолчанию | `config.go:405` `FORGECRM_RATE_LIMIT_STORE` дефолт `"memory"`; конфиг отказывает, если задан Redis URL без переключения store |
| Поиск | деградация на 50k | `crm/contacts.go:108` — три ветки `ILIKE` через `OR` (`first_name`/`last_name`/`email::text`); `tsvector` в миграциях отсутствует, есть только `pg_trgm` (`0030_perf_trgm_indexes`) |
| CVE | устранены | `go.mod`: `filippo.io/edwards25519 v1.2.0`, `golang.org/x/crypto v0.58.1-0.2026…` |
| Сторожевые тесты | существуют | все 12 имён из AGENTS.md найдены ровно по одному разу: `TestWorkflowEventTriggerIsDeDuplicated`, `TestSeedDataMatchesThePermissionCatalog`, `TestTestHarnessWiresTheSameServicesAsProduction`, `TestDockerfilesOmitTheSyntaxDirective`, `TestComposeWorkerSelectsTheWorkerBinary`, `TestWebhookEventVocabularyMatchesTheDispatcher`, `TestExtractionBoundaryHolds`, `TestNoRouteIsInvented`, `TestNoOrphanComponents`, `TestAgentApprovalExecutesOnce`, `TestAgentDecisionRefusesChangedArguments`, `TestAgentExecutionRefusesForgedSignature` |
| Конструктор | 36 модулей | `scripts/extract-test/modules.txt` перечисляет 36 модулей; `internal/constructor/recipes/` содержит `analytics-demo` |
| Consent gate | в транспорте | `internal/notify/consent.go` — `ConsentGate` спрашивается транспортом перед отправкой |
| Webhook in-flight | учитывает `delivering` | `internal/api/webhook_replay_http_test.go:305` ставит `status='delivering'` и требует отказа повтора |

### 2.2 Дрейф документации (не код)

Это расхождения текста, а не дефекты поведения. Ни одно не является P0.

| # | Где | Что устарело | Верно сейчас |
|---|---|---|---|
| D-1 | `docs/ROADMAP.md:428` (1.1) | «144 операции, 111 путей» | `openapi.json` = 182 пути, 236 операций (`docs-check.sh`) |
| D-2 | `docs/audit/FINAL_RELEASE_CERTIFICATION.md` §G | «CI 15/15» | 16/16 на HEAD |
| D-3 | `docs/audit/FINAL_RELEASE_CERTIFICATION.md` §F.8 | «20 предупреждений линта фронтенда» | 31 предупреждение, 0 ошибок |
| D-4 | `docs/telegram/PHASE2_STATUS.md:20` | «Миграции 0032 и 0033 … в production не применялись» | миграции существуют в дереве (`0032_telegram_link_codes`, `0033_telegram_updates`) и применены в проверенных средах; заметка осталась с момента подготовки фазы 2 |
| D-5 | `docs/audit/FINAL_RELEASE_CERTIFICATION.md` §G | «39+43 пакета backend» | 53 пакета, 41 с тестами |

Замечание: D-2/D-3/D-5 — снимок состояния на момент сертификации, а не ложь. Их стоит
обновить, потому что читатель принимает числа за текущие.

### 2.3 Два release-blocker из прошлого шага — закрыты как «не блокеры»

- **B-1 (устаревший commit в `/ready`).** В этой копии API-процесс не запущен, поэтому
  `8d350b0` vs `cc65d3f` здесь не воспроизводится. Природа ясна из кода: значение
  приходит из `FORGECRM_COMMIT` (`config.go`), а не из `git` в рантайме, поэтому это
  косметическая инъекция переменной в деплой, не дефект сборки. Статус: **косметика, не
  блокер**.
- **B-2 (дрейф доков про Telegram).** Подтверждён как дрейф текста (D-4). Код и миграции
  на месте; расходится только заметка. Статус: **дрейф документации, не блокер**.

---

## 3. Проверка «страшных предупреждений» из аудита

| Предупреждение | Проверка | Вывод |
|---|---|---|
| Поиск деградирует до полного скана на 50k | форма запроса `ILIKE`/`OR` подтверждена в коде; `tsvector` нет | **Подтверждено как записанное ограничение.** Не блокер, но и не решено |
| Лимитер на процесс по умолчанию | `config.go:405` = `memory` | **Подтверждено.** Осознанный дефолт, документирован |
| CSP не блокирует | `CSPEnforce=false` | **Подтверждено.** Report-Only, документировано |
| HSTS выключен | `max-age=0` | **Подтверждено.** Включается за TLS |
| Защита ветки `main` не включена | это настройка GitHub, не код | **Вне репозитория**, действие владельца |
| Нет дашбордов/SLO | подтверждено отсутствие | **Подтверждено.** Операционная, не кодовая готовность |
| Внешняя доставка не проверена | нет credentials в среде | **Подтверждено как NOT VERIFIED** |

Ни одно предупреждение не оказалось преувеличенным, и ни одно не оказалось скрытым
дефектом кода: все семь — либо записанные ограничения, либо внешние/эксплуатационные.

---

## 4. Оставшиеся пробелы (не-VERIFIED пункты дорожной карты)

### 4.1 NOT_STARTED (10) — честно отсутствует

| # | Пункт |
|---|---|
| 2.9 | Адаптеры MSSQL / Oracle |
| 2.10 | Адаптер CDC |
| 3.4 | Альтернативные почтовые провайдеры |
| 6.6 | Адаптер LDAP / Active Directory (нужен подтверждённый спрос) |
| 2.5.5 | Circuit Breaker / Timeout Budget / Retry Budget |
| 2.5.6 | Failure Injection Dashboard |
| 4.1 | Runtime агента, бюджеты, область (Фаза 4 — агентная система) |
| 5.2 | Bootstrap организации |
| 5.3 | Кастомные объекты |
| 5.4 | SDK и CLI |

Вне этих десяти в дорожной карте есть один `DEFERRED` — 2.8 (MySQL/MariaDB как хранилище):
осознанно отложен, а не забыт.

### 4.2 IMPLEMENTED (2) — код есть, автотестом не закрыто

| # | Пункт | Почему не VERIFIED |
|---|---|---|
| 1.3 | Валидация OpenAPI в CI | гейт `openapi-gen -check` идёт в задании бэкенда и зелёный локально; пункт помечен `IMPLEMENTED`, потому что это шаг CI, а не покрытие поведения |
| 3.3 | Адаптер бота Telegram | требуется реальный токен бота для сквозного доказательства |

### 4.3 IN_PROGRESS (3) / PARTIAL (2)

| # | Пункт | Состояние |
|---|---|---|
| 1.13 | Тесты: модульные/интеграционные/API/безопасность | IN_PROGRESS — часть областей проверяется вручную |
| 6.2 | Браузерные E2E | IN_PROGRESS — есть 4 спеки, но не все сценарии покрыты |
| 2.5.4 | Authorization Policy Engine (BOLA/IDOR как контракт) | VERIFIED — единое правило `objectOwnerPredicate` применяется списком, чтением по id, поиском и записями; same-tenant IDOR закрыт, тесты в `testsupport/object_authz_test.go` |
| 2.2 | Локальный тестовый IdP (Keycloak) | PARTIAL — поток проверен против in-process провайдера, общий Keycloak для E2E открыт |
| 3.5 | Контекстный ИИ, калиброванная уверенность, корректный сбой, передача человеку | PARTIAL — всё реализовано (включая стриминг и `confidence.go`), но «калиброванная уверенность» — вычисляемый серверный балл обоснованности, а не калиброванная вероятность |

### 4.4 NOT VERIFIED (внешние)

- Внешняя доставка вебхуков/Telegram/email к реальным сервисам (нужны credentials).
- Производительность под конкурентной нагрузкой и реальным профилем трафика.
- Поведение production-развёртывания (публичный деплой недоступен).
- Дашборды и подтверждённые данными SLO.

---

## 5. Наблюдение по покрытию тестами (не заявлено в дорожной карте)

12 пакетов без собственных тестовых файлов:

```
cmd/forgecrm  cmd/forgecrm-worker  cmd/openapi-gen
internal/bootstrap  internal/events  internal/forecast
internal/integrations  internal/mcpcred  internal/migrations
internal/risk  internal/simulate  internal/wiring
```

Это **не** значит «не покрыто»: `internal/testsupport` прогоняет интеграционные сценарии
для `risk`, `simulate` и `mcpcred` (`intelligence_test.go`, `simulation_test.go`,
`mcp_credentials_test.go`), и эти тесты проходят. Пробел узкий: у самих пакетов нет
модульных тестов, поэтому их поведение доказывается только сквозь HTTP-путь. Наиболее
заметны `internal/wiring` (проводка сервисов — её стерегут статические тесты в
`cmd/forgecrm-api`, что частично закрывает риск) и `internal/integrations` (защита SSRF).

---

## 6. Вердикт

**Baseline честный и зелёный.** Сборка, vet, формат, модульные тесты, интеграционные
тесты против живого PostgreSQL, OpenAPI-гейт, docs-гейт, факты проекта, фронтенд
(typecheck/339 тестов/сборка) и CI 16/16 — всё проходит на `2f84e24`. Плюс поверх него
лежит зелёная, но незакоммиченная правка генератора OpenAPI (§1.3): она проверена на этом
дереве, но `main` её не содержит.

Код соответствует 136 закрытым пунктам (`VERIFIED` 130 + `VERIFIED FIXED` 6); я не нашёл
пункта, помеченного `VERIFIED`, который на деле не имел бы реализации. Единственные
расхождения — устаревшие числа в тексте документации (§2.2), они не влияют на поведение.

Реальные оставшиеся пробелы — это 10 `NOT_STARTED` (§4.1), 1 осознанно `DEFERRED` (2.8),
2 `IMPLEMENTED` без автотеста (§4.2), 5 частично готовых — 3 `IN_PROGRESS` + 2 `PARTIAL`
(§4.3) и 4 внешних `NOT VERIFIED` (§4.4). Ни одного P0. Единственный инфраструктурный
блокер публичного релиза — **защита ветки `main`**, который является действием владельца,
а не изменением в репозитории.

Рекомендуемые следующие шаги (в порядке отдачи):

1. Обновить числа в текстах: D-1, D-2, D-3, D-5 (§2.2) — дёшево, убирает ложное
   впечатление о состоянии.
2. Обновить `docs/telegram/PHASE2_STATUS.md` (D-4) — либо пометить как исторический
   снимок, либо снять утверждение «не применялись».
3. Включить защиту `main` с обязательными проверками CI (владелец).
4. Закрыть 2.5.4 (единый контракт объектной авторизации) — сделано: правило
   вынесено в `objectOwnerPredicate` и применено к чтению по id, поиску и записям,
   доказано `testsupport/object_authz_test.go`.
