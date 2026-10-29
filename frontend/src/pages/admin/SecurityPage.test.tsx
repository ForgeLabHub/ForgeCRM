/**
 * Tests for the sessions tab of the Security page.
 *
 * The list endpoint and the revoke endpoint both existed, but nothing in the
 * interface called revoke: an operator could see the sessions open against their
 * account and had no way to end one. These tests pin the two behaviours that
 * matter — a session other than the current one offers a revoke control, and
 * confirming it issues a DELETE to that session's id. The current session is
 * deliberately not revocable from the list, so a stray click cannot sign the
 * operator out mid-task.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SecurityPage } from './SecurityPage'
import { AuthProvider } from '../../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const sessions = [
  {
    id: 's-current',
    created_at: '2026-01-01T00:00:00Z',
    expires_at: '2026-02-01T00:00:00Z',
    last_seen_at: '2026-01-02T00:00:00Z',
    ip: '10.0.0.1',
    user_agent: 'this browser',
    current: true,
  },
  {
    id: 's-other',
    created_at: '2026-01-01T00:00:00Z',
    expires_at: '2026-02-01T00:00:00Z',
    last_seen_at: '2026-01-02T00:00:00Z',
    ip: '10.0.0.2',
    user_agent: 'another device',
    current: false,
  },
]

function stubFetch() {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/auth/sessions/') && method === 'DELETE') {
      return Promise.resolve(json({ status: 'session revoked' }))
    }
    if (url.includes('/api/v1/auth/sessions')) {
      return Promise.resolve(json({ items: sessions }))
    }
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: ['security.read'],
          csrf_token: 't',
        }),
      )
    }
    return Promise.resolve(json({ items: [] }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <SecurityPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('SecurityPage sessions', () => {
  it('offers a revoke control for another session but not the current one', async () => {
    stubFetch()
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'My sessions' }))

    expect(await screen.findByText('another device')).toBeInTheDocument()
    // Exactly one revoke button: the other session's. The current session shows a
    // label instead.
    const buttons = await screen.findAllByRole('button', { name: 'Revoke' })
    expect(buttons).toHaveLength(1)
    expect(screen.getByText('Current')).toBeInTheDocument()
  })

  it('revokes the chosen session after confirmation', async () => {
    const fetchMock = stubFetch()
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'My sessions' }))

    await userEvent.click(await screen.findByRole('button', { name: 'Revoke' }))
    // The confirmation names the consequence before the request is sent.
    expect(await screen.findByText(/must authenticate again/i)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Revoke session' }))

    await waitFor(() => {
      const deleted = fetchMock.mock.calls.some(
        ([input, init]) =>
          String(input).includes('/api/v1/auth/sessions/s-other') &&
          (init as RequestInit | undefined)?.method === 'DELETE',
      )
      expect(deleted).toBe(true)
    })
  })
})
