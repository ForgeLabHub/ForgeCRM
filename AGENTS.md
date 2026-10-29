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
- `frontend/` — React 19 + TypeScript + Vite web interface (pages, api clients, tests)
- `docs/index.md` — **start here**; maps every question to the document that answers it
- `docs/verification/matrix.md` — claim -> command -> CI job; edit it when behaviour changes
- `BUILD.md` — pinned versions and build levels A-E

## Build and test

```bash
cd backend
export PATH=$PATH:/usr/local/go/bin
export GOTOOLCHAIN=local            # the sandbox toolchain is pinned

go build ./...
go vet ./...
gofmt -l ./cmd ./internal           # must print nothing
go test -race -count=1 ./...

# The API specification is generated from the live router, never hand-edited.
go run ./cmd/openapi-gen            # regenerate docs/api/openapi.json
go run ./cmd/openapi-gen -check     # what CI runs; fails when the two disagree
```

**Never edit `docs/api/openapi.json` by hand.** It is generated from the router and
from the route registrations, and CI fails if it disagrees with them. A route
change means running the generator and committing the result.

Request and response schemas are derived from the types the handlers actually use,
so the document cannot describe a shape no code accepts:

- The contract table lives in `internal/api/contract.go` (`APIContract`). It names
  the real DTO for each route. It cannot live in `internal/openapi`, because the
  request DTOs are unexported types in package `api` and only that package can name
  them — a re-declared copy would describe a different type.
- Field names, types, nullability and the required set of a **response** come from
  the struct and its tags. Only a **request**'s required set is declared, next to
  the validation it mirrors, because presence is a fact about validation and cannot
  be read off the type.
- A type used as both a request and a response gets a distinct `...Input` component
  for the request direction (`DealInput`), so its request `required` set cannot
  contradict its response one. Two types sharing a bare name (`importx.Job`,
  `jobs.Job`) are disambiguated with `Contract.TypeNames`.
- A validation rule that is conditional — "at least one of these", "one of a fixed
  set" — cannot be expressed as a required list. It goes in the request body
  description, and `internal/api/contract_test.go` fails if a rule the validator
  enforces is neither declared required nor recorded as conditional.

Adding a request body or response to a route means adding a row to the table in
`internal/api/contract.go`. A route with no row is left unspecified rather than
guessed, and `TestNoRouteIsInvented` / `TestNoOrphanComponents` keep the two
directions honest.

A route must declare its access level in one of three ways, or generation fails:
a permission via `s.perm(rbac.PermX)` in its registration, an entry in
`openapi.Anonymous` (no session at all), or an entry in `openapi.SessionOnly`
(session, no permission). `Anonymous` is a closed list of six; adding to it means
adding an unauthenticated endpoint, which needs a reason.

Integration tests need a real database and a build tag. They skip themselves when
the URL is unset, so the default test run stays green:

```bash
FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable' \
  go test -tags=integration -count=1 ./...
```

`internal/testsupport` drops and rebuilds the `public` schema on every test, so it
must only ever point at a scratch database.

The frontend is a separate npm project:

```bash
cd frontend
npm ci
npm run typecheck   # tsc -b --noEmit
npm run lint        # eslint
npm run test        # vitest run
npm run build       # tsc -b && vite build
```

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
- **The GitHub repository moved to `ForgeLabHub/ForgeCRM`** (the old
  `geff1778/ForgeCRM` redirects). The `origin` remote in this checkout still points
  at the old name and `GITHUB_TOKEN` is read-only for it, so `git push` fails with
  `403`. Push with the personal token and the current name:
  `git remote set-url origin "https://x-access-token:${GITHUB_PERSONAL_ACCESS_TOKEN}@github.com/ForgeLabHub/ForgeCRM.git"`.
- **The E2E API process is a prebuilt binary at `/tmp/forgecrm-api`** started
  against `forgecrm_e2e` on `127.0.0.1:8080` (bootstrap owner
  `e2e-owner@forgecrm.test`). A route added since it was built returns `404`, and a
  migration added since it started is unapplied — the Simulation Lab page then fails
  with `relation "simulation_scenarios" does not exist`. Rebuild, apply migrations
  (`/tmp/forgecrm-api-new --migrate`), and restart it before running Playwright.
- **The Go toolchain is pinned in three places** and they must move together:
  `go.mod`'s `go` directive, `backend/Dockerfile` and `GO_VERSION` in CI. A
  stdlib CVE is fixed by bumping all three; bumping only one lets a fix pass in
  one place and be missing in another.
- **`database.WithTx` rolls back on *any* error a closure returns.** A closure
  that needs both to write and to report a failure cannot do so by returning the
  error: the write is discarded. Record the outcome in a variable, return `nil`
  to commit, and return the error *after* the transaction. The expired-approval
  transition was broken this way — it updated `agent_actions.status` to `expired`
  and then returned `ErrExpired` from inside the transaction, so the caller saw
  the right error while the row stayed `proposed` and the deadline was never
  enforced. Assert persisted state by reading the table, not the returned struct;
  a rollback leaves the caller holding a struct that describes a write the
  database never kept. When a second path enforces the same invariant as a
  background sweep, both must apply the same reconciliation, or a row the sweep
  no longer matches stays stranded.
