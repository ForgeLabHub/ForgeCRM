/**
 * The overview quick actions are create affordances.
 *
 * Each one links to a list page with `?new=1`, which opens that page's create
 * dialog. They were gated on the matching `.read` permission, so a read-only
 * user — the seeded `readonly_auditor` holds `accounts.read` but not
 * `accounts.create` — saw a "New account" button that opened a form the server
 * rejects. The interface must not offer an action the caller cannot perform.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { OverviewPage } from './OverviewPage'
import { AuthProvider } from '../hooks/useAuth'
import { I18nProvider } from '../i18n'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(permissions: string[]) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')
    if (path.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'org-1', name: 'Org' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    if (path.includes('/api/v1/system/info')) {
      return Promise.resolve(json({ version: '1.0.0', environment: 'test' }))
    }
    return Promise.resolve(json({ items: [], total: 0, limit: 5, offset: 0 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderOverview() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/']}>
        <I18nProvider>
          <AuthProvider>
            <OverviewPage />
          </AuthProvider>
        </I18nProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('OverviewPage quick actions', () => {
  it('offers the create actions to a user who may create records', async () => {
    stubFetch(['accounts.read', 'accounts.create', 'deals.create', 'tasks.create', 'tickets.create'])

    renderOverview()

    expect(await screen.findByRole('link', { name: /new account/i })).toBeDefined()
    expect(screen.getByRole('link', { name: /new deal/i })).toBeDefined()
    expect(screen.getByRole('link', { name: /new task/i })).toBeDefined()
    expect(screen.getByRole('link', { name: /new ticket/i })).toBeDefined()
  })

  it('hides every create action from a read-only user', async () => {
    stubFetch(['accounts.read', 'deals.read', 'tasks.read', 'tickets.read', 'reports.read', 'workflows.read'])

    renderOverview()

    // Wait for the session to load before asserting absence. The reports action
    // stays, because it only reads.
    expect(await screen.findByRole('link', { name: /reports/i })).toBeDefined()
    expect(screen.queryByRole('link', { name: /new account/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /new deal/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /new task/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /new ticket/i })).toBeNull()
  })
})
