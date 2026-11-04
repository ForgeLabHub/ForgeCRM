# Integration completion report

**Verdict: BLOCKED** — investigation snapshot, not a release certification.

**Workspace recovery:** after an approval interaction, the execution environment re-created Git metadata, leaving local HEAD at `6484a50` while remote arena was `36e74d8`. The working files survived. Their exact tree was recorded as `83f46fec4dee5b81fd05a2ad4b19b171594aca2a` and two recovery stashes were retained. Local arena was fast-forwarded to the remote revision, and a non-rewriting merge of current main `8cceccf940d0f0ca267f1362d4e3a760d5f2ea11` was completed as `b78e151083b206cf2e9b68ad65f6330bc0906d0d`. Comparison with the saved tree showed only changes from the newer main, plus this report; no saved work was discarded.

## Git and PRs

- Initial investigated arena HEAD: `36e74d8bd85ca071fe30d6ea38825b7dbfa5e62f`; merge under review: `b78e151083b206cf2e9b68ad65f6330bc0906d0d`.
- Original main/base: `6484a50a742f3a1a1e4cadf02e8ba2f05beceda5`. Remote `main` advanced to `0c748e21d5f46e4a5c132b279eeda4491db09502`; arena and main have diverged (6 commits only on main, 24 only on arena, counting merge ancestry). The local `main` ref was not changed.
- PR #45: `delivery` → `main`, **merged** as `5584b89fba05c09c0d48ff91a8b64493db0b323b`; published checks succeeded for its then-current revision. Its historical diff spans 74 files: localization, responsive layout, create permission gating, breadcrumbs and shared modals. Its merge tree is byte-for-byte identical to delivery tip `3e0460b9c0de8b59c1bf9f52703722fd91c4fedd` (`f600322712f070e7110c2eaaa1c8af6bcd4cdaf7`).
- PR #47: `feat/frontend-redesign` → `main`, **open and conflicting**; tip `1ae759a5f08746845c8647deb512bb3d35b8f98c`; six source commits and 25 changed files as reported by GitHub. Its older CI checks succeeded, but do not establish correctness of the new main or arena tree. No automatic closure.
- PR #48: `arena/7bd19000-forgecrm` → `main`, **open draft and conflicting**. GitHub lists 24 commits, including pre-existing delivery ancestry; this is not 24 new functional changes. The tree diff from the original base is 35 files: design-system and command-palette replacement plus MCP/cookie changes, tests and documentation. CI for `36e74d8` failed visual regression twice; remaining displayed jobs succeeded. The later merge `b78e151` incorporates current main in arena; new CI and GitHub conflict status must be checked after push.
- `2c0d495` merges parents `6484a50` and `3e0460b` using `ours`: its tree equals `6484a50` exactly, so the second parent contributed ancestry, not file changes. `055fd3c` merges `2c0d495` and redesign `1ae759a`; its first-parent tree diff contains 23 files (design-system components, palette and shared lists, shell, styling, dictionaries and tests). Conflict resolution was performed in the prior session; it is not a pristine copy of the redesign branch.
- `git merge-tree --write-tree origin/main HEAD` previews one conflict: `docs/verification/project-facts.md`; `AGENTS.md` auto-merges. After explicit user approval, the regenerated facts file was recovered from the saved worktree, and latest main was merged without rebase or rewriting history.

## Delivery matrix

The identical PR #45 merge and delivery-tip trees establish that the full delivery tip was already integrated by PR #45, before the `ours` merge. Subsequent main and arena work may modify the same features, so presence does not imply regression-free behavior.

| Delivery feature | In main | In arena | In PR #45 | Transfer from old delivery |
|---|---|---|---|---|
| RU/EN localization and shared components | yes, with later changes | yes, with redesign changes | yes | none identified; test regressions |
| Responsive tables, mobile drawer and layouts | yes, with later changes | yes, with redesign changes | yes | none identified; visual CI fails |
| Permission-gated creation, create hook | yes | yes | yes | none identified |
| Record breadcrumbs | yes | yes | yes | none identified |
| Shared modal form dialogs | yes | yes | yes | none identified |
| Responsive E2E coverage | yes, later updated | yes | yes | none identified |

There are no file differences between `5584b89` and `3e0460b`. Copying delivery over current main would instead remove later main improvements, including security and visual-test changes. Do not copy it wholesale.

## Redesign and regression evidence

The redesign replaces `GlobalSearch` and its tests with `CommandPalette` and tests, and introduces `DataTable`, `ListLayout`, controls, overlays, icons and navigation. Retention of every route, permission, accessible interaction and responsive state has not been verified exhaustively. The earlier prepared merge tree passed frontend lint (21 warnings), typecheck, 257 unit tests and production build locally; it preceded the latest main dependency changes. E2E and visual were not run locally on the new merge.