- **A "still in flight" guard must name every in-flight state, not the obvious
  one.** The webhook replay guard refused only `status = 'pending'`, but a delivery
  a worker has claimed is `delivering` — so an operator could replay a delivery
  that was being sent at that moment, granting a second window the in-progress
  attempt would consume and allowing one event to produce two sends. The route also
  nests a delivery under its subscription (`/webhooks/{id}/deliveries/{deliveryId}
  /replay`), and both ids are required path parameters, so the mutating statement
  filters on `subscription_id` as well as `organization_id`; a handler that reads
  only one half of the path lets the same request mean two different things. When a
  status column gains a state, check every guard that enumerates states — the
  `CHECK` constraint lists the full set.
- **A webhook subscription's event list must come from the server, never a local
  constant.** The form once hard-coded bare names (`contact.created`) while the
  dispatcher matched namespaced ones (`crm.contact.created`), so a subscription
  created in the UI was accepted and enabled and then delivered nothing — a failure
  that looks like a network fault and is invisible in the UI. The vocabulary is
  `outbox.EventTypes()`, published as `event_types` on `/api/v1/system/capabilities`;
  the picker reads it. `TestWebhookEventVocabularyMatchesTheDispatcher` fails if the
  two ever diverge.
- **The audit log is append-only**, so a user who has acted can never be deleted
  — the FK cascade is blocked by a trigger. Disable the account instead
  (`status` must be one of `invited`, `active`, `suspended`, `blocked`).
- **Frontend response types are hand-written** in `src/types/api.ts`. Check a
  changed handler's real JSON before editing them; several fields were guessed
  wrong the first time (audit ids are integers, not UUIDs).
- **The frontend is served same-origin by Nginx.** Do not add a CORS
  configuration or move the session token into `localStorage`; the cookie plus
  CSRF double-submit depends on same-origin.
- **A capability flag must name the condition it tests, not the feature a reader
  hopes for.** `capabilities.oidc` once reported that settings were present while
  no sign-in flow existed, so clients offered SSO that could not work. It is now
  `oidc_configured`, and the tool endpoint is `tool_api` because it speaks
  `tools/list` and `tools/call` but is not a full MCP server.
  `capabilities_test.go` fails if either misleading name returns. `Enabled()` on
  the optional services tolerates a nil receiver, so a subsystem left unconstructed
  reports "off" rather than panicking the diagnostics endpoint.

- **The workflow engine calls `crm.Service` directly, so it enforces the coarse
  permission itself.** The API enforces permissions in middleware and the MCP
  registry enforces them in its tool layer; the service layer checks tenant scope
  and record ownership but not the coarse "may you do this at all" permission.
  The engine bypasses both, so it maps each step type to a `rbac.Permission` and
  checks it against the author's freshly resolved principal before running the
  step. A new step type without an entry in that map is a privilege-escalation
  bug: a user holding only `workflows.manage` could perform an action their role
  forbids. Add the permission with the step type.
- **A workflow acts as its author, resolved at execution time.** Not captured at
  authoring time: a blocked author, a removed membership or a revoked role stops
  the next run. If you cache the principal on the workflow row, you break this.
- **Event-triggered runs are de-duplicated by a database constraint, not a flag.**
  `workflow_run_events` has a primary key on `(workflow_id, event_id)` and the run
  row is created in the same transaction as the claim. Do not split them: creating
  the run first and marking a duplicate `skipped` leaves one orphan run per
  redelivery, which is unbounded under the outbox's retries. This was a real bug
  and `TestWorkflowEventTriggerIsDeDuplicated` is the guard.
- **A chain a workflow causes is bounded by correlation, not by a timer.** A step's
  emitted event inherits the run's correlation id, and the chain depth is the count
  of runs already recorded for it. `run_depth` is therefore derived, not passed in
  a payload. Two workflows that watch each other stop at `max_depth`.

- **A service that `internal/wiring` builds is not in the API until `main.go`
  copies it into `api.Deps`.** The two lists are maintained by hand and nothing at
  startup compares them, so a service added to `wiring.Services` but forgotten in
  the `api.New(&api.Deps{...})` literal leaves its handlers dereferencing nil. The
  recovery middleware turns the panic into an opaque 500, so the fault looks like
  a broken endpoint rather than a wiring mistake — the Forecast/Risk omission
  surfaced only in the E2E run against the real binary.
  `cmd/forgecrm-api/deps_wiring_test.go` now parses both declarations and fails
  when a wired service is never copied, so the mistake is a unit-test failure.
  The intelligence routes also mount behind `requireService`, which answers 503
  naming the subsystem when its service is nil rather than panicking.

## Conventions

- `gofmt` clean, `go vet` clean, no new dependency where the standard library works.
- Every endpoint checks a permission on the server. Hiding UI is not access control.
- Tenant-scoped queries always filter by `organization_id`.
- Comments explain *why*. Do not narrate the change in code; use the commit message.
- Errors are wrapped with context: `fmt.Errorf("do the thing: %w", err)`.

## Status

Backend, schema, RBAC, auth, audit, jobs, outbox, CRM domain, Import Center,
integrations, events, notifications and the AI layer exist. The web interface
covers sign-in with second factor, the CRM workspace, the Import Center, and the
administration pages. Deployment, docs and CI are in place; CI runs a frontend
job as well as the backend. The AI layer is backend-only — there is no AI page
beyond the status screen. No license has been chosen — do not add one without
asking the owner.
