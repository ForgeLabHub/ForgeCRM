# ForgeCRM

Self-hosted, open-source CRM platform for corporate data, integrations and automation.

ForgeCRM is one stable foundation for many business solutions: sales and customer
management, support, data migration from existing CRMs, ERP and API integration,
event-driven automation, and controlled AI assistance.

The platform runs on its own, with no AI provider, no external API and no optional
integration enabled. Every integration is opt-in and configurable.

## Status

Under active development on `main`. The backend,
database schema, Import Center and the web interface are implemented and verified
against a real PostgreSQL instance.

Verified (see [docs/PROJECT_AUDIT.md](docs/PROJECT_AUDIT.md) for evidence):

| Area | State |
| --- | --- |
| Go API, migrations, PostgreSQL schema | Working, 16 migrations applied |
| Authentication, sessions, CSRF, TOTP | Working |
| RBAC: 87 permissions in the catalog, 14 system roles | Working, enforcement verified |
| Audit log and security events | Working |
| CRM entities (contacts, accounts, deals, tasks, tickets, ...) | Working |
| Import Center: CSV, TSV, JSON, JSONL, XLSX, XML, archives | Working |
| Background jobs and transactional outbox | Working |
| Web interface: sign-in, CRM, Import Center, administration | Working, verified in a browser |
| Docker Compose deployment | Verified end to end |
| AI & MCP | Provider abstraction (Ollama, OpenAI-compatible), MCP JSON-RPC transport, contextual AI on account, contact, deal, ticket and task records, AI & MCP admin page |
| Agent approvals (human-in-the-loop) | Working, second-factor step-up for destructive actions |
| Workflow automation (event, schedule, manual triggers) | Working, verified against PostgreSQL; template catalog, import/export and a structured editor |
| File attachments (S3-compatible) | Working; provider-agnostic object store, metadata in PostgreSQL, per-route permissions |
| Retention & rotation | Working; opt-in per-table windows, dry-run, batched, audit-protected |
| Constructor (recipe -> standalone project) | Working, verified end to end; the `analytics-demo` recipe generates a project that builds, tests and runs; a CI job regenerates and runs it |

The web interface covers sign-in with second factor, the CRM workspace, the Import
Center including the mapping wizard, integrations, events and webhooks, background
jobs, agent approvals, and the administration pages for users, roles, organizations,
teams, email, Telegram, audit, security and diagnostics.

## Quick start

Requires Docker with the Compose plugin.

```bash
git clone https://github.com/ForgeLabHub/ForgeCRM.git
cd ForgeCRM

cp .env.example .env
# Generate the key that encrypts stored integration credentials.
# Losing it makes those credentials unrecoverable.
printf 'FORGECRM_SECRET_KEY=%s\n' "$(openssl rand -base64 48)" >> .env

docker compose up -d
```

Create the first administrator. The password is passed through the environment so it
never appears in the process list or shell history:

```bash
FORGECRM_BOOTSTRAP_PASSWORD='<a strong password>' \
  docker compose run --rm --no-deps -e FORGECRM_BOOTSTRAP_PASSWORD \
  api -bootstrap-admin -bootstrap-email you@example.com
```

Confirm the instance is healthy:

```bash
./scripts/smoke-test.sh http://localhost:8080

FORGECRM_SMOKE_EMAIL=you@example.com FORGECRM_SMOKE_PASSWORD='<your password>' \
  ./scripts/smoke-test.sh http://localhost:8080
```

Or run `./scripts/setup.sh you@example.com`, which performs these steps and checks the
host environment first.

## Commands

Everything below has a stable entry point; nothing requires remembering the
underlying tool invocation.

| Command | What it does |
| --- | --- |
| `make doctor` | check tools, pinned versions and services; changes nothing |
| `make build` | build API, worker and frontend artefacts |
| `make test` | unit tests, no database required |
| `make test-integration` | tests against a real PostgreSQL |
| `make test-e2e` | Playwright against a real API and database |
| `make openapi-check` | fail when the API specification drifts from the code |
| `make compose-up` / `make compose-down` | start / stop the stack (data is kept) |
| `make smoke-test` | verify a running instance |
| `make verify` | lint, tests, OpenAPI and documentation checks locally |
| `make docs-check` | documentation completeness, links and module specs |

The full matrix of what each check proves, and which CI job runs it, is
[docs/verification/matrix.md](docs/verification/matrix.md).

## Documentation

Start at [docs/index.md](docs/index.md) - it maps every question to the document
that answers it.

