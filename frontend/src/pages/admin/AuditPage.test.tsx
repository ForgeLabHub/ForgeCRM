/**
 * Tests for the audit integrity surface.
 *
 * The page's job is to let an operator make a claim about the trail: that it is
 * intact, or that it was broken and where. The assertions are about the verdict
 * being shown plainly, the counts being reported, and the failure naming the first
 * affected entry rather than a generic error.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuditPage } from './AuditPage'
import { AuthProvider } from '../../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(verify: unknown) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: ['audit.read'],
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/audit/verify')) {
      return Promise.resolve(json(verify))
    }
    if (url.includes('/api/v1/audit')) {
      return Promise.resolve(json({ items: [], total: 0, limit: 25, offset: 0 }))
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
      <AuthProvider>
        <AuditPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('AuditPage integrity check', () => {
  it('reports an intact chain with the number of entries verified', async () => {
    stubFetch({ intact: true, checked: 12, unchained: 0 })
    renderPage()

    await userEvent.click(screen.getByRole('button', { name: 'Verify integrity' }))

    expect(await screen.findByText(/trail is intact/i)).toBeInTheDocument()
    expect(screen.getByText(/12 chained entries verified/i)).toBeInTheDocument()
  })

  it('names the first broken entry when the chain is tampered with', async () => {
    stubFetch({
      intact: false,
      checked: 3,
      unchained: 0,
      first_break_id: 7,
      reason: 'entry_hash does not match the entry contents',
    })
    renderPage()

    await userEvent.click(screen.getByRole('button', { name: 'Verify integrity' }))

    expect(await screen.findByText(/broken at entry #7/i)).toBeInTheDocument()
    expect(screen.getByText(/does not match the entry contents/i)).toBeInTheDocument()
  })

  it('mentions entries that predate the chain', async () => {
    stubFetch({ intact: true, checked: 4, unchained: 2 })
    renderPage()

    await userEvent.click(screen.getByRole('button', { name: 'Verify integrity' }))

    expect(await screen.findByText(/2 written before the chain existed/i)).toBeInTheDocument()
  })
})
