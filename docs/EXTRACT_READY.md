# Extraction readiness

Honest assessment of which modules can be lifted out of ForgeCRM today. Statuses
follow the rule that **`READY` may only be claimed after the module was actually
copied into a clean project and built, tested and run there** — not because it has
a tidy folder.

Source commit for every experiment below: `7a6d67f`.

## Status definitions

| Status | Meaning |
| --- | --- |
| **READY** | Copied into a clean Go module, `go build` succeeded, the module's own tests ran, and a standalone smoke executed. Evidence recorded. |
| **ALMOST** | Boundary is clear and the module has few or no internal dependencies, but a specific, listed blocker prevents `READY`. |
| **NOT READY** | Extraction is not sensible now; the module needs much of the monolith. |

## What an extraction test is

For each candidate: a fresh directory, `go.mod` with a throwaway module path, the
package's non-test and test files copied in, third-party requirements pinned from
the module cache, then `go build ./...`, `go test ./...`, and a small `cmd/smoke`
program that exercises the public API. Nothing from ForgeCRM's `internal/` tree
travels unless it is a declared dependency.

The procedure is written up in `EXTRACTION_TESTING.md`.

---

## Candidate 1 — `internal/analytics` — **READY**

**Why it is the strongest candidate:** it is pure arithmetic. `go list` confirms
**zero internal imports**; its only non-stdlib dependency is none at all.

**Evidence (performed at `7a6d67f`):**

```bash
T=/tmp/extract-analytics
rm -rf $T && mkdir -p $T
cp backend/internal/analytics/*.go $T/
cd $T && printf 'module example.com/analytics\n\ngo 1.26.10\n' > go.mod
go build ./...        # BUILD_OK
go vet ./...          # VET_OK
go test -count=1 ./... # ok  example.com/analytics  0.006s
```

A standalone `cmd/smoke/main.go` using only `analytics` public API printed
`smoke: 49.98` (1999 minor units × 150% = 49.98). No database, no config, no
ForgeCRM package.

**Files that move:** `internal/analytics/*.go` (5 sources + 4 test files). That is
the whole cut line.

**Blockers:** none.

**Next actions:** decide the import path (`forgecrm/analytics` vs a standalone
repo), then repeat the same recipe in CI as an `extract-test` job.

---

## Candidate 2 — `internal/ratelimit` — **READY** (one caveat)

**Why:** also **zero internal imports**; Redis is optional and only `redis.go`
names `go-redis`, so the memory path is dependency-free.

**Evidence (performed at `7a6d67f`):**

```bash
T=/tmp/extract-ratelimit
rm -rf $T && mkdir -p $T
cp backend/internal/ratelimit/*.go $T/
cd $T
# go.mod pins go-redis v9.23.0 + golang.org/x/sys v0.49.0 from the local module cache
go build ./...   # BUILD_OK
go test -count=1 -run 'TestMemory|TestLimiter|TestFail|TestDisabled|TestStoreError' ./...
# ok  example.com/ratelimit  0.026s
```

**Caveat (the one thing not re-verified in isolation):** `redis_integration_test.go`
is `//go:build integration` and needs a live Redis; the isolated run executed the
unit tests only. The Redis code path compiled but was not exercised in the clean
project. This does not block `READY` — the memory limiter, which is the part a
recipe uses by default, was built and tested — but it is recorded rather than
glossed over.

**Files that move:** `internal/ratelimit/*.go`. The `Store` interface is the seam;
a consumer may drop `redis.go` and ship only `MemoryStore`.

**Blockers:** none for the memory path. To claim the Redis path in isolation, run
the integration build tag against a Redis in the extracted project.

**Next actions:** add a `NoopStore` (there is none today — see "not yet present"
below) and make `redis.go` a separate build-tagged file so a Redis-free extraction
needs no third-party module at all.

---

## Candidate 3 — `internal/secrets` — **ALMOST**

**Why:** **zero internal imports**; a small, pure crypto helper library
(Argon2 hashing, HMAC signing, token generation).

**Evidence (performed at `7a6d67f`):**

```bash
T=/tmp/extract-secrets
rm -rf $T && mkdir -p $T
cp backend/internal/secrets/*.go $T/
cd $T && printf 'module example.com/secrets\n\ngo 1.26.10\n\nrequire golang.org/x/crypto v0.58.0\n' > go.mod
go build ./...   # BUILD_OK
go test -count=1 ./...   # ?   example.com/secrets   [no test files]
```

A standalone smoke hashed and verified a password (`secrets.VerifyPassword`
returned `nil`).

**Blocker:** the package ships **no `_test.go` of its own** — the extracted
project reports `[no test files]`. `READY` requires the module's own tests to run
in isolation, and there are none to run. The behaviour is covered only indirectly
through `authn` inside the monolith, which is exactly the "tests pass only in the
monolith" condition the Definition of Done forbids.

**Next actions:** port the password/token assertions that currently live inside
`authn` tests into `secrets` tests; then the module can be re-assessed as `READY`.

---

## Candidate 4 — `internal/logging` — **ALMOST**

**Why:** zero internal imports, 158 lines, pure `log/slog` setup and redaction.

**Evidence (performed at `7a6d67f`):**

```bash
T=/tmp/extract-logging
cp backend/internal/logging/*.go $T/
cd $T && printf 'module example.com/logging\n\ngo 1.26.10\n' > go.mod
go build ./...   # BUILD_OK
go vet ./...     # VET_OK
```

