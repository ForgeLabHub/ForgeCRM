# Extraction testing

A reproducible procedure for proving that a module can stand alone. The rule this
document enforces is the one in `ORG_DOD.md`: **a module is not extract-ready until
it has been copied into a clean project and built, tested and run there.** A
green monolith build is not evidence.

This document defines the procedure and records the first executions. It does not
implement a universal extraction CLI — that is later work.

## Procedure

For a candidate module `M` at ForgeCRM commit `SHA`:

1. **Record the source SHA.**
   ```bash
   git -C /workspace/project/ForgeCRM rev-parse HEAD
   ```
2. **Create a clean project.**
   ```bash
   T=/tmp/extract-M
   rm -rf "$T" && mkdir -p "$T"
   ```
3. **Copy only the module's files.** Non-test and test files of the package, and
   nothing else unless it is a *declared* dependency:
   ```bash
   cp backend/internal/M/*.go "$T"/
   ```
4. **Remove references to ForgeCRM internal packages.** If any import still names
   `github.com/geff1778/forgecrm/internal/…`, the module is not independent: that
   import either travels (add it and repeat) or must be inverted behind an
   interface. Record which.
5. **Provide a `go.mod`** with a throwaway module path and only the third-party
   requirements the module actually needs, pinned to versions from the module
   cache:
   ```bash
   cd "$T" && printf 'module example.com/M\n\ngo 1.26.10\n' > go.mod
   ```
6. **Add fake adapters** for any external dependency so the module runs with no
   real service.
7. **Build:** `go build ./...`
8. **Unit tests:** `go test -count=1 ./...`
9. **Integration/smoke:** run the module's integration test if it needs a real
   service, or a `cmd/smoke` program that exercises the public API:
   ```bash
   mkdir -p cmd/smoke   # a main.go using only the module's public API
   go run ./cmd/smoke
   ```
10. **Confirm no ForgeCRM Compose is required.** The smoke must pass with no
    PostgreSQL/Redis/S3 unless the module's whole purpose is that service.
