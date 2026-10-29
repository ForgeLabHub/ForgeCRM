# Organisation Definition of Done for ForgeCRM modules

This is the organisation-wide bar a module must clear before anyone may call it
"done", and a higher bar before anyone may call it "extract-ready". It exists
because the two failure modes this project has already hit are:

1. A subsystem that looks finished in the UI and delivers nothing — the webhook
   vocabulary that accepted names the dispatcher never matched.
2. A "module" that is only modular inside the monolith — the code sits in its own
   folder but cannot be built without half the repository.

Both passed review. Both were caught later, expensively. This document makes the
checklist explicit so "done" is a set of commands that were run, not an opinion.

**A green CI on the whole monolith does not satisfy this document.** A monolith
build proves the packages compile together. It does not prove any one of them can
stand alone.

---

## Part A — Required for every module

Every module, extract-ready or not, must satisfy all of the following.

### A1. Responsibility is defined

- One sentence naming what the module does.
- A second sentence naming what it deliberately does **not** do.
- Recorded in `docs/modules/<name>.md` using the 11-section template
  (`docs/modules/module-template.md`), which `make docs-check` enforces.

### A2. Public contract is documented

- Every exported type and function is either part of the supported contract or
  explicitly marked internal-by-convention.
- The contract is written down, not inferred from the source.
- If the module exposes HTTP, its routes appear in `docs/api/openapi.json` and are
  generated from the router — never hand-edited.
- The module declares its access level for each route (a permission, session-only,
  or anonymous) so `internal/openapi` can enforce it.

### A3. Dependencies are enumerated

- Internal dependencies (other `internal/…` packages) are listed.
- External dependencies (PostgreSQL, Redis, S3, SMTP, an AI provider, an outbound
  HTTP target) are listed.
- The list is compared against `go list -f '{{join .Imports "\n"}}' ./internal/<pkg>`,
  not written from memory.

### A4. No import cycles

- `internal/archtest` passes. It reads the real graph via `go list` and fails on a
  package importing `api`, on production importing `testsupport`, and on the core
  importing composition or HTTP.
- If a cycle is found, it is recorded as a blocker, not refactored silently.

### A5. No unaccounted global state

- The module takes its dependencies by construction (a `New…` that returns a
  configured value), or reads them from `context.Context` when the value is
  request-scoped (the ambient transaction).
- No package-level mutable variable carries wiring or configuration.
- `Enabled()`-style probes tolerate a nil receiver, so an unconfigured subsystem
  reports "off" instead of panicking the caller.

### A6. Unit tests

- `go test ./internal/<pkg>/...` passes with no database and no network.
- Tests exercise real code paths. A mock is used only where a real dependency
  cannot run in a unit test, and its use is justified in the test.
- A test that would pass when the feature is deleted is not a test.

### A7. At least one integration test

- An integration test exercises the module against its real external dependency
  (PostgreSQL, Redis, an HTTP sink, S3), gated by `//go:build integration` and a
  URL environment variable so the default run stays green.
- The test asserts persisted state by reading it back, not by trusting a returned
  struct — a rollback leaves the caller holding a struct describing a write the
  database never kept.

### A8. Fake adapters where they are needed

- Every external dependency the module talks to is reached through an interface or
  a constructor parameter, so a fake can be substituted.
- The fake models realistic behaviour — success, failure, timeout, unavailable —
  and is deterministic. A fake that returns success for everything is not
  acceptable: it cannot fail a test.
- The fake is documented as such. Where the module already ships a real
  in-process implementation (for example `storage.Memory`), that counts and is
  named.

### A9. `MODULE.md` exists

- `docs/modules/<name>.md` contains all 11 sections the template names.
- `make docs-check` passes for it.

### A10. Configuration needs no real secrets

- The module starts with documented defaults.
- Any secret it needs is read from the environment and never committed.
- A missing optional secret leaves the module reporting itself off, not failing
  startup.

### A11. Static checks pass

- `gofmt -l` prints nothing for the module.
- `go vet ./...` is clean.
- Frontend modules: `npm run typecheck`, `npm run lint`, `npm test` pass.

