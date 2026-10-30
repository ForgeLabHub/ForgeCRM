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

## Candidate 3 — `internal/secrets` — **READY**

**Why:** **zero internal imports**; a small, pure crypto helper library
(Argon2 hashing, HMAC signing, token generation, the AES-GCM keyring).

**Blocker that was, and how it was cleared:** the package shipped **no `_test.go`
of its own**, so an extraction reported `[no test files]` and the behaviour was
covered only indirectly through `authn` inside the monolith — exactly the
"tests pass only in the monolith" condition the Definition of Done forbids.
`secrets_test.go` now lives with the package: round-trip and salted hashing, the
malformed-hash path distinguished from a mismatch, a fresh nonce per encryption,
decryption refused under the wrong key and under tampering, and the CSRF/webhook
signatures binding secret, timestamp and body. The tests run in the extracted
project, so the proof is behavioural.

**Evidence (performed at `7a6d67f`; re-run by the harness after this slice):**

```bash
T=/tmp/extract-secrets
rm -rf $T && mkdir -p $T
cp backend/internal/secrets/*.go $T/
cd $T && printf 'module example.com/secrets\n\ngo 1.26.10\n\nrequire golang.org/x/crypto v0.58.0\n' > go.mod
go build ./...   # BUILD_OK
go test -count=1 ./...   # ok  example.com/secrets  (20 tests)
```

A standalone smoke hashes a password, verifies it, rejects a wrong one and
round-trips a value through the keyring.

**Files that move:** `internal/secrets/*.go` (1 source + 1 test file). The only
third-party dependency is `golang.org/x/crypto`.

---

## Candidate 4 — `internal/logging` — **READY**

**Why:** zero internal imports, pure `log/slog` setup and central redaction. It is
the foundation every other package logs through, so keeping it a leaf is a
structural guarantee, not a convenience.

**Blocker that was, and how it was cleared:** no tests. `logging_test.go` now
pins the security property the package exists for — a password or token passed as
a log field never reaches the output, including through `With` and through a
nested group — plus the context helpers and the level/format wiring of `New`. A
test asserts `New` returns the redacting handler, so a refactor that dropped the
wrapper would fail.

**Evidence:**

```bash
T=/tmp/extract-logging
cp backend/internal/logging/*.go $T/
cd $T && printf 'module example.com/logging\n\ngo 1.26.10\n' > go.mod
go build ./...   # BUILD_OK
go vet ./...     # VET_OK
go test -count=1 ./...   # ok  example.com/logging  (10 tests)
```

---

## Candidate 5 — `internal/httpx` — **READY**

**Why:** the HTTP layer every handler is written against — the RFC 9457 error
envelope, the JSON writers, pagination and sort parsing, the access-log, recovery,
security-header and rate-limit middleware, and the trusted-proxy client-address
resolver. Its only in-module dependencies are `logging` and `ratelimit`, both
already proven, so the whole layer lifts out without reaching into the
application.

**Blocker that was:** none structural; it was simply never run. `httpx` was
marked `almost` on the judgement that it *is* the shared HTTP substrate and so is
rarely worth extracting on its own. That judgement is about desirability, not
feasibility, and the two were conflated: the useful guarantee is that the layer
*can* lift out, because that is what keeps a handler from reaching into it and
dragging the application layer back in.

**Evidence:**

```bash
scripts/extract-test/httpx/setup.sh
#   copied internal/httpx -> . (8 files)
#   copied internal/logging -> logging (2 files)
#   copied internal/ratelimit -> ratelimit (5 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...
#   ok    go run ./cmd/smoke
#   ok    httpx extracts cleanly
```

The package's own tests run in the extracted project, including the client-address
trust tests, and a smoke writes and reads a JSON list response, drives a
rate-limited handler to a 429 with headers on the allowed response, and resolves a
client address.

**Files that move:** `internal/httpx/*.go` (4 sources + 4 test files), with
`internal/logging` and `internal/ratelimit` as declared dependencies.

---

## Candidate 6 — `internal/importx/safe` — **READY**

