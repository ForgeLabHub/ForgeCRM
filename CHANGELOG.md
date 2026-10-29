# Changelog

All notable changes to ForgeCRM are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once it reaches 1.0.

## [Unreleased]

### Added

- **Workflow template catalog.** `internal/workflow/templates.go` ships six ready
  CRM scenarios — an overdue-deal owner notification, a follow-up task on a stage
  change, an inactivity reminder, a new-ticket acknowledgement, a deal-won
  onboarding task, and a ticket priority triage — each built from
  a trigger and steps the engine already runs. The catalog is registered against
  `outbox.EventTypes()` at startup, so a template whose trigger is not in the
  platform's vocabulary fails construction rather than shipping enabled and silent.
  `POST /api/v1/workflow-templates/{id}/create` builds and validates the
  definition server-side, so a client never supplies it.

- **Workflow import and export.** A workflow is a portable document with a format
  name and a version. Import checks both before applying anything, so a document
  from another version is refused with a reason rather than partially applied.

- **A structured workflow editor.** `WorkflowEditor` reads and writes the same
  `{steps: [...]}` JSON the engine parses; the raw-JSON view is that document
  serialised, so there is no second definition format and an edit made in either
  view is what is saved. Step types and condition operators come from lists that
  mirror the engine, so the form cannot offer a step that does not exist. Structural
  rules the parser enforces are checked before save and shown per error; the server
  remains the authority.

- **`find_deals` and `foreach` workflow steps**, with bound date filters
  (`expected_close_before`, `updated_before`, `no_activity_since`) on `ListDeals`.
  Every new step type carries an entry in the engine's permission map, so a
  workflow cannot use a step to exceed its author's rights.

- **Attachments in the CRM.** `AttachmentPanel` lists, uploads and archives the
  files bound to an account, contact, deal or ticket, and renders only when the
  server reports storage on. `GET /api/v1/system/capabilities` now reports `storage`
  and `max_upload_bytes`, so a deployment with no object store shows nothing rather
  than an upload that would answer 503. The task screen gains the contextual-AI
  panel the other entity screens already had.

- **S3-compatible file attachments.** Documents are stored through a single
  provider-agnostic `ObjectStore` interface, implemented for any S3-compatible
  server (AWS S3, MinIO, Ceph RGW, B2, Wasabi) with the S3 SDK, and for an
  in-process memory store used by tests and by a deployment that wants to try
  attachments before wiring a bucket. Metadata lives in the existing `documents`
  table; the object key is `org/<org>/documents/<doc><ext>`, built server-side from
  the organization and document ids, so a client filename never reaches the key and
  a name containing `..` cannot escape its tenant prefix. Migration 0016 adds the
  `ticket_id`, `lead_id` and `task_id` attachment columns.

  The properties that matter, and the tests that hold them:

  - **A permission is checked on every route.** Upload, read, download, presign and
    archive each check a `documents.*` permission, and a document attached to an
    entity additionally requires the permission to read that entity, so a file
    cannot be used to write into or read out of a record the caller cannot see.
    `TestStorageUploadToUnreadableEntityIsRefused` and
    `TestStorageReadIsOrganizationScoped` cover both directions.
  - **The object and the row are reconciled.** An upload writes the object first and
    then the row; if the insert fails the object is deleted, so a failed upload
    leaves nothing behind (`TestStorageInsertFailureRemovesTheObject`). The reverse
    state — a row whose object is missing, and an object no row references — is
    repaired by `Reconcile`, which archives the broken row rather than deleting it
    and removes an orphan only once it is older than a grace period, so it is safe to
    run while uploads are in flight (`TestStorageReconcileRepairsBothDirections`,
    `TestStorageReconcileKeepsFreshOrphans`).
  - **The endpoint is an SSRF target and is guarded as one.** A private or loopback
    endpoint is refused unless `FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT=true`, and
    credentials are read from the environment and never logged or returned.
  - **The S3 path is tested against a real S3 server.** `internal/testsupport`
    drives the real client — signing, path-style addressing, paginated listing,
    presigning, 404 translation — through an in-process S3 implementation, because a
    mock of the interface would skip all of it (`storage_s3_test.go`).
  - **Off is a state, not an error.** A deployment with no endpoint gets a service
    that reports itself off; `/system/capabilities` reports `storage: false` and the
    document routes answer 503 rather than 500, so the UI hides uploads instead of
    showing a broken screen.

