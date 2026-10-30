# API reference

The API specification is generated from the running router. It cannot describe a
route the server does not serve, and it cannot miss one the server does.

- **Browsable reference:** [`index.html`](./index.html) — a single self-contained
  page, generated from the specification
- **Machine-readable specification:** [`openapi.json`](./openapi.json) (OpenAPI 3.1)
- **Error contract:** [`errors.md`](./errors.md)
- **Permission reference:** `docs/security/permissions.md`

## The browsable reference

`index.html` is generated from the same document as `openapi.json`, so it is a
view of the specification rather than a second, hand-maintained description. It
opens straight from disk — the stylesheet is inlined and the only script is a few
lines of operation filter — so it needs no network, no CDN and no server. Every
operation the document declares is rendered as its own anchored section, grouped
by the document's tags, with its parameters, request body and responses.

Because it is generated, it cannot drift: `go run ./cmd/openapi-gen -check` fails
if `index.html` is out of date in the same way it fails for `openapi.json`. A
route change means running the generator and committing both files.

## Why the specification is generated

A hand-written specification drifts on the first route addition. The drift is
silent: the document still validates, clients still generate, and the difference
only surfaces when a client calls an endpoint that has moved. Generating the
document from the router removes that failure mode.

The generator does three things:

1. **Walks the real router.** `cmd/openapi-gen` builds the same `chi.Router` the
   server serves and enumerates every method and path with `chi.Walk`. Paths and
   methods come from the router, never from a list someone maintains by hand.
2. **Reads permissions from the route registrations.** `chi.Walk` exposes only the
   innermost handler, so the permission middleware is not visible at runtime. The
   generator parses the `mount*` functions in `internal/api` and resolves each
   `rbac.Perm*` constant to its string value by reading `internal/rbac`. The
   statement that installs the middleware is the statement that is parsed, so the
   document and the enforcement cannot disagree.
3. **Fails on an undeclared route.** A route that is neither permission-annotated
   nor listed in `openapi.Public` with a reason makes generation fail. An
   unauthenticated endpoint cannot appear by accident.

## Regenerating

```bash
cd backend
go run ./cmd/openapi-gen
```

CI runs the check mode, which regenerates in memory and compares with the
committed file:

```bash
go run ./cmd/openapi-gen -check
```

If the check fails, the API and the specification disagree. Run the generator and
commit the result. Do not edit `openapi.json` by hand: the next run overwrites it,
and the check will fail again.

## Contract tests

`internal/openapi/openapi_test.go` is the contract. It asserts, against the live
router:

| Test | What it prevents |
| --- | --- |
| `TestCommittedSpecMatchesTheRouter` | A route change merged without regenerating the document. |
| `TestEveryRouteIsDocumented` | The generator silently dropping routes. |
| `TestNoRouteIsInvented` | The document promising an endpoint that returns 404. |
| `TestEveryRouteDeclaresItsAccessRequirement` | An authenticated endpoint with no access statement. |
| `TestPermissionsAreResolvedToStringValues` | A document that says `PermContactsRead` instead of `contacts.read`. |
| `TestPublicRoutesDoNotInheritSessionSecurity` | A sign-in endpoint that tells clients to send a session cookie. |
| `TestSessionRoutesInheritSecurity` | A protected endpoint that silently becomes anonymous. |
| `TestPathParametersAreTyped` | Identifiers typed as `string`, so clients cannot validate them. |
| `TestOperationIDsAreUnique` | Two endpoints collapsing into one generated SDK method. |
| `TestGenerationIsDeterministic` | Output that churns on every run, so it can never be committed. |
| `TestSpecIsValidJSON` | An encoding failure that breaks every client. |
| `TestRoutesAreSorted` | Diff noise from unstable ordering. |

## Document structure

The document groups operations into tags derived from the path, so a new route is
classified without anyone remembering to say so:

| Tag | Path prefix |
| --- | --- |
| `system` | `/health`, `/ready`, `/api/v1/system` |
| `authentication` | `/api/v1/auth` |
| `users-and-permissions` | `/api/v1/users`, `/roles`, `/permissions`, `/teams`, `/organizations` |
| `crm` | The remaining `/api/v1` resources |
| `import-and-export` | `/api/v1/imports`, `/exports` |
| `migrations` | `/api/v1/migrations` |
| `integrations` | `/api/v1/integrations` |
| `events` | `/api/v1/events`, `/webhooks` |
| `jobs` | `/api/v1/jobs` |
| `audit-and-diagnostics` | `/api/v1/audit`, `/admin` |
| `notifications` | `/api/v1/notifications` |
| `ai-and-mcp` | `/api/v1/ai`, `/api/v1/mcp` |

A route that lands in the `other` tag has a surprising path. Treat it as a signal
to reconsider the path rather than a classification to fix by hand.

## Known limits

The specification is generated, but two parts are still declared rather than
derived, and both are listed here so the document is not read as more complete
than it is:

- **Query parameters.** Collection operations declare the shared
  `limit`/`offset`/`q`/`sort`/`order` set. Resource-specific filters are declared
  for the routes that register them in `internal/api/contract.go`; a route whose
  filters are not registered there is documented without them, so a filter the
  server accepts may be missing from the document.
- **Conditional validation rules.** A rule that cannot be expressed as a required
  list — "at least one of these", "one of a fixed set" — is recorded in the
  request body description rather than the `required` set. `contract_test.go`
  fails if a rule the validator enforces is neither declared required nor recorded
  as conditional.

Both gaps are tracked in `docs/ROADMAP.md`. Neither affects the guarantee that
matters most: every route the server serves is listed, with the permission it
requires.
