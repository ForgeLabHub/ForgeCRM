# Аудит Portability после merge и состояния deployment

Дата аудита: 4 ноября 2026 г. Все времена запусков ниже — UTC. Аудит выполнен без изменения кода, конфигурации, истории Git и production. Этот документ — единственный новый файл в рабочем дереве.

## Краткий вывод

- Удалённый `ForgeLabHub/ForgeCRM` `main` указывает на ожидаемый merge-коммит `623ac949e664149d13a8639502dfb807354deceb`; PR #49 объединён. Дерево было чистым до создания отчёта.
- [Portability run 41867925002](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867925002) на этом SHA завершился ошибкой в двух независимых jobs. Первый job получил HTTP/2 `INTERNAL_ERROR` при загрузке `github.com/redis/go-redis/v9@v9.23.0` через Go proxy. Это подтверждённая ошибка загрузки; её временный характер вероятен, но не доказан повтором именно неудачного запуска.
- Второй job завершился на `docker compose up -d --build`, код шага 1. Детальный лог недоступен из этой среды; конкретная внутренняя команда и причина **не установлены**. Нельзя утверждать, что он упал по той же причине, что первый.
- Успешный [предшествующий Portability run 41863533491](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41863533491) проверял **в точности то же Git-дерево**, хотя и другой commit SHA. Это аргумент против детерминированной регрессии дерева, но не исключает недетерминированной ошибки, различий окружения или влияния контекста коммита.
- Текущий deployment SHA не подтверждён. Согласно предыдущему наблюдению и сообщению пользователя, сайт показывал страницу Blitz «ForgeCRM is waking up». В рамках этого аудита новые HTTP-запросы к сайту, `/health` или `/ready` намеренно не выполнялись: запрос к спящему приложению может инициировать его пробуждение, а пользователь прямо запретил попытки пробуждения. Поэтому текущие HTTP-коды, тела и SHA неизвестны.

## 1. Идентичность репозитория и состояние дерева