CI runs [push](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41746576542) and [PR](https://github.com/ForgeLabHub/ForgeCRM/actions/runs/41746583418) for `36e74d8` show successful backend, frontend, E2E, security, extraction and portability jobs, but both visual jobs failed. The failing step was “Compare against the committed baselines”. The user supplied log excerpts reporting that the API became ready, all eight visual tests failed `toHaveScreenshot` comparisons, and the pixel differences were large. The decisive failure was visual comparison, not API startup; a prior PostgreSQL missing-database message cannot be conclusively classified from the excerpts alone. Detailed logs and the uploaded visual-report artifact could not be fetched in this environment. The screenshots must be reviewed to distinguish intended redesign changes from unintended regressions. Do not blindly update baselines.

## MCP and credentials

- Existing HTTP endpoint: `POST /api/v1/mcp`, JSON-RPC `initialize`, `ping`, `tools/list`, `tools/call`, notifications and bounded batches (at most 100 items; 1 MiB body limit). Advertised capability is tools only. The registry includes search accounts, account summary, list tasks, pipeline overview, create task/ticket, update deal stage and get report; mutating tools require confirmation and pass through authorization.
- Supported version strings in code: `2025-06-18`, `2025-03-26`, `2024-11-05`. These strings are not proof of full transport or specification compliance. Resources and prompts are not implemented. The endpoint does not implement complete Streamable HTTP session lifecycle, GET/DELETE or legacy SSE. MCP OAuth/discovery/PKCE is not implemented; OIDC login is a separate feature.
- Code inspection found JSON-RPC validation gaps: an omitted `jsonrpc` member is accepted, and request ID types are not validated. Full protocol/security audit, concurrent requests, cancellation and current official-spec comparison remain outstanding.
- MCP session authentication applies CSRF on mutations; `Authorization: Bearer` and `X-API-Key` use the same scoped `fcmcp_` credential service. Tokens are generated using cryptographic randomness, stored as hashes, scoped to an owner and organization, optionally expire, and support revocation and rotation; lifecycle operations are audited and requests are rate limited. Tenant/permission checks exist in code, but their entire threat model and new header path need current integration verification.

## Session cookies and consent

Session cookies are HttpOnly, path `/`, optionally domain-scoped, persistent until their expiry, and Secure in production. Default SameSite is Lax; Strict and None are configurable. None and Partitioned require Secure; Partitioned is opt-in. Session-authenticated mutations require a CSRF header validated against the session. Existing unit tests cover some but not all browser, cross-site and lifecycle combinations.

CRM contact consent is not cookie consent. No functional cookie-consent policy/status/preferences subsystem or category-based cookie enforcement was found; acceptance, rejection, selection, withdrawal and persistence are **not implemented or verified**. Do not represent a banner or unrelated contact-consent screen as cookie consent. Technical controls need independent legal review before any compliance claim.

## PostgreSQL and verification limits

`.github/workflows/ci.yml` uses a PostgreSQL 17 service and runs `go test -count=1 -p 1 -tags=integration ./...`, in addition to unit/race, vet, build and empty-database migration steps. The backend CI job passed for `36e74d8`. The run is evidence for that revision, not for the subsequent merged tree, and does not by itself prove every individual rollback/concurrency/down-migration scenario. PostgreSQL and Docker are unavailable locally. No SQLite substitute is represented here as PostgreSQL evidence.

Frontend CI includes real API/browser E2E; portability includes PostgreSQL 17 upgrades, backup/restore and smoke tests; security runs gitleaks, govulncheck and gosec. Visual failed. On the earlier prepared merge tree, Go unit tests, archtest, deploycheck, vet, build, OpenAPI/facts checks, docs checks and forge-check passed locally. After merging newer main, docs checks, claims checks and forge-check passed; the full suite has not yet run on this merge. The Go test setup used external dependency substitutions (including a SQLite stub); it is not PostgreSQL integration evidence. Local visual/E2E, real PostgreSQL integration and security tooling have not been run against the merge tree. Detailed current-SHA step logs and artifacts were inaccessible through the allowed network hosts.

## Findings and next actions

- **P0:** none established; absence of evidence is not a security certification.
- **P1:** arena and remote main diverged; PR #48 conflicts. A non-rewriting merge of current `origin/main` into arena was completed after preserving the recovered worktree. Recheck mergeability and all CI on the pushed final SHA; do not modify main directly.
- **P1:** visual CI failed twice. Obtain the visual-report artifact or allow access to GitHub Actions log/artifact hosts; review actual diffs and fix regressions or deliberately regenerate and review approved baselines.
- **P1:** MCP OAuth, complete transports and cookie consent are absent; they remain out of any release-readiness claim until implemented and tested.
- **P2:** JSON-RPC envelope and ID validation need negative tests and correction. Expand credential tenant/auth integration tests and cookie attribute/browser combinations.
- **P2:** review cross-branch redesign changes for route, permission, accessibility and responsive regressions. Verify real PostgreSQL integration against the final commit.
- **P3:** reconcile generated documentation and repeat full project gates on the final tree. Establish official MCP spec evidence and legal review for consent.

This report is an investigation snapshot. Merge commit `b78e151` was created on arena after user approval. The current report is a separate change; CI on its final SHA is pending. No PR closure, merge into main, production access or branch deletion was performed during this investigation. PR #48 must remain draft until conflicts, failing visual tests and scope limitations have been reviewed.