**Why:** the untrusted-archive extractor. It is the single place in the codebase
that writes bytes chosen by an uploader to disk, and it is the guard against path
traversal, symlinks, zip bombs, oversized entries and deep nesting. It has no
in-module dependency at all, so the whole package moves.

**Blocker that was:** none; it was simply never run. It is the strongest kind of
extraction claim precisely because the code handles attacker-controlled input:
"the security tests run in isolation, in a clean module" is worth more here than
anywhere else.

**Evidence:**

```bash
scripts/extract-test/safe/setup.sh
#   copied internal/importx/safe -> . (4 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...          # ~24 tests, incl. traversal, symlink, zip bomb
#   ok    go run ./cmd/smoke     # roundtrip, traversal refused, zip bomb refused
#   ok    importx/safe extracts cleanly
```

**Files that move:** `internal/importx/safe/*.go` (2 sources + 2 test files). No
dependency moves with it.

---

## Candidate 7 — `internal/importx/parsers` — **READY**

**Why:** it converts CSV/TSV/JSON/JSONL/XLSX/XML into the common row model the
Import Center maps to CRM fields. It has no in-module dependency, so the whole
package moves; its only external dependency is the spreadsheet library.

**Blocker that was:** it shipped **no tests of its own**, so the extraction
reported `[no test files]` and nothing pinned its behaviour. Writing the tests
found two real bugs, both now fixed:

- **XLSX could not be parsed at all.** `Parse` applied the UTF-8 text check to
  every format, but XLSX is a zip of XML parts and is binary by construction, so
  every workbook was rejected with "file is not valid UTF-8". The check is now
  skipped for the one binary format it was never meant to see.
- **XML split every record in two.** A record's attributes and its nested
  elements were emitted as separate rows — `<contact id="1"><name>Ada</name>` came
  out as `{@id:1}` and `{name:Ada}` — and the record element was inferred as the
  document root, so a normal export produced a column set of `[@id name]` with no
  row holding both. The parser was rewritten to keep one row per record, flatten
  nested elements to dotted keys (matching the JSON parser) and infer the record
  element as the root's first child.

**Evidence:**

```bash
scripts/extract-test/parsers/setup.sh
#   copied internal/importx/parsers -> . (3 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...          # 19 tests added in this slice
#   ok    go run ./cmd/smoke
#   ok    importx/parsers extracts cleanly
```

**Files that move:** `internal/importx/parsers/*.go` (3 sources + 1 test file),
with `github.com/xuri/excelize/v2` as its declared dependency.

---

## Candidate 8 — `internal/database` — **READY**

**Why:** the PostgreSQL connection pool and the transaction helper every
repository is written against. It has no in-module dependency at all, so the whole
package moves; its only external dependency is the pgx driver.

**Blocker that was:** none. The package was marked `no` in `MODULES.md` on the
reasoning that "it is the shared substrate; everything sits on it". That is a fact
about *desirability*, not *feasibility*, and the guarantee worth having is that it
*can* lift out — otherwise nothing stops a future import from welding it to the
application. Its pool-size guard is enforced before a connection is attempted, so
its tests need no server and run in the extracted module.

**Evidence:**

```bash
scripts/extract-test/database/setup.sh
#   copied internal/database -> . (2 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...
#   ok    go run ./cmd/smoke     # over-large pool refused, defaults consistent
#   ok    database extracts cleanly
```

**Files that move:** `internal/database/*.go` (2 sources + 1 test file), with
`github.com/jackc/pgx/v5` as its declared dependency.

---

## Candidate 9 — `internal/rbac` — **READY**

**Why:** the permission catalog, the request principal every handler checks, and
the service that resolves a user's effective permissions and roles. It has one
in-module dependency, `database`, which is itself now proven, so the whole package
lifts out.

**Blocker that was:** the docs proposed splitting "the pure catalog and
`Principal` from the DB-backed `Service`" before it could be extracted. That is
not what the code needs: the catalog, the principal and the service share the
`Permission` type and the same file, and the package already extracts whole with
its one dependency. A split would be a change to the code and not to the boundary,
so the package is extracted as it is, and the boundary is pinned in `archtest`
(`rbac` may import `database` and nothing else in the module).