11. **Record the result** in `docs/EXTRACT_READY.md` with both SHAs (source
    ForgeCRM SHA and the test project's state) and the exact commands and output.

**The procedure is not successful until step 11 is written down.** An extraction
that ran but was not recorded does not count.

## What counts as a hidden dependency

A hidden dependency is any import or runtime requirement discovered during
extraction that the module's documentation did not list. Common ones:

- a shared `httpx` helper for error responses;
- `database` for a transaction;
- a `logging` redactor;
- a `config` struct;
- an ambient `context` value another package sets.

Each is a finding. The extraction either carries it (and the module's dependency
list grows) or inverts it behind an interface (and the module's boundary shrinks).
Both are acceptable outcomes; silently importing half the monolith is not.

## Executed extractions

### `analytics` — READY

- **Source SHA:** `7a6d67f`
- **Internal imports:** none.
- **Commands:**
  ```bash
  cp backend/internal/analytics/*.go /tmp/extract-analytics/
  cd /tmp/extract-analytics && printf 'module example.com/analytics\n\ngo 1.26.10\n' > go.mod
  go build ./...          # ok
  go vet ./...            # ok
  go test -count=1 ./...  # ok  example.com/analytics  0.006s
  go run ./cmd/smoke      # smoke: 49.98
  ```
- **Result:** built, tested and ran standalone. No ForgeCRM package, no database.
- **Hidden dependencies found:** none.

### `ratelimit` — READY (memory path)

- **Source SHA:** `7a6d67f`
- **Internal imports:** none.
- **Commands:**
  ```bash
  cp backend/internal/ratelimit/*.go /tmp/extract-ratelimit/
  # go.mod pins go-redis v9.23.0 and golang.org/x/sys v0.49.0 from the cache
  go build ./...   # ok
  go test -count=1 -run 'TestMemory|TestLimiter|TestFail|TestDisabled|TestStoreError' ./...
  # ok  example.com/ratelimit  0.026s
  ```
- **Result:** built and its memory-path unit tests passed standalone.
- **Not verified in isolation:** `redis_integration_test.go` (build tag
  `integration`) needs a live Redis; the Redis code path compiled but was not
  executed in the clean project. Recorded, not glossed over.

### `netguard` — READY

- **Internal imports:** none (standard library only).
- **Commands:** `scripts/extract-test/netguard/setup.sh` — `go build`, `go test`,
  and a smoke that asserts loopback, link-local metadata and `file://` are refused
  while a public literal address is allowed.
- **Result:** built, tested and ran standalone with no dependency at all.
- **Hidden dependencies found:** none. The setup asserts the `go.mod` has no
  `require` block, so a third-party dependency would fail the extraction.

### `objectstore` — READY

- **Internal imports:** `netguard` only, copied into a subdirectory of the extracted
  module and re-pointed at it.
- **Commands:** `scripts/extract-test/objectstore/setup.sh` — `go build`, `go test`
  (the shared provider contract runs against `Memory` and against a real S3 wire
  protocol via `gofakes3`), and a smoke that stores, reads, lists, presigns and
  deletes an object.
- **Result:** built, tested and ran standalone. No database, no audit log, no RBAC,
  no HTTP error type.
- **Hidden dependencies found:** none.

### `secrets` — READY

- **Internal imports:** none (`golang.org/x/crypto` only).
- **Commands:** `scripts/extract-test/secrets/setup.sh` — `go build`, `go test`
  and a smoke that hashes, verifies, rejects a mismatch and round-trips a value
  through the keyring.
- **Result:** built, tested and ran standalone. The package shipped no tests
  before this slice; it now ships 20, and they run in the extracted project.
- **Hidden dependencies found:** none.

### `logging` — READY

- **Internal imports:** none (standard library only).
- **Commands:** `scripts/extract-test/logging/setup.sh` — `go build`, `go test` and
  a smoke that logs a secret through `New` and asserts it is absent from the
  output.
- **Result:** built, tested and ran standalone. The package shipped no tests
  before this slice; it now ships tests pinning the redaction control.
- **Hidden dependencies found:** none.

### `httpx` — READY

- **Internal imports:** `logging`, `ratelimit` (both proven leaves, copied into
  the extracted module).
- **Commands:** `scripts/extract-test/httpx/setup.sh` — `go build`, `go test` and a
  smoke that round-trips a JSON list response, drives a rate-limited handler to a
  429 (with the limit header on the allowed response) and resolves a client
  address.
- **Result:** built, tested and ran standalone. The package's own tests, including
  the client-address trust tests, run in the extracted module.
- **Hidden dependencies found:** none.

### `importx/safe` — READY

- **Internal imports:** none.
- **Commands:** `scripts/extract-test/safe/setup.sh` — `go build`, `go test` and a
  smoke that round-trips a ZIP, refuses a path traversal and refuses a zip bomb.
- **Result:** built, tested and ran standalone; the package's ~24 security tests
  (traversal, symlink, zip bomb, deep nesting, corrupt data) run in the extracted
  module.
- **Hidden dependencies found:** none.

### `importx/parsers` — READY

- **Internal imports:** none.
- **Commands:** `scripts/extract-test/parsers/setup.sh` — `go build`, `go test` and
  a smoke that parses a CSV and a JSON export.
- **Result:** built, tested and ran standalone. The package had no tests; 19 were
  added in this slice, and they found two real bugs — XLSX was rejected by the
  UTF-8 check applied to every format, and the XML parser emitted a record's
  attributes and nested elements as separate rows. Both are fixed.
- **Hidden dependencies found:** none.

### `database` — READY

- **Internal imports:** none.
- **Commands:** `scripts/extract-test/database/setup.sh` — `go build`, `go test`
  and a smoke that confirms an over-large pool is refused before any connection.
- **Result:** built, tested and ran standalone. Its pool-guard tests need no
  server, so the extracted module is green everywhere.
- **Hidden dependencies found:** none.

### `rbac` — READY

- **Internal imports:** `database` (extracted alongside it).
- **Commands:** `scripts/extract-test/rbac/setup.sh` — `go build`, `go test` and a
  smoke that drives the catalog, the principal checks and the owner's permission
  set.
- **Result:** built, tested and ran standalone, whole rather than split — the
  catalog, `Principal` and `Service` already lift out together, so the split the
  docs once proposed is a code change and not a boundary change.
- **Hidden dependencies found:** none beyond `database`.

### `audit` — READY

- **Internal imports:** `database`, `logging` (extracted alongside it).
- **Commands:** `scripts/extract-test/audit/setup.sh` — `go build`, `go test` and a
  smoke that confirms the validation runs before any write.
- **Result:** built, tested and ran standalone. Its first tests were added in this
  slice (action vocabulary, validation ordering, marshalling helpers). Its schema
  lives in migration 0001 and is not part of the package — a real extraction would
  carry a migration with it.
- **Hidden dependencies found:** none in the Go package.

### `outbox` — READY

- **Internal imports:** `database`, `logging` (extracted alongside it).
- **Commands:** `scripts/extract-test/outbox/setup.sh` — `go build`, `go test` and a
  smoke that drives the event vocabulary and the event-to-envelope mapping.
- **Result:** built, tested and ran standalone; its vocabulary test runs in the
  extracted module.
- **Hidden dependencies found:** none.

### `retention` — READY

- **Internal imports:** `audit`, `database` (extracted alongside it).
- **Commands:** `scripts/extract-test/retention/setup.sh` — `go build`, `go test`
  and a smoke that checks the 22-policy catalog and the safety posture.
- **Result:** built, tested and ran standalone. The smoke caught a real fact
  while it was written — an expiry-driven policy legitimately carries a zero
  window, and a protected policy has no default on purpose — which the smoke now
  encodes instead of assuming away.
- **Hidden dependencies found:** none.

### `agent` — READY

- **Internal imports:** `audit`, `database`, `httpx` (and `httpx`'s leaves
  `logging`, `ratelimit`), all extracted alongside it.
- **Commands:** `scripts/extract-test/agent/setup.sh` — `go build`, `go test` and
  a smoke that drives the argument hash, the risk tiers and the secret guard.
- **Result:** built, tested and ran standalone; the argument-hash stability test —
  the property the whole approval flow rests on — runs in the extracted module.
- **Hidden dependencies found:** none. The `Executor` seam travels with the
  package, which is what lets a host supply the real tool path.

### `jobs` — READY

- **Internal imports:** `database` (extracted alongside it).
- **Commands:** `scripts/extract-test/jobs/setup.sh` — `go build`, `go test` and a
  smoke that checks the kind/status vocabularies and the pre-write validation.
- **Result:** built, tested and ran standalone. Writing the backoff test found a
  real latent bug: the shift was clamped at 10, so the retry delay topped out at
  1024s and the documented 30-minute ceiling was unreachable dead code.
- **Hidden dependencies found:** none.

### `crm` — READY

- **Internal imports:** `audit`, `database`, `httpx`, `outbox`, `rbac` (all
  extracted alongside it).
- **Commands:** `scripts/extract-test/crm/setup.sh` — `go build`, `go test` and a
  smoke that drives the validators and the `JSONMap` codec.
- **Result:** built, tested and ran standalone. The package had no in-package
  tests; it now has the first, covering the validators that run before any
  database access.
- **Hidden dependencies found:** none. The docs' claim that `httpx` and `outbox`
  were "the real friction" was wrong — both are proven, so they travel.

### `storage` — READY

- **Internal imports:** `audit`, `database`, `httpx`, `objectstore`, `netguard`,
  `ratelimit`, `rbac` (all extracted alongside it).
- **Commands:** `scripts/extract-test/storage/setup.sh` — `go build`, `go test`
  (the key/extension/entity/S3-config tests) and a smoke that drives the memory
  store round trip and the unconfigured-service posture.
- **Result:** built, tested and ran standalone.
- **Hidden dependencies found:** none.

### `integrations` — READY

- **Internal imports:** `audit`, `database`, `httpx`, `netguard`, `ratelimit`,
  `rbac`, `secrets` (all extracted alongside it).
- **Commands:** `scripts/extract-test/integrations/setup.sh` — `go build`,
  `go test` and a smoke that drives the SSRF guard's address and URL decisions.
- **Result:** built, tested and ran standalone. The guard refuses loopback,
  private and link-local addresses and a `file://` URL before any dial.
- **Hidden dependencies found:** none.

### `secrets` — ALMOST (superseded by READY above)

- **Source SHA:** `7a6d67f`
- **Commands:** copied, built (`ok`) and a smoke hashed/verified a password.
- **Blocker (now cleared):** `go test ./...` reported `[no test files]` — the
  package shipped no tests of its own, so "the module's own tests run in isolation"
  could not be satisfied. The package now ships `secrets_test.go`.

### `logging` — ALMOST (superseded by READY above)

- **Commands:** copied, `go build ./...` and `go vet ./...` both clean.
- **Blocker (now cleared):** no tests. The package now ships `logging_test.go`,
  which pins the redaction control.

## Next extraction candidate

The two `importx` sub-packages, `safe` and `parsers`, are now extracted and
proven; they were the cheapest remaining targets because both are leaves with no
in-module dependency, and running the parsers extraction is what surfaced its two
real bugs.

The next candidates, in order of how much the boundary has to change first, are in
`docs/EXTRACT_READY.md`: `rbac` (split the pure catalog and `Principal` from the
DB-backed service), `retention` (extract the engine, inject a table catalog), and
`integrations` (the SSRF guard is already out; the rest needs `secrets` and
`audit`).

## Automating this

The procedure is implemented as `scripts/extract-test/`, driven by the table in
`scripts/extract-test/modules.txt`. Each module has a `setup.sh` that performs
steps 3–10 of the procedure above; `run.sh` runs every module's setup and fails if
any of them fails. A CI job `extraction` runs it, and `internal/archtest`'s
`TestExtractionBoundaryHolds` enforces the same boundaries from the import graph on
every `go test ./...`.

The job fails if a module claimed `READY` no longer builds or tests in isolation —
which is the point: extraction readiness is a property that can regress, and CI
should notice. The harness was itself verified to fail: replacing `objectstore`'s
`netguard` import with `integrations` made both the harness and the archtest report
the violation, and giving `logging` an in-module import made the archtest fail.

`modules.txt` lists thirty-two modules today: `netguard`, `analytics`,
`ratelimit`, `objectstore`, `secrets`, `logging`, `httpx`, `safe`
(internal/importx/safe), `parsers` (internal/importx/parsers), `database`, `rbac`,
`audit`, `outbox`, `retention`, `agent`, `jobs`, `crm`, `storage`, `integrations`,
`config`, `workflow`, `events`, `migratex`, `simulate`, `forecast`, `risk`,
`notify`, `ai`, `importx`, `migrations`, `migrate` and `authn`.

See `scripts/extract-test/README.md` for how to add a module.
