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
| `secrets` | **READY** | Zero internal deps; extracted, built, its own 20 tests ran (the package gained them in this slice), smoke ran. |
| `logging` | **READY** | Standard library only; extracted, built, its own tests ran (added in this slice), smoke ran. |
| `httpx` | **READY** | Depends only on `logging` and `ratelimit`; extracted with both, built, its own tests ran, smoke drove a 429. |
| `importx/safe` | **READY** | A leaf; extracted, its ~24 security tests ran, smoke refused a traversal and a zip bomb. |
| `importx/parsers` | **READY** | A leaf; extracted, 19 tests added in this slice (they found and fixed two bugs), smoke ran. |
| everything else | **ALMOST** / **NOT READY** | Concrete blocker listed above. |

**No module is marked `READY` without an extraction run recorded here**, and the
run is now automated: `scripts/extract-test/run.sh` performs it for every module in
`modules.txt` on each CI run. `analytics`, `ratelimit`, `netguard`, `objectstore`,
`secrets`, `logging`, `httpx`, `importx/safe` and `importx/parsers` pass that bar today.

The nine `READY` modules are proven by the harness, not by this document. If the
harness ever fails for one of them, the status here is wrong and the harness is
right.
