# Configuration profiles

ForgeCRM is configured entirely from environment variables. This document
describes four intended profiles — **demo**, **development**, **hackathon** and
**production** — and, crucially, separates **what exists in the code today** from
**what is targeted**. Nothing here is implemented by this document; the profiles
are described so the work can be planned against a fixed target.

The single most important rule:

> **Demo mode must never be able to switch itself on in production.** A profile
> is a deliberate choice, not a default that leaks. Today the code enforces part
> of this already (see "What exists today"); the rest is target behaviour.

---

## What exists today (code-verified at `7a6d67f`)

- `FORGECRM_ENV` is validated to be exactly one of `development`, `test` or
  `production` (`internal/config`). There is **no `demo` and no `hackathon` value
  today** — a request for one is refused at startup.
- `Config.IsProduction()` returns `Env == "production"`.
- In production, `FORGECRM_SECRET_KEY` is **required**; startup fails without it.
- Optional subsystems degrade to "off" rather than failing:
  - `FORGECRM_AI_ENABLED=false` → the assist/MCP service reports `disabled`.
  - `FORGECRM_STORAGE_ENABLED=false` → document routes answer 503
    `not configured`, not 500.
  - `FORGECRM_SMTP_ENABLED=false` → `EmailService.Enabled()` is false.
  - `FORGECRM_TELEGRAM_ENABLED=false` → `TelegramService.Enabled()` is false.
  - No `FORGECRM_REDIS_URL` → the rate limiter uses `MemoryStore`.
- `FORGECRM_RETENTION_ENABLED` defaults to **off**, and retention defaults to
  **dry-run** even when enabled.
- The storage S3 client refuses a private/loopback endpoint unless
  `FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT=true` (SSRF guard).
- `.env.example` ships development defaults and states they are not production
  safe.

**There is no demo/hackathon profile, no seed generator, and no fake-adapter
switch in the code yet.** Where a profile below describes fakes or a `demo` mode,
it is target behaviour, marked as such.

---

## Profile: `demo`

**Goal:** run the product with no external services at all — no PostgreSQL, no
Redis, no S3, no SMTP, no AI provider — and predictable data.

### Services (target)

| Capability | Adapter | Status |
| --- | --- | --- |
| Storage | `storage.Memory` (exists) | **exists** |
| Rate limit | `ratelimit.MemoryStore` (exists) | **exists** |
| AI provider | a deterministic fake `ai.Provider` | **target** (today: `Provider` is an interface but no fake ships) |
| SMTP | a no-op or file-backed mailer | **target** (today: disabled service only) |
| Database | PostgreSQL required | **blocker** — nothing runs without a DB today |

### Constraints

- External calls are off; every adapter is in-process.
- Seed data is related and reproducible from a fixed seed.
- All state is resettable.

### Required settings

- `FORGECRM_ENV=development` (there is no `demo` value yet; a real `demo` value is
  target work).
- `FORGECRM_AI_ENABLED=false`, `FORGECRM_STORAGE_ENABLED=false`,
  `FORGECRM_SMTP_ENABLED=false`, `FORGECRM_TELEGRAM_ENABLED=false`.
- No `FORGECRM_REDIS_URL`.

### Security rules

- Demo must not be reachable by setting `FORGECRM_ENV=demo` in a production
  deployment. The target is that `production` **refuses** any demo adapter
  (a fake `ai.Provider`, a `MemoryStore`, an `AllowAllAuthorizer`) unless an
  explicit unsafe flag is set, and that flag is refused when `IsProduction()`.
- Demo data must never be written over real data: the seed generator must refuse
  to run against a database that contains non-demo rows.

---

## Profile: `development`

**Goal:** local development with real services where they are needed.

### Services

| Capability | Adapter |
| --- | --- |
| Database | real local PostgreSQL (required) |
| Rate limit | `MemoryStore` by default, or Redis if `FORGECRM_REDIS_URL` is set |
| Storage | disabled, or MinIO with `FORGECRM_STORAGE_ALLOW_PRIVATE_ENDPOINT=true` |
| AI | disabled, or a local Ollama / OpenAI-compatible endpoint |
| SMTP | disabled, or Mailpit |