- **Workflow automation engine.** A workflow is a trigger plus an ordered list of
  steps, defined as JSON and stored per organization. Three trigger types are
  supported: an event committed through the outbox, a cron schedule, and a manual
  run. Step types are `condition`, `branch`, `create_task`, `update_deal`,
  `create_ticket`, `notify` and `delay`. Every action goes through the same
  `crm.Service` the HTTP API uses, so a workflow can never do anything its author
  could not do. See `docs/architecture/workflows.md` and
  `docs/adr/0007-workflow-automation-engine.md`.

  The design decisions that matter, and the tests that hold them:

  - **A workflow acts as its author, with permissions resolved at execution time,
    not authoring time.** If the author is blocked, removed from the organization,
    or has a role revoked, the workflow loses that authority on its next run rather
    than carrying a stale permission set. The engine enforces the coarse permission
    itself (`tasks.create`, `deals.change_stage`, …) because it calls the service
    directly: the API's middleware and the MCP tool registry both check it on their
    own paths, and without the engine's own check a user holding only
    `workflows.manage` could author a workflow to perform an action their role
    forbids. `TestWorkflowPermissionIsRecheckedAtExecutionTime` revokes the
    permission and asserts the next run fails without creating anything.
  - **Event-triggered runs are de-duplicated on `(workflow_id, event_id)`.** The
    outbox delivers at least once, so the same event can reach the engine twice. The
    claim is a primary-key insert, and the run row is created in the same
    transaction; a duplicate delivery commits nothing at all, so the history stays
    one run per event rather than one per delivery.
    `TestWorkflowEventTriggerIsDeDuplicated` delivers one event three times and
    asserts one run and one task.
  - **A delay does not block a worker.** A `delay` step writes a resume instant and
    returns; the worker's scheduler resumes the run from the recorded step index.
    `TestWorkflowDelayPausesAndResumes` asserts nothing runs before the instant and
    the run completes after it.
  - **A chain that a workflow causes is bounded.** Events emitted by a step inherit
    the run's correlation id, and the chain depth is the number of runs already
    recorded for it. A run at the workflow's `max_depth` is recorded as `skipped`
    rather than executed, which stops two workflows that watch each other from
    running forever. `TestWorkflowDepthLimitStopsLoops` drives a chained event and
    asserts it is skipped.

  - **A notification can only address a member of the same organization.** The
    recipient's active membership is verified before the insert, so a guessed user
    id cannot write into another tenant's feed.
    `TestWorkflowCannotNotifyAcrossOrganizations` asserts the run fails and nothing
    is written.

  The scheduler claims each due workflow by advancing its `next_run_at` inside the
  same transaction, so several worker replicas can run the loop without
  double-firing.

- **`/api/v1/workflows` and `/api/v1/workflow-runs`.** List, create, read, update,
  enable, archive and run workflows, and read a run with its per-step outcome log.
  Reading is gated by `workflows.read`, authoring by `workflows.manage`, running by
  `workflows.run`. A manual run is synchronous and returns the run result; a run
  that pauses on a delay returns `202` with the run id.

### Fixed

- **The retention sweep could not remove an expired idempotency key.** The delete
  statement selected a column called `id`, but `idempotency_keys` is keyed on
  `(scope, key)` and has no `id` column, so the policy failed with
  `column "id" does not exist` on every real run. The failure was invisible: it is
  recorded in that policy's result and the worker only logs it, so the table grew
  without bound while the sweep reported success for the policies beside it. The
  delete now selects `ctid`, which is what the statement's own comment said it did
  and which exists on every swept table regardless of its key.
  `TestRetentionSweepsAKeylessTable` fails against the old statement and passes
  against this one.

