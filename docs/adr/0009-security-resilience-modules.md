# ADR 0009 — Security and resilience as independent building blocks, not a monolith

- **Status:** Accepted
- **Date:** 2026-10-29
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0001 (hybrid architecture), ADR 0002 (extraction triggers)

## Context

ForgeCRM is being repositioned from a single CRM product into a *hackathon backend
builder*: a kit from which a developer can lift verified components, assemble a
backend for a specific case, substitute external dependencies with fakes, run
without heavy infrastructure, and extract individual pieces into separate
projects.

Security and resilience are the parts of the system most tempting to grow into one
big subsystem — "the Security Platform" — because authorization, rate limiting,
auditing and idempotency are all "security", and because they share a request
pipeline today. The audit at `7a6d67f` shows what that tendency produced:

- Authorization is **not** a module. It is enforced inline: `rbac.Principal` holds
  a permission set, `api` middleware calls `Require`, and `crm` adds
  ownership/tenant checks. There is no `Authorizer` type, no `Decision`, and no
  policy object. It works, but it cannot be lifted or tested in isolation.
- Rate limiting **is** already a clean module (`internal/ratelimit`) with a
  `Store` interface, a real in-process `MemoryStore` and a `RedisStore`. It is the
  proof that the shape this ADR argues for is achievable here.
- Audit **is** a module (`internal/audit`) but has no verifier and no hash chain;
  the trail is append-only by a database trigger, which protects against the
  application but not against anyone who can rewrite rows with DDL rights.

A monolithic Security Platform would make all of these unextractable at once, and
would couple a project that only needs an authorizer to Redis, PostgreSQL and an
audit schema it does not use.

## Decision

ForgeCRM will **not** build a monolithic Security Platform. Security and
resilience are provided as **independent building blocks**, each designed around:

- an **interface** (the contract a consumer depends on);
- a **fake implementation** (deterministic, realistic, able to fail);
- one or more **real implementations** (the production adapter);
- **configuration** (so the block is selected, never guessed);
- **tests** (unit, integration, contract);
- an explicit **extraction boundary** (what moves, what stays).

Each block is independently selectable, independently testable, and independently
extractable. A consumer that needs only an authorizer takes only the authorizer;
it does not inherit Redis, S3 or the audit schema.

### Planned blocks

These are **plans, not implemented capabilities.** Naming them here fixes the
target and the order; it does not claim they exist.

1. **Authorization Policy Engine** — a `Subject`/`Action`/`Resource` → `Decision`
   contract with `RBACAuthorizer`, `FakeAuthorizer`, and an `AllowAllAuthorizer`
   that only demo/test may select. Planned in ADR 0010.
2. **Rate Limiter** — the existing `internal/ratelimit` generalised to the
   `Limiter` contract with `MemoryLimiter`, `RedisLimiter`, `NoopLimiter` and
   per-policy fail modes. Planned in ADR 0011.
3. **Audit Writer + Verifier** — the existing append-only writer plus a
   `HashChainVerifier` over a SHA-256 hash chain. Planned in ADR 0012.

Further blocks (idempotency, circuit breaker, timeout/retry budget) are recorded
in the roadmap and follow the same shape. They are not authorised by this ADR.

## Constraints each block must satisfy

- **Interface + fake + real.** A block with no fake is not finished; a fake that
  returns success for everything is not a fake.
- **No hidden coupling.** A block does not import `api`, `wiring` or another
  block's concrete implementation. Dependencies point at interfaces.
- **Configurable selection.** The adapter is chosen by configuration; the default
  is the safe one.
- **Demo cannot leak into production.** `AllowAllAuthorizer`, `NoopLimiter` and a
  fake audit writer are refused when `config.IsProduction()` is true. This is the
  same rule the profiles in `CONFIG_PROFILES.md` enforce.
- **Honest failure.** A block that cannot reach its store reports the condition;
  it does not report success. This mirrors the existing `ErrProviderUnavailable`
  handling and the rate limiter's `ErrUnavailable`.
- **No second source of truth.** A block integrates with the existing subsystem
  (the RBAC catalog, the audit table, the rate-limit middleware). It must not
  create a parallel authorization model or a parallel log.

## Alternatives considered

- **One `security` package** containing authorization, rate limiting, auditing
  and idempotency. Rejected: it couples unrelated dependencies (Redis for the
  limiter, a DB for audit) and makes selective reuse impossible — the opposite of
  the builder goal.
- **Leave authorization inline and only generalise rate limiting.** Rejected: the
  inline model is the hardest to test for BOLA/IDOR and the most valuable to
  extract.
- **Adopt an external policy engine (OPA/Casbin).** Rejected for now: it adds a
  large dependency and a second policy language before the existing RBAC model has
  been expressed as a contract. Revisit if the contract proves insufficient.

## Consequences

- More, smaller packages instead of one large one. Slightly more wiring in
  `wiring.Build`, in exchange for independent extraction.
- Each block needs its own fake and its own extract-test before it may be called
  ready. This is deliberate friction: it is what makes "modular" provable rather
  than asserted.
- The existing inline authorization must be migrated onto the engine carefully —
  a behavioural change in authorization is a security change. The engine's first
  implementation must reproduce the current decisions exactly, proven by tests
  that run against both.

## Revision trigger

Revisit if the contract proves too coarse to express a real rule (for example a
conditional permission), if an external engine becomes justified, or if a block's
fake cannot model a failure the production adapter can produce.