### Constraints

- Dev conveniences are on: `FORGECRM_ENV=development` returns reset tokens in API
  responses. This is why it must never be used in production.
- Diagnostics and verbose logs are acceptable.

### Required settings

- `FORGECRM_ENV=development`
- `FORGECRM_DATABASE_URL=postgres://…`
- `FORGECRM_SECRET_KEY` may be a throwaway value.

### Security rules

- The dev convenience of echoing reset tokens is refused under
  `IsProduction()`.

---

## Profile: `hackathon`

**Goal:** the shortest path from clone to a running, protected API for a
demonstration. Few external dependencies, real protections kept on.

### Services (target)

| Capability | Adapter |
| --- | --- |
| Database | PostgreSQL (Docker Compose or a managed instance) |
| Rate limit | `MemoryStore` (single replica) |
| Storage | `storage.Memory` or MinIO |
| AI | a real provider if a key is supplied, otherwise a fake |
| Audit | on, real |

### Constraints

- Authentication, RBAC, tenant isolation and audit stay **on**. A hackathon
  profile is a fast profile, not an insecure one.
- `.env.example` is the entry point; the number of mandatory variables is
  minimised.

### Required settings

- `FORGECRM_ENV=development` (until a `hackathon` value exists).
- `FORGECRM_DATABASE_URL`, `FORGECRM_SECRET_KEY`.

### Security rules

- No demo adapter is enabled unless the operator opts in.
- The SSRF guard stays on for storage endpoints.

---

## Profile: `production`

**Goal:** real services only, safe defaults, mandatory secrets.

### Services

| Capability | Adapter |
| --- | --- |
| Database | managed PostgreSQL |
| Rate limit | Redis (`FORGECRM_RATE_LIMIT_STORE=redis` + `FORGECRM_REDIS_URL`) for multi-replica |
| Storage | S3-compatible (real endpoint) |
| AI | a real provider, or disabled |
| SMTP/Telegram | real, or disabled |

### Constraints

- Multi-replica correctness requires Redis (see ADR 0003).
- Retention, if enabled, must be configured with explicit windows.

### Required settings

- `FORGECRM_ENV=production`
- `FORGECRM_SECRET_KEY` — **required**; startup fails without it.
- `FORGECRM_COOKIE_SECURE=true`, `FORGECRM_BASE_URL` over HTTPS.

### Security rules

- **Demo adapters are forbidden.** Target: startup refuses `MemoryStore` as the
  rate-limit store, a fake AI provider, and any `AllowAll` authorizer when
  `IsProduction()` is true.
- Storage private endpoints are refused unless explicitly allowed, and the
  allowance is logged.
- No secret is ever written to a log or returned by an API.

---

## The demo-never-in-production rule

This is the rule the profiles exist to make enforceable:

1. A profile is selected by an explicit variable (`FORGECRM_ENV`), never inferred
   from the presence or absence of another setting.
2. `production` fails fast on any configuration that would silently weaken a
   protection:
   - missing `FORGECRM_SECRET_KEY` — **already enforced**;
   - a demo/fake adapter — **target**;
   - an insecure cookie setting — **target**.
3. A fake that returns success for everything is never acceptable, in any profile,
   because it cannot fail a test.

## Summary table

| Setting | demo (target) | development | hackathon (target) | production |
| --- | --- | --- | --- | --- |
| `FORGECRM_ENV` | `development`* | `development` | `development`* | `production` |
| Database | PostgreSQL (blocker) | local PostgreSQL | PostgreSQL | managed PostgreSQL |
| Rate limit store | Memory | Memory / Redis | Memory | Redis |
| Storage | Memory | disabled / MinIO | Memory / MinIO | S3 |
| AI | fake (target) | disabled / local | real or fake | real / disabled |
| SMTP | no-op (target) | disabled / Mailpit | disabled / real | real / disabled |
| Secrets required | no | throwaway | yes | yes (enforced) |
| Demo adapters allowed | yes | yes | opt-in | **never** |

\* A distinct `demo`/`hackathon` value for `FORGECRM_ENV` is target work; today
both would run as `development`.