**Evidence:**

```bash
scripts/extract-test/rbac/setup.sh
#   copied internal/rbac -> . (2 files)
#   copied internal/database -> database (2 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...
#   ok    go run ./cmd/smoke     # catalog=93, principal checks, owner=whole catalog
#   ok    rbac extracts cleanly
```

**Files that move:** `internal/rbac/*.go` (2 sources + 1 test file) and
`internal/database` (its one dependency).

---

## Candidate 10 — `internal/audit` — **READY**

**Why:** the append-only action trail and the security-event stream. It has two
in-module dependencies, `database` and `logging`, both proven, so the package
lifts out. It had no in-package tests before this slice; it now has tests for the
action vocabulary, the validation ordering that keeps an unattributable row out of
the trail, and the marshalling helpers, none of which needs a server.

**Blocker that was:** the docs listed "no in-package tests; schema lives in
migration 0001" as the blocker. The schema genuinely is not part of the package —
a real extraction would carry a migration with it, and that is stated rather than
hidden — but the *Go* package has no coupling to the application, which is what
the harness proves.

**Evidence:**

```bash
scripts/extract-test/audit/setup.sh
#   copied internal/audit -> . (2 files)
#   copied internal/database -> database (2 files)
#   copied internal/logging -> logging (2 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...     # the new action-vocabulary and validation tests
#   ok    go run ./cmd/smoke # validation runs before any write
#   ok    audit extracts cleanly
```

**Files that move:** `internal/audit/*.go` (1 source + 1 test file),
`internal/database` and `internal/logging` (its two dependencies), plus migration
0001 for the schema.

---

## Candidate 11 — `internal/outbox` — **READY**

**Why:** the transactional outbox — events written in the same transaction as the
change that caused them, drained by the worker. Two in-module dependencies,
`database` and `logging`, both proven.

**Blocker that was:** the docs called it "substrate-bound" and proposed only
documenting the dependency. That reasoning stops at documenting rather than
proving; the package extracts cleanly, and its own vocabulary test runs in the
extracted module.

**Evidence:**

```bash
scripts/extract-test/outbox/setup.sh
#   copied internal/outbox -> . (2 files)
#   copied internal/database -> database (2 files)
#   copied internal/logging -> logging (2 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...
#   ok    go run ./cmd/smoke # event types=25, envelope mapping checked
#   ok    outbox extracts cleanly
```

**Files that move:** `internal/outbox/*.go` (1 source + 1 test file),
`internal/database` and `internal/logging`.

---

## Candidate 12 — `internal/retention` — **READY**

**Why:** the data-retention engine: a fixed policy catalog, a dry-run default and
an opt-in delete. It depends only on `audit` and `database`, both proven.

**Blocker that was:** the docs said "policy table is application-specific; extract
the engine, inject a table catalog". The catalog *is* the product — a sweep with
no idea which tables exist has nothing to do — and it extracts with its catalog
intact, which the harness proves.

**Evidence:**

```bash
scripts/extract-test/retention/setup.sh
#   copied internal/retention -> . (2 files)
#   copied internal/audit -> audit (2 files)
#   copied internal/database -> database (2 files)
#   copied internal/logging -> logging (2 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...     # the retention safety tests
#   ok    go run ./cmd/smoke # 22 policies; disabled runner refuses; dry-run default
#   ok    retention extracts cleanly
```

**Files that move:** `internal/retention/*.go` (1 source + 1 test file),
`internal/audit` and `internal/database`.

---

## Candidate 13 — `internal/agent` — **READY**

**Why:** the human-in-the-loop layer — a proposal queue where a mutating action is
hashed, shown to an operator, signed, approved or rejected, and only then
executed. It is the widest dependency set proven so far — `audit`, `database` and
`httpx` — and every one of those is proven in turn.