- [Build from a clean machine](BUILD.md) - pinned versions, build levels A-E
- [Project audit](docs/PROJECT_AUDIT.md) - verified state, findings, risks
- [Portability audit](docs/audit/portability-audit-2026-10-29.md) - gaps found and closed
- [Verification matrix](docs/verification/matrix.md) - claim -> command -> CI job
- [Architecture](docs/ARCHITECTURE.md) - module boundaries and data flow
- [Constructor](docs/CONSTRUCTOR.md) - build a standalone project from a recipe
- [Workflow automation](docs/architecture/workflows.md) - triggers, steps, execution model
- [Roadmap](docs/ROADMAP.md) - staged plan
- [Acceptance criteria](docs/ACCEPTANCE_CRITERIA.md) - definition of done
- [Frontend development](docs/development/frontend.md) - stack, layout, checks
- [Deployment](docs/deployment/) - configuration and production checklist
- [Operations](docs/operations/) - backup, restore, troubleshooting

## Repository layout

```
backend/            Go API and worker
  cmd/              forgecrm-api, forgecrm-worker, forgecrm (constructor CLI)
  internal/
    api/            HTTP handlers, routing, middleware
    authn/          passwords, sessions, TOTP, OIDC
    rbac/           permission catalog and enforcement
    crm/            CRM domain: contacts, deals, tasks, tickets, search
    importx/        Import Center: parsers, safe extraction, execution
    importx/safe/   untrusted-archive handling
    integrations/   adapter lifecycle and SSRF-guarded outbound HTTP
    events/         event gateway and webhooks
    outbox/         transactional outbox
    jobs/           background job queue
    audit/          audit and security event logging
    notify/         email and Telegram
    ai/             model provider abstraction
    constructor/    recipes that build standalone projects; embeds recipes/
    migrate/        schema migrations
    database/       connection pool and transactions
frontend/           React + TypeScript web interface (Vite)
  src/api/          one module per backend area, plus the HTTP client
  src/components/   shell, table list, permission gate, primitives
  src/pages/        one file per route; admin pages in pages/admin/
deploy/             reverse proxy configuration
docs/               architecture, security, operations
scripts/            setup, migrate, backup, restore, smoke test
```

## Development

```bash
cd backend
go build ./...
go vet ./...
go test ./...
gofmt -l ./cmd ./internal    # must print nothing
```

The constructor turns a recipe from `internal/constructor` into a standalone
project on disk. `analytics-demo` lifts the pure analytics core into a project
with no database, no service and no secrets:

```bash
cd backend
go run ./cmd/forgecrm recipes
go run ./cmd/forgecrm scaffold analytics-demo --output /tmp/demo
cd /tmp/demo && go test ./... && go run ./cmd/demo
```

Integration tests run against a real database:

```bash
FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable' \
  go test -tags=integration ./...
```

The frontend has its own checks:

```bash
cd frontend
npm ci
npm run typecheck && npm run lint && npm run test && npm run build
```

## Known limitations

Stated explicitly so nobody assumes more than the project provides:

- **PostgreSQL 17 is the only supported database.** MySQL/MariaDB are not supported
  (see [ADR 0008](docs/adr/0008-mysql-mariadb-datastore-blockers.md)).
- **No license file yet.** Until one is chosen the default applies and the code is
  not licensed for redistribution - an owner decision, tracked in
  [CONTRIBUTING.md](CONTRIBUTING.md).
- **No horizontal scaling without Redis**: the in-memory rate limiter is per process
  (see [ADR 0003](docs/adr/0003-multi-replica-safety-detour.md)).
- **Schema migrations do not roll back automatically**; the recovery path is a
  verified backup ([docs/database/backup-restore.md](docs/database/backup-restore.md)).
- **No distributed tracing**: `request_id` covers the request, not the async path.
- **AI output is not guaranteed**; approvals and audit are the compensating controls.
- **No load tests and no numeric SLOs** are claimed.

## License

No license file is present yet. The distribution model is an owner decision; see
[CONTRIBUTING.md](CONTRIBUTING.md). Until a license is chosen, the default applies and
the code is not licensed for redistribution.

## Управление проектом: `forge.sh` и `forge.bat`

Один скрипт на Unix-подобные системы (`forge.sh`) и один на Windows
(`forge.bat`). Интерфейс одинаковый: те же команды, та же логика, одна
реализация на каждой платформе — без bash-скриптов внутри Windows.