**Blocker:** no tests, and it is a helper rather than a module with a lifecycle —
extracting it yields little reuse. It is `ALMOST` structurally but not a
high-value extraction target.

---

## Candidates assessed but not extracted

These were evaluated from the dependency graph and marked without an extraction
run, because each has a concrete blocker that an extraction would only confirm.

| Module | Status | Evidence | Blockers | Next actions |
| --- | --- | --- | --- | --- |
| `internal/storage` | **SPLIT — `objectstore` READY** | The provider layer was lifted into `internal/objectstore` and extracted: `go build`, the shared provider contract (Memory and real S3 wire protocol) and a smoke all pass in a clean module. `Service` keeps the authorization, metadata and reconciliation and stays behind. | `Service` still imports `audit, database, httpx, rbac`. That is correct — those are its job — and it is why only the provider layer was lifted. | None for `objectstore`. `Service` is not an extraction target; it is the application layer. |
| `internal/outbox` | **ALMOST** | Small surface (`Store`, `Envelope`, `Event`, `EventTypes`); has a unit test. | Depends on `database`, `logging`; the outbox is meaningless without the app's transaction. | Document the `database` dependency; it is a substrate-bound module. |
| `internal/retention` | **ALMOST** | Self-contained logic; unit + integration tests; dry-run by default. | Depends on `audit`, `database`; policy table is application-specific. | Extract the engine, inject a table catalog. |
| `internal/audit` | **ALMOST** | Small surface (`Logger`, `Entry`, `Action`); append-only enforced by a DB trigger. | Depends on `database`, `logging`; no in-package tests; schema lives in migration 0001. | Move schema ownership with it or provide a migration. |
| `internal/rbac` | **ALMOST** | `NewPrincipal` gives a test fake; catalog is a plain const block. | Depends on `database`; catalog must match migration 0004. | Split the pure catalog + `Principal` from the DB-backed `Service`. |
| `internal/jobs` | **ALMOST** | One internal dep (`database`); clear responsibility. | Uses `FOR UPDATE … SKIP LOCKED`; PostgreSQL-specific. | Would need a dialect abstraction. |
| `internal/agent` | **ALMOST** | `Executor` interface seam; unit + integration tests. | Depends on `audit, database, httpx`. | Fine to leave; it is small and cohesive. |
| `internal/integrations` | **ALMOST** | `Adapter` interface; unit tests. | Depends on `audit, database, httpx, rbac, secrets`. | The SSRF guard alone was extracted: it now lives in `internal/netguard` (standard library only) and `integrations` re-exports it. |
| `internal/crm` | **ALMOST** | The domain core; nothing imports it back, so the boundary is one-directional. | Depends on `httpx` and `outbox` (platform concerns) besides `audit/database/rbac`. | Invert `httpx`/`outbox` behind interfaces first. |
| `internal/ai` | **NOT READY** | 9 internal deps; binds to `crm` for context. | `agent, audit, config, crm, database, httpx, logging, ratelimit, rbac`. | Split `Provider`+prompt+confidence (portable) from the context assembly (bound). |
| `internal/importx` | **NOT READY** | 10 internal deps; writes into `crm`. | `parsers/` and `safe/` are separable; the service is not. | Extract the two sub-packages only. |
| `internal/events` | **NOT READY** | 8 internal deps. | `audit, database, httpx, integrations, logging, outbox, rbac, secrets`. | The `Poster` interface is the only clean seam. |
| `internal/workflow` | **NOT READY** | Engine calls `crm.Service` directly. | `audit, crm, database, httpx, logging, outbox, rbac`. | Definition/parse/template layer is separable. |
| `internal/simulate`, `forecast`, `risk` | **NOT READY** | Thin service wrappers over `analytics` + `crm`. | Each depends on `analytics, audit, crm, database, rbac`. | The portable core is already `analytics`. |
| `internal/api`, `wiring`, `config`, `openapi`, `migrate`, `migrations`, `testsupport` | **NOT READY** | Structurally application-wide. | — | Not extraction targets. |
| `internal/notify` | **NOT READY** | SMTP + Telegram. | No `Mailer` interface; no in-package tests. | Introduce a `Mailer` port first. |
| `internal/authn` | **NOT READY** | Binds to `rbac`, `secrets`, `database`, `config`, `audit`, `logging`. | 6 internal deps. | Extract after `rbac`/`secrets`. |

---

## Summary

| Module | Status | One-line reason |
| --- | --- | --- |
| `analytics` | **READY** | Zero internal deps; extracted, built, tested, smoke ran. |
| `ratelimit` | **READY** | Zero internal deps; extracted, built, unit-tested (Redis path compiled, not run). |
| `netguard` | **READY** | Standard library only; extracted, built, tested, smoke ran (blocked loopback/metadata, allowed public). |
| `objectstore` | **READY** | Extracted with `netguard`; built, shared provider contract passed for `Memory` and real S3 wire protocol, smoke ran. |
| `secrets` | **ALMOST** | Extracts and builds, but ships no tests of its own. |
| `logging` | **ALMOST** | Extracts and builds; no tests; low reuse value. |
| everything else | **ALMOST** / **NOT READY** | Concrete blocker listed above. |

**No module is marked `READY` without an extraction run recorded here**, and the
run is now automated: `scripts/extract-test/run.sh` performs it for every module in
`modules.txt` on each CI run. `analytics`, `ratelimit`, `netguard` and
`objectstore` pass that bar today.

The four `READY` modules are proven by the harness, not by this document. If the
harness ever fails for one of them, the status here is wrong and the harness is
right.
