# ForgeCRM — repository notes for agents

Self-hosted CRM platform. Go backend + PostgreSQL, modular monolith, no AI or
external integration required for the core to work.

## Layout

- `backend/` — Go module `github.com/geff1778/forgecrm`
  - `cmd/forgecrm-api`, `cmd/forgecrm-worker` — the two entrypoints
  - `internal/api` — HTTP handlers and routing, one file per area
  - `internal/importx` — Import Center: parsers, format detection, execution
  - `internal/importx/safe` — untrusted-archive extraction
  - `internal/integrations` — adapter lifecycle and the SSRF guard
  - `internal/migrations` — embedded SQL, numbered `NNNN_name.{up,down}.sql`
  - `internal/rbac` — permission catalog; must stay in sync with migration 0004
- `docs/` — audit, architecture, roadmap, acceptance criteria, deployment, operations
- `scripts/` — check-env, setup, migrate, backup, restore, smoke-test
- `frontend/` — **empty**; the API is the only interface today

## Build and test

```bash
cd backend
export PATH=$PATH:/usr/local/go/bin
export GOTOOLCHAIN=local            # the sandbox toolchain is pinned

go build ./...
go vet ./...
gofmt -l ./cmd ./internal           # must print nothing
go test -race -count=1 ./...
```

Integration tests need a real database and a build tag. They skip themselves when
the URL is unset, so the default test run stays green:

```bash
FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable' \
  go test -tags=integration -count=1 ./...
```

`internal/testsupport` drops and rebuilds the `public` schema on every test, so it
must only ever point at a scratch database.

## Running locally

```bash
cd backend
go build -o /tmp/forgecrm-api ./cmd/forgecrm-api
go build -o /tmp/forgecrm-worker ./cmd/forgecrm-worker

FORGECRM_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm?sslmode=disable' \
FORGECRM_SECRET_KEY='dev-secret-key-0123456789abcdef0123456789abcdef' \
FORGECRM_ENV=development FORGECRM_HTTP_PORT=8080 \
  /tmp/forgecrm-api &
```

Full stack: `docker compose up -d --build`. The image embeds migrations, so a
schema change requires a rebuild, not just a restart.

## Things that will bite you

- **Migrations are embedded in the binary.** A new migration does nothing until
  the image or binary is rebuilt.
- **Never edit an applied migration.** Add a new numbered pair.
- **The permission catalog and migration 0004 must agree.** `webhooks.manage` was
  in the catalog but not seeded for a long time, which silently blocked every
  non-owner role. `TestSeedDataMatchesThePermissionCatalog` now catches this.
- **The API and worker are separate processes.** An import that stays `queued`
  means no worker is running.
- **Integration tests are gated by both a build tag and an env var.**
- **`.env` holds `FORGECRM_SECRET_KEY`** and is gitignored. Never commit it.
- **`.env` in this workspace already has a generated key** and `FORGECRM_HTTP_PORT`
  may be overridden; check before assuming port 8080.

## Conventions

- `gofmt` clean, `go vet` clean, no new dependency where the standard library works.
- Every endpoint checks a permission on the server. Hiding UI is not access control.
- Tenant-scoped queries always filter by `organization_id`.
- Comments explain *why*. Do not narrate the change in code; use the commit message.
- Errors are wrapped with context: `fmt.Errorf("do the thing: %w", err)`.

## Status

Backend, schema, RBAC, auth, audit, jobs, outbox, CRM domain, Import Center,
integrations, events, notifications and the AI layer exist. Deployment, docs and
CI are in place. The frontend does not exist. No license has been chosen — do not
add one without asking the owner.