- **The worker aborted an in-flight job on shutdown instead of draining it.** Each
  job's context derived from the signal context, so SIGTERM cancelled the job in
  hand at the same instant it stopped the poll loops: a job that was mid-write was
  killed with its row still `running` and its write lost, and the requeue sweep
  only returned the row fifteen minutes later. The loops and the claim step now
  watch a context cancelled on the signal, while job execution and the bookkeeping
  that records its outcome watch a separate one cancelled only after
  `FORGECRM_WORKER_SHUTDOWN_GRACE` (30s default) has elapsed.
  `docker-compose.yml` sets `stop_grace_period: 45s` on the worker so Docker does
  not SIGKILL it before the drain window closes. `TestWorkerDrainsInFlightJobOnShutdown`
  fails against the old wiring and passes against this one.

- **A user could see their sessions but not end one.** The list endpoint, the
  revoke endpoint and `sessionsApi.revoke` all existed, but nothing in the
  interface called revoke, so a session left open on a lost device could only be
  closed by a password change. The Security page's sessions tab now offers a
  revoke control on every session but the current one.

- **`expected_close_date` failed to scan.** The column is a PostgreSQL `date` and
  was read into a `*string`, which pgx cannot encode in binary format, so any deal
  that had a close date failed to load from `ListDeals`, `GetDeal` and the workflow
  deal sweep. The column is cast to text, which is the ISO date the API already
  promised.

- **A redelivered event left an orphan run per delivery.** The event claim ran after
  the run row was created, so every duplicate delivery of the same event wrote a
  second `workflow_runs` row and marked it `skipped`. Under an outbox that retries,
  this accumulated without bound and made "one run per event" false. The claim and
  the run creation are now one transaction, and a duplicate commits nothing.

- **A dead webhook delivery left no durable record.** When a subscriber's endpoint
  kept failing, the delivery was marked `dead` and nothing else happened: no audit
  entry, no counter. An operator reading the audit trail could not see that a
  customer had stopped receiving events, and there was no value to alert on. The
  terminal transition now writes exactly one `webhooks.delivery_dead_lettered`
  audit entry and stamps `dead_lettered_at`. "Exactly one" is enforced in the
  database: the transition and the `status <> 'dead'` guard are one `UPDATE`, so a
  delivery several workers raced to fail produces one entry, not one per worker.
  `TestDeliveryDeadLetterIsAuditedOnce` asserts the count is 1 and that the entry
  is attributable to the owning organization.
- **Replaying a dead delivery erased how many times it had failed.** `Replay` reset
  `attempts` to zero and left nothing behind, so an operator could not tell a
  delivery that had just died from one that had already exhausted two full budgets.
  The per-window `attempts` is still reset — that is what grants the fresh retry
  budget — but a new `replay_count`, never reset, records the lifetime number of
  windows, and the frontend shows it next to the attempt count.
- **A delivery could be replayed while it was being sent.** The replay guard refused
  only `status = 'pending'`, but a delivery a worker has claimed is `delivering`. An
  operator could therefore replay a delivery mid-send, granting a second retry window
  that the attempt already in progress would consume, with the possibility of one
  event producing two outbound POSTs. The guard now refuses both in-flight states
  (`status NOT IN ('pending', 'delivering')`), which is what ADR 0004 already claimed
  to do.
- **A delivery could be replayed under a subscription it did not belong to.** The
  replay route nests a delivery under its subscription
  (`/webhooks/{id}/deliveries/{deliveryId}/replay`) and the OpenAPI document marks
  both path parameters required, but the handler read only `deliveryId` and ignored
  `{id}`. The mutating statement now filters on `subscription_id` as well as
  `organization_id`, so the subscription named in the path is part of what identifies
  the delivery and a mismatch is refused.

### Added

- **Human-in-the-loop agent approvals, with a second factor for destructive
  decisions.** An agent proposes an action; a person decides it. Nothing the agent
  proposes runs on its own. The decision carries the arguments hash the operator was
  shown and the server refuses a decision whose hash does not match the stored one,
  so approving cannot be redirected to a different payload. An action stored at the
  `destructive` risk tier additionally requires the deciding session to have
  presented a second factor within a short window: a lapsed window is refused with
  `403` and a `urn:forgecrm:error:forbidden` reason, and `POST /api/v1/auth/step-up`
  re-verifies the factor and refreshes the window. The factor is never read from the
  request body, so it cannot be asserted by the caller. The approvals screen
  (`/approvals`) shows the exact arguments, the binding hash and the risk tier, and
  prompts for the factor inline when the server refuses. See
  `docs/adr/0005-hitl-agent-execution-model.md`.