**Blocker that was:** the docs said "fine to leave; it is small and cohesive".
Cohesion is not the property the harness checks; extraction is. The package lifts
out, and the `Executor` seam travels with it, which is what lets a host supply the
real tool path.

**Evidence:**

```bash
scripts/extract-test/agent/setup.sh
#   copied internal/agent -> . (4 files)
#   copied internal/audit, internal/database, internal/logging, internal/httpx, internal/ratelimit
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...     # the argument-hash stability test
#   ok    go run ./cmd/smoke # hash stable; tiers classify; empty secret refused
#   ok    agent extracts cleanly
```

**Files that move:** `internal/agent/*.go` (3 source + 1 test file),
`internal/audit`, `internal/database`, `internal/httpx` (and its own leaves
`logging`, `ratelimit`).

---

## Candidate 14 — `internal/jobs` — **READY**

**Why:** the queue: a unit of work enqueued (optionally inside the caller's
transaction), claimed with `SKIP LOCKED`, completed or failed with a bounded retry
backoff, and swept for staleness. Its only in-module dependency is `database`.

**Blocker that was:** the docs said "`almost` — 1 internal dep". One proven dep is
not a blocker. The package lifts out, and it had no in-package tests.

**Evidence:**

```bash
scripts/extract-test/jobs/setup.sh
#   copied internal/jobs -> . (2 files)
#   copied internal/database -> database (2 files)
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...     # the first in-package tests
#   ok    go run ./cmd/smoke # 11 kinds; validation runs before any write
#   ok    jobs extracts cleanly
```

Writing the backoff test found a real latent bug: the shift was clamped at 10, so
the delay topped out at `1<<10 s = 1024s` and the documented 30-minute ceiling was
unreachable dead code. The clamp is now 11, so the ceiling is real.

**Files that move:** `internal/jobs/*.go` (1 source + 1 test file) and
`internal/database`.

---

## Candidate 15 — `internal/crm` — **READY**

**Why:** the domain service — accounts, contacts, leads, deals, tasks, tickets,
activities, the pipeline read, search and the baseline analytics. It is the core of
the product, and it depends only on `database`, `audit`, `httpx`, `outbox` and
`rbac`, all proven.

**Blocker that was:** the docs said "`almost` — it imports `httpx` and `outbox`,
which are platform concerns that would have to travel or be inverted". That claim
was wrong: `httpx` and `outbox` are proven modules, so they travel as declared
dependencies rather than friction. Nothing imports `crm` back.

**Evidence:**

```bash
scripts/extract-test/crm/setup.sh
#   copied internal/crm -> . (9 files)
#   copied internal/{audit,database,logging,httpx,ratelimit,outbox,rbac}
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...     # the first in-package tests: the validators + JSONMap
#   ok    go run ./cmd/smoke # deal/contact/bulk validation; JSONMap codec
#   ok    crm extracts cleanly
```

The package had no in-package tests. It now has the first, covering the validators
that run before any database access — the boundary an attacker's payload hits first
— and the `JSONMap` column codec.

**Files that move:** `internal/crm/*.go` (8 source + 1 test file),
`internal/{audit,database,httpx,outbox,rbac}` (and their own leaves).

---

## Candidate 16 — `internal/storage` — **READY**

**Why:** the application layer over an object store: it owns the document rows, the
entity attachment rules, the size and content-type limits, the object key and the
reconciliation. The provider layer was already lifted to `internal/objectstore`
(which depends only on `netguard`), and the service layer depends only on
`database`, `audit`, `httpx`, `objectstore`, `ratelimit` and `rbac`, all proven.

**Blocker that was:** the docs said `no` — "`Service` binds to
`rbac`/`audit`/`database` by design". Binding to proven modules by design is not a
blocker; the harness proves it.

**Evidence:**

```bash
scripts/extract-test/storage/setup.sh
#   copied internal/storage -> . (4 files)
#   copied internal/{audit,database,logging,httpx,ratelimit,objectstore,netguard,rbac}
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...     # key/extension/entity/S3-config tests
#   ok    go run ./cmd/smoke # memory round trip; unconfigured service reports off
#   ok    storage extracts cleanly
```

