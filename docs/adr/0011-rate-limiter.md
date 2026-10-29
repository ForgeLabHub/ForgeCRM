# ADR 0011 — Pluggable rate limiter

- **Status:** Proposed (plan; the module partly exists)
- **Date:** 2026-10-29
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0009 (security as independent building blocks), ADR 0003 (multi-replica safety)

## Context

`internal/ratelimit` already exists and already has the shape this ADR argues for:
a `Store` interface, a `MemoryStore` (single process, swept, bounded), a
`RedisStore` (atomic Lua, shared across replicas) and a `Policy` with a `FailMode`
(`fail-open` / `fail-closed`). `httpx.RateLimitMiddleware` consumes it. It is the
one security concern in ForgeCRM that is already a clean, extractable block, and
it is the model the other blocks follow.

Two gaps remain against the target:

1. There is no `NoopLimiter` for explicitly-disabled scenarios, and no explicit
   `Limiter` contract distinct from the concrete `Limiter` type.
2. Policies are partly configuration-driven (`FORGECRM_RATE_LIMIT_PER_MIN`,
   `FORGECRM_LOGIN_RATE_LIMIT_PER_MIN`) but not yet a named policy catalog.

## Proposed contract

```go
type Policy struct {
	Name      string
	Limit     int
	Window    time.Duration
	FailMode  FailMode
	KeyPrefix string
}

type Result struct {
	Allowed   bool
	Remaining int
	ResetAt   time.Time
	FailMode  string
}

type Limiter interface {
	Allow(ctx context.Context, key string, policy Policy) (Result, error)
}
```

The existing `Store.Increment(ctx, key, window, now)` stays as the storage seam;
`now` is passed in so window boundaries are testable without a real clock, which
the current tests already rely on.

## Implementations

- `MemoryLimiter` — the current `MemoryStore` behind the `Limiter` contract.
  Bounds its key count and sweeps expired keys.
- `RedisLimiter` — the current `RedisStore`. Coordinates several instances so a
  limit is shared, not per-process. Redis is **optional**: the memory path needs
  no Redis module at all (proven by the extract-test in `EXTRACT_READY.md`).
- `NoopLimiter` — allowed only when explicitly selected, and refused under
  `config.IsProduction()`.

## Policies

Policies are configuration, not constants buried in middleware. The named set:

- `login`, `otp_send`, `otp_verify`, `password_reset` — the authentication
  surface, where fail-closed is defensible;
- `api_read`, `api_write` — the general API;
- `webhook` — inbound delivery;
- `ai_requests` — the AI surface (already enforced per-organisation in `ai`).

Initial values come from configuration with safe defaults. `FailMode` is chosen
per policy: **fail-closed only for explicitly critical routes** (login, OTP,
password reset). An unreachable Redis must **not** block the general API — a
rate-limiter outage becoming a full outage is the failure this rule prevents, and
it is already reflected in the current tests (`TestDegradedFailOpenServesTheRequest`).

## Required tests

- over-limit is refused; under-limit is allowed;
- the window resets after expiry; `ResetAt` is correct;
- concurrent requests for one key yield distinct counts (race-free);
- different identities, IPs and tenants do not share a budget;
- an unreachable store with fail-open serves the request; with fail-closed it
  refuses;
- `MemoryLimiter` and `NoopLimiter` behave as named;
- a store error is reported as an error, not as `rate_limited`.

Run with `-race`.

## Compatibility

`httpx.RateLimitMiddleware` and `SubjectFunc` are unchanged; the limiter is
injected as it is today. Existing route limits keep their current values.

## Extract boundary

The `Store`/`Limiter` interfaces, `MemoryStore`/`MemoryLimiter` and the tests move
as a unit and build with **no third-party module**. `redis.go` is the only file
that names `go-redis`, and it should be separable (a build tag or a sub-package)
so a Redis-free extraction needs no external dependency. The extract-test builds
the memory path in a clean module and runs its tests; the Redis path is verified
separately against a real Redis.

## Revision trigger

Revisit if a policy needs a sliding window rather than a fixed window, or if
per-route fail modes multiply into per-route limiter instances.