- **Dead-letter metrics.** `GET /api/v1/admin/metrics` now reports
  `webhook_deliveries_dead` (deliveries currently dead, which falls on a
  successful replay; named without a `_total` suffix because it is a gauge, so
  `rate()` over it would be meaningless), `webhook_deliveries_dead_lettered_total` (transitions ever,
  read from the append-only audit trail, so it only rises and is the value to alert
  on) and `webhook_deliveries_pending_total`. All three are unlabelled scalars: a
  tenant id, target URL or event id in a metric label would multiply the series
  without bound and leak a customer's endpoint into the monitoring system. They are
  read from the database rather than held in a process, so they are correct when
  several workers and API replicas run at once.

### Fixed

- **Outgoing webhooks were recorded but never sent.** The dispatcher created a
  `webhook_deliveries` row for every subscription matching an event, and no code
  ever read those rows back. A subscription could be created, verified with a
  test, receive a green status — and the customer's endpoint was never called for
  a real event. The row sat `pending` until the retention sweep deleted it. The
  worker now runs a delivery loop that claims pending deliveries, signs the
  payload and posts it, with exponential backoff and a bounded attempt count.
  `TestOutboundWebhookIsActuallyDelivered` asserts a real HTTP server receives a
  request whose signature verifies against the subscription's own secret.
- **Two workers could send the same webhook twice.** Deliveries were selected
  without being claimed, so two workers polling at the same moment both read the
  same row and both POSTed it. The customer's endpoint saw two deliveries for one
  domain change, which for a non-idempotent receiver is a duplicated side effect.
  Claiming is now a single `UPDATE ... FOR UPDATE SKIP LOCKED`, and a unique
  index on `(subscription_id, outbox_event_id)` rejects a duplicate delivery row
  even if the dispatcher runs concurrently with itself.
- **A failed outbox event was retried forever.** `MarkFailed` incremented the
  attempt count and nothing ever acted on it, so a permanently broken event was
  re-read by every drain until the end of the deployment. Retries are now bounded
  and an exhausted event is dead-lettered, so the failure is terminal and
  visible rather than an unbounded loop.
- **Outbox draining could dispatch the same event twice.** The drain loop read
  pending events without claiming them, so the scheduled drain and the queue job
  could both process one event. Both paths now claim, and a claim abandoned by a
  crashed worker is released on the next worker start instead of stranding the
  event.
- **The retention sweep never ran.** The handler existed and nothing enqueued
  it, so delivered webhooks, finished jobs and application logs accumulated
  without bound — a slow disk-filling defect rather than a theoretical one. The
  worker now enqueues the sweep hourly, idempotently, so a slow sweep is never
  doubled up by the next tick.

### Security

- **`X-Forwarded-For` was believed from any caller.** `chi`'s
  `middleware.RealIP` rewrote `RemoteAddr` from the forwarding header for every
  request, so a client could choose the address it was rate limited and logged
  under: rotating the header gave it an unlimited supply of fresh rate-limit
  buckets, and a brute-force attempt was attributed to addresses the attacker
  invented. The middleware is removed. The client address is now resolved once,
  from the connection address unless the peer is listed in the new
  `FORGECRM_TRUSTED_PROXIES` setting, and the proxy chain is read right-to-left
  skipping trusted hops. See `docs/operations/README.md`.

### Fixed

- **Data race in the rate limiter, with a possible process-killing panic.** The
  limiter kept its counters in a plain `map` that every request goroutine read
  and wrote. `net/http` serves each request in its own goroutine, so this was a
  live race rather than a theoretical one: `go test -race` reports it, and a
  sufficiently unlucky interleaving of concurrent map writes is an unrecoverable
  runtime panic. Counters now live behind a mutex in `internal/ratelimit`,
  covered by a 64-goroutine test that asserts no increment is lost.
- **The limiter's key map grew without bound.** `Sweep` existed but had no
  caller, so expired windows were never reclaimed. Since the keys are client
  addresses, anyone able to send a request could grow the map until the process
  was killed. The store now sweeps on a timer and holds a maximum key count.
