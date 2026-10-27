/**
 * Tests for the permission gate and the sign-in flow.
 *
 * These render real components against a stubbed `fetch`, so they cover the
 * path from an HTTP response through the client, the auth context and the
 * router, rather than asserting against mocks of our own code.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { AuthProvider } from '../hooks/useAuth'
import { RequirePermission } from './RequirePermission'
import { LoginPage } from '../pages/LoginPage'
import { csrfTokenStore } from '../api/client'
import type { MeResponse } from '../types/api'

const ME: MeResponse = {
  user: {
    id: 'u-1',
    email: 'sales@example.com',
    display_name: 'Sam Sales',
    status: 'active',
    is_platform_owner: false,
    mfa_enabled: false,
  },
  organization: { id: 'o-1', name: 'Acme Inc' },
  permissions: ['contacts.read'],
  teams: [],
  csrf_token: 'csrf-from-me',
}

/** Routes requests by path so a test can describe a whole session. */
function stubFetch(routes: Record<string, () => Response | Promise<Response>>) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')
    const handler = routes[path.split('?')[0]]
    if (!handler) {
      return new Response(JSON.stringify({ error: { code: 'not_found', message: `no stub for ${path}` } }), {
        status: 404,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return handler()
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function renderApp(routes: Record<string, () => Response | Promise<Response>>, path = '/') {
  stubFetch(routes)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route
              path="/"
              element={
                <RequirePermission permission="contacts.read">
                  <div>Contacts workspace</div>
                </RequirePermission>
              }
            />
            <Route
              path="/admin"
              element={
                <RequirePermission permission="users.manage">
                  <div>Admin workspace</div>
                </RequirePermission>
              }
            />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('RequirePermission', () => {
  it('renders the page when the session holds the permission', async () => {
    renderApp({ '/api/v1/auth/me': () => json(ME) })

    expect(await screen.findByText('Contacts workspace')).toBeInTheDocument()
  })

  it('shows a refusal, not the page, when the permission is missing', async () => {
    renderApp({ '/api/v1/auth/me': () => json(ME) }, '/admin')

    expect(await screen.findByText(/do not have access/i)).toBeInTheDocument()
    // The permission is named so the user can ask for the right one.
    expect(screen.getByText(/users\.manage/)).toBeInTheDocument()
    expect(screen.queryByText('Admin workspace')).not.toBeInTheDocument()
  })

  it('does not leak page content when the session check fails', async () => {
    renderApp({
      '/api/v1/auth/me': () =>
        json({ error: { code: 'unauthorized', message: 'no session' } }, 401),
    })

    expect(await screen.findByText(/do not have access/i)).toBeInTheDocument()
    expect(screen.queryByText('Contacts workspace')).not.toBeInTheDocument()
  })
})

describe('sign-in', () => {
  it('stores the CSRF token returned at login so later mutations work', async () => {
    const user = userEvent.setup()
    csrfTokenStore.clear()
    renderApp({
      '/api/v1/auth/login': () =>
        json({
          user: ME.user,
          organization: ME.organization,
          permissions: ME.permissions,
          teams: [],
          csrf_token: 'csrf-from-login',
        }),
      '/api/v1/auth/me': () => json(ME),
    }, '/login')

    await user.type(await screen.findByLabelText(/email/i), 'sales@example.com')
    await user.type(screen.getByLabelText(/password/i), 'correct-horse-battery')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(csrfTokenStore.get()).toBe('csrf-from-me'))
    expect(await screen.findByText('Contacts workspace')).toBeInTheDocument()
  })

  it('asks for a second factor when the server requires one', async () => {
    const user = userEvent.setup()
    renderApp({
      '/api/v1/auth/login': () => json({ mfa_required: true, mfa_ticket: 'ticket-1' }),
      '/api/v1/auth/mfa/verify': () =>
        json({
          user: ME.user,
          organization: ME.organization,
          permissions: ME.permissions,
          teams: [],
          csrf_token: 'csrf-after-mfa',
        }),
      '/api/v1/auth/me': () => json(ME),
    }, '/login')

    await user.type(await screen.findByLabelText(/email/i), 'sales@example.com')
    await user.type(screen.getByLabelText(/password/i), 'correct-horse-battery')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByText(/two-factor verification/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/authentication code/i)).toBeInTheDocument()
    // The password must not be asked for again at this stage.
    expect(screen.queryByLabelText(/^password$/i)).not.toBeInTheDocument()
  })

  it('surfaces a rejected sign-in without leaving the form', async () => {
    const user = userEvent.setup()
    renderApp({
      '/api/v1/auth/login': () =>
        json({ error: { code: 'unauthorized', message: 'Invalid email or password' } }, 401),
      '/api/v1/auth/me': () =>
        json({ error: { code: 'unauthorized', message: 'no session' } }, 401),
    }, '/login')

    await user.type(await screen.findByLabelText(/email/i), 'sales@example.com')
    await user.type(screen.getByLabelText(/password/i), 'wrong')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByText(/invalid email or password/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })
})
