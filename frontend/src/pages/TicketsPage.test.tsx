/**
 * Tests for the ticket detail page.
 *
 * The page reads a single ticket through `GET /api/v1/tickets/{id}`, which the
 * interface only reaches by clicking a row. These tests hold the page to that
 * path: the row click navigates to the detail route, the page reads the record
 * the server returns rather than a list, and a failure is surfaced instead of
 * rendering an empty card. A ticket that is already resolved must not offer the
 * Resolve control, because the server would reject a no-op update that the
 * button implies succeeded.
 */
import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { TicketDetailPage, TicketsPage } from './TicketsPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const ticket = {
  id: 'ticket-1',
  organization_id: 'org-1',
  subject: 'Cannot export invoices',
  description: 'The export button returns a 500',
  status: 'open',
  priority: 'high',
  channel: 'email',
  sla_due_at: '2026-11-05T09:00:00Z',
  created_at: '2026-10-01T09:00:00Z',
  updated_at: '2026-10-02T09:00:00Z',
}

function stubFetch(handler: (url: string) => Response | undefined) {
  const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')
    if (path.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'org-1', name: 'Org' },
          permissions: ['tickets.read', 'tickets.update'],
          csrf_token: 't',
        }),
      )
    }
    const handled = handler(path)
    if (handled) return Promise.resolve(handled)
    return Promise.resolve(json({ items: [], total: 0, limit: 25, offset: 0 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderDetail() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/tickets/ticket-1']}>
        <AuthProvider>
          <Routes>
            <Route path="/tickets/:id" element={<TicketDetailPage />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('TicketDetailPage', () => {
  it('renders the ticket the server returns for the id in the path', async () => {
    const fetchMock = stubFetch((path) =>
      path === '/api/v1/tickets/ticket-1' ? json(ticket) : undefined,
    )

    renderDetail()

    expect(await screen.findByRole('heading', { name: 'Cannot export invoices' })).toBeDefined()
    expect(screen.getByText('The export button returns a 500')).toBeDefined()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).includes('/api/v1/tickets/ticket-1')),
    ).toBe(true)
  })

  it('surfaces a load failure instead of an empty card', async () => {
    stubFetch((path) =>
      path === '/api/v1/tickets/ticket-1'
        ? json({ error: { code: 'not_found', message: 'ticket not found' } }, 404)
        : undefined,
    )

    renderDetail()

    expect(await screen.findByText(/ticket not found/i)).toBeDefined()
  })

  it('does not offer to resolve a ticket that is already resolved', async () => {
    stubFetch((path) =>
      path === '/api/v1/tickets/ticket-1'
        ? json({ ...ticket, status: 'resolved', resolved_at: '2026-10-03T09:00:00Z' })
        : undefined,
    )

    renderDetail()

    await screen.findByRole('heading', { name: 'Cannot export invoices' })
    expect(screen.queryByRole('button', { name: 'Resolve' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDefined()
  })
})

describe('TicketsPage row navigation', () => {
  it('opens the detail route when a row is clicked', async () => {
    stubFetch((path) =>
      path.startsWith('/api/v1/tickets?') || path === '/api/v1/tickets'
        ? json({ items: [ticket], total: 1, limit: 25, offset: 0 })
        : undefined,
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/tickets']}>
          <AuthProvider>
            <Routes>
              <Route path="/tickets" element={<TicketsPage />} />
              <Route path="/tickets/:id" element={<div>Ticket detail page</div>} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const user = userEvent.setup()
    await user.click(await screen.findByText('Cannot export invoices'))
    await waitFor(() => expect(screen.getByText('Ticket detail page')).toBeDefined())
  })
})
