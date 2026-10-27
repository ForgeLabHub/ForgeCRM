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