- **`WriteError` panicked on a typed-nil error.** `errors.As` reports success for
  an error interface holding a nil `*APIError`, leaving the target nil; the
  following field access then panicked *inside the error path*, so a handler that
  returned a typed nil turned a recoverable failure into a crash. Found by a test
  written for the new envelope below, which is the kind of defect that a suite
  covering only the happy path does not reach.

### Added

- **Distributed rate limiting.** `internal/ratelimit` separates the counter
  store from the policy. `MemoryStore` is the default and keeps counters in the
  process; `RedisStore` shares them across replicas using a single Lua script, so
  the read-modify-write is atomic on the server rather than in the client. Select
  it with `FORGECRM_RATE_LIMIT_STORE=redis` and `FORGECRM_REDIS_URL`. The failure
  mode is per policy: general API traffic fails open, authentication fails closed,
  and a store error is never reported as a `429`. Both decisions are marked
  degraded and logged. Redis is opt-in through a compose profile, so the default
  `docker compose up` starts nothing extra. See `docs/operations/README.md`.
- **Rate-limit response headers.** `RateLimit-Limit`, `RateLimit-Remaining`,
  `RateLimit-Reset` and `Retry-After` are emitted, along with the older
  `X-RateLimit-*` names, so a client can see the budget instead of only
  discovering it by being refused.
- **Bulk archive for accounts, contacts and deals.** `POST
  /api/v1/{accounts,contacts,deals}/bulk-archive` takes up to 200 ids and returns
  which records were archived and which were not, with a reason for each. A
  record that is missing or already archived is reported in `failed` rather than
  failing the request, so one bad id cannot discard the rest of the batch. Every
  record is archived through the same org-scoped path as the single-record
  endpoint, so it emits the same events and writes the same audit entries. The
  lists gained a checkbox column and a bulk action that keeps a partial result
  visible instead of collapsing it into a success message.

- **RFC 9457 Problem Details in every error response.** The error envelope now
  carries `type`, `title`, `status`, `detail` and `instance` alongside the
  existing `error` object, so a generic client or gateway can consume a failure
  without knowing this API. Both views are derived from one `APIError`, so they
  cannot disagree, and `error.code` remains the value to branch on.
  - `retryable` states whether resending the identical request could succeed. It
    is not derivable from the status code: a 500 may be a transient fault or a
    deterministic defect, and retrying a defect is how a small incident becomes a
    large one, so 500 reports `false` while 429, 502, 503 and 504 report `true`.
  - `instance` is the request path with the query string removed, because a query
    string can carry tokens and personal data that must not be echoed into logs
    or client-side error reporting.
  - `correlation_id` is now emitted next to `request_id`; both carry the same
    value, since the log pipeline and the frontend read different names.
  - `type` is a URN, not an https URL: no dereferenceable error catalogue is
    operated, and a link that 404s is worse than an identifier that never claimed
    to resolve.

- **Archive format coverage for bzip2 and xz.** `GET /api/v1/system/capabilities`
  advertised `tar.bz2`, `tar.xz`, `bzip2` and `xz`, but the suite proved only
  zip, tar, tar.gz and gzip — four advertised formats were reachable from the API
  and exercised by no test. They are now covered by round-trip tests, corrupt-input
  rejection, magic-byte detection and an assertion that every advertised format is
  reported as supported. The bzip2 fixtures are embedded as base64 because the Go
  standard library reads bzip2 but cannot write it; they are decoded back with an
  independent reader so a fixture cannot drift into a false pass.

### Security

- **Patched GO-2026-6809 (CVE-2026-111943) in the Go standard library.** The
  vulnerability affects `crypto/x509.Certificate.Verify` and is reachable from
  this codebase through the SMTP TLS handshake in `internal/notify/email.go`,
  so it is a real exposure rather than an unused transitive finding. The
  toolchain is now pinned to Go 1.26.10 (the first fixed 1.26 release) in
  `go.mod`, the build image and CI.

### Added

- **Web interface.** React 19 + TypeScript single-page application built with Vite,
  served by the same Nginx instance as the API so every request stays same-origin.
  Covers sign-in with second-factor verification, the CRM workspace (accounts,
  contacts, leads, deals, tasks, support tickets, reports), the Import Center with
  the field-mapping wizard, integrations, migrations, events and webhooks,
  background jobs, the AI and MCP status page, and the administration pages for
  users, roles, organizations, teams, email, Telegram, audit, security and
  diagnostics. Authorisation is enforced by the server on every request; the
  interface only decides what to show.
