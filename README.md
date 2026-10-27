# ForgeCRM

Self-hosted, open-source CRM platform for corporate data, integrations and automation.

ForgeCRM is one stable foundation for many business solutions: sales and customer
management, support, data migration from existing CRMs, ERP and API integration,
event-driven automation, and controlled AI assistance.

The platform runs on its own, with no AI provider, no external API and no optional
integration enabled. Every integration is opt-in and configurable.

## Status

Under active development on the `feat/platform-foundation` branch. The backend,
database schema and Import Center are implemented and verified against a real
PostgreSQL instance. The web administration panel is not built yet; the API is the
current interface.

Verified today (see [docs/PROJECT_AUDIT.md](docs/PROJECT_AUDIT.md) for evidence):

| Area | State |
| --- | --- |
| Go API, migrations, PostgreSQL schema | Working, 5 migrations applied |
| Authentication, sessions, CSRF, TOTP | Working |
| RBAC: 80 permissions, 28 role records | Working, enforcement verified |
| Audit log and security events | Working |
| CRM entities (contacts, accounts, deals, tasks, tickets, ...) | Working |
| Import Center: CSV, TSV, JSON, JSONL, XLSX, XML, archives | Working |
| Background jobs and transactional outbox | Working |
| Docker Compose deployment | Verified end to end |
| Web administration panel | Not started |
| AI and MCP layer | Backend only, no UI |

## Quick start

Requires Docker with the Compose plugin.

```bash
git clone https://github.com/geff1778/ForgeCRM.git
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

## Documentation

- [Project audit](docs/PROJECT_AUDIT.md) - verified state, findings, risks
- [Architecture](docs/ARCHITECTURE.md) - module boundaries and data flow
- [Roadmap](docs/ROADMAP.md) - staged plan
- [Acceptance criteria](docs/ACCEPTANCE_CRITERIA.md) - definition of done
- [Deployment](docs/deployment/) - configuration and production checklist
- [Operations](docs/operations/) - backup, restore, troubleshooting

## Repository layout

```
backend/            Go API and worker
  cmd/              forgecrm-api, forgecrm-worker
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
    migrate/        schema migrations
    database/       connection pool and transactions
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

Integration tests run against a real database:

```bash
FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable' \
  go test -tags=integration ./...
```

## License

No license file is present yet. The distribution model is an owner decision; see
[CONTRIBUTING.md](CONTRIBUTING.md). Until a license is chosen, the default applies and
the code is not licensed for redistribution.
