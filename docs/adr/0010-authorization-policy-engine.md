# ADR 0010 — Authorization Policy Engine

- **Status:** Proposed (plan; no code yet)
- **Date:** 2026-10-29
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0009 (security as independent building blocks)

## Context

Authorization in ForgeCRM today is real but not a module. Three layers enforce it:

1. `rbac.Principal` carries a permission set and exposes `Has`, `HasAll`,
   `HasAny`, `Require`. It is built from the user's roles and team memberships.
2. `api` middleware calls `Require(perm)` before a handler runs, per route. The
   route's access level is declared so `internal/openapi` can enforce that every
   route names one.
3. `crm` adds object-level checks: tenant scoping (`organization_id`) and
   ownership, so a user cannot read another organisation's record by changing an
   id in the URL.

This works. Its problem is that it cannot be tested or extracted as a unit: there
is no single place that answers "may this subject do this action to this
resource?", so BOLA/IDOR behaviour is proven only through the full HTTP stack, and
a new project that wants the same authorization must copy the middleware, the
catalog and the ownership checks separately.

The security risk this ADR addresses is **BOLA/IDOR**: validating a session is not
authorizing a resource. A valid JWT plus a permission is not enough if the
resource belongs to another tenant or another owner.

## Proposed contract

```go
type Subject struct {
	UserID   string
	TenantID string
	Roles    []string
}

type Action string

type Resource struct {
	Type     string
	ID       string
	TenantID string
	OwnerID  string
}

type Decision struct {
	Allow  bool
	Reason string
}

type Authorizer interface {
	Authorize(ctx context.Context, subject Subject, action Action, resource Resource) (Decision, error)
}
```

This is a **starting shape**, not a fixed one. It must be checked against the
existing model before implementation:

- `rbac.Principal` uses `uuid.UUID` ids and a `map[Permission]struct{}`; the
  contract's string ids and role list must map onto it without loss.
- Permissions are a fixed catalog (`rbac.Permission`), not free-form actions. The
  `Action` type should resolve to that catalog, or the catalog becomes a second
  source of truth.
- The existing ownership rule lives in `crm`; the engine must reproduce it exactly
  or the two disagree.

If the contract cannot express a real current rule, it is corrected — the existing
authorization is the source of truth, and a new model must not silently change a
decision.

## Implementations

- `RBACAuthorizer` — the production adapter, built from the existing
  `rbac.Service` and the catalog. Its first requirement is **decision parity**: for
  every case the current inline path decides, it decides the same. This is proven
  by tests that run both and compare.
- `FakeAuthorizer` — deterministic, returns a configured decision per
  (action, resource). It can deny, and it can return an error, so a test can drive
  the failure path.
- `AllowAllAuthorizer` — allowed **only** in explicitly selected demo/test
  profiles. `config.IsProduction()` must refuse it (see `CONFIG_PROFILES.md`).

## Required checks

Every implementation is tested against at least:

- a subject reading its own resource → allow;
- a subject reading another owner's resource → deny;
- a subject from another tenant → deny;
- an id swapped in the URL to a foreign resource → deny;
- a subject deleting another's resource → deny;
- a subject raising its own role → deny;
- an unknown resource type → deny (not allow);
- an unknown action → deny;
- a malformed tenant → deny;
- an authorizer error → deny (fail closed), never allow.

**Unknown and error cases must never accidentally allow.** This is the rule that
BOLA bugs violate.

## Mass assignment

Authorization is not complete without input control. The engine does not read the
request body; handlers must bind explicit DTOs with a field whitelist. A client
must never be able to set `role`, `tenant_id`, `owner_id`, or a system status
through a create/update payload. Negative tests assert each is ignored or refused.

## Compatibility and risks

- **Behavioural drift** is the main risk: a re-implementation that decides
  differently is a security regression. Mitigation: parity tests against the
  current path before the engine is used for real decisions.
- **Second catalog** is the second risk. Mitigation: the engine consumes
  `rbac.Permission`; it does not define its own action strings.
- **AllowAll leakage** is the third. Mitigation: refused under `IsProduction()`.

## Extract boundary

The engine, its fakes and its tests move as a unit. It must not import `api` or
`wiring`; it depends on `rbac` (catalog + principal) and, for the real adapter,
`database`. The extract-test builds it in a clean module with the fake authorizer
and runs the BOLA smoke without the CRM, the frontend or Compose.

## Revision trigger

Revisit if a real rule cannot be expressed (a conditional permission, a
relationship-based rule), or if decision parity with the inline path cannot be
reached without changing a current decision.