- **Frontend tests.** 31 Vitest tests covering the HTTP client (CSRF handling,
  error envelopes, session expiry), the paginated list component and the
  permission gate and sign-in flow. `fetch` is stubbed at the boundary so the
  client, auth context and components all run for real.
- **ESLint flat configuration** for the frontend, wired into the checks above.

- **Platform foundation.** Go API and worker with PostgreSQL, `chi` routing, `pgx`
  connection pooling, structured logging, request correlation and graceful shutdown.
- **Schema and migrations.** Five migrations covering platform, CRM, platform modules,
  RBAC seed data and migration secrets. Applied automatically on start when
  `FORGECRM_MIGRATE_ON_START` is set.
- **Authentication.** Argon2id password hashing, server-side sessions with opaque
  cookies, CSRF protection, TOTP enrolment and verification, login rate limiting,
  account lockout, session listing and revocation, password reset, and OIDC.
- **RBAC.** A permission catalog of 80 permissions, 14 default roles cloned per
  organization, server-side enforcement on every route, and audit of role changes.
- **CRM domain.** Accounts, contacts, leads, deals, pipelines and stages, tasks,
  activities, tickets, tags, comments, custom fields, documents, notifications,
  contracts, approvals, notes, search across permitted entities, and sales and support
  reports. List endpoints support pagination, sorting, filtering and search.
- **Import Center.** Staged wizard API with format detection by content and extension,
  type inference, preview, field mapping, deduplication, dry run, background execution,
  per-row error reporting, and idempotent re-runs. Formats: CSV, TSV, JSON, JSONL,
  XLSX, XML, and the archives ZIP, TAR, TAR.GZ, GZIP, BZIP2 and XZ.
- **Safe archive handling.** Entry count, total size, per-entry size, nesting depth and
  compression-ratio limits; rejection of absolute paths, `..` traversal, backslash
  traversal and symlinks; private extraction root; cleanup on every exit path.
- **Data migration.** Schema discovery, chunked reads, field mapping and
  reconciliation for PostgreSQL and SQLite sources, with read-only defaults.
- **Integration Hub.** A consistent adapter lifecycle (validate, test, discover, read,
  transform, write, status, close), SSRF-guarded outbound HTTP, retries, timeouts and
  run history.
- **Events and webhooks.** Inbound endpoints with signature verification and
  idempotency keys, outbound subscriptions with signed delivery, retries with backoff,
  delivery history and replay, and a transactional outbox.
- **Background jobs.** A database-backed queue with attempts, backoff, cancellation and
  retry, driven by a separate worker process.
- **Audit and observability.** Append-only audit log, security event log, application
  logs, and administrative diagnostics endpoints.
- **Notifications.** An email module with SMTP configuration validation, templates,
  queueing and a send log, plus a Telegram bot with webhook authentication and account
  linking. Both are disabled unless configured.
- **AI layer.** A provider-agnostic interface with cloud and local (Ollama) backends,
  timeouts, request limits and metadata logging. Disabled by default.
- **Deployment.** A multi-stage Dockerfile, Docker Compose stack with health checks and
  named volumes, an optional Nginx reverse proxy, and scripts for environment checking,
  setup, migrations, backup, restore and smoke testing.
- **CI.** Formatting, vet, unit and integration tests against a real PostgreSQL,
  builds, Docker build, secret scanning, dependency vulnerability scanning and a
  documentation presence check.

### Fixed

- **Outbox polling failed on a null correlation identifier.** `FetchPending` scanned a
  nullable column into a non-pointer `string`, which made every poll fail and stopped
  event dispatch.
- **Uploaded files lost their format.** The format was not recorded when the file was
  stored, so the worker rejected every import with `unsupported tabular format
  "unknown"`.
- **Background imports re-sniffed the storage path.** Stored uploads are named
  `<uuid>.upload`, so extension-based detection found nothing. The resolved source now
  carries the detected format.
- **Archive extraction failed with a missing temporary directory.** The extraction
  parent directory was assumed to exist; it is now created before use.
