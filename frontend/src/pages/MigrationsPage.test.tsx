/**
 * Tests for the Migrations page.
 *
 * The assertion that matters is where the source-type list comes from. A migration
 * is created by posting `source_type` to the server, which refuses a kind it cannot
 * build. If the form built its options from a local constant it could offer a kind
 * the server refuses (it once offered `sqlserver`, and spelled REST `rest` while the
 * server calls it `rest_api`), so the test drives the picker from a stubbed
 * `migration_sources` capability and proves the form sends exactly that vocabulary.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { MigrationsPage } from './MigrationsPage'
import { AuthProvider } from '../hooks/useAuth'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function stubFetch(options: { migrationSources?: string[]; onWrite?: (url: string, body: unknown) => void } = {}) {
  const migrationSources = options.migrationSources ?? ['postgres', 'mysql', 'sqlite', 'rest_api']
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions: ['migrations.read', 'migrations.manage'],
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/system/capabilities')) {
      return Promise.resolve(json({ migration_sources: migrationSources }))
    }
    if (url.includes('/api/v1/migrations')) {
      if (method === 'POST') {
        const body = init?.body ? JSON.parse(String(init.body)) : {}
        options.onWrite?.(url, body)
        return Promise.resolve(json({ id: 'm1', name: 'x', source_type: body.source_type, status: 'pending' }))
      }
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
      <MemoryRouter>
        <AuthProvider>
          <MigrationsPage />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('MigrationsPage source picker', () => {
  it('offers exactly the kinds the server advertises, and not a refused one', async () => {
    // sqlserver is recognised by the server as a value but refused with "not
    // implemented", so it must not appear in the picker.
    stubFetch({ migrationSources: ['postgres', 'mysql', 'sqlite', 'rest_api'] })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'New migration' }))

    const select = await screen.findByLabelText(/Source type/i)
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.getAttribute('value'))
    expect(values).toEqual(['postgres', 'mysql', 'sqlite', 'rest_api'])
    expect(values).not.toContain('sqlserver')
  })

  it('sends the chosen source type, defaulting to the first advertised kind', async () => {
    const writes: Array<{ source_type?: string }> = []
    stubFetch({ migrationSources: ['mysql', 'postgres'], onWrite: (_url, body) => writes.push(body as { source_type?: string }) })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'New migration' }))
    await userEvent.type(screen.getByLabelText(/Name/i), 'Legacy cutover')
    await userEvent.click(screen.getByRole('button', { name: /Create|Save|Start/i }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].source_type).toBe('mysql')
  })
})