### A12. Documentation matches the code

- `make docs-check` passes: required documents exist, relative links resolve, the
  schema counts in `docs/database/schema.md` match the migrations, and every
  command cited in `docs/verification/matrix.md` exists.

### A13. The check ran on a specific SHA

- The evidence records the commit SHA the checks were run against.
- "It passed yesterday" is not evidence.

### A14. No TODO/FIXME in new code

- New code contains no `TODO`, `FIXME`, `XXX` or placeholder comment. A known gap
  is a tracked issue and a line in the module's "Limitations" section, not a
  comment in the source.

### A15. Licence and provenance

- Any new third-party dependency has a compatible licence (see "Licence policy").
- No code is copied from elsewhere without a recorded licence and origin.
- ForgeCRM's own licence is MIT (see `LICENSE`); a module extracted from it
  inherits that licence unless the owner decides otherwise, which must be an
  explicit decision recorded in an ADR.

### A16. No secrets

- `gitleaks` reports nothing for the change.
- No `.env`, key, token or password is committed.
- A secret that may have been exposed is rotated; deleting the file is not
  remediation.

### A17. CI is current

- The change is green on the CI run for its own SHA.
- A skipped or queued job is reported as such, never as "passed".

---

## Part B — Additional requirements for "extract-ready"

A module may be called **extract-ready** only when, in addition to Part A:

### B1. It builds in isolation

- Copied into a clean module (a throwaway `go.mod`, no ForgeCRM `replace`
  directives beyond declared internal dependencies), `go build ./...` succeeds.
- The command and its output are recorded in `docs/EXTRACT_READY.md`.

### B2. Its own tests run in isolation

- `go test ./...` in the clean module passes.
- If the module ships no tests of its own, it is `ALMOST`, not `READY` — a test
  that only runs inside the monolith is not evidence of a standalone module.

### B3. A standalone smoke executes

- A minimal `cmd/smoke` (or an equivalent integration test) uses only the module's
  public API and produces observable output in the clean project.
- No ForgeCRM Compose stack is required.

### B4. Hidden dependencies are surfaced

- The extraction records every package that had to travel or be stubbed. A
  dependency discovered during extraction is a finding, not an inconvenience.

### B5. The extract-test is automated

- The extraction is a script (`scripts/extract-test/…`) so it can run in CI, not a
  one-off manual session.
- A CI job (`extract-test`) runs it for every module claiming `READY`.

### B6. The status is honest

- `READY` requires B1–B5 all performed and recorded, with the SHA.
- A module whose boundary is clear but whose test/build has not been run in
  isolation is `ALMOST` with a blocker list.
- Nothing is promoted to `READY` because it "should" work.

---

## Licence policy

ForgeCRM is MIT. A new dependency is acceptable when its licence is permissive
(MIT, BSD-2/3-Clause, Apache-2.0, ISC) or a compatible weak copyleft used as a
library. Copyleft that would force ForgeCRM's own distribution terms (strong
GPL/AGPL) is not acceptable without an explicit owner decision recorded in an ADR.
Every new dependency records its name, version, licence, activity and reason in
the commit that adds it.

## Evidence table

Every module's `MODULE.md` ends with this table, filled in with real commands and
real results for the recorded SHA:

| Check | Command | Result |
| --- | --- | --- |
| Formatting | `gofmt -l ./internal/<pkg>` | |
| Vet | `go vet ./...` | |
| Unit tests | `go test -count=1 ./internal/<pkg>/...` | |
| Integration tests | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration …` | |
| Extract build | `cd /tmp/extract-<pkg> && go build ./...` | |
| Extract tests | `cd /tmp/extract-<pkg> && go test ./...` | |
| Extract smoke | `cd /tmp/extract-<pkg> && go run ./cmd/smoke` | |
| Secrets scan | `gitleaks detect` | |
| Docs | `make docs-check` | |

## Revision trigger

This document is revisited when a module is the first to be extracted, when a new
external dependency class is introduced, or when a Definition of Done gap causes a
defect to reach `main`.
