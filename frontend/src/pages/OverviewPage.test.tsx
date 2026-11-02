/**
 * Tests for the Overview page.
 *
 * Two things break silently here and are pinned below: a count that arrives
 * wrapped rather than as a number (and the hero heading the e2e fixture waits
 * on), and the create affordances, which must not be offered to a caller who
 * cannot perform them.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { OverviewPage } from './OverviewPage'
import { I18nProvider } from '../i18n'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const ALL_PERMISSIONS = [
  'accounts.read',
  'accounts.create',
  'deals.read',
  'deals.create',
  'leads.read',
  'tasks.read',
  'tasks.create',
  'tickets.read',
  'tickets.create',
  'reports.read',
  'workflows.read',
  'imports.read',
]

function stubFetch(permissions: string[] = ALL_PERMISSIONS) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')
    if (path.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'Ada' },
          organization: { id: 'o1', name: 'ForgeLab' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    if (path.includes('/api/v1/system/info')) {
      return Promise.resolve(
        json({
          service: 'forgecrm-api',
          version: '1.2.3',
          commit: 'abcdef0123456789',
          environment: 'development',
          started_at: '2026-01-01T00:00:00Z',
          uptime_seconds: 60,
          database: 'ok',
          server_time: '2026-01-01T00:01:00Z',
        }),
      )
    }
    if (path.includes('/api/v1/accounts')) {
      return Promise.resolve(json({ items: [], total: 7, limit: 1, offset: 0 }))
    }
    if (path.includes('/api/v1/deals')) {
      return Promise.resolve(json({ items: [], total: 3, limit: 1, offset: 0 }))
    }
    if (path.includes('/api/v1/leads')) {
      return Promise.resolve(json({ items: [], total: 4, limit: 1, offset: 0 }))
    }
    if (path.includes('/api/v1/tasks')) {
      return Promise.resolve(
        json({
          items: [
            { id: 't1', title: 'Call the lead', priority: 'urgent', due_at: '2026-02-01T00:00:00Z' },
          ],
          total: 5,
          limit: 5,
          offset: 0,
        }),
      )
    }
    if (path.includes('/api/v1/tickets')) {
      return Promise.resolve(
        json({
          items: [
            {
              id: 'k1',
              subject: 'Printer on fire',
              priority: 'high',
              created_at: '2026-01-02T00:00:00Z',
            },
          ],
          total: 2,
          limit: 5,
          offset: 0,
        }),
      )
    }
    return Promise.resolve(json({ items: [], total: 0, limit: 5, offset: 0 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
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

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('OverviewPage', () => {
  it('renders the greeting as the page heading', async () => {
    stubFetch()
    renderPage()
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent('Welcome back, Ada')
  })

  it('shows each permitted count in its own tile', async () => {
    stubFetch()
    renderPage()

    const accounts = await screen.findByText('Accounts', { selector: '.metric__label' })
    expect(await screen.findByText('7', { selector: '.metric__value' })).toBeInTheDocument()
    expect(accounts.closest('.metric')?.textContent).toContain('7')
    const tasks = screen.getByText('Open tasks', { selector: '.metric__label' })
    expect(tasks.closest('.metric')?.textContent).toContain('5')
  })

  it('lists open tasks and tickets as links to their detail pages', async () => {
    stubFetch()
    renderPage()

    const task = await screen.findByText('Call the lead')
    expect(task.closest('a')).toHaveAttribute('href', '/tasks/t1')
    const ticket = screen.getByText('Printer on fire')
    expect(ticket.closest('a')).toHaveAttribute('href', '/tickets/k1')
  })

  it('offers quick actions for the pages the user may open', async () => {
    stubFetch()
    renderPage()

    const actions = await screen.findByText('Quick actions')
    expect(actions).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Workflows/ })).toHaveAttribute('href', '/workflows')
  })

  it('offers the create actions to a user who may create records', async () => {
    stubFetch()
    renderPage()

    expect(await screen.findByRole('link', { name: /new account/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /new deal/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /new task/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /new ticket/i })).toBeInTheDocument()
  })

  it('hides every create action from a read-only user', async () => {
    stubFetch([
      'accounts.read',
      'deals.read',
      'tasks.read',
      'tickets.read',
      'reports.read',
      'workflows.read',
    ])

    renderPage()

    // Wait for the session to load before asserting absence. The reports action
    // stays, because it only reads.
    expect(await screen.findByRole('link', { name: /reports/i })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /new account/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /new deal/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /new task/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /new ticket/i })).toBeNull()
  })
})
