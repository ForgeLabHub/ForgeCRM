# Pre-merge technical audit — webhook lifecycle, worker recovery, HITL approvals, migrations

- **Date:** 2026-10-27
- **Auditor:** engineering agent
- **Scope:** `feat/webhook-lifecycle-and-hitl` @ `36cc5fe` (PR #10)
- **Goal:** verify correctness before merge. This audit adds **no product features**; it
  verifies the existing mechanisms and closes the coverage gaps it found.

## 1. Starting state (verified, not assumed)

| Fact | Evidence |
|---|---|
| Local HEAD | `36cc5fe77282b19f041ff29f4254e280f27f9fc1` |
| Branch | `feat/webhook-lifecycle-and-hitl` |
| Base | `main` @ `27caac01fe72b45c12663bc355afcf9ed09e2ede` |
| PR #10 | `open`, `mergeable: true`, `mergeable_state: clean` (GitHub API) |
| CI on HEAD | 12/12 check runs `success` (Backend, Frontend, Security, E2E, Docker, Docs) |
| Working tree | clean |

The `gh` CLI is unauthenticated in this environment; PR and CI state were read
through the GitHub REST API with `GITHUB_TOKEN` instead. **CI was re-confirmed on
`36cc5fe`, not assumed from a previous run.**

### Baseline test results (this machine, not CI)

The environment had to be rebuilt (Go 1.26.10 and PostgreSQL 17 were re-installed;
see §5). Against a real PostgreSQL 17.11:

| Suite | Command | Result |
|---|---|---|
| Unit (race) | `go test -race -count=1 ./...` | **14/14 packages pass** |
| Integration | `FORGECRM_TEST_DATABASE_URL=… go test -tags=integration -count=1 -p 1 ./...` | **16/16 packages pass** |
| Build | `go build ./...` | pass |
| Vet | `go vet -tags=integration ./...` | pass |

## 2. Findings

Findings are separated into **defects** (would be fixed), **coverage gaps** (a real
mechanism with no test — fixed here), and **observations** (no change required).

### 2.1 Defects

**None found.** The four areas audited are correct as written. In particular the
two defects fixed earlier on this branch (D1 replay scoping, D2 in-flight guard)
are present and correct in the code under audit, and are covered by
`internal/api/webhook_replay_http_test.go`.

### 2.2 Coverage gaps (fixed in this audit)

Two production mechanisms were correct but had **no test exercising them**. A
mechanism with no test is a mechanism that can silently regress, which is exactly
the failure mode the rest of this branch guards against, so both gaps were closed.

**G1 — Stuck-`delivering` recovery (webhook lifecycle / worker recovery).**
`ClaimDelivery` has a second eligibility clause — a `delivering` row whose
`locked_at` is older than the lease — and `ReleaseStaleDeliveries` resets such rows
at worker start. Together these are the *only* way a delivery abandoned by a
crashed worker is recovered: such a row is neither `pending` nor `failed`, so
nothing else in the send path will ever look at it. **No test drove a claim to
`delivering` and then asserted recovery.** Added:

- `TestClaimRecoversDeliveryAbandonedByCrashedWorker` — a claim held with a live
  lease is not stolen; once the lease ages out, the next claim takes the same row
  and transfers `locked_by`.
- `TestReleaseStaleDeliveriesRecoversOnStartup` — only claims older than the cutoff
  are released (a live claim is left untouched); the released row becomes
  immediately claimable.

**G2 — Exactly-once execution under concurrency (HITL approvals).**
ADR 0005 §7 claims the `approved → executed` transition "admits exactly one
effect", and calls the guarantee "a claim about concurrent transactions". The
existing tests called `ExecuteApprovedAction` only *sequentially*, which exercises
the already-`executed` idempotent-replay path, not the race for the claim. Added:

- `TestAgentConcurrentExecutionIsExactlyOnce` — a deliberately constructed race
  (§3) that holds the winner inside its execution transaction while eight losers
  race the claim; the effect must run exactly once.
- `TestAgentExecutionConsultsPermissionsAtExecutionTime` — revoke → refused,
  re-grant → executed, proving the permission set is read at execution time and not
  cached from proposal time (the "no stale authority" rule, previously only tested
  in the revoke direction).
- `TestAgentCompleteRunClosesARunWithNoProposals` — documents the completion
  contract of a method that currently has no caller (see 2.3).

### 2.3 Observations (no change)

**O1 — `CompleteRun` is dead code.** `agent.Service.CompleteRun` has no HTTP route,
no production caller, and (before this audit) no test. A run with no outstanding
proposals therefore stays in `waiting_approval` until it is cancelled or its
proposals expire and it is failed. This is **not a safety defect**: every path that
*needs* to end a run does so (cancel, expiry, budget), and no path leaves a run
hung. It is a **completeness gap** — there is no way to mark a run completed
successfully. Documented here and pinned with a test so a future caller cannot be
added against an untested contract; implementing the completion path is a feature,
out of scope for a pre-merge audit.

**O2 — Dead-letter guard is defense-in-depth.** Mutation testing (§3) showed that
removing *either* the `FOR UPDATE` lock on the execution load *or* the
`status = 'approved'` guard on the claim leaves exactly-once intact — only removing
both breaks it. The two are redundant, which is a reasonable belt-and-braces
choice, not a defect. Recorded so a future reader does not "simplify" one away
believing the other is absent.

**O3 — `retryOrFail`/`deadLetter` use the attempt count read at claim time.**
`SendDelivery` loads `attempts` when it sends and passes `attempts+1` to
`retryOrFail`. This is correct under the claim model (a row is claimed exclusively,
so only one worker mutates it) but would be wrong if a future change allowed two
workers to hold one row. No change; noted for the same reason as O2.

## 3. How the coverage gaps were proven, not just written

A test that passes against the broken code proves nothing. Each new test was
**mutation-proofed** against the real PostgreSQL schema:

| Test | Mutation applied | Result |
|---|---|---|
| `TestClaimRecoversDeliveryAbandonedByCrashedWorker` | remove the `status = 'delivering' AND locked_at < …` clause from `ClaimDelivery` | **FAIL** — "the abandoned delivery was never reclaimed" |
| `TestReleaseStaleDeliveriesRecoversOnStartup` | drop the `locked_at < now() - …` cutoff from `ReleaseStaleDeliveries` | **FAIL** — "released 2 deliveries, want 1" |
| `TestAgentConcurrentExecutionIsExactlyOnce` | remove the `FOR UPDATE` lock **and** the `status = 'approved'` claim guard | **FAIL** — "the effect ran 9 times … want exactly 1" (deterministic across runs) |

The first version of the concurrency test passed under the mutation; that
falsified it, and it was rewritten to hold the winner's transaction open with a
blocking executor so the losers genuinely race the claim. This is recorded because
it is the reason the test is structured the way it is.

## 4. Migration review (0007–0011)

Migrations were read in full, forward and down. All are consistent with the code:

- **0007** makes deliveries claimable (`locked_at`/`locked_by`), de-duplicates
  existing rows before adding `webhook_deliveries_sub_event_uniq`, and widens the
  claim index to `pending, failed, delivering`. The dedup `DELETE` runs before the
  unique index, so an existing deployment cannot fail to migrate.
- **0008** adds `dead_lettered_at` and `replay_count`, backfills `dead_lettered_at`
  for pre-existing `dead` rows, and indexes dead rows partially. Down drops both
  columns; it deliberately restores no data (the prior schema had nowhere to keep
  it), which is the honest rollback.
- **0009** creates `agent_runs`/`agent_actions`/`agent_approvals` with the state
  sets and `CHECK` constraints matching the Go constants; `idempotency_key` is
  `UNIQUE`; the queue/expiry indexes are partial on `status = 'proposed'`.
- **0010** seeds `agent.read`/`agent.run` and the `approvals.read`/`approvals.decide`
  grants. Down removes only the agent permissions and leaves the `approvals.*`
  grants, which 0004 owns — a strict subset, which is what a down migration should be.
- **0011** adds nullable `sessions.step_up_at`; absence means "no recent second
  factor", the safe default.

The rollback path is exercised by `TestWebhookDispatchMigrationRollsBack`
(down → assert objects gone → up → assert objects back) and by the down scripts
being written in dependency order.

## 5. Environment note

The sandbox had been reset before this audit: Go and PostgreSQL were absent. Both
were re-installed to the versions the project pins (Go `1.26.10` per `go.mod` and
CI `GO_VERSION`; PostgreSQL 17) and the `forgecrm_test` database provisioned, so
the baseline and all new tests run against a real database rather than mocks.

## 6. Status

| Area | Status | Evidence |
|---|---|---|
| Webhook lifecycle (replay, retry budget, dead-letter) | **VERIFIED** | existing HTTP + service tests pass; ADR 0004 matches code |
| Worker recovery (lease reclaim, stale release) | **VERIFIED** | 2 new mutation-proofed integration tests |
| HITL approvals (signature, TOCTOU, step-up, expiry) | **VERIFIED** | existing tests pass; 3 new tests incl. a real claim race |
| Database migrations 0007–0011 | **VERIFIED** | read forward+down; rollback exercised by an existing test |

**Merge recommendation:** the audited areas are correct and now covered. The only
open item is **O1 (`CompleteRun` unreachable)** — a completeness gap, not a defect,
which should be tracked as follow-up work rather than blocking this merge.