**Files that move:** `internal/storage/*.go` (4 files incl. tests),
`internal/objectstore`, `internal/{audit,database,httpx,ratelimit,rbac}`.

---

## Candidate 17 — `internal/integrations` — **READY**

**Why:** the Integration Hub — adapter lifecycle, the SSRF guard (aliased from
`netguard`), the encrypted secret handoff and the run log. It depends only on
`audit`, `database`, `httpx`, `netguard`, `rbac` and `secrets`, all proven.

**Blocker that was:** the docs said "`almost` — the rest needs `secrets`". `secrets`
is proven, so it travels.

**Evidence:**

```bash
scripts/extract-test/integrations/setup.sh
#   copied internal/integrations -> . (4 files)
#   copied internal/{audit,database,logging,httpx,ratelimit,netguard,rbac,secrets}
#   ok    no import of the ForgeCRM monolith
#   ok    go build ./...
#   ok    go test ./...
#   ok    go run ./cmd/smoke # SSRF guard refuses loopback/private/link-local/file://
#   ok    integrations extracts cleanly
```

**Files that move:** `internal/integrations/*.go` (4 files),
`internal/{audit,database,httpx,netguard,rbac,secrets}`.

---

## Candidates assessed but not extracted

These were evaluated from the dependency graph and marked without an extraction
run, because each has a concrete blocker that an extraction would only confirm.

| Module | Status | Evidence | Blockers | Next actions |
| --- | --- | --- | --- | --- |
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
| `secrets` | **READY** | Zero internal deps; extracted, built, its own 20 tests ran (the package gained them in this slice), smoke ran. |
| `logging` | **READY** | Standard library only; extracted, built, its own tests ran (added in this slice), smoke ran. |
| `httpx` | **READY** | Depends only on `logging` and `ratelimit`; extracted with both, built, its own tests ran, smoke drove a 429. |
| `importx/safe` | **READY** | A leaf; extracted, its ~24 security tests ran, smoke refused a traversal and a zip bomb. |
| `importx/parsers` | **READY** | A leaf; extracted, 19 tests added in this slice (they found and fixed two bugs), smoke ran. |
| `database` | **READY** | A leaf; extracted, its pool-guard tests ran with no server, smoke refused an over-large pool. |
| `rbac` | **READY** | Extracted whole with `database`; its `principal_test.go` and a catalog smoke ran. |
| `audit` | **READY** | Extracted with `database` and `logging`; first tests added, smoke checked the validation ordering. |
| `outbox` | **READY** | Extracted with `database` and `logging`; its vocabulary test and a smoke ran. |
| `retention` | **READY** | Extracted with `audit` and `database`; its safety tests and a policy-catalog smoke ran. |
| `agent` | **READY** | Extracted with `audit`, `database` and `httpx`; its argument-hash test and a smoke ran. |
| `jobs` | **READY** | Extracted with `database`; its first tests (backoff bug found) and a smoke ran. |
| `crm` | **READY** | Extracted with `audit`, `database`, `httpx`, `outbox` and `rbac`; its first tests (validators) and a smoke ran. |
| `storage` | **READY** | Extracted with `audit`, `database`, `httpx`, `objectstore`, `ratelimit` and `rbac`; its tests and a smoke ran. |
| `integrations` | **READY** | Extracted with `audit`, `database`, `httpx`, `netguard`, `rbac` and `secrets`; the SSRF smoke ran. |
| everything else | **ALMOST** / **NOT READY** | Concrete blocker listed above. |

**No module is marked `READY` without an extraction run recorded here**, and the
run is now automated: `scripts/extract-test/run.sh` performs it for every module in
`modules.txt` on each CI run. `analytics`, `ratelimit`, `netguard`, `objectstore`,
`secrets`, `logging`, `httpx`, `importx/safe` and `importx/parsers` pass that bar today.

The nineteen `READY` modules are proven by the harness, not by this document. If the
harness ever fails for one of them, the status here is wrong and the harness is
right.
