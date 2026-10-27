# ForgeCRM — Project Audit

**Audit date:** 2026-10-27
**Auditor:** engineering agent (OpenHands)
**Branch audited:** `main` @ `7cf51de` ("Initial commit")
**Repository:** https://github.com/geff1778/ForgeCRM

This document records the **verified, factual** state of the repository before any
implementation work. It deliberately separates confirmed facts from risks and proposals.

---

## 1. Confirmed facts

### 1.1 Repository contents

| Item | Verified state |
|---|---|
| Tracked files | **1** — `README.md` (10 bytes, contents: `# ForgeCRM`) |
| Source code | **None** |
| Backend | **None** |
| Frontend | **None** |
| Migrations | **None** |
| Tests | **None** |
| Docker / Compose | **None** |
| CI | **None** |
| License | **None** |
| Documentation | **None** (beyond the single-line README) |

Command evidence:

```
$ git ls-files
README.md

$ git log --oneline
7cf51de (grafted, HEAD -> main, origin/main, origin/HEAD) Initial commit

$ git rev-parse --is-shallow-repository
true

$ find . -maxdepth 2 -not -path './.git/*'
.
./README.md
```

### 1.2 Git state

- Single branch: `main` (default). No other branches local or remote.
- Shallow clone (depth 1, "grafted" commit). History before `7cf51de` is unavailable.
- Working tree clean; nothing to commit.
- Remote: `origin` → `github.com/geff1778/ForgeCRM.git`.

### 1.3 Pre-existing checks

There were **no tests, no build, and no linters** in the repository to run. The baseline
"initial test run" is therefore vacuously empty — not a pass and not a failure. There is
no technical debt in code because there is no code.

### 1.4 Audit environment (this machine)

| Tool | Version | Notes |
|---|---|---|
| Go | 1.24.5 | installed during audit (`/usr/local/go`) |
| Node.js | 24.21.0 | preinstalled |
| npm | 11.21.0 | preinstalled |
| Docker | 29.9.2 | daemon available |
| PostgreSQL | 17.11 | installed + cluster started during audit |
| GCC | 14.2.0 | available for cgo if ever needed |
| OS | Debian 13 (trixie), 4 vCPU, 15 GiB RAM | |

Database provisioned for development/testing:

```
role:     forgecrm
database: forgecrm       (dev)
database: forgecrm_test  (test)
```

---

## 2. Problems

Because the repository is empty, "problems" are really **gaps against the mission
brief**. Each is a missing capability rather than a defect in existing code.

| # | Gap | Severity |
|---|---|---|
| P1 | No data model, no migrations, no persistence layer | Blocker |
| P2 | No authentication, sessions, MFA or bootstrap process | Blocker |
| P3 | No RBAC / permission enforcement | Blocker |
| P4 | No CRM domain (contacts, accounts, deals, tasks, tickets, …) | Blocker |
| P5 | No import / migration center (the brief's headline module) | Blocker |
| P6 | No events, webhooks, outbox or job queue | High |
| P7 | No integration hub / adapter lifecycle | High |
| P8 | No audit trail or observability surface | High |
| P9 | No admin UI | High |
| P10 | No tests of any kind | Blocker |
| P11 | No deployment (Compose, reverse proxy, migrations runner) | High |
| P12 | No license — legal ambiguity for an "open-source" product | Medium |
| P13 | No security policy / docs / contribution guide | Medium |
| P14 | README is a single line; no onboarding path | Low |

---

## 3. Risks

| # | Risk | Likelihood | Impact | Mitigation taken |
|---|---|---|---|---|
| R1 | Mission scope vastly exceeds one work cycle | Certain | High | Deliver a **complete vertical slice** (Foundation → Import), keep the rest explicitly `NOT_STARTED`/`IN_PROGRESS` in `ROADMAP.md` |
| R2 | Archive handling is untrusted input; naive extraction is a remote-code/data risk | High if built carelessly | Critical | Central `safezip`/`safetar` extractors with size/file/depth limits, path-traversal rejection, no symlink following, no execution |
| R3 | Tenant data leakage via guessed IDs | Medium | Critical | `organization_id` on every tenant table + mandatory org predicate in every query + cross-tenant tests |
| R4 | Privilege escalation through role/permission handling | Medium | Critical | Permissions are data, not scattered `if` statements; explicit escalation tests |
| R5 | Secrets leaking into logs or config commits | Medium | High | Structured logging with redaction; `.env.example` only; secret scan in CI |
| R6 | Green CI hiding broken behaviour | Medium | Medium | CI runs real-Postgres integration tests; no test disabling |
| R7 | Go toolchain / PG not present for future contributors | Medium | Medium | Docker Compose is the canonical path; scripts verify the environment |
| R8 | Licensing an "open-source" product without checking dependency compatibility | Medium | Medium | **No license chosen in this cycle** — recorded as an open decision (see below) |

---

## 4. Proposals (adopted)

1. **Go modular monolith.** Single deployable API + a separate worker process sharing
   one codebase and one schema. Module boundaries enforced by package layout, not by
   network hops. Extractable to services later because cross-module calls go through
   narrow interfaces.
2. **PostgreSQL as the only mandatory stateful dependency.** The job queue, outbox and
   event log all use Postgres (`FOR UPDATE SKIP LOCKED`, transactional outbox). This
   removes Redis/RabbitMQ from the critical path and keeps operations simple — Redis is
   deliberately **not** introduced.
3. **chi + pgx + golang-migrate.** Mature, small, well-understood. Migrations embedded
   in the binary so deployment cannot drift from code.
4. **OpenAPI as the contract**, served from the binary, with the frontend using a typed
   client generated from it.
5. **Rust deferred.** No component in this cycle meets the brief's bar ("isolated
   processing of untrusted archives", "high-throughput heavy transforms"). Archive
   extraction is implemented in Go with strict limits. A Rust sidecar is documented as a
   *candidate* in `ARCHITECTURE.md` with explicit entry criteria, and is **not** created
   just to use a second language.
6. **AI and integrations are optional by construction.** With no provider keys
   configured, the API, CRM and import paths work fully; AI/MCP endpoints report
   `disabled` rather than failing.

## 5. Open decisions (not decided unilaterally)

| Decision | Why deferred |
|---|---|
| **License** | The brief says not to choose a license without checking dependency compatibility and the intended distribution model. Dependencies are AGPL/MIT/BSD-compatible, but the distribution model is the owner's call. No `LICENSE` file is added this cycle. |
| **SSO/OIDC provider** | Interface + adapter planned; concrete IdP is deployment-specific. |
| **Oracle / MSSQL / MySQL migration sources** | Adapter interfaces defined; only Postgres + SQLite + REST implemented and tested this cycle. |

---

## 6. Baseline conclusion

The project is a **greenfield repository with a name**. There is no legacy code to
preserve, no migration of existing data, and no risk of overwriting prior work. The
correct first move is to establish a real foundation — schema, auth, RBAC, CRM core,
and one end-to-end import path — with tests that prove it works against a real
PostgreSQL instance, and to be honest in `ROADMAP.md` about what remains.
