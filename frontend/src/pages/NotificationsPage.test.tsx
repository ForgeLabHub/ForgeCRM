/**
 * Tests for the Notifications page.
 *
 * The page is the only interface to the notifications a workflow raises. The
 * assertions are about what it reads and what it sends: the unread filter is
 * forwarded, marking a row read posts against that row, and a read notification
 * offers no action.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { NotificationsPage } from './NotificationsPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

interface StubOptions {
  permissions?: string[]
  rows?: unknown[]
  onWrite?: (url: string) => void
}

function stubFetch(options: StubOptions = {}) {
  const permissions = options.permissions ?? ['notifications.read']
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/notifications')) {
      if (method === 'POST') {
        options.onWrite?.(url)
        return Promise.resolve(json({ status: 'read' }))
      }
      const unreadOnly = url.includes('unread_only=true')
      const rows = (options.rows ?? []).filter(
        (row) => !unreadOnly || !(row as { read_at?: string }).read_at,
      )
      return Promise.resolve(json({ items: rows, total: rows.length, limit: 25, offset: 0 }))
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <AuthProvider>
          <NotificationsPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const UNREAD = {
  id: 'n1',
  organization_id: 'o1',
  user_id: 'u1',
  type: 'workflow',
  title: 'Deal moved to Won',
  created_at: '2026-03-01T00:00:00Z',
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('NotificationsPage', () => {
  it('lists notifications and counts the unread ones', async () => {
    stubFetch({ rows: [UNREAD] })
    renderPage()

    expect(await screen.findByText('Deal moved to Won')).toBeInTheDocument()
    expect(await screen.findByText(/1 unread/)).toBeInTheDocument()
  })

  it('marks a notification read against that id', async () => {
    const writes: string[] = []
    stubFetch({ rows: [UNREAD], onWrite: (url) => writes.push(url) })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Mark read' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toContain('/api/v1/notifications/n1/read')
  })

  it('offers no action on an already read notification', async () => {
    stubFetch({ rows: [{ ...UNREAD, read_at: '2026-03-02T00:00:00Z' }] })
    renderPage()

    expect(await screen.findByText('Deal moved to Won')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mark read' })).not.toBeInTheDocument()
  })

  it('reports an empty inbox honestly', async () => {
    stubFetch({ rows: [] })
    renderPage()

    expect(await screen.findByText('No notifications')).toBeInTheDocument()
  })
})