```bash
./forge.sh              # интерактивное меню
./forge.sh doctor       # проверить окружение, ничего не меняя
./forge.sh install      # поставить зависимости
./forge.sh start        # запустить в фоне, дождаться готовности, открыть браузер
./forge.sh status       # что запущено и отвечает ли порт
./forge.sh logs api -f  # логи в реальном времени
./forge.sh test         # тесты
./forge.sh stop         # остановить
```

```bat
forge.bat
forge.bat start
forge.bat status
```

### Что умеет

| Команда | Действие |
| --- | --- |
| `status` | состояние по портам и HTTP, а не «на глаз» |
| `start [--fg]` | фон (PID-файлы в `.run/`) или передний план |
| `stop` | останавливает дерево процессов целиком, порт освобождается |
| `restart`, `logs [имя] [-f]`, `open` | обычные операции |
| `doctor` | инструменты, версии, `.env`, `.env` в git, занятость порта |
| `install`, `test`, `lint`, `build`, `verify` | через `make`, если он есть |
| `migrate` | применить миграции |

| `db migrate\|backup\|restore` | база: миграции, копия, восстановление |
| `docker up\|down\|logs\|reset` | контейнеры (`down` данные не удаляет) |
| `clean [--hard]` | артефакты и логи; `--hard` ещё `.venv` и `node_modules` |

### Правила, заложенные в скрипт

- **Ничего не меняет без команды.** `doctor` и `status` только читают.
- **Данные не удаляются.** `stop` и `docker down` сохраняют базу; удаление —
  только `--hard` или явное подтверждение в `docker reset`.
- **Предполётная проверка.** Если нет `go`/python или занят порт — скрипт
  говорит об этом сразу, а не через две минуты таймаута.
- **Не висит после падения.** Если процесс умер, запуск прекращается и
  показывает последние строки лога.
- **Venv главнее системы.** Если есть `.venv`, им пользуются `make`, `pytest`
  и uvicorn — чтобы `install` и `test` не расходились.

Текущий способ запуска без скрипта: `cd backend && go run ./cmd/forgecrm-api` — адрес `http://localhost:8080/healthz`.

### Конфигурация: `.env`

Скрипт читает `.env` **собственным парсером**, а не через `source`: файл
приходит из репозитория, а выполнять из него произвольный код нельзя.
Поддержаны комментарии `#`, пустые строки, префикс `export`, одинарные и
двойные кавычки, CRLF, пробелы вокруг `=` и комментарий после значения.

Приоритет значений: **переменная окружения → `.env` → встроенное значение**.
Поэтому `PORT=9000 ./forge.sh start` работает независимо от файла.

| Команда | Действие |
| --- | --- |
| `env` / `env show` | переменные, источник каждого значения и замаскированные секреты |
| `env init [--force]` | создать `.env` из `.env.example`: заглушки секретов заменяются случайными, права `600`, прежний файл — в `.env.bak` |
| `env set KEY=VALUE` | записать или заменить одну переменную (атомарно, через временный файл) |
| `env check` | сверка с `.env.example`: пропавшие переменные, пустые и «примерные» секреты, `.env` в `.gitignore` |
| `env edit` | открыть файл в `$EDITOR` (fallback: nano → vim → vi) |
| `env path`, `env reload` | путь к файлу и перечитывание без перезапуска |

Секреты в выводе маскируются: `ADMIN_CODE=ch…de (31 симв.)`, пароль внутри
строки подключения — `postgres://user:***@host`. Имена вида `*_MINUTES`,
`*_URL`, `*_PORT` секретами не считаются: маскировать число минут бессмысленно.

Путь к файлу переопределяется: `FORGE_ENV_FILE=/etc/app.env ./forge.sh status`.
В Windows-версии те же команды: `forge.bat env init`, `forge.bat env check`.
Ограничение `cmd.exe`: значения, содержащие `%` и `!`, не поддерживаются.

### Флаги и автодополнение

```bash
./forge.sh -h | --help            # справка
./forge.sh -V | --version         # версия скрипта, коммит, состояние .env
./forge.sh -q status              # молчать об успехах
./forge.sh --no-color status      # без ANSI (то же делает NO_COLOR=1)
./forge.sh --json status          # машинный вывод для скриптов и мониторинга

eval "$(./forge.sh completion bash)"   # автодополнение для bash
./forge.sh completion zsh > ~/.zfunc/_forge   # для zsh
```

`test`, `lint`, `build` и `verify` запускаются в окружении **без** переменных
из `.env`: иначе локальные проверки вели бы себя иначе, чем CI, и тесты вида
«без ключа должен быть fallback» перестали бы что-либо проверять.

### Запуск в этом проекте

```bash
./forge.sh install && ./forge.sh start
```
