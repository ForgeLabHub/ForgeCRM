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

### `secrets` — ALMOST

- **Source SHA:** `7a6d67f`
- **Commands:** copied, built (`ok`) and a smoke hashed/verified a password.
- **Blocker:** `go test ./...` reported `[no test files]` — the package ships no
  tests of its own, so "the module's own tests run in isolation" cannot be
  satisfied.

### `logging` — ALMOST

- **Commands:** copied, `go build ./...` and `go vet ./...` both clean.
- **Blocker:** no tests, and low reuse value as a standalone module.

## Next extraction candidate

`internal/storage` is the recommended next target after the core blocks. It
already has the shape the procedure wants — an `ObjectStore` interface with a real
in-process `Memory` fake and an `S3` adapter — so the fake-adapter step (step 6)
is already satisfied. Its blocker is that `Service` imports `audit`, `database`,
`httpx`, `integrations` and `rbac`; the extraction would lift `ObjectStore`,
`Memory` and `S3` as a sub-package and leave `Service` behind. That split is the
work, and the extract-test would prove it.

## Automating this

The procedure becomes `scripts/extract-test/<module>.sh`, driven by a table of
modules with status `READY`, and a CI job `extract-test` runs it. The job fails if
a module claimed `READY` no longer builds or tests in isolation — which is the
point: extraction readiness is a property that can regress, and CI should notice.