| Проверка | Результат |
|---|---|
| Remote `origin` | `https://github.com/ForgeLabHub/ForgeCRM.git` |
| Репозиторий и default branch по GitHub API | `ForgeLabHub/ForgeCRM`, `main` |
| Remote `main` по GitHub API | `623ac949e664149d13a8639502dfb807354deceb` |
| Локальный `refs/remotes/origin/main` | `623ac949e664149d13a8639502dfb807354deceb` |
| Ожидаемый SHA относительно remote `main` | GitHub compare: `identical`, ahead 0, behind 0; это и есть текущий `main` |
| Текущая ветка | `arena/8ac70b5e-forgecrm` |
| Локальный HEAD | `623ac949e664149d13a8639502dfb807354deceb` |
| `git status --porcelain` до создания документа | пусто; checkout чистый |
| Checkout | shallow; родители сравнивались через GitHub API, а не предположением о локальной истории |
| [PR #49](https://github.com/ForgeLabHub/ForgeCRM/pull/49) | `MERGED`, head `8ecb7a30b99b1b2b819d97c364b41332fb2cfcac`, merge `623ac949e664149d13a8639502dfb807354deceb` |

У merge-коммита родители: `76a5298e4d636e22ec9fa3c53c17930aaac0e5d3` (предыдущий main) и `8ecb7a30b99b1b2b819d97c364b41332fb2cfcac` (head PR). GitHub Git API сообщает одинаковый tree object `0c46f4287ee983bceed5deeef21baa30d00509f8` для head PR и merge-коммита. Значит, результат merge имеет те же файлы и содержимое, что проверенный head PR; это не означает идентичность commit metadata или runtime окружения.

Метаданные получены чтением GitHub API и локальных ссылок; fetch не понадобился. Reset, rebase, cherry-pick, merge, push и изменения production не выполнялись.

## 2. Portability: все jobs и шаги

Запуск `41867925002`, attempt 1, `push` в `main`, SHA `623ac949…`, 2026-11-04 16:30:36–16:32:07 UTC: общий результат `failure`. Таблица основана на GitHub Actions Jobs API. Условные post/cleanup-шаги показаны отдельно от функциональных шагов.

| Job | Результат | Шаги |
|---|---|---|
| Upgrade an existing database | success | Set up job; Initialize containers; checkout; setup-go; Set up the previous release; Migrate with the previous version; Migrate forward with the current version; Start the current API on the upgraded schema — все success. Post setup-go, post checkout, Stop containers, Complete job — success. |
| Backup, restore and smoke test | success | Set up job; Initialize containers; checkout; setup-go; Prepare a populated database; Back up; Restore into a fresh database; Smoke test the restored instance — все success. Post setup-go, post checkout, Stop containers, Complete job — success. |
| Runs with no optional configuration | **failure** | Set up job; Initialize containers; checkout; setup-go — success. **Build without secrets — failure**. Run with only the mandatory variables и Reject contradictory configuration — skipped; post setup-go — skipped; post checkout, Stop containers, Complete job — success. |
| Full stack (Compose) smoke test | **failure** | Set up job; checkout; Prepare the environment — success. **Build and start the stack — failure**. Apply migrations и Smoke test — skipped; Logs on failure, post checkout, Complete job — success. Успешный `Logs on failure` означает только успешное завершение этого шага, а не доступность его содержимого в аудите. |
| Documentation is present and linked | success | Set up job; checkout; Check documentation; Check forge.sh commands; post checkout; Complete job — все success. |

Работа jobs независима; порядок строк таблицы не означает причинную связь.

### 2.1. `Runs with no optional configuration`: Go proxy

- Job ID `126706903800`, шаг `Build without secrets`, шаг 5. Команда из workflow: `cd backend && go build -o /tmp/api ./cmd/forgecrm-api`.
- Аннотация GitHub сообщает `Process completed with exit code 1`. Это код **шага**, а не отдельно подтверждённый код конкретной внутренней сетевой операции.
- Другая failure-аннотация содержит фактическую ошибку:

  ```text
  github.com/redis/go-redis/v9@v9.23.0: read "https://proxy.golang.org/github.com/redis/go-redis/v9/@v/v9.23.0.zip": stream error: stream ID 43; INTERNAL_ERROR; received from peer
  ```

- Это HTTP/2 stream `INTERNAL_ERROR` при чтении zip от Go proxy. Аннотация не содержит HTTP 404, 429 или ошибки проверки суммы. Она не устанавливает, какая именно сторона вызвала сбой, был ли модуль недоступен постоянно, имело ли место rate limiting, или был ли затронут sumdb.
- Подтверждено: тег GitHub `redis/go-redis` `v9.23.0` существует, указывает на commit `6f2b2639e563b11709e06f42bc0addbfb5a1abb6`, а корневой `go.mod` объявляет `module github.com/redis/go-redis/v9`. Это подтверждает доступность исходного тега на GitHub, но не доступность zip у Go proxy в момент сбоя.
- Во всех трёх ревизиях — оба родителя и merge — в `backend/go.mod` указан `github.com/redis/go-redis/v9 v9.23.0`, а соответствующие две записи `go.sum` одинаковы. Из архива тега GitHub независимо вычислены `h1` для корневого `go.mod` и канонический tree hash для файлов модуля (463 файла, без вложенных модулей, vendor и VCS): оба совпали с записями `go.sum`. Это проверка согласованности с исходным тегом, **не** проверка ответа proxy или sumdb на GitHub runner во время сбоя.
- Успешный pre-merge запуск с тем же Git-деревом и модулем показывает, что сборка может проходить. Версия о временном сетевом/прокси-сбое правдоподобна; воспроизводимость в момент сбоя и результат повтора не установлены. Версии зависимостей не менялись и повторный запуск CI не выполнялся.

### 2.2. `Full stack (Compose) smoke test`: отдельная неустановленная причина

- Job ID `126706904468`, шаг `Build and start the stack`, шаг 4. Единственная команда шага по workflow: `docker compose up -d --build`.
- GitHub сообщает `Process completed with exit code 1` для шага. По доступным метаданным неизвестно, какой подшаг Docker/Compose завершился ошибкой, какой текст ошибки был в выводе, и был ли какой-либо сетевой запрос причиной. `Apply migrations` и `Smoke test` не выполнялись.
- На основании только аннотации нельзя установить точную независимую root cause, локализовать сбой до конкретного `RUN` в Dockerfile или утверждать совпадение с проблемой Redis proxy. Возможны сетевой сбой, ошибка сборки/контейнера или другие причины; это **гипотезы**, а не вывод.
- Оба полного журнала и отдельно журналы каждого из двух jobs запрошены через `gh run view --log` и GitHub Jobs API. Ответы GitHub перенаправляют загрузку на внешний Azure Blob host, недоступный из разрешённой сети этой среды; клиент получает EOF. Поэтому полный контекст вывода и журналы шага `Logs on failure` прочитать не удалось. Адреса с временными подписанными параметрами и какие-либо credentials в отчёт не включены.

### 2.3. Runner, версии и cache

- Для обоих jobs workflow задаёт `runs-on: ubuntu-latest`; Jobs API указывает соответствующую label и GitHub Actions runner group. Точная версия образа Ubuntu и фактическая архитектура runner не подтверждены без setup-лога. Не следует подменять их архитектурой локального sandbox.
- Для job Go-сборки workflow задаёт `GO_VERSION: 1.26.10`, использует `actions/setup-go@v5` с `cache-dependency-path: backend/go.sum`; `backend/go.mod` также объявляет `go 1.26.10`. Это конфигурация; реальный вывод `go version`, точный cache key, hit/miss и состояние module cache в момент сбоя из недоступных логов не извлечены.
- Compose job использует корневой Dockerfile, где Go stage указан как `golang:1.26.10-alpine` и выполняется `go mod download`, но без журнала нельзя утверждать, что сбой произошёл на этом шаге. Его фактические архитектура, версия Docker/Compose, cache и настройки сети неизвестны.
- В workflow нет явных значений `GOPROXY`, `GOSUMDB`, `GOMODCACHE` или `GOCACHE`. Действовавшие значения окружения runner и проверки sumdb неизвестны. Фактический URL первого сбоя виден в аннотации выше.

## 3. Сравнение запусков и версий

| Запуск | Коммит / событие | Результат | Значение |
|---|---|---|---|
| [41867925002](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867925002) | `623ac949…`, push main | failure | Два job отказа, раздельные причины установлены не полностью. |
| [41863533491](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41863533491) | `8ecb7a30…`, PR | success | Все пять jobs, включая оба впоследствии упавших, успешны; tree hash совпадает с merge. |
| [41863525994](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41863525994) | `8ecb7a30…`, push ветки | success | Все пять jobs успешны на том же tree. |
| [41860209869](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41860209869) | `76a5298…`, push main | success | Базовый parent до объединения тоже проходил Portability. |
| [41867924938](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41867924938) | `623ac949…`, push main, основной CI | success | Все jobs основного CI успешны; это не заменяет неудавшиеся Portability jobs. |

Файл `.github/workflows/portability.yml` в обоих родителях и merge имеет один Git blob SHA `d6201b0e9211144fcbdc23d36d0bc05245f1fb3b`. Таким образом, сценарий workflow не менялся между проверенными SHA. `go.mod` в PR менялся, но указанная версия Redis и обе его записи `go.sum` сохранились. Идентичное tree плюс два успешных предыдущих запуска не подтверждают детерминированную регрессию PR #49. Для Go proxy подтверждён сетевой/transport-симптом; для Compose недостаточно данных, чтобы классифицировать причину как инфраструктурную, flaky или регрессию.

## 4. Deployment: граница read-only проверки

Проверяемый адрес: `https://forgecrm.forgecrm.prod.page/login`; диагностические пути — `/health` и `/ready`. Предыдущий ответ в контексте диалога и сообщение пользователя указывали на страницу Blitz «ForgeCRM is waking up», а не ответ приложения. Инструмент предыдущего наблюдения не предоставил пригодный для этого аудита фактический HTTP status code или commit SHA.

При текущем запрете «не пытаться пробудить» дополнительный HTTP HEAD/GET может противоречить требованию: платформа может пробуждать приложение при любом входящем запросе. Поэтому во время этого аудита такие запросы **не отправлялись**, не выполнялись авторизация, restart, deployment, миграции или доступ к production DB. Текущая доступность, HTTP status codes, тела `/health` и `/ready`, а также фактически развёрнутый SHA **неизвестны**. Состояние GitHub не является доказательством deployment.

## 5. Минимальные следующие действия

1. В GitHub UI открыть полный лог обоих jobs запуска `41867925002`, особенно `Build and start the stack` и `Logs on failure`; установить точный внутренний вызов, ошибку, runner image/архитектуру и cache hit/miss. Не копировать секреты или подписанные ссылки в отчёты.
2. При отдельном разрешении повторить failed jobs на том же SHA в GitHub Actions и сопоставить результат и логи. Успех повтора подтвердит единичный характер наблюдения, но сам по себе не докажет конкретную причину Compose. Для rerun ранее отсутствовали необходимые права GitHub integration; при отказе потребуется восстановить права, а не менять версии зависимостей.
3. В согласованное окно оператору, которому разрешено пробуждение приложения, проверить публичные HTTP status/body `/health` и `/ready`, извлечь фактический reported commit и сравнить с `623ac949…`; отдельно проверить worker и webhook. До этого deployment identity остаётся непроверенной.
4. Любые исправления, развёртывание, регистрация webhook или миграции вынести за рамки этого read-only аудита и выполнять только после отдельного разрешения.