- **Archives were misdetected as delimited text.** A binary archive header can contain
  a delimiter byte, so content sniffing classified ZIP files as CSV and failed with a
  misleading encoding error. An archive container now always clears the tabular format.
- **Archive analysis only handled single-file containers.** Multi-file archives could
  be imported but not analysed, so the preview and mapping steps were unavailable.
- **Archive analysis leaked server filesystem paths.** The absolute extraction path was
  serialised to clients; it is now excluded from the response.
- **Search returned `null` instead of an empty array** when nothing matched, breaking
  clients that iterate the result.
- **`webhooks.manage` was never seeded.** The permission was added to the Go catalog
  and guards six webhook endpoints, but migration 0004 did not insert it. No role could
  hold it, so only a platform owner could create, list, test or replay webhooks, and
  `integration_admin` was refused despite the role existing to manage integrations.
  Migration 0006 seeds the permission and grants it to `platform_owner`,
  `platform_admin`, `integration_admin` and `tenant_admin`, for platform role
  templates and for every organization's existing clones.

### Security

- Uploaded archives are handled as hostile input, with dedicated tests for path
  traversal, absolute paths, symlinks, archive bombs, truncation and cleanup.
- SSRF protection blocks loopback, link-local, private and cloud metadata destinations.
- Cross-organization access returns 404 rather than 403.
- Blocking a user revokes their sessions immediately.

### Added

- **End-to-end test suite.** `frontend/e2e/platform.spec.ts` drives a real
  Chromium browser against the real Go API and a real PostgreSQL database; the
  network is never stubbed. It covers sign-in, sign-out, session revocation, the
  account create/edit/archive lifecycle, cross-checks each write through the API
  rather than the screen alone, and loads every workspace and administration
  route asserting on the page's own heading so a route that silently falls
  through to the overview is caught. `npm run e2e` runs it; the `e2e` job in CI
  starts PostgreSQL, applies migrations, provisions the owner and runs the suite.
- **`scripts/e2e-bootstrap.sh`** provisions the end-to-end platform owner through
  the same `--bootstrap-admin` path an operator uses, so the suite has no
  test-only back door. It is idempotent and safe to run before every suite.
- **Regression tests for request throttling** in
  `backend/internal/api/ratelimit_test.go`, covering the two defects below.

### Fixed

- **Health probes were rate limited, so a busy API reported the container as
  unhealthy.** `/health` and `/ready` shared the API's request budget. Once the
  budget was spent the probes answered 429, which an orchestrator reads as a
  failed container and acts on by restarting it — turning a traffic spike into an
  outage. The probes now sit outside the limiter while `/api/v1` stays inside it,
  which `TestHealthEndpointsAreNotRateLimited` and `TestAPISubtreeIsRateLimited`
  pin down.
- **The login throttle was a hardcoded 20 requests per minute per client IP.**
  Everyone behind one NAT gateway shares a single bucket, so an office of thirty
  people would lock itself out after twenty sign-ins between them. The budget is
  now the `FORGECRM_LOGIN_RATE_LIMIT_PER_MIN` setting (default 60) and is
  documented in `.env.example`, because the correct value depends on how many
  users share an address.
- **Six lint errors and two warnings in the frontend** that would have failed the
  CI lint step, including a Playwright fixture parameter named `use` that lint
  read as React's `use` hook.
- **The integration-test step ran the packages in parallel over one shared
  database.** Each integration package resets the schema in its setup, so one
  package could drop the tables another was still seeding, and the suite failed
  with `relation "users" does not exist`. It passed in earlier runs by luck of
  scheduling, which is the worst kind of flake: green until it is not. The step
  now runs with `-p 1`.
- **The end-to-end job could time out waiting for the Vite dev server.** Vite
  binds `localhost` by default, which resolves to `::1` on some runners, while
  Playwright polls `127.0.0.1`; the server started correctly and the wait still
  expired. The config now pins the dev server to `127.0.0.1`.

### Known gaps

- No license file.
- OIDC has not been exercised against a real identity provider.
- Rate limiting is per process, not global. Behind more than one API replica the
  effective budget is multiplied by the replica count, so a shared store
  (Redis) is required before running more than one replica in production.
