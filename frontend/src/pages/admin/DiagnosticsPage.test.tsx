/**
 * Tests for the platform-settings card of the Diagnostics page.
 *
 * The list and write endpoints both existed and a client method called the write,
 * but the client put the setting key in the body while the route reads it from the
 * `key` query parameter — so every save was rejected as a missing key, and nothing
 * rendered the store. These tests pin the two behaviours that matter: the current
 * values are shown, and a save sends the key as a query parameter (the contract
 * the server enforces) and the value as the body.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { DiagnosticsPage } from './DiagnosticsPage'
import { AuthProvider } from '../../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(permissions: string[] = ['system.settings']) {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/admin/settings') && method === 'PUT') {
      return Promise.resolve(json({ status: 'updated' }))
    }
    if (url.includes('/api/v1/admin/settings')) {
      return Promise.resolve(
        json({ items: { 'crm.default_currency': { value: 'USD', updated_at: '2026-01-01T00:00:00Z' } } }),
      )
    }
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
        <DiagnosticsPage />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('DiagnosticsPage platform settings', () => {
  it('shows the stored settings and offers editing to a permitted user', async () => {
    stubFetch()
    renderPage()

    expect(await screen.findByText('crm.default_currency')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })

  it('does not offer editing without the permission', async () => {
    stubFetch([])
    renderPage()

    expect(await screen.findByText('crm.default_currency')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
  })

  it('saves a setting with the key as a query parameter, not in the body', async () => {
    const fetchMock = stubFetch()
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    const input = await screen.findByLabelText('Value for crm.default_currency')
    await userEvent.clear(input)
    await userEvent.type(input, 'EUR')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const call = fetchMock.mock.calls.find(
        ([input, init]) =>
          String(input).includes('/api/v1/admin/settings') &&
          (init as RequestInit | undefined)?.method === 'PUT',
      )
      expect(call).toBeTruthy()
      const [input, init] = call!
      expect(String(input)).toContain('key=crm.default_currency')
      expect(JSON.parse(String((init as RequestInit).body))).toEqual({ value: 'EUR' })
    })
  })
})
