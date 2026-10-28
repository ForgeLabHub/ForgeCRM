# Frontend development

The frontend is a React + TypeScript single-page application built with Vite. It
is served by the same Nginx instance as the API, so every request is same-origin.
That is what lets the session cookie and the CSRF double-submit work without any
CORS configuration.

## Stack

| Concern | Choice |
| --- | --- |
| Build tool | Vite 6 |
| Language | TypeScript 5.7 (strict) |
| UI | React 19 |
| Routing | React Router 7 |
| Server state | TanStack Query 5 |
| Tests | Vitest 3 with Testing Library |
| Lint | ESLint 9 (flat config) |

There is no component library and no CSS framework. `src/index.css` holds a small
set of design tokens and the primitives the application needs, which keeps the
bundle small and the markup readable.

## Layout

```
frontend/src/
  api/          One module per backend area, plus the shared HTTP client
  components/   Reusable UI: shell, table list, permission gate, primitives
  hooks/        Auth context and session state
  pages/        One file per route; admin pages live in pages/admin/
  test/         Vitest setup
  types/        API response types, kept in step with the Go handlers
```

## Running it

The API must be running first. Then:

```bash
cd frontend
npm ci
npm run dev
```

Vite proxies `/api` and `/health` to `http://127.0.0.1:8080` by default. Point it
elsewhere with `FORGECRM_API_URL`.

## Checks

```bash
npm run typecheck   # tsc --noEmit
npm run lint        # eslint
npm run test        # vitest run
npm run build       # tsc -b && vite build
```

All four must pass before a change is mergeable.

## How the frontend talks to the API

Two rules shape `src/api/client.ts`, and both exist for security rather than
convenience:

1. **The session lives in an HttpOnly cookie.** The browser cannot read it and
   the client never tries to. No credential is ever put in `localStorage`, so an
   XSS bug cannot exfiltrate a long-lived token.
2. **Every mutation carries a CSRF token** that the server derived for the
   current session. The token is not a secret on its own — it only proves the
   request came from our own page — and it is held in memory, refreshed from
   `/auth/me` after a reload.

A `401` clears the stored CSRF token and notifies the application so the user is
returned to the sign-in page. A `403` deliberately does not: a permission failure
must not sign the user out.

## Authorisation in the interface

`useAuth()` exposes `can(permission)`, and `RequirePermission` guards a route.
These control what is *shown*; they are not the security boundary. The server
re-checks every request, and the interface is written on the assumption that a
user may know a record ID they are not allowed to read.

## Adding a page

1. Add the API calls to the relevant module under `src/api/`.
2. Add or extend the response types in `src/types/api.ts`. Keep them matched to
   the Go handler's JSON, including nullability.
3. Build the page under `src/pages/`, reusing `ResourceList` for any paginated
   collection — it already handles search, paging and the empty and error states.
4. Register the route in `src/App.tsx` and, if it belongs in the navigation, in
   `src/components/AppShell.tsx`.
5. Add a test if the page contains logic worth testing. Tests exercise real
   components against a stubbed `fetch` rather than mocking our own modules.

## Testing approach

`fetch` is stubbed at the boundary, so the client, the auth context, the query
cache and the components all run for real. Mocking our own modules would test
that the mocks agree with each other and nothing else. See
`src/api/client.test.ts`, `src/components/ResourceList.test.tsx` and
`src/components/RequirePermission.test.tsx`.

## End-to-end tests

The unit tests above stub `fetch`, which proves the components behave. They
cannot prove that a button performs the operation it claims to, because the
operation itself is the thing being stubbed. The end-to-end suite closes that
gap: it drives a real Chromium browser against the real Go API and a real
PostgreSQL database, with no network stubbing anywhere.

```bash
# 1. PostgreSQL with migrations applied
createdb forgecrm_e2e
cd backend
FORGECRM_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_e2e?sslmode=disable' \
FORGECRM_SECRET_KEY='a-development-secret-key-long-enough-for-hkdf' \
FORGECRM_ENV=development \
  go run ./cmd/forgecrm-api -migrate

# 2. A platform owner, created through the same path an operator uses
FORGECRM_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_e2e?sslmode=disable' \
FORGECRM_SECRET_KEY='a-development-secret-key-long-enough-for-hkdf' \
FORGECRM_ENV=development \
  ../scripts/e2e-bootstrap.sh

# 3. The API
FORGECRM_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_e2e?sslmode=disable' \
FORGECRM_SECRET_KEY='a-development-secret-key-long-enough-for-hkdf' \
FORGECRM_ENV=development \
FORGECRM_HTTP_ADDR=127.0.0.1:8080 \
  go run ./cmd/forgecrm-api

# 4. The suite; Playwright starts the Vite dev server itself
cd ../frontend
FORGECRM_API_URL='http://127.0.0.1:8080' npm run e2e
```

CI runs exactly these steps as the `e2e` job in `.github/workflows/ci.yml`.

### Two things that will bite you

**Raise the rate limits when running the suite.** The suite signs in repeatedly
and drives many requests from a single address, which the production budgets are
correct to refuse. Set `FORGECRM_RATE_LIMIT_PER_MIN` and
`FORGECRM_LOGIN_RATE_LIMIT_PER_MIN` high for a local run, as CI does. A `429` in
a failure message is the product working, not a broken test.

**The tests share one database and must run one at a time.** `workers: 1` and
`fullyParallel: false` are set in `playwright.config.ts` for that reason.

### Writing a test

Prefer asserting on the effect over the rendering. After a write, re-read the
record through the API and confirm the value landed; then reload the page and
confirm it is still there. A test that only checks a toast appeared will pass
even when nothing was saved.

For the page-load cases, assert on the page's own `h1`, not the shell's. The
application shell renders `<h1>ForgeCRM</h1>` on every route, so a test that
looks for any `h1` passes even when the route fell through to the overview.

Target form inputs by their stable `id` (`#account-name`) rather than their
label. A required field renders its label as `Name *`, which makes the visible
text differ from the `label` prop and breaks a label-based locator.
